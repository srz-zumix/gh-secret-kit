package env

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewListCmd creates the env list command
func NewListCmd() *cobra.Command {
	var repo string

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List GitHub Actions environments for a repository",
		Long: `List all GitHub Actions environments configured for a repository.

The repository is specified via --repo (defaults to the current repository).`,
		Aliases: []string{"ls"},
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to parse repository: %w", err)
			}

			ctx := context.Background()
			client, err := gh.NewGitHubClientWithRepo(r)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			envs, err := gh.ListEnvironments(ctx, client, r)
			if err != nil {
				return fmt.Errorf("failed to list environments for %s/%s: %w", r.Owner, r.Name, err)
			}

			renderer := render.NewRenderer(nil)
			renderer.RenderEnvironments(envs, nil)
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "Repository to list environments for (e.g., owner/repo; defaults to current repository)")

	return cmd
}
