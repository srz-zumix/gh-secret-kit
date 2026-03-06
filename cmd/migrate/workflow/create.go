package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	migratePackage "github.com/srz-zumix/gh-secret-kit/pkg/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// RunCreate generates and pushes the migration workflow YAML to the source repository
func RunCreate(ctx context.Context, config *CreateConfig) error {
	logger.Info("Creating migration workflow")
	logger.Debug(fmt.Sprintf("Source: %s, Destination: %s, Scope: %s", config.Source, config.Destination, config.Scope))
	logger.Debug(fmt.Sprintf("Workflow Name: %s, Branch: %s, Runner Label: %s", config.WorkflowName, config.Branch, config.RunnerLabel))

	// Parse source repository
	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	// Initialize GitHub client
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	scope := config.Scope
	logger.Info(fmt.Sprintf("Secret scope: %s (destination: %s)", scope, config.Destination))

	// Get secrets to migrate
	secrets := config.Secrets
	if len(secrets) == 0 {
		switch scope {
		case migratePackage.SecretScopeOrg:
			logger.Info("No specific secrets specified, fetching org secrets from source...")
			secrets, err = fetchOrgSecrets(ctx, client, sourceRepo)
		case migratePackage.SecretScopeEnv:
			logger.Info("No specific secrets specified, fetching env secrets from source...")
			secrets, err = fetchEnvSecrets(ctx, client, sourceRepo, config.SourceEnv)
		default:
			logger.Info("No specific secrets specified, fetching repo secrets from source...")
			secrets, err = fetchRepoSecrets(ctx, client, sourceRepo)
		}
		if err != nil {
			return fmt.Errorf("failed to fetch secrets from source: %w", err)
		}
		logger.Info(fmt.Sprintf("Found %d secrets to migrate", len(secrets)))
	}

	// Parse rename mappings
	renameMap := make(map[string]string)
	for _, mapping := range config.Rename {
		parts := strings.SplitN(mapping, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid rename mapping format: %s (expected OLD_NAME=NEW_NAME)", mapping)
		}
		renameMap[parts[0]] = parts[1]
	}

	// Determine destination host
	destHost := config.DestinationHost
	if destHost == "" {
		destHost = sourceRepo.Host
	}
	if destHost == "" {
		destHost = "github.com"
	}

	// Build workflow configuration
	workflowConfig := migratePackage.WorkflowConfig{
		WorkflowName:           config.WorkflowName,
		RunnerLabel:            config.RunnerLabel,
		TriggerLabel:           config.Label,
		Source:                 config.Source,
		Destination:            config.Destination,
		DestinationHost:        destHost,
		SourceEnv:              config.SourceEnv,
		DestinationEnv:         config.DestinationEnv,
		Secrets:                secrets,
		Rename:                 renameMap,
		Overwrite:              config.Overwrite,
		DestinationTokenSecret: config.DestinationTokenSecret,
		Scope:                  scope,
	}

	// Generate workflow YAML
	logger.Info("Generating workflow YAML...")
	workflowYAML, err := migratePackage.GenerateWorkflowYAML(workflowConfig)
	if err != nil {
		return fmt.Errorf("failed to generate workflow YAML: %w", err)
	}

	// Determine topic branch
	branch := config.Branch

	// Get default branch to create topic branch from
	logger.Debug("Fetching default branch...")
	repoInfo, err := gh.GetRepository(ctx, client, sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to get repository to determine default branch: %w", err)
	}
	defaultBranch := repoInfo.GetDefaultBranch()
	logger.Debug(fmt.Sprintf("Default branch: %s, Topic branch: %s", defaultBranch, branch))

	// Create topic branch if it doesn't exist
	_, err = gh.GetBranch(ctx, client, sourceRepo, branch)
	if err != nil {
		logger.Info(fmt.Sprintf("Creating topic branch %s from %s...", branch, defaultBranch))
		defaultBranchInfo, berr := gh.GetBranch(ctx, client, sourceRepo, defaultBranch)
		if berr != nil {
			return fmt.Errorf("failed to get default branch %s: %w", defaultBranch, berr)
		}
		sha := defaultBranchInfo.GetCommit().GetSHA()
		_, berr = gh.CreateBranch(ctx, client, sourceRepo, branch, sha)
		if berr != nil {
			return fmt.Errorf("failed to create topic branch %s: %w", branch, berr)
		}
		logger.Info(fmt.Sprintf("Topic branch %s created", branch))
	} else {
		logger.Debug(fmt.Sprintf("Topic branch %s already exists", branch))
	}

	// Push workflow file to topic branch
	workflowPath := fmt.Sprintf(".github/workflows/%s.yml", config.WorkflowName)
	logger.Info(fmt.Sprintf("Pushing workflow file to %s/%s at %s (branch: %s)...", sourceRepo.Owner, sourceRepo.Name, workflowPath, branch))

	// Check if file already exists
	existingContent, _, gerr := client.GetRepositoryContent(ctx, sourceRepo.Owner, sourceRepo.Name, workflowPath, &branch)

	commitMessage := fmt.Sprintf("Add workflow for secret migration: %s [ci skip]", config.WorkflowName)
	fileOptions := &gh.RepositoryContentFileOptions{
		Message: commitMessage,
		Content: []byte(workflowYAML),
		Branch:  &branch,
	}

	if gerr == nil && existingContent != nil {
		logger.Info("Workflow file already exists, updating...")
		sha := existingContent.GetSHA()
		fileOptions.SHA = &sha
		_, err = gh.UpdateRepositoryFile(ctx, client, sourceRepo, workflowPath, fileOptions)
		if err != nil {
			return fmt.Errorf("failed to update workflow file: %w", err)
		}
		logger.Info("Workflow file updated successfully")
	} else {
		logger.Info("Creating new workflow file...")
		_, err = gh.CreateRepositoryFile(ctx, client, sourceRepo, workflowPath, fileOptions)
		if err != nil {
			return fmt.Errorf("failed to create workflow file: %w", err)
		}
		logger.Info("Workflow file created successfully")
	}

	logger.Info(fmt.Sprintf("Migration workflow created at: %s/%s/.github/workflows/%s.yml", sourceRepo.Owner, sourceRepo.Name, config.WorkflowName))
	return nil
}

func fetchRepoSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository) ([]string, error) {
	secrets, err := gh.ListRepoSecrets(ctx, client, repo)
	if err != nil {
		return nil, err
	}
	secretNames := make([]string, len(secrets))
	for i, secret := range secrets {
		secretNames[i] = secret.Name
	}
	return secretNames, nil
}

func fetchOrgSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository) ([]string, error) {
	secrets, err := gh.ListRepoOrgSecrets(ctx, client, repo)
	if err != nil {
		return nil, err
	}
	secretNames := make([]string, len(secrets))
	for i, secret := range secrets {
		secretNames[i] = secret.Name
	}
	return secretNames, nil
}

func fetchEnvSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, env string) ([]string, error) {
	repoInfo, err := gh.GetRepository(ctx, client, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}
	secrets, err := gh.ListEnvSecrets(ctx, client, int(repoInfo.GetID()), env)
	if err != nil {
		return nil, err
	}
	secretNames := make([]string, len(secrets))
	for i, secret := range secrets {
		secretNames[i] = secret.Name
	}
	return secretNames, nil
}
