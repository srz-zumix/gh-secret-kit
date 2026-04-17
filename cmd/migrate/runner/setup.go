package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/actions/scaleset"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	setupRepo       string
	setupRunnerOpts types.RunnerOptions
)

// NewSetupCmd creates the runner setup command
func NewSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup [[HOST]/ORG]",
		Short: "Register and start a self-hosted runner",
		Long: `Register and start a self-hosted runner for secret migration.

This command creates a runner scale set using actions/scaleset, downloads
the GitHub Actions runner binary, and starts a message session listener
that waits for job assignments. When a workflow job is dispatched to the
scale set, the listener automatically generates a JIT configuration and
starts an ephemeral runner to execute the job.

The command runs in the foreground and blocks until the migration job
completes or the process is interrupted (Ctrl+C). Run the workflow
dispatch command from another terminal while this command is running.

Arguments:
  org   Organization name for organization-scoped runner (optional).
        When omitted, uses the current repository's owner.`,
		RunE: runSetup,
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()

	// Common flags
	f.StringVarP(&setupRepo, "repo", "R", "", "Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository")

	// Runner-specific flags
	f.StringVar(&setupRunnerOpts.RunnerLabel, "runner-label", types.DefaultRunnerLabel, "Custom label for the runner")
	f.StringVar(&setupRunnerOpts.RunnerGroup, "runner-group", "", "Runner group name (created if not found; defaults to the default runner group)")
	f.IntVar(&setupRunnerOpts.MaxRunners, "max-runners", 2, "Maximum number of concurrent runners")

	return cmd
}

func runSetup(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	logger.Info("Setting up runner for migration")

	sourceRepo, err := resolveSourceRepo(setupRepo, args, setupRunnerOpts.RunnerLabel)
	if err != nil {
		return err
	}

	return setupNewRunner(ctx, sourceRepo)
}

func setupNewRunner(ctx context.Context, sourceRepo repository.Repository) error {
	// Check if migration state already exists
	if migrator.StateExists() {
		return fmt.Errorf("migration state already exists; run 'runner teardown' first or remove the state file")
	}

	// Initialize GitHub client (for registration token)
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Build GitHub config URL for scaleset
	configURL := migrator.BuildGitHubConfigURL(sourceRepo)
	logger.Info(fmt.Sprintf("Creating scale set client for: %s", configURL))

	// Create scaleset client
	scalesetClient, err := migrator.NewScaleSetClient(configURL)
	if err != nil {
		return fmt.Errorf("failed to create scaleset client: %w", err)
	}

	// Resolve runner group ID
	runnerGroupID := migrator.DefaultRunnerGroupID
	runnerGroupCreated := false
	if setupRunnerOpts.RunnerGroup != "" {
		resolvedID, created, err := resolveRunnerGroup(ctx, client, scalesetClient, sourceRepo, setupRunnerOpts.RunnerGroup)
		if err != nil {
			return err
		}
		runnerGroupID = resolvedID
		runnerGroupCreated = created
	}

	// Create runner scale set
	logger.Info(fmt.Sprintf("Creating runner scale set: %s (runner group ID=%d)", setupRunnerOpts.RunnerLabel, runnerGroupID))
	scaleSet, err := migrator.CreateRunnerScaleSet(ctx, scalesetClient, setupRunnerOpts.RunnerLabel, runnerGroupID)
	if err != nil {
		return fmt.Errorf("failed to create runner scale set: %w", err)
	}
	var labelNames []string
	for _, l := range scaleSet.Labels {
		labelNames = append(labelNames, fmt.Sprintf("%s(type=%s)", l.Name, l.Type))
	}
	logger.Info(fmt.Sprintf("Created runner scale set: ID=%d, Name=%s, RunnerGroupID=%d, RunnerGroupName=%s, Labels=%v",
		scaleSet.ID, scaleSet.Name, scaleSet.RunnerGroupID, scaleSet.RunnerGroupName, labelNames))

	// Verify runner group accessibility
	runnerGroupName := "default"
	if setupRunnerOpts.RunnerGroup != "" {
		runnerGroupName = setupRunnerOpts.RunnerGroup
	}
	runnerGroup, err := migrator.GetRunnerGroupByName(ctx, scalesetClient, runnerGroupName)
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to verify runner group '%s': %v", runnerGroupName, err))
	} else {
		logger.Info(fmt.Sprintf("Runner group verified: ID=%d, Name=%s, IsDefault=%v", runnerGroup.ID, runnerGroup.Name, runnerGroup.IsDefault))
	}

	// Verify scale set by reading back
	verifiedScaleSet, err := migrator.GetRunnerScaleSetByID(ctx, scalesetClient, scaleSet.ID)
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to verify scale set by ID: %v", err))
	} else {
		var verifiedLabels []string
		for _, l := range verifiedScaleSet.Labels {
			verifiedLabels = append(verifiedLabels, fmt.Sprintf("%s(type=%s)", l.Name, l.Type))
		}
		logger.Info(fmt.Sprintf("Verified scale set: ID=%d, Name=%s, RunnerGroupID=%d, Labels=%v",
			verifiedScaleSet.ID, verifiedScaleSet.Name, verifiedScaleSet.RunnerGroupID, verifiedLabels))
	}

	// Update system info with scale set ID
	migrator.SetScaleSetSystemInfo(scalesetClient, scaleSet.ID)

	// Determine runner directory
	runnerDir, err := migrator.RunnerDirPath()
	if err != nil {
		cleanupScaleSet(ctx, scalesetClient, scaleSet.ID)
		return fmt.Errorf("failed to determine runner directory: %w", err)
	}

	// Download runner binary
	logger.Info("Detecting runner binary for current platform...")
	binaryInfo, err := migrator.DetectRunnerBinary("")
	if err != nil {
		cleanupScaleSet(ctx, scalesetClient, scaleSet.ID)
		return fmt.Errorf("failed to detect runner binary: %w", err)
	}

	logger.Info(fmt.Sprintf("Downloading runner binary: %s", binaryInfo.Filename))
	if err := migrator.DownloadRunnerBinary(ctx, binaryInfo.URL, runnerDir); err != nil {
		cleanupScaleSet(ctx, scalesetClient, scaleSet.ID)
		return fmt.Errorf("failed to download runner binary: %w", err)
	}

	// Save state for teardown (before starting listener, so teardown works even if interrupted)
	sourceString := sourceRepo.Owner
	if sourceRepo.Name != "" {
		sourceString = sourceRepo.Owner + "/" + sourceRepo.Name
	}
	state := &migrator.MigrateState{
		Source:             sourceString,
		ScaleSetID:         scaleSet.ID,
		ScaleSetName:       scaleSet.Name,
		RunnerGroupID:      runnerGroupID,
		RunnerGroupCreated: runnerGroupCreated,
		RunnerDir:          runnerDir,
		ConfigURL:          configURL,
		CreatedAt:          time.Now(),
	}
	if err := migrator.SaveState(state); err != nil {
		logger.Warn(fmt.Sprintf("Failed to save migration state: %v", err))
	}

	logger.Info("Runner setup complete!")
	logger.Info(fmt.Sprintf("  Scale Set ID: %d", scaleSet.ID))
	logger.Info(fmt.Sprintf("  Runner Label: %s", setupRunnerOpts.RunnerLabel))
	logger.Info("")

	// Build a token refresher for config.sh-based GHES registration.
	// Registration tokens are one-time-use on GHES, so we obtain a fresh one
	// before each ConfigureRunner call instead of reusing a single token.
	var tokenRefresher func(ctx context.Context) (string, error)
	logger.Info("Verifying registration token availability...")
	_, err = gh.CreateRegistrationToken(ctx, client, sourceRepo)
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to obtain registration token (will use JIT config): %v", err))
	} else {
		logger.Info("Registration token available; using config.sh mode for runners")
		tokenRefresher = func(ctx context.Context) (string, error) {
			token, err := gh.CreateRegistrationToken(ctx, client, sourceRepo)
			if err != nil {
				return "", fmt.Errorf("failed to create registration token: %w", err)
			}
			return token.GetToken(), nil
		}
	}

	logger.Info("Starting message session listener (foreground)...")
	logger.Info("Dispatch the workflow from another terminal, then the listener will")
	logger.Info("automatically start an ephemeral runner when a job is assigned.")
	logger.Info("The listener will keep running after job completion, ready for subsequent runs.")
	logger.Info("Press Ctrl+C to stop the listener.")
	logger.Info("")

	// Run the message session listener loop (blocks until job completes or interrupted)
	listenerConfig := &migrator.ListenerConfig{
		Client:         scalesetClient,
		ScaleSetID:     scaleSet.ID,
		RunnerDir:      runnerDir,
		ConfigURL:      configURL,
		RunnerLabel:    setupRunnerOpts.RunnerLabel,
		TokenRefresher: tokenRefresher,
		MaxRunners:     setupRunnerOpts.MaxRunners,
	}
	listenerErr := migrator.RunListenerLoop(ctx, listenerConfig)

	// After listener exits, show teardown instructions
	logger.Info("")
	if listenerErr == nil {
		logger.Info("Listener stopped.")
	} else if ctx.Err() != nil {
		logger.Info("Listener was interrupted.")
	} else {
		logger.Warn(fmt.Sprintf("Listener exited with error: %v", listenerErr))
	}
	logger.Info("To clean up resources, run:")
	teardownArgs := sourceRepo.Owner
	if sourceRepo.Name != "" {
		teardownArgs = "--repo " + sourceRepo.Owner + "/" + sourceRepo.Name
	}
	logger.Info(fmt.Sprintf("  gh secret-kit migrate runner teardown %s --runner-label %s",
		teardownArgs, setupRunnerOpts.RunnerLabel))

	return listenerErr
}

// cleanupScaleSet deletes the scale set on failure, logging any errors
func cleanupScaleSet(ctx context.Context, client interface {
	DeleteRunnerScaleSet(context.Context, int) error
}, scaleSetID int) {
	logger.Warn(fmt.Sprintf("Cleaning up scale set (ID=%d) due to setup failure...", scaleSetID))
	if err := client.DeleteRunnerScaleSet(ctx, scaleSetID); err != nil {
		logger.Error(fmt.Sprintf("Failed to clean up scale set: %v", err))
	}
}

// resolveRunnerGroup resolves a runner group by name, creating it if it does not exist.
// Returns the runner group ID and whether the group was newly created.
func resolveRunnerGroup(ctx context.Context, client *gh.GitHubClient, scalesetClient *scaleset.Client, sourceRepo repository.Repository, groupName string) (int, bool, error) {
	// First, try to find the runner group via the scaleset client.
	group, err := migrator.GetRunnerGroupByName(ctx, scalesetClient, groupName)
	if err != nil {
		return 0, false, fmt.Errorf("failed to get runner group '%s': %w", groupName, err)
	}
	if group != nil {
		logger.Info(fmt.Sprintf("Found runner group: ID=%d, Name=%s", group.ID, group.Name))
		return group.ID, false, nil
	}

	// Runner group was not found; create it via the GitHub API.
	logger.Info(fmt.Sprintf("Runner group '%s' not found, creating...", groupName))
	created, err := gh.CreateOrgRunnerGroup(ctx, client, sourceRepo, groupName)
	if err != nil {
		return 0, false, fmt.Errorf("failed to create runner group '%s': %w", groupName, err)
	}
	logger.Info(fmt.Sprintf("Created runner group: ID=%d, Name=%s", created.GetID(), created.GetName()))
	return int(created.GetID()), true, nil
}
