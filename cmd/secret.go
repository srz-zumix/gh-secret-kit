package cmd

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/secret"
)

// NewSecretCmd creates the secret command
func NewSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage GitHub Actions secrets",
		Long:  "Manage GitHub Actions secrets for repositories, organizations, and environments.",
	}

	cmd.AddCommand(secret.NewCodespacesCmd())
	cmd.AddCommand(secret.NewCopyCmd())
	cmd.AddCommand(secret.NewHistoryCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewSecretCmd())
}
