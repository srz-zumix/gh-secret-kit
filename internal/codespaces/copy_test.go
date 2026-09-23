package codespaces

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/internal/destination"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	ghclient "github.com/srz-zumix/go-gh-extension/pkg/gh/client"
)

func TestRemoteCommand(t *testing.T) {
	cmd := remoteCommand("#!/usr/bin/env bash\necho hi\n")

	trapIdx := strings.Index(cmd, "trap '")
	decodeIdx := strings.Index(cmd, "base64 -d >")
	chmodIdx := strings.Index(cmd, "chmod 600")
	bashIdx := strings.Index(cmd, "bash -l ")

	if trapIdx < 0 || decodeIdx < 0 || chmodIdx < 0 || bashIdx < 0 {
		t.Fatalf("remoteCommand is missing an expected step:\n%s", cmd)
	}
	// The cleanup trap must be installed before anything can fail so both files
	// are removed on every exit path.
	if trapIdx >= decodeIdx || decodeIdx >= chmodIdx || chmodIdx >= bashIdx {
		t.Errorf("expected order trap < decode < chmod < bash, got %d/%d/%d/%d:\n%s",
			trapIdx, decodeIdx, chmodIdx, bashIdx, cmd)
	}
	// bash -l must be the final command so its exit status is what the EXIT trap
	// captures; nothing may run after it that could replace that status.
	if strings.Contains(cmd[bashIdx:], ";") {
		t.Errorf("no command may follow bash -l, got trailing %q", cmd[bashIdx:])
	}
	// Both decode and chmod must propagate failure.
	if !strings.Contains(cmd, "base64 -d > "+remoteScriptFile+" || exit 1") {
		t.Errorf("decode step must propagate failure:\n%s", cmd)
	}
	if !strings.Contains(cmd, "chmod 600 "+remoteTokenFile+" || exit 1") {
		t.Errorf("chmod step must propagate failure:\n%s", cmd)
	}
	// The trap must remove both the script and token files and preserve $rc.
	if !strings.Contains(cmd, "trap 'rc=$?; rm -f "+remoteScriptFile+" "+remoteTokenFile+"; exit $rc' EXIT") {
		t.Errorf("cleanup trap must remove both files and preserve the exit code:\n%s", cmd)
	}
	if !strings.HasPrefix(cmd, "umask 077;") {
		t.Errorf("remoteCommand must start by restricting the umask:\n%s", cmd)
	}
}

func TestWriteTokenFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())

	destinations := []*destination.Destination{
		{Host: "github.com"},
		{Host: "github.com"}, // duplicate host must not be written twice
		{Host: "ghe.example.com"},
	}
	tokenEnvNames := map[string]string{
		"github.com":      "TOKEN_GITHUB_COM",
		"ghe.example.com": "TOKEN_GHE_EXAMPLE_COM",
	}
	hostTokens := map[string]string{
		"github.com":      "gho_token1",
		"ghe.example.com": "ghe_token2",
	}

	path, err := writeTokenFile(destinations, tokenEnvNames, hostTokens)
	if err != nil {
		t.Fatalf("writeTokenFile returned error: %v", err)
	}
	defer func() { _ = os.Remove(path) }()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read token file: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "TOKEN_GITHUB_COM='gho_token1'\n") {
		t.Errorf("expected github.com token line, got:\n%s", content)
	}
	if !strings.Contains(content, "TOKEN_GHE_EXAMPLE_COM='ghe_token2'\n") {
		t.Errorf("expected ghe token line, got:\n%s", content)
	}
	if got := strings.Count(content, "TOKEN_GITHUB_COM="); got != 1 {
		t.Errorf("expected the duplicate host to be written once, got %d lines", got)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("failed to stat token file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("expected owner-only 0600 permissions, got %o", perm)
		}
	}
}

func TestWriteTokenFileRejectsUnsafeToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)

	for _, token := range []string{"bad\ntoken", "bad\rtoken", "bad'token"} {
		destinations := []*destination.Destination{{Host: "github.com"}}
		tokenEnvNames := map[string]string{"github.com": "TOKEN_GITHUB_COM"}
		hostTokens := map[string]string{"github.com": token}

		path, err := writeTokenFile(destinations, tokenEnvNames, hostTokens)
		if err == nil {
			_ = os.Remove(path)
			t.Errorf("expected an error for token %q", token)
		}
		if path != "" {
			t.Errorf("expected no path on error, got %q", path)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read temp dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("no token file should be left behind on error, found %d entries", len(entries))
	}
}

// newAPIClient wires a GitHubClient to an in-process HTTP server so
// collectSecrets can be exercised without network access.
func newAPIClient(t *testing.T, handler http.HandlerFunc) *gh.GitHubClient {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestCollectSecretsOrgAccessExcludesUserSecrets(t *testing.T) {
	sourceRepo := repository.Repository{Host: "github.com", Owner: "owner", Name: "repo"}

	cases := []struct {
		name            string
		config          *CopyConfig
		wantNames       []string
		wantAccessNames []string
	}{
		{
			// --include-user-secrets merges user names into the copy, but they
			// are not organization secrets, so they must be excluded from
			// organization access (otherwise Collect marks them Skip and the
			// generator drops them).
			name: "auto include user secrets",
			config: &CopyConfig{
				Scope:                migrator.SecretScopeOrg,
				IncludeUserSecrets:   true,
				CopyRepositoryAccess: true,
			},
			wantNames:       []string{"ORGSEC", "USERSEC"},
			wantAccessNames: []string{"ORGSEC"},
		},
		{
			// An explicitly named user secret is likewise not an organization
			// secret and must not receive organization access.
			name: "explicit names mix org and user",
			config: &CopyConfig{
				Scope:                migrator.SecretScopeOrg,
				Secrets:              []string{"ORGSEC", "USERSEC"},
				CopyRepositoryAccess: true,
			},
			wantNames:       []string{"ORGSEC", "USERSEC"},
			wantAccessNames: []string{"ORGSEC"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := newAPIClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/orgs/owner/codespaces/secrets":
					_, _ = fmt.Fprint(w, `{"total_count":1,"secrets":[{"name":"ORGSEC","visibility":"all"}]}`)
				case "/user/codespaces/secrets":
					_, _ = fmt.Fprint(w, `{"total_count":1,"secrets":[{"name":"USERSEC"}]}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					http.Error(w, "unexpected", http.StatusInternalServerError)
				}
			})

			names, accessNames, err := collectSecrets(context.Background(), client, sourceRepo, tc.config, tc.config.Scope)
			if err != nil {
				t.Fatalf("collectSecrets returned error: %v", err)
			}
			slices.Sort(names)
			slices.Sort(accessNames)
			if !slices.Equal(names, tc.wantNames) {
				t.Errorf("names = %v, want %v", names, tc.wantNames)
			}
			if !slices.Equal(accessNames, tc.wantAccessNames) {
				t.Errorf("accessNames = %v, want %v", accessNames, tc.wantAccessNames)
			}
		})
	}
}
