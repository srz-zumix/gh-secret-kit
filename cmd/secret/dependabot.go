package secret

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/secret/dependabot"
)

// NewDependabotCmd creates the secret dependabot command.
func NewDependabotCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dependabot",
		Short: "Manage GitHub Dependabot secrets",
		Long:  "Manage GitHub Dependabot secrets for repositories and organizations.",
	}

	cmd.AddCommand(dependabot.NewCopyCmd())

	return cmd
}
