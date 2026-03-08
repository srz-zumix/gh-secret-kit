package workflow

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// handleUnarchive unarchives a repository and returns a cleanup function
// that re-archives it. The cleanup function should be deferred by the caller.
func handleUnarchive(
	ctx context.Context,
	client *gh.GitHubClient,
	repo repository.Repository,
) (cleanup func(), err error) {
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

// handleUnarchiveWithCheck fetches repository info and handles unarchive if needed.
// Returns a cleanup function that re-archives the repository.
// If the repository is archived but unarchive is false, returns an error.
func handleUnarchiveWithCheck(
	ctx context.Context,
	client *gh.GitHubClient,
	repo repository.Repository,
	unarchive bool,
) (cleanup func(), err error) {
	cleanup = func() {}

	repoInfo, err := gh.GetRepository(ctx, client, repo)
	if err != nil {
		return nil, fmt.Errorf("failed to get repository info: %w", err)
	}

	if repoInfo.GetArchived() {
		if !unarchive {
			return nil, fmt.Errorf("repository %s/%s is archived; use --unarchive to temporarily unarchive it", repo.Owner, repo.Name)
		}
		cleanup, err = handleUnarchive(ctx, client, repo)
		if err != nil {
			return nil, err
		}
	}
	return cleanup, nil
}
