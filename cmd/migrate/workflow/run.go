package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v79/github"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

var (
	runCommonOpts   types.CommonOptions
	runWorkflowOpts types.WorkflowOptions
)

// NewRunCmd creates the workflow run command
func NewRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Dispatch the migration workflow",
		Long: `Dispatch the migration workflow via workflow_dispatch event.

This command dispatches the workflow and optionally waits for it to complete,
reporting success/failure for each secret migration.`,
		RunE: runWorkflow,
		Args: cobra.NoArgs,
	}

	// Common flags
	cmd.Flags().StringVarP(&runCommonOpts.Source, "source", "s", "", "Source repository or organization (e.g., owner/repo or org)")
	cmd.Flags().StringVarP(&runCommonOpts.Destination, "destination", "d", "", "Destination repository or organization (e.g., owner2/repo2 or org2)")
	cmd.MarkFlagRequired("source")
	cmd.MarkFlagRequired("destination")

	// Workflow-specific flags
	cmd.Flags().StringVar(&runWorkflowOpts.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the workflow to dispatch")
	cmd.Flags().BoolVar(&runWorkflowOpts.Wait, "wait", true, "Wait for the workflow run to complete")
	cmd.Flags().StringVar(&runWorkflowOpts.Timeout, "timeout", "10m", "Timeout for waiting for the workflow run")

	return cmd
}

func runWorkflow(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	logger.Info("Running migration workflow")
	logger.Debug(fmt.Sprintf("Source: %s, Destination: %s", runCommonOpts.Source, runCommonOpts.Destination))
	logger.Debug(fmt.Sprintf("Workflow Name: %s, Wait: %v, Timeout: %s", runWorkflowOpts.WorkflowName, runWorkflowOpts.Wait, runWorkflowOpts.Timeout))

	// Parse source repository
	sourceRepo, err := parser.Repository(parser.RepositoryInput(runCommonOpts.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	// Initialize GitHub client
	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	// Get workflow file name
	workflowFileName := fmt.Sprintf(".github/workflows/%s.yml", runWorkflowOpts.WorkflowName)

	// Dispatch workflow
	logger.Info(fmt.Sprintf("Dispatching workflow %s...", workflowFileName))
	event := github.CreateWorkflowDispatchEventRequest{
		Ref: "", // Use default branch
		Inputs: map[string]interface{}{
			"destination": runCommonOpts.Destination,
		},
	}
	err = gh.CreateWorkflowDispatchEventByFileName(ctx, client, sourceRepo, workflowFileName, event)
	if err != nil {
		return fmt.Errorf("failed to dispatch workflow: %w", err)
	}

	logger.Info("Workflow dispatched successfully")

	// If wait is enabled, poll for workflow run completion
	if runWorkflowOpts.Wait {
		timeout, err := time.ParseDuration(runWorkflowOpts.Timeout)
		if err != nil {
			return fmt.Errorf("failed to parse timeout: %w", err)
		}

		logger.Info(fmt.Sprintf("Waiting for workflow to complete (timeout: %s)...", runWorkflowOpts.Timeout))
		run, err := waitForWorkflowRun(ctx, client, sourceRepo, workflowFileName, timeout)
		if err != nil {
			return fmt.Errorf("workflow run failed: %w", err)
		}

		logger.Info(fmt.Sprintf("Workflow run completed: %s", run.GetConclusion()))
		if run.GetConclusion() != "success" {
			return fmt.Errorf("workflow run failed with conclusion: %s", run.GetConclusion())
		}
	}

	return nil
}

// waitForWorkflowRun waits for the latest workflow run to complete
func waitForWorkflowRun(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, workflowFileName string, timeout time.Duration) (*github.WorkflowRun, error) {
	startTime := time.Now()
	pollInterval := 5 * time.Second

	// Wait a bit for the workflow run to be created
	time.Sleep(2 * time.Second)

	var latestRun *github.WorkflowRun

	for {
		// Check timeout
		if time.Since(startTime) > timeout {
			return nil, fmt.Errorf("timeout waiting for workflow run to complete")
		}

		// Get latest workflow run
		runs, err := gh.ListWorkflowRunsByFileName(ctx, client, repo, workflowFileName, &gh.ListWorkflowRunsOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to list workflow runs: %w", err)
		}

		if len(runs) == 0 {
			logger.Debug("No workflow runs found yet, waiting...")
			time.Sleep(pollInterval)
			continue
		}

		// Get the latest run
		latestRun = runs[0]
		logger.Debug(fmt.Sprintf("Latest workflow run status: %s, conclusion: %s", latestRun.GetStatus(), latestRun.GetConclusion()))

		// Check if run is completed
		if latestRun.GetStatus() == "completed" {
			return latestRun, nil
		}

		// Wait before polling again
		time.Sleep(pollInterval)
	}
}
