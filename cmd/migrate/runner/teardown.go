package runner

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	teardownRepo       string
	teardownRunnerOpts types.RunnerOptions
)

// NewTeardownCmd creates the runner teardown command
func NewTeardownCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "teardown [[HOST]/ORG]",
		Short: "Unregister and stop the self-hosted runner",
		Long: `Unregister and stop the self-hosted runner for secret migration.

This command reads .gh-secret-kit-state.json from the current working directory,
stops the runner process, deletes the runner scale set from the source
repository/organization, and cleans up local runner files.

If the runner group was created during setup, it is also deleted.

When a source argument is given on the CLI it is validated against the
source stored in the state file. If they do not match the command aborts
without touching the state file, so the original setup can still be torn
down correctly.

Arguments:
  org   Organization name for organization-scoped runner (optional).
        Must match the source saved in the state file when provided.`,
		RunE: runTeardown,
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()

	// Common flags
	f.StringVarP(&teardownRepo, "repo", "R", "", "Source repository (owner/repo); validated against state when provided")

	// Runner-specific flags (fallback when state file is unavailable)
	f.StringVar(&teardownRunnerOpts.RunnerLabel, "runner-label", types.DefaultRunnerLabel, "Label of the runner to tear down (read from state when available)")
	f.StringVar(&teardownRunnerOpts.RunnerGroup, "runner-group", "", "Runner group name to search for the scale set (read from state when available)")

	return cmd
}

func runTeardown(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	logger.Info("Tearing down runner for migration")

	// Load state from the current working directory.
	// State is the authoritative source for source, runner label, runner group, and runner dir.
	state, stateErr := migrator.LoadState()
	if stateErr != nil {
		logger.Warn(fmt.Sprintf("No state file found in current directory: %v", stateErr))
	}

	// Determine config URL and source repo.
	// When state is available it is the primary source of truth.
	var configURL string
	var sourceRepo repository.Repository

	if stateErr == nil {
		configURL = state.ConfigURL
		var parseErr error
		sourceRepo, parseErr = migrator.ParseConfigURL(state.ConfigURL)
		if parseErr != nil {
			return fmt.Errorf("failed to parse source from state: %w", parseErr)
		}
		// Validate CLI source against state to prevent accidental teardown of the wrong runner.
		if teardownRepo != "" || len(args) > 0 {
			explicitRepo, err := resolveSourceRepo(teardownRepo, args, teardownRunnerOpts.RunnerLabel)
			if err != nil {
				return err
			}
			explicitConfigURL := migrator.BuildGitHubConfigURL(explicitRepo)
			if explicitConfigURL != configURL {
				return fmt.Errorf("specified source %s does not match state source %s; aborting to protect the state file", explicitConfigURL, configURL)
			}
		}
	} else {
		// No state: fall back to CLI-specified source.
		var err error
		sourceRepo, err = resolveSourceRepo(teardownRepo, args, teardownRunnerOpts.RunnerLabel)
		if err != nil {
			return err
		}
		configURL = migrator.BuildGitHubConfigURL(sourceRepo)
	}

	// Effective runner label and group: prefer state over flag.
	effectiveRunnerLabel := teardownRunnerOpts.RunnerLabel
	if stateErr == nil && state.RunnerLabel != "" {
		effectiveRunnerLabel = state.RunnerLabel
	}

	// Create scaleset client.
	scalesetClient, err := migrator.NewScaleSetClient(configURL)
	if err != nil {
		return fmt.Errorf("failed to create scaleset client: %w", err)
	}

	// Resolve runner group ID: prefer state over flag.
	runnerGroupID := migrator.DefaultRunnerGroupID
	if stateErr == nil && state.RunnerGroupID > 0 {
		runnerGroupID = state.RunnerGroupID
	} else if teardownRunnerOpts.RunnerGroup != "" {
		group, err := migrator.GetRunnerGroupByName(ctx, scalesetClient, teardownRunnerOpts.RunnerGroup)
		if err != nil {
			logger.Warn(fmt.Sprintf("Failed to find runner group '%s': %v", teardownRunnerOpts.RunnerGroup, err))
			logger.Warn("Falling back to default runner group for scale set lookup")
		} else {
			runnerGroupID = group.ID
			logger.Info(fmt.Sprintf("Using runner group '%s' (ID=%d) for scale set lookup", group.Name, group.ID))
		}
	}

	// Stop runner process if state has a PID (legacy or interrupted listener).
	if stateErr == nil && state.RunnerPID > 0 {
		logger.Info(fmt.Sprintf("Stopping runner process (PID: %d)...", state.RunnerPID))
		if err := migrator.StopRunner(state.RunnerPID); err != nil {
			logger.Warn(fmt.Sprintf("Failed to stop runner process: %v", err))
		} else {
			logger.Info("Runner process stopped")
		}
	}

	// Delete scale set.
	var scaleSetDeleted bool
	if stateErr == nil && state.ScaleSetID > 0 {
		logger.Info(fmt.Sprintf("Deleting runner scale set (ID: %d)...", state.ScaleSetID))
		if err := migrator.DeleteRunnerScaleSet(ctx, scalesetClient, state.ScaleSetID); err != nil {
			logger.Warn(fmt.Sprintf("Failed to delete scale set by ID: %v", err))
		} else {
			scaleSetDeleted = true
			logger.Info("Runner scale set deleted")
		}
	}

	if !scaleSetDeleted {
		logger.Info(fmt.Sprintf("Looking up runner scale set by name: %s", effectiveRunnerLabel))
		scaleSet, err := migrator.FindRunnerScaleSet(ctx, scalesetClient, effectiveRunnerLabel, runnerGroupID)
		if err != nil {
			logger.Warn(fmt.Sprintf("Failed to find scale set by name: %v", err))
		} else if scaleSet != nil {
			logger.Info(fmt.Sprintf("Deleting runner scale set (ID: %d)...", scaleSet.ID))
			if err := migrator.DeleteRunnerScaleSet(ctx, scalesetClient, scaleSet.ID); err != nil {
				logger.Warn(fmt.Sprintf("Failed to delete scale set: %v", err))
			} else {
				scaleSetDeleted = true
				logger.Info("Runner scale set deleted")
			}
		} else {
			logger.Info("No runner scale set found to delete")
		}
	}

	// Delete runner group if it was created during setup.
	if stateErr == nil && state.RunnerGroupCreated && state.RunnerGroupID > 0 {
		logger.Info(fmt.Sprintf("Deleting runner group (ID: %d) created during setup...", state.RunnerGroupID))
		ghClient, err := gh.NewGitHubClientWithRepo(sourceRepo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Failed to create GitHub client for runner group deletion: %v", err))
		} else {
			if err := gh.DeleteOrgRunnerGroup(ctx, ghClient, sourceRepo, int64(state.RunnerGroupID)); err != nil {
				logger.Warn(fmt.Sprintf("Failed to delete runner group: %v", err))
			} else {
				logger.Info("Runner group deleted")
			}
		}
	}

	// Clean up local runner files.
	runnerDir := ""
	if stateErr == nil && state.RunnerDir != "" {
		runnerDir = state.RunnerDir
	} else {
		runnerDir, _ = migrator.RunnerDirPathForCwd()
	}

	if runnerDir != "" {
		instancesDir := migrator.RunnerInstancesBaseDir(runnerDir)
		logger.Info(fmt.Sprintf("Removing registered runners in: %s", instancesDir))
		migrator.RemoveRunnerInstances(instancesDir)

		if err := migrator.RemoveRunner(runnerDir); err != nil {
			logger.Warn(fmt.Sprintf("Failed to remove runner from template dir: %v", err))
		}

		logger.Info(fmt.Sprintf("Cleaning up runner directory: %s", runnerDir))
		if err := migrator.CleanupRunnerDir(runnerDir); err != nil {
			logger.Warn(fmt.Sprintf("Failed to clean up runner directory: %v", err))
		} else {
			logger.Info("Runner directory cleaned up")
		}

		if err := migrator.CleanupRunnerDir(instancesDir); err != nil {
			logger.Warn(fmt.Sprintf("Failed to clean up runner instances directory: %v", err))
		} else {
			logger.Info(fmt.Sprintf("Runner instances directory cleaned up: %s", instancesDir))
		}
	}

	// Remove state file.
	if err := migrator.RemoveState(); err != nil {
		logger.Warn(fmt.Sprintf("Failed to remove state file: %v", err))
	}

	logger.Info("")
	if scaleSetDeleted {
		logger.Info("Runner teardown complete")
	} else {
		logger.Warn("Runner teardown completed with warnings (scale set may not have been fully cleaned up)")
	}

	return nil
}
