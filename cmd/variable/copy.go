package variable

import (
	"context"
	"fmt"
	"slices"
	"sort"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/orgaccess"
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
Use --dst-host to apply a host to destination arguments that do not include one.`,
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

			var srcAccess map[string]orgaccess.Source
			if src.Name == "" && copyRepositoryAccess {
				srcAccess, err = collectOrgVariableAccess(ctx, srcClient, src, vars)
				if err != nil {
					logger.Warn("failed to collect source organization variable access, skipping repository access copy", "org", src.Owner, "error", err)
					srcAccess = nil
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

				for _, v := range vars {
					if len(variables) > 0 && !slices.Contains(variables, v.Name) {
						continue
					}

					copyVar := *v
					if src.Name == "" && dst.Name == "" && copyRepositoryAccess && srcAccess != nil {
						if access, ok := srcAccess[v.Name]; ok && access.Visibility != "" && !access.Skip {
							copyVar.Visibility = github.String(access.Visibility)
						}
					}

					err := gh.CreateOrUpdateVariable(ctx, dstClient, dst, &copyVar, overwrite)
					if err != nil {
						if !errorIfExists && gh.IsVariableAlreadyExists(err) {
							logger.Warn(fmt.Sprintf("variable %q already exists in %q, skipping", v.Name, dstArg))
							continue
						}
						return fmt.Errorf("failed to copy variable %q to %q: %w", v.Name, dstArg, err)
					}

					if src.Name == "" && dst.Name == "" && copyRepositoryAccess && srcAccess != nil {
						if access, ok := srcAccess[v.Name]; ok && !access.Skip && access.Visibility == "selected" {
							repos, err := resolveDestinationSelectedRepos(ctx, dstClient, dst.Host, dst.Owner, access.Repos)
							if err != nil {
								logger.Warn("failed to resolve destination repositories for organization variable access", "org", dst.Owner, "variable", v.Name, "error", err)
								continue
							}
							if len(repos) == 0 {
								logger.Warn("no destination repositories left for selected organization variable access, skipping selected repository assignment", "org", dst.Owner, "variable", v.Name)
								continue
							}
							ids, err := resolveSelectedRepoIDs(ctx, dstClient, dst.Host, dst.Owner, repos)
							if err != nil {
								logger.Warn("failed to resolve selected repository IDs for organization variable access", "org", dst.Owner, "variable", v.Name, "error", err)
								continue
							}
							if err := gh.SetSelectedReposForOrgVariable(ctx, dstClient, dst, v.Name, ids); err != nil {
								logger.Warn("failed to apply selected repository access to organization variable", "org", dst.Owner, "variable", v.Name, "error", err)
							}
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

func collectOrgVariableAccess(ctx context.Context, client *gh.GitHubClient, org repository.Repository, vars []*github.ActionsVariable) (map[string]orgaccess.Source, error) {
	wanted := make(map[string]struct{}, len(vars))
	for _, v := range vars {
		if v != nil {
			wanted[v.GetName()] = struct{}{}
		}
	}

	listed, err := gh.ListOrgVariables(ctx, client, org)
	if err != nil {
		return nil, err
	}

	result := make(map[string]orgaccess.Source, len(wanted))
	for _, v := range listed {
		if v == nil {
			continue
		}
		name := v.GetName()
		if _, ok := wanted[name]; !ok {
			continue
		}

		access := orgaccess.Source{Visibility: ""}
		if v.Visibility != nil {
			access.Visibility = *v.Visibility
		}
		if access.Visibility == "selected" {
			repos, err := gh.ListSelectedReposForOrgVariable(ctx, client, org, name)
			if err != nil {
				logger.Warn("failed to list selected repositories for organization variable, skipping selected access", "org", org.Owner, "variable", name, "error", err)
				access.Skip = true
				result[name] = access
				continue
			}
			for _, repo := range repos {
				if repo != nil && repo.GetName() != "" {
					access.Repos = append(access.Repos, repo.GetName())
				}
			}
		}
		result[name] = access
	}

	for _, v := range vars {
		if v == nil {
			continue
		}
		name := v.GetName()
		if _, ok := result[name]; !ok {
			result[name] = orgaccess.Source{Skip: true}
		}
	}
	return result, nil
}

func resolveDestinationSelectedRepos(ctx context.Context, client *gh.GitHubClient, host, org string, sourceRepos []string) ([]string, error) {
	if len(sourceRepos) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(sourceRepos))
	for _, name := range sourceRepos {
		if name == "" {
			continue
		}
		repo := repository.Repository{Host: host, Owner: org, Name: name}
		if _, err := gh.GetRepository(ctx, client, repo); err == nil {
			seen[name] = struct{}{}
		} else {
			logger.Warn("destination repository not found for organization variable access, dropping it from selected access", "org", org, "repo", name, "error", err)
		}
	}
	result := make([]string, 0, len(seen))
	for name := range seen {
		result = append(result, name)
	}
	sort.Strings(result)
	return result, nil
}

func resolveSelectedRepoIDs(ctx context.Context, client *gh.GitHubClient, host, org string, repoNames []string) ([]int64, error) {
	ids := make([]int64, 0, len(repoNames))
	for _, name := range repoNames {
		repo := repository.Repository{Host: host, Owner: org, Name: name}
		obj, err := gh.GetRepository(ctx, client, repo)
		if err != nil {
			return nil, err
		}
		if obj != nil && obj.ID != nil {
			ids = append(ids, *obj.ID)
		}
	}
	return ids, nil
}

func matchSelectedReposForDestination(source []string, existing map[string]struct{}) []string {
	result := make([]string, 0, len(source))
	for _, name := range source {
		if _, ok := existing[name]; ok {
			result = append(result, name)
		}
	}
	sort.Strings(result)
	return result
}
