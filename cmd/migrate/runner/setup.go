package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

var (
	setupRepo       string
	setupRunnerOpts types.RunnerOptions
)

// NewSetupCmd creates the runner setup command
func NewSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup [owner]",
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
  owner   Organization name for organization-scoped runner (optional).
          When omitted, uses the current repository's owner.`,
		RunE: runSetup,
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()

	// Common flags
	f.StringVarP(&setupRepo, "repo", "R", "", "Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository")

	// Runner-specific flags
	f.StringVar(&setupRunnerOpts.RunnerLabel, "runner-label", "gh-secret-kit-migrate", "Custom label for the runner")

	return cmd
}

func runSetup(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	logger.Info("Setting up runner for migration")

	var sourceRepo repository.Repository
	var err error
	if setupRepo != "" {
		// -R/--repo specified: repository-scoped runner
		logger.Debug(fmt.Sprintf("Repo: %s, Runner Label: %s", setupRepo, setupRunnerOpts.RunnerLabel))
		sourceRepo, err = parser.Repository(parser.RepositoryInput(setupRepo))
	} else if len(args) > 0 {
		// First positional arg: organization-scoped runner
		logger.Debug(fmt.Sprintf("Org: %s, Runner Label: %s", args[0], setupRunnerOpts.RunnerLabel))
		sourceRepo, err = parser.Repository(parser.RepositoryOwnerWithHost(args[0]))
	} else {
		// Fall back to current repository's owner (org-scoped runner)
		var currentRepo repository.Repository
		currentRepo, err = parser.Repository(parser.RepositoryInput(""))
		if err == nil {
			sourceRepo = repository.Repository{Host: currentRepo.Host, Owner: currentRepo.Owner}
			logger.Debug(fmt.Sprintf("Org: %s, Runner Label: %s (current repo owner)", sourceRepo.Owner, setupRunnerOpts.RunnerLabel))
		}
	}
	if err != nil {
		return fmt.Errorf("failed to parse source: %w", err)
	}

	return setupNewRunner(ctx, sourceRepo)
}

func setupNewRunner(ctx context.Context, sourceRepo repository.Repository) error {
	// Check if migration state already exists
	if migrate.StateExists() {
		return fmt.Errorf("migration state already exists; run 'runner teardown' first or remove the state file")
	}

	// Initialize GitHub client (for registration token)
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Build GitHub config URL for scaleset
	configURL := migrate.BuildGitHubConfigURL(sourceRepo)
	logger.Info(fmt.Sprintf("Creating scale set client for: %s", configURL))

	// Create scaleset client
	scalesetClient, err := migrate.NewScaleSetClient(configURL)
	if err != nil {
		return fmt.Errorf("failed to create scaleset client: %w", err)
	}

	// Create runner scale set
	logger.Info(fmt.Sprintf("Creating runner scale set: %s", setupRunnerOpts.RunnerLabel))
	scaleSet, err := migrate.CreateRunnerScaleSet(ctx, scalesetClient, setupRunnerOpts.RunnerLabel)
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
	runnerGroup, err := migrate.GetRunnerGroupByName(ctx, scalesetClient, "default")
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to verify runner group 'default': %v", err))
	} else {
		logger.Info(fmt.Sprintf("Runner group verified: ID=%d, Name=%s, IsDefault=%v", runnerGroup.ID, runnerGroup.Name, runnerGroup.IsDefault))
	}

	// Verify scale set by reading back
	verifiedScaleSet, err := migrate.GetRunnerScaleSetByID(ctx, scalesetClient, scaleSet.ID)
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
	migrate.SetScaleSetSystemInfo(scalesetClient, scaleSet.ID)

	// Determine runner directory
	runnerDir, err := migrate.RunnerDirPath()
	if err != nil {
		cleanupScaleSet(ctx, scalesetClient, scaleSet.ID)
		return fmt.Errorf("failed to determine runner directory: %w", err)
	}

	// Download runner binary
	logger.Info("Detecting runner binary for current platform...")
	binaryInfo, err := migrate.DetectRunnerBinary("")
	if err != nil {
		cleanupScaleSet(ctx, scalesetClient, scaleSet.ID)
		return fmt.Errorf("failed to detect runner binary: %w", err)
	}

	logger.Info(fmt.Sprintf("Downloading runner binary: %s", binaryInfo.Filename))
	if err := migrate.DownloadRunnerBinary(ctx, binaryInfo.URL, runnerDir); err != nil {
		cleanupScaleSet(ctx, scalesetClient, scaleSet.ID)
		return fmt.Errorf("failed to download runner binary: %w", err)
	}

	// Save state for teardown (before starting listener, so teardown works even if interrupted)
	sourceString := sourceRepo.Owner
	if sourceRepo.Name != "" {
		sourceString = sourceRepo.Owner + "/" + sourceRepo.Name
	}
	state := &migrate.MigrateState{
		Source:       sourceString,
		ScaleSetID:   scaleSet.ID,
		ScaleSetName: scaleSet.Name,
		RunnerDir:    runnerDir,
		ConfigURL:    configURL,
		CreatedAt:    time.Now(),
	}
	if err := migrate.SaveState(state); err != nil {
		logger.Warn(fmt.Sprintf("Failed to save migration state: %v", err))
	}

	logger.Info("Runner setup complete!")
	logger.Info(fmt.Sprintf("  Scale Set ID: %d", scaleSet.ID))
	logger.Info(fmt.Sprintf("  Runner Label: %s", setupRunnerOpts.RunnerLabel))
	logger.Info("")

	// Get runner registration token for config.sh-based registration (GHES compatibility)
	logger.Info("Creating registration token for runner...")
	regToken, err := gh.CreateRegistrationToken(ctx, client, sourceRepo)
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to create registration token: %v", err))
		logger.Info("Falling back to JIT config mode")
	}

	registrationToken := ""
	if regToken != nil && regToken.Token != nil {
		registrationToken = regToken.GetToken()
		logger.Info("Registration token obtained successfully")
	}

	logger.Info("Starting message session listener (foreground)...")
	logger.Info("Dispatch the workflow from another terminal, then the listener will")
	logger.Info("automatically start an ephemeral runner when a job is assigned.")
	logger.Info("The listener will keep running after job completion, ready for subsequent runs.")
	logger.Info("Press Ctrl+C to stop the listener.")
	logger.Info("")

	// Run the message session listener loop (blocks until job completes or interrupted)
	listenerConfig := &migrate.ListenerConfig{
		Client:            scalesetClient,
		ScaleSetID:        scaleSet.ID,
		RunnerDir:         runnerDir,
		ConfigURL:         configURL,
		RunnerLabel:       setupRunnerOpts.RunnerLabel,
		RegistrationToken: registrationToken,
	}
	listenerErr := migrate.RunListenerLoop(ctx, listenerConfig)

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
