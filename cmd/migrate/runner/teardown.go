package runner

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	teardownCommonOpts migrate.CommonOptions
	teardownRunnerOpts migrate.RunnerOptions
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
	_ = context.Background() // Reserved for future implementation
	logger.Info("Tearing down runner for migration")
	logger.Debug(fmt.Sprintf("Source: %s, Runner Label: %s", teardownCommonOpts.Source, teardownRunnerOpts.RunnerLabel))

	// TODO: Implement runner teardown
	// 1. Stop runner process
	// 2. Unregister runner from source repo/org
	// 3. Clean up local runner files
	return fmt.Errorf("tearing down runner is not yet implemented")
}
