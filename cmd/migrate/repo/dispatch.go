package repo

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewDispatchCmd creates the repo dispatch command
func NewDispatchCmd() *cobra.Command {
	var config workflow.DispatchConfig
	config.Scope = migrator.SecretScopeRepo

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Generate and trigger a workflow to migrate repository secrets",
		Long: `Generate a secret migration workflow, push it to a temporary branch, and
trigger it via workflow_dispatch.

This command has two modes:

  - Self-rewrite (no --src): it must run inside a workflow_dispatch-triggered
    workflow and rewrites the currently running workflow. The generated workflow
    reuses the same runner setting as the running workflow unless --runner-label
    is given.
  - Target-specified (--src given): the workflow does not exist in the source
    repository, so it does not need to run inside a workflow. The workflow is
    registered using a syntax-error workflow trick, then the corrected workflow
    is pushed and dispatched. --runner-label is required in this mode and
    --workflow-name selects the workflow file name.

The temporary branch is deleted by the generated workflow after a successful run.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := parser.ValidateTokenSecretName(config.DestinationTokenSecret); err != nil {
				return err
			}
			return workflow.RunDispatch(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to the repository running the workflow; when set, enables target-specified mode)")
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination repository (e.g., owner/repo)")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to migrate (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.ExcludeSecrets, "exclude-secrets", []string{}, "Secret names to exclude from migration (comma-separated or repeated flag)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Overwrite existing secrets at destination")
	f.StringVar(&config.DestinationTokenSecret, "dst-token-secret", "", "Secret variable name that holds the PAT for the destination (e.g. DST_PAT; referenced as ${{ secrets.<name> }} in the workflow)")
	f.StringVar(&config.RunnerLabel, "runner-label", "", "Runner label for runs-on (defaults to the running workflow's runner setting; required with --src)")
	f.StringVar(&config.WorkflowName, "workflow-name", types.DefaultWorkflowName, "Workflow file name (without extension) for target-specified mode")
	f.StringVar(&config.Branch, "branch", "", "Temporary dispatch branch name (defaults to a unique name derived from the workflow run ID or a timestamp)")
	f.BoolVarP(&config.Wait, "wait", "w", false, "Wait for the dispatched workflow run to complete")
	f.StringVar(&config.Timeout, "timeout", "10m", "Timeout duration when waiting for workflow completion (e.g., 5m, 1h)")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the source repository if it is archived, then re-archive after the dispatch")

	_ = cmd.MarkFlagRequired("dst")

	return cmd
}
