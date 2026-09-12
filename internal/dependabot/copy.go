// Package dependabot copies Dependabot secrets by running the copy inside a
// workflow run that Dependabot itself triggers.
package dependabot

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/internal/destination"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/internal/runlog"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/actions"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/gitutil"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// pollInterval is how often the workflow runs are checked while waiting for
// Dependabot to run the copy.
const pollInterval = 20 * time.Second

// pushEvent is the workflow run event of the run Dependabot triggers when it
// pushes the branch of the update it found.
const pushEvent = "push"

// logMaxRedirects bounds the redirects followed when downloading a job log.
const logMaxRedirects = 3

// clockSkewMargin widens the window in which a workflow run is accepted as
// belonging to this copy, so a clock difference with GitHub cannot hide it.
const clockSkewMargin = 5 * time.Minute

// copyWorkflowPath is the generated workflow that copies the secrets.
const copyWorkflowPath = ".github/workflows/gh-secret-kit-dependabot-copy.yml"

// baitWorkflowPath is the workflow that only exists to hold the outdated action
// reference the Dependabot check finds an update for.
const baitWorkflowPath = ".github/workflows/gh-secret-kit-dependabot-trigger.yml"

// CopyConfig holds configuration for the Dependabot secret copy.
//
// Dependabot secret values cannot be read through the API, and they are only
// exposed to a workflow run that Dependabot triggered; an Actions run started by
// a user is given no Dependabot secret at all. The copy therefore commits a copy
// workflow to a temporary branch, makes that branch the default one, and commits
// a Dependabot configuration, which makes Dependabot check for updates
// immediately. The branch Dependabot pushes runs the copy workflow with the
// Dependabot secrets, so the values never reach the local machine. Every
// temporary change is reverted afterwards.
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
	// Branch is the temporary branch that carries the copy workflow and is made
	// the default branch for the duration of the copy. Empty means a unique
	// generated name.
	Branch string
	// Timeout bounds how long the copy waits for the Dependabot check and the
	// workflow run it triggers.
	Timeout time.Duration
	// KeepWorkflow leaves the temporary branch, the Dependabot secrets and the
	// branches Dependabot pushed in place, for debugging. The default branch is
	// always restored.
	KeepWorkflow bool
}

// RunCopy copies the Dependabot secrets available to the source repository to
// every destination, by running the copy inside a Dependabot-triggered run.
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
	sourceSlug := sourceRepo.Owner + "/" + sourceRepo.Name

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	secrets, err := collectSecrets(ctx, sourceRepo, config, scope)
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

	scriptConfig := migrator.DependabotCopyConfig{
		Scope:          scope,
		DestinationApp: app,
		Secrets:        secrets,
		Rename:         renameMap,
		Overwrite:      config.Overwrite,
		Destinations:   buildScriptDestinations(destinations, tokenSecretNames),
	}
	script, err := migrator.GenerateDependabotCopyScript(scriptConfig)
	if err != nil {
		return fmt.Errorf("failed to generate the copy script: %w", err)
	}
	workflow, err := migrator.GenerateDependabotCopyWorkflowYAML(scriptConfig, script)
	if err != nil {
		return fmt.Errorf("failed to generate the copy workflow: %w", err)
	}
	bait, err := migrator.GenerateDependabotBaitWorkflowYAML()
	if err != nil {
		return fmt.Errorf("failed to generate the trigger workflow: %w", err)
	}

	branch, err := resolveBranch(config.Branch)
	if err != nil {
		return err
	}
	dependabotConfig, err := migrator.GenerateDependabotConfigYAML(branch)
	if err != nil {
		return err
	}

	logger.Info(fmt.Sprintf("Copying %d Dependabot secrets to %d destinations", len(secrets), len(destinations)))

	// Runs created before the copy started belong to an earlier one. The margin
	// absorbs the clock difference between this machine and GitHub.
	since := time.Now().Add(-clockSkewMargin)

	// The destination tokens have to be Dependabot secrets: a
	// Dependabot-triggered run is not given Actions, Codespaces, or Agents
	// secrets.
	if err := registerTokenSecrets(ctx, sourceRepo, tokenSecretNames, hostTokens); err != nil {
		return err
	}
	if config.KeepWorkflow {
		logger.Warn(fmt.Sprintf("Keeping the temporary Dependabot secrets on %s", sourceSlug))
	} else {
		defer removeTokenSecrets(context.WithoutCancel(ctx), sourceRepo, tokenSecretNames)
	}

	// Dependabot only reads its configuration from the default branch, so the
	// workflows are committed to a temporary branch that is made the default one
	// for the duration of the copy.
	files := map[string]string{
		copyWorkflowPath: workflow,
		baitWorkflowPath: bait,
	}
	restore, err := prepareCopyBranch(ctx, client, sourceRepo, branch, files, config.KeepWorkflow)
	if err != nil {
		return err
	}
	defer restore(context.WithoutCancel(ctx))

	// The branches Dependabot pushes are deleted before the temporary branch, so
	// that the pull requests it opened can still be found through it.
	observed := &observedBranches{}
	if config.KeepWorkflow {
		logger.Warn("Keeping the branches Dependabot pushed")
	} else {
		defer observed.cleanup(context.WithoutCancel(ctx), client, sourceRepo, branch)
	}

	// Committing the Dependabot configuration to the default branch is what
	// makes Dependabot check for updates right away.
	if err := commitFile(ctx, client, sourceRepo, branch, migrator.DependabotConfigPath, dependabotConfig,
		"chore: add temporary dependabot config for gh-secret-kit secret copy"); err != nil {
		return err
	}

	if err := waitForCopy(ctx, client, sourceRepo, since, config.Timeout, observed); err != nil {
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

// buildScriptDestinations converts the resolved destinations into the form
// expected by the script generator.
func buildScriptDestinations(destinations []*destination.Destination, tokenSecretNames map[string]string) []migrator.DependabotCopyDestination {
	result := make([]migrator.DependabotCopyDestination, 0, len(destinations))
	for _, dest := range destinations {
		result = append(result, migrator.DependabotCopyDestination{
			Target:   dest.Target,
			Host:     dest.Host,
			TokenEnv: tokenSecretNames[dest.Host],
		})
	}
	return result
}

// registerTokenSecrets stores the destination tokens as temporary Dependabot
// secrets on the source repository. An existing secret with the same name is not
// touched, because it would be deleted during the cleanup.
func registerTokenSecrets(ctx context.Context, repo repository.Repository, tokenSecretNames, hostTokens map[string]string) error {
	existing, err := listRepoSecretNames(ctx, repo)
	if err != nil {
		return fmt.Errorf("failed to list the Dependabot secrets of the source repository: %w", err)
	}
	present := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		present[name] = struct{}{}
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
		if err := setRepoSecret(ctx, repo, name, hostTokens[host]); err != nil {
			removeTokenSecrets(context.WithoutCancel(ctx), repo, created)
			return fmt.Errorf("failed to register the temporary Dependabot secret %s: %w", name, err)
		}
		created[host] = name
	}
	return nil
}

// removeTokenSecrets deletes the temporary Dependabot secrets, logging instead
// of failing so that the remaining cleanup still runs.
func removeTokenSecrets(ctx context.Context, repo repository.Repository, tokenSecretNames map[string]string) {
	for _, name := range tokenSecretNames {
		logger.Info(fmt.Sprintf("Deleting the temporary Dependabot secret %s...", name))
		if err := deleteRepoSecret(ctx, repo, name); err != nil {
			logger.Warn(fmt.Sprintf("failed to delete the temporary Dependabot secret %s: %v", name, err))
		}
	}
}

// prepareCopyBranch creates a temporary branch carrying the given files and
// makes it the default branch, because Dependabot only reads its configuration
// and opens pull requests against the default branch. The returned function
// always restores the original default branch, and removes the temporary branch
// unless keepBranch is set.
func prepareCopyBranch(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch string, files map[string]string, keepBranch bool) (cleanup func(context.Context), err error) {
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

	// The workflows have to be in place before the default branch is switched,
	// so that the branches Dependabot pushes already carry the copy workflow.
	for _, path := range sortedKeys(files) {
		if err = commitFile(ctx, client, repo, branch, path, files[path],
			"chore: add temporary workflow for gh-secret-kit secret copy"); err != nil {
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

// commitFile creates or updates a file on the given branch.
func commitFile(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch, path, content, message string) error {
	logger.Info(fmt.Sprintf("Committing %s to %s...", path, branch))
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

// sortedKeys returns the keys of files in a stable order, so the commits are
// made in the same order on every run.
func sortedKeys(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// observedBranches records the branches Dependabot pushed, so they can be
// deleted once the copy is done. Deleting them also closes the pull requests
// Dependabot opened for them.
type observedBranches struct {
	names []string
}

// add records a branch once, ignoring branches that are not Dependabot ones so
// that the cleanup can never delete an unrelated branch.
func (o *observedBranches) add(branch string) {
	if !strings.HasPrefix(branch, migrator.DependabotBranchPrefix) {
		return
	}
	for _, name := range o.names {
		if name == branch {
			return
		}
	}
	o.names = append(o.names, branch)
}

// cleanup deletes the recorded branches, logging instead of failing so that the
// remaining cleanup still runs. The pull requests Dependabot opened against the
// temporary branch are inspected as well, because a pull request may have been
// opened without the copy workflow ever being observed.
func (o *observedBranches) cleanup(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, baseBranch string) {
	prs, err := gh.ListPullRequests(ctx, client, repo, &gh.ListPullRequestsOptionBase{Base: baseBranch}, gh.ListPullRequestsOptionStateAll())
	if err != nil {
		logger.Warn(fmt.Sprintf("failed to list the pull requests opened against %s: %v", baseBranch, err))
	}
	for _, pr := range prs {
		o.add(pr.GetHead().GetRef())
	}
	for _, name := range o.names {
		logger.Info(fmt.Sprintf("Deleting the branch %s Dependabot pushed...", name))
		if err := gh.DeleteBranchIfExists(ctx, client, repo, name); err != nil {
			logger.Warn(fmt.Sprintf("failed to delete branch %s: %v", name, err))
		}
	}
}

// waitForCopy waits until the copy script reports that it finished in a run
// Dependabot triggered, or until the timeout expires. The Dependabot check is
// scheduled by GitHub, so the run does not appear immediately.
func waitForCopy(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, since time.Time, timeout time.Duration, observed *observedBranches) error {
	logger.Info("Waiting for Dependabot to check for updates and run the copy...")
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastLog string
	for {
		runs, err := copyWorkflowRuns(ctx, client, repo, since)
		if err != nil {
			logger.Debug(fmt.Sprintf("failed to list the workflow runs: %v", err))
		}
		for _, run := range runs {
			observed.add(run.GetHeadBranch())
			log, lerr := runJobLogs(ctx, client, repo, run.GetID())
			if lerr != nil {
				// The log is not served until the run has started.
				logger.Debug(fmt.Sprintf("the log of run %d is not available yet: %v", run.GetID(), lerr))
				continue
			}
			lastLog = log
			if runlog.Contains(log, migrator.DependabotCopyDoneMarker) {
				runlog.ReportCopyResults(log)
				return nil
			}
			if run.GetStatus() == "completed" && run.GetConclusion() != "success" {
				return fmt.Errorf("the copy workflow run %d finished with conclusion %q before the copy completed%s",
					run.GetID(), run.GetConclusion(), runlog.FailureDetail(log))
			}
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %s waiting for Dependabot to run the copy%s", timeout, runlog.FailureDetail(lastLog))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// copyWorkflowRuns returns the runs of the generated copy workflow that
// Dependabot triggered after the copy started.
func copyWorkflowRuns(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, since time.Time) ([]*github.WorkflowRun, error) {
	runs, err := gh.ListRepositoryWorkflowRuns(ctx, client, repo, &gh.ListWorkflowRunsOptions{
		Actor: migrator.DependabotActor,
		Event: pushEvent,
	})
	if err != nil {
		return nil, err
	}
	matched := make([]*github.WorkflowRun, 0, len(runs))
	for _, run := range runs {
		if run.GetPath() != copyWorkflowPath {
			continue
		}
		if !strings.HasPrefix(run.GetHeadBranch(), migrator.DependabotBranchPrefix) {
			continue
		}
		// Runs older than the start of this copy belong to an earlier one and
		// must not be mistaken for this one.
		if run.GetCreatedAt().Before(since) {
			continue
		}
		matched = append(matched, run)
	}
	return matched, nil
}

// runJobLogs downloads the GitHub Actions job logs of a workflow run.
func runJobLogs(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, runID int64) (string, error) {
	jobs, err := gh.ListWorkflowJobs(ctx, client, repo, runID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to list the jobs of workflow run %d: %w", runID, err)
	}
	if len(jobs) == 0 {
		return "", fmt.Errorf("workflow run %d has no job yet", runID)
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

// collectSecrets resolves the Dependabot secret names to copy and applies the
// include and exclude filters.
func collectSecrets(ctx context.Context, sourceRepo repository.Repository, config *CopyConfig, scope migrator.SecretScope) ([]string, error) {
	names := config.Secrets
	if len(names) == 0 {
		var err error
		if scope == migrator.SecretScopeOrg {
			logger.Info("No specific secrets specified, fetching org Dependabot secrets from source...")
			names, err = listOrgSecretNames(ctx, sourceRepo)
		} else {
			logger.Info("No specific secrets specified, fetching repo Dependabot secrets from source...")
			names, err = listRepoSecretNames(ctx, sourceRepo)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Dependabot secrets from source: %w", err)
		}
	}
	return migrator.ExcludeSecrets(names, config.ExcludeSecrets), nil
}
