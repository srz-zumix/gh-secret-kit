package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// copyDestination is a destination resolved from a command line argument,
// together with the temporary source repository secret that holds its token.
type copyDestination struct {
	arg         string
	repo        repository.Repository
	target      string
	host        string
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
	hostTokens, err := resolveDestinationTokens(config, destinations)
	if err != nil {
		return err
	}
	if err := verifyCopyDestinations(ctx, scope, destinations, hostTokens); err != nil {
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

	renameMap, err := parseRenameMappings(config.Rename)
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
		if _, done := registered[dest.host]; done {
			continue
		}
		registered[dest.host] = struct{}{}
		logger.Info(fmt.Sprintf("Registering temporary token secret %s for host %s...", dest.tokenSecret, dest.host))
		if err := gh.SetRepoSecret(ctx, client, sourceRepo, dest.tokenSecret, hostTokens[dest.host]); err != nil {
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

// resolveCopyDestinations parses each destination argument and resolves its host,
// falling back to the source host and then github.com.
func resolveCopyDestinations(config *CopyConfig, scope migrator.SecretScope, sourceRepo repository.Repository) ([]*copyDestination, error) {
	destinations := make([]*copyDestination, 0, len(config.Destinations))
	for _, arg := range config.Destinations {
		var destRepo repository.Repository
		var err error
		if scope == migrator.SecretScopeOrg {
			destRepo, err = parser.Repository(parser.RepositoryOwnerWithHost(arg))
		} else {
			destRepo, err = parser.Repository(parser.RepositoryInput(arg))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse destination %q: %w", arg, err)
		}

		host := destRepo.Host
		if host == "" {
			host = sourceRepo.Host
		}
		if host == "" {
			host = "github.com"
		}
		destRepo.Host = host

		target := destRepo.Owner
		if scope != migrator.SecretScopeOrg {
			if destRepo.Name == "" {
				return nil, fmt.Errorf("destination %q must be in [HOST/]OWNER/REPO format", arg)
			}
			target = destRepo.Owner + "/" + destRepo.Name
		}

		destinations = append(destinations, &copyDestination{
			arg:    arg,
			repo:   destRepo,
			target: target,
			host:   host,
		})
	}
	return destinations, nil
}

// resolveDestinationTokens returns a token per distinct destination host, taken
// from --dst-token or from the local gh authentication.
func resolveDestinationTokens(config *CopyConfig, destinations []*copyDestination) (map[string]string, error) {
	hosts := make(map[string]struct{})
	for _, dest := range destinations {
		hosts[dest.host] = struct{}{}
	}

	tokens := make(map[string]string, len(hosts))
	if config.DestinationToken != "" {
		if len(hosts) > 1 {
			return nil, fmt.Errorf("--dst-token cannot be used when the destinations span multiple hosts")
		}
		for host := range hosts {
			tokens[host] = config.DestinationToken
		}
		return tokens, nil
	}

	for host := range hosts {
		token, _ := auth.TokenForHost(host)
		if token == "" {
			return nil, fmt.Errorf("no token found for destination host %s; run \"gh auth login --hostname %s\" or pass --dst-token", host, host)
		}
		tokens[host] = token
	}
	return tokens, nil
}

// verifyCopyDestinations checks that each destination exists and is reachable
// with the resolved token, so a typo does not waste a workflow run.
func verifyCopyDestinations(ctx context.Context, scope migrator.SecretScope, destinations []*copyDestination, hostTokens map[string]string) error {
	for _, dest := range destinations {
		destClient, err := gh.NewGitHubClientWithToken(dest.repo, hostTokens[dest.host])
		if err != nil {
			return fmt.Errorf("failed to create GitHub client for destination %q: %w", dest.arg, err)
		}
		if scope == migrator.SecretScopeOrg {
			if _, err := destClient.GetOrg(ctx, dest.repo.Owner); err != nil {
				return fmt.Errorf("destination organization %q not found or inaccessible: %w", dest.target, err)
			}
			continue
		}
		if _, err := gh.GetRepository(ctx, destClient, dest.repo); err != nil {
			return fmt.Errorf("destination repository %q not found or inaccessible: %w", dest.target, err)
		}
	}
	return nil
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
		dest.tokenSecret = names[dest.host]
	}
	return nil
}

// tokenSecretNameForHost builds a secret name unique to a destination host by
// appending the host in the character set allowed for secret names.
func tokenSecretNameForHost(base, host string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(host) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return base + "_" + b.String()
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
			Target:      dest.target,
			Host:        dest.host,
			Env:         destEnv,
			TokenSecret: dest.tokenSecret,
		})
	}
	return result
}

// parseRenameMappings converts OLD_NAME=NEW_NAME entries into a lookup map.
func parseRenameMappings(mappings []string) (map[string]string, error) {
	renameMap := make(map[string]string, len(mappings))
	for _, mapping := range mappings {
		parts := strings.SplitN(mapping, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid rename mapping format: %s (expected OLD_NAME=NEW_NAME)", mapping)
		}
		renameMap[parts[0]] = parts[1]
	}
	return renameMap, nil
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
