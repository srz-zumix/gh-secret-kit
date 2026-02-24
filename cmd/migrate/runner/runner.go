package runner

import (
	"github.com/spf13/cobra"
)

// NewRunnerCmd creates the runner command
func NewRunnerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runner",
		Short: "Manage self-hosted runner for secret migration",
		Long:  `Manage self-hosted runner lifecycle (setup/teardown) for secret migration.`,
	}

	// Add subcommands
	cmd.AddCommand(NewSetupCmd())
	cmd.AddCommand(NewTeardownCmd())

	return cmd
}
