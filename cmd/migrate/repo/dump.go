package repo

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/workflow"
)

// NewDumpCmd creates the (undocumented) repo dump command. It is hidden from
// help output and shell completion but can still be invoked directly.
func NewDumpCmd() *cobra.Command {
	var config workflow.DumpConfig

	cmd := &cobra.Command{
		Use:    "dump",
		Short:  "Generate and trigger a workflow to dump repository secret values to a file",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.RunDump(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to the repository running the workflow; when set, enables target-specified mode)")
	f.StringVarP(&config.Output, "output", "o", "", "Path (on the workflow runner) to write NAME=BASE64_VALUE secret lines to")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to dump (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.ExcludeSecrets, "exclude-secrets", []string{}, "Secret names to exclude from the dump (comma-separated or repeated flag)")
	f.StringVar(&config.RunnerLabel, "runner-label", "", "Runner label for runs-on (defaults to the running workflow's runner setting; required with --src)")
	f.StringVar(&config.WorkflowName, "workflow-name", types.DefaultDumpWorkflowName, "Workflow file name (without extension) for target-specified mode")
	f.StringVar(&config.Branch, "branch", "", "Temporary dispatch branch name (defaults to a unique name derived from the workflow run ID or a timestamp)")
	f.BoolVarP(&config.Wait, "wait", "w", false, "Wait for the dispatched workflow run to complete")
	f.StringVar(&config.Timeout, "timeout", "10m", "Timeout duration when waiting for workflow completion (e.g., 5m, 1h)")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the source repository if it is archived, then re-archive after the dispatch")

	_ = cmd.MarkFlagRequired("output")

	return cmd
}
