package repo

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
)

// NewCheckCmd creates the repo check command
func NewCheckCmd() *cobra.Command {
	var config workflow.CheckConfig
	config.Scope = migrate.SecretScopeRepo

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check whether repository secrets have been migrated",
		Long: `Compare repository secrets between source and destination repositories.
For each secret in the source, check whether the corresponding secret
(after applying any --rename mappings) exists in the destination.
Exits with a non-zero status if any secrets have not been migrated yet.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.RunCheck(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination repository (e.g., owner/repo)")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to check (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.StringVar(&config.DestinationToken, "dst-token", "", "PAT or token for the destination (required if destination is on a different host)")
	f.StringVar(&config.DestinationHost, "dst-host", "", "GitHub host for the destination (defaults to source repository host)")
	_ = cmd.Flags().MarkHidden("dst-token")

	_ = cmd.MarkFlagRequired("dst")

	return cmd
}
