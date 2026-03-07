package org

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
)

// NewCheckCmd creates the org check command
func NewCheckCmd() *cobra.Command {
	var config workflow.CheckConfig
	config.Scope = migrate.SecretScopeOrg

	cmd := &cobra.Command{
		Use:   "check",
		Short: "Check whether organization secrets have been migrated",
		Long: `Compare organization secrets between source and destination organizations.
For each secret in the source, check whether the corresponding secret
(after applying any --rename mappings) exists in the destination.
Exits with a non-zero status if any secrets have not been migrated yet.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return workflow.RunCheck(context.Background(), &config)
		},
		Args: cobra.NoArgs,
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "src", "s", "", "Source organization name")
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination organization name")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to check (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.StringVar(&config.DestinationToken, "dst-token", "", "PAT or token for the destination (required if destination is on a different host)")
	f.StringVar(&config.DestinationHost, "dst-host", "", "GitHub host for the destination (defaults to source host)")
	_ = cmd.Flags().MarkHidden("dst-token")

	_ = cmd.MarkFlagRequired("src")
	_ = cmd.MarkFlagRequired("dst")

	return cmd
}
