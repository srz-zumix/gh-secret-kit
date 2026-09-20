package orgaccess

import (
	"context"
	"fmt"
	"net/http"
	fixture "net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	ghclient "github.com/srz-zumix/go-gh-extension/pkg/gh/client"
)

var srcRepo = repository.Repository{Host: "github.com", Owner: "owner", Name: "repo"}

// newAPIClient returns a GitHubClient wired to an in-process HTTP server so the
// package's API-backed logic can be exercised without network access.
func newAPIClient(t *testing.T, handler http.HandlerFunc) *gh.GitHubClient {
	t.Helper()
	server := fixture.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		r.URL.Path = strings.TrimPrefix(r.URL.Path, "/api/v3")
		handler(w, r)
	}))
	t.Cleanup(server.Close)
	api, err := github.NewClient(github.WithHTTPClient(server.Client()), github.WithEnterpriseURLs(server.URL, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	client, err := ghclient.NewClient(api)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestCollect(t *testing.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/orgs/owner/actions/secrets":
			// IGNORED is not requested, so it must be filtered out.
			_, _ = fmt.Fprint(w, `{"total_count":5,"secrets":[
				{"name":"ALLVIS","visibility":"all"},
				{"name":"PRIVATEVIS","visibility":"private"},
				{"name":"PICKED","visibility":"selected"},
				{"name":"UNLISTABLE","visibility":"selected"},
				{"name":"IGNORED","visibility":"all"}
			]}`)
		case r.URL.Path == "/orgs/owner/actions/secrets/PICKED/repositories":
			_, _ = fmt.Fprint(w, `{"total_count":2,"repositories":[{"name":"repo-a"},{"name":"repo-b"}]}`)
		case r.URL.Path == "/orgs/owner/actions/secrets/UNLISTABLE/repositories":
			// The selected list cannot be read, so the secret must be skipped
			// rather than silently broadened.
			http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})

	got, err := Collect(context.Background(), client, srcRepo, []string{"ALLVIS", "PRIVATEVIS", "PICKED", "UNLISTABLE"})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 collected secrets, got %d: %+v", len(got), got)
	}
	if _, ok := got["IGNORED"]; ok {
		t.Error("unrequested secret must not be collected")
	}
	if got["ALLVIS"].Visibility != "all" || got["ALLVIS"].Skip {
		t.Errorf("unexpected ALLVIS: %+v", got["ALLVIS"])
	}
	if got["PRIVATEVIS"].Visibility != "private" || got["PRIVATEVIS"].Skip {
		t.Errorf("unexpected PRIVATEVIS: %+v", got["PRIVATEVIS"])
	}
	if got["PICKED"].Visibility != "selected" || got["PICKED"].Skip || !slices.Equal(got["PICKED"].Repos, []string{"repo-a", "repo-b"}) {
		t.Errorf("unexpected PICKED: %+v", got["PICKED"])
	}
	// A selected secret whose repositories cannot be listed is marked Skip so a
	// broadening "private" default is never emitted.
	if !got["UNLISTABLE"].Skip {
		t.Errorf("expected UNLISTABLE to be skipped, got %+v", got["UNLISTABLE"])
	}
}

func TestCollectListFailureReturnsEmpty(t *testing.T) {
	// When the org secrets cannot be listed at all, Collect degrades to an
	// empty result without error; callers then fall back to gh's default.
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/orgs/owner/actions/secrets" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
	})

	got, err := Collect(context.Background(), client, srcRepo, []string{"FOO"})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected an empty result on list failure, got %+v", got)
	}
}

func TestMapForDestination(t *testing.T) {
	// repo-a and repo-b exist at the destination; everything else 404s.
	existing := map[string]bool{"repo-a": true, "repo-b": true}
	var lookups int
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/repos/dest-org/") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
			return
		}
		lookups++
		name := strings.TrimPrefix(r.URL.Path, "/repos/dest-org/")
		if existing[name] {
			_, _ = fmt.Fprintf(w, `{"name":%q}`, name)
			return
		}
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})

	src := map[string]Source{
		"ALLVIS":            {Visibility: "all"},
		"PRIVATEVIS":        {Visibility: "private"},
		"ALL_EXIST":         {Visibility: "selected", Repos: []string{"repo-a", "repo-b"}},
		"PARTIAL":           {Visibility: "selected", Repos: []string{"repo-a", "missing"}},
		"NONE_EXIST":        {Visibility: "selected", Repos: []string{"missing-x", "missing-y"}},
		"SKIPPED_AT_SOURCE": {Visibility: "selected", Skip: true},
	}

	got := MapForDestination(context.Background(), client, "github.com", "dest-org", src)

	// Non-selected visibilities pass through unchanged and cost no lookup.
	if got["ALLVIS"].Visibility != "all" || got["ALLVIS"].Skip || len(got["ALLVIS"].Repos) != 0 {
		t.Errorf("unexpected ALLVIS: %+v", got["ALLVIS"])
	}
	if got["PRIVATEVIS"].Visibility != "private" || got["PRIVATEVIS"].Skip || len(got["PRIVATEVIS"].Repos) != 0 {
		t.Errorf("unexpected PRIVATEVIS: %+v", got["PRIVATEVIS"])
	}
	// Every existing selected repository is retained.
	if got["ALL_EXIST"].Visibility != "selected" || !slices.Equal(got["ALL_EXIST"].Repos, []string{"repo-a", "repo-b"}) {
		t.Errorf("unexpected ALL_EXIST: %+v", got["ALL_EXIST"])
	}
	// Missing repositories are dropped, keeping only the ones that exist.
	if got["PARTIAL"].Visibility != "selected" || !slices.Equal(got["PARTIAL"].Repos, []string{"repo-a"}) {
		t.Errorf("unexpected PARTIAL: %+v", got["PARTIAL"])
	}
	// When no selected repository exists at the destination, the secret is
	// skipped rather than downgraded to a broadening "private".
	if !got["NONE_EXIST"].Skip {
		t.Errorf("expected NONE_EXIST to be skipped, got %+v", got["NONE_EXIST"])
	}
	// A secret already marked Skip at the source stays skipped without lookups.
	if !got["SKIPPED_AT_SOURCE"].Skip {
		t.Errorf("expected SKIPPED_AT_SOURCE to be skipped, got %+v", got["SKIPPED_AT_SOURCE"])
	}

	// existsCache must dedupe lookups: repo-a, repo-b, missing, missing-x,
	// missing-y are each looked up exactly once (5 total).
	if lookups != 5 {
		t.Errorf("expected 5 deduplicated repository lookups, got %d", lookups)
	}

	// Every produced access must be valid for the generators.
	for name, access := range got {
		if err := migrator.ValidateOrgSecretAccess(access); err != nil {
			t.Errorf("invalid access for %q: %v", name, err)
		}
	}
}

func TestMapForDestinationLookupErrorSkips(t *testing.T) {
	// A non-404 lookup error is treated as "absent" (fail closed): the only
	// selected repository is dropped, so the secret is skipped rather than
	// broadened.
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Server Error"}`, http.StatusInternalServerError)
	})

	got := MapForDestination(context.Background(), client, "github.com", "dest-org", map[string]Source{
		"ONLY": {Visibility: "selected", Repos: []string{"repo-a"}},
	})
	if !got["ONLY"].Skip {
		t.Errorf("expected ONLY to be skipped on lookup failure, got %+v", got["ONLY"])
	}
}

func TestSkipUnresolved(t *testing.T) {
	src := map[string]Source{
		"ALL":             {Visibility: "all"},
		"PRIVATE":         {Visibility: "private"},
		"SELECTED":        {Visibility: "selected", Repos: []string{"repo-a"}},
		"ALREADY_SKIPPED": {Visibility: "selected", Skip: true},
	}

	got := SkipUnresolved(src)

	// "selected" cannot be verified without a destination client, so it must be
	// skipped rather than broadened to "private".
	if !got["SELECTED"].Skip {
		t.Errorf("expected SELECTED to be skipped, got %+v", got["SELECTED"])
	}
	if !got["ALREADY_SKIPPED"].Skip {
		t.Errorf("expected ALREADY_SKIPPED to stay skipped, got %+v", got["ALREADY_SKIPPED"])
	}
	// "all" and "private" do not broaden when reproduced, so they pass through.
	if got["ALL"].Skip || got["ALL"].Visibility != "all" {
		t.Errorf("expected ALL to pass through, got %+v", got["ALL"])
	}
	if got["PRIVATE"].Skip || got["PRIVATE"].Visibility != "private" {
		t.Errorf("expected PRIVATE to pass through, got %+v", got["PRIVATE"])
	}
}

func TestSkipUnresolvedValidAccess(t *testing.T) {
	// Every produced access must pass validation.
	got := SkipUnresolved(map[string]Source{
		"SELECTED": {Visibility: "selected", Repos: []string{"repo-a"}},
		"ALL":      {Visibility: "all"},
	})
	for name, access := range got {
		if err := migrator.ValidateOrgSecretAccess(access); err != nil {
			t.Errorf("invalid access for %q: %v", name, err)
		}
	}
}
