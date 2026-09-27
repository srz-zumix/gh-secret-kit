// Package varaccess collects the repository access settings (visibility and
// selected repositories) of organization Actions variables at the source and
// resolves them against a destination organization so a copy can reproduce the
// same access without leaving unverified access behind.
package varaccess

import (
	"context"
	"sort"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/internal/orgaccess"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// Collect fetches the visibility (and, for "selected", the repository list) of
// each requested organization variable from the source organization. A
// "selected" variable whose granted repositories cannot be listed, or a
// requested variable missing from the listing, is marked Skip so the copy stays
// fail-closed and does not reproduce unverified access at the destination.
func Collect(ctx context.Context, client *gh.GitHubClient, org repository.Repository, vars []*github.ActionsVariable) (map[string]orgaccess.Source, error) {
	wanted := make(map[string]struct{}, len(vars))
	for _, v := range vars {
		if v != nil {
			wanted[v.GetName()] = struct{}{}
		}
	}

	listed, err := gh.ListOrgVariables(ctx, client, org)
	if err != nil {
		return nil, err
	}

	result := make(map[string]orgaccess.Source, len(wanted))
	for _, v := range listed {
		if v == nil {
			continue
		}
		name := v.GetName()
		if _, ok := wanted[name]; !ok {
			continue
		}

		src := orgaccess.Source{}
		if v.Visibility != nil {
			src.Visibility = *v.Visibility
		}
		if src.Visibility == "selected" {
			repos, err := gh.ListSelectedReposForOrgVariable(ctx, client, org, name)
			if err != nil {
				// The variable is restricted to selected repositories but the
				// list could not be read, so skip it rather than reproduce
				// unverified access at the destination.
				logger.Warn("failed to list selected repositories for organization variable, skipping it to avoid reproducing unverified access", "org", org.Owner, "variable", name, "error", err)
				src.Skip = true
				result[name] = src
				continue
			}
			for _, repo := range repos {
				if repo != nil && repo.GetName() != "" {
					src.Repos = append(src.Repos, repo.GetName())
				}
			}
		}
		result[name] = src
	}

	// A requested variable missing from the listing has an unknown access, so
	// skip it rather than copy it with unverified access.
	for _, v := range vars {
		if v == nil {
			continue
		}
		name := v.GetName()
		if _, ok := result[name]; !ok {
			result[name] = orgaccess.Source{Skip: true}
		}
	}
	return result, nil
}

// Applied is the resolved destination access for a single variable.
type Applied struct {
	// Visibility to write for the variable ("all", "private", or "selected").
	Visibility string
	// RepoIDs are the destination repository IDs to grant when Visibility is
	// "selected".
	RepoIDs []int64
	// Skip marks a variable that must not be written because its access could
	// not be reproduced at the destination without leaving unverified access.
	Skip bool
}

// Resolve maps each source variable's access to the destination organization.
// Each destination repository name is looked up once and cached across
// variables, resolving existence and ID in a single pass to avoid duplicate API
// calls. A variable already marked Skip at the source, or a "selected" variable
// none of whose repositories exist at the destination, is marked Skip.
func Resolve(ctx context.Context, client *gh.GitHubClient, host, org string, src map[string]orgaccess.Source) map[string]Applied {
	// repoIDs caches a destination repository name to its ID, or 0 when the
	// repository does not exist, so repositories shared across variables are
	// resolved only once.
	repoIDs := make(map[string]int64)
	resolveID := func(name string) int64 {
		if id, ok := repoIDs[name]; ok {
			return id
		}
		var id int64
		repo := repository.Repository{Host: host, Owner: org, Name: name}
		obj, err := gh.GetRepository(ctx, client, repo)
		if err != nil {
			logger.Warn("destination repository not found for organization variable access, dropping it from selected access", "org", org, "repo", name, "error", err)
		} else if obj != nil && obj.ID != nil {
			id = *obj.ID
		}
		repoIDs[name] = id
		return id
	}

	result := make(map[string]Applied, len(src))
	for name, source := range src {
		if source.Skip {
			result[name] = Applied{Skip: true}
			continue
		}
		if source.Visibility != "selected" {
			result[name] = Applied{Visibility: source.Visibility}
			continue
		}

		var ids []int64
		for _, repoName := range uniqueSorted(source.Repos) {
			if id := resolveID(repoName); id != 0 {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			logger.Warn("no destination repositories left for selected organization variable access, skipping it to avoid reproducing unverified access", "org", org, "variable", name)
			result[name] = Applied{Skip: true}
			continue
		}
		result[name] = Applied{Visibility: "selected", RepoIDs: ids}
	}
	return result
}

// uniqueSorted returns the non-empty repository names de-duplicated and sorted
// for stable, cache-friendly resolution.
func uniqueSorted(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}
