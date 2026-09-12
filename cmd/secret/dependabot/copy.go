package dependabot

import (
	"context"
	"fmt"
	"time"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	dependabotcopy "github.com/srz-zumix/gh-secret-kit/internal/dependabot"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the secret dependabot copy command
func NewCopyCmd() *cobra.Command {
	var config dependabotcopy.CopyConfig
	var scope string
	var dstApp string
	var timeout string

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...]",
		Short: "Copy Dependabot secrets from a source repository to one or more destinations",
		Long: `Copy Dependabot secrets from a source repository to one or more destinations.

Dependabot secret values cannot be read through the GitHub API, and they are only
exposed to a workflow run that Dependabot itself triggered; a run started by a user
is given no Dependabot secret at all. Dependabot also only reads its configuration
from the default branch. The copy therefore commits a temporary copy workflow to a
temporary branch, makes that branch the default one, registers the destination
tokens as temporary Dependabot secrets, and commits a temporary Dependabot
configuration, which makes Dependabot check for updates immediately. The branch
Dependabot pushes runs the copy workflow with the Dependabot secrets. The values
never reach the local machine, and every temporary change is reverted once the copy
finishes.

Use --scope to select which secrets are copied and at which level they are
written:

  - repo (default): repository Dependabot secrets of --repo
  - org:            organization Dependabot secrets of the source owner

Each destination argument is [host/]owner/repo, or [host/]org when --scope is org.
Destinations without a host use the source host. Use --dst-app to write the
values to a different destination secret store.

Requirements:

  - Dependabot must be enabled for the source repository.
  - The command temporarily changes the default branch of the source repository,
    so admin permission is required.
  - The pull request Dependabot opens is closed when the branch it pushed and the
    temporary branch are deleted during the cleanup.
  - The destination host must be reachable from the GitHub Actions runner.
  - The Dependabot check is scheduled by GitHub, so the copy usually takes
    several minutes; raise --timeout when it runs out.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch migrator.SecretScope(scope) {
			case migrator.SecretScopeRepo, migrator.SecretScopeOrg:
				config.Scope = migrator.SecretScope(scope)
			default:
				return fmt.Errorf("invalid --scope %q: expected repo or org", scope)
			}
			config.DestinationApp = migrator.SecretApp(dstApp)
			if err := parser.ValidateTokenSecretName(config.TokenSecretName); err != nil {
				return err
			}
			duration, err := time.ParseDuration(timeout)
			if err != nil {
				return fmt.Errorf("invalid --timeout %q: %w", timeout, err)
			}
			if duration <= 0 {
				return fmt.Errorf("invalid --timeout %q: expected a positive duration", timeout)
			}
			config.Timeout = duration
			config.Destinations = args
			return dependabotcopy.RunCopy(context.Background(), &config)
		},
		Args: cobra.MinimumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&scope, "scope", string(migrator.SecretScopeRepo), "Secret scope to copy: repo or org")
	cmdutil.StringEnumFlag(cmd, &dstApp, "dst-app", "", string(migrator.SecretAppDependabot), migrator.SecretAppValues(), "Destination secret store")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to copy (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.ExcludeSecrets, "exclude-secrets", []string{}, "Secret names to exclude from the copy (comma-separated or repeated flag)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Overwrite existing secrets at destination")
	f.StringVar(&config.DestinationToken, "dst-token", "", "PAT or token for the destination host (defaults to the local gh authentication)")
	f.StringVar(&config.TokenSecretName, "token-secret-name", types.DefaultDependabotCopyTokenSecretName, "Base name of the temporary Dependabot secret holding the destination token")
	f.StringVar(&config.Branch, "branch", "", "Temporary branch made the default branch while the copy runs (defaults to a unique name)")
	f.StringVar(&timeout, "timeout", types.DefaultDependabotCopyTimeout, "How long to wait for Dependabot to run the copy (e.g., 30m, 1h)")
	f.BoolVar(&config.KeepWorkflow, "keep-workflow", false, "Keep the temporary branch, the Dependabot secrets and the branch Dependabot pushed after the copy instead of removing them")

	return cmd
}
