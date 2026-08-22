package workflow

import (
	"context"
	"fmt"
	"sort"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewDeleteRunsCmd creates a reusable delete-runs command (shared by repo/env)
// that removes completed workflow run history for the given workflow. This is
// intended to clean up runs left behind by the dispatch/dump commands, whose
// per-run dispatch branches self-delete on success but whose run history in
// the Actions UI is not removed automatically.
func NewDeleteRunsCmd(defaultWorkflowName string) *cobra.Command {
	var config DeleteRunsConfig
	cmd := &cobra.Command{
		Use:   "delete-runs",
		Short: "Delete completed workflow run history for a dispatch/dump workflow",
		Long: `Delete completed workflow run history for the given workflow file name.

This targets the run history left behind by the dispatch/dump commands: their
per-run dispatch branches self-delete after a successful run, but the run
entries themselves remain in the Actions UI until removed. In-progress runs
are never deleted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return RunDeleteRuns(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&config.WorkflowName, "workflow-name", defaultWorkflowName, "Workflow file name (without extension) to delete run history for")
	f.IntVar(&config.KeepLast, "keep-last", 0, "Keep the N most recent completed runs and delete the rest (0 deletes all completed runs)")
	f.BoolVarP(&config.DryRun, "dryrun", "n", false, "List the runs that would be deleted without deleting them")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the repository if it is archived, then re-archive after completion")

	return cmd
}

// RunDeleteRuns deletes completed workflow run history for config.WorkflowName,
// keeping the config.KeepLast most recent completed runs.
func RunDeleteRuns(ctx context.Context, config *DeleteRunsConfig) error {
	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	if !config.SkipArchiveCheck {
		cleanup, err := handleUnarchiveWithCheck(ctx, client, sourceRepo, config.Unarchive)
		if err != nil {
			return err
		}
		defer cleanup()
	}

	workflowFileName := config.WorkflowName + ".yml"
	runs, err := gh.ListWorkflowRunsByFileName(ctx, client, sourceRepo, workflowFileName, nil)
	if err != nil {
		if gh.IsHTTPNotFound(err) {
			logger.Info(fmt.Sprintf("No workflow runs found for %s", workflowFileName))
			return nil
		}
		return fmt.Errorf("failed to list workflow runs: %w", err)
	}

	completed := make([]int64, 0, len(runs))
	for _, run := range runs {
		if run.GetStatus() != "completed" {
			continue
		}
		completed = append(completed, run.GetID())
	}
	// Newest first (higher run IDs are newer).
	sort.Slice(completed, func(i, j int) bool { return completed[i] > completed[j] })

	if config.KeepLast >= len(completed) {
		completed = nil
	} else if config.KeepLast > 0 {
		completed = completed[config.KeepLast:]
	}

	if len(completed) == 0 {
		logger.Info(fmt.Sprintf("No completed workflow runs to delete for %s", workflowFileName))
		return nil
	}

	logger.Info(fmt.Sprintf("Found %d completed workflow run(s) to delete for %s", len(completed), workflowFileName))
	for _, runID := range completed {
		if config.DryRun {
			logger.Info(fmt.Sprintf("[dryrun] Would delete workflow run ID %d", runID))
			continue
		}
		if err := gh.DeleteWorkflowRun(ctx, client, sourceRepo, runID); err != nil {
			return fmt.Errorf("failed to delete workflow run ID %d: %w", runID, err)
		}
		logger.Info(fmt.Sprintf("Deleted workflow run ID %d", runID))
	}

	return nil
}
