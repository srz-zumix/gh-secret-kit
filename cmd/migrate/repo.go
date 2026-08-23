package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/repo"
)

// NewRepoCmd creates the migrate repo command
func NewRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Migrate repository secrets",
		Long:  "Migrate GitHub Actions repository secrets between repositories.",
	}

	cmd.AddCommand(repo.NewInitCmd())
	cmd.AddCommand(repo.NewCreateCmd())
	cmd.AddCommand(repo.NewRunCmd())
	cmd.AddCommand(repo.NewDeleteCmd())
	cmd.AddCommand(repo.NewCheckCmd())
	cmd.AddCommand(repo.NewAllCmd())
	cmd.AddCommand(repo.NewDispatchCmd())
	cmd.AddCommand(repo.NewDumpCmd())

	return cmd
}
