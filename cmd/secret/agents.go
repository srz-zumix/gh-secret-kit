package secret

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/secret/agents"
)

// NewAgentsCmd creates the secret agents command
func NewAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Manage GitHub Copilot coding agent secrets",
		Long:  "Manage GitHub Copilot coding agent (Agents) secrets for repositories and organizations.",
	}

	cmd.AddCommand(agents.NewCopyCmd())

	return cmd
}
