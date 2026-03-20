package env

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewGetCmd creates the env get command
func NewGetCmd() *cobra.Command {
	var repo string

	cmd := &cobra.Command{
		Use:     "get <environment> [flags]",
		Short:   "Get details of a GitHub Actions environment",
		Aliases: []string{"view"},
		Long: `Show detailed information about a specific GitHub Actions environment.

The output includes environment settings such as wait timer, reviewers,
deployment branch policy, and protection rules.

The repository is specified via --repo (defaults to the current repository).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			envName := args[0]

			r, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to parse repository: %w", err)
			}

			ctx := context.Background()
			client, err := gh.NewGitHubClientWithRepo(r)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			environment, err := gh.GetEnvironment(ctx, client, r, envName)
			if err != nil {
				return fmt.Errorf("failed to get environment %q: %w", envName, err)
			}

			// Fetch custom deployment branch policies when applicable
			policies, err := gh.ListDeploymentCustomBranchPolicies(ctx, client, r, environment)
			if err != nil {
				return fmt.Errorf("failed to list deployment branch policies for environment %q: %w", envName, err)
			}

			renderer := render.NewRenderer(nil)
			renderer.RenderEnvironment(environment, policies)
			return nil
		},
	}

	cmd.Flags().StringVarP(&repo, "repo", "R", "", "Repository to get the environment from (e.g., owner/repo; defaults to current repository)")

	return cmd
}
