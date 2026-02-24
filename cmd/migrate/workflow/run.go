package workflow

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	runCommonOpts   migrate.CommonOptions
	runWorkflowOpts migrate.WorkflowOptions
)

// NewRunCmd creates the workflow run command
func NewRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Dispatch the migration workflow",
		Long: `Dispatch the migration workflow via workflow_dispatch event.

This command dispatches the workflow and optionally waits for it to complete,
reporting success/failure for each secret migration.`,
		RunE: runWorkflow,
		Args: cobra.NoArgs,
	}

	// Common flags
	cmd.Flags().StringVarP(&runCommonOpts.Source, "source", "s", "", "Source repository or organization (e.g., owner/repo or org)")
	cmd.Flags().StringVarP(&runCommonOpts.Destination, "destination", "d", "", "Destination repository or organization (e.g., owner2/repo2 or org2)")
	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("destination")

	// Workflow-specific flags
	cmd.Flags().StringVar(&runWorkflowOpts.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the workflow to dispatch")
	cmd.Flags().BoolVar(&runWorkflowOpts.Wait, "wait", true, "Wait for the workflow run to complete")
	cmd.Flags().StringVar(&runWorkflowOpts.Timeout, "timeout", "10m", "Timeout for waiting for the workflow run")

	return cmd
}

func runWorkflow(cmd *cobra.Command, args []string) error {
	_ = context.Background() // Reserved for future implementation
	logger.Info("Running migration workflow")
	logger.Debug(fmt.Sprintf("Source: %s, Destination: %s", runCommonOpts.Source, runCommonOpts.Destination))
	logger.Debug(fmt.Sprintf("Workflow Name: %s, Wait: %v, Timeout: %s", runWorkflowOpts.WorkflowName, runWorkflowOpts.Wait, runWorkflowOpts.Timeout))

	// TODO: Implement workflow dispatch
	// 1. Dispatch workflow via workflow_dispatch
	// 2. If wait is true, poll until workflow run completes
	// 3. Report success/failure for each secret migration
	return fmt.Errorf("running migration workflow is not yet implemented")
}
