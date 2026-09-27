package varaccess

import (
	"context"
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
