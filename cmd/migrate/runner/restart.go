package runner

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	restartRepo       string
	restartRunnerOpts types.RunnerOptions
)

// NewRestartCmd creates the runner restart command
func NewRestartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart [[HOST]/ORG]",
		Short: "Restart the self-hosted runner listener from state",
		Long: `Restart the self-hosted runner listener for secret migration.

This command reads .gh-secret-kit-state.json from the current working directory,
reuses the saved runner scale set and runner directory, and starts the foreground
message session listener again. Use this when runner setup was interrupted and
the state file still exists.

When a source argument is given on the CLI it is validated against the
source stored in the state file. If they do not match the command aborts
without touching the state file.

Arguments:
  org   Organization name for organization-scoped runner (optional).
        Must match the source saved in the state file when provided.`,
		RunE: runRestart,
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&restartRepo, "repo", "R", "", "Source repository (owner/repo); validated against state when provided")
	f.IntVar(&restartRunnerOpts.MaxRunners, "max-runners", 2, "Maximum number of concurrent runners")

	return cmd
}

func runRestart(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	logger.Info("Restarting runner listener from migration state")

	state, err := migrator.LoadState()
	if err != nil {
		return err
	}
	if state.ConfigURL == "" {
		return fmt.Errorf("state does not contain a source config URL")
	}
	if state.RunnerLabel == "" {
		return fmt.Errorf("state does not contain a runner label")
	}

	sourceRepo, err := migrator.ParseConfigURL(state.ConfigURL)
	if err != nil {
		return fmt.Errorf("failed to parse source from state: %w", err)
	}
	if restartRepo != "" || len(args) > 0 {
		explicitRepo, err := resolveSourceRepo(restartRepo, args, state.RunnerLabel)
		if err != nil {
			return err
		}
		explicitConfigURL := migrator.BuildGitHubConfigURL(explicitRepo)
		if explicitConfigURL != state.ConfigURL {
			return fmt.Errorf("specified source %s does not match state source %s; aborting to protect the state file", explicitConfigURL, state.ConfigURL)
		}
	}

	if state.RunnerDir == "" {
		state.RunnerDir, err = migrator.RunnerDirPathForCwd()
		if err != nil {
			return fmt.Errorf("failed to determine runner directory: %w", err)
		}
	}

	scalesetClient, err := migrator.NewScaleSetClient(state.ConfigURL)
	if err != nil {
		return fmt.Errorf("failed to create scaleset client: %w", err)
	}

	scaleSet, err := migrator.GetRunnerScaleSetByID(ctx, scalesetClient, state.ScaleSetID)
	if err != nil {
		return fmt.Errorf("failed to get runner scale set from state: %w", err)
	}
	if scaleSet == nil {
		return fmt.Errorf("runner scale set from state was not found: %d", state.ScaleSetID)
	}
	if state.RunnerGroupName == "" && scaleSet.RunnerGroupID != migrator.DefaultRunnerGroupID {
		state.RunnerGroupName = scaleSet.RunnerGroupName
	}

	logger.Info(fmt.Sprintf("Using runner scale set: ID=%d, Name=%s", scaleSet.ID, scaleSet.Name))
	logger.Info(fmt.Sprintf("Using runner label: %s", state.RunnerLabel))

	return runListenerForState(ctx, sourceRepo, scalesetClient, state, restartRunnerOpts.MaxRunners)
}
