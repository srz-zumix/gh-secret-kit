package runner

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	teardownCommonOpts types.CommonOptions
	teardownRunnerOpts types.RunnerOptions
)

// NewTeardownCmd creates the runner teardown command
func NewTeardownCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "teardown",
		Short: "Unregister and stop the self-hosted runner",
		Long: `Unregister and stop the self-hosted runner for secret migration.

This command stops the runner process, unregisters the runner from the source
repository/organization, and cleans up local runner files.`,
		RunE: runTeardown,
		Args: cobra.NoArgs,
	}

	// Common flags
	cmd.Flags().StringVarP(&teardownCommonOpts.Source, "source", "s", "", "Source repository or organization (e.g., owner/repo or org)")
	cmd.MarkFlagRequired("source")

	// Runner-specific flags
	cmd.Flags().StringVar(&teardownRunnerOpts.RunnerLabel, "runner-label", "gh-secret-kit-migrate", "Label of the runner to tear down")

	return cmd
}

func runTeardown(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	logger.Info("Tearing down runner for migration")
	logger.Debug(fmt.Sprintf("Source: %s, Runner Label: %s", teardownCommonOpts.Source, teardownRunnerOpts.RunnerLabel))

	logger.Warn("Automatic runner teardown is not yet implemented")
	logger.Info("Please manually tear down the self-hosted runner:")
	logger.Info("")
	logger.Info("Steps:")
	logger.Info("1. Stop the runner process (Ctrl+C or kill the process)")
	logger.Info("2. Go to your repository/organization settings")
	logger.Info("3. Navigate to Actions > Runners")
	logger.Info(fmt.Sprintf("4. Find the runner with label '%s'", teardownRunnerOpts.RunnerLabel))
	logger.Info("5. Click on the runner and select 'Remove'")
	logger.Info("")
	logger.Info("Note: If the runner was set up using this tool's future automatic")
	logger.Info("setup feature, this command will handle teardown automatically")

	_ = ctx // Suppress unused variable warning
	return nil
}
