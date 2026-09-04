package audit

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// Secret scopes supported by the audit log secret history.
const (
	ScopeRepo        = "repo"
	ScopeOrg         = "org"
	ScopeEnvironment = "environment"
)

// Secret stores supported by the audit log secret history.
const (
	SecretTypeActions    = "actions"
	SecretTypeDependabot = "dependabot"
	SecretTypeCodespaces = "codespaces"
)

// Sort orders supported by the audit log API.
const (
	OrderAsc  = "asc"
	OrderDesc = "desc"
)

// Scopes lists the supported secret scopes.
var Scopes = []string{ScopeRepo, ScopeOrg, ScopeEnvironment}

// SecretTypes lists the supported secret stores.
var SecretTypes = []string{SecretTypeActions, SecretTypeDependabot, SecretTypeCodespaces}

// operations lists the audit log operations recorded for a secret.
var operations = []string{"create", "update", "remove"}

// dateFormats lists the accepted layouts for the --since and --until values.
var dateFormats = []string{time.RFC3339, "2006-01-02"}

// SecretHistoryOptions holds the filters used to build audit log queries.
type SecretHistoryOptions struct {
	// Repo is the target repository. Only the owner is required; when Name is
	// set, repository and environment scoped events are filtered by repository.
	Repo repository.Repository
	// Scopes selects the secret scopes to query. Defaults to all scopes.
	Scopes []string
	// SecretTypes selects the secret stores to query. Defaults to all stores.
	SecretTypes []string
	// SecretName filters entries by secret name.
	SecretName string
	// Environment filters environment scoped entries by environment name.
	Environment string
	// Since and Until filter entries by event date ("YYYY-MM-DD" or RFC3339).
	Since string
	Until string
	// Order is the sort order of the returned entries: "asc" or "desc".
	Order string
	// Limit caps the number of returned entries. Zero or negative means unlimited.
	Limit int
}

// query is a single audit log request derived from the requested filters.
type query struct {
	action string
	// integration is the expected value of the "integration" field for
	// integration secret events. It is empty for Actions secret events.
	integration string
}

// SecretHistory returns audit log entries for secret create, update and remove
// events of the organization that owns opts.Repo. The audit log API accepts a
// single action qualifier per request, so one request is issued per action and
// the results are merged, filtered and sorted afterwards.
func SecretHistory(ctx context.Context, g *gh.GitHubClient, opts SecretHistoryOptions) ([]*gh.AuditEntry, error) {
	queries, err := opts.queries()
	if err != nil {
		return nil, err
	}
	createdPhrase, err := opts.createdPhrase()
	if err != nil {
		return nil, err
	}

	order := opts.Order
	if order == "" {
		order = OrderDesc
	}
	if order != OrderAsc && order != OrderDesc {
		return nil, fmt.Errorf("invalid order %q: must be %q or %q", opts.Order, OrderAsc, OrderDesc)
	}

	seen := make(map[string]struct{})
	entries := []*gh.AuditEntry{}
	for _, q := range queries {
		found, err := gh.GetAuditLog(ctx, g, opts.Repo, &gh.GetAuditLogOptions{
			Phrase:     opts.phrase(q, createdPhrase),
			Order:      order,
			MaxEntries: opts.Limit,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to get audit log entries for action %q: %w", q.action, err)
		}
		for _, entry := range found {
			if !opts.match(entry, q) {
				continue
			}
			if id := entry.GetDocumentID(); id != "" {
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
			}
			entries = append(entries, entry)
		}
	}

	sortEntries(entries, order)
	if opts.Limit > 0 && len(entries) > opts.Limit {
		entries = entries[:opts.Limit]
	}
	return entries, nil
}

// queries builds the audit log queries for the requested scopes and secret types.
func (o *SecretHistoryOptions) queries() ([]query, error) {
	scopes, err := normalizeValues(o.Scopes, Scopes, "scope")
	if err != nil {
		return nil, err
	}
	secretTypes, err := normalizeValues(o.SecretTypes, SecretTypes, "secret type")
	if err != nil {
		return nil, err
	}

	queries := []query{}
	for _, scope := range scopes {
		for _, secretType := range secretTypes {
			// Environment secrets exist for Actions only.
			if scope == ScopeEnvironment && secretType != SecretTypeActions {
				continue
			}
			for _, operation := range operations {
				if secretType == SecretTypeActions {
					queries = append(queries, query{action: fmt.Sprintf("%s.%s_actions_secret", scope, operation)})
					continue
				}
				queries = append(queries, query{
					action:      fmt.Sprintf("%s.%s_integration_secret", scope, operation),
					integration: secretType,
				})
			}
		}
	}
	return queries, nil
}

// phrase builds the audit log search phrase for a single query.
func (o *SecretHistoryOptions) phrase(q query, createdPhrase string) string {
	terms := []string{"action:" + q.action}
	// Organization secret events are not bound to a repository, so the repo
	// qualifier is only applied to repository and environment scoped events.
	if o.Repo.Name != "" && !strings.HasPrefix(q.action, ScopeOrg+".") {
		terms = append(terms, fmt.Sprintf("repo:%s/%s", o.Repo.Owner, o.Repo.Name))
	}
	if createdPhrase != "" {
		terms = append(terms, createdPhrase)
	}
	return strings.Join(terms, " ")
}

// createdPhrase builds the "created:" qualifier from the Since and Until options.
func (o *SecretHistoryOptions) createdPhrase() (string, error) {
	since, err := normalizeDate(o.Since, "since")
	if err != nil {
		return "", err
	}
	until, err := normalizeDate(o.Until, "until")
	if err != nil {
		return "", err
	}
	switch {
	case since != "" && until != "":
		return fmt.Sprintf("created:%s..%s", since, until), nil
	case since != "":
		return "created:>=" + since, nil
	case until != "":
		return "created:<=" + until, nil
	}
	return "", nil
}

// match reports whether an entry passes the filters that the audit log search
// phrase cannot express.
func (o *SecretHistoryOptions) match(entry *gh.AuditEntry, q query) bool {
	if entry == nil {
		return false
	}
	if q.integration != "" && !strings.EqualFold(gh.AuditEntryStringField(entry, "integration"), q.integration) {
		return false
	}
	if o.SecretName != "" && !strings.EqualFold(gh.AuditEntryStringField(entry, "key"), o.SecretName) {
		return false
	}
	if o.Environment != "" && !strings.EqualFold(gh.AuditEntryStringField(entry, "environment_name"), o.Environment) {
		return false
	}
	return true
}

// sortEntries sorts entries by event time in the requested order.
func sortEntries(entries []*gh.AuditEntry, order string) {
	slices.SortStableFunc(entries, func(a, b *gh.AuditEntry) int {
		if order == OrderAsc {
			return entryTime(a).Compare(entryTime(b))
		}
		return entryTime(b).Compare(entryTime(a))
	})
}

// entryTime returns the event time of an audit log entry.
func entryTime(entry *gh.AuditEntry) time.Time {
	if entry == nil {
		return time.Time{}
	}
	if entry.Timestamp != nil {
		return entry.Timestamp.Time
	}
	if entry.CreatedAt != nil {
		return entry.CreatedAt.Time
	}
	return time.Time{}
}

// normalizeValues lower cases and validates the given values, returning all
// allowed values when no value is specified.
func normalizeValues(values, allowed []string, label string) ([]string, error) {
	if len(values) == 0 {
		return allowed, nil
	}
	normalized := []string{}
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if !slices.Contains(allowed, value) {
			return nil, fmt.Errorf("invalid %s %q: must be one of %s", label, value, strings.Join(allowed, ", "))
		}
		if !slices.Contains(normalized, value) {
			normalized = append(normalized, value)
		}
	}
	if len(normalized) == 0 {
		return allowed, nil
	}
	// Keep the canonical order so the generated queries are deterministic.
	ordered := []string{}
	for _, value := range allowed {
		if slices.Contains(normalized, value) {
			ordered = append(ordered, value)
		}
	}
	return ordered, nil
}

// normalizeDate validates a date option and returns it as a "YYYY-MM-DD" value.
func normalizeDate(value, label string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	for _, layout := range dateFormats {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.Format("2006-01-02"), nil
		}
	}
	return "", fmt.Errorf("invalid --%s value %q: expected YYYY-MM-DD or RFC3339 format", label, value)
}
