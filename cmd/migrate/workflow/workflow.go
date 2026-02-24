package workflow

import (
	"github.com/spf13/cobra"
)

// NewWorkflowCmd creates the workflow command
func NewWorkflowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workflow",
		Short: "Manage migration workflow",
		Long:  `Manage migration workflow lifecycle (create/run/delete) for secret migration.`,
	}

	// Add subcommands
	cmd.AddCommand(NewCreateCmd())
	cmd.AddCommand(NewRunCmd())
	cmd.AddCommand(NewDeleteCmd())

	return cmd
}
