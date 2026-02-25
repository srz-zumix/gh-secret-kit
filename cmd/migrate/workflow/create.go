package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v79/github"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	migratePackage "github.com/srz-zumix/gh-secret-kit/pkg/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

var (
	createCommonOpts   types.CommonOptions
	createWorkflowOpts types.WorkflowOptions
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
	ctx := context.Background()
	logger.Info("Creating migration workflow")
	logger.Debug(fmt.Sprintf("Source: %s, Destination: %s", createCommonOpts.Source, createCommonOpts.Destination))
	logger.Debug(fmt.Sprintf("Workflow Name: %s, Branch: %s, Runner Label: %s", createWorkflowOpts.WorkflowName, createWorkflowOpts.Branch, createWorkflowOpts.RunnerLabel))

	// Parse source repository
	sourceRepo, err := parser.Repository(parser.RepositoryInput(createCommonOpts.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	// Initialize GitHub client
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Get secrets to migrate
	secrets := createCommonOpts.Secrets
	if len(secrets) == 0 {
		logger.Info("No specific secrets specified, fetching all secrets from source...")
		secrets, err = fetchAllSecrets(ctx, client, sourceRepo)
		if err != nil {
			return fmt.Errorf("failed to fetch secrets from source: %w", err)
		}
		logger.Info(fmt.Sprintf("Found %d secrets to migrate", len(secrets)))
	}

	// Parse rename mappings
	renameMap := make(map[string]string)
	for _, mapping := range createCommonOpts.Rename {
		parts := strings.SplitN(mapping, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid rename mapping format: %s (expected OLD_NAME=NEW_NAME)", mapping)
		}
		renameMap[parts[0]] = parts[1]
	}

	// Build workflow configuration
	workflowConfig := migratePackage.WorkflowConfig{
		WorkflowName:     createWorkflowOpts.WorkflowName,
		RunnerLabel:      createWorkflowOpts.RunnerLabel,
		Source:           createCommonOpts.Source,
		Destination:      createCommonOpts.Destination,
		SourceEnv:        createCommonOpts.SourceEnv,
		DestinationEnv:   createCommonOpts.DestinationEnv,
		Secrets:          secrets,
		Rename:           renameMap,
		Overwrite:        createCommonOpts.Overwrite,
		DestinationToken: createCommonOpts.DestinationToken,
	}

	// Generate workflow YAML
	logger.Info("Generating workflow YAML...")
	workflowYAML, err := migratePackage.GenerateWorkflowYAML(workflowConfig)
	if err != nil {
		return fmt.Errorf("failed to generate workflow YAML: %w", err)
	}

	// Determine branch
	branch := createWorkflowOpts.Branch
	if branch == "" {
		logger.Debug("No branch specified, using default branch")
		repo, err := gh.GetRepository(ctx, client, sourceRepo)
		if err != nil {
			return fmt.Errorf("failed to get repository to determine default branch: %w", err)
		}
		branch = repo.GetDefaultBranch()
		logger.Debug(fmt.Sprintf("Using default branch: %s", branch))
	}

	// Push workflow file to repository
	workflowPath := fmt.Sprintf(".github/workflows/%s.yml", createWorkflowOpts.WorkflowName)
	logger.Info(fmt.Sprintf("Pushing workflow file to %s/%s at %s...", sourceRepo.Owner, sourceRepo.Name, workflowPath))

	// Check if file already exists
	existingContent, _, err := client.GetRepositoryContent(ctx, sourceRepo.Owner, sourceRepo.Name, workflowPath, &branch)

	commitMessage := fmt.Sprintf("Add workflow for secret migration: %s", createWorkflowOpts.WorkflowName)
	fileOptions := &github.RepositoryContentFileOptions{
		Message: &commitMessage,
		Content: []byte(workflowYAML),
		Branch:  &branch,
	}

	if err == nil && existingContent != nil {
		// File exists, update it
		logger.Info("Workflow file already exists, updating...")
		fileOptions.SHA = existingContent.SHA
		_, err = gh.UpdateFile(ctx, client, sourceRepo, workflowPath, fileOptions)
		if err != nil {
			return fmt.Errorf("failed to update workflow file: %w", err)
		}
		logger.Info("Workflow file updated successfully")
	} else {
		// File doesn't exist, create it
		logger.Info("Creating new workflow file...")
		_, err = gh.CreateFile(ctx, client, sourceRepo, workflowPath, fileOptions)
		if err != nil {
			return fmt.Errorf("failed to create workflow file: %w", err)
		}
		logger.Info("Workflow file created successfully")
	}

	logger.Info(fmt.Sprintf("Migration workflow created at: %s/%s/.github/workflows/%s.yml", sourceRepo.Owner, sourceRepo.Name, createWorkflowOpts.WorkflowName))
	return nil
}

func fetchAllSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository) ([]string, error) {
	secrets, err := gh.ListSecrets(ctx, client, repo)
	if err != nil {
		return nil, err
	}

	secretNames := make([]string, len(secrets))
	for i, secret := range secrets {
		secretNames[i] = secret.Name
	}

	return secretNames, nil
}
