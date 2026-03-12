package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/env"
)

// NewEnvCmd creates the env command
func NewEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Manage GitHub Actions environment resources",
		Long:  "Manage GitHub Actions environment resources such as variables for repository environments.",
	}

	cmd.AddCommand(env.NewVariableCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewEnvCmd())
}
