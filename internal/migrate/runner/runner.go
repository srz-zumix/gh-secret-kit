package runner

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v88/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// ListRunners returns self-hosted runners for the given org or repository.
// When runnerGroup is non-empty the result is restricted to runners belonging
// to that group (org-level only); otherwise all runners are returned.
func ListRunners(ctx context.Context, client *gh.GitHubClient, sourceRepo repository.Repository, runnerGroup string) ([]*github.Runner, error) {
	if runnerGroup != "" {
		// Runner groups are an organization/enterprise feature and are not
		// available for repository-level runners.
		if sourceRepo.Name != "" {
			return nil, fmt.Errorf("--runner-group is not supported for repository runners (runner groups are org/enterprise only)")
		}
		group, err := gh.FindOrgRunnerGroup(ctx, client, sourceRepo, runnerGroup)
		if err != nil {
			return nil, fmt.Errorf("failed to find runner group %q: %w", runnerGroup, err)
		}
		if group == nil {
			return nil, fmt.Errorf("runner group %q not found", runnerGroup)
		}
		runners, err := gh.ListOrgRunnerGroupRunners(ctx, client, sourceRepo, group.GetID())
		if err != nil {
			return nil, fmt.Errorf("failed to list runners for group %q: %w", runnerGroup, err)
		}
		return runners, nil
	}

	runners, err := gh.ListRunners(ctx, client, sourceRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to list runners: %w", err)
	}
	return runners, nil
}
