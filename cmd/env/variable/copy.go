package variable

import (
	"context"
	"fmt"
	"slices"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the env variable copy command
func NewCopyCmd() *cobra.Command {
	var repo, dstHost, srcEnv, dstEnv string
	var variables []string
	var overwrite bool

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...] [flags]",
		Short: "Copy environment variables from a source environment to one or more destinations",
		Long: `Copy GitHub Actions environment variables from a source repository environment to
one or more destination repository environments.

The source repository is specified via --repo (defaults to the current repository).
The source environment is specified via --src-env (required).
The destination environment is specified via --dst-env (defaults to the value of --src-env).
Each destination argument must be in owner/repo format.
Use --dst-host to apply a host to destination arguments that do not specify one.`,
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

			vars, err := gh.ListEnvVariables(ctx, srcClient, src, srcEnv)
			if err != nil {
				return fmt.Errorf("failed to list variables from source environment %q: %w", srcEnv, err)
			}

			for _, dstArg := range args {
				dst, err := parser.Repository(parser.RepositoryInput(dstArg))
				if err != nil {
					return fmt.Errorf("failed to parse destination %q: expected owner/repo format", dstArg)
				}
				if dstHost != "" && dst.Host == "" {
					dst.Host = dstHost
				}

				dstClient := srcClient
				if src.Host != dst.Host {
					dstClient, err = gh.NewGitHubClientWithRepo(dst)
					if err != nil {
						return fmt.Errorf("failed to create destination GitHub client for %q: %w", dstArg, err)
					}
				}

				for _, v := range vars {
					if len(variables) > 0 && !slices.Contains(variables, v.Name) {
						continue
					}
					err := gh.CreateOrUpdateEnvVariable(ctx, dstClient, dst, dstEnv, v, overwrite)
					if err != nil {
						return fmt.Errorf("failed to copy variable %q to %q (env: %s): %w", v.Name, dstArg, dstEnv, err)
					}
					fmt.Printf("Copied variable: %s -> %s (env: %s)\n", v.Name, dstArg, dstEnv)
				}
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&srcEnv, "src-env", "", "Source environment name")
	f.StringVar(&dstEnv, "dst-env", "", "Destination environment name (defaults to --src-env)")
	f.StringVar(&dstHost, "dst-host", "", "Host to apply to destination arguments that do not specify one (e.g., github.com)")
	f.StringSliceVar(&variables, "variables", []string{}, "Specific variable names to copy (comma-separated or repeated flag; defaults to all)")
	f.BoolVar(&overwrite, "overwrite", false, "Overwrite existing variables at destination")

	_ = cmd.MarkFlagRequired("src-env")

	return cmd
}
