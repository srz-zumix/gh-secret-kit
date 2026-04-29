package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/org"
)

// NewOrgCmd creates the migrate org command
func NewOrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "org",
		Short: "Migrate organization secrets",
		Long:  "Migrate GitHub Actions organization secrets between organizations.",
	}

	cmd.AddCommand(org.NewInitCmd())
	cmd.AddCommand(org.NewCreateCmd())
	cmd.AddCommand(org.NewRunCmd())
	cmd.AddCommand(org.NewDeleteCmd())
	cmd.AddCommand(org.NewCheckCmd())
	cmd.AddCommand(org.NewAllCmd())

	return cmd
}
