package workflow

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	createCommonOpts   migrate.CommonOptions
	createWorkflowOpts migrate.WorkflowOptions
)

// NewCreateCmd creates the workflow create command
func NewCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create",
		Short: "Generate and push the migration workflow YAML",
		Long: `Generate and push the migration workflow YAML to the source repository.

This command generates a GitHub Actions workflow that runs on the self-hosted
runner, reads secret values using the secrets context, and sets them directly
to the destination via GitHub API.`,
		RunE: runCreate,
		Args: cobra.NoArgs,
	}

	// Common flags
	cmd.Flags().StringVarP(&createCommonOpts.Source, "source", "s", "", "Source repository or organization (e.g., owner/repo or org)")
	cmd.Flags().StringVarP(&createCommonOpts.Destination, "destination", "d", "", "Destination repository or organization (e.g., owner2/repo2 or org2)")
	cmd.Flags().StringVar(&createCommonOpts.SourceEnv, "source-env", "", "Source environment name (for environment secrets)")
	cmd.Flags().StringVar(&createCommonOpts.DestinationEnv, "destination-env", "", "Destination environment name (for environment secrets)")
	cmd.Flags().StringSliceVar(&createCommonOpts.Secrets, "secrets", []string{}, "Specific secret names to migrate (comma-separated or repeated flag)")
	cmd.Flags().StringSliceVar(&createCommonOpts.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	cmd.Flags().BoolVar(&createCommonOpts.Overwrite, "overwrite", false, "Overwrite existing secrets at the destination (default is skip)")
	cmd.Flags().StringVar(&createCommonOpts.DestinationToken, "destination-token", "", "PAT or token for the destination (required if destination is a different owner/org)")

	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("destination")

	// Workflow-specific flags
	cmd.Flags().StringVar(&createWorkflowOpts.RunnerLabel, "runner-label", "gh-secret-kit-migrate", "Runner label to use in the workflow runs-on")
	cmd.Flags().StringVar(&createWorkflowOpts.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the generated workflow file")
	cmd.Flags().StringVar(&createWorkflowOpts.Branch, "branch", "", "Branch to push the workflow YAML to (default: default branch)")

	return cmd
}

func runCreate(cmd *cobra.Command, args []string) error {
	_ = context.Background() // Reserved for future implementation
	logger.Info("Creating migration workflow")
	logger.Debug(fmt.Sprintf("Source: %s, Destination: %s", createCommonOpts.Source, createCommonOpts.Destination))
	logger.Debug(fmt.Sprintf("Workflow Name: %s, Branch: %s, Runner Label: %s", createWorkflowOpts.WorkflowName, createWorkflowOpts.Branch, createWorkflowOpts.RunnerLabel))

	// TODO: Implement workflow creation
	// 1. Generate workflow YAML
	// 2. Push workflow file to source repository
	return fmt.Errorf("creating migration workflow is not yet implemented")
}
