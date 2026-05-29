package repo

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
)

// NewDispatchCmd creates the repo dispatch command
func NewDispatchCmd() *cobra.Command {
	var config workflow.DispatchConfig
	config.Scope = migrator.SecretScopeRepo

	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "Rewrite and re-trigger the running workflow to migrate repository secrets",
		Long: `Rewrite the currently running workflow with a secret migration workflow,
push it to a temporary branch, and re-trigger it via workflow_dispatch.

This command is intended to be invoked from inside a workflow_dispatch-triggered
workflow and refuses to run otherwise. The generated workflow reuses the same
runner setting as the running workflow (unless --runner-label is given) and is
itself triggered by workflow_dispatch. The temporary branch is deleted by the
generated workflow after a successful run.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.RunDispatch(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to the repository running the workflow)")
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination repository (e.g., owner/repo)")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to migrate (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.ExcludeSecrets, "exclude-secrets", []string{}, "Secret names to exclude from migration (comma-separated or repeated flag)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Overwrite existing secrets at destination")
	f.StringVar(&config.DestinationTokenSecret, "dst-token", "", "Secret variable name that holds the PAT for the destination (e.g. DST_PAT; referenced as ${{ secrets.<name> }} in the workflow)")
	_ = cmd.Flags().MarkHidden("dst-token")
	f.StringVar(&config.RunnerLabel, "runner-label", "", "Runner label for runs-on (defaults to the running workflow's runner setting)")
	f.StringVar(&config.Branch, "branch", "", "Temporary dispatch branch name (defaults to a unique name derived from the workflow run ID)")

	_ = cmd.MarkFlagRequired("dst")

	return cmd
}
