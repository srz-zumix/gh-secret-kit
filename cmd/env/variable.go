package env

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/env/variable"
)

// NewVariableCmd creates the env variable command
func NewVariableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "variable",
		Short: "Manage GitHub Actions environment variables",
		Long:  "Manage GitHub Actions environment variables for repository environments.",
	}

	cmd.AddCommand(variable.NewCopyCmd())

	return cmd
}
