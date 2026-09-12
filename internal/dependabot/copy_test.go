package dependabot

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	fixture "net/http/httptest"
	"reflect"
	"slices"
	"strings"
	check "testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	ghclient "github.com/srz-zumix/go-gh-extension/pkg/gh/client"
)

var sourceRepo = repository.Repository{Host: "github.com", Owner: "owner", Name: "repo"}

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
		User: &github.User{Login: github.Ptr(actor)},
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
			client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests = append(requests, r.Method+" "+r.URL.Path)
				switch {
				case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/actions/runs":
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
					if keep || r.URL.Query().Get("state") != "all" || r.URL.Query().Get("base") != "copy-base" {
						t.Error("incorrect pull request cleanup query")
					}
					closed := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/closed", "copy-base")
					closed.State = github.Ptr("closed")
					historical := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/historical", "copy-base")
					historical.State = github.Ptr("closed")
					_ = json.NewEncoder(w).Encode([]*github.PullRequest{
						closed,
						historical,
						pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/deleted", "copy-base"),
						pullRequest("human", "owner/repo", "dependabot/human", "copy-base"),
						pullRequest(migrator.DependabotActor, "fork/repo", "dependabot/fork", "copy-base"),
						pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/other", "other-base"),
						pullRequest(migrator.DependabotActor, "owner/repo", "human/branch", "copy-base"),
					})
				case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/contents/.github/workflows/copy.yml":
					runName := "unique-invocation"
					switch r.URL.Query().Get("ref") {
					case "dependabot/closed":
					case "dependabot/historical":
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
				case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/git/refs/heads/"):
					if !stopped || keep {
						t.Error("deleted a branch before cancellation or despite keep")
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
				}
			}
			slices.Sort(deleted)
			if !reflect.DeepEqual(deleted, want) {
				t.Fatalf("deleted %v, want %v", deleted, want)
			}
		})
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
		case "GET /repos/owner/repo/actions/runs":
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
					case "GET /repos/owner/repo/actions/runs":
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
		case "GET /repos/owner/repo/actions/runs":
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
			w.Header().Set("Link", fmt.Sprintf(`<http://%s/api/v3/repos/owner/repo/actions/runs?page=2>; rel="next"`, r.Host))
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
	} {
		if err := RunCopy(context.Background(), invalid); err == nil {
			t.Fatalf("expected early validation failure for %+v", invalid)
		}
	}
}

func TestPrepareBranchRestoresDefaultWithKeep(t *check.T) {
	for _, keep := range []bool{false, true} {
		t.Run(fmt.Sprintf("keep=%v", keep), func(t *check.T) {
			var events []string
			client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /repos/owner/repo":
					fmt.Fprint(w, `{"default_branch":"main"}`)
				case "GET /repos/owner/repo/branches/main":
					fmt.Fprint(w, `{"name":"main","commit":{"sha":"base-sha"}}`)
				case "GET /repos/owner/repo/branches/copy-base":
					http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
				case "POST /repos/owner/repo/git/refs":
					events = append(events, "create")
					fmt.Fprint(w, `{"ref":"refs/heads/copy-base"}`)
				case "PATCH /repos/owner/repo":
					var body github.Repository
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					events = append(events, body.GetDefaultBranch())
					fmt.Fprint(w, `{}`)
				case "DELETE /repos/owner/repo/git/refs/heads/copy-base":
					events = append(events, "delete")
					w.WriteHeader(http.StatusNoContent)
				default:
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusInternalServerError)
				}
			})
			restore, err := prepareCopyBranch(context.Background(), client, sourceRepo, "copy-base", nil, keep)
			if err != nil {
				t.Fatal(err)
			}
			if err := restore(context.Background(), keep); err != nil {
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
