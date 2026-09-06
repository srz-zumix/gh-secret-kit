package workflow

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/gh-secret-kit/internal/destination"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// copyDestination is a resolved destination together with the temporary source
// repository secret that holds its token.
type copyDestination struct {
	*destination.Destination
	tokenSecret string
}

// RunCopy copies secrets from the source repository to every destination by
// generating a workflow, pushing it to a temporary branch, and triggering it
// via workflow_dispatch.
//
// Because secret values cannot be read through the API, the copy has to happen
// inside a workflow run. The token for each destination host is taken from the
// local gh authentication and registered as a temporary source repository
// secret, so a GitHub-hosted runner can reach the destination without a
// self-hosted runner. The temporary branch, token secrets, and run history are
// removed once the workflow finishes.
func RunCopy(ctx context.Context, config *CopyConfig) error {
	logger.Info("Copying secrets")

	scope := config.Scope
	if scope == "" {
		scope = migrator.SecretScopeRepo
	}
	if scope == migrator.SecretScopeEnv && config.SourceEnv == "" {
		return fmt.Errorf("--src-env is required when --scope is env")
	}

	app := config.DestinationApp
	if app == "" {
		app = migrator.SecretAppActions
	}
	if err := migrator.ValidateSecretApp(app); err != nil {
		return err
	}
	// Environments only exist for Actions secrets.
	if app != migrator.SecretAppActions && config.DestinationEnv != "" {
		return fmt.Errorf("--dst-env cannot be used with --dst-app %q", app)
	}

	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	secrets, err := collectCopySecrets(ctx, client, sourceRepo, config, scope)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		logger.Info("No secrets found to copy, skipping")
		return nil
	}

	destinations, err := resolveCopyDestinations(config, scope, sourceRepo)
	if err != nil {
		return err
	}
	hostTokens, err := destination.ResolveTokens(config.DestinationToken, toResolvedDestinations(destinations))
	if err != nil {
		return err
	}
	if err := destination.Verify(ctx, scope == migrator.SecretScopeOrg, toResolvedDestinations(destinations), hostTokens); err != nil {
		return err
	}
	if err := assignTokenSecretNames(ctx, client, sourceRepo, config, destinations, hostTokens); err != nil {
		return err
	}

	archiveCleanup, err := handleUnarchiveWithCheck(ctx, client, sourceRepo, config.Unarchive)
	if err != nil {
		return err
	}
	defer archiveCleanup()

	logger.Info(fmt.Sprintf("Copying %d secrets to %d destinations", len(secrets), len(destinations)))

	renameMap, err := migrator.ParseRenameMappings(config.Rename)
	if err != nil {
		return err
	}

	workflowConfig := migrator.CopyWorkflowConfig{
		WorkflowName:   config.WorkflowName,
		RunsOn:         config.RunnerLabel,
		Scope:          scope,
		DestinationApp: app,
		SourceEnv:      config.SourceEnv,
		Secrets:        secrets,
		Rename:         renameMap,
		Overwrite:      config.Overwrite,
		Destinations:   buildWorkflowDestinations(config, scope, app, destinations),
	}
	logger.Info("Generating copy workflow YAML...")
	workflowYAML, err := migrator.GenerateCopyWorkflowYAML(workflowConfig)
	if err != nil {
		return fmt.Errorf("failed to generate workflow YAML: %w", err)
	}

	baseSHA, err := defaultBranchSHA(ctx, client, sourceRepo)
	if err != nil {
		return err
	}

	branch, err := resolveDispatchBranch(config.Branch, config.WorkflowName)
	if err != nil {
		return err
	}

	// Register the destination tokens before creating the branch so a failure
	// here leaves nothing behind.
	registered := make(map[string]struct{}, len(hostTokens))
	for _, dest := range destinations {
		if _, done := registered[dest.Host]; done {
			continue
		}
		registered[dest.Host] = struct{}{}
		logger.Info(fmt.Sprintf("Registering temporary token secret %s for host %s...", dest.tokenSecret, dest.Host))
		if err := gh.SetRepoSecret(ctx, client, sourceRepo, dest.tokenSecret, hostTokens[dest.Host]); err != nil {
			return fmt.Errorf("failed to register temporary token secret %s: %w", dest.tokenSecret, err)
		}
		defer deleteTokenSecret(ctx, client, sourceRepo, dest.tokenSecret)
	}

	if err := createDispatchBranch(ctx, client, sourceRepo, branch, baseSHA); err != nil {
		return err
	}
	defer deleteCopyBranch(ctx, client, sourceRepo, branch)

	setup := &dispatchSetup{
		sourceRepo:       sourceRepo,
		client:           client,
		workflowPath:     ".github/workflows/" + config.WorkflowName + ".yml",
		workflowFileName: config.WorkflowName + ".yml",
		targetSpecified:  true,
		baseSHA:          baseSHA,
		runsOn:           config.RunnerLabel,
	}
	if err := triggerDispatchWorkflow(ctx, client, sourceRepo, setup, branch, config.WorkflowName, workflowYAML, true, true, config.Timeout); err != nil {
		return fmt.Errorf("failed to run the copy workflow: %w", err)
	}

	logger.Info("Secrets copied successfully")
	return nil
}

// resolveCopyDestinations resolves each destination argument and pairs it with
// the temporary token secret assigned later.
func resolveCopyDestinations(config *CopyConfig, scope migrator.SecretScope, sourceRepo repository.Repository) ([]*copyDestination, error) {
	resolved, err := destination.Resolve(config.Destinations, scope == migrator.SecretScopeOrg, sourceRepo)
	if err != nil {
		return nil, err
	}
	destinations := make([]*copyDestination, 0, len(resolved))
	for _, dest := range resolved {
		destinations = append(destinations, &copyDestination{Destination: dest})
	}
	return destinations, nil
}

// toResolvedDestinations unwraps the shared destination of each entry.
func toResolvedDestinations(destinations []*copyDestination) []*destination.Destination {
	resolved := make([]*destination.Destination, 0, len(destinations))
	for _, dest := range destinations {
		resolved = append(resolved, dest.Destination)
	}
	return resolved
}

// assignTokenSecretNames derives one temporary secret name per destination host
// and rejects names that are already used in the source repository, so an
// existing secret is never overwritten and then deleted.
func assignTokenSecretNames(ctx context.Context, client *gh.GitHubClient, sourceRepo repository.Repository, config *CopyConfig, destinations []*copyDestination, hostTokens map[string]string) error {
	names := make(map[string]string, len(hostTokens))
	for host := range hostTokens {
		name := tokenSecretNameForHost(config.TokenSecretName, host)
		if _, err := gh.GetRepoSecret(ctx, client, sourceRepo, name); err == nil {
			return fmt.Errorf("secret %s already exists in %s/%s; use --token-secret-name to choose another name", name, sourceRepo.Owner, sourceRepo.Name)
		} else if !gh.IsHTTPNotFound(err) {
			return fmt.Errorf("failed to check the temporary token secret %s: %w", name, err)
		}
		names[host] = name
	}
	for _, dest := range destinations {
		dest.tokenSecret = names[dest.Host]
	}
	return nil
}

// tokenSecretNameForHost builds a secret name unique to a destination host by
// appending the host in the character set allowed for secret names.
func tokenSecretNameForHost(base, host string) string {
	return destination.NameForHost(base, host)
}

// collectCopySecrets resolves the secret names to copy for the given scope and
// applies the include and exclude filters.
func collectCopySecrets(ctx context.Context, client *gh.GitHubClient, sourceRepo repository.Repository, config *CopyConfig, scope migrator.SecretScope) ([]string, error) {
	secrets := config.Secrets
	if len(secrets) == 0 {
		var err error
		switch scope {
		case migrator.SecretScopeOrg:
			logger.Info("No specific secrets specified, fetching org secrets from source...")
			secrets, err = fetchOrgSecrets(ctx, client, sourceRepo)
		case migrator.SecretScopeEnv:
			logger.Info("No specific secrets specified, fetching env secrets from source...")
			secrets, err = fetchEnvSecrets(ctx, client, sourceRepo, config.SourceEnv)
		default:
			logger.Info("No specific secrets specified, fetching repo secrets from source...")
			secrets, err = fetchRepoSecrets(ctx, client, sourceRepo)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch secrets from source: %w", err)
		}
	}
	return excludeSecrets(secrets, config.ExcludeSecrets), nil
}

// buildWorkflowDestinations converts the resolved destinations into the form
// expected by the workflow generator.
func buildWorkflowDestinations(config *CopyConfig, scope migrator.SecretScope, app migrator.SecretApp, destinations []*copyDestination) []migrator.CopyDestination {
	destEnv := config.DestinationEnv
	if scope == migrator.SecretScopeEnv && destEnv == "" {
		destEnv = config.SourceEnv
	}
	// Non-Actions stores have no environment level, so the copy targets the
	// repository or organization level regardless of the source scope.
	if app != migrator.SecretAppActions {
		destEnv = ""
	}
	result := make([]migrator.CopyDestination, 0, len(destinations))
	for _, dest := range destinations {
		result = append(result, migrator.CopyDestination{
			Target:      dest.Target,
			Host:        dest.Host,
			Env:         destEnv,
			TokenSecret: dest.tokenSecret,
		})
	}
	return result
}

// defaultBranchSHA returns the head commit SHA of the repository default branch.
func defaultBranchSHA(ctx context.Context, client *gh.GitHubClient, repo repository.Repository) (string, error) {
	repoInfo, err := gh.GetRepository(ctx, client, repo)
	if err != nil {
		return "", fmt.Errorf("failed to get repository: %w", err)
	}
	defaultBranch := repoInfo.GetDefaultBranch()
	branchInfo, err := gh.GetBranch(ctx, client, repo, defaultBranch)
	if err != nil {
		return "", fmt.Errorf("failed to get default branch %s: %w", defaultBranch, err)
	}
	return branchInfo.GetCommit().GetSHA(), nil
}

// deleteTokenSecret removes a temporary token secret, logging instead of
// failing so that the remaining cleanup still runs.
func deleteTokenSecret(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, name string) {
	logger.Info(fmt.Sprintf("Deleting temporary token secret %s...", name))
	if err := gh.DeleteRepoSecret(ctx, client, repo, name); err != nil {
		logger.Warn(fmt.Sprintf("failed to delete temporary token secret %s: %v", name, err))
	}
}

// deleteCopyBranch removes the temporary branch, logging instead of failing so
// that the remaining cleanup still runs.
func deleteCopyBranch(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, branch string) {
	logger.Info(fmt.Sprintf("Deleting temporary branch %s...", branch))
	if err := gh.DeleteBranchIfExists(ctx, client, repo, branch); err != nil {
		logger.Warn(fmt.Sprintf("failed to delete temporary branch %s: %v", branch, err))
	}
}
