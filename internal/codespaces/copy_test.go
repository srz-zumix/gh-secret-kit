package codespaces

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/srz-zumix/gh-secret-kit/internal/destination"
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
	if !(trapIdx < decodeIdx && decodeIdx < chmodIdx && chmodIdx < bashIdx) {
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
	defer os.Remove(path)

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
			os.Remove(path)
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
