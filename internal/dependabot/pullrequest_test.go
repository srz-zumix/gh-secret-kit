package dependabot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	check "testing"

	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
)

func TestSnapshotOnlyOpenDependabotPullRequests(t *check.T) {
	human := pullRequest("human", "owner/repo", "feature/work", "main")
	human.Number = github.Ptr(10)
	fork := pullRequest(migrator.DependabotActor, "fork/repo", "dependabot/pip/jinja2", "main")
	fork.Number = github.Ptr(11)
	mine := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/pip/jinja2", "release")
	mine.Number = github.Ptr(12)
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/repos/owner/repo/pulls" || r.URL.Query().Get("state") != "open" {
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			http.Error(w, "unexpected", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode([]*github.PullRequest{human, fork, mine})
	})
	snapshot, err := snapshotDependabotPullRequests(context.Background(), client, sourceRepo)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 1 || snapshot[12] != "release" {
		t.Fatalf("snapshot=%v, want only the source-owned Dependabot pull request", snapshot)
	}
}

func TestRestoreOnlyRetargetedPullRequests(t *check.T) {
	states := map[int]*github.PullRequest{
		20: pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/pip/a", "copy-base"),
		21: pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/pip/b", "main"),
		22: pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/pip/c", "copy-base"),
	}
	states[22].State = github.Ptr("closed")
	var restored []string
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		number := 0
		if _, err := fmt.Sscanf(r.URL.Path, "/repos/owner/repo/pulls/%d", &number); err != nil {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
			return
		}
		switch r.Method {
		case "GET":
			_ = json.NewEncoder(w).Encode(states[number])
		case "PATCH":
			var body struct {
				Base string `json:"base"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			restored = append(restored, fmt.Sprintf("%d=%s", number, body.Base))
			_ = json.NewEncoder(w).Encode(states[number])
		default:
			t.Errorf("unexpected method %s", r.Method)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	// 23 was already based on the temporary branch, so it is not restored.
	snapshot := retargetedPullRequests{20: "main", 21: "main", 22: "main", 23: "copy-base"}
	if err := snapshot.restore(context.Background(), client, sourceRepo, "copy-base"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(restored, []string{"20=main"}) {
		t.Fatalf("restored %v, want only the open pull request moved onto the temporary base", restored)
	}
}

func TestRestoreReportsFailuresPerPullRequest(t *check.T) {
	pr := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/pip/a", "copy-base")
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			http.Error(w, `{"message":"boom"}`, http.StatusUnprocessableEntity)
			return
		}
		_ = json.NewEncoder(w).Encode(pr)
	})
	err := retargetedPullRequests{99: "main"}.restore(context.Background(), client, sourceRepo, "copy-base")
	if err == nil || !strings.Contains(err.Error(), "retarget it manually before deleting copy-base") {
		t.Fatalf("error=%v, want a manual recovery hint", err)
	}
}

func TestCleanupKeepsPreexistingRetargetedPullRequests(t *check.T) {
	observed := newObservedCopy()
	observed.retargeted = retargetedPullRequests{30: "main"}
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == workflowRunsPath:
			_ = json.NewEncoder(w).Encode(github.WorkflowRuns{})
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/pulls":
			if r.URL.Query().Get("base") != "copy-base" {
				_, _ = fmt.Fprint(w, `[]`)
				return
			}
			pr := pullRequest(migrator.DependabotActor, "owner/repo", "dependabot/pip/jinja2", "copy-base")
			pr.Number = github.Ptr(30)
			_ = json.NewEncoder(w).Encode([]*github.PullRequest{pr})
		default:
			// An ownership check or a branch deletion would reach this branch.
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})
	safe, err := observed.cleanup(context.Background(), client, sourceRepo, "copy-base", false)
	if !safe || err != nil {
		t.Fatalf("safe=%v error=%v", safe, err)
	}
}
