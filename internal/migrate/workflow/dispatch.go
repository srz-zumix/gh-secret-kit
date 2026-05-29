package workflow

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v84/github"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/actions"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// RunDispatch generates a migration workflow, pushes it to a temporary branch,
// and triggers it via workflow_dispatch.
//
// It operates in two modes depending on whether a source repository is given:
//
//   - Self-rewrite (Source empty): it must run inside a workflow_dispatch-triggered
//     workflow and rewrites the currently running workflow. The runs-on of the
//     running workflow is reused unless --runner-label is set.
//   - Target-specified (Source set): the workflow does not exist in the target
//     repository, so the in-workflow precondition is skipped. A syntax-error
//     workflow is pushed first to register it, then the corrected workflow is
//     pushed and dispatched. --runner-label is required in this mode.
func RunDispatch(ctx context.Context, config *DispatchConfig) error {
	logger.Info("Dispatching migration workflow")

	targetSpecified := config.Source != ""

	var workflowPath, workflowFileName string
	if targetSpecified {
		// Target-specified mode: --runner-label is required because the running
		// workflow's runs-on cannot be reused.
		if config.RunnerLabel == "" {
			return fmt.Errorf("--runner-label is required when --src is specified")
		}
		name := config.WorkflowName
		if name == "" {
			return fmt.Errorf("--workflow-name must not be empty when --src is specified")
		}
		workflowFileName = name + ".yml"
		workflowPath = ".github/workflows/" + workflowFileName
	} else {
		// Self-rewrite mode: this command must run inside a
		// workflow_dispatch-triggered workflow.
		if !actions.IsRunsOn() {
			return fmt.Errorf("dispatch must run inside a GitHub Actions workflow")
		}
		if !actions.IsWorkflowDispatchEvent() {
			return fmt.Errorf("dispatch must be triggered by a workflow_dispatch event (current event: %q)", actions.GetEventName())
		}
		// Determine the running workflow file path from GITHUB_WORKFLOW_REF.
		workflowPath = actions.GetWorkflowFilePath()
		if workflowPath == "" {
			return fmt.Errorf("failed to determine the running workflow path from GITHUB_WORKFLOW_REF")
		}
		workflowFileName = path.Base(workflowPath)
	}
	logger.Debug(fmt.Sprintf("Workflow path: %s", workflowPath))

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
	baseSHA := ""
	if !targetSpecified {
		baseSHA = actions.GetSHA()
	}
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

	dispatchReq := github.CreateWorkflowDispatchEventRequest{Ref: branch}

	if targetSpecified {
		// Target-specified mode: the workflow does not exist on the default
		// branch, so a plain dispatch would return 404. Register the workflow by
		// first pushing a syntax-error version and attempting a dispatch (which
		// fails but registers it), then push the corrected workflow and dispatch.
		logger.Info("Registering workflow via syntax-error workflow trick...")
		brokenYAML, berr := migrator.GenerateBrokenWorkflowYAML(workflowConfig.WorkflowName)
		if berr != nil {
			return fmt.Errorf("failed to generate registration workflow YAML: %w", berr)
		}
		if err := pushWorkflowFile(ctx, client, sourceRepo, workflowPath, branch,
			fmt.Sprintf("Register workflow for secret migration dispatch: %s", workflowFileName), brokenYAML); err != nil {
			return err
		}
		// Attempt a dispatch to register the workflow. The error is expected and ignored.
		if _, derr := gh.CreateWorkflowDispatchEventByFileName(ctx, client, sourceRepo, workflowFileName, dispatchReq); derr != nil {
			logger.Debug(fmt.Sprintf("Registration dispatch returned an expected error: %v", derr))
		}
	}

	// Push the migration workflow file to the dispatch branch.
	logger.Info(fmt.Sprintf("Pushing workflow file to %s/%s at %s (branch: %s)...", sourceRepo.Owner, sourceRepo.Name, workflowPath, branch))
	if err := pushWorkflowFile(ctx, client, sourceRepo, workflowPath, branch,
		fmt.Sprintf("Rewrite workflow for secret migration dispatch: %s", workflowFileName), workflowYAML); err != nil {
		return err
	}
	logger.Info("Workflow file pushed successfully")

	// Trigger the workflow_dispatch event on the dispatch branch.
	logger.Info(fmt.Sprintf("Triggering workflow_dispatch for %s on branch %s...", workflowFileName, branch))
	if err := dispatchWithRetry(ctx, client, sourceRepo, workflowFileName, dispatchReq, targetSpecified); err != nil {
		return fmt.Errorf("failed to trigger workflow_dispatch for %s: %w", workflowFileName, err)
	}
	logger.Info("Migration workflow dispatched!")

	return nil
}

// pushWorkflowFile creates or updates the workflow file at workflowPath on the
// given branch.
func pushWorkflowFile(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, workflowPath, branch, message, content string) error {
	fileOptions := &gh.RepositoryContentFileOptions{
		Message: message,
		Content: []byte(content),
		Branch:  &branch,
	}
	existing, gerr := gh.GetRepositoryFileContent(ctx, client, repo, workflowPath, &branch)
	if gerr == nil && existing != nil {
		sha := existing.GetSHA()
		fileOptions.SHA = &sha
		if _, err := gh.UpdateRepositoryFile(ctx, client, repo, workflowPath, fileOptions); err != nil {
			return fmt.Errorf("failed to update workflow file: %w", err)
		}
		return nil
	}
	if _, err := gh.CreateRepositoryFile(ctx, client, repo, workflowPath, fileOptions); err != nil {
		return fmt.Errorf("failed to create workflow file: %w", err)
	}
	return nil
}

// dispatchWithRetry triggers a workflow_dispatch event. When retry is enabled, it
// retries a few times with a short backoff to allow GitHub to index the workflow
// after it has just been registered.
func dispatchWithRetry(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, workflowFileName string, req github.CreateWorkflowDispatchEventRequest, retry bool) error {
	attempts := 1
	if retry {
		attempts = 5
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i*2) * time.Second)
		}
		if _, err := gh.CreateWorkflowDispatchEventByFileName(ctx, client, repo, workflowFileName, req); err != nil {
			lastErr = err
			logger.Debug(fmt.Sprintf("Dispatch attempt %d failed: %v", i+1, err))
			continue
		}
		return nil
	}
	return lastErr
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
