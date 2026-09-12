package runlog

import (
	"testing"
)

const marker = "gh-secret-kit: copy finished"

func TestContainsIgnoresTheEchoedScript(t *testing.T) {
	// GitHub Actions echoes the step script into the log in color before it runs
	// it, so the marker appears there even when the copy never completed.
	echoed := "2024-01-02T03:04:05.0000000Z \x1b[36;1mecho '" + marker + "'\x1b[0m\n"
	if Contains(echoed, marker) {
		t.Errorf("Contains() matched the echoed step script")
	}

	printed := echoed + "2024-01-02T03:04:06.0000000Z " + marker + "\n"
	if !Contains(printed, marker) {
		t.Errorf("Contains() did not match the marker the script printed")
	}
}

func TestFailureDetail(t *testing.T) {
	log := "2024-01-02T03:04:05.0000000Z ##[error]Process completed with exit code 1.\n"
	if got, want := FailureDetail(log), ": Process completed with exit code 1."; got != want {
		t.Errorf("FailureDetail() = %q, want %q", got, want)
	}
	if got := FailureDetail(""); got != "" {
		t.Errorf("FailureDetail() = %q, want an empty string", got)
	}
}
