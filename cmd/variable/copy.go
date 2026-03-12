package variable

import (
	"context"
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the variable copy command
func NewCopyCmd() *cobra.Command {
	var repo, owner, dstHost string
	var variables []string
	var overwrite bool

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...] [flags]",
		Short: "Copy variables from a source to one or more destinations",
		Long: `Copy GitHub Actions variables from a source repository or organization to one or more destinations.

Since variable values are accessible via the GitHub API (unlike secrets), this command
reads values directly from the source and writes them to each destination.

The source scope is determined by --repo (repository variables) or --owner (organization
variables). When neither is specified, the current repository is used as the source.

Each destination argument can be owner/repo (repository scope) or owner (organization scope).
Use --dst-host to apply a host to destination arguments that do not include one.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Parse source: RepositoryInput(repo) and RepositoryOwnerWithHost(owner) are both
			// no-ops when their input is empty, so parser.Repository falls back to the current
			// repository when neither flag is set.
			src, err := parser.Repository(
				parser.RepositoryInput(repo),
				parser.RepositoryOwnerWithHost(owner),
			)
			if err != nil {
				return fmt.Errorf("failed to parse source: %w", err)
			}

			ctx := context.Background()
			srcClient, err := gh.NewGitHubClientWithRepo(src)
			if err != nil {
				return fmt.Errorf("failed to create source GitHub client: %w", err)
			}

			vars, err := gh.ListVariables(ctx, srcClient, src)
			if err != nil {
				return fmt.Errorf("failed to list variables from source: %w", err)
			}

			for _, dstArg := range args {
				dst, err := parser.Repository(parser.RepositoryInput(dstArg))
				if err != nil {
					dst, err = parser.Repository(parser.RepositoryOwnerWithHost(dstArg))
				}
				if err != nil {
					return fmt.Errorf("failed to parse destination %q: expected owner/repo or owner: %w", dstArg, err)
				}
				if dstHost != "" && dst.Host == "" {
					dst.Host = dstHost
				}

				dstClient := srcClient
				if src.Host != dst.Host {
					dstClient, err = gh.NewGitHubClientWithRepo(dst)
					if err != nil {
						return fmt.Errorf("failed to create GitHub client for destination %q on host %q: %w", dstArg, dst.Host, err)
					}
				}

				for _, v := range vars {
					if len(variables) > 0 && !slices.Contains(variables, v.Name) {
						continue
					}
					err := gh.CreateOrUpdateVariable(ctx, dstClient, dst, v, overwrite)
					if err != nil {
						return fmt.Errorf("failed to copy variable %q to %q: %w", v.Name, dstArg, err)
					}
					fmt.Printf("Copied variable: %s -> %s\n", v.Name, dstArg)
				}
			}

			return nil
		},
		Args: cobra.MinimumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository). Mutually exclusive with --owner")
	f.StringVar(&owner, "owner", "", "Source organization/owner for organization-level variables. Mutually exclusive with --repo")
	f.StringVar(&dstHost, "dst-host", "", "Host to apply to destination arguments that do not specify one (e.g., github.com)")
	f.StringSliceVar(&variables, "variables", []string{}, "Specific variable names to copy (comma-separated or repeated flag; defaults to all)")
	f.BoolVar(&overwrite, "overwrite", false, "Overwrite existing variables at destination")
	cmd.MarkFlagsMutuallyExclusive("repo", "owner")

	return cmd
}
