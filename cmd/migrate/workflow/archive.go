package workflow

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v79/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// handleUnarchiveIfNeeded checks if a repository is archived and handles
// unarchiving if requested. Returns a cleanup function that re-archives the
// repository and a boolean indicating whether unarchiving was performed.
// The cleanup function should be deferred by the caller.
//
// If the repository is archived and unarchive is false, returns an error.
// If the repository is not archived, returns a no-op cleanup function.
func handleUnarchiveIfNeeded(
	ctx context.Context,
	client *gh.GitHubClient,
	repo repository.Repository,
	repoInfo *github.Repository,
	unarchive bool,
) (cleanup func(), err error) {
	// No-op cleanup by default
	cleanup = func() {}

	if !repoInfo.GetArchived() {
		return cleanup, nil
	}

	// Repository is archived
	if !unarchive {
		return nil, fmt.Errorf("repository %s/%s is archived; use --unarchive to temporarily unarchive it", repo.Owner, repo.Name)
	}

	logger.Info(fmt.Sprintf("Repository %s/%s is archived, temporarily unarchiving...", repo.Owner, repo.Name))
	_, err = gh.UnarchiveRepository(ctx, client, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to unarchive repository: %w", err)
	}

	cleanup = func() {
		logger.Info(fmt.Sprintf("Re-archiving repository %s/%s...", repo.Owner, repo.Name))
		_, archiveErr := gh.ArchiveRepository(ctx, client, repo)
		if archiveErr != nil {
			logger.Warn(fmt.Sprintf("Failed to re-archive repository: %v", archiveErr))
		}
	}

	return cleanup, nil
}
