package env

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the env copy command
func NewCopyCmd() *cobra.Command {
	var repo, dstHost, srcEnv, dstEnv string
	var overwrite bool

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...]",
		Short: "Copy a GitHub Actions environment to one or more destination repositories",
		Long: `Copy a GitHub Actions environment (settings, deployment branch policies, and environment variables) from a
source repository to one or more destination repositories.

The source repository is specified via --repo (defaults to the current repository).
The source environment is specified via --src-env (required).
The destination environment is specified via --dst-env (defaults to the value of --src-env).
Each destination argument must be in owner/repo format.
Use --dst-host to apply a host to destination arguments that do not specify one.
When copying environment variables, existing variables at the destination are preserved unless --overwrite is specified.

Note: Secrets cannot be copied because their values are not accessible via the GitHub API.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to parse source repository: %w", err)
			}

			if dstEnv == "" {
				dstEnv = srcEnv
			}

			ctx := context.Background()
			srcClient, err := gh.NewGitHubClientWithRepo(src)
			if err != nil {
				return fmt.Errorf("failed to create source GitHub client: %w", err)
			}

			// Fetch source environment settings
			srcEnvObj, err := gh.GetEnvironment(ctx, srcClient, src, srcEnv)
			if err != nil {
				return fmt.Errorf("failed to get source environment %q: %w", srcEnv, err)
			}

			// Build CreateUpdateEnvironment request from source environment
			envRequest := gh.EnvironmentToCreateUpdateRequest(srcEnvObj)

			// Fetch custom deployment branch policies
			branchPolicies, err := gh.ListDeploymentCustomBranchPolicies(ctx, srcClient, src, srcEnvObj)
			if err != nil {
				return fmt.Errorf("failed to list deployment branch policies for environment %q: %w", srcEnv, err)
			}

			srcVars, err := gh.ListEnvVariables(ctx, srcClient, src, srcEnv)
			if err != nil {
				return fmt.Errorf("failed to list variables from source environment %q: %w", srcEnv, err)
			}

			for _, dstArg := range args {
				dst, parseErr := parser.Repository(parser.RepositoryInput(dstArg))
				if parseErr != nil {
					return fmt.Errorf("failed to parse destination %q as owner/repo: %w", dstArg, parseErr)
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
						return fmt.Errorf("failed to create destination GitHub client for %q: %w", dstArg, err)
					}
				}

				if _, envErr := gh.CreateUpdateEnvironment(ctx, dstClient, dst, dstEnv, envRequest); envErr != nil {
					return fmt.Errorf("failed to create/update environment %q in %q: %w", dstEnv, dstArg, envErr)
				}
				fmt.Printf("Copied environment: %s -> %s (env: %s)\n", srcEnv, dstArg, dstEnv)

				for _, policy := range branchPolicies {
					if policy.Name == nil {
						continue
					}
					refType := "branch"
					if policy.Type != nil {
						refType = *policy.Type
					}
					if _, policyErr := gh.CreateDeploymentBranchPolicy(ctx, dstClient, dst, dstEnv, *policy.Name, refType); policyErr != nil {
						return fmt.Errorf("failed to copy deployment branch policy %q to environment %q in %q: %w", *policy.Name, dstEnv, dstArg, policyErr)
					}
					fmt.Printf("Copied deployment branch policy: %s -> %s (env: %s)\n", *policy.Name, dstArg, dstEnv)
				}

				for _, v := range srcVars {
					if copyErr := gh.CreateOrUpdateEnvVariable(ctx, dstClient, dst, dstEnv, v, overwrite); copyErr != nil {
						return fmt.Errorf("failed to copy variable %q to environment %q in %q: %w", v.Name, dstEnv, dstArg, copyErr)
					}
					fmt.Printf("Copied variable: %s -> %s (env: %s)\n", v.Name, dstArg, dstEnv)
				}
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&srcEnv, "src-env", "", "Source environment name (required)")
	f.StringVar(&dstEnv, "dst-env", "", "Destination environment name (defaults to --src-env)")
	f.StringVar(&dstHost, "dst-host", "", "Host to apply to destination arguments that do not specify one (e.g., github.com)")
	f.BoolVar(&overwrite, "overwrite", false, "Overwrite existing variables at destination (used with --with-variables)")

	_ = cmd.MarkFlagRequired("src-env")

	return cmd
}
