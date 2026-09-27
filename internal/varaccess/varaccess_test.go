package varaccess

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	fixture "net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/internal/orgaccess"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	ghclient "github.com/srz-zumix/go-gh-extension/pkg/gh/client"
)

var srcOrg = repository.Repository{Host: "github.com", Owner: "owner"}

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

func vars(names ...string) []*github.ActionsVariable {
	result := make([]*github.ActionsVariable, 0, len(names))
	for _, name := range names {
		result = append(result, &github.ActionsVariable{Name: name})
	}
	return result
}

func TestCollect(t *testing.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/owner/actions/variables":
			// IGNORED is not requested, so it must be filtered out.
			_, _ = fmt.Fprint(w, `{"total_count":5,"variables":[
				{"name":"ALLVIS","visibility":"all"},
				{"name":"PRIVATEVIS","visibility":"private"},
				{"name":"PICKED","visibility":"selected"},
				{"name":"UNLISTABLE","visibility":"selected"},
				{"name":"IGNORED","visibility":"all"}
			]}`)
		case "/orgs/owner/actions/variables/PICKED/repositories":
			_, _ = fmt.Fprint(w, `{"total_count":2,"repositories":[{"name":"repo-a"},{"name":"repo-b"}]}`)
		case "/orgs/owner/actions/variables/UNLISTABLE/repositories":
			// The selected list cannot be read, so the variable must be
			// skipped rather than reproduced with unverified access.
			http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})

	got, err := Collect(context.Background(), client, srcOrg, vars("ALLVIS", "PRIVATEVIS", "PICKED", "UNLISTABLE"))
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 collected variables, got %d: %+v", len(got), got)
	}
	if _, ok := got["IGNORED"]; ok {
		t.Error("unrequested variable must not be collected")
	}
	if got["ALLVIS"].Visibility != "all" || got["ALLVIS"].Skip {
		t.Errorf("ALLVIS = %+v", got["ALLVIS"])
	}
	if got["PRIVATEVIS"].Visibility != "private" || got["PRIVATEVIS"].Skip {
		t.Errorf("PRIVATEVIS = %+v", got["PRIVATEVIS"])
	}
	if got["PICKED"].Visibility != "selected" || got["PICKED"].Skip || !slices.Equal(got["PICKED"].Repos, []string{"repo-a", "repo-b"}) {
		t.Errorf("PICKED = %+v", got["PICKED"])
	}
	if !got["UNLISTABLE"].Skip {
		t.Errorf("UNLISTABLE must be skipped when its selected repositories cannot be listed: %+v", got["UNLISTABLE"])
	}
}

func TestCollectMissingVariableSkipped(t *testing.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/orgs/owner/actions/variables" {
			_, _ = fmt.Fprint(w, `{"total_count":1,"variables":[{"name":"PRESENT","visibility":"all"}]}`)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})

	got, err := Collect(context.Background(), client, srcOrg, vars("PRESENT", "MISSING"))
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	if got["PRESENT"].Visibility != "all" || got["PRESENT"].Skip {
		t.Errorf("PRESENT = %+v", got["PRESENT"])
	}
	if !got["MISSING"].Skip {
		t.Errorf("a variable missing from the listing must be skipped: %+v", got["MISSING"])
	}
}

func TestCollectListFailure(t *testing.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
	})

	if _, err := Collect(context.Background(), client, srcOrg, vars("FOO")); err == nil {
		t.Fatal("Collect must return an error when the variable listing fails")
	}
}

func TestResolveMapsAndCaches(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/repos/dest/repo-a":
			_, _ = fmt.Fprint(w, `{"id":101,"name":"repo-a"}`)
		case "/repos/dest/repo-b":
			_, _ = fmt.Fprint(w, `{"id":102,"name":"repo-b"}`)
		case "/repos/dest/missing":
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})

	src := map[string]orgaccess.Source{
		"ALLVIS":     {Visibility: "all"},
		"PRIVATEVIS": {Visibility: "private"},
		// Two selected variables share repo-a to exercise the cache.
		"PICKED":  {Visibility: "selected", Repos: []string{"repo-a", "repo-b"}},
		"PICKED2": {Visibility: "selected", Repos: []string{"repo-a", "missing"}},
		// None of its repositories exist at the destination, so it is skipped.
		"GONE":    {Visibility: "selected", Repos: []string{"missing"}},
		"SKIPPED": {Skip: true},
	}

	got := Resolve(context.Background(), client, "dest.host", "dest", src)

	if got["ALLVIS"].Visibility != "all" || got["ALLVIS"].Skip || len(got["ALLVIS"].RepoIDs) != 0 {
		t.Errorf("ALLVIS = %+v", got["ALLVIS"])
	}
	if got["PRIVATEVIS"].Visibility != "private" || got["PRIVATEVIS"].Skip {
		t.Errorf("PRIVATEVIS = %+v", got["PRIVATEVIS"])
	}
	if got["PICKED"].Visibility != "selected" || !slices.Equal(got["PICKED"].RepoIDs, []int64{101, 102}) {
		t.Errorf("PICKED = %+v", got["PICKED"])
	}
	if got["PICKED2"].Visibility != "selected" || !slices.Equal(got["PICKED2"].RepoIDs, []int64{101}) {
		t.Errorf("PICKED2 = %+v", got["PICKED2"])
	}
	if !got["GONE"].Skip {
		t.Errorf("GONE must be skipped when no destination repository exists: %+v", got["GONE"])
	}
	if !got["SKIPPED"].Skip {
		t.Errorf("SKIPPED source must stay skipped: %+v", got["SKIPPED"])
	}

	// repo-a is shared by PICKED and PICKED2 and missing by PICKED2 and GONE;
	// each destination repository must be looked up exactly once.
	if calls["/repos/dest/repo-a"] != 1 {
		t.Errorf("repo-a looked up %d times, want 1", calls["/repos/dest/repo-a"])
	}
	if calls["/repos/dest/missing"] != 1 {
		t.Errorf("missing looked up %d times, want 1", calls["/repos/dest/missing"])
	}
}

var destOrg = repository.Repository{Host: "github.com", Owner: "dest"}
var destRepo = repository.Repository{Host: "github.com", Owner: "dest", Name: "repo"}

// captured records the visibility sent to the create endpoint and whether the
// selected-repositories endpoint was called.
type captured struct {
	createVisibility string
	setRepos         []int64
}

func newCopyClient(t *testing.T, cap *captured, exists bool) *gh.GitHubClient {
	t.Helper()
	return newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/orgs/dest/actions/variables":
			if exists {
				http.Error(w, `{"message":"Conflict"}`, http.StatusConflict)
				return
			}
			var body struct {
				Visibility string `json:"visibility"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			cap.createVisibility = body.Visibility
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPost && r.URL.Path == "/repos/dest/repo/actions/variables":
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodPut && r.URL.Path == "/orgs/dest/actions/variables/NAME/repositories":
			var body struct {
				IDs []int64 `json:"selected_repository_ids"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			cap.setRepos = body.IDs
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
}

// TestCopyDisabledUsesDefaultVisibility verifies that when access copying is
// disabled (access is nil) the source visibility is not reproduced at an
// organization destination; gh's default ("private") is written instead.
func TestCopyDisabledUsesDefaultVisibility(t *testing.T) {
	cap := &captured{}
	client := newCopyClient(t, cap, false)
	v := &github.ActionsVariable{Name: "NAME", Value: "value", Visibility: github.Ptr("all")}

	outcome, err := Copy(context.Background(), client, destOrg, v, nil, false, false)
	if err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	if outcome != CopyWritten {
		t.Fatalf("outcome = %v, want CopyWritten", outcome)
	}
	if cap.createVisibility != "private" {
		t.Errorf("created visibility = %q, want %q (source visibility must not be reproduced)", cap.createVisibility, "private")
	}
	if v.GetVisibility() != "all" {
		t.Errorf("source variable visibility mutated to %q", v.GetVisibility())
	}
}

// TestCopySelectedAppliesAccess verifies that a resolved "selected" variable is
// created with selected visibility and its repositories are applied.
func TestCopySelectedAppliesAccess(t *testing.T) {
	cap := &captured{}
	client := newCopyClient(t, cap, false)
	v := &github.ActionsVariable{Name: "NAME", Value: "value", Visibility: github.Ptr("private")}
	access := map[string]Applied{"NAME": {Visibility: "selected", RepoIDs: []int64{101, 102}}}

	outcome, err := Copy(context.Background(), client, destOrg, v, access, false, false)
	if err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	if outcome != CopyWritten {
		t.Fatalf("outcome = %v, want CopyWritten", outcome)
	}
	if cap.createVisibility != "selected" {
		t.Errorf("created visibility = %q, want selected", cap.createVisibility)
	}
	if !slices.Equal(cap.setRepos, []int64{101, 102}) {
		t.Errorf("set repos = %v, want [101 102]", cap.setRepos)
	}
}

// TestCopyEnabledMissingEntryFailsClosed verifies that with access copying
// enabled (access non-nil) a variable without a resolved entry is skipped
// rather than written with the source visibility.
func TestCopyEnabledMissingEntryFailsClosed(t *testing.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	v := &github.ActionsVariable{Name: "NAME", Value: "value", Visibility: github.Ptr("all")}
	access := map[string]Applied{"OTHER": {Visibility: "all"}}

	outcome, err := Copy(context.Background(), client, destOrg, v, access, false, false)
	if err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	if outcome != CopySkippedAccess {
		t.Fatalf("outcome = %v, want CopySkippedAccess", outcome)
	}
}

// TestCopySkippedEntry verifies that a variable whose resolved access is marked
// Skip is not written.
func TestCopySkippedEntry(t *testing.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected, got %s %s", r.Method, r.URL.Path)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	v := &github.ActionsVariable{Name: "NAME", Value: "value", Visibility: github.Ptr("selected")}
	access := map[string]Applied{"NAME": {Skip: true}}

	outcome, err := Copy(context.Background(), client, destOrg, v, access, false, false)
	if err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	if outcome != CopySkippedAccess {
		t.Fatalf("outcome = %v, want CopySkippedAccess", outcome)
	}
}

// TestCopyAlreadyExists verifies that a 409 from the destination is reported as
// CopySkippedExists when errorIfExists is false.
func TestCopyAlreadyExists(t *testing.T) {
	cap := &captured{}
	client := newCopyClient(t, cap, true)
	v := &github.ActionsVariable{Name: "NAME", Value: "value", Visibility: github.Ptr("all")}

	outcome, err := Copy(context.Background(), client, destOrg, v, nil, false, false)
	if err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	if outcome != CopySkippedExists {
		t.Fatalf("outcome = %v, want CopySkippedExists", outcome)
	}
}

// TestCopyRepositoryDestination verifies that a repository destination is copied
// without touching organization visibility handling.
func TestCopyRepositoryDestination(t *testing.T) {
	cap := &captured{}
	client := newCopyClient(t, cap, false)
	v := &github.ActionsVariable{Name: "NAME", Value: "value", Visibility: github.Ptr("all")}

	outcome, err := Copy(context.Background(), client, destRepo, v, nil, false, false)
	if err != nil {
		t.Fatalf("Copy returned error: %v", err)
	}
	if outcome != CopyWritten {
		t.Fatalf("outcome = %v, want CopyWritten", outcome)
	}
}
