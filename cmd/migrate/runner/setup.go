package runner

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

var (
	setupCommonOpts types.CommonOptions
	setupRunnerOpts types.RunnerOptions
)

// NewSetupCmd creates the runner setup command
func NewSetupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Register and start a self-hosted runner",
		Long: `Register and start a self-hosted runner for secret migration.

This command downloads and configures actions/scaleset runner, registers it
to the source repository/organization with the specified label, starts the
runner process, and verifies it is online and ready to accept jobs.`,
		RunE: runSetup,
		Args: cobra.NoArgs,
	}

	// Common flags
	cmd.Flags().StringVarP(&setupCommonOpts.Source, "source", "s", "", "Source repository or organization (e.g., owner/repo or org)")
	cmd.MarkFlagRequired("source")

	// Runner-specific flags
	cmd.Flags().StringVar(&setupRunnerOpts.RunnerLabel, "runner-label", "gh-secret-kit-migrate", "Custom label for the runner")
	cmd.Flags().BoolVar(&setupRunnerOpts.ExistingRunner, "existing-runner", false, "Use an existing self-hosted runner instead of setting up a new one")

	return cmd
}

func runSetup(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	logger.Info("Setting up runner for migration")
	logger.Debug(fmt.Sprintf("Source: %s, Runner Label: %s, Existing: %v", setupCommonOpts.Source, setupRunnerOpts.RunnerLabel, setupRunnerOpts.ExistingRunner))

	if setupRunnerOpts.ExistingRunner {
		return verifyExistingRunner(ctx)
	}

	return setupNewRunner(ctx)
}

func verifyExistingRunner(ctx context.Context) error {
	logger.Info("Verifying existing runner...")

	// Parse source repository
	sourceRepo, err := parser.Repository(parser.RepositoryInput(setupCommonOpts.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	// Initialize GitHub client
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Get list of runners
	runners, err := gh.ListRunners(ctx, client, sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to list runners: %w", err)
	}

	// Find runner with matching label
	var foundRunner bool
	for _, runner := range runners {
		if runner == nil || runner.Labels == nil {
			continue
		}
		for _, label := range runner.Labels {
			if label != nil && label.GetName() == setupRunnerOpts.RunnerLabel {
				foundRunner = true
				if runner.GetStatus() == "online" {
					logger.Info(fmt.Sprintf("Found online runner with label '%s': %s (ID: %d)", setupRunnerOpts.RunnerLabel, runner.GetName(), runner.GetID()))
					return nil
				}
				logger.Warn(fmt.Sprintf("Found runner with label '%s' but it is not online: %s (Status: %s)", setupRunnerOpts.RunnerLabel, runner.GetName(), runner.GetStatus()))
			}
		}
	}

	if !foundRunner {
		return fmt.Errorf("no runner found with label '%s'", setupRunnerOpts.RunnerLabel)
	}
	return fmt.Errorf("no online runner found with label '%s'", setupRunnerOpts.RunnerLabel)
}

func setupNewRunner(ctx context.Context) error {
	logger.Info("Setting up new runner...")
	logger.Warn("Automatic runner setup is not yet implemented")
	logger.Info("Please manually set up a self-hosted runner with the following label:")
	logger.Info(fmt.Sprintf("  Label: %s", setupRunnerOpts.RunnerLabel))
	logger.Info("")
	logger.Info("Steps:")
	logger.Info("1. Go to your repository/organization settings")
	logger.Info("2. Navigate to Actions > Runners")
	logger.Info("3. Click 'New self-hosted runner'")
	logger.Info("4. Follow the instructions to download and configure the runner")
	logger.Info(fmt.Sprintf("5. Add the label '%s' when configuring", setupRunnerOpts.RunnerLabel))
	logger.Info("6. Start the runner")
	logger.Info("")
	logger.Info("Alternatively, use --existing-runner flag if you already have a runner set up")
	return fmt.Errorf("automatic runner setup is not yet implemented. Please set up a runner manually")
}
