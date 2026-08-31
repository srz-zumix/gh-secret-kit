package secret

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the secret copy command
func NewCopyCmd() *cobra.Command {
	var config workflow.CopyConfig
	var scope string

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...]",
		Short: "Copy secrets from a source repository to one or more destinations",
		Long: `Copy GitHub Actions secrets from a source repository to one or more destinations.

Secret values cannot be read through the GitHub API, so the copy is performed by a
workflow generated in the source repository and triggered via workflow_dispatch. The
token for each destination host is taken from the local gh authentication (or from
--dst-token) and registered as a temporary source repository secret, so a
GitHub-hosted runner can reach the destination and no self-hosted runner is needed.

Use --scope to select which secrets are copied:

  - repo (default): repository secrets of --repo
  - org:            organization secrets visible to --repo
  - env:            environment secrets of --src-env

Each destination argument is [host/]owner/repo, or [host/]org when --scope is org.
Destinations without a host use the source host.

Once the workflow run finishes, the temporary branch, the temporary token secrets,
and the workflow run history are deleted.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch migrator.SecretScope(scope) {
			case migrator.SecretScopeRepo, migrator.SecretScopeOrg, migrator.SecretScopeEnv:
				config.Scope = migrator.SecretScope(scope)
			default:
				return fmt.Errorf("invalid --scope %q: expected repo, org, or env", scope)
			}
			if err := parser.ValidateTokenSecretName(config.TokenSecretName); err != nil {
				return err
			}
			config.Destinations = args
			return workflow.RunCopy(context.Background(), &config)
		},
		Args: cobra.MinimumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&scope, "scope", string(migrator.SecretScopeRepo), "Secret scope to copy: repo, org, or env")
	f.StringVar(&config.SourceEnv, "src-env", "", "Source environment name (required with --scope env)")
	f.StringVar(&config.DestinationEnv, "dst-env", "", "Destination environment name (defaults to --src-env)")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to copy (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.ExcludeSecrets, "exclude-secrets", []string{}, "Secret names to exclude from the copy (comma-separated or repeated flag)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Overwrite existing secrets at destination")
	f.StringVar(&config.DestinationToken, "dst-token", "", "PAT or token for the destination host (defaults to the local gh authentication)")
	f.StringVar(&config.TokenSecretName, "token-secret-name", types.DefaultCopyTokenSecretName, "Base name of the temporary source repository secret holding the destination token")
	f.StringVar(&config.RunnerLabel, "runner-label", types.DefaultCopyRunnerLabel, "Runner label for runs-on of the generated workflow")
	f.StringVar(&config.WorkflowName, "workflow-name", types.DefaultCopyWorkflowName, "Workflow file name (without extension) of the generated workflow")
	f.StringVar(&config.Branch, "branch", "", "Temporary branch name (defaults to a unique name derived from a timestamp)")
	f.StringVar(&config.Timeout, "timeout", "10m", "Timeout duration when waiting for workflow completion (e.g., 5m, 1h)")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Temporarily unarchive the source repository if it is archived, then re-archive after the copy")

	return cmd
}
