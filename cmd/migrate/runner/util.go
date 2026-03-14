package runner

import (
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// resolveSourceRepo resolves the source repository/organization from the
// --repo flag, a positional argument, or the current repository's owner.
// repoFlag is the value of --repo (empty string when not specified).
// args are the cobra positional arguments.
// label is used only for debug logging (e.g., the runner label).
func resolveSourceRepo(repoFlag string, args []string, label string) (repository.Repository, error) {
	var (
		sourceRepo repository.Repository
		err        error
	)

	switch {
	case repoFlag != "":
		// -R/--repo specified: repository-scoped runner
		logger.Debug(fmt.Sprintf("Repo: %s, Runner Label: %s", repoFlag, label))
		sourceRepo, err = parser.Repository(parser.RepositoryInput(repoFlag))
	case len(args) > 0:
		// First positional arg: organization-scoped runner
		logger.Debug(fmt.Sprintf("Org: %s, Runner Label: %s", args[0], label))
		sourceRepo, err = parser.Repository(parser.RepositoryOwnerWithHost(args[0]))
	default:
		// Fall back to current repository's owner (org-scoped runner)
		var currentRepo repository.Repository
		currentRepo, err = parser.Repository(parser.RepositoryInput(""))
		if err == nil {
			sourceRepo = repository.Repository{Host: currentRepo.Host, Owner: currentRepo.Owner}
			logger.Debug(fmt.Sprintf("Org: %s, Runner Label: %s (current repo owner)", sourceRepo.Owner, label))
		}
	}

	if err != nil {
		return repository.Repository{}, fmt.Errorf("failed to parse source: %w", err)
	}
	return sourceRepo, nil
}
