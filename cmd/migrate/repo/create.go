package repo

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
)

// NewCreateCmd creates the repo create command
func NewCreateCmd() *cobra.Command {
	var config workflow.CreateConfig
	config.Scope = migrate.SecretScopeRepo

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Generate and push the migration workflow for repository secrets",
		Long: `Generate a GitHub Actions workflow that migrates repository secrets
from the source repository to the destination repository.
The workflow is pushed to the source repository on a topic branch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.RunCreate(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination repository (e.g., owner/repo)")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to migrate (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Overwrite existing secrets at destination")
	f.StringVar(&config.DestinationTokenSecret, "dst-token", "", "Secret variable name that holds the PAT for the destination (e.g. DST_PAT; referenced as ${{ secrets.<name> }} in the workflow)")
	f.StringVar(&config.DestinationHost, "dst-host", "", "GitHub host for the destination (defaults to source repository host)")
	f.StringVar(&config.RunnerLabel, "runner-label", types.DefaultRunnerLabel, "Runner label for the workflow")
	_ = cmd.Flags().MarkHidden("dst-token")
	f.StringVar(&config.WorkflowName, "workflow-name", types.DefaultWorkflowName, "Name of the generated workflow file")
	f.StringVar(&config.Branch, "branch", types.DefaultBranch, "Branch to push the workflow to")
	f.StringVar(&config.Label, "label", types.DefaultLabel, "Label name for triggering the migration workflow")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the repository if it is archived, then re-archive after completion")

	_ = cmd.MarkFlagRequired("dst")

	return cmd
}
