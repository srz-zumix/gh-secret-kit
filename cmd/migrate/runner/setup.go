package runner

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	setupCommonOpts migrate.CommonOptions
	setupRunnerOpts migrate.RunnerOptions
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
	// TODO: Implement existing runner verification
	return fmt.Errorf("verifying existing runner is not yet implemented")
}

func setupNewRunner(ctx context.Context) error {
	logger.Info("Setting up new runner...")
	// TODO: Implement new runner setup
	// 1. Download actions/scaleset runner
	// 2. Register runner to source repo/org with specified label
	// 3. Start runner process
	// 4. Verify runner is online
	return fmt.Errorf("setting up new runner is not yet implemented")
}
