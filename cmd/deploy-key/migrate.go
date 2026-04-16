package deploykey

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewMigrateCmd creates the deploy-key migrate command
func NewMigrateCmd() *cobra.Command {
	var repo string
	var excludePatterns []string

	cmd := &cobra.Command{
		Use:   "migrate <dst>",
		Short: "Migrate deploy keys to a destination repository",
		Long: `Migrate all deploy keys from a source repository to a destination repository.

The source repository is specified via --repo (defaults to the current repository).
The destination argument must be in [HOST/]OWNER/REPO format.

Note: GitHub does not allow the same public key to be registered as a deploy key
in multiple repositories on the same host. This command is intended for migrating
keys across different hosts (e.g., github.com to a GitHub Enterprise Server instance).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			src, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to parse source repository: %w", err)
			}

			dst, err := parser.Repository(parser.RepositoryInput(args[0]))
			if err != nil {
				return fmt.Errorf("failed to parse destination %q as owner/repo: %w", args[0], err)
			}
			if dst.Host == "" && src.Host != "" {
				dst.Host = src.Host
			}

			ctx := context.Background()
			srcClient, err := gh.NewGitHubClientWithRepo(src)
			if err != nil {
				return fmt.Errorf("failed to create source GitHub client: %w", err)
			}

			dstClient := srcClient
			if dst.Host != src.Host {
				dstClient, err = gh.NewGitHubClientWithRepo(dst)
				if err != nil {
					return fmt.Errorf("failed to create destination GitHub client: %w", err)
				}
			}

			keys, err := gh.ListDeployKeys(ctx, srcClient, src)
			if err != nil {
				return fmt.Errorf("failed to list deploy keys for %s/%s: %w", src.Owner, src.Name, err)
			}

			for _, key := range keys {
				title := key.GetTitle()
				if shouldExcludeKey(title, excludePatterns) {
					fmt.Printf("Skipped deploy key: %q (matched exclude pattern)\n", title)
					continue
				}
				keyStr := key.GetKey()
				readOnly := key.GetReadOnly()
				if _, copyErr := gh.CreateDeployKey(ctx, dstClient, dst, title, keyStr, readOnly); copyErr != nil {
					return fmt.Errorf("failed to migrate deploy key %q to %s/%s: %w", title, dst.Owner, dst.Name, copyErr)
				}
				fmt.Printf("Migrated deploy key: %q -> %s/%s\n", title, dst.Owner, dst.Name)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	cmdflags.NonEmptyStringSliceVar(cmd, &excludePatterns, "exclude", []string{}, "Exclude deploy keys whose title contains the specified string (comma-separated or repeated flag)")

	return cmd
}

// shouldExcludeKey returns true if the key title contains any of the exclude patterns.
func shouldExcludeKey(title string, excludeNames []string) bool {
	for _, pattern := range excludeNames {
		if strings.Contains(title, pattern) {
			return true
		}
	}
	return false
}
