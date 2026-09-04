package audit

import (
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

func TestQueriesDefaultsToAllScopesAndSecretTypes(t *testing.T) {
	opts := SecretHistoryOptions{}
	queries, err := opts.queries()
	if err != nil {
		t.Fatalf("queries returned error: %v", err)
	}

	// 3 scopes * 3 operations for actions secrets, plus repo and org scopes
	// * 3 operations * 2 integrations for integration secrets.
	if len(queries) != 21 {
		t.Fatalf("expected 21 queries, got %d", len(queries))
	}

	actions := map[string]string{}
	for _, q := range queries {
		actions[q.action+"|"+q.integration] = q.action
	}
	for _, want := range []string{
		"repo.update_actions_secret|",
		"org.create_actions_secret|",
		"environment.remove_actions_secret|",
		"repo.update_integration_secret|dependabot",
		"org.update_integration_secret|codespaces",
	} {
		if _, ok := actions[want]; !ok {
			t.Errorf("expected query %q to be generated", want)
		}
	}
	if _, ok := actions["environment.update_integration_secret|dependabot"]; ok {
		t.Error("environment scope must not generate integration secret queries")
	}
}

func TestQueriesWithSelectedScopeAndSecretType(t *testing.T) {
	opts := SecretHistoryOptions{Scopes: []string{"ENVIRONMENT"}, SecretTypes: []string{"actions"}}
	queries, err := opts.queries()
	if err != nil {
		t.Fatalf("queries returned error: %v", err)
	}
	if len(queries) != len(operations) {
		t.Fatalf("expected %d queries, got %d", len(operations), len(queries))
	}
	for _, q := range queries {
		if !strings.HasPrefix(q.action, "environment.") {
			t.Errorf("unexpected action %q", q.action)
		}
	}
}

func TestQueriesInvalidScope(t *testing.T) {
	opts := SecretHistoryOptions{Scopes: []string{"team"}}
	if _, err := opts.queries(); err == nil {
		t.Fatal("expected an error for an invalid scope")
	}
}

func TestPhrase(t *testing.T) {
	opts := SecretHistoryOptions{Repo: repository.Repository{Owner: "octo", Name: "hello"}}

	got := opts.phrase(query{action: "repo.update_actions_secret"}, "created:>=2024-01-01")
	want := "action:repo.update_actions_secret repo:octo/hello created:>=2024-01-01"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}

	// Organization events are not bound to a repository.
	got = opts.phrase(query{action: "org.update_actions_secret"}, "")
	want = "action:org.update_actions_secret"
	if got != want {
		t.Errorf("expected %q, got %q", want, got)
	}
}

func TestCreatedPhrase(t *testing.T) {
	mustParse := func(value string) time.Time {
		t.Helper()
		for _, layout := range DateFormats {
			if parsed, err := time.Parse(layout, value); err == nil {
				return parsed
			}
		}
		t.Fatalf("failed to parse %q", value)
		return time.Time{}
	}

	tests := []struct {
		name  string
		since string
		until string
		want  string
	}{
		{name: "empty"},
		{name: "since only", since: "2024-01-02", want: "created:>=2024-01-02"},
		{name: "until only", until: "2024-01-02", want: "created:<=2024-01-02"},
		{name: "range", since: "2024-01-02", until: "2024-02-03", want: "created:2024-01-02..2024-02-03"},
		{name: "rfc3339", since: "2024-01-02T03:04:05Z", want: "created:>=2024-01-02"},
		{name: "rfc3339 fractional", since: "2024-01-02T03:04:05.123Z", want: "created:>=2024-01-02"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := SecretHistoryOptions{}
			if tt.since != "" {
				opts.Since = mustParse(tt.since)
			}
			if tt.until != "" {
				opts.Until = mustParse(tt.until)
			}
			if got := opts.createdPhrase(); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func newTestEntry(fields map[string]any) *gh.AuditEntry {
	return &gh.AuditEntry{AdditionalFields: fields}
}

func TestMatch(t *testing.T) {
	opts := SecretHistoryOptions{SecretName: "my_secret", Environment: "production"}
	entry := newTestEntry(map[string]any{"key": "MY_SECRET", "environment_name": "Production"})
	if !opts.match(entry, query{}) {
		t.Error("expected the entry to match case insensitively")
	}

	if opts.match(newTestEntry(map[string]any{"key": "OTHER"}), query{}) {
		t.Error("expected a different secret name to be filtered out")
	}

	integrationOpts := SecretHistoryOptions{}
	if integrationOpts.match(newTestEntry(map[string]any{"integration": "codespaces"}), query{integration: "dependabot"}) {
		t.Error("expected a different integration to be filtered out")
	}
	if !integrationOpts.match(newTestEntry(map[string]any{"integration": "dependabot"}), query{integration: "dependabot"}) {
		t.Error("expected a matching integration to pass the filter")
	}
}

func TestSortEntries(t *testing.T) {
	older := &gh.AuditEntry{Timestamp: &github.Timestamp{Time: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}}
	newer := &gh.AuditEntry{Timestamp: &github.Timestamp{Time: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)}}

	entries := []*gh.AuditEntry{older, newer}
	sortEntries(entries, OrderDesc)
	if entries[0] != newer {
		t.Error("expected the newest entry first in descending order")
	}

	sortEntries(entries, OrderAsc)
	if entries[0] != older {
		t.Error("expected the oldest entry first in ascending order")
	}
}
