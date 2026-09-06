package secret

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/secret/codespaces"
)

// NewCodespacesCmd creates the secret codespaces command
func NewCodespacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "codespaces",
		Short: "Manage GitHub Codespaces development environment secrets",
		Long:  "Manage GitHub Codespaces development environment secrets for repositories and organizations.",
	}

	cmd.AddCommand(codespaces.NewCopyCmd())

	return cmd
}
