package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
)

// NewWorkflowCmd creates the workflow command
func NewWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage migration workflow",
		Long:  `Manage migration workflow lifecycle (create/run/delete) for secret migration.`,
	}

	// Add subcommands
	cmd.AddCommand(workflow.NewCreateCmd())
	cmd.AddCommand(workflow.NewRunCmd())
	cmd.AddCommand(workflow.NewDeleteCmd())

	return cmd
}
