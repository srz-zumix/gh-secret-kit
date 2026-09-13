// Package dependabot copies Dependabot secrets inside a workflow run triggered
// by a Dependabot push, without exposing the values to the local machine.
package dependabot

import (
	"context"
	"errors"
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
	"gopkg.in/yaml.v3"
)

const (
	pollInterval        = 20 * time.Second
	cleanupTimeout      = 2 * time.Minute
	cancelPollInterval  = 2 * time.Second
	forceCancelAfter    = 15 * time.Second
	completedLogRetries = 3
	logMaxRedirects     = 3
	pushEvent           = "push"
	baitWorkflowPath    = ".github/workflows/gh-secret-kit-dependabot-trigger.yml"
)

// CopyConfig configures the temporary default branch and Dependabot-triggered
// workflow used to copy secrets. The original default branch is always restored.
type CopyConfig struct {
	Source         string
	Destinations   []string
	Secrets        []string
	ExcludeSecrets []string
	Rename         []string
	Overwrite      bool
	// Scope supports repository or organization secrets at both ends.
	Scope migrator.SecretScope
	// DestinationApp defaults to Dependabot.
	DestinationApp migrator.SecretApp
	// DestinationToken overrides local authentication for a single host.
	DestinationToken string
	// TokenSecretName is suffixed with each destination host.
	TokenSecretName string
	// Branch defaults to a unique temporary branch name.
	Branch string
	// WorkflowName is the generated workflow filename without its extension.
	WorkflowName string
	// RunnerLabel is a literal runs-on label.
	RunnerLabel string
	Timeout     time.Duration
	// KeepWorkflow preserves branches, temporary secrets and workflow history,
	// but does not prevent cancellation of unfinished copy runs.
	KeepWorkflow bool
}

// RunCopy registers temporary Dependabot credentials, prepares the copy and bait
// workflows, and triggers an update check by committing Dependabot configuration.
func RunCopy(ctx context.Context, config *CopyConfig) (result error) {
	if config == nil {
		return fmt.Errorf("dependabot copy configuration is required")
	}
	options := *config
	if err := applyDefaults(&options); err != nil {
		return err
	}
	config = &options
	branch, err := resolveBranch(config.Branch)
	if err != nil {
		return err
	}
	workflowPath := ".github/workflows/" + config.WorkflowName + ".yml"
	if workflowPath == baitWorkflowPath {
		return fmt.Errorf("--workflow-name %q is reserved for the Dependabot trigger workflow", config.WorkflowName)
	}
	// Unlike a timestamp window or branch, this remains unique when a caller
	// reuses --branch or runs several copies with the same workflow filename.
	runName := "gh-secret-kit-dependabot-copy-" + strconv.FormatInt(time.Now().UnixNano(), 10)

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
	secrets, err := collectSecrets(ctx, client, sourceRepo, config)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		logger.Info("No Dependabot secrets found to copy, skipping")
		return nil
	}

	orgLevel := config.Scope == migrator.SecretScopeOrg
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
		Scope:          config.Scope,
		DestinationApp: config.DestinationApp,
		Secrets:        secrets,
		Rename:         renameMap,
		Overwrite:      config.Overwrite,
		Destinations:   buildScriptDestinations(destinations, tokenSecretNames),
	}
	script, err := migrator.GenerateDependabotCopyScript(scriptConfig)
	if err != nil {
		return fmt.Errorf("failed to generate the copy script: %w", err)
	}
	workflow, err := migrator.GenerateDependabotCopyWorkflowYAML(scriptConfig, script, config.WorkflowName, config.RunnerLabel, runName)
	if err != nil {
		return fmt.Errorf("failed to generate the copy workflow: %w", err)
	}
	bait, err := migrator.GenerateDependabotBaitWorkflowYAML()
	if err != nil {
		return fmt.Errorf("failed to generate the trigger workflow: %w", err)
	}
	dependabotConfig, err := migrator.GenerateDependabotConfigYAML(branch)
	if err != nil {
		return err
	}

	logger.Info(fmt.Sprintf("Copying %d Dependabot secrets to %d destinations", len(secrets), len(destinations)))
	if err := registerTokenSecrets(ctx, client, sourceRepo, tokenSecretNames, hostTokens); err != nil {
		return err
	}
	safeToRemove := true
	defer func() {
		result = errors.Join(result, cleanupTokenSecrets(ctx, client, sourceRepo, tokenSecretNames, config.KeepWorkflow))
	}()

	files := map[string]string{workflowPath: workflow, baitWorkflowPath: bait}
	restore, err := prepareCopyBranch(ctx, client, sourceRepo, branch, files, config.KeepWorkflow)
	if err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		result = errors.Join(result, restore(cleanupCtx, config.KeepWorkflow || !safeToRemove))
	}()
	observed := &observedCopy{workflowPath: workflowPath, runName: runName}
	// LIFO: stop runs, delete histories and update branches, restore the
	// default branch, remove the temporary branch, then remove token secrets.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		stopped, cleanupErr := observed.cleanup(cleanupCtx, client, sourceRepo, branch, config.KeepWorkflow)
		safeToRemove = stopped
		result = errors.Join(result, cleanupErr)
	}()

	if err := commitFile(ctx, client, sourceRepo, branch, migrator.DependabotConfigPath, dependabotConfig,
		"chore: add temporary dependabot config for gh-secret-kit secret copy"); err != nil {
		return err
	}
	if err := waitForCopy(ctx, client, sourceRepo, config.Timeout, observed); err != nil {
		return err
	}
	logger.Info("Dependabot secrets copied successfully")
	return nil
}

func applyDefaults(config *CopyConfig) error {
	if config.Scope == "" {
		config.Scope = migrator.SecretScopeRepo
	}
	if config.Scope != migrator.SecretScopeRepo && config.Scope != migrator.SecretScopeOrg {
		return fmt.Errorf("unsupported --scope %q for Dependabot secrets: expected repo or org", config.Scope)
	}
	if config.DestinationApp == "" {
		config.DestinationApp = migrator.SecretAppDependabot
	}
	if err := migrator.ValidateSecretApp(config.DestinationApp); err != nil {
		return err
	}
	if config.WorkflowName == "" {
		config.WorkflowName = types.DefaultDependabotCopyWorkflowName
	}
	if config.RunnerLabel == "" {
		config.RunnerLabel = types.DefaultCopyRunnerLabel
	}
	if err := migrator.ValidateDependabotWorkflowOptions(config.WorkflowName, config.RunnerLabel); err != nil {
		return err
	}
	if config.TokenSecretName == "" {
		config.TokenSecretName = types.DefaultDependabotCopyTokenSecretName
	}
	if config.Timeout == 0 {
		var err error
		config.Timeout, err = time.ParseDuration(types.DefaultDependabotCopyTimeout)
		if err != nil {
			return fmt.Errorf("invalid default Dependabot copy timeout: %w", err)
		}
	}
	if config.Timeout < 0 {
		return fmt.Errorf("dependabot copy timeout must be positive")
	}
	return nil
}

func resolveBranch(branch string) (string, error) {
	if branch != "" {
		if err := gitutil.ValidateBranchName(branch); err != nil {
			return "", err
		}
		return branch, nil
	}
	branch = types.DefaultDependabotCopyBranch
	if runID := actions.GetRunID(); runID != "" {
		branch += "-" + runID
	}
	return branch + "-" + strconv.FormatInt(time.Now().UnixNano(), 10), nil
}

func buildScriptDestinations(destinations []*destination.Destination, tokenSecretNames map[string]string) []migrator.DependabotCopyDestination {
	result := make([]migrator.DependabotCopyDestination, 0, len(destinations))
	for _, dest := range destinations {
		result = append(result, migrator.DependabotCopyDestination{
			Target: dest.Target, Host: dest.Host, TokenEnv: tokenSecretNames[dest.Host],
		})
	}
	return result
}

func collectSecrets(ctx context.Context, client *gh.GitHubClient, sourceRepo repository.Repository, config *CopyConfig) ([]string, error) {
	names := config.Secrets
	if len(names) == 0 {
		var secrets []*github.Secret
		var err error
		if config.Scope == migrator.SecretScopeOrg {
			secrets, err = listDependabotOrgSecrets(ctx, client, sourceRepo)
		} else {
			secrets, err = listDependabotRepoSecrets(ctx, client, sourceRepo)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Dependabot secrets from source: %w", err)
		}
		for _, secret := range secrets {
			names = append(names, secret.GetName())
		}
	}
	return migrator.ExcludeSecrets(names, config.ExcludeSecrets), nil
}

// prepareCopyBranch installs workflows before switching the default branch.
// Its cleanup always restores the default, even when artifacts are preserved.
func prepareCopyBranch(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch string, files map[string]string, keep bool) (cleanup func(context.Context, bool) error, err error) {
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
	if _, err := gh.CreateBranch(ctx, client, repo, branch, baseBranch.GetCommit().GetSHA()); err != nil {
		return nil, fmt.Errorf("failed to create temporary branch %s: %w", branch, err)
	}
	switched := false
	cleanup = func(cleanupCtx context.Context, preserve bool) error {
		if switched {
			logger.Info(fmt.Sprintf("Restoring the default branch to %s...", originalDefault))
			if _, err := gh.EditRepository(cleanupCtx, client, repo, &github.Repository{DefaultBranch: github.Ptr(originalDefault)}); err != nil {
				return fmt.Errorf("failed to restore the default branch to %s; preserving branch %s: %w", originalDefault, branch, err)
			}
		}
		if preserve {
			logger.Warn(fmt.Sprintf("Keeping temporary branch %s", branch))
			return nil
		}
		if err := gh.DeleteBranchIfExists(cleanupCtx, client, repo, branch); err != nil {
			return fmt.Errorf("failed to delete temporary branch %s: %w", branch, err)
		}
		return nil
	}
	defer func() {
		if err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
			defer cancel()
			err = errors.Join(err, cleanup(cleanupCtx, keep))
			cleanup = nil
		}
	}()
	for _, path := range sortedKeys(files) {
		if err = commitFile(ctx, client, repo, branch, path, files[path],
			"chore: add temporary workflow for gh-secret-kit secret copy"); err != nil {
			return
		}
	}
	// Restore even if a cancelled HTTP response obscures a successful switch.
	switched = true
	if _, err = gh.EditRepository(ctx, client, repo, &github.Repository{DefaultBranch: github.Ptr(branch)}); err != nil {
		err = fmt.Errorf("failed to switch the default branch to %s: %w", branch, err)
	}
	return
}

func commitFile(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch, path, content, message string) error {
	logger.Info(fmt.Sprintf("Committing %s to %s...", path, branch))
	opts := &gh.RepositoryContentFileOptions{Message: message, Content: []byte(content), Branch: github.Ptr(branch)}
	existing, err := gh.GetRepositoryFileContent(ctx, client, repo, path, github.Ptr(branch))
	if err != nil && !gh.IsHTTPNotFound(err) {
		return fmt.Errorf("failed to check %s on %s: %w", path, branch, err)
	}
	if existing != nil {
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

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// observedCopy binds polling and cleanup to one generated run-name, rather than
// a workflow filename or a reusable branch shared with earlier invocations.
type observedCopy struct {
	workflowPath string
	runName      string
	runs         map[int64]*github.WorkflowRun
	branches     map[string]bool
}

func (o *observedCopy) owns(run *github.WorkflowRun) bool {
	return run != nil && o.runName != "" &&
		run.GetDisplayTitle() == o.runName &&
		run.GetPath() == o.workflowPath &&
		strings.EqualFold(run.GetActor().GetLogin(), migrator.DependabotActor) &&
		run.GetEvent() == pushEvent &&
		strings.HasPrefix(run.GetHeadBranch(), migrator.DependabotBranchPrefix)
}

func (o *observedCopy) observe(run *github.WorkflowRun) {
	if !o.owns(run) {
		return
	}
	if o.runs == nil {
		o.runs = make(map[int64]*github.WorkflowRun)
	}
	o.runs[run.GetID()] = run
	o.addBranch(run.GetHeadBranch())
}

func (o *observedCopy) addBranch(branch string) {
	if !strings.HasPrefix(branch, migrator.DependabotBranchPrefix) {
		return
	}
	if o.branches == nil {
		o.branches = make(map[string]bool)
	}
	o.branches[branch] = true
}

func (o *observedCopy) workflowRuns(ctx context.Context, client *gh.GitHubClient, repo repository.Repository) ([]*github.WorkflowRun, error) {
	runs, err := gh.ListRepositoryWorkflowRuns(ctx, client, repo, &gh.ListWorkflowRunsOptions{
		Actor: migrator.DependabotActor, Event: pushEvent,
	})
	if err != nil {
		return nil, err
	}
	var matched []*github.WorkflowRun
	for _, run := range runs {
		if o.owns(run) {
			o.observe(run)
			matched = append(matched, run)
		}
	}
	return matched, nil
}

// copyResult never accepts a marker from an unfinished or failed workflow.
// A nil error with done=false means its completed job logs should be retried.
func copyResult(run *github.WorkflowRun, log string, logErr error, attempts int) (done bool, err error) {
	if run.GetStatus() != "completed" {
		return false, nil
	}
	if run.GetConclusion() != "success" {
		return true, fmt.Errorf("the copy workflow run %d finished with conclusion %q%s",
			run.GetID(), run.GetConclusion(), runlog.FailureDetail(log))
	}
	if logErr == nil && runlog.Contains(log, migrator.DependabotCopyDoneMarker) {
		return true, nil
	}
	if attempts < completedLogRetries {
		return false, nil
	}
	if logErr != nil {
		return true, fmt.Errorf("cannot verify the successful copy workflow run %d: completion logs remain unavailable: %w", run.GetID(), logErr)
	}
	return true, fmt.Errorf("the copy workflow run %d succeeded but did not print the copy completion marker; the copy may have been skipped or incomplete%s",
		run.GetID(), runlog.FailureDetail(log))
}

func waitForCopy(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, timeout time.Duration, observed *observedCopy) error {
	logger.Info("Waiting for Dependabot to check for updates and run the copy...")
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	attempts := make(map[int64]int)
	var lastLog string
	var lastErr error
	for {
		runs, err := observed.workflowRuns(waitCtx, client, repo)
		if err != nil {
			lastErr = err
			logger.Debug(fmt.Sprintf("failed to list the workflow runs: %v", err))
		}
		var successfulLog string
		success := false
		for _, run := range runs {
			if run.GetStatus() != "completed" {
				continue
			}
			log, logErr := runJobLogs(waitCtx, client, repo, run.GetID())
			lastLog = log
			attempts[run.GetID()]++
			done, err := copyResult(run, log, logErr, attempts[run.GetID()])
			if err != nil {
				return err
			}
			if done {
				success, successfulLog = true, log
			}
		}
		if success {
			runlog.ReportCopyResults(successfulLog)
			return nil
		}
		select {
		case <-waitCtx.Done():
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("timed out after %s waiting for Dependabot to complete and verify the copy%s: %w",
				timeout, runlog.FailureDetail(lastLog), errors.Join(waitCtx.Err(), lastErr))
		case <-ticker.C:
		}
	}
}

func runJobLogs(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, runID int64) (string, error) {
	jobs, err := gh.ListWorkflowJobs(ctx, client, repo, runID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to list the jobs of workflow run %d: %w", runID, err)
	}
	if len(jobs) == 0 {
		return "", fmt.Errorf("workflow run %d has no job logs yet", runID)
	}
	var content strings.Builder
	for _, job := range jobs {
		data, err := gh.GetWorkflowJobLogsContent(ctx, client, repo, job.GetID(), logMaxRedirects)
		if err != nil {
			return "", fmt.Errorf("failed to read the log of job %d: %w", job.GetID(), err)
		}
		content.Write(data)
		content.WriteByte('\n')
	}
	return content.String(), nil
}

// stopRuns cancels every owned active run before waiting for any of them.
// Force cancellation is a fallback for jobs that ignore normal cancellation.
func (o *observedCopy) stopRuns(ctx context.Context, client *gh.GitHubClient, repo repository.Repository) error {
	pending := make(map[int64]*github.WorkflowRun)
	cancelErrors := make(map[int64]error)
	for id, run := range o.runs {
		if o.owns(run) && run.GetStatus() != "completed" {
			pending[id] = run
			cancelErrors[id] = gh.CancelWorkflowRunByID(ctx, client, repo, id)
		}
	}
	started := time.Now()
	forced := make(map[int64]bool)
	ticker := time.NewTicker(cancelPollInterval)
	defer ticker.Stop()
	var result error
	for len(pending) > 0 {
		for id, run := range pending {
			current, err := gh.GetWorkflowRunByID(ctx, client, repo, id)
			if gh.IsHTTPNotFound(err) {
				delete(pending, id)
				continue
			}
			if err == nil {
				if !o.owns(current) {
					result = errors.Join(result, fmt.Errorf("workflow run %d no longer matches this copy invocation", id))
					delete(pending, id)
					continue
				}
				o.observe(current)
				if current.GetStatus() == "completed" {
					delete(pending, id)
					continue
				}
			}
			if !forced[id] && (cancelErrors[id] != nil || time.Since(started) >= forceCancelAfter) {
				cancelErrors[id] = gh.ForceCancelWorkflowRunByID(ctx, client, repo, id)
				forced[id] = true
			}
			if ctx.Err() != nil {
				result = errors.Join(result, fmt.Errorf("could not confirm copy workflow run %d stopped; check %s before removing copy artifacts: %w",
					id, run.GetHTMLURL(), errors.Join(ctx.Err(), cancelErrors[id], err)))
			}
		}
		if len(pending) == 0 || ctx.Err() != nil {
			return result
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	return result
}

func isDependabotPullRequest(pr *github.PullRequest, repo repository.Repository, base string) bool {
	return isOwnableDependabotPullRequest(pr, repo) && pr.GetBase().GetRef() == base
}

// isOwnableDependabotPullRequest reports whether pr could belong to a copy run,
// without requiring a specific base so retargeted pull requests still match.
func isOwnableDependabotPullRequest(pr *github.PullRequest, repo repository.Repository) bool {
	return pr != nil &&
		strings.EqualFold(pr.GetUser().GetLogin(), migrator.DependabotActor) &&
		strings.EqualFold(pr.GetHead().GetRepo().GetFullName(), repo.Owner+"/"+repo.Name) &&
		strings.HasPrefix(pr.GetHead().GetRef(), migrator.DependabotBranchPrefix)
}

// A reused temporary base can have old, closed Dependabot pull requests. Check
// the live head's generated run-name before treating an extra branch as ours.
func (o *observedCopy) ownsBranch(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch string) (bool, error) {
	content, err := gh.GetFileContent(ctx, client, repo, o.workflowPath, github.Ptr(branch))
	if gh.IsHTTPNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var metadata struct {
		RunName string `yaml:"run-name"`
	}
	if err := yaml.Unmarshal(content, &metadata); err != nil {
		return false, err
	}
	return o.runName != "" && metadata.RunName == o.runName, nil
}

// cleanup closes owned open pull requests before deleting their heads. Closed
// pull requests are also inspected, but only this invocation's heads are removed.
// The returned bool reports whether removing the temporary base branch is safe;
// it is false whenever an owned pull request cannot be enumerated or closed.
func (o *observedCopy) cleanup(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, base string, keep bool) (bool, error) {
	_, listErr := o.workflowRuns(ctx, client, repo)
	stopErr := o.stopRuns(ctx, client, repo)
	if listErr != nil || stopErr != nil {
		return false, fmt.Errorf("failed to stop all copy runs; preserving branches and run history: %w", errors.Join(listErr, stopErr))
	}
	if keep {
		logger.Warn("Keeping Dependabot branches and copy workflow run history")
		return true, nil
	}
	var result error
	// A pull request still based on the temporary branch would be orphaned once
	// the base is deleted, so a listing failure means the base cannot be removed
	// safely and history must be preserved for a later retry.
	prs, err := gh.ListPullRequests(ctx, client, repo, &gh.ListPullRequestsOptionBase{Base: base}, gh.ListPullRequestsOptionStateAll())
	if err != nil {
		return false, errors.Join(result, fmt.Errorf("failed to list Dependabot pull requests against %s: %w", base, err))
	}
	// A pull request retargeted away from the temporary base no longer matches
	// the base filter above, so enumerate every open pull request as well to
	// avoid deleting the head of a still-open owned pull request.
	openPRs, err := gh.ListPullRequests(ctx, client, repo, gh.ListPullRequestsOptionStateOpen())
	if err != nil {
		return false, errors.Join(result, fmt.Errorf("failed to list open Dependabot pull requests: %w", err))
	}

	safe := true
	blockedHeads := make(map[string]bool)
	handle := func(pr *github.PullRequest) {
		head := pr.GetHead().GetRef()
		owned := o.branches[head]
		if !owned {
			var err error
			owned, err = o.ownsBranch(ctx, client, repo, head)
			if err != nil {
				result = errors.Join(result, fmt.Errorf("failed to verify Dependabot branch %s belongs to this copy: %w", head, err))
				return
			}
		}
		if !owned {
			return
		}
		if pr.GetState() == "open" {
			logger.Info(fmt.Sprintf("Closing Dependabot pull request #%d...", pr.GetNumber()))
			if _, err := gh.ClosePullRequest(ctx, client, repo, pr.GetNumber()); err != nil {
				blockedHeads[head] = true
				safe = false
				result = errors.Join(result, fmt.Errorf("failed to close Dependabot pull request #%d; preserving branch %s: %w", pr.GetNumber(), head, err))
				return
			}
		}
		o.addBranch(head)
	}
	for _, pr := range prs {
		if isDependabotPullRequest(pr, repo, base) {
			handle(pr)
		}
	}
	for _, pr := range openPRs {
		// Pull requests still based on the temporary branch were handled above.
		if pr.GetBase().GetRef() == base || !isOwnableDependabotPullRequest(pr, repo) {
			continue
		}
		handle(pr)
	}
	for _, branch := range sortedKeys(o.branches) {
		if branch == base || blockedHeads[branch] {
			continue
		}
		if err := gh.DeleteBranchIfExists(ctx, client, repo, branch); err != nil {
			result = errors.Join(result, fmt.Errorf("failed to delete Dependabot branch %s: %w", branch, err))
		}
	}
	// Delete run history only after every ownership check; ownsBranch reads the
	// generated workflow file from the live head branches.
	for id := range o.runs {
		if err := gh.DeleteWorkflowRun(ctx, client, repo, id); err != nil && !gh.IsHTTPNotFound(err) {
			result = errors.Join(result, fmt.Errorf("failed to delete copy workflow run %d: %w", id, err))
		}
	}
	return safe, result
}
