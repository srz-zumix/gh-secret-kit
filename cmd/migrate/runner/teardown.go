package runner

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
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

	var sourceRepo repository.Repository
	var err error
	if teardownRepo != "" {
		logger.Debug(fmt.Sprintf("Repo: %s, Runner Label: %s", teardownRepo, teardownRunnerOpts.RunnerLabel))
		sourceRepo, err = parser.Repository(parser.RepositoryInput(teardownRepo))
	} else if len(args) > 0 {
		logger.Debug(fmt.Sprintf("Org: %s, Runner Label: %s", args[0], teardownRunnerOpts.RunnerLabel))
		sourceRepo, err = parser.Repository(parser.RepositoryOwnerWithHost(args[0]))
	} else {
		// Fall back to current repository's owner (org-scoped runner)
		var currentRepo repository.Repository
		currentRepo, err = parser.Repository(parser.RepositoryInput(""))
		if err == nil {
			sourceRepo = repository.Repository{Host: currentRepo.Host, Owner: currentRepo.Owner}
			logger.Debug(fmt.Sprintf("Org: %s, Runner Label: %s (current repo owner)", sourceRepo.Owner, teardownRunnerOpts.RunnerLabel))
		}
	}
	if err != nil {
		return fmt.Errorf("failed to parse source: %w", err)
	}

	// Try to load state from the state file
	state, stateErr := migrate.LoadState()

	// Build GitHub config URL for scaleset
	configURL := migrate.BuildGitHubConfigURL(sourceRepo)

	// Create scaleset client
	scalesetClient, err := migrate.NewScaleSetClient(configURL)
	if err != nil {
		return fmt.Errorf("failed to create scaleset client: %w", err)
	}

	// Stop runner process if state has a PID (legacy or interrupted listener)
	if stateErr == nil && state.RunnerPID > 0 {
		logger.Info(fmt.Sprintf("Stopping runner process (PID: %d)...", state.RunnerPID))
		if err := migrate.StopRunner(state.RunnerPID); err != nil {
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
		if err := migrate.DeleteRunnerScaleSet(ctx, scalesetClient, state.ScaleSetID); err != nil {
			logger.Warn(fmt.Sprintf("Failed to delete scale set by ID: %v", err))
		} else {
			scaleSetDeleted = true
			logger.Info("Runner scale set deleted")
		}
	}

	if !scaleSetDeleted {
		// Try to find and delete by name
		logger.Info(fmt.Sprintf("Looking up runner scale set by name: %s", teardownRunnerOpts.RunnerLabel))
		scaleSet, err := migrate.FindRunnerScaleSet(ctx, scalesetClient, teardownRunnerOpts.RunnerLabel)
		if err != nil {
			logger.Warn(fmt.Sprintf("Failed to find scale set by name: %v", err))
		} else if scaleSet != nil {
			logger.Info(fmt.Sprintf("Deleting runner scale set (ID: %d)...", scaleSet.ID))
			if err := migrate.DeleteRunnerScaleSet(ctx, scalesetClient, scaleSet.ID); err != nil {
				logger.Warn(fmt.Sprintf("Failed to delete scale set: %v", err))
			} else {
				scaleSetDeleted = true
				logger.Info("Runner scale set deleted")
			}
		} else {
			logger.Info("No runner scale set found to delete")
		}
	}

	// Clean up local runner files
	runnerDir := ""
	if stateErr == nil && state.RunnerDir != "" {
		runnerDir = state.RunnerDir
	} else {
		runnerDir, _ = migrate.RunnerDirPath()
	}

	if runnerDir != "" {
		logger.Info(fmt.Sprintf("Cleaning up runner directory: %s", runnerDir))
		if err := migrate.CleanupRunnerDir(runnerDir); err != nil {
			logger.Warn(fmt.Sprintf("Failed to clean up runner directory: %v", err))
		} else {
			logger.Info("Runner directory cleaned up")
		}
	}

	// Remove state file
	if err := migrate.RemoveState(); err != nil {
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
