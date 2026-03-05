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
			return RunInit(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&config.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the generated workflow file")
	f.StringVar(&config.Branch, "branch", "gh-secret-kit-migrate", "Branch to push the stub workflow to")
	f.StringVar(&config.Label, "label", "gh-secret-kit-migrate", "Label name to create for triggering the migration workflow")

	return cmd
}

// RunInit initializes the stub workflow via a draft PR
func RunInit(ctx context.Context, config *InitConfig) error {
	logger.Info("Initializing stub workflow via draft PR")
	logger.Debug(fmt.Sprintf("Source: %s, Workflow Name: %s", config.Source, config.WorkflowName))

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

	// Get default branch
	repo, err := gh.GetRepository(ctx, client, sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to get repository: %w", err)
	}
	defaultBranch := repo.GetDefaultBranch()
	logger.Debug(fmt.Sprintf("Default branch: %s", defaultBranch))

	// Determine topic branch name
	branch := config.Branch
	workflowPath := fmt.Sprintf(".github/workflows/%s.yml", config.WorkflowName)

	// Generate stub workflow YAML
	stubYAML, err := migratePackage.GenerateStubWorkflowYAML(config.WorkflowName)
	if err != nil {
		return fmt.Errorf("failed to generate stub workflow YAML: %w", err)
	}

	// Get default branch HEAD SHA
	defaultBranchInfo, err := gh.GetBranch(ctx, client, sourceRepo, defaultBranch)
	if err != nil {
		return fmt.Errorf("failed to get default branch %s: %w", defaultBranch, err)
	}
	headSHA := defaultBranchInfo.GetCommit().GetSHA()

	// Create (or reuse) topic branch
	_, err = gh.GetBranch(ctx, client, sourceRepo, branch)
	if err != nil {
		logger.Info(fmt.Sprintf("Creating topic branch %s from %s...", branch, defaultBranch))
		_, err = gh.CreateBranch(ctx, client, sourceRepo, branch, headSHA)
		if err != nil {
			return fmt.Errorf("failed to create topic branch %s: %w", branch, err)
		}
	} else {
		logger.Debug(fmt.Sprintf("Topic branch %s already exists", branch))
	}

	// Push stub workflow to topic branch
	// [ci skip] in the commit message prevents push/pull_request workflows from being triggered.
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
			return fmt.Errorf("failed to update stub workflow file on branch %s: %w", branch, err)
		}
	} else {
		_, err = gh.CreateRepositoryFile(ctx, client, sourceRepo, workflowPath, fileOptions)
		if err != nil {
			return fmt.Errorf("failed to create stub workflow file on branch %s: %w", branch, err)
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
			return fmt.Errorf("failed to create trigger label %s: %w", labelName, err)
		}
		logger.Info(fmt.Sprintf("Trigger label %s created", labelName))
	} else {
		logger.Debug(fmt.Sprintf("Trigger label %s already exists", labelName))
	}

	// Check for an existing open PR from branch to defaultBranch
	var prNumber int
	existingPRs, err := gh.ListPullRequests(ctx, client, sourceRepo,
		&gh.ListPullRequestsOptionHead{Head: fmt.Sprintf("%s:%s", sourceRepo.Owner, branch)},
		gh.ListPullRequestsOptionStateOpen(),
	)
	if err != nil {
		return fmt.Errorf("failed to check existing pull requests: %w", err)
	}

	if len(existingPRs) > 0 {
		prNumber = existingPRs[0].GetNumber()
		logger.Debug(fmt.Sprintf("Reusing existing PR #%d", prNumber))
	} else {
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
			return fmt.Errorf("failed to create pull request: %w", err)
		}
		prNumber = pr.GetNumber()
		logger.Info(fmt.Sprintf("PR #%d created: %s", prNumber, pr.GetHTMLURL()))
	}

	logger.Info(fmt.Sprintf("Stub workflow initialized for: %s/%s/.github/workflows/%s.yml", sourceRepo.Owner, sourceRepo.Name, config.WorkflowName))
	return nil
}

