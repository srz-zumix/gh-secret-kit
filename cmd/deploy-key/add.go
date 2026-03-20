package deploykey

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewAddCmd creates the deploy-key add command
func NewAddCmd() *cobra.Command {
	var repo string
	var title string
	var keyFile string
	var readOnly bool

	cmd := &cobra.Command{
		Use:   "add [public-key]",
		Short: "Add a deploy key to a repository",
		Long: `Add a deploy key to a repository.

The public key can be provided as a string argument or read from a file via --key-file.
Use --title to set a label for the key.
Use --read-only to create a read-only deploy key (default: false, i.e., read-write).

The repository is specified via --repo (defaults to the current repository).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var keyStr string
			if keyFile != "" && len(args) == 1 {
				return fmt.Errorf("provide the public key either as an argument or via --key-file, not both")
			} else if keyFile != "" {
				data, err := os.ReadFile(keyFile)
				if err != nil {
					return fmt.Errorf("failed to read key file %q: %w", keyFile, err)
				}
				keyStr = strings.TrimSpace(string(data))
				if keyStr == "" {
					return fmt.Errorf("key file %q is empty", keyFile)
				}
			} else if len(args) == 1 {
				keyStr = args[0]
			} else {
				return fmt.Errorf("a public key must be provided as an argument or via --key-file")
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

			key, err := gh.CreateDeployKey(ctx, client, r, title, keyStr, readOnly)
			if err != nil {
				return fmt.Errorf("failed to add deploy key to %s/%s: %w", r.Owner, r.Name, err)
			}

			renderer := render.NewRenderer(nil)
			return renderer.RenderDeployKey(key, nil)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository to add the deploy key to (e.g., owner/repo; defaults to current repository)")
	f.StringVarP(&title, "title", "t", "", "Title (label) for the deploy key")
	f.StringVarP(&keyFile, "key-file", "f", "", "Path to the public key file")
	f.BoolVar(&readOnly, "read-only", false, "Create a read-only deploy key (default: false, i.e., read-write)")

	return cmd
}
