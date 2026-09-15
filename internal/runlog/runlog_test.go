package runlog

import (
	"reflect"
	"strings"
	"testing"
)

func TestContains(t *testing.T) {
	const marker = "gh-secret-kit: copy finished"
	const timestamp = "2026-09-12T03:04:05.0000000Z "
	cases := []struct {
		name string
		log  string
		want bool
	}{
		{"empty", "", false},
		{"colored script", timestamp + "\x1b[36;1mecho '" + marker + "'\x1b[0m\n", false},
		{"uncolored script", timestamp + "echo '" + marker + "'\n", false},
		{"marker inside preamble", timestamp + "##[group]Run cat <<'EOF'\n" + timestamp + marker + "\n" + timestamp + "EOF\n" + timestamp + "##[endgroup]\n", false},
		{"real output", timestamp + marker + "\r\n", true},
		{"unrelated output mentioning marker", timestamp + "failed before " + marker + "\n", false},
		{"real output after preamble", timestamp + "##[group]Run echo '" + marker + "'\n" + timestamp + "##[endgroup]\n" + timestamp + marker + "\n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Contains(tc.log, marker); got != tc.want {
				t.Errorf("Contains() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFailureDetail(t *testing.T) {
	log := "2026-09-12T03:04:05Z ##[error]Process completed with exit code 1.\r\n" +
		"2026-09-12T03:04:06Z ##[error] permission denied\n"
	if got, want := FailureDetail(log), ": Process completed with exit code 1.; permission denied"; got != want {
		t.Errorf("FailureDetail() = %q, want %q", got, want)
	}
	if got := FailureDetail(""); got != "" {
		t.Errorf("FailureDetail() = %q, want empty", got)
	}
}

func TestLines(t *testing.T) {
	log := strings.Join([]string{
		"2026-09-12T03:04:05Z ##[group]Run \x1b[36;1mecho hello\x1b[0m",
		"2026-09-12T03:04:05Z shell: /usr/bin/bash",
		"2026-09-12T03:04:05Z ##[endgroup]",
		"2026-09-12T03:04:06Z Copying secrets to owner/repo (github.com)",
		"2026-09-12T03:04:07Z Successfully migrated secret: FOO\r",
	}, "\n")
	want := []string{"Copying secrets to owner/repo (github.com)", "Successfully migrated secret: FOO"}
	if got := Lines(log); !reflect.DeepEqual(got, want) {
		t.Errorf("Lines() = %q, want %q", got, want)
	}
}
