package codespaces

import (
	"context"
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/codespaces"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the secret codespaces copy command
func NewCopyCmd() *cobra.Command {
	var config codespaces.CopyConfig
	var scope string
	var dstApp string

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...]",
		Short: "Copy Codespaces secrets from a source repository to one or more destinations",
		Long: `Copy GitHub Codespaces development environment secrets from a source repository to
one or more destinations.

Codespaces secret values cannot be read through the GitHub API. They are only
exposed as environment variables inside a running codespace, so the copy creates
an ephemeral codespace on the source repository and runs the copy from within it.
The values never reach the local machine: only the destination tokens are sent
into the codespace, and the codespace is deleted once the copy finishes.

Use --scope to select which secrets are copied and at which level they are
written:

  - repo (default): repository Codespaces secrets of --repo
  - org:            organization Codespaces secrets of the source owner

Add --include-user-secrets to also copy the Codespaces secrets of the
authenticated user that the source repository has access to.

Each destination argument is [host/]owner/repo, or [host/]org when --scope is org.
Destinations without a host use the source host. Use --dst-app to write the
values to a different destination secret store.

Requirements:

  - The source must be on github.com; Codespaces is not available on GitHub
    Enterprise Server.
  - The gh authentication must have the codespace scope
    (gh auth refresh -s codespace).
  - Creating a codespace consumes Codespaces compute and storage quota.
  - The destination host must be reachable from the codespace.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch migrator.SecretScope(scope) {
			case migrator.SecretScopeRepo, migrator.SecretScopeOrg:
				config.Scope = migrator.SecretScope(scope)
			default:
				return fmt.Errorf("invalid --scope %q: expected repo or org", scope)
			}
			config.DestinationApp = migrator.SecretApp(dstApp)
			if err := parser.ValidateTokenSecretName(config.TokenEnvName); err != nil {
				return err
			}
			config.Destinations = args
			return codespaces.RunCopy(context.Background(), &config)
		},
		Args: cobra.MinimumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&scope, "scope", string(migrator.SecretScopeRepo), "Secret scope to copy: repo or org")
	cmdutil.StringEnumFlag(cmd, &dstApp, "dst-app", "", string(migrator.SecretAppCodespaces), migrator.SecretAppValues(), "Destination secret store")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to copy (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.ExcludeSecrets, "exclude-secrets", []string{}, "Secret names to exclude from the copy (comma-separated or repeated flag)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.BoolVar(&config.IncludeUserSecrets, "include-user-secrets", false, "Also copy the Codespaces secrets of the authenticated user")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Overwrite existing secrets at destination")
	f.StringVar(&config.DestinationToken, "dst-token", "", "PAT or token for the destination host (defaults to the local gh authentication)")
	f.StringVar(&config.TokenEnvName, "token-env-name", types.DefaultCodespacesCopyTokenEnvName, "Base name of the environment variable holding the destination token inside the codespace")
	f.StringVar(&config.Branch, "branch", "", "Source repository branch the codespace is created from (defaults to the default branch)")
	f.StringVar(&config.DevcontainerPath, "devcontainer-path", "", "Path to the devcontainer.json used for the codespace")
	f.StringVar(&config.Machine, "machine", "", "Machine type of the codespace (defaults to the smallest machine type available for the source repository)")
	f.StringVar(&config.IdleTimeout, "idle-timeout", types.DefaultCodespacesCopyIdleTimeout, "Allowed inactivity before the codespace is stopped (e.g., 5m, 1h)")
	f.StringVar(&config.RetentionPeriod, "retention-period", types.DefaultCodespacesCopyRetentionPeriod, "Allowed time after shutting down before the codespace is deleted (e.g., 1h, 72h)")
	f.BoolVar(&config.KeepCodespace, "keep-codespace", false, "Keep the codespace after the copy instead of deleting it")

	return cmd
}
