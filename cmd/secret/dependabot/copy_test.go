package dependabot

import (
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
)

func TestCopyFlagDefaults(t *testing.T) {
	cmd := NewCopyCmd()
	defaults := map[string]string{
		"repo":              "",
		"scope":             "repo",
		"dst-app":           "dependabot",
		"secrets":           "[]",
		"exclude-secrets":   "[]",
		"rename":            "[]",
		"overwrite":         "false",
		"dst-token":         "",
		"token-secret-name": "GH_SECRET_KIT_COPY_TOKEN",
		"branch":            "",
		"workflow-name":     "gh-secret-kit-dependabot-copy",
		"runner-label":      "ubuntu-latest",
		"timeout":           "30m",
		"keep-workflow":     "false",
	}
	for name, want := range defaults {
		t.Run(name, func(t *testing.T) {
			flag := cmd.Flags().Lookup(name)
			if flag == nil {
				t.Fatalf("missing flag --%s", name)
			}
			if flag.DefValue != want {
				t.Errorf("default = %q, want %q", flag.DefValue, want)
			}
		})
	}
	if cmd.Flags().Lookup("dst") != nil {
		t.Error("destination must be positional, not --dst")
	}
	if flag := cmd.Flags().ShorthandLookup("R"); flag == nil || flag.Name != "repo" {
		t.Error("-R must select the source repository")
	}
	if types.DefaultDependabotCopyBranch != "gh-secret-kit-dependabot-copy" {
		t.Errorf("branch prefix = %q", types.DefaultDependabotCopyBranch)
	}
}

func TestCopyFlagParsing(t *testing.T) {
	cmd := NewCopyCmd()
	args := []string{
		"-R", "source/repo", "--scope", "org", "--dst-app", "actions",
		"--secrets", "API_KEY,DB_PASSWORD", "--secrets", "OTHER",
		"--exclude-secrets", "SKIPPED", "--exclude-secrets", "ALSO_SKIPPED",
		"--rename", "API_KEY=PROD_API_KEY", "--rename", "OTHER=PROD_OTHER",
		"--overwrite", "--dst-token", "placeholder",
		"--token-secret-name", "COPY_TOKEN", "--branch", "copy-branch",
		"--workflow-name", "custom-copy", "--runner-label", "self-hosted",
		"--timeout", "1h", "--keep-workflow", "dest-org", "enterprise.example/other-org",
	}
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{
		"repo": "source/repo", "scope": "org", "dst-app": "actions",
		"dst-token": "placeholder", "token-secret-name": "COPY_TOKEN",
		"branch": "copy-branch", "workflow-name": "custom-copy",
		"runner-label": "self-hosted", "timeout": "1h",
	} {
		if got, err := cmd.Flags().GetString(name); err != nil || got != want {
			t.Errorf("--%s = %q, %v; want %q", name, got, err, want)
		}
	}
	for name, want := range map[string][]string{
		"secrets":         {"API_KEY", "DB_PASSWORD", "OTHER"},
		"exclude-secrets": {"SKIPPED", "ALSO_SKIPPED"},
		"rename":          {"API_KEY=PROD_API_KEY", "OTHER=PROD_OTHER"},
	} {
		if got, err := cmd.Flags().GetStringSlice(name); err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("--%s = %v, %v; want %v", name, got, err, want)
		}
	}
	for _, name := range []string{"overwrite", "keep-workflow"} {
		if got, err := cmd.Flags().GetBool(name); err != nil || !got {
			t.Errorf("--%s = %v, %v; want true", name, got, err)
		}
	}
	wantDestinations := []string{"dest-org", "enterprise.example/other-org"}
	if got := cmd.Flags().Args(); !reflect.DeepEqual(got, wantDestinations) {
		t.Errorf("destinations = %v, want %v", got, wantDestinations)
	}
	if err := cmd.Args(cmd, cmd.Flags().Args()); err != nil {
		t.Errorf("multiple destinations rejected: %v", err)
	}
}

func TestCopyInvalidArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing destination", []string{}, "requires at least 1 arg"},
		{"destination flag", []string{"--dst", "owner/repo"}, "unknown flag: --dst"},
		{"environment scope", []string{"--scope", "env", "owner/repo"}, "invalid --scope"},
		{"unknown scope", []string{"--scope", "other", "owner/repo"}, "invalid --scope"},
		{"empty scope", []string{"--scope=", "owner/repo"}, "invalid --scope"},
		{"unknown store", []string{"--dst-app", "other", "owner/repo"}, "dst-app"},
		{"token instead of name", []string{"--token-secret-name", "ghp_placeholder", "owner/repo"}, "invalid --token-secret-name"},
		{"fine-grained token instead of name", []string{"--token-secret-name", "github_pat_placeholder", "owner/repo"}, "invalid --token-secret-name"},
		{"invalid timeout", []string{"--timeout", "later", "owner/repo"}, "invalid --timeout"},
		{"zero timeout", []string{"--timeout", "0s", "owner/repo"}, "expected a positive duration"},
		{"negative timeout", []string{"--timeout", "-1m", "owner/repo"}, "expected a positive duration"},
		{"overflow timeout", []string{"--timeout", "999999999999999h", "owner/repo"}, "invalid --timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := NewCopyCmd()
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want error containing %q", err, tc.want)
			}
		})
	}
}

func TestCopySupportedStoresAndScopes(t *testing.T) {
	for _, scope := range []string{"repo", "org"} {
		for _, store := range []string{"actions", "agents", "codespaces", "dependabot"} {
			t.Run(scope+"/"+store, func(t *testing.T) {
				cmd := NewCopyCmd()
				cmd.SetOut(io.Discard)
				cmd.SetErr(io.Discard)
				// Stop before authentication after validating scope and store.
				cmd.SetArgs([]string{"--scope", scope, "--dst-app", store, "--timeout", "0s", "destination"})
				err := cmd.Execute()
				if err == nil || !strings.Contains(err.Error(), "expected a positive duration") {
					t.Fatalf("valid scope/store rejected: %v", err)
				}
			})
		}
	}
}
