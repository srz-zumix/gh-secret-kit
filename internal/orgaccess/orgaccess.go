package orgaccess

// Package orgaccess collects the repository access settings (visibility and
// selected repositories) of organization secrets at the source, and maps them
// to the destination organization so a copy can reproduce the same access.

import (
	"context"

	"github.com/cli/go-gh/v2/pkg/repository"
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
	// Skip marks a "selected" secret whose granted repositories could not be
	// listed at the source. Its access cannot be reproduced without broadening
	// it, so the secret is skipped by MapForDestination and the generators.
	Skip bool
}

// Collect fetches the visibility (and, for "selected", the repository list) of
// each named organization secret from the source. The source secret values and
// names are always read from the Actions store (the only store whose secret
// values a generated copy workflow can read), so access metadata is likewise
// read from the Actions store regardless of the destination store. Secrets that
// cannot be found, or that the token cannot inspect (403/404), are silently
// omitted from the result; callers should treat a missing entry the same as
// "unknown" and fall back to gh's own default.
func Collect(ctx context.Context, client *gh.GitHubClient, srcRepo repository.Repository, secrets []string) (map[string]Source, error) {
	wanted := make(map[string]struct{}, len(secrets))
	for _, name := range secrets {
		wanted[name] = struct{}{}
	}

	orgSecrets, err := gh.ListOrgSecrets(ctx, client, srcRepo)
	if err != nil {
		logger.Warn("failed to list organization secrets to copy repository access, skipping", "org", srcRepo.Owner, "error", err)
		return map[string]Source{}, nil
	}

	result := make(map[string]Source, len(wanted))
	for _, secret := range orgSecrets {
		if _, ok := wanted[secret.Name]; !ok {
			continue
		}
		src := Source{Visibility: secret.Visibility}
		if secret.Visibility == "selected" {
			repos, err := gh.ListSelectedReposForOrgSecret(ctx, client, srcRepo, secret.Name)
			if err != nil {
				// The secret is restricted to selected repositories, but the
				// list could not be retrieved. Copying it with gh's default
				// ("private") would broaden access to every private repository,
				// so mark it to be skipped instead.
				logger.Warn("failed to list selected repositories for organization secret, skipping this secret to avoid broadening access", "secret", secret.Name, "error", err)
				src.Skip = true
				result[secret.Name] = src
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

// SkipUnresolved maps source access without a destination client, for when the
// destination cannot be inspected. A "selected" secret (or one already marked
// Skip at the source) cannot be verified against the destination, so it is
// skipped to avoid broadening its access to "private"; "all" and "private"
// secrets keep their visibility because reproducing them does not broaden
// access.
func SkipUnresolved(src map[string]Source) map[string]migrator.OrgSecretAccess {
	result := make(map[string]migrator.OrgSecretAccess, len(src))
	for name, source := range src {
		if source.Skip || source.Visibility == "selected" {
			result[name] = migrator.OrgSecretAccess{Skip: true}
			continue
		}
		result[name] = migrator.OrgSecretAccess{Visibility: source.Visibility}
	}
	return result
}

// MapForDestination resolves each source repository name to a repository that
// exists in the destination organization, dropping any that do not exist
// (with a warning). A "selected" secret whose repositories cannot be
// represented at the destination without broadening access (its selected list
// could not be read at the source, or none of its repositories exist at the
// destination) is marked to be skipped, because falling back to gh's default
// ("private") would grant it to every private repository.
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
		if source.Skip {
			logger.Warn("skipping organization secret to avoid broadening access; its selected repositories could not be determined at the source", "org", destOrg, "secret", name)
			result[name] = migrator.OrgSecretAccess{Skip: true}
			continue
		}
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
			logger.Warn("no destination repository left for selected access, skipping this secret to avoid broadening access to private", "org", destOrg, "secret", name)
			result[name] = migrator.OrgSecretAccess{Skip: true}
			continue
		}
		result[name] = migrator.OrgSecretAccess{Visibility: "selected", Repos: repos}
	}
	return result
}
