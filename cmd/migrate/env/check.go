package env

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
)

// NewCheckCmd creates the env check command
func NewCheckCmd() *cobra.Command {
	var config workflow.CheckConfig
	config.Scope = migrate.SecretScopeEnv

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check whether environment secrets have been migrated",
		Long: `Compare environment secrets between source and destination repositories.
For each secret in the source environment, check whether the corresponding secret
(after applying any --rename mappings) exists in the destination environment.
Exits with a non-zero status if any secrets have not been migrated yet.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.RunCheck(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination repository (e.g., owner/repo)")
	f.StringVar(&config.SourceEnv, "src-env", "", "Source environment name")
	f.StringVar(&config.DestinationEnv, "dst-env", "", "Destination environment name")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to check (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.StringVar(&config.DestinationToken, "dst-token", "", "PAT or token for the destination (required if destination is on a different host)")
	f.StringVar(&config.DestinationHost, "dst-host", "", "GitHub host for the destination (defaults to source repository host)")

	_ = cmd.MarkFlagRequired("dst")
	_ = cmd.MarkFlagRequired("src-env")
	_ = cmd.MarkFlagRequired("dst-env")

	return cmd
}
