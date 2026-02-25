package workflow

import (
	"context"
	"fmt"

	"github.com/google/go-github/v79/github"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

var (
	deleteCommonOpts   types.CommonOptions
	deleteWorkflowOpts types.WorkflowOptions
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
	ctx := context.Background()
	logger.Info("Deleting migration workflow")
	logger.Debug(fmt.Sprintf("Source: %s", deleteCommonOpts.Source))
	logger.Debug(fmt.Sprintf("Workflow Name: %s, Branch: %s", deleteWorkflowOpts.WorkflowName, deleteWorkflowOpts.Branch))

	// Parse source repository
	sourceRepo, err := parser.Repository(parser.RepositoryInput(deleteCommonOpts.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	// Initialize GitHub client
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Get workflow file path
	workflowFilePath := fmt.Sprintf(".github/workflows/%s.yml", deleteWorkflowOpts.WorkflowName)

	// Get current file to retrieve SHA
	logger.Info(fmt.Sprintf("Deleting workflow file: %s", workflowFilePath))

	// Get ref parameter for branch
	var ref *string
	if deleteWorkflowOpts.Branch != "" {
		ref = &deleteWorkflowOpts.Branch
	}

	file, err := gh.GetRepositoryFileContent(ctx, client, sourceRepo, workflowFilePath, ref)
	if err != nil {
		return fmt.Errorf("failed to get workflow file: %w", err)
	}

	// Delete the workflow file
	message := fmt.Sprintf("Delete migration workflow %s", deleteWorkflowOpts.WorkflowName)
	opts := &github.RepositoryContentFileOptions{
		Message: &message,
		SHA:     file.SHA,
		Branch:  ref,
	}
	err = gh.DeleteFile(ctx, client, sourceRepo, workflowFilePath, opts)
	if err != nil {
		return fmt.Errorf("failed to delete workflow file: %w", err)
	}

	logger.Info("Workflow deleted successfully")
	return nil
}
