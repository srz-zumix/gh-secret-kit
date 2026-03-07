package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/org"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
)

// NewOrgCmd creates the migrate org command
func NewOrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Migrate organization secrets",
		Long:  "Migrate GitHub Actions organization secrets between organizations.",
	}

	cmd.AddCommand(workflow.NewInitCmd())
	cmd.AddCommand(org.NewCreateCmd())
	cmd.AddCommand(workflow.NewRunCmd())
	cmd.AddCommand(workflow.NewDeleteCmd())
	cmd.AddCommand(org.NewCheckCmd())
	cmd.AddCommand(org.NewAllCmd())

	return cmd
}
