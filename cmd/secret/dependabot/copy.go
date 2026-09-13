package dependabot

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	dependabotcopy "github.com/srz-zumix/gh-secret-kit/internal/dependabot"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the secret dependabot copy command.
func NewCopyCmd() *cobra.Command {
	var config dependabotcopy.CopyConfig
	var scope string
	var dstApp string
	var timeout string

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...]",
		Short: "Copy Dependabot secrets from a source repository to one or more destinations",
		Long: `Copy Dependabot secrets using a temporary workflow triggered by a Dependabot
push. Secret values stay on the runner and are copied with gh secret set.
The local command manages temporary token secrets directly through the GitHub
API, without plaintext token files.

Use --scope repo (default) or org to select repository secrets or organization
secrets shared with the source repository. Each destination is [host/]owner/repo,
or [host/]org for organization scope; omitted hosts use the source host.
Use --dst-app to change the destination store, --secrets and --exclude-secrets to
select names, --rename to rename them, and --overwrite to replace existing values.
Customize the workflow with --workflow-name and --runner-label. Use --dryrun to
print the generated copy workflow YAML to stdout without making changes.

The source requires admin permission, Dependabot version updates, and GitHub
Actions. The runner needs Bash, gh CLI, and network access to destination hosts.
The source default branch is temporarily changed and always restored.
--keep-workflow preserves temporary resources for inspection. Dependabot update
checks may take several minutes; adjust --timeout if needed.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			switch migrator.SecretScope(scope) {
			case migrator.SecretScopeRepo, migrator.SecretScopeOrg:
				config.Scope = migrator.SecretScope(scope)
			default:
				return fmt.Errorf("invalid --scope %q: expected repo or org", scope)
			}
			config.DestinationApp = migrator.SecretApp(dstApp)
			if err := parser.ValidateTokenSecretName(config.TokenSecretName); err != nil {
				return fmt.Errorf("invalid --token-secret-name: %w", err)
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
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			if err := dependabotcopy.RunCopy(ctx, &config); err != nil {
				return fmt.Errorf("failed to copy Dependabot secrets: %w", err)
			}
			return nil
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
	f.BoolVarP(&config.DryRun, "dryrun", "n", false, "Print the generated copy workflow YAML to stdout without making changes")
	f.StringVar(&config.DestinationToken, "dst-token", "", "PAT or token for the destination host (defaults to the local gh authentication)")
	f.StringVar(&config.TokenSecretName, "token-secret-name", types.DefaultDependabotCopyTokenSecretName, "Base name of the temporary Dependabot secret holding the destination token")
	f.StringVar(&config.Branch, "branch", "", "Temporary branch made the default branch while the copy runs (defaults to a unique name)")
	f.StringVar(&config.WorkflowName, "workflow-name", types.DefaultDependabotCopyWorkflowName, "Workflow file name (without extension) of the generated copy workflow")
	f.StringVar(&config.RunnerLabel, "runner-label", types.DefaultCopyRunnerLabel, "Runner label for runs-on of the generated copy workflow")
	f.StringVar(&timeout, "timeout", types.DefaultDependabotCopyTimeout, "How long to wait for Dependabot and the copy workflow to finish (e.g., 30m, 1h)")
	f.BoolVar(&config.KeepWorkflow, "keep-workflow", false, "Keep temporary branches, Dependabot secrets, pull requests, and run history; still restore the default branch")

	return cmd
}
