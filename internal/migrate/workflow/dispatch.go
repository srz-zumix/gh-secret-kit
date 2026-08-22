package workflow

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/actions"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/gitutil"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// dispatchSetup holds the resolved workflow location, source client, base
// commit, and runs-on value shared by both the migration and dump dispatch
// flows.
type dispatchSetup struct {
	sourceRepo       repository.Repository
	client           *gh.GitHubClient
	workflowPath     string
	workflowFileName string
	targetSpecified  bool
	baseSHA          string
	runsOn           any
}

// prepareDispatchSetup resolves everything needed before dispatch workflow
// content is generated: the workflow file location and mode (self-rewrite vs.
// target-specified), the source repository and GitHub client, the archive
// state, the base commit to branch from, and the runs-on value for the
// generated workflow's job.
//
// It operates in two modes depending on whether source is given:
//
//   - Self-rewrite (source empty): it must run inside a workflow_dispatch-triggered
//     workflow and rewrites the currently running workflow. The runs-on of the
//     running workflow is reused unless runnerLabel is set.
//   - Target-specified (source set): the workflow does not exist in the target
//     repository, so the in-workflow precondition is skipped. runnerLabel is
//     required in this mode.
//
// The returned cleanup function must be deferred by the caller; it restores
// the repository archive state if unarchive requested it. On error, any
// archive state already changed is restored before returning.
func prepareDispatchSetup(ctx context.Context, source, runnerLabel, workflowName string, unarchive, skipArchiveCheck bool) (*dispatchSetup, func(), error) {
	targetSpecified := source != ""

	var workflowPath, workflowFileName string
	if targetSpecified {
		// Target-specified mode: --runner-label is required because the running
		// workflow's runs-on cannot be reused.
		if runnerLabel == "" {
			return nil, nil, fmt.Errorf("--runner-label is required when a source repository is specified")
		}
		if workflowName == "" {
			return nil, nil, fmt.Errorf("--workflow-name must not be empty when a source repository is specified")
		}
		workflowFileName = workflowName + ".yml"
		workflowPath = ".github/workflows/" + workflowFileName
	} else {
		// Self-rewrite mode: this command must run inside a
		// workflow_dispatch-triggered workflow.
		if !actions.IsRunsOn() {
			return nil, nil, fmt.Errorf("dispatch must run inside a GitHub Actions workflow")
		}
		if !actions.IsWorkflowDispatchEvent() {
			return nil, nil, fmt.Errorf("dispatch must be triggered by a workflow_dispatch event (current event: %q)", actions.GetEventName())
		}
		// Determine the running workflow file path from GITHUB_WORKFLOW_REF.
		workflowPath = actions.GetWorkflowFilePath()
		if workflowPath == "" {
			return nil, nil, fmt.Errorf("failed to determine the running workflow path from GITHUB_WORKFLOW_REF")
		}
		workflowFileName = path.Base(workflowPath)
	}
	logger.Debug(fmt.Sprintf("Workflow path: %s", workflowPath))

	// Resolve the source repository, defaulting to the repository running the workflow.
	resolvedSource := source
	if resolvedSource == "" {
		resolvedSource = currentActionsRepository()
	}
	sourceRepo, err := parser.Repository(parser.RepositoryInput(resolvedSource))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse source repository: %w", err)
	}

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Check if the repository is archived and handle unarchive if requested.
	cleanup := func() {}
	if !skipArchiveCheck {
		c, uerr := handleUnarchiveWithCheck(ctx, client, sourceRepo, unarchive)
		if uerr != nil {
			return nil, nil, uerr
		}
		cleanup = c
	}

	// Determine the base commit to branch from.
	baseSHA := ""
	if !targetSpecified {
		baseSHA = actions.GetSHA()
	}
	if baseSHA == "" {
		repoInfo, gerr := gh.GetRepository(ctx, client, sourceRepo)
		if gerr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to get repository: %w", gerr)
		}
		branchInfo, gerr := gh.GetBranch(ctx, client, sourceRepo, repoInfo.GetDefaultBranch())
		if gerr != nil {
			cleanup()
			return nil, nil, fmt.Errorf("failed to get default branch %s: %w", repoInfo.GetDefaultBranch(), gerr)
		}
		baseSHA = branchInfo.GetCommit().GetSHA()
	}

	// Determine the runs-on value for the generated workflow.
	var runsOn any
	if runnerLabel != "" {
		runsOn = runnerLabel
	} else {
		runsOn, err = resolveRunsOn(ctx, client, sourceRepo, workflowPath, baseSHA)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
	}

	return &dispatchSetup{
		sourceRepo:       sourceRepo,
		client:           client,
		workflowPath:     workflowPath,
		workflowFileName: workflowFileName,
		targetSpecified:  targetSpecified,
		baseSHA:          baseSHA,
		runsOn:           runsOn,
	}, cleanup, nil
}

// resolveDispatchBranch validates a user-supplied branch name, or generates a
// unique one from prefix and the current workflow run ID (falling back to a
// timestamp outside GitHub Actions).
func resolveDispatchBranch(branch, prefix string) (string, error) {
	// Validate user-supplied branch name. Auto-generated names are always safe;
	// only a non-empty branch (from --branch) needs to be checked. Git ref
	// names must not contain shell metacharacters because the branch name is
	// embedded in the cleanup script that runs inside the generated workflow.
	if branch != "" {
		if err := gitutil.ValidateBranchName(branch); err != nil {
			return "", err
		}
		return branch, nil
	}
	branch = prefix
	if runID := actions.GetRunID(); runID != "" {
		branch += "-" + runID
	} else {
		// Outside GitHub Actions (e.g. target-specified mode) there is no run
		// ID, so use a timestamp to keep the branch name unique.
		branch += "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return branch, nil
}

// createDispatchBranch creates the temporary dispatch branch from baseSHA. An
// existing branch is treated as an error because the generated workflow
// deletes the branch after a successful run; reusing it could update a
// caller-owned branch's workflow file and then delete that branch.
func createDispatchBranch(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch, baseSHA string) error {
	if _, err := gh.GetBranch(ctx, client, repo, branch); err == nil {
		return fmt.Errorf("dispatch branch %s already exists; specify a different --branch", branch)
	} else if !gh.IsHTTPNotFound(err) {
		return fmt.Errorf("failed to check dispatch branch %s: %w", branch, err)
	}
	logger.Info(fmt.Sprintf("Creating dispatch branch %s from %s...", branch, baseSHA))
	if _, err := gh.CreateBranch(ctx, client, repo, branch, baseSHA); err != nil {
		return fmt.Errorf("failed to create dispatch branch %s: %w", branch, err)
	}
	return nil
}

// triggerDispatchWorkflow registers (in target-specified mode), pushes, and
// triggers workflowYAML on the dispatch branch, then optionally waits for the
// run to complete.
func triggerDispatchWorkflow(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, setup *dispatchSetup, branch, workflowName, workflowYAML string, wait, deleteRunAfterWait bool, timeout string) error {
	dispatchReq := github.CreateWorkflowDispatchEventRequest{Ref: branch}

	if setup.targetSpecified {
		// Target-specified mode: the workflow does not exist on the default
		// branch, so a plain dispatch would return 404. Register the workflow by
		// first pushing a syntax-error version and attempting a dispatch (which
		// fails but registers it), then push the corrected workflow and dispatch.
		logger.Info("Registering workflow via syntax-error workflow trick...")
		brokenYAML, berr := migrator.GenerateBrokenWorkflowYAML(workflowName)
		if berr != nil {
			return fmt.Errorf("failed to generate registration workflow YAML: %w", berr)
		}
		if err := pushWorkflowFile(ctx, client, repo, setup.workflowPath, branch,
			fmt.Sprintf("Register workflow for dispatch: %s", setup.workflowFileName), brokenYAML); err != nil {
			return err
		}
		// Attempt a dispatch to register the workflow. The error is expected and ignored.
		if _, derr := gh.CreateWorkflowDispatchEventByFileName(ctx, client, repo, setup.workflowFileName, dispatchReq); derr != nil {
			logger.Debug(fmt.Sprintf("Registration dispatch returned an expected error: %v", derr))
		}
	}

	// Push the generated workflow file to the dispatch branch.
	logger.Info(fmt.Sprintf("Pushing workflow file to %s/%s at %s (branch: %s)...", repo.Owner, repo.Name, setup.workflowPath, branch))
	if err := pushWorkflowFile(ctx, client, repo, setup.workflowPath, branch,
		fmt.Sprintf("Rewrite workflow for dispatch: %s", setup.workflowFileName), workflowYAML); err != nil {
		return err
	}
	logger.Info("Workflow file pushed successfully")

	// Build a RunConfig to share wait/polling helpers with the run command.
	runConfig := &RunConfig{
		WorkflowName: workflowName,
		Branch:       branch,
		Timeout:      timeout,
	}

	// Snapshot the latest run number before triggering so we can identify the
	// new run when waiting.
	preDispatchMaxNumber := fetchLatestRunNumber(ctx, client, repo, runConfig)

	// Trigger the workflow_dispatch event on the dispatch branch.
	logger.Info(fmt.Sprintf("Triggering workflow_dispatch for %s on branch %s...", setup.workflowFileName, branch))
	if err := dispatchWithRetry(ctx, client, repo, setup.workflowFileName, dispatchReq, setup.targetSpecified); err != nil {
		return fmt.Errorf("failed to trigger workflow_dispatch for %s: %w", setup.workflowFileName, err)
	}
	logger.Info("Workflow dispatched!")

	if !wait {
		return nil
	}

	runID, err := waitForWorkflowRun(ctx, client, repo, runConfig, preDispatchMaxNumber)
	if err != nil {
		return err
	}

	if deleteRunAfterWait {
		logger.Info(fmt.Sprintf("Deleting workflow run #%d history...", runID))
		if derr := gh.DeleteWorkflowRun(ctx, client, repo, runID); derr != nil {
			return fmt.Errorf("failed to delete workflow run history: %w", derr)
		}
		logger.Info("Workflow run history deleted")
	}

	return nil
}


// excludeSecrets removes any secret names present in exclude from secrets,
// preserving order.
func excludeSecrets(secrets, exclude []string) []string {
	if len(exclude) == 0 {
		return secrets
	}
	excludeSet := make(map[string]struct{}, len(exclude))
	for _, name := range exclude {
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
	return filtered
}

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

	setup, cleanup, err := prepareDispatchSetup(ctx, config.Source, config.RunnerLabel, config.WorkflowName, config.Unarchive, config.SkipArchiveCheck)
	if err != nil {
		return err
	}
	defer cleanup()

	scope := config.Scope
	if scope == "" {
		scope = migrator.SecretScopeRepo
	}

	// Resolve the list of secrets to migrate.
	secrets := config.Secrets
	if len(secrets) == 0 {
		switch scope {
		case migrator.SecretScopeEnv:
			logger.Info("No specific secrets specified, fetching env secrets from source...")
			secrets, err = fetchEnvSecrets(ctx, setup.client, setup.sourceRepo, config.SourceEnv)
		case migrator.SecretScopeOrg:
			logger.Info("No specific secrets specified, fetching org secrets from source...")
			secrets, err = fetchOrgSecrets(ctx, setup.client, setup.sourceRepo)
		default:
			logger.Info("No specific secrets specified, fetching repo secrets from source...")
			secrets, err = fetchRepoSecrets(ctx, setup.client, setup.sourceRepo)
		}
		if err != nil {
			return fmt.Errorf("failed to fetch secrets from source: %w", err)
		}
		logger.Info(fmt.Sprintf("Found %d secrets to migrate", len(secrets)))
	}
	secrets = excludeSecrets(secrets, config.ExcludeSecrets)

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
		destHost = setup.sourceRepo.Host
	}
	if destHost == "" {
		destHost = "github.com"
	}
	normalizedDst := config.Destination
	if destRepo.Owner != "" && destRepo.Name != "" {
		normalizedDst = destRepo.Owner + "/" + destRepo.Name
	}

	branch, err := resolveDispatchBranch(config.Branch, "gh-secret-kit-migrate-dispatch")
	if err != nil {
		return err
	}

	// Build and generate the workflow YAML.
	workflowConfig := migrator.WorkflowConfig{
		WorkflowName:           strings.TrimSuffix(setup.workflowFileName, path.Ext(setup.workflowFileName)),
		Source:                 setup.sourceRepo.Owner + "/" + setup.sourceRepo.Name,
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
		RunsOn:                 setup.runsOn,
		CleanupBranch:          branch,
	}
	logger.Info("Generating dispatch workflow YAML...")
	workflowYAML, err := migrator.GenerateWorkflowYAML(workflowConfig)
	if err != nil {
		return fmt.Errorf("failed to generate workflow YAML: %w", err)
	}

	if err := createDispatchBranch(ctx, setup.client, setup.sourceRepo, branch, setup.baseSHA); err != nil {
		return err
	}

	return triggerDispatchWorkflow(ctx, setup.client, setup.sourceRepo, setup, branch, workflowConfig.WorkflowName, workflowYAML, config.Wait, config.DeleteRunAfterWait, config.Timeout)
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
	if gerr != nil && !gh.IsHTTPNotFound(gerr) {
		return fmt.Errorf("failed to check workflow file %s: %w", workflowPath, gerr)
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
