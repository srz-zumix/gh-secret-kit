package orgaccess

// Package orgaccess collects the repository access settings (visibility and
// selected repositories) of organization secrets at the source, and maps them
// to the destination organization so a copy can reproduce the same access.

import (
	"context"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// Source is a source organization secret's repository access, keyed by
// secret name in the map returned by Collect.
type Source struct {
	// Visibility is "all", "private", or "selected".
	Visibility string
	// Repos lists source repository names granted access when Visibility is
	// "selected".
	Repos []string
}

// selectedReposFunc lists the repositories granted access to a "selected"
// organization secret. It is used to dispatch to the right app-specific API
// without branching on migrator.SecretApp outside of listOrgSecretsForApp.
type selectedReposFunc func(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, name string) ([]*github.Repository, error)

// listOrgSecretsForApp lists organization secrets for the given app and
// returns the matching selected-repositories lookup function.
func listOrgSecretsForApp(ctx context.Context, client *gh.GitHubClient, srcRepo repository.Repository, app migrator.SecretApp) ([]*github.Secret, selectedReposFunc, error) {
	switch app {
	case "", migrator.SecretAppActions:
		secrets, err := gh.ListOrgSecrets(ctx, client, srcRepo)
		return secrets, gh.ListSelectedReposForOrgSecret, err
	case migrator.SecretAppAgents:
		secrets, err := gh.ListAgentsOrgSecrets(ctx, client, srcRepo)
		return secrets, gh.ListSelectedReposForAgentsOrgSecret, err
	case migrator.SecretAppCodespaces:
		secrets, err := gh.ListCodespacesOrgSecrets(ctx, client, srcRepo)
		return secrets, gh.ListSelectedReposForCodespacesOrgSecret, err
	case migrator.SecretAppDependabot:
		secrets, err := gh.ListDependabotOrgSecrets(ctx, client, srcRepo)
		return secrets, gh.ListSelectedReposForDependabotOrgSecret, err
	default:
		return nil, nil, migrator.ValidateSecretApp(app)
	}
}

// Collect fetches the visibility (and, for "selected", the repository list) of
// each named organization secret from the source. Secrets that cannot be
// found, or that the token cannot inspect (403/404), are silently omitted from
// the result; callers should treat a missing entry the same as "unknown" and
// fall back to gh's own default.
func Collect(ctx context.Context, client *gh.GitHubClient, srcRepo repository.Repository, app migrator.SecretApp, secrets []string) (map[string]Source, error) {
	wanted := make(map[string]struct{}, len(secrets))
	for _, name := range secrets {
		wanted[name] = struct{}{}
	}

	orgSecrets, selectedRepos, err := listOrgSecretsForApp(ctx, client, srcRepo, app)
	if err != nil {
		logger.Warn("failed to list organization secrets to copy repository access, skipping", "app", app, "org", srcRepo.Owner, "error", err)
		return map[string]Source{}, nil
	}

	result := make(map[string]Source, len(wanted))
	for _, secret := range orgSecrets {
		if _, ok := wanted[secret.Name]; !ok {
			continue
		}
		src := Source{Visibility: secret.Visibility}
		if secret.Visibility == "selected" {
			repos, err := selectedRepos(ctx, client, srcRepo, secret.Name)
			if err != nil {
				logger.Warn("failed to list selected repositories for organization secret, skipping its access copy", "app", app, "secret", secret.Name, "error", err)
				continue
			}
			for _, r := range repos {
				src.Repos = append(src.Repos, r.GetName())
			}
		}
		result[secret.Name] = src
	}
	return result, nil
}

// MapForDestination resolves each source repository name to a repository that
// exists in the destination organization, dropping any that do not exist
// (with a warning). If every repository of a "selected" secret is dropped,
// the secret falls back to "private" (with a warning) since gh rejects an
// empty --repos list.
func MapForDestination(ctx context.Context, destClient *gh.GitHubClient, destHost, destOrg string, src map[string]Source) map[string]migrator.OrgSecretAccess {
	result := make(map[string]migrator.OrgSecretAccess, len(src))
	// existsCache avoids repeating a lookup for the same repository across
	// multiple secrets that share it.
	existsCache := make(map[string]bool)

	repoExists := func(name string) bool {
		if v, ok := existsCache[name]; ok {
			return v
		}
		repo := repository.Repository{Owner: destOrg, Name: name, Host: destHost}
		_, err := gh.GetRepository(ctx, destClient, repo)
		exists := err == nil
		existsCache[name] = exists
		return exists
	}

	for name, source := range src {
		if source.Visibility != "selected" {
			result[name] = migrator.OrgSecretAccess{Visibility: source.Visibility}
			continue
		}
		var repos []string
		for _, repoName := range source.Repos {
			if repoExists(repoName) {
				repos = append(repos, repoName)
			} else {
				logger.Warn("destination repository not found, dropping it from selected access", "org", destOrg, "repo", repoName, "secret", name)
			}
		}
		if len(repos) == 0 {
			logger.Warn("no destination repository left for selected access, falling back to private", "org", destOrg, "secret", name)
			result[name] = migrator.OrgSecretAccess{Visibility: "private"}
			continue
		}
		result[name] = migrator.OrgSecretAccess{Visibility: "selected", Repos: repos}
	}
	return result
}
