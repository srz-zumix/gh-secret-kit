package dependabot

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	fixture "net/http/httptest"
	"reflect"
	"regexp"
	"slices"
	"strings"
	check "testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/internal/destination"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	ghclient "github.com/srz-zumix/go-gh-extension/pkg/gh/client"
)

var sourceRepo = repository.Repository{Host: "github.com", Owner: "owner", Name: "repo"}

const workflowRunsPath = "/repos/owner/repo/actions/workflows/copy.yml/runs"

func newAPIClient(t *check.T, handler http.HandlerFunc) *gh.GitHubClient {
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

func ownedRun(id int64) *github.WorkflowRun {
	return &github.WorkflowRun{
		ID:           github.Ptr(id),
		DisplayTitle: github.Ptr("unique-invocation"),
		Path:         github.Ptr(".github/workflows/copy.yml"),
		Event:        github.Ptr(pushEvent),
		Actor:        &github.User{Login: github.Ptr(migrator.DependabotActor)},
		HeadBranch:   github.Ptr(fmt.Sprintf("dependabot/github_actions/update-%d", id)),
		Status:       github.Ptr("completed"),
		Conclusion:   github.Ptr("success"),
	}
}

func newObservedCopy() *observedCopy {
	return &observedCopy{workflowPath: ".github/workflows/copy.yml", runName: "unique-invocation"}
}

func TestCopyResultRequiresCompletedSuccessAndPrintedMarker(t *check.T) {
	marker := "2026-01-01T00:00:00Z " + migrator.DependabotCopyDoneMarker + "\n"
	cases := []struct {
		name       string
		status     string
		conclusion string
		log        string
		logErr     error
		attempts   int
		done       bool
		failure    string
	}{
		{name: "running marker", status: "in_progress", log: marker, attempts: 3},
		{name: "queued marker", status: "queued", log: marker, attempts: 3},
		{name: "completed success", status: "completed", conclusion: "success", log: marker, done: true},
		{name: "failure overrides marker", status: "completed", conclusion: "failure", log: marker, done: true, failure: `conclusion "failure"`},
		{name: "cancelled overrides marker", status: "completed", conclusion: "cancelled", log: marker, done: true, failure: `conclusion "cancelled"`},
		{name: "failure without logs", status: "completed", conclusion: "failure", logErr: errors.New("not ready"), done: true, failure: `conclusion "failure"`},
		{name: "missing marker retry", status: "completed", conclusion: "success", attempts: 1},
		{name: "missing marker failure", status: "completed", conclusion: "success", attempts: 3, done: true, failure: "did not print the copy completion marker"},
		{name: "unavailable logs retry", status: "completed", conclusion: "success", logErr: errors.New("not ready"), attempts: 2},
		{name: "unavailable logs failure", status: "completed", conclusion: "success", logErr: errors.New("not ready"), attempts: 3, done: true, failure: "logs remain unavailable"},
		{name: "colored script echo", status: "completed", conclusion: "success", log: "timestamp \x1b[36;1m" + migrator.DependabotCopyDoneMarker + "\x1b[0m\n", attempts: 3, done: true, failure: "did not print"},
		{name: "uncolored script echo", status: "completed", conclusion: "success", log: "timestamp ##[group]Run echo\n" + marker + "timestamp ##[endgroup]\n", attempts: 3, done: true, failure: "did not print"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *check.T) {
			run := ownedRun(1)
			run.Status, run.Conclusion = github.Ptr(tc.status), github.Ptr(tc.conclusion)
			done, err := copyResult(run, tc.log, tc.logErr, tc.attempts)
			if done != tc.done {
				t.Fatalf("done=%v, want %v", done, tc.done)
			}
			if tc.failure == "" && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.failure != "" && (err == nil || !strings.Contains(err.Error(), tc.failure)) {
				t.Fatalf("error=%v, want %q", err, tc.failure)
			}
		})
	}
}

func TestRunOwnershipChecksEveryBoundary(t *check.T) {
	cases := []struct {
		name   string
		change func(*github.WorkflowRun)
	}{
		{"display title", func(r *github.WorkflowRun) { r.DisplayTitle = github.Ptr("previous-invocation") }},
		{"path", func(r *github.WorkflowRun) { r.Path = github.Ptr(".github/workflows/other.yml") }},
		{"actor", func(r *github.WorkflowRun) { r.Actor = &github.User{Login: github.Ptr("human")} }},
		{"event", func(r *github.WorkflowRun) { r.Event = github.Ptr("pull_request") }},
		{"branch", func(r *github.WorkflowRun) { r.HeadBranch = github.Ptr("human/update") }},
		{"missing actor", func(r *github.WorkflowRun) { r.Actor = nil }},
	}
	observed := newObservedCopy()
	if !observed.owns(ownedRun(1)) || observed.owns(nil) {
		t.Fatal("unexpected ownership decision")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *check.T) {
			run := ownedRun(2)
			tc.change(run)
			observed.observe(run)
			if observed.owns(run) || len(observed.runs) != 0 {
				t.Fatal("accepted an unrelated run")
			}
		})
	}
	observed.observe(ownedRun(1))
	observed.observe(ownedRun(1))
	if len(observed.runs) != 1 || len(observed.branches) != 1 {
		t.Fatal("observations were not deduplicated")
	}
}

func pullRequest(actor, headRepo, head, base string) *github.PullRequest {
	return &github.PullRequest{
		Number: github.Ptr(99),
		State:  github.Ptr("open"),
		User:   &github.User{Login: github.Ptr(actor)},
		Head: &github.PullRequestBranch{
			Ref: github.Ptr(head), Repo: &github.Repository{FullName: github.Ptr(headRepo)},
		},
		Base: &github.PullRequestBranch{Ref: github.Ptr(base)},
	}
}

func TestCleanupOnlyOwnedRunsAndDependabotBranches(t *check.T) {
	for _, keep := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep=%v", keep), func(t *check.T) {
			observed := newObservedCopy()
			active := ownedRun(1)
			active.Status = github.Ptr("in_progress")
			unrelated := ownedRun(2)
			unrelated.DisplayTitle = github.Ptr("earlier-copy")
			observed.observe(ownedRun(3))
			var requests []string
			stopped := false
			closedPRs := make(map[string]bool)
			client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch {
				case r.Method == "GET" && r.URL.Path == workflowRunsPath:
					if r.URL.Query().Get("actor") != migrator.DependabotActor || r.URL.Query().Get("event") != pushEvent {
						t.Error("missing server-side actor/event filters")
					}
					_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{active, unrelated}})
				case r.Method == "POST" && r.URL.Path == "/repos/owner/repo/actions/runs/1/cancel":
					stopped = true
					w.WriteHeader(http.StatusAccepted)
				case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/actions/runs/1":
					if !stopped {
						t.Error("run inspected before cancellation")
					}
					_ = json.NewEncoder(w).Encode(ownedRun(1))
				case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/actions/runs/"):
					if !stopped || strings.HasSuffix(r.URL.Path, "/2") || keep {
						t.Error("deleted an unrelated, active or preserved run")
					}
					w.WriteHeader(http.StatusNoContent)
				case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
					if keep {
						t.Error("listed pull requests despite keep")
					}
					if r.URL.Query().Get("state") == "open" && r.URL.Query().Get("base") == "" {
						// Retargeted-PR discovery finds nothing in this scenario.
						_, _ = fmt.Fprint(w, `[]`)
						return
					}
					if r.URL.Query().Get("state") != "all" || r.URL.Query().Get("base") != "copy-base" {
						t.Error("incorrect pull request cleanup query")
					}
					closed := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/closed", "copy-base")
					closed.State = github.Ptr("closed")
					historical := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/historical", "copy-base")
					historical.State = github.Ptr("closed")
					openObserved := pullRequest(migrator.DependabotActor, "owner/repo", active.GetHeadBranch(), "copy-base")
					openObserved.Number = github.Ptr(10)
					openExtra := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/open", "copy-base")
					openExtra.Number = github.Ptr(11)
					unrelated := []*github.PullRequest{
						pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/historical-open", "copy-base"),
						pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/deleted", "copy-base"),
						pullRequest("human", "owner/repo", "dependabot/human", "copy-base"),
						pullRequest(migrator.DependabotActor, "fork/repo", "dependabot/fork", "copy-base"),
						pullRequest(migrator.DependabotActor, "owner/repo", "human/branch", "copy-base"),
					}
					for _, pr := range unrelated {
						pr.State = github.Ptr("closed")
					}
					_ = json.NewEncoder(w).Encode([]*github.PullRequest{
						closed,
						historical,
						openObserved,
						openExtra,
						unrelated[0],
						unrelated[1],
						unrelated[2],
						unrelated[3],
						pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/other", "other-base"),
						unrelated[4],
					})
				case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/contents/.github/workflows/copy.yml":
					runName := "unique-invocation"
					switch r.URL.Query().Get("ref") {
					case "dependabot/closed", "dependabot/open":
					case "dependabot/historical", "dependabot/historical-open":
						runName = "previous-invocation"
					case "dependabot/deleted":
						http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
						return
					default:
						t.Error("attempted to verify an unrelated head")
					}
					_ = json.NewEncoder(w).Encode(github.RepositoryContent{
						Type: github.Ptr("file"), Encoding: github.Ptr("base64"),
						Content: github.Ptr(base64.StdEncoding.EncodeToString([]byte("run-name: " + runName + "\n"))),
					})
				case r.Method == "PATCH" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/pulls/"):
					if keep || !stopped || (r.URL.Path != "/repos/owner/repo/pulls/10" && r.URL.Path != "/repos/owner/repo/pulls/11") {
						t.Error("closed an unowned, historical, already-closed or preserved pull request")
					}
					var body github.PullRequest
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.GetState() != "closed" {
						t.Errorf("invalid close payload: %v, state=%q", err, body.GetState())
					}
					closedPRs[r.URL.Path] = true
					_, _ = fmt.Fprint(w, `{"state":"closed"}`)
				case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"):
					if !stopped || keep {
						t.Error("deleted a branch before cancellation or despite keep")
					}
					if strings.HasSuffix(r.URL.Path, "/dependabot/github_actions/update-1") && !closedPRs["/repos/owner/repo/pulls/10"] {
						t.Error("deleted an observed head before closing its pull request")
					}
					if strings.HasSuffix(r.URL.Path, "/dependabot/open") && !closedPRs["/repos/owner/repo/pulls/11"] {
						t.Error("deleted an extra head before closing its pull request")
					}
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected request", http.StatusInternalServerError)
				}
			})
			safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", keep)
			if err != nil || !safe {
				t.Fatalf("cleanup: safe=%v, error=%v", safe, err)
			}
			var deleted []string
			for _, request := range requests {
				if strings.HasPrefix(request, "DELETE ") {
					deleted = append(deleted, request)
				}
			}
			var want []string
			if !keep {
				want = []string{
					"DELETE /repos/owner/repo/actions/runs/1",
					"DELETE /repos/owner/repo/actions/runs/3",
					"DELETE /repos/owner/repo/git/refs/heads/dependabot/closed",
					"DELETE /repos/owner/repo/git/refs/heads/dependabot/github_actions/update-1",
					"DELETE /repos/owner/repo/git/refs/heads/dependabot/github_actions/update-3",
					"DELETE /repos/owner/repo/git/refs/heads/dependabot/open",
				}
			}
			slices.Sort(deleted)
			if !reflect.DeepEqual(deleted, want) {
				t.Fatalf("deleted %v, want %v", deleted, want)
			}
			wantClosed := 2
			if keep {
				wantClosed = 0
			}
			if len(closedPRs) != wantClosed {
				t.Fatalf("closed %d pull requests, want %d", len(closedPRs), wantClosed)
			}
		})
	}
}

func TestCleanupPreservesHeadWhenClosingPullRequestFails(t *check.T) {
	observed := newObservedCopy()
	failedRun := ownedRun(1)
	successfulRun := ownedRun(2)
	observed.observe(failedRun)
	observed.observe(successfulRun)
	var deleted []string
	closedOther := false
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{failedRun, successfulRun}})
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/actions/runs/"):
			t.Error("deleted run history despite an unsafe cleanup")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			failing := pullRequest(migrator.DependabotActor, "owner/repo", failedRun.GetHeadBranch(), "copy-base")
			failing.Number = github.Ptr(10)
			closedSameHead := pullRequest(migrator.DependabotActor, "owner/repo", failedRun.GetHeadBranch(), "copy-base")
			closedSameHead.State = github.Ptr("closed")
			succeeding := pullRequest(migrator.DependabotActor, "owner/repo", successfulRun.GetHeadBranch(), "copy-base")
			succeeding.Number = github.Ptr(11)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{failing, closedSameHead, succeeding})
		case r.Method == "PATCH" && r.URL.Path == "/repos/owner/repo/pulls/10":
			http.Error(w, `{"message":"close denied"}`, http.StatusForbidden)
		case r.Method == "PATCH" && r.URL.Path == "/repos/owner/repo/pulls/11":
			closedOther = true
			_, _ = fmt.Fprint(w, `{"state":"closed"}`)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"):
			if !closedOther {
				t.Error("deleted the other head before its pull request was closed")
			}
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", false)
	if safe || err == nil || !strings.Contains(err.Error(), "failed to close Dependabot pull request #10") {
		t.Fatalf("safe=%v error=%v", safe, err)
	}
	if !strings.Contains(err.Error(), failedRun.GetHeadBranch()) {
		t.Fatalf("error does not identify the preserved head: %v", err)
	}
	if !slices.Equal(deleted, []string{successfulRun.GetHeadBranch()}) {
		t.Fatalf("deleted heads %v, want only %s", deleted, successfulRun.GetHeadBranch())
	}
}

func TestCleanupClosesRetargetedOwnedPullRequest(t *check.T) {
	observed := newObservedCopy()
	run := ownedRun(1)
	observed.observe(run)
	closed := false
	var deleted []string
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{run}})
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/actions/runs/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			if r.URL.Query().Get("base") == "copy-base" {
				_, _ = fmt.Fprint(w, `[]`)
				return
			}
			// The owned pull request was retargeted away from the temporary base.
			retargeted := pullRequest(migrator.DependabotActor, "owner/repo", run.GetHeadBranch(), "main")
			retargeted.Number = github.Ptr(21)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{retargeted})
		case r.Method == "PATCH" && r.URL.Path == "/repos/owner/repo/pulls/21":
			closed = true
			_, _ = fmt.Fprint(w, `{"state":"closed"}`)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"):
			if !closed {
				t.Error("deleted the retargeted head before closing its pull request")
			}
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", false)
	if !safe || err != nil {
		t.Fatalf("safe=%v error=%v", safe, err)
	}
	if !closed {
		t.Fatal("retargeted pull request was not closed")
	}
	if !slices.Equal(deleted, []string{run.GetHeadBranch()}) {
		t.Fatalf("deleted heads %v, want only %s", deleted, run.GetHeadBranch())
	}
}

func TestCleanupUnsafeWhenOwnershipCheckFailsForOpenBasePullRequest(t *check.T) {
	observed := newObservedCopy()
	run := ownedRun(1)
	observed.observe(run)
	var deletedBranches []string
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{run}})
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/actions/runs/"):
			t.Error("deleted run history despite an unverifiable ownership check")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			if r.URL.Query().Get("state") == "open" && r.URL.Query().Get("base") == "" {
				_, _ = fmt.Fprint(w, `[]`)
				return
			}
			unverified := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/unverified", "copy-base")
			unverified.Number = github.Ptr(30)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{unverified})
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/contents/.github/workflows/copy.yml":
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"):
			deletedBranches = append(deletedBranches, strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"))
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", false)
	if safe || err == nil || !strings.Contains(err.Error(), "failed to verify Dependabot branch dependabot/unverified") {
		t.Fatalf("safe=%v error=%v", safe, err)
	}
	// The unverified head is never deleted, but proven observed heads still are.
	if !slices.Equal(deletedBranches, []string{run.GetHeadBranch()}) {
		t.Fatalf("deleted branches %v, want only %s", deletedBranches, run.GetHeadBranch())
	}
}

func TestCleanupPreservesBaseForOpenPullRequestOwnedByAnotherInvocation(t *check.T) {
	observed := newObservedCopy()
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{})
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			if r.URL.Query().Get("base") == "copy-base" {
				pr := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/foreign", "copy-base")
				pr.Number = github.Ptr(77)
				_ = json.NewEncoder(w).Encode([]*github.PullRequest{pr})
				return
			}
			_, _ = fmt.Fprint(w, `[]`)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/contents/.github/workflows/copy.yml":
			_ = json.NewEncoder(w).Encode(github.RepositoryContent{
				Type: github.Ptr("file"), Encoding: github.Ptr("base64"),
				Content: github.Ptr(base64.StdEncoding.EncodeToString([]byte("run-name: previous-invocation\n"))),
			})
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"):
			t.Errorf("deleted a branch owned by another invocation: %s", r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", false)
	if safe || err == nil || !strings.Contains(err.Error(), "owned by another invocation") {
		t.Fatalf("safe=%v error=%v", safe, err)
	}
}

func TestCleanupPreservesHeadsWhenPullRequestListingFails(t *check.T) {
	observed := newObservedCopy()
	run := ownedRun(1)
	observed.observe(run)
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET " + workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{run}})
		case "DELETE /repos/owner/repo/actions/runs/1":
			w.WriteHeader(http.StatusNoContent)
		case "GET /repos/owner/repo/pulls":
			http.Error(w, `{"message":"unavailable"}`, http.StatusServiceUnavailable)
		default:
			t.Errorf("unexpected request after pull request listing failed: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", false)
	if safe || err == nil || !strings.Contains(err.Error(), "failed to list Dependabot pull requests") {
		t.Fatalf("safe=%v error=%v", safe, err)
	}
}

func TestCleanupPreservesBaseForUnrelatedOpenPullRequest(t *check.T) {
	observed := newObservedCopy()
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{})
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			if r.URL.Query().Get("base") == "copy-base" {
				pr := pullRequest("human", "owner/repo", "human/branch", "copy-base")
				pr.Number = github.Ptr(42)
				_ = json.NewEncoder(w).Encode([]*github.PullRequest{pr})
				return
			}
			_, _ = fmt.Fprint(w, `[]`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", false)
	if safe || err == nil || !strings.Contains(err.Error(), "open pull request #42") {
		t.Fatalf("safe=%v error=%v", safe, err)
	}
}

func TestCleanupPreservesArtifactsWhenBranchDeletionFails(t *check.T) {
	observed := newObservedCopy()
	run := ownedRun(1)
	observed.observe(run)
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{run}})
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			_, _ = fmt.Fprint(w, `[]`)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"):
			http.Error(w, `{"message":"unavailable"}`, http.StatusServiceUnavailable)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/actions/runs/"):
			t.Error("deleted run history after branch deletion failed")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", false)
	if safe || err == nil || !strings.Contains(err.Error(), "failed to delete Dependabot branch") {
		t.Fatalf("safe=%v error=%v", safe, err)
	}
}

func TestCleanupPreservesArtifactsWhenRunDiscoveryFails(t *check.T) {
	observed := newObservedCopy()
	run := ownedRun(1)
	run.Status = github.Ptr("queued")
	observed.observe(run)
	cancelled := false
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET " + workflowRunsPath:
			http.Error(w, `{"message":"unavailable"}`, http.StatusServiceUnavailable)
		case "POST /repos/owner/repo/actions/runs/1/cancel":
			cancelled = true
			w.WriteHeader(http.StatusAccepted)
		case "GET /repos/owner/repo/actions/runs/1":
			_ = json.NewEncoder(w).Encode(ownedRun(1))
		default:
			t.Errorf("unexpected cleanup mutation: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "base", false)
	if safe || err == nil || !cancelled {
		t.Fatalf("safe=%v err=%v cancelled=%v", safe, err, cancelled)
	}
}

func TestTokenCleanupStillRunsAfterArtifactCleanupFailure(t *check.T) {
	for _, failure := range []string{"list", "cancel"} {
		for _, keep := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/keep=%v", failure, keep), func(t *check.T) {
				observed := newObservedCopy()
				active := ownedRun(1)
				active.Status = github.Ptr("in_progress")
				deleted := false
				client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
					switch r.Method + " " + r.URL.Path {
					case "GET " + workflowRunsPath:
						if failure == "list" {
							http.Error(w, `{"message":"unavailable"}`, http.StatusServiceUnavailable)
							return
						}
						_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{active}})
					case "POST /repos/owner/repo/actions/runs/1/cancel":
						w.WriteHeader(http.StatusAccepted)
					case "GET /repos/owner/repo/actions/runs/1":
						_ = json.NewEncoder(w).Encode(active)
					case "DELETE /repos/owner/repo/dependabot/secrets/TOKEN":
						deleted = true
						w.WriteHeader(http.StatusNoContent)
					default:
						t.Errorf("unexpected artifact deletion: %s %s", r.Method, r.URL.Path)
						http.Error(w, "unexpected", http.StatusInternalServerError)
					}
				})
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
				defer cancel()
				safe, err := observed.cleanup(ctx, client, sourceRepo, "base", keep)
				if safe || err == nil {
					t.Fatalf("expected failed run cleanup: safe=%v err=%v", safe, err)
				}
				cancel()
				if err := cleanupTokenSecrets(ctx, client, sourceRepo, map[string]string{"github.com": "TOKEN"}, keep); err != nil {
					t.Fatal(err)
				}
				if deleted == keep {
					t.Fatalf("deleted=%v keep=%v", deleted, keep)
				}
			})
		}
	}
}

func TestStopRunsCancelsAllBeforeWaitingAndForcesRejectedCancellation(t *check.T) {
	observed := newObservedCopy()
	for _, id := range []int64{1, 2} {
		run := ownedRun(id)
		run.Status = github.Ptr("in_progress")
		observed.observe(run)
	}
	cancelled := make(map[int64]bool)
	forced := false
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		var id int64
		_, _ = fmt.Sscanf(r.URL.Path, "/repos/owner/repo/actions/runs/%d", &id)
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/force-cancel"):
			if id != 1 {
				t.Error("force-cancelled a completed run")
			}
			forced = true
			w.WriteHeader(http.StatusAccepted)
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/cancel"):
			cancelled[id] = true
			if id == 1 {
				http.Error(w, `{"message":"cannot cancel normally"}`, http.StatusConflict)
			} else {
				w.WriteHeader(http.StatusAccepted)
			}
		case r.Method == "GET":
			if len(cancelled) != 2 {
				t.Error("started waiting before all active runs received cancellation")
			}
			run := ownedRun(id)
			if id == 1 && !forced {
				run.Status = github.Ptr("in_progress")
			}
			_ = json.NewEncoder(w).Encode(run)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := observed.stopRuns(ctx, client, sourceRepo); err != nil {
		t.Fatal(err)
	}
	if !forced {
		t.Fatal("normal cancellation rejection did not trigger force cancellation")
	}
}

func TestCleanupDoesNotDeleteArtifactsWhileRunIsActive(t *check.T) {
	observed := newObservedCopy()
	active := ownedRun(1)
	active.Status = github.Ptr("in_progress")
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET " + workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{active}})
		case "GET /repos/owner/repo/actions/runs/1":
			_ = json.NewEncoder(w).Encode(active)
		case "POST /repos/owner/repo/actions/runs/1/cancel":
			w.WriteHeader(http.StatusAccepted)
		default:
			t.Errorf("unexpected mutation while run is active: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	safe, err := observed.cleanup(ctx, client, sourceRepo, "base", false)
	if safe || err == nil || !strings.Contains(err.Error(), "could not confirm") {
		t.Fatalf("safe=%v err=%v", safe, err)
	}
}

func TestWorkflowRunDiscoveryPaginatesAndIgnoresOtherInvocations(t *check.T) {
	pages := 0
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		pages++
		run := ownedRun(int64(pages))
		if pages == 1 {
			run.DisplayTitle = github.Ptr("previous-copy")
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/api/v3%s?page=2>; rel="next"`, r.Host, workflowRunsPath))
		} else if r.URL.Query().Get("page") != "2" {
			t.Error("missing second page")
		}
		_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{run}})
	})
	observed := newObservedCopy()
	runs, err := observed.workflowRuns(context.Background(), client, sourceRepo)
	if err != nil || len(runs) != 1 || runs[0].GetID() != 2 || len(observed.runs) != 1 {
		t.Fatalf("runs=%v observed=%v err=%v", runs, observed.runs, err)
	}
}

func TestWorkflowRunDiscoveryTreatsMissingWorkflowAsEmpty(t *check.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})
	runs, err := newObservedCopy().workflowRuns(context.Background(), client, sourceRepo)
	if err != nil || len(runs) != 0 {
		t.Fatalf("runs=%v error=%v", runs, err)
	}
}

func TestWaitForCopyHonorsShortTimeout(t *check.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(github.WorkflowRuns{})
	})
	start := time.Now()
	err := waitForCopy(context.Background(), client, sourceRepo, 20*time.Millisecond, newObservedCopy())
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout, got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("timeout waited for the poll interval")
	}
}

func TestParseTimeout(t *check.T) {
	duration, err := ParseTimeout("1h")
	if err != nil || duration != time.Hour {
		t.Fatalf("duration=%v error=%v", duration, err)
	}
	for _, value := range []string{"later", "0s", "-1m"} {
		if _, err := ParseTimeout(value); err == nil {
			t.Errorf("ParseTimeout(%q) succeeded", value)
		}
	}
}

func TestCopyDefaultsAndEarlyValidation(t *check.T) {
	config := &CopyConfig{}
	if err := applyDefaults(config); err != nil {
		t.Fatal(err)
	}
	if config.WorkflowName != types.DefaultDependabotCopyWorkflowName ||
		config.RunnerLabel != types.DefaultCopyRunnerLabel ||
		config.Scope != migrator.SecretScopeRepo ||
		config.DestinationApp != migrator.SecretAppDependabot || config.Timeout <= 0 {
		t.Fatalf("unexpected defaults: %+v", config)
	}
	for _, invalid := range []*CopyConfig{
		nil,
		{WorkflowName: "../escape"},
		{WorkflowName: "copy.yml"},
		{WorkflowName: "${{ secrets.NAME }}"},
		{WorkflowName: "gh-secret-kit-dependabot-trigger"},
		{RunnerLabel: "${{ secrets.RUNNER }}"},
		{RunnerLabel: "   "},
		{RunnerLabel: "ubuntu\nself-hosted"},
		{Timeout: -time.Second},
		{Scope: migrator.SecretScopeEnv},
		{Branch: "../bad"},
		{TokenSecretName: "ghp_not_a_secret_name"},
	} {
		if err := RunCopy(context.Background(), invalid); err == nil {
			t.Fatalf("expected early validation failure for %+v", invalid)
		}
	}
}

func TestAcquireCopyLock(t *check.T) {
	created := false
	deleted := false
	markerCreated := false
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo":
			_, _ = fmt.Fprint(w, `{"default_branch":"main"}`)
		case "GET /repos/owner/repo/branches/main":
			_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
		case "GET /repos/owner/repo/branches/" + copyLockBranch:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case "POST /repos/owner/repo/git/refs":
			created = true
			_, _ = fmt.Fprintf(w, `{"ref":"refs/heads/%s"}`, copyLockBranch)
		case "GET /repos/owner/repo/contents/" + copyLockMarkerPath:
			if !markerCreated {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				return
			}
			encoded := base64.StdEncoding.EncodeToString([]byte("invocation"))
			_, _ = fmt.Fprintf(w, `{"type":"file","encoding":"base64","content":%q}`, encoded)
		case "PUT /repos/owner/repo/contents/" + copyLockMarkerPath:
			markerCreated = true
			_, _ = fmt.Fprint(w, `{}`)
		case "DELETE /repos/owner/repo/git/refs/heads/" + copyLockBranch:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	release, err := acquireCopyLock(context.Background(), client, sourceRepo, "invocation")
	if err != nil || !created {
		t.Fatalf("created=%v error=%v", created, err)
	}
	if err := release(context.Background()); err != nil || !deleted {
		t.Fatalf("deleted=%v error=%v", deleted, err)
	}
}

func TestAcquireCopyLockRejectsExistingLock(t *check.T) {
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo":
			_, _ = fmt.Fprint(w, `{"default_branch":"main"}`)
		case "GET /repos/owner/repo/branches/main":
			_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
		case "GET /repos/owner/repo/branches/" + copyLockBranch:
			_, _ = fmt.Fprintf(w, `{"name":%q}`, copyLockBranch)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	release, err := acquireCopyLock(context.Background(), client, sourceRepo, "invocation")
	if release != nil || err == nil || !strings.Contains(err.Error(), "another Dependabot secret copy may be running") {
		t.Fatalf("release is nil=%v error=%v", release == nil, err)
	}
}

func TestAcquireCopyLockPreservesAmbiguousCreation(t *check.T) {
	deleted := false
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo":
			_, _ = fmt.Fprint(w, `{"default_branch":"main"}`)
		case "GET /repos/owner/repo/branches/main":
			_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
		case "GET /repos/owner/repo/branches/" + copyLockBranch:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case "POST /repos/owner/repo/git/refs":
			http.Error(w, `{"message":"ambiguous"}`, http.StatusServiceUnavailable)
		case "DELETE /repos/owner/repo/git/refs/heads/" + copyLockBranch:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	release, err := acquireCopyLock(context.Background(), client, sourceRepo, "invocation")
	if release != nil || err == nil || deleted {
		t.Fatalf("release is nil=%v deleted=%v error=%v", release == nil, deleted, err)
	}
}

func TestReleaseCopyLockPreservesReplacementOwner(t *check.T) {
	marker := "invocation"
	deleted := false
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo":
			_, _ = fmt.Fprint(w, `{"default_branch":"main"}`)
		case "GET /repos/owner/repo/branches/main":
			_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
		case "GET /repos/owner/repo/branches/" + copyLockBranch:
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case "POST /repos/owner/repo/git/refs":
			_, _ = fmt.Fprintf(w, `{"ref":"refs/heads/%s"}`, copyLockBranch)
		case "GET /repos/owner/repo/contents/" + copyLockMarkerPath:
			encoded := base64.StdEncoding.EncodeToString([]byte(marker))
			_, _ = fmt.Fprintf(w, `{"type":"file","encoding":"base64","content":%q}`, encoded)
		case "PUT /repos/owner/repo/contents/" + copyLockMarkerPath:
			_, _ = fmt.Fprint(w, `{}`)
		case "DELETE /repos/owner/repo/git/refs/heads/" + copyLockBranch:
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	release, err := acquireCopyLock(context.Background(), client, sourceRepo, "invocation")
	if err != nil {
		t.Fatal(err)
	}
	marker = "replacement"
	if err := release(context.Background()); err == nil || deleted {
		t.Fatalf("deleted=%v error=%v", deleted, err)
	}
}

func TestPrepareBranchRestoresDefaultWithKeep(t *check.T) {
	for _, keep := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep=%v", keep), func(t *check.T) {
			var events []string
			currentDefault := "main"
			client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /repos/owner/repo":
					_, _ = fmt.Fprintf(w, `{"default_branch":%q}`, currentDefault)
				case "GET /repos/owner/repo/branches/main":
					_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
				case "GET /repos/owner/repo/branches/copy-base":
					http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				case "POST /repos/owner/repo/git/refs":
					events = append(events, "create")
					_, _ = fmt.Fprint(w, `{"ref":"refs/heads/copy-base"}`)
				case "PATCH /repos/owner/repo":
					var body github.Repository
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					currentDefault = body.GetDefaultBranch()
					events = append(events, body.GetDefaultBranch())
					_, _ = fmt.Fprint(w, `{}`)
				case "DELETE /repos/owner/repo/git/refs/heads/copy-base":
					events = append(events, "delete")
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusInternalServerError)
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			restore, _, err := prepareCopyBranch(ctx, client, sourceRepo, "copy-base", nil, keep)
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			if err := restore(context.WithoutCancel(ctx), keep); err != nil {
				t.Fatal(err)
			}
			want := []string{"create", "copy-base", "main"}
			if !keep {
				want = append(want, "delete")
			}
			if !reflect.DeepEqual(events, want) {
				t.Fatalf("events %v, want %v", events, want)
			}
		})
	}
}

func TestPrepareBranchPreservesAmbiguousCreateFailure(t *check.T) {
	deleted := false
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo":
			_, _ = fmt.Fprint(w, `{"default_branch":"main"}`)
		case "GET /repos/owner/repo/branches/main":
			_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
		case "GET /repos/owner/repo/branches/copy-base":
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case "POST /repos/owner/repo/git/refs":
			http.Error(w, `{"message":"ambiguous"}`, http.StatusServiceUnavailable)
		case "DELETE /repos/owner/repo/git/refs/heads/copy-base":
			deleted = true
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	restore, _, err := prepareCopyBranch(context.Background(), client, sourceRepo, "copy-base", nil, false)
	if restore != nil || err == nil || deleted {
		t.Fatalf("restore is nil=%v deleted=%v error=%v", restore == nil, deleted, err)
	}
}

func TestPrepareBranchDoesNotOverwriteChangedDefault(t *check.T) {
	currentDefault := "main"
	restorePatches := 0
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo":
			_, _ = fmt.Fprintf(w, `{"default_branch":%q}`, currentDefault)
		case "GET /repos/owner/repo/branches/main":
			_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
		case "GET /repos/owner/repo/branches/copy-base":
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case "POST /repos/owner/repo/git/refs":
			_, _ = fmt.Fprint(w, `{"ref":"refs/heads/copy-base"}`)
		case "PATCH /repos/owner/repo":
			var body github.Repository
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			if currentDefault != "main" {
				restorePatches++
			}
			currentDefault = body.GetDefaultBranch()
			_, _ = fmt.Fprint(w, `{}`)
		case "DELETE /repos/owner/repo/git/refs/heads/copy-base":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	restore, _, err := prepareCopyBranch(context.Background(), client, sourceRepo, "copy-base", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	currentDefault = "release"
	if err := restore(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if restorePatches != 0 || currentDefault != "release" {
		t.Fatalf("restore patches=%d default=%s", restorePatches, currentDefault)
	}
}

func TestPrepareBranchKeepsStateUnsafeWhenRestoreFailsAfterAmbiguousSwitch(t *check.T) {
	currentDefault := "main"
	patches := 0
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/owner/repo":
			_, _ = fmt.Fprintf(w, `{"default_branch":%q}`, currentDefault)
		case "GET /repos/owner/repo/branches/main":
			_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
		case "GET /repos/owner/repo/branches/copy-base":
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case "POST /repos/owner/repo/git/refs":
			_, _ = fmt.Fprint(w, `{"ref":"refs/heads/copy-base"}`)
		case "PATCH /repos/owner/repo":
			patches++
			var body github.Repository
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			// The switch applies server-side but returns an error (ambiguous),
			// while the later restore attempt fails without taking effect.
			if patches == 1 {
				currentDefault = body.GetDefaultBranch()
			}
			http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	restore, unsafe, err := prepareCopyBranch(context.Background(), client, sourceRepo, "copy-base", nil, false)
	if err == nil || !unsafe || restore != nil {
		t.Fatalf("restore=%v unsafe=%v err=%v", restore != nil, unsafe, err)
	}
	if currentDefault != "copy-base" {
		t.Fatalf("expected the default branch left at copy-base, got %s", currentDefault)
	}
	if patches != 2 {
		t.Fatalf("expected a switch and a restore attempt, got %d PATCH calls", patches)
	}
}

func TestRunCopyDryRun(t *check.T) {
	origNewClient := newClient
	origVerify := verifyDestinations
	t.Cleanup(func() {
		newClient = origNewClient
		verifyDestinations = origVerify
	})

	mutations := 0
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			mutations++
			t.Errorf("unexpected mutation request in dryrun: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected mutation", http.StatusInternalServerError)
			return
		}
		switch r.URL.Path {
		case "/repos/owner/repo/dependabot/secrets":
			_ = json.NewEncoder(w).Encode(github.Secrets{
				TotalCount: 2,
				Secrets: []*github.Secret{
					{Name: "FOO_SECRET"},
					{Name: "BAR_SECRET"},
				},
			})
		default:
			t.Errorf("unexpected GET request: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	newClient = func(repo repository.Repository) (*gh.GitHubClient, error) {
		return client, nil
	}
	verifyDestinations = func(ctx context.Context, orgLevel bool, destinations []*destination.Destination, hostTokens map[string]string) error {
		return nil
	}

	var buf bytes.Buffer
	config := &CopyConfig{
		Source:           "owner/repo",
		Destinations:     []string{"destowner/destrepo"},
		DestinationToken: "gho_dummy_token",
		DryRun:           true,
		Out:              &buf,
	}

	if err := RunCopy(context.Background(), config); err != nil {
		t.Fatalf("RunCopy failed in dryrun: %v", err)
	}

	if mutations > 0 {
		t.Fatalf("dryrun performed %d mutations", mutations)
	}

	output := buf.String()
	if !strings.Contains(output, "name: gh-secret-kit-dependabot-copy") {
		t.Errorf("output missing workflow name: %s", output)
	}
	if !regexp.MustCompile(`GH_SECRET_KIT_COPY_TOKEN_[0-9]+_GITHUB_COM`).MatchString(output) {
		t.Fatalf("dryrun output does not use an invocation-unique token secret name:\n%s", output)
	}
	if !strings.Contains(output, "FOO_SECRET") || !strings.Contains(output, "BAR_SECRET") {
		t.Errorf("output missing secrets: %s", output)
	}
	if !strings.Contains(output, "destowner/destrepo") {
		t.Errorf("output missing destination: %s", output)
	}
	if !strings.Contains(output, "runs-on: ubuntu-latest") {
		t.Errorf("output missing runs-on: %s", output)
	}
}

func TestRunCopyCleanupOrder(t *check.T) {
	origNewClient := newClient
	origVerify := verifyDestinations
	t.Cleanup(func() {
		newClient = origNewClient
		verifyDestinations = origVerify
	})

	var events []string
	defaultBranch := "main"
	secretLists := 0
	runName := ""
	runLists := 0
	jobLists := 0
	logReads := 0
	lockMarker := ""
	publicKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/dependabot/secrets":
			secretLists++
			if secretLists == 1 {
				_, _ = fmt.Fprint(w, `{"secrets":[{"name":"FOO"}]}`)
			} else {
				_, _ = fmt.Fprint(w, `{"secrets":[]}`)
			}
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/dependabot/secrets/public-key":
			_, _ = fmt.Fprintf(w, `{"key_id":"key","key":%q}`, publicKey)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/rulesets":
			_, _ = fmt.Fprint(w, `[{"id":7,"name":"main","target":"branch","enforcement":"active"}]`)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/rulesets/7":
			_, _ = fmt.Fprint(w, `{"id":7,"name":"main","target":"branch","enforcement":"active",`+
				`"conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},"rules":[{"type":"deletion"}]}`)
		case r.Method == "PUT" && r.URL.Path == "/repos/owner/repo/rulesets/7":
			var body github.RepositoryRuleset
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			events = append(events, "ruleset-"+string(body.Enforcement))
			_, _ = fmt.Fprint(w, `{}`)
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/dependabot/secrets/"):
			name := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			invocationID := strings.TrimSuffix(
				strings.TrimPrefix(name, types.DefaultDependabotCopyTokenSecretName+"_"),
				"_GITHUB_COM",
			)
			runName = "gh-secret-kit-dependabot-copy-" + invocationID
			events = append(events, "token-create")
			w.WriteHeader(http.StatusCreated)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/dependabot/secrets/"):
			events = append(events, "token-delete")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo":
			_, _ = fmt.Fprintf(w, `{"default_branch":%q}`, defaultBranch)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/branches/main":
			_, _ = fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
		case r.Method == "GET" && (r.URL.Path == "/repos/owner/repo/branches/"+copyLockBranch ||
			r.URL.Path == "/repos/owner/repo/branches/copy-base"):
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case r.Method == "POST" && r.URL.Path == "/repos/owner/repo/git/refs":
			var body github.Reference
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			ref := body.GetRef()
			if strings.HasSuffix(ref, copyLockBranch) {
				events = append(events, "lock-create")
			} else {
				events = append(events, "branch-create")
			}
			_ = json.NewEncoder(w).Encode(body)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/contents/"):
			if r.URL.Path == "/repos/owner/repo/contents/"+copyLockMarkerPath && lockMarker != "" {
				encoded := base64.StdEncoding.EncodeToString([]byte(lockMarker))
				_, _ = fmt.Fprintf(w, `{"type":"file","encoding":"base64","content":%q}`, encoded)
				return
			}
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/contents/"):
			if r.URL.Path == "/repos/owner/repo/contents/"+copyLockMarkerPath {
				var body struct {
					Content string `json:"content"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				content, err := base64.StdEncoding.DecodeString(body.Content)
				if err != nil {
					t.Error(err)
				}
				lockMarker = string(content)
			}
			_, _ = fmt.Fprint(w, `{}`)
		case r.Method == "PATCH" && r.URL.Path == "/repos/owner/repo":
			var body github.Repository
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			defaultBranch = body.GetDefaultBranch()
			events = append(events, "default-"+defaultBranch)
			_, _ = fmt.Fprint(w, `{}`)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/actions/workflows/gh-secret-kit-dependabot-copy.yml/runs":
			runLists++
			if runName == "" {
				t.Error("run name was not captured from the temporary token secret")
			}
			run := ownedRun(1)
			run.DisplayTitle = github.Ptr(runName)
			run.Path = github.Ptr(".github/workflows/gh-secret-kit-dependabot-copy.yml")
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{WorkflowRuns: []*github.WorkflowRun{run}})
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/actions/runs/1/jobs":
			jobLists++
			_ = json.NewEncoder(w).Encode(github.Jobs{Jobs: []*github.WorkflowJob{{ID: github.Ptr(int64(10))}}})
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/actions/jobs/10/logs":
			logReads++
			http.Redirect(w, r, "http://"+r.Host+"/job-log-content", http.StatusFound)
		case r.Method == "GET" && r.URL.Path == "/job-log-content":
			logReads++
			w.Header().Set("Content-Type", "text/plain")
			_, _ = fmt.Fprintf(w, "2026-01-01T00:00:00Z %s\n", migrator.DependabotCopyDoneMarker)
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			_, _ = fmt.Fprint(w, `[]`)
		case r.Method == "DELETE" && strings.Contains(r.URL.Path, "/git/refs/heads/dependabot/"):
			events = append(events, "dependabot-branch-delete")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "DELETE" && r.URL.Path == "/repos/owner/repo/actions/runs/1":
			events = append(events, "run-delete")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "DELETE" && r.URL.Path == "/repos/owner/repo/git/refs/heads/copy-base":
			events = append(events, "branch-delete")
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "DELETE" && r.URL.Path == "/repos/owner/repo/git/refs/heads/"+copyLockBranch:
			events = append(events, "lock-delete")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	newClient = func(repository.Repository) (*gh.GitHubClient, error) {
		return client, nil
	}
	verifyDestinations = func(context.Context, bool, []*destination.Destination, map[string]string) error {
		return nil
	}

	err := RunCopy(context.Background(), &CopyConfig{
		Source:           "owner/repo",
		Destinations:     []string{"destowner/destrepo"},
		Secrets:          []string{"FOO"},
		DestinationToken: "gho_dummy_token",
		Branch:           "copy-base",
		Timeout:          time.Second,
	})
	if err != nil {
		t.Fatalf("%v (run lists=%d job lists=%d log reads=%d run name=%q)", err, runLists, jobLists, logReads, runName)
	}
	wantOrder := []string{
		"lock-create",
		"ruleset-disabled",
		"token-create",
		"branch-create",
		"default-copy-base",
		"dependabot-branch-delete",
		"run-delete",
		"default-main",
		"branch-delete",
		"token-delete",
		"ruleset-active",
		"lock-delete",
	}
	if !reflect.DeepEqual(events, wantOrder) {
		t.Fatalf("events=%v, want %v", events, wantOrder)
	}
}
