package repo

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
)

// NewAllCmd creates the repo all command
func NewAllCmd() *cobra.Command {
	var config workflow.AllConfig
	config.Scope = migrate.SecretScopeRepo

	cmd := &cobra.Command{
		Use:   "all",
		Short: "Run the full migration pipeline for repository secrets",
		Long: `Execute all migration steps in sequence: init, create, run, check, and delete.

This command initializes the stub workflow, generates and pushes the migration
workflow, triggers it, waits for completion, verifies the results, and cleans up.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.RunAll(context.Background(), &config)
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
	f.StringVar(&config.RunnerLabel, "runner-label", "gh-secret-kit-migrate", "Runner label for the workflow")
	_ = cmd.Flags().MarkHidden("dst-token")
	f.StringVar(&config.WorkflowName, "workflow-name", "gh-secret-kit-migrate", "Name of the generated workflow file")
	f.StringVar(&config.Branch, "branch", "gh-secret-kit-migrate", "Branch to push the workflow to")
	f.StringVar(&config.Label, "label", "gh-secret-kit-migrate", "Label name for triggering the migration workflow")
	f.StringVar(&config.Timeout, "timeout", "10m", "Timeout duration when waiting for workflow completion (e.g., 5m, 1h)")

	_ = cmd.MarkFlagRequired("dst")

	return cmd
}
