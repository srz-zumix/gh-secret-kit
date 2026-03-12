package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/variable"
)

// NewVariableCmd creates the variable command
func NewVariableCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "variable",
		Short: "Manage GitHub Actions variables",
		Long:  "Manage GitHub Actions variables for repositories and organizations.",
	}

	cmd.AddCommand(variable.NewCopyCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewVariableCmd())
}
