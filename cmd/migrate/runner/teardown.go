package runner

import (
	"context"
	"fmt"

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

This command stops the runner process, deletes the runner scale set from the
source repository/organization, and cleans up local runner files.

Arguments:
  org   Organization name for organization-scoped runner (optional).
        When omitted, uses the current repository's owner.`,
		RunE: runTeardown,
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()

	// Common flags
	f.StringVarP(&teardownRepo, "repo", "R", "", "Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository")

	// Runner-specific flags
	f.StringVar(&teardownRunnerOpts.RunnerLabel, "runner-label", types.DefaultRunnerLabel, "Label of the runner to tear down")

	return cmd
}

func runTeardown(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	logger.Info("Tearing down runner for migration")

	sourceRepo, err := resolveSourceRepo(teardownRepo, args, teardownRunnerOpts.RunnerLabel)
	if err != nil {
		return err
	}

	// Try to load state from the state file
	state, stateErr := migrator.LoadState()

	// Build GitHub config URL for scaleset
	configURL := migrator.BuildGitHubConfigURL(sourceRepo)

	// Create scaleset client
	scalesetClient, err := migrator.NewScaleSetClient(configURL)
	if err != nil {
		return fmt.Errorf("failed to create scaleset client: %w", err)
	}

	// Stop runner process if state has a PID (legacy or interrupted listener)
	if stateErr == nil && state.RunnerPID > 0 {
		logger.Info(fmt.Sprintf("Stopping runner process (PID: %d)...", state.RunnerPID))
		if err := migrator.StopRunner(state.RunnerPID); err != nil {
			logger.Warn(fmt.Sprintf("Failed to stop runner process: %v", err))
		} else {
			logger.Info("Runner process stopped")
		}
	}

	// Delete scale set
	var scaleSetDeleted bool
	if stateErr == nil && state.ScaleSetID > 0 {
		// Use scale set ID from state
		logger.Info(fmt.Sprintf("Deleting runner scale set (ID: %d)...", state.ScaleSetID))
		if err := migrator.DeleteRunnerScaleSet(ctx, scalesetClient, state.ScaleSetID); err != nil {
			logger.Warn(fmt.Sprintf("Failed to delete scale set by ID: %v", err))
		} else {
			scaleSetDeleted = true
			logger.Info("Runner scale set deleted")
		}
	}

	if !scaleSetDeleted {
		// Try to find and delete by name
		logger.Info(fmt.Sprintf("Looking up runner scale set by name: %s", teardownRunnerOpts.RunnerLabel))
		runnerGroupID := migrator.DefaultRunnerGroupID
		if stateErr == nil && state.RunnerGroupID > 0 {
			runnerGroupID = state.RunnerGroupID
		}
		scaleSet, err := migrator.FindRunnerScaleSet(ctx, scalesetClient, teardownRunnerOpts.RunnerLabel, runnerGroupID)
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

	// Delete runner group if it was created during setup
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

	// Clean up local runner files
	runnerDir := ""
	if stateErr == nil && state.RunnerDir != "" {
		runnerDir = state.RunnerDir
	} else {
		runnerDir, _ = migrator.RunnerDirPath()
	}

	if runnerDir != "" {
		// Deregister any leftover runner instances before removing files
		instancesDir := migrator.RunnerInstancesBaseDir(runnerDir)
		logger.Info(fmt.Sprintf("Removing registered runners in: %s", instancesDir))
		migrator.RemoveRunnerInstances(instancesDir)

		// Also deregister the template runner dir itself if it was configured
		if err := migrator.RemoveRunner(runnerDir); err != nil {
			logger.Warn(fmt.Sprintf("Failed to remove runner from template dir: %v", err))
		}

		logger.Info(fmt.Sprintf("Cleaning up runner directory: %s", runnerDir))
		if err := migrator.CleanupRunnerDir(runnerDir); err != nil {
			logger.Warn(fmt.Sprintf("Failed to clean up runner directory: %v", err))
		} else {
			logger.Info("Runner directory cleaned up")
		}

		// Clean up runner instance directories (sibling of runnerDir)
		if err := migrator.CleanupRunnerDir(instancesDir); err != nil {
			logger.Warn(fmt.Sprintf("Failed to clean up runner instances directory: %v", err))
		} else {
			logger.Info(fmt.Sprintf("Runner instances directory cleaned up: %s", instancesDir))
		}
	}

	// Remove state file
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
