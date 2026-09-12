// Package dependabot copies GitHub Dependabot secrets by running the copy
// inside a GitHub Actions workflow run triggered by Dependabot.
package dependabot

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

// pollInterval is how often the Dependabot pull request and the copy workflow
// run are checked while waiting for the copy to finish.
const pollInterval = 15 * time.Second

// logMaxRedirects bounds the redirects followed when downloading a job log.
const logMaxRedirects = 3

// CopyConfig holds configuration for the Dependabot secret copy.
//
// Dependabot secret values cannot be read through the API; they are only
// exposed to Dependabot itself and to GitHub Actions workflow runs that
// Dependabot triggers. The copy therefore commits a copy workflow, an outdated
// action reference, and a Dependabot configuration to a temporary branch, makes
// that branch the default one so Dependabot picks the configuration up,
// registers the destination tokens as temporary Dependabot secrets, and lets
// the workflow run of the pull request Dependabot opens perform the copy, so
// the values never reach the local machine. Every temporary change is reverted
// afterwards.
type CopyConfig struct {
	Source string
	// Destinations are [HOST/]OWNER/REPO references, or [HOST/]ORG when Scope
	// is SecretScopeOrg.
	Destinations   []string
	Secrets        []string
	ExcludeSecrets []string
	Rename         []string
	Overwrite      bool
	// Scope selects both the source Dependabot secret level and the destination
	// level. Only repo and org are supported.
	Scope migrator.SecretScope
	// DestinationApp selects which secret store the destination secrets are
	// written to. Empty means migrator.SecretAppDependabot.
	DestinationApp migrator.SecretApp
	// DestinationToken overrides the token resolved from the local gh
	// authentication for the destination host. It cannot be used when the
	// destinations span multiple hosts.
	DestinationToken string
	// TokenSecretName is the base name of the temporary Dependabot secret that
	// holds the destination token. The destination host is appended.
	TokenSecretName string
	// Branch is the temporary branch that carries the generated files and is
	// made the default branch for the duration of the copy. Empty means a
	// unique generated name.
	Branch string
	// WorkflowName is the file name (without extension) of the generated copy
	// workflow.
	WorkflowName string
	// RunnerLabel is the runs-on value of the generated copy workflow.
	RunnerLabel string
	// Timeout bounds how long the copy waits for Dependabot to open the pull
	// request and for the copy workflow run to finish.
	Timeout time.Duration
	// KeepWorkflow leaves the temporary branch, Dependabot secrets, pull
	// requests, and run history in place, for debugging. The default branch is
	// always restored.
	KeepWorkflow bool
}

// RunCopy copies the Dependabot secrets available to the source repository to
// every destination, by running the copy inside a Dependabot-triggered GitHub
// Actions workflow run.
func RunCopy(ctx context.Context, config *CopyConfig) error {
	logger.Info("Copying Dependabot secrets")

	scope := config.Scope
	if scope == "" {
		scope = migrator.SecretScopeRepo
	}
	if scope != migrator.SecretScopeRepo && scope != migrator.SecretScopeOrg {
		return fmt.Errorf("unsupported --scope %q for Dependabot secrets: expected repo or org", scope)
	}

	app := config.DestinationApp
	if app == "" {
		app = migrator.SecretAppDependabot
	}
	if err := migrator.ValidateSecretApp(app); err != nil {
		return err
	}

	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}
	if sourceRepo.Name == "" {
		return fmt.Errorf("source must be in [HOST/]OWNER/REPO format")
	}

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	secrets, err := collectSecrets(ctx, client, sourceRepo, config, scope)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		logger.Info("No Dependabot secrets found to copy, skipping")
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

	branch, err := resolveBranch(config.Branch)
	if err != nil {
		return err
	}

	workflowConfig := migrator.CopyWorkflowConfig{
		WorkflowName:   config.WorkflowName,
		RunsOn:         config.RunnerLabel,
		Scope:          scope,
		DestinationApp: app,
		Secrets:        secrets,
		Rename:         renameMap,
		Overwrite:      config.Overwrite,
		Destinations:   buildWorkflowDestinations(destinations, tokenSecretNames),
	}
	logger.Info("Generating copy workflow YAML...")
	copyWorkflow, err := migrator.GenerateDependabotCopyWorkflowYAML(workflowConfig, branch)
	if err != nil {
		return fmt.Errorf("failed to generate the copy workflow YAML: %w", err)
	}
	updateTarget, err := migrator.GenerateDependabotUpdateTargetYAML(config.WorkflowName + migrator.DependabotUpdateTargetSuffix)
	if err != nil {
		return fmt.Errorf("failed to generate the update target workflow YAML: %w", err)
	}

	logger.Info(fmt.Sprintf("Copying %d Dependabot secrets to %d destinations", len(secrets), len(destinations)))

	// The destination tokens have to be Dependabot secrets: a workflow run
	// triggered by Dependabot is not given Actions secrets.
	if err := registerTokenSecrets(ctx, client, sourceRepo, tokenSecretNames, hostTokens); err != nil {
		return err
	}
	if config.KeepWorkflow {
		logger.Warn(fmt.Sprintf("Keeping the temporary Dependabot secrets on %s/%s", sourceRepo.Owner, sourceRepo.Name))
	} else {
		defer removeTokenSecrets(context.WithoutCancel(ctx), client, sourceRepo, tokenSecretNames)
	}

	workflowFile := config.WorkflowName + ".yml"
	files := []fileContent{
		{
			path:    ".github/workflows/" + workflowFile,
			message: "chore: add temporary copy workflow for gh-secret-kit secret copy",
			content: copyWorkflow,
		},
		{
			path:    ".github/workflows/" + config.WorkflowName + migrator.DependabotUpdateTargetSuffix + ".yml",
			message: "chore: add temporary Dependabot update target for gh-secret-kit secret copy",
			content: updateTarget,
		},
	}

	// Dependabot only reads its configuration from the default branch, so the
	// temporary branch that carries the generated files is made the default one
	// for the duration of the copy.
	restore, err := prepareCopyBranch(ctx, client, sourceRepo, branch, files, config.KeepWorkflow)
	if err != nil {
		return err
	}
	defer restore(context.WithoutCancel(ctx))
	defer cleanupDependabotArtifacts(context.WithoutCancel(ctx), client, sourceRepo, workflowFile, branch, config.KeepWorkflow)

	// Committing the Dependabot configuration to the now-default temporary
	// branch triggers an immediate update check.
	logger.Info(fmt.Sprintf("Committing %s to %s...", migrator.DependabotConfigPath, branch))
	if err := commitFile(ctx, client, sourceRepo, migrator.DependabotConfigPath, branch,
		"chore: add temporary Dependabot configuration for gh-secret-kit secret copy",
		migrator.GenerateDependabotConfigYAML()); err != nil {
		return err
	}

	if err := waitForCopy(ctx, client, sourceRepo, branch, workflowFile, config.Timeout); err != nil {
		return err
	}

	logger.Info("Dependabot secrets copied successfully")
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
	branch = types.DefaultDependabotCopyBranch
	if runID := actions.GetRunID(); runID != "" {
		return branch + "-" + runID, nil
	}
	return branch + "-" + strconv.FormatInt(time.Now().UnixNano(), 10), nil
}

// collectSecrets resolves the Dependabot secret names to copy and applies the
// include and exclude filters.
func collectSecrets(ctx context.Context, client *gh.GitHubClient, sourceRepo repository.Repository, config *CopyConfig, scope migrator.SecretScope) ([]string, error) {
	names := config.Secrets
	if len(names) == 0 {
		var secrets []*github.Secret
		var err error
		if scope == migrator.SecretScopeOrg {
			logger.Info("No specific secrets specified, fetching org Dependabot secrets from source...")
			secrets, err = listDependabotOrgSecrets(ctx, client, sourceRepo)
		} else {
			logger.Info("No specific secrets specified, fetching repo Dependabot secrets from source...")
			secrets, err = listDependabotRepoSecrets(ctx, client, sourceRepo)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Dependabot secrets from source: %w", err)
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

// buildWorkflowDestinations converts the resolved destinations into the form
// expected by the workflow generator.
func buildWorkflowDestinations(destinations []*destination.Destination, tokenSecretNames map[string]string) []migrator.CopyDestination {
	result := make([]migrator.CopyDestination, 0, len(destinations))
	for _, dest := range destinations {
		result = append(result, migrator.CopyDestination{
			Target:      dest.Target,
			Host:        dest.Host,
			TokenSecret: tokenSecretNames[dest.Host],
		})
	}
	return result
}

// registerTokenSecrets stores the destination tokens as temporary Dependabot
// secrets on the source repository. An existing secret with the same name is
// not touched, because it would be deleted during the cleanup.
func registerTokenSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, tokenSecretNames, hostTokens map[string]string) error {
	existing, err := listDependabotRepoSecrets(ctx, client, repo)
	if err != nil {
		return fmt.Errorf("failed to list the Dependabot secrets of the source repository: %w", err)
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
			return fmt.Errorf("the source repository already has a Dependabot secret named %q: use --token-secret-name to pick another name", name)
		}
	}

	// Track the secrets created so far so they can be removed if a later
	// registration fails, otherwise a partial failure would leak live
	// destination tokens.
	created := make(map[string]string, len(tokenSecretNames))
	for host, name := range tokenSecretNames {
		logger.Info(fmt.Sprintf("Registering the temporary Dependabot secret %s...", name))
		if err := setDependabotRepoSecret(ctx, client, repo, name, hostTokens[host]); err != nil {
			removeTokenSecrets(context.WithoutCancel(ctx), client, repo, created)
			return fmt.Errorf("failed to register the temporary Dependabot secret %s: %w", name, err)
		}
		created[host] = name
	}
	return nil
}

// removeTokenSecrets deletes the temporary Dependabot secrets, logging instead
// of failing so that the remaining cleanup still runs.
func removeTokenSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, tokenSecretNames map[string]string) {
	for _, name := range tokenSecretNames {
		logger.Info(fmt.Sprintf("Deleting the temporary Dependabot secret %s...", name))
		if err := deleteDependabotRepoSecret(ctx, client, repo, name); err != nil {
			logger.Warn(fmt.Sprintf("failed to delete the temporary Dependabot secret %s: %v", name, err))
		}
	}
}

// fileContent is a file committed to the temporary branch.
type fileContent struct {
	path    string
	message string
	content string
}

// prepareCopyBranch creates a temporary branch carrying the given files and
// makes it the default branch, because Dependabot only reads its configuration
// from the default branch. The returned function always restores the original
// default branch, and removes the temporary branch unless keepBranch is set.
// Deleting the temporary branch also closes the pull requests Dependabot opens
// against it.
func prepareCopyBranch(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch string, files []fileContent, keepBranch bool) (cleanup func(context.Context), err error) {
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

	for _, file := range files {
		logger.Info(fmt.Sprintf("Committing %s to %s...", file.path, branch))
		if cerr := commitFile(ctx, client, repo, file.path, branch, file.message, file.content); cerr != nil {
			// Use a naked return so the deferred cleanup still sees the installed
			// cleanup closure; "return nil, err" would clear the named cleanup
			// result before the defer runs and panic on the nil call.
			err = cerr
			return
		}
	}

	logger.Info(fmt.Sprintf("Temporarily switching the default branch to %s...", branch))
	if _, err = gh.EditRepository(ctx, client, repo, &github.Repository{DefaultBranch: github.Ptr(branch)}); err != nil {
		err = fmt.Errorf("failed to switch the default branch to %s: %w", branch, err)
		return
	}
	switched = true

	return cleanup, nil
}

// commitFile creates the file on the branch, or updates it when it already
// exists there (for example an inherited Dependabot configuration).
func commitFile(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, path, branch, message, content string) error {
	opts := &gh.RepositoryContentFileOptions{
		Message: message,
		Content: []byte(content),
		Branch:  github.Ptr(branch),
	}
	existing, gerr := gh.GetRepositoryFileContent(ctx, client, repo, path, github.Ptr(branch))
	if gerr != nil && !gh.IsHTTPNotFound(gerr) {
		return fmt.Errorf("failed to check %s on %s: %w", path, branch, gerr)
	}
	var err error
	if gerr == nil && existing != nil {
		opts.SHA = github.Ptr(existing.GetSHA())
		_, err = gh.UpdateRepositoryFile(ctx, client, repo, path, opts)
	} else {
		_, err = gh.CreateRepositoryFile(ctx, client, repo, path, opts)
	}
	if err != nil {
		return fmt.Errorf("failed to commit %s to %s: %w", path, branch, err)
	}
	return nil
}

// cleanupDependabotArtifacts deletes the update branches of the pull requests
// Dependabot opened against the temporary branch and removes the copy workflow
// run history. Failures are only logged so the remaining cleanup still runs.
func cleanupDependabotArtifacts(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, workflowFile, branch string, keep bool) {
	if keep {
		logger.Warn("Keeping the Dependabot pull requests and the copy workflow run history")
		return
	}

	prs, err := gh.ListPullRequests(ctx, client, repo,
		&gh.ListPullRequestsOptionBase{Base: branch},
		gh.ListPullRequestsOptionStateOpen())
	if err != nil {
		logger.Warn(fmt.Sprintf("failed to list the Dependabot pull requests: %v", err))
	} else {
		for _, pr := range prs {
			if !strings.EqualFold(pr.GetUser().GetLogin(), migrator.DependabotActor) {
				continue
			}
			head := pr.GetHead().GetRef()
			logger.Info(fmt.Sprintf("Deleting Dependabot update branch %s...", head))
			if derr := gh.DeleteBranchIfExists(ctx, client, repo, head); derr != nil {
				logger.Warn(fmt.Sprintf("failed to delete Dependabot update branch %s: %v", head, derr))
			}
		}
	}

	runs, err := gh.ListWorkflowRunsByFileName(ctx, client, repo, workflowFile, nil)
	if err != nil {
		if !gh.IsHTTPNotFound(err) {
			logger.Warn(fmt.Sprintf("failed to list the copy workflow runs: %v", err))
		}
		return
	}
	for _, run := range runs {
		logger.Info(fmt.Sprintf("Deleting copy workflow run ID %d history...", run.GetID()))
		if derr := gh.DeleteWorkflowRun(ctx, client, repo, run.GetID()); derr != nil {
			logger.Warn(fmt.Sprintf("failed to delete copy workflow run ID %d: %v", run.GetID(), derr))
		}
	}
}

// waitForCopy waits until a Dependabot-triggered run of the copy workflow
// completes, or the timeout expires. Dependabot has no API that triggers an
// update check, so the pull request the check opens is simply polled for.
func waitForCopy(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch, workflowFile string, timeout time.Duration) error {
	logger.Info("Waiting for Dependabot to open the pull request that runs the copy...")
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	prReported := false
	for {
		if !prReported {
			if pr := findDependabotPullRequest(ctx, client, repo, branch); pr != nil {
				logger.Info(fmt.Sprintf("Dependabot opened pull request #%d: %s", pr.GetNumber(), pr.GetHTMLURL()))
				prReported = true
			}
		}

		run, rerr := findCopyRun(ctx, client, repo, workflowFile)
		if rerr != nil {
			logger.Debug(fmt.Sprintf("the copy workflow run is not available yet: %v", rerr))
		} else if run != nil && run.GetStatus() == "completed" {
			log, lerr := copyRunLog(ctx, client, repo, run.GetID())
			if lerr != nil {
				logger.Debug(fmt.Sprintf("the copy workflow run log is not available: %v", lerr))
			}
			if run.GetConclusion() == "success" {
				logCopyResults(log)
				return nil
			}
			return fmt.Errorf("the copy workflow run %d finished with conclusion %q%s", run.GetID(), run.GetConclusion(), copyFailureDetail(log))
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for the Dependabot-triggered copy workflow run", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// findDependabotPullRequest returns an open pull request Dependabot opened
// against the temporary branch, or nil while none exists.
func findDependabotPullRequest(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch string) *github.PullRequest {
	prs, err := gh.ListPullRequests(ctx, client, repo,
		&gh.ListPullRequestsOptionBase{Base: branch},
		gh.ListPullRequestsOptionStateOpen())
	if err != nil {
		logger.Debug(fmt.Sprintf("failed to list the Dependabot pull requests: %v", err))
		return nil
	}
	for _, pr := range prs {
		if strings.EqualFold(pr.GetUser().GetLogin(), migrator.DependabotActor) {
			return pr
		}
	}
	return nil
}

// findCopyRun returns the most recent Dependabot-triggered run of the copy
// workflow, or nil while none exists.
func findCopyRun(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, workflowFile string) (*github.WorkflowRun, error) {
	runs, err := gh.ListWorkflowRunsByFileName(ctx, client, repo, workflowFile, &gh.ListWorkflowRunsOptions{
		Actor: migrator.DependabotActor,
		Event: "pull_request",
	})
	if err != nil {
		// The workflow is not registered until Dependabot opens the pull request.
		if gh.IsHTTPNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	var latest *github.WorkflowRun
	for _, run := range runs {
		if latest == nil || run.GetCreatedAt().After(latest.GetCreatedAt().Time) {
			latest = run
		}
	}
	return latest, nil
}

// copyRunLog downloads the job logs of the copy workflow run.
func copyRunLog(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, runID int64) (string, error) {
	jobs, err := gh.ListWorkflowJobs(ctx, client, repo, runID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to list the jobs of workflow run %d: %w", runID, err)
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

// logCopyResults reports the progress lines the copy steps printed.
func logCopyResults(log string) {
	for _, line := range logLines(log) {
		if strings.HasPrefix(line, "Successfully migrated secret:") || strings.HasPrefix(line, "Secret ") {
			logger.Info(line)
		}
	}
}

// copyFailureDetail collects the errors GitHub Actions reported, so that a
// failure of the copy steps is visible without opening the run.
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
