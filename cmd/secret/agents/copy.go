package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	agentscopy "github.com/srz-zumix/gh-secret-kit/internal/agents"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// NewCopyCmd creates the secret agents copy command
func NewCopyCmd() *cobra.Command {
	var config agentscopy.CopyConfig
	var scope string
	var dstApp string
	var timeout string

	cmd := &cobra.Command{
		Use:   "copy <dst> [dst...]",
		Short: "Copy Copilot coding agent secrets from a source repository to one or more destinations",
		Long: `Copy GitHub Copilot coding agent (Agents) secrets from a source repository to one
or more destinations.

Agents secret values cannot be read through the GitHub API. They are only exposed
as environment variables inside the Copilot coding agent environment, and only
Agents secrets are exposed there. Copilot also only runs the setup steps workflow
from the default branch. The copy therefore commits a temporary
copilot-setup-steps workflow to a temporary branch, makes that branch the default
one, registers the destination tokens as temporary Agents secrets, starts a
Copilot coding agent session, and lets the setup steps perform the copy. The
values never reach the local machine, and every temporary change is reverted once
the copy finishes.

Use --scope to select which secrets are copied and at which level they are
written:

  - repo (default): repository Agents secrets of --repo
  - org:            organization Agents secrets of the source owner

Each destination argument is [host/]owner/repo, or [host/]org when --scope is org.
Destinations without a host use the source host. Use --dst-app to write the
values to a different destination secret store.

Requirements:

  - The source must be on github.com; the Copilot coding agent is not available
    on GitHub Enterprise Server.
  - The Copilot coding agent must be enabled for the source repository, and the
    authenticated user needs a Copilot license.
  - The command temporarily changes the default branch of the source repository,
    so admin permission is required.
  - The pull request the agent opens is based on the temporary branch and is
    closed when that branch is deleted during the cleanup.
  - The destination host must be reachable from the agent environment.`,
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
			return agentscopy.RunCopy(context.Background(), &config)
		},
		Args: cobra.MinimumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&config.Source, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&scope, "scope", string(migrator.SecretScopeRepo), "Secret scope to copy: repo or org")
	cmdutil.StringEnumFlag(cmd, &dstApp, "dst-app", "", string(migrator.SecretAppAgents), migrator.SecretAppValues(), "Destination secret store")
	f.StringSliceVar(&config.Secrets, "secrets", []string{}, "Specific secret names to copy (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&config.ExcludeSecrets, "exclude-secrets", []string{}, "Secret names to exclude from the copy (comma-separated or repeated flag)")
	f.StringSliceVar(&config.Rename, "rename", []string{}, "Rename mapping in OLD_NAME=NEW_NAME format (repeatable)")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Overwrite existing secrets at destination")
	f.StringVar(&config.DestinationToken, "dst-token", "", "PAT or token for the destination host (defaults to the local gh authentication)")
	f.StringVar(&config.TokenSecretName, "token-secret-name", types.DefaultAgentsCopyTokenSecretName, "Base name of the temporary Agents secret holding the destination token")
	f.StringVar(&config.Branch, "branch", "", "Temporary branch made the default branch while the copy runs (defaults to a unique name)")
	f.StringVar(&config.Prompt, "prompt", types.DefaultAgentsCopyPrompt, "Task description passed to the Copilot coding agent")
	f.StringVar(&timeout, "timeout", types.DefaultAgentsCopyTimeout, "How long to wait for the agent environment to run the copy (e.g., 30m, 1h)")
	f.BoolVar(&config.KeepWorkflow, "keep-workflow", false, "Keep the temporary branch and Agents secrets after the copy instead of removing them")

	return cmd
}
