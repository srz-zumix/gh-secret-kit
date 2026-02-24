package workflow

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	deleteCommonOpts   migrate.CommonOptions
	deleteWorkflowOpts migrate.WorkflowOptions
)

// NewDeleteCmd creates the workflow delete command
func NewDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Remove the migration workflow YAML",
		Long: `Remove the migration workflow YAML from the source repository.

This command deletes the workflow file from the source repository and optionally
cleans up any workflow run artifacts.`,
		RunE: runDelete,
		Args: cobra.NoArgs,
	}

	// Common flags
	cmd.Flags().StringVarP(&deleteCommonOpts.Source, "source", "s", "", "Source repository or organization (e.g., owner/repo or org)")
	cmd.MarkFlagRequired("source")

	// Workflow-specific flags
	cmd.Flags().StringVar(&deleteWorkflowOpts.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the workflow file to delete")
	cmd.Flags().StringVar(&deleteWorkflowOpts.Branch, "branch", "", "Branch to delete the workflow YAML from (default: default branch)")

	return cmd
}

func runDelete(cmd *cobra.Command, args []string) error {
	_ = context.Background() // Reserved for future implementation
	logger.Info("Deleting migration workflow")
	logger.Debug(fmt.Sprintf("Source: %s", deleteCommonOpts.Source))
	logger.Debug(fmt.Sprintf("Workflow Name: %s, Branch: %s", deleteWorkflowOpts.WorkflowName, deleteWorkflowOpts.Branch))

	// TODO: Implement workflow deletion
	// 1. Delete workflow YAML file from source repository
	// 2. Optionally clean up workflow run artifacts
	return fmt.Errorf("deleting migration workflow is not yet implemented")
}
