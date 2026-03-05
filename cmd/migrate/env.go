package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/env"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
)

// NewEnvCmd creates the migrate env command
func NewEnvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Migrate environment secrets",
		Long:  "Migrate GitHub Actions environment secrets between repositories.",
	}

	cmd.AddCommand(workflow.NewInitCmd())
	cmd.AddCommand(env.NewCreateCmd())
	cmd.AddCommand(workflow.NewRunCmd())
	cmd.AddCommand(workflow.NewDeleteCmd())
	cmd.AddCommand(env.NewCheckCmd())

	return cmd
}
