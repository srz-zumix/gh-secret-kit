package deploykey

import (
	"context"
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewDeleteCmd creates the deploy-key delete command
func NewDeleteCmd() *cobra.Command {
	var repo string

	cmd := &cobra.Command{
		Use:   "delete <key-id>",
		Short: "Delete a deploy key from a repository",
		Long: `Delete a deploy key from a repository by its ID.

The repository is specified via --repo (defaults to the current repository).`,
		Aliases: []string{"rm"},
		Args:    cobra.ExactArgs(1),
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

			if err := gh.DeleteDeployKey(ctx, client, r, id); err != nil {
				return fmt.Errorf("failed to delete deploy key %d from %s/%s: %w", id, r.Owner, r.Name, err)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "Repository to delete the deploy key from (e.g., owner/repo; defaults to current repository)")

	return cmd
}
