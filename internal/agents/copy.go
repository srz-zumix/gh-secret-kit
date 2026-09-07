// Package agents copies GitHub Copilot coding agent (Agents) secrets by running
// the copy inside an ephemeral Copilot coding agent environment.
package agents

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/internal/destination"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/actions"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/gitutil"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// pollInterval is how often the agent session is checked while waiting for the
// copy to finish.
const pollInterval = 20 * time.Second

// taskListLimit is how many agent tasks are inspected when looking for the
// session that was just created.
const taskListLimit = 30

// copilotAgentRunEvent is the workflow run event of the ephemeral GitHub Actions
// run that executes the setup steps and then the Copilot cloud agent.
const copilotAgentRunEvent = "dynamic"

// logMaxRedirects bounds the redirects followed when downloading a job log.
const logMaxRedirects = 3

// CopyConfig holds configuration for the Agents secret copy.
//
// Agents secret values cannot be read through the API; they are only exposed as
// environment variables inside the Copilot coding agent environment, and only
// Agents secrets are exposed there. The copy therefore commits a
// copilot-setup-steps workflow to a temporary branch, makes that branch the
// default one so Copilot picks the workflow up, registers the destination tokens
// as temporary Agents secrets, starts a Copilot coding agent session, and lets
// the setup steps perform the copy, so the values never reach the local machine.
// Every temporary change is reverted afterwards.
type CopyConfig struct {
	Source string
	// Destinations are [HOST/]OWNER/REPO references, or [HOST/]ORG when Scope
	// is SecretScopeOrg.
	Destinations   []string
	Secrets        []string
	ExcludeSecrets []string
	Rename         []string
	Overwrite      bool
	// Scope selects both the source Agents secret level and the destination
	// level. Only repo and org are supported.
	Scope migrator.SecretScope
	// DestinationApp selects which secret store the destination secrets are
	// written to. Empty means migrator.SecretAppAgents.
	DestinationApp migrator.SecretApp
	// DestinationToken overrides the token resolved from the local gh
	// authentication for the destination host. It cannot be used when the
	// destinations span multiple hosts.
	DestinationToken string
	// TokenSecretName is the base name of the temporary Agents secret that
	// holds the destination token. The destination host is appended.
	TokenSecretName string
	// Branch is the temporary branch that carries the setup steps workflow and
	// is made the default branch for the duration of the copy. Empty means a
	// unique generated name.
	Branch string
	// Prompt is the task description passed to the Copilot coding agent. The
	// agent itself has nothing to do; the copy runs in the setup steps.
	Prompt string
	// Timeout bounds how long the copy waits for the agent session.
	Timeout time.Duration
	// KeepWorkflow leaves the temporary branch and Agents secrets in place, for
	// debugging. The default branch is always restored.
	KeepWorkflow bool
}

// RunCopy copies the Agents secrets available to the source repository to every
// destination, by running the copy inside a Copilot coding agent environment.
func RunCopy(ctx context.Context, config *CopyConfig) error {
	logger.Info("Copying Agents secrets")

	scope := config.Scope
	if scope == "" {
		scope = migrator.SecretScopeRepo
	}
	if scope != migrator.SecretScopeRepo && scope != migrator.SecretScopeOrg {
		return fmt.Errorf("unsupported --scope %q for Agents secrets: expected repo or org", scope)
	}

	app := config.DestinationApp
	if app == "" {
		app = migrator.SecretAppAgents
	}
	if err := migrator.ValidateSecretApp(app); err != nil {
		return err
	}

	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}
	if sourceRepo.Host != "" && sourceRepo.Host != "github.com" {
		return fmt.Errorf("the Copilot coding agent is only available on github.com, but the source host is %s", sourceRepo.Host)
	}
	if sourceRepo.Name == "" {
		return fmt.Errorf("source must be in [HOST/]OWNER/REPO format")
	}
	sourceSlug := sourceRepo.Owner + "/" + sourceRepo.Name

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	secrets, err := collectSecrets(ctx, client, sourceRepo, config, scope)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		logger.Info("No Agents secrets found to copy, skipping")
		return nil
	}

	orgLevel := scope == migrator.SecretScopeOrg
	destinations, err := destination.Resolve(config.Destinations, orgLevel, sourceRepo)
	if err != nil {
		return err
	}
	hostTokens, err := destination.ResolveTokens(config.DestinationToken, destinations)
	if err != nil {
		return err
	}
	if err := destination.Verify(ctx, orgLevel, destinations, hostTokens); err != nil {
		return err
	}

	renameMap, err := migrator.ParseRenameMappings(config.Rename)
	if err != nil {
		return err
	}

	tokenSecretNames := make(map[string]string, len(hostTokens))
	for host := range hostTokens {
		tokenSecretNames[host] = destination.NameForHost(config.TokenSecretName, host)
	}

	scriptConfig := migrator.AgentsCopyConfig{
		Scope:          scope,
		DestinationApp: app,
		Secrets:        secrets,
		Rename:         renameMap,
		Overwrite:      config.Overwrite,
		Destinations:   buildScriptDestinations(destinations, tokenSecretNames),
	}
	script, err := migrator.GenerateAgentsCopyScript(scriptConfig)
	if err != nil {
		return fmt.Errorf("failed to generate the copy script: %w", err)
	}
	workflow, err := migrator.GenerateAgentsSetupStepsYAML(scriptConfig, script)
	if err != nil {
		return fmt.Errorf("failed to generate the setup steps workflow: %w", err)
	}

	branch, err := resolveBranch(config.Branch)
	if err != nil {
		return err
	}

	logger.Info(fmt.Sprintf("Copying %d Agents secrets to %d destinations", len(secrets), len(destinations)))

	// The destination tokens have to be Agents secrets: the Copilot coding agent
	// environment is not given Actions, Codespaces, or Dependabot secrets.
	if err := registerTokenSecrets(ctx, client, sourceRepo, tokenSecretNames, hostTokens); err != nil {
		return err
	}
	if config.KeepWorkflow {
		logger.Warn(fmt.Sprintf("Keeping the temporary Agents secrets on %s", sourceSlug))
	} else {
		defer removeTokenSecrets(context.WithoutCancel(ctx), client, sourceRepo, tokenSecretNames)
	}

	// Copilot only runs the setup steps workflow from the default branch, so the
	// workflow is committed to a temporary branch that is made the default one
	// for the duration of the copy.
	restore, err := prepareSetupStepsBranch(ctx, client, sourceRepo, branch, workflow, config.KeepWorkflow)
	if err != nil {
		return err
	}
	defer restore(context.WithoutCancel(ctx))

	task, err := startCopyTask(ctx, sourceSlug, branch, config.Prompt)
	if err != nil {
		return err
	}
	logger.Info(fmt.Sprintf("Started agent session %s", task.ID))

	if err := waitForCopy(ctx, client, sourceRepo, sourceSlug, task.ID, config.Timeout); err != nil {
		return err
	}

	if config.KeepWorkflow {
		logger.Warn(fmt.Sprintf("The agent session %s and the pull request it opened are not cleaned up; close them with \"gh agent-task view --repo %s %s\"", task.ID, sourceSlug, task.ID))
	}
	logger.Info("Agents secrets copied successfully")
	return nil
}

// resolveBranch validates a user-supplied branch name or generates a unique one.
func resolveBranch(branch string) (string, error) {
	if branch != "" {
		if err := gitutil.ValidateBranchName(branch); err != nil {
			return "", err
		}
		return branch, nil
	}
	branch = types.DefaultAgentsCopyBranch
	if runID := actions.GetRunID(); runID != "" {
		return branch + "-" + runID, nil
	}
	return branch + "-" + strconv.FormatInt(time.Now().UnixNano(), 10), nil
}

// buildScriptDestinations converts the resolved destinations into the form
// expected by the script generator.
func buildScriptDestinations(destinations []*destination.Destination, tokenSecretNames map[string]string) []migrator.AgentsCopyDestination {
	result := make([]migrator.AgentsCopyDestination, 0, len(destinations))
	for _, dest := range destinations {
		result = append(result, migrator.AgentsCopyDestination{
			Target:   dest.Target,
			Host:     dest.Host,
			TokenEnv: tokenSecretNames[dest.Host],
		})
	}
	return result
}

// registerTokenSecrets stores the destination tokens as temporary Agents secrets
// on the source repository. An existing secret with the same name is not
// touched, because it would be deleted during the cleanup.
func registerTokenSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, tokenSecretNames, hostTokens map[string]string) error {
	existing, err := gh.ListAgentsRepoSecrets(ctx, client, repo)
	if err != nil {
		return fmt.Errorf("failed to list the Agents secrets of the source repository: %w", err)
	}
	present := make(map[string]struct{}, len(existing))
	for _, secret := range existing {
		present[secret.GetName()] = struct{}{}
	}

	// Preflight every name before creating anything, because map iteration order
	// is nondeterministic and creating a secret only to abort on a later name
	// clash would leave a live destination token behind.
	for _, name := range tokenSecretNames {
		if _, ok := present[name]; ok {
			return fmt.Errorf("the source repository already has an Agents secret named %q: use --token-secret-name to pick another name", name)
		}
	}

	// Track the secrets created so far so they can be removed if a later
	// registration fails, otherwise a partial failure would leak live
	// destination tokens.
	created := make(map[string]string, len(tokenSecretNames))
	for host, name := range tokenSecretNames {
		logger.Info(fmt.Sprintf("Registering the temporary Agents secret %s...", name))
		if err := gh.SetAgentsRepoSecret(ctx, client, repo, name, hostTokens[host]); err != nil {
			removeTokenSecrets(context.WithoutCancel(ctx), client, repo, created)
			return fmt.Errorf("failed to register the temporary Agents secret %s: %w", name, err)
		}
		created[host] = name
	}
	return nil
}

// removeTokenSecrets deletes the temporary Agents secrets, logging instead of
// failing so that the remaining cleanup still runs.
func removeTokenSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, tokenSecretNames map[string]string) {
	for _, name := range tokenSecretNames {
		logger.Info(fmt.Sprintf("Deleting the temporary Agents secret %s...", name))
		if err := gh.DeleteAgentsRepoSecret(ctx, client, repo, name); err != nil {
			logger.Warn(fmt.Sprintf("failed to delete the temporary Agents secret %s: %v", name, err))
		}
	}
}

// prepareSetupStepsBranch creates a temporary branch carrying the generated
// workflow and makes it the default branch, because Copilot only runs
// copilot-setup-steps.yml from the default branch. The returned function always
// restores the original default branch, and removes the temporary branch unless
// keepBranch is set. Deleting the temporary branch also closes the pull request
// the agent opens against it.
func prepareSetupStepsBranch(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch, workflow string, keepBranch bool) (cleanup func(context.Context), err error) {
	repoInfo, err := gh.GetRepository(ctx, client, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get the source repository: %w", err)
	}
	originalDefault := repoInfo.GetDefaultBranch()
	baseBranch, err := gh.GetBranch(ctx, client, repo, originalDefault)
	if err != nil {
		return nil, fmt.Errorf("failed to get the default branch %s: %w", originalDefault, err)
	}

	if _, err := gh.GetBranch(ctx, client, repo, branch); err == nil {
		return nil, fmt.Errorf("branch %s already exists: specify a different --branch", branch)
	} else if !gh.IsHTTPNotFound(err) {
		return nil, fmt.Errorf("failed to check branch %s: %w", branch, err)
	}

	logger.Info(fmt.Sprintf("Creating temporary branch %s from %s...", branch, originalDefault))
	if _, err := gh.CreateBranch(ctx, client, repo, branch, baseBranch.GetCommit().GetSHA()); err != nil {
		return nil, fmt.Errorf("failed to create temporary branch %s: %w", branch, err)
	}

	switched := false
	cleanup = func(cleanupCtx context.Context) {
		if switched {
			logger.Info(fmt.Sprintf("Restoring the default branch to %s...", originalDefault))
			if _, rerr := gh.EditRepository(cleanupCtx, client, repo, &github.Repository{DefaultBranch: github.Ptr(originalDefault)}); rerr != nil {
				logger.Warn(fmt.Sprintf("failed to restore the default branch to %s: %v", originalDefault, rerr))
				return
			}
		}
		if keepBranch {
			logger.Warn(fmt.Sprintf("Keeping the temporary branch %s", branch))
			return
		}
		logger.Info(fmt.Sprintf("Deleting temporary branch %s...", branch))
		if derr := gh.DeleteBranchIfExists(cleanupCtx, client, repo, branch); derr != nil {
			logger.Warn(fmt.Sprintf("failed to delete temporary branch %s: %v", branch, derr))
		}
	}
	// The branch already exists from here on, so undo it when a later step fails.
	defer func() {
		if err != nil {
			cleanup(context.WithoutCancel(ctx))
			cleanup = nil
		}
	}()

	path := migrator.AgentsSetupStepsPath
	logger.Info(fmt.Sprintf("Committing %s to %s...", path, branch))
	opts := &gh.RepositoryContentFileOptions{
		Message: "chore: add temporary copilot-setup-steps for gh-secret-kit secret copy",
		Content: []byte(workflow),
		Branch:  github.Ptr(branch),
	}
	existing, gerr := gh.GetRepositoryFileContent(ctx, client, repo, path, github.Ptr(branch))
	if gerr != nil && !gh.IsHTTPNotFound(gerr) {
		// Use a naked return so the deferred cleanup still sees the installed
		// cleanup closure; "return nil, err" would clear the named cleanup
		// result before the defer runs and panic on the nil call.
		err = fmt.Errorf("failed to check %s on %s: %w", path, branch, gerr)
		return
	}
	if gerr == nil && existing != nil {
		opts.SHA = github.Ptr(existing.GetSHA())
		_, err = gh.UpdateRepositoryFile(ctx, client, repo, path, opts)
	} else {
		_, err = gh.CreateRepositoryFile(ctx, client, repo, path, opts)
	}
	if err != nil {
		err = fmt.Errorf("failed to commit %s to %s: %w", path, branch, err)
		return
	}

	logger.Info(fmt.Sprintf("Temporarily switching the default branch to %s...", branch))
	if _, err = gh.EditRepository(ctx, client, repo, &github.Repository{DefaultBranch: github.Ptr(branch)}); err != nil {
		err = fmt.Errorf("failed to switch the default branch to %s: %w", branch, err)
		return
	}
	switched = true

	return cleanup, nil
}

// startCopyTask starts a Copilot coding agent session and returns the session it
// created. "gh agent-task create" does not report the session, so the session
// list is compared before and after.
func startCopyTask(ctx context.Context, repoSlug, base, prompt string) (*agentTask, error) {
	before, err := listAgentTasks(ctx, taskListLimit)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(before))
	for _, task := range before {
		known[task.ID] = struct{}{}
	}

	logger.Info("Starting a Copilot coding agent session on the source repository...")
	if err := createAgentTask(ctx, repoSlug, base, prompt); err != nil {
		return nil, err
	}

	after, err := listAgentTasks(ctx, taskListLimit)
	if err != nil {
		return nil, err
	}
	for _, task := range after {
		if _, ok := known[task.ID]; ok {
			continue
		}
		if !strings.EqualFold(task.Repository, repoSlug) {
			continue
		}
		return &task, nil
	}
	return nil, fmt.Errorf("failed to determine the agent session started on %s", repoSlug)
}

// waitForCopy waits until the copy script reports that it finished, the agent
// session ends, or the timeout expires.
func waitForCopy(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, repoSlug, id string, timeout time.Duration) error {
	logger.Info("Waiting for the copy to run in the agent environment...")
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		task, terr := viewAgentTask(ctx, repoSlug, id)
		var log string
		if terr == nil && task.PullRequestNumber > 0 {
			var lerr error
			log, lerr = setupStepsLog(ctx, client, repo, task.PullRequestNumber)
			if lerr != nil {
				// The log is not served until the Actions run has started.
				logger.Debug(fmt.Sprintf("the setup steps log is not available yet: %v", lerr))
			} else if strings.Contains(log, migrator.AgentsCopyDoneMarker) {
				logCopyResults(log)
				return nil
			}
		}

		if terr == nil && task.isFinished() {
			return fmt.Errorf("the agent session %s finished with state %q before the copy completed%s", id, task.State, copyFailureDetail(log))
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for the copy to run in agent session %s", timeout, id)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// setupStepsLog downloads the GitHub Actions job log of the Copilot cloud agent
// run of the given pull request. The setup steps output is not part of
// "gh agent-task view --log", so it is read from the Actions run instead.
func setupStepsLog(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, prNumber int) (string, error) {
	pr, err := gh.GetPullRequest(ctx, client, repo, prNumber)
	if err != nil {
		return "", fmt.Errorf("failed to get pull request %d: %w", prNumber, err)
	}
	head := pr.GetHead().GetRef()
	if head == "" {
		return "", fmt.Errorf("pull request %d has no head branch", prNumber)
	}

	runs, err := gh.ListRepositoryWorkflowRuns(ctx, client, repo, &gh.ListWorkflowRunsOptions{
		Branch: head,
		Event:  copilotAgentRunEvent,
	})
	if err != nil {
		return "", fmt.Errorf("failed to list the workflow runs of %s: %w", head, err)
	}
	var latest *github.WorkflowRun
	for _, run := range runs {
		if latest == nil || run.GetCreatedAt().After(latest.GetCreatedAt().Time) {
			latest = run
		}
	}
	if latest == nil {
		return "", fmt.Errorf("no %s workflow run found on %s", copilotAgentRunEvent, head)
	}

	jobs, err := gh.ListWorkflowJobs(ctx, client, repo, latest.GetID(), nil)
	if err != nil {
		return "", fmt.Errorf("failed to list the jobs of workflow run %d: %w", latest.GetID(), err)
	}
	var content strings.Builder
	for _, job := range jobs {
		data, err := gh.GetWorkflowJobLogsContent(ctx, client, repo, job.GetID(), logMaxRedirects)
		if err != nil {
			return "", fmt.Errorf("failed to read the log of job %d: %w", job.GetID(), err)
		}
		content.Write(data)
	}
	return content.String(), nil
}

// logCopyResults reports the progress lines the copy script printed.
func logCopyResults(log string) {
	for _, line := range logLines(log) {
		if strings.HasPrefix(line, "Successfully migrated secret:") || strings.HasPrefix(line, "Copying secrets to ") {
			logger.Info(line)
		}
	}
}

// copyFailureDetail collects the errors GitHub Actions reported, so that a
// failure of the copy script is visible without opening the run.
func copyFailureDetail(log string) string {
	var details []string
	for _, line := range logLines(log) {
		if after, ok := strings.CutPrefix(line, "##[error]"); ok {
			details = append(details, strings.TrimSpace(after))
		}
	}
	if len(details) == 0 {
		return ""
	}
	return ": " + strings.Join(details, "; ")
}

// logLines splits an Actions job log into lines, dropping the timestamp prefix
// and the echoed step script, which is colored and would match the markers.
func logLines(log string) []string {
	raw := strings.Split(log, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.Contains(line, "\x1b[") {
			continue
		}
		if _, rest, ok := strings.Cut(strings.TrimRight(line, "\r"), " "); ok {
			lines = append(lines, rest)
		}
	}
	return lines
}

// collectSecrets resolves the Agents secret names to copy and applies the
// include and exclude filters.
func collectSecrets(ctx context.Context, client *gh.GitHubClient, sourceRepo repository.Repository, config *CopyConfig, scope migrator.SecretScope) ([]string, error) {
	names := config.Secrets
	if len(names) == 0 {
		var secrets []*github.Secret
		var err error
		if scope == migrator.SecretScopeOrg {
			logger.Info("No specific secrets specified, fetching org Agents secrets from source...")
			secrets, err = gh.ListAgentsOrgSecrets(ctx, client, sourceRepo)
		} else {
			logger.Info("No specific secrets specified, fetching repo Agents secrets from source...")
			secrets, err = gh.ListAgentsRepoSecrets(ctx, client, sourceRepo)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Agents secrets from source: %w", err)
		}
		names = secretNames(secrets)
	}
	return migrator.ExcludeSecrets(names, config.ExcludeSecrets), nil
}

// secretNames extracts the names of the given secrets.
func secretNames(secrets []*github.Secret) []string {
	names := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		names = append(names, secret.GetName())
	}
	return names
}
