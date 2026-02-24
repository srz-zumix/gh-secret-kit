package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/runner"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
)

// NewMigrateCmd creates the migrate command
func NewMigrateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Migrate GitHub Actions secrets between repositories/organizations",
		Long: `Migrate GitHub Actions secrets (key/value) from a source repository/organization
to a destination repository/organization/environment.

Since the GitHub API does not expose secret values, this command uses actions/scaleset
to register a self-hosted runner on the source, then dispatches a workflow that reads
secret values and sets them directly to the destination via API.`,
	}

	// Add subcommands
	cmd.AddCommand(runner.NewRunnerCmd())
	cmd.AddCommand(workflow.NewWorkflowCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewMigrateCmd())
}
