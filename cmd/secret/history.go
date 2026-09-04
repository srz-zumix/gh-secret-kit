package secret

import (
	"context"
	"fmt"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/pkg/audit"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewHistoryCmd creates the secret history command
func NewHistoryCmd() *cobra.Command {
	var repo, owner, envName, secretName, since, until, order string
	var scopes, secretTypes []string
	var limit int
	var exporter cmdutil.Exporter

	cmd := &cobra.Command{
		Use:     "history",
		Short:   "Show the change history of GitHub Actions secrets",
		Aliases: []string{"log"},
		Long: `Show the create, update and remove history of secrets from the organization audit log.

The audit log API is organization scoped, so the history is read from the organization
that owns --repo (or from --owner) and repository scoped events are filtered by repository.
Secret values are never recorded in the audit log; only the secret name and the actor are.

Requires GitHub Enterprise Cloud or GitHub Enterprise Server, organization owner
permission, and a token with the read:audit_log scope. The audit log retains events
for a limited period (180 days on GitHub Enterprise Cloud).

The audit log search accepts a single action per query, so one request is issued for
each combination of --scope, --secret-type and operation, and the results are merged.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// RepositoryInput(repo) and RepositoryOwnerWithHost(owner) are both no-ops
			// when their input is empty, so parser.Repository falls back to the current
			// repository when neither flag is set.
			r, err := parser.Repository(
				parser.RepositoryInput(repo),
				parser.RepositoryOwnerWithHost(owner),
			)
			if err != nil {
				return fmt.Errorf("failed to parse repository: %w", err)
			}

			ctx := context.Background()
			client, err := gh.NewGitHubClientWithRepo(r)
			if err != nil {
				return fmt.Errorf("failed to create GitHub client: %w", err)
			}

			entries, err := audit.SecretHistory(ctx, client, audit.SecretHistoryOptions{
				Repo:        r,
				Scopes:      scopes,
				SecretTypes: secretTypes,
				SecretName:  secretName,
				Environment: envName,
				Since:       since,
				Until:       until,
				Order:       order,
				Limit:       limit,
			})
			if err != nil {
				return fmt.Errorf("failed to get secret history for %q: %w", r.Owner, err)
			}

			headers := []string{"TIMESTAMP", "ACTION"}
			if r.Name == "" {
				headers = append(headers, "REPO")
			}
			headers = append(headers, "ENVIRONMENT_NAME", "KEY", "ACTOR")

			renderer := render.NewRenderer(exporter)
			return renderer.RenderAuditEntries(entries, headers)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Repository to show the secret history for (e.g., owner/repo; defaults to current repository). Mutually exclusive with --owner")
	f.StringVar(&owner, "owner", "", "Organization/owner to show the secret history for, without filtering by repository. Mutually exclusive with --repo")
	f.StringVar(&envName, "env", "", "Environment name to filter environment secret events by (defaults to all environments)")
	f.StringVar(&secretName, "secret", "", "Secret name to filter events by (defaults to all secrets)")
	f.StringSliceVar(&scopes, "scope", []string{}, "Secret scopes to show: repo, org, or environment (comma-separated or repeated flag; defaults to all)")
	f.StringSliceVar(&secretTypes, "secret-type", []string{}, "Secret stores to show: actions, dependabot, or codespaces (comma-separated or repeated flag; defaults to all)")
	f.StringVar(&since, "since", "", "Show events created on or after this date (YYYY-MM-DD or RFC3339)")
	f.StringVar(&until, "until", "", "Show events created on or before this date (YYYY-MM-DD or RFC3339)")
	f.StringVar(&order, "order", "desc", "Sort order of the events: asc or desc")
	f.IntVar(&limit, "limit", 100, "Maximum number of events to show (0 or negative for unlimited)")
	cmd.MarkFlagsMutuallyExclusive("repo", "owner")

	cmdutil.AddFormatFlags(cmd, &exporter)

	return cmd
}
