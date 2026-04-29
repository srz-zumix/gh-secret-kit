package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/env"
)

// NewEnvCmd creates the migrate env command
func NewEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Migrate environment secrets",
		Long:  "Migrate GitHub Actions environment secrets between repositories.",
	}

	cmd.AddCommand(env.NewInitCmd())
	cmd.AddCommand(env.NewCreateCmd())
	cmd.AddCommand(env.NewRunCmd())
	cmd.AddCommand(env.NewDeleteCmd())
	cmd.AddCommand(env.NewCheckCmd())
	cmd.AddCommand(env.NewAllCmd())

	return cmd
}
