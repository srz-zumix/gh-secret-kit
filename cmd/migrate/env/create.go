package env

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
)

// NewCreateCmd creates the env create command
func NewCreateCmd() *cobra.Command {
	var config workflow.CreateConfig
	config.Scope = migrate.SecretScopeEnv

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Generate and push the migration workflow for environment secrets",
		Long: `Generate a GitHub Actions workflow that migrates environment secrets
from the source repository's environment to the destination repository's environment.
The workflow is pushed to the source repository on a topic branch.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.RunCreate(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination repository (e.g., owner/repo)")
	f.StringVar(&config.SourceEnv, "src-env", "", "Source environment name")
	f.StringVar(&config.DestinationEnv, "dst-env", "", "Destination environment name")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to migrate (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Overwrite existing secrets at destination")
	f.StringVar(&config.DestinationTokenSecret, "dst-token", "", "Secret variable name that holds the PAT for the destination (e.g. DST_PAT; referenced as ${{ secrets.<name> }} in the workflow)")
	f.StringVar(&config.DestinationHost, "dst-host", "", "GitHub host for the destination (defaults to source repository host)")
	f.StringVar(&config.RunnerLabel, "runner-label", "gh-secret-kit-migrate", "Runner label for the workflow")
	_ = cmd.Flags().MarkHidden("dst-token")
	f.StringVar(&config.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the generated workflow file")
	f.StringVar(&config.Branch, "branch", "gh-secret-kit-migrate", "Branch to push the workflow to")
	f.StringVar(&config.Label, "label", "gh-secret-kit-migrate", "Label name for triggering the migration workflow")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the repository if it is archived, then re-archive after completion")

	_ = cmd.MarkFlagRequired("dst")
	_ = cmd.MarkFlagRequired("src-env")
	_ = cmd.MarkFlagRequired("dst-env")

	return cmd
}
