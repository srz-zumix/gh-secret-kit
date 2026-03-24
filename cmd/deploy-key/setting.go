package deploykey

import (
	"context"
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewSettingCmd creates the deploy-key setting command
func NewSettingCmd() *cobra.Command {
	var owner string
	var set string // "enable", "disable", or "" (get only)

	cmd := &cobra.Command{
		Use:   "setting [org]",
		Short: "Get or set the deploy keys setting for an organization",
		Long: `Get or set whether deploy keys are enabled for repositories in an organization.

When --set is omitted, the current setting is printed.
When --set enable is given, deploy keys are enabled.
When --set disable is given, deploy keys are disabled.

The organization is specified as an argument (e.g., myorg or HOST/myorg).
If omitted, the owner of the current repository is used.
Use --owner to specify the organization via a flag instead.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				owner = args[0]
			}
			r, err := parser.Repository(parser.RepositoryOwnerWithHost(owner))
			if err != nil {
				return fmt.Errorf("failed to parse organization: %w", err)
			}

			ctx := context.Background()
			client, err := gh.NewGitHubClientWithRepo(r)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			switch set {
			case "":
				enabled, err := gh.GetOrgDeployKeysEnabled(ctx, client, r)
				if err != nil {
					return fmt.Errorf("failed to get deploy keys setting for organization %s: %w", r.Owner, err)
				}
				if enabled {
					fmt.Println("true")
				} else {
					fmt.Println("false")
				}
			case "enable":
				if _, err := gh.SetOrgDeployKeysEnabled(ctx, client, r, true); err != nil {
					return fmt.Errorf("failed to enable deploy keys for organization %s: %w", r.Owner, err)
				}
				logger.Info(fmt.Sprintf("Deploy keys enabled for organization: %s", r.Owner))
			case "disable":
				if _, err := gh.SetOrgDeployKeysEnabled(ctx, client, r, false); err != nil {
					return fmt.Errorf("failed to disable deploy keys for organization %s: %w", r.Owner, err)
				}
				logger.Info(fmt.Sprintf("Deploy keys disabled for organization: %s", r.Owner))
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&owner, "owner", "", "Organization (e.g., owner or HOST/owner; defaults to current repository owner)")
	cmdutil.StringEnumFlag(cmd, &set, "set", "", "", []string{"enable", "disable"}, "Set deploy keys setting (omit to get current value)")

	return cmd
}
