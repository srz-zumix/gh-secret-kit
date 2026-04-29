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
	restartRunnerOpts types.RunnerOptions
)

// NewRestartCmd creates the runner restart command
func NewRestartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the self-hosted runner listener from state",
		Long: `Restart the self-hosted runner listener for secret migration.

This command reads .gh-secret-kit-state.json from the current working directory,
reuses the saved runner scale set and runner directory, and starts the foreground
message session listener again. Use this when runner setup was interrupted and
the state file still exists. The source repository or organization is read from
the state file.`,
		RunE: runRestart,
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
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
	if state.RunnerLabel == "" {
		return fmt.Errorf("state does not contain a runner label")
	}
	if state.ScaleSetID <= 0 {
		return fmt.Errorf("state does not contain a valid runner scale set ID")
	}

	sourceRepo, err := resolveStateSourceRepo(state.Source)
	if err != nil {
		return err
	}
	state.ConfigURL = migrator.BuildGitHubConfigURL(sourceRepo)

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
