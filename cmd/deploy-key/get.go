package deploykey

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewGetCmd creates the deploy-key get command
func NewGetCmd() *cobra.Command {
	var repo string

	cmd := &cobra.Command{
		Use:     "get <key-id>",
		Short:   "Get details of a deploy key",
		Aliases: []string{"view"},
		Long: `Show detailed information about a specific deploy key by its ID.

The repository is specified via --repo (defaults to the current repository).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid key ID %q: %w", args[0], err)
			}

			r, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to parse repository: %w", err)
			}

			ctx := context.Background()
			client, err := gh.NewGitHubClientWithRepo(r)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			key, err := gh.GetDeployKey(ctx, client, r, id)
			if err != nil {
				return fmt.Errorf("failed to get deploy key %d for %s/%s: %w", id, r.Owner, r.Name, err)
			}

			renderer := render.NewRenderer(nil)
			return renderer.RenderDeployKey(key, nil)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository to get the deploy key from (e.g., owner/repo; defaults to current repository)")

	return cmd
}
