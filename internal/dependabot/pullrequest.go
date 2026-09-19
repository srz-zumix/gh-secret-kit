package dependabot

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// retargetedPullRequests maps a pull request number to the base branch it had
// before the copy started.
type retargetedPullRequests map[int]string

// snapshotDependabotPullRequests records the base branch of every open
// Dependabot pull request. Dependabot retargets its own open pull requests to
// whichever branch becomes the default, so the copy has to be able to undo that
// before the temporary base is removed.
func snapshotDependabotPullRequests(ctx context.Context, client *gh.GitHubClient, repo repository.Repository) (retargetedPullRequests, error) {
	prs, err := gh.ListPullRequests(ctx, client, repo, gh.ListPullRequestsOptionStateOpen())
	if err != nil {
		return nil, fmt.Errorf("failed to list the open Dependabot pull requests of %s/%s: %w", repo.Owner, repo.Name, err)
	}
	snapshot := make(retargetedPullRequests)
	for _, pr := range prs {
		if !isOwnableDependabotPullRequest(pr, repo) {
			continue
		}
		snapshot[pr.GetNumber()] = pr.GetBase().GetRef()
	}
	return snapshot, nil
}

// restore retargets the recorded pull requests that Dependabot moved onto the
// temporary base back to their original base branch. Pull requests left on
// another base are untouched.
func (r retargetedPullRequests) restore(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, temporaryBase string) error {
	var result error
	for _, number := range slices.Sorted(maps.Keys(r)) {
		original := r[number]
		if original == temporaryBase {
			continue
		}
		pr, err := gh.GetPullRequest(ctx, client, repo, number)
		if err != nil {
			result = errors.Join(result, fmt.Errorf("failed to get pull request #%d while restoring its base branch: %w", number, err))
			continue
		}
		if pr.GetState() != "open" || pr.GetBase().GetRef() != temporaryBase {
			continue
		}
		logger.Info(fmt.Sprintf("Restoring the base branch of pull request #%d to %s...", number, original))
		if _, err := gh.UpdatePullRequestBase(ctx, client, repo, number, original); err != nil {
			result = errors.Join(result, fmt.Errorf("failed to restore the base branch of pull request #%d to %s; retarget it manually before deleting %s: %w",
				number, original, temporaryBase, err))
		}
	}
	return result
}
