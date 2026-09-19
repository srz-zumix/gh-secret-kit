package dependabot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	check "testing"

	"github.com/google/go-github/v90/github"
)

func TestSuspendOnlyDefaultBranchFollowingRulesets(t *check.T) {
	details := map[string]string{
		"1": `{"id":1,"name":"follows","target":"branch","enforcement":"evaluate",` +
			`"conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},"rules":[{"type":"deletion"}]}`,
		"2": `{"id":2,"name":"pinned","target":"branch","enforcement":"active",` +
			`"conditions":{"ref_name":{"include":["refs/heads/main"],"exclude":[]}},"rules":[{"type":"deletion"}]}`,
		"3": `{"id":3,"name":"excluded","target":"branch","enforcement":"active",` +
			`"conditions":{"ref_name":{"include":["~ALL"],"exclude":["~DEFAULT_BRANCH"]}},"rules":[{"type":"deletion"}]}`,
		"4": `{"id":4,"name":"tags","target":"tag","enforcement":"active",` +
			`"conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},"rules":[{"type":"deletion"}]}`,
		"5": `{"id":5,"name":"ruleless","target":"branch","enforcement":"active",` +
			`"conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},"rules":[]}`,
	}
	var events []string
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/rulesets":
			_, _ = fmt.Fprint(w, `[{"id":1,"enforcement":"evaluate"},{"id":2,"enforcement":"active"},`+
				`{"id":3,"enforcement":"active"},{"id":4,"enforcement":"active"},{"id":5,"enforcement":"active"},`+
				`{"id":6,"enforcement":"disabled"}]`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/rulesets/"):
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			detail, ok := details[id]
			if !ok {
				t.Errorf("unexpected ruleset detail request for %s", id)
				http.Error(w, "unexpected", http.StatusInternalServerError)
				return
			}
			_, _ = fmt.Fprint(w, detail)
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/rulesets/"):
			var body github.RepositoryRuleset
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			events = append(events, fmt.Sprintf("%s=%s:%s", id, body.Name, body.Enforcement))
			_, _ = fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})

	restore, err := suspendDefaultBranchRulesets(context.Background(), client, sourceRepo)
	if err != nil {
		t.Fatalf("suspendDefaultBranchRulesets failed: %v", err)
	}
	if err := restore(context.Background()); err != nil {
		t.Fatalf("restore failed: %v", err)
	}
	// Only the ruleset following the default branch is touched, and it is
	// returned to its original enforcement rather than a hard-coded "active".
	want := []string{"1=follows:disabled", "1=follows:evaluate"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v, want %v", events, want)
	}
}

func TestSuspendRollsBackWhenDisablingFails(t *check.T) {
	var events []string
	client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/repos/owner/repo/rulesets":
			_, _ = fmt.Fprint(w, `[{"id":1,"enforcement":"active"},{"id":2,"enforcement":"active"}]`)
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/rulesets/"):
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			_, _ = fmt.Fprintf(w, `{"id":%s,"name":"rs-%s","target":"branch","enforcement":"active",`+
				`"conditions":{"ref_name":{"include":["~DEFAULT_BRANCH"],"exclude":[]}},"rules":[{"type":"deletion"}]}`, id, id)
		case r.Method == "PUT" && r.URL.Path == "/repos/owner/repo/rulesets/1":
			events = append(events, "1="+enforcementOf(t, r))
			_, _ = fmt.Fprint(w, `{}`)
		case r.Method == "PUT" && r.URL.Path == "/repos/owner/repo/rulesets/2":
			events = append(events, "2="+enforcementOf(t, r))
			http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	})

	restore, err := suspendDefaultBranchRulesets(context.Background(), client, sourceRepo)
	if restore != nil || err == nil {
		t.Fatalf("restore is nil=%v error=%v", restore == nil, err)
	}
	// The failed ruleset is restored as well, because an error response can
	// still hide a successful update.
	want := []string{"1=disabled", "2=disabled", "1=active", "2=active"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events=%v, want %v", events, want)
	}
}

func enforcementOf(t *check.T, r *http.Request) string {
	t.Helper()
	var body github.RepositoryRuleset
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		t.Error(err)
	}
	return string(body.Enforcement)
}
