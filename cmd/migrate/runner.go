package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/runner"
)

// NewRunnerCmd creates the runner command
func NewRunnerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runner",
		Short: "Manage self-hosted runner for secret migration",
		Long:  `Manage self-hosted runner for secret migration (setup, teardown, prune).`,
	}

	// Add subcommands
	cmd.AddCommand(runner.NewSetupCmd())
	cmd.AddCommand(runner.NewTeardownCmd())
	cmd.AddCommand(runner.NewPruneCmd())

	return cmd
}
