package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/repo"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
)

// NewRepoCmd creates the migrate repo command
func NewRepoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repo",
		Short: "Migrate repository secrets",
		Long:  "Migrate GitHub Actions repository secrets between repositories.",
	}

	cmd.AddCommand(workflow.NewInitCmd())
	cmd.AddCommand(repo.NewCreateCmd())
	cmd.AddCommand(workflow.NewRunCmd())
	cmd.AddCommand(workflow.NewDeleteCmd())
	cmd.AddCommand(repo.NewCheckCmd())
	cmd.AddCommand(repo.NewAllCmd())

	return cmd
}
