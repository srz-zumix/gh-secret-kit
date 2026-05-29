package workflow

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v84/github"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/actions"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// RunDispatch rewrites the currently running workflow with a migration workflow,
// pushes it to a temporary branch, and re-triggers it via workflow_dispatch.
//
// It is intended to be invoked from inside a workflow_dispatch-triggered
// workflow and refuses to run otherwise.
func RunDispatch(ctx context.Context, config *DispatchConfig) error {
	logger.Info("Dispatching migration workflow")

	// Guard: this command must run inside a workflow_dispatch-triggered workflow.
	if !actions.IsRunsOn() {
		return fmt.Errorf("dispatch must run inside a GitHub Actions workflow")
	}
	if !actions.IsWorkflowDispatchEvent() {
		return fmt.Errorf("dispatch must be triggered by a workflow_dispatch event (current event: %q)", actions.GetEventName())
	}

	// Determine the running workflow file path from GITHUB_WORKFLOW_REF.
	workflowPath := actions.GetWorkflowFilePath()
	if workflowPath == "" {
		return fmt.Errorf("failed to determine the running workflow path from GITHUB_WORKFLOW_REF")
	}
	workflowFileName := path.Base(workflowPath)
	logger.Debug(fmt.Sprintf("Running workflow path: %s", workflowPath))

	// Resolve the source repository, defaulting to the repository running the workflow.
	source := config.Source
	if source == "" {
		source = currentActionsRepository()
	}
	sourceRepo, err := parser.Repository(parser.RepositoryInput(source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Determine the base commit to branch from.
	baseSHA := actions.GetSHA()
	if baseSHA == "" {
		repoInfo, gerr := gh.GetRepository(ctx, client, sourceRepo)
		if gerr != nil {
			return fmt.Errorf("failed to get repository: %w", gerr)
		}
		branchInfo, gerr := gh.GetBranch(ctx, client, sourceRepo, repoInfo.GetDefaultBranch())
		if gerr != nil {
			return fmt.Errorf("failed to get default branch %s: %w", repoInfo.GetDefaultBranch(), gerr)
		}
		baseSHA = branchInfo.GetCommit().GetSHA()
	}

	// Determine the runs-on value for the generated workflow.
	var runsOn any
	if config.RunnerLabel != "" {
		runsOn = config.RunnerLabel
	} else {
		runsOn, err = resolveRunsOn(ctx, client, sourceRepo, workflowPath, baseSHA)
		if err != nil {
			return err
		}
	}

	scope := config.Scope
	if scope == "" {
		scope = migrator.SecretScopeRepo
	}

	// Resolve the list of secrets to migrate.
	secrets := config.Secrets
	if len(secrets) == 0 {
		logger.Info("No specific secrets specified, fetching repo secrets from source...")
		secrets, err = fetchRepoSecrets(ctx, client, sourceRepo)
		if err != nil {
			return fmt.Errorf("failed to fetch secrets from source: %w", err)
		}
		logger.Info(fmt.Sprintf("Found %d secrets to migrate", len(secrets)))
	}

	// Apply exclusion filter.
	if len(config.ExcludeSecrets) > 0 {
		excludeSet := make(map[string]struct{}, len(config.ExcludeSecrets))
		for _, name := range config.ExcludeSecrets {
			excludeSet[name] = struct{}{}
		}
		filtered := secrets[:0]
		for _, name := range secrets {
			if _, excluded := excludeSet[name]; excluded {
				logger.Debug(fmt.Sprintf("Excluding secret: %s", name))
				continue
			}
			filtered = append(filtered, name)
		}
		secrets = filtered
	}

	// Parse rename mappings.
	renameMap := make(map[string]string)
	for _, mapping := range config.Rename {
		parts := strings.SplitN(mapping, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid rename mapping format: %s (expected OLD_NAME=NEW_NAME)", mapping)
		}
		renameMap[parts[0]] = parts[1]
	}

	// Parse the destination and determine its host.
	destRepo, err := parser.Repository(parser.RepositoryInput(config.Destination))
	if err != nil {
		return fmt.Errorf("failed to parse destination: %w", err)
	}
	destHost := destRepo.Host
	if destHost == "" {
		destHost = sourceRepo.Host
	}
	if destHost == "" {
		destHost = "github.com"
	}
	normalizedDst := config.Destination
	if destRepo.Owner != "" && destRepo.Name != "" {
		normalizedDst = destRepo.Owner + "/" + destRepo.Name
	}

	// Determine the temporary dispatch branch name.
	branch := config.Branch
	if branch == "" {
		branch = "gh-secret-kit-migrate-dispatch"
		if runID := actions.GetRunID(); runID != "" {
			branch += "-" + runID
		}
	}

	// Build and generate the workflow YAML.
	workflowConfig := migrator.WorkflowConfig{
		WorkflowName:           strings.TrimSuffix(workflowFileName, path.Ext(workflowFileName)),
		Source:                 sourceRepo.Owner + "/" + sourceRepo.Name,
		Destination:            normalizedDst,
		DestinationHost:        destHost,
		SourceEnv:              config.SourceEnv,
		DestinationEnv:         config.DestinationEnv,
		Secrets:                secrets,
		Rename:                 renameMap,
		Overwrite:              config.Overwrite,
		DestinationTokenSecret: config.DestinationTokenSecret,
		Scope:                  scope,
		DispatchMode:           true,
		RunsOn:                 runsOn,
		CleanupBranch:          branch,
	}
	logger.Info("Generating dispatch workflow YAML...")
	workflowYAML, err := migrator.GenerateWorkflowYAML(workflowConfig)
	if err != nil {
		return fmt.Errorf("failed to generate workflow YAML: %w", err)
	}

	// Create the temporary dispatch branch from the base commit.
	if _, err := gh.GetBranch(ctx, client, sourceRepo, branch); err != nil {
		logger.Info(fmt.Sprintf("Creating dispatch branch %s from %s...", branch, baseSHA))
		if _, berr := gh.CreateBranch(ctx, client, sourceRepo, branch, baseSHA); berr != nil {
			return fmt.Errorf("failed to create dispatch branch %s: %w", branch, berr)
		}
	} else {
		logger.Debug(fmt.Sprintf("Dispatch branch %s already exists", branch))
	}

	// Push the rewritten workflow file to the dispatch branch.
	logger.Info(fmt.Sprintf("Pushing workflow file to %s/%s at %s (branch: %s)...", sourceRepo.Owner, sourceRepo.Name, workflowPath, branch))
	fileOptions := &gh.RepositoryContentFileOptions{
		Message: fmt.Sprintf("Rewrite workflow for secret migration dispatch: %s", workflowFileName),
		Content: []byte(workflowYAML),
		Branch:  &branch,
	}
	existing, gerr := gh.GetRepositoryFileContent(ctx, client, sourceRepo, workflowPath, &branch)
	if gerr == nil && existing != nil {
		sha := existing.GetSHA()
		fileOptions.SHA = &sha
		if _, err := gh.UpdateRepositoryFile(ctx, client, sourceRepo, workflowPath, fileOptions); err != nil {
			return fmt.Errorf("failed to update workflow file: %w", err)
		}
	} else {
		if _, err := gh.CreateRepositoryFile(ctx, client, sourceRepo, workflowPath, fileOptions); err != nil {
			return fmt.Errorf("failed to create workflow file: %w", err)
		}
	}
	logger.Info("Workflow file pushed successfully")

	// Trigger the workflow_dispatch event on the dispatch branch.
	logger.Info(fmt.Sprintf("Triggering workflow_dispatch for %s on branch %s...", workflowFileName, branch))
	if _, err := gh.CreateWorkflowDispatchEventByFileName(ctx, client, sourceRepo, workflowFileName,
		github.CreateWorkflowDispatchEventRequest{Ref: branch}); err != nil {
		return fmt.Errorf("failed to trigger workflow_dispatch for %s: %w", workflowFileName, err)
	}
	logger.Info("Migration workflow dispatched!")

	return nil
}

// resolveRunsOn fetches the running workflow file from the source repository and
// extracts the runs-on value of the current job (GITHUB_JOB).
func resolveRunsOn(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, workflowPath, ref string) (any, error) {
	content, err := gh.GetFileContent(ctx, client, repo, workflowPath, &ref)
	if err != nil {
		return nil, fmt.Errorf("failed to read running workflow file %s: %w", workflowPath, err)
	}
	runsOn, err := migrator.ParseRunsOnFromWorkflow(string(content), actions.GetJobName())
	if err != nil {
		return nil, fmt.Errorf("failed to parse runs-on from running workflow: %w", err)
	}
	if runsOn == nil {
		return nil, fmt.Errorf("could not determine runs-on from running workflow; specify --runner-label")
	}
	return runsOn, nil
}

// currentActionsRepository builds a "[HOST/]OWNER/REPO" reference from the
// GitHub Actions environment variables.
func currentActionsRepository() string {
	full := actions.GetRepositoryFullName()
	if full == "" {
		return ""
	}
	host := strings.TrimPrefix(actions.GetRepositoryFullNameWithHost(), "https://")
	host = strings.TrimPrefix(host, "http://")
	if idx := strings.Index(host, "/"); idx > 0 {
		return host
	}
	return full
}
