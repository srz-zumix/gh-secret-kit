package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewRunCmd creates a reusable run command (shared by org/repo/env)
func NewRunCmd() *cobra.Command {
	var config RunConfig
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Trigger the migration workflow",
		Long: `Trigger the migration workflow by removing and re-adding the trigger label
on the open PR. Optionally wait for the workflow run to complete.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunWorkflow(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&config.WorkflowName, "workflow-name", types.DefaultWorkflowName, "Name of the workflow file")
	f.StringVar(&config.Branch, "branch", types.DefaultBranch, "Branch name for the migration PR")
	f.StringVar(&config.Label, "label", types.DefaultLabel, "Label name that triggers the migration workflow")
	f.BoolVarP(&config.Wait, "wait", "w", false, "Wait for the workflow run to complete")
	f.StringVar(&config.Timeout, "timeout", "10m", "Timeout duration when waiting for workflow completion (e.g., 5m, 1h)")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the repository if it is archived, then re-archive after the workflow run")

	return cmd
}

// RunWorkflow triggers the migration workflow by toggling the trigger label
func RunWorkflow(ctx context.Context, config *RunConfig) error {
	logger.Info("Running migration workflow")

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
	if !config.SkipArchiveCheck {
		cleanup, err := handleUnarchiveWithCheck(ctx, client, sourceRepo, config.Unarchive)
		if err != nil {
			return err
		}
		defer cleanup()
	}

	branch := config.Branch

	// Determine PR number: use explicitly provided value or search for it
	prNumber := config.PRNumber
	if prNumber == 0 {
		existingPRs, err := gh.ListPullRequests(ctx, client, sourceRepo,
			&gh.ListPullRequestsOptionHead{Head: fmt.Sprintf("%s:%s", sourceRepo.Owner, branch)},
			gh.ListPullRequestsOptionStateOpen(),
		)
		if err != nil {
			return fmt.Errorf("failed to list pull requests: %w", err)
		}
		if len(existingPRs) == 0 {
			return fmt.Errorf("no open PR found from branch %s; run init first", branch)
		}
		prNumber = existingPRs[0].GetNumber()
	}
	logger.Info(fmt.Sprintf("Using PR #%d", prNumber))

	// Remove label (if present) then add label to trigger the workflow
	labelName := config.Label
	logger.Info(fmt.Sprintf("Removing label %s from PR #%d (if present)...", labelName, prNumber))
	_ = gh.RemoveIssueLabel(ctx, client, sourceRepo, prNumber, labelName)

	logger.Info(fmt.Sprintf("Adding label %s to PR #%d to trigger workflow...", labelName, prNumber))
	_, err = gh.AddIssueLabels(ctx, client, sourceRepo, prNumber, []string{labelName})
	if err != nil {
		return fmt.Errorf("failed to add label %s to PR #%d: %w", labelName, prNumber, err)
	}
	logger.Info("Migration workflow triggered!")

	if config.Wait {
		return waitForWorkflowRun(ctx, client, sourceRepo, config)
	}
	return nil
}

// waitForWorkflowRun polls for workflow completion until the run finishes or timeout expires
func waitForWorkflowRun(ctx context.Context, client *gh.GitHubClient, sourceRepo repository.Repository, config *RunConfig) error {
	timeout, err := time.ParseDuration(config.Timeout)
	if err != nil {
		return fmt.Errorf("invalid timeout duration %q: %w", config.Timeout, err)
	}

	workflowFileName := config.WorkflowName + ".yml"
	logger.Info(fmt.Sprintf("Waiting for workflow run to complete (timeout: %s)...", timeout))
	deadline := time.Now().Add(timeout)
	pollInterval := 10 * time.Second

	// Wait a bit for the workflow to start
	time.Sleep(5 * time.Second)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for workflow run to complete")
		}

		runs, err := gh.ListWorkflowRunsByFileName(ctx, client, sourceRepo, workflowFileName, &gh.ListWorkflowRunsOptions{
			Branch: config.Branch,
		})
		if err != nil {
			return fmt.Errorf("failed to list workflow runs: %w", err)
		}
		if len(runs) > 0 {
			latestRun := runs[0]
			status := latestRun.GetStatus()
			conclusion := latestRun.GetConclusion()
			logger.Info(fmt.Sprintf("Workflow run #%d: status=%s, conclusion=%s", latestRun.GetID(), status, conclusion))

			if status == "completed" {
				if conclusion == "success" {
					logger.Info("Workflow run completed successfully!")
					return nil
				}
				return fmt.Errorf("workflow run completed with conclusion: %s", conclusion)
			}
		}

		time.Sleep(pollInterval)
	}
}
