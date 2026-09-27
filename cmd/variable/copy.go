package variable

import (
	"context"
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/orgaccess"
	"github.com/srz-zumix/gh-secret-kit/internal/varaccess"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the variable copy command
func NewCopyCmd() *cobra.Command {
	var repo, owner, dstHost string
	var variables []string
	var overwrite, errorIfExists bool
	var copyRepositoryAccess = true
	var noCopyRepositoryAccess bool

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...]",
		Short: "Copy variables from a source to one or more destinations",
		Long: `Copy GitHub Actions variables from a source repository or organization to one or more destinations.

Since variable values are accessible via the GitHub API (unlike secrets), this command
reads values directly from the source and writes them to each destination.

The source scope is determined by --repo (repository variables) or --owner (organization
variables). When neither is specified, the current repository is used as the source.

Each destination argument can be owner/repo (repository scope) or owner (organization scope).
Use --dst-host to apply a host to destination arguments that do not include one.

With org-to-org copy, repository access is copied by default: each variable's visibility
(all/private/selected) and, for selected, the granted repositories are reproduced at the
destination organization. A selected variable whose granted repositories cannot be
determined at the source, or none of which exist at the destination, is skipped rather than
copied with unverified access. Pass --no-copy-repository-access to skip copying access.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if noCopyRepositoryAccess {
				copyRepositoryAccess = false
			}

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

			// Apply the --variables filter to the source list once, up front, so
			// access collection and every destination copy operate on the same
			// requested subset instead of listing access for variables that are
			// not being copied.
			if len(variables) > 0 {
				filtered := vars[:0]
				for _, v := range vars {
					if slices.Contains(variables, v.Name) {
						filtered = append(filtered, v)
					}
				}
				vars = filtered
			}

			// srcAccess is only meaningful for an organization source; it stays nil for
			// repository sources or when access copying is disabled.
			var srcAccess map[string]orgaccess.Source
			if src.Name == "" && copyRepositoryAccess {
				srcAccess, err = varaccess.Collect(ctx, srcClient, src, vars)
				if err != nil {
					// Access could not be determined, so fail closed rather than
					// copy selected variables without their verified repositories.
					return fmt.Errorf("failed to collect source organization variable access: %w", err)
				}
			}

			for _, dstArg := range args {
				dst, err := parser.Repository(parser.RepositoryOwnerOrRepo(dstArg))
				if err != nil {
					return fmt.Errorf("failed to parse destination %q: expected [host/]owner[/repo]: %w", dstArg, err)
				}
				if dstHost != "" && dst.Host == "" {
					dst.Host = dstHost
				} else if dst.Host == "" && src.Host != "" {
					dst.Host = src.Host
				}

				dstClient := srcClient
				if dst.Host != src.Host {
					dstClient, err = gh.NewGitHubClientWithRepo(dst)
					if err != nil {
						return fmt.Errorf("failed to create GitHub client for destination %q on host %q: %w", dstArg, dst.Host, err)
					}
				}

				// access maps each variable to its resolved destination access; it stays
				// nil unless this is an org-to-org copy with access copying enabled.
				var access map[string]varaccess.Applied
				if dst.Name == "" && srcAccess != nil {
					access = varaccess.Resolve(ctx, dstClient, dst.Host, dst.Owner, srcAccess)
				}

				for _, v := range vars {
					copyVar := *v
					applied, haveAccess := access[v.Name]
					if haveAccess {
						if applied.Skip {
							logger.Warn("skipping organization variable to avoid reproducing unverified repository access", "org", dst.Owner, "variable", v.Name)
							continue
						}
						if applied.Visibility != "" {
							copyVar.Visibility = &applied.Visibility
						}
					}

					if err := gh.CreateOrUpdateVariable(ctx, dstClient, dst, &copyVar, overwrite); err != nil {
						if !errorIfExists && gh.IsVariableAlreadyExists(err) {
							logger.Warn(fmt.Sprintf("variable %q already exists in %q, skipping", v.Name, dstArg))
							continue
						}
						return fmt.Errorf("failed to copy variable %q to %q: %w", v.Name, dstArg, err)
					}

					if haveAccess && applied.Visibility == "selected" {
						if err := gh.SetSelectedReposForOrgVariable(ctx, dstClient, dst, v.Name, applied.RepoIDs); err != nil {
							return fmt.Errorf("failed to apply selected repository access to variable %q in %q: %w", v.Name, dstArg, err)
						}
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
	f.BoolVar(&errorIfExists, "error-if-exists", false, "Return an error if a variable already exists at destination instead of skipping")
	f.BoolVar(&copyRepositoryAccess, "copy-repository-access", true, "With org-to-org variable copy, also copy each variable's visibility and selected repositories to the destination")
	f.BoolVar(&noCopyRepositoryAccess, "no-copy-repository-access", false, "With org-to-org variable copy, skip copying each variable's visibility and selected repositories to the destination")
	cmd.MarkFlagsMutuallyExclusive("repo", "owner")

	return cmd
}
