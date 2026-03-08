package workflow

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewDeleteCmd creates a reusable delete command (shared by org/repo/env)
func NewDeleteCmd() *cobra.Command {
	var config DeleteConfig
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Clean up the migration workflow branch and PRs",
		Long: `Close any open pull requests from the migration topic branch and then
delete the branch. This removes the generated workflow file and all
related resources from the source repository.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunDelete(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&config.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the workflow file")
	f.StringVar(&config.Branch, "branch", "gh-secret-kit-migrate", "Branch to delete")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the repository if it is archived, then re-archive after completion")

	return cmd
}

// RunDelete cleans up the migration workflow by closing PRs and deleting the branch
func RunDelete(ctx context.Context, config *DeleteConfig) error {
	logger.Info("Deleting migration workflow resources")

	// Parse source repository
	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	// Initialize GitHub client
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Check if the repository is archived and handle unarchive if requested
	cleanup, err := handleUnarchiveWithCheck(ctx, client, sourceRepo, config.Unarchive)
	if err != nil {
		return err
	}
	defer cleanup()

	branch := config.Branch

	// Close any open PRs from the topic branch
	openPRs, err := gh.ListPullRequests(ctx, client, sourceRepo,
		&gh.ListPullRequestsOptionHead{Head: fmt.Sprintf("%s:%s", sourceRepo.Owner, branch)},
		gh.ListPullRequestsOptionStateOpen(),
	)
	if err != nil {
		return fmt.Errorf("failed to list pull requests: %w", err)
	}
	for _, pr := range openPRs {
		prNumber := pr.GetNumber()
		logger.Info(fmt.Sprintf("Closing PR #%d...", prNumber))
		_, cerr := gh.ClosePullRequest(ctx, client, sourceRepo, prNumber)
		if cerr != nil {
			return fmt.Errorf("failed to close PR #%d: %w", prNumber, cerr)
		}
		logger.Info(fmt.Sprintf("PR #%d closed", prNumber))
	}

	// Delete the topic branch
	logger.Info(fmt.Sprintf("Deleting branch %s...", branch))
	err = gh.DeleteBranch(ctx, client, sourceRepo, branch)
	if err != nil {
		return fmt.Errorf("failed to delete branch %s: %w", branch, err)
	}
	logger.Info(fmt.Sprintf("Branch %s deleted", branch))

	logger.Info("Migration workflow resources cleaned up successfully")
	return nil
}
