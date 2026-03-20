package cmd

import (
	"github.com/spf13/cobra"
	deploykey "github.com/srz-zumix/gh-secret-kit/cmd/deploy-key"
)

// NewDeployKeyCmd creates the deploy-key command
func NewDeployKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-key",
		Short: "Manage repository deploy keys",
		Long:  "Manage deploy keys for GitHub repositories.",
	}

	cmd.AddCommand(deploykey.NewAddCmd())
	cmd.AddCommand(deploykey.NewDeleteCmd())
	cmd.AddCommand(deploykey.NewGetCmd())
	cmd.AddCommand(deploykey.NewListCmd())
	cmd.AddCommand(deploykey.NewMigrateCmd())

	return cmd
}

func init() {
	rootCmd.AddCommand(NewDeployKeyCmd())
}
