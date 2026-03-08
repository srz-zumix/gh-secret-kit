package workflow

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	migratePackage "github.com/srz-zumix/gh-secret-kit/pkg/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewInitCmd creates a reusable init command (shared by org/repo/env)
func NewInitCmd() *cobra.Command {
	var config InitConfig
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Register the stub workflow via a draft PR",
		Long: `Push a stub workflow file (with [ci skip] in the commit message) to a topic
branch, then open a draft PR so GitHub recognises the workflow file.
The PR and branch are kept open for later use by "run".

The branch can be cleaned up later with "delete".`,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := RunInit(context.Background(), &config)
			return err
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&config.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the generated workflow file")
	f.StringVar(&config.Branch, "branch", "gh-secret-kit-migrate", "Branch to push the stub workflow to")
	f.StringVar(&config.Label, "label", "gh-secret-kit-migrate", "Label name to create for triggering the migration workflow")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the repository if it is archived, then re-archive after completion")

	return cmd
}

// RunInit initializes the stub workflow via a draft PR.
// It returns the PR number that was created or reused.
func RunInit(ctx context.Context, config *InitConfig) (int, error) {
	logger.Info("Initializing stub workflow via draft PR")
	logger.Debug(fmt.Sprintf("Source: %s, Workflow Name: %s", config.Source, config.WorkflowName))

	// Parse source repository
	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return 0, fmt.Errorf("failed to parse source repository: %w", err)
	}

	// Initialize GitHub client
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return 0, fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Get default branch
	repo, err := gh.GetRepository(ctx, client, sourceRepo)
	if err != nil {
		return 0, fmt.Errorf("failed to get repository: %w", err)
	}

	// Check if the repository is archived and handle unarchive if requested
	cleanup := func() {}
	if !config.SkipArchiveCheck && repo.GetArchived() {
		if !config.Unarchive {
			return 0, fmt.Errorf("repository %s/%s is archived; use --unarchive to temporarily unarchive it", sourceRepo.Owner, sourceRepo.Name)
		}
		cleanup, err = handleUnarchive(ctx, client, sourceRepo)
		if err != nil {
			return 0, err
		}
	}
	defer cleanup()

	defaultBranch := repo.GetDefaultBranch()
	logger.Debug(fmt.Sprintf("Default branch: %s", defaultBranch))

	// Determine topic branch name
	branch := config.Branch
	workflowPath := fmt.Sprintf(".github/workflows/%s.yml", config.WorkflowName)

	// Generate stub workflow YAML
	stubYAML, err := migratePackage.GenerateStubWorkflowYAML(config.WorkflowName)
	if err != nil {
		return 0, fmt.Errorf("failed to generate stub workflow YAML: %w", err)
	}

	// Get default branch HEAD SHA
	defaultBranchInfo, err := gh.GetBranch(ctx, client, sourceRepo, defaultBranch)
	if err != nil {
		return 0, fmt.Errorf("failed to get default branch %s: %w", defaultBranch, err)
	}
	headSHA := defaultBranchInfo.GetCommit().GetSHA()

	// Check for existing open PR first to decide whether to reuse branch or recreate
	existingPRs, err := gh.ListPullRequests(ctx, client, sourceRepo,
		&gh.ListPullRequestsOptionHead{Head: fmt.Sprintf("%s:%s", sourceRepo.Owner, branch)},
		gh.ListPullRequestsOptionStateOpen(),
	)
	if err != nil {
		return 0, fmt.Errorf("failed to check existing pull requests: %w", err)
	}

	var prNumber int
	if len(existingPRs) > 0 {
		// Reuse existing PR and branch - don't recreate to avoid closing the PR
		prNumber = existingPRs[0].GetNumber()
		logger.Info(fmt.Sprintf("Reusing existing PR #%d and topic branch %s", prNumber, branch))
	} else {
		// No open PR exists - create or recreate branch
		_, branchErr := gh.GetBranch(ctx, client, sourceRepo, branch)
		if branchErr != nil {
			// Branch doesn't exist, create it
			logger.Info(fmt.Sprintf("Creating topic branch %s from %s...", branch, defaultBranch))
			_, err = gh.CreateBranch(ctx, client, sourceRepo, branch, headSHA)
			if err != nil {
				return 0, fmt.Errorf("failed to create topic branch %s: %w", branch, err)
			}
		} else {
			// Branch exists but no open PR - delete and recreate from latest HEAD
			logger.Info(fmt.Sprintf("Topic branch %s exists but no open PR found, recreating from latest %s HEAD...", branch, defaultBranch))
			if derr := gh.DeleteBranch(ctx, client, sourceRepo, branch); derr != nil {
				return 0, fmt.Errorf("failed to delete stale topic branch %s: %w", branch, derr)
			}
			_, err = gh.CreateBranch(ctx, client, sourceRepo, branch, headSHA)
			if err != nil {
				return 0, fmt.Errorf("failed to recreate topic branch %s: %w", branch, err)
			}
			logger.Info(fmt.Sprintf("Topic branch %s recreated from %s", branch, defaultBranch))
		}
	}

	// Push stub workflow to topic branch
	// [ci skip] in the commit message prevents push/pull_request workflows from
	// being triggered by the stub push. The real migration workflow (pushed by
	// "create") intentionally omits [ci skip] so that the labeled event fires.
	logger.Info(fmt.Sprintf("Pushing stub workflow to branch %s at %s...", branch, workflowPath))
	existingContent, _, gerr := client.GetRepositoryContent(ctx, sourceRepo.Owner, sourceRepo.Name, workflowPath, &branch)
	commitMessage := fmt.Sprintf("Add stub workflow for secret migration: %s [ci skip]", config.WorkflowName)
	fileOptions := &gh.RepositoryContentFileOptions{
		Message: commitMessage,
		Content: []byte(stubYAML),
		Branch:  &branch,
	}
	if gerr == nil && existingContent != nil {
		sha := existingContent.GetSHA()
		fileOptions.SHA = &sha
		_, err = gh.UpdateRepositoryFile(ctx, client, sourceRepo, workflowPath, fileOptions)
		if err != nil {
			return 0, fmt.Errorf("failed to update stub workflow file on branch %s: %w", branch, err)
		}
	} else {
		_, err = gh.CreateRepositoryFile(ctx, client, sourceRepo, workflowPath, fileOptions)
		if err != nil {
			return 0, fmt.Errorf("failed to create stub workflow file on branch %s: %w", branch, err)
		}
	}
	logger.Info("Stub workflow file pushed successfully")

	// Create trigger label if it does not already exist
	labelName := config.Label
	_, err = gh.GetLabel(ctx, client, sourceRepo, labelName)
	if err != nil {
		logger.Info(fmt.Sprintf("Creating trigger label: %s", labelName))
		desc := "Trigger label for gh-secret-kit migration workflow"
		color := "0075ca"
		_, err = gh.CreateLabel(ctx, client, sourceRepo, &labelName, &desc, &color)
		if err != nil {
			return 0, fmt.Errorf("failed to create trigger label %s: %w", labelName, err)
		}
		logger.Info(fmt.Sprintf("Trigger label %s created", labelName))
	} else {
		logger.Debug(fmt.Sprintf("Trigger label %s already exists", labelName))
	}

	// Create PR if we don't have one yet
	if prNumber == 0 {
		// Create PR: branch → defaultBranch
		logger.Info(fmt.Sprintf("Creating PR: %s → %s...", branch, defaultBranch))
		title := fmt.Sprintf("[gh-secret-kit] Register stub workflow: %s", config.WorkflowName)
		body := "Draft PR to register the stub workflow via pull_request event. Used by gh-secret-kit to trigger migration workflows via label events."
		draft := true
		pr, err := gh.CreatePullRequest(ctx, client, sourceRepo, gh.NewPullRequest{
			Title: title,
			Head:  branch,
			Base:  defaultBranch,
			Body:  &body,
			Draft: &draft,
		})
		if err != nil {
			return 0, fmt.Errorf("failed to create pull request: %w", err)
		}
		prNumber = pr.GetNumber()
		logger.Info(fmt.Sprintf("PR #%d created: %s", prNumber, pr.GetHTMLURL()))
	}

	logger.Info(fmt.Sprintf("Stub workflow initialized for: %s/%s/.github/workflows/%s.yml", sourceRepo.Owner, sourceRepo.Name, config.WorkflowName))
	return prNumber, nil
}
