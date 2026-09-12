// Package runlog extracts the progress and failure information of a copy from a
// GitHub Actions job log, for the copy commands that run the copy inside a
// workflow run and can only observe it through that log.
package runlog

import (
	"strings"

	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// Contains reports whether the marker was printed by the copy script. The
// marker is matched against the filtered lines, because GitHub Actions echoes
// the whole step script into the log before running it and the echo of the
// marker would otherwise be mistaken for the marker itself.
func Contains(log, marker string) bool {
	for _, line := range Lines(log) {
		if strings.Contains(line, marker) {
			return true
		}
	}
	return false
}

// ReportCopyResults reports the progress lines the copy script printed.
func ReportCopyResults(log string) {
	for _, line := range Lines(log) {
		if strings.HasPrefix(line, "Successfully migrated secret:") || strings.HasPrefix(line, "Copying secrets to ") {
			logger.Info(line)
		}
	}
}

// FailureDetail collects the errors GitHub Actions reported, so that a failure
// of the copy script is visible without opening the run. The returned string is
// ready to be appended to an error message, and empty when no error was
// reported.
func FailureDetail(log string) string {
	var details []string
	for _, line := range Lines(log) {
		if after, ok := strings.CutPrefix(line, "##[error]"); ok {
			details = append(details, strings.TrimSpace(after))
		}
	}
	if len(details) == 0 {
		return ""
	}
	return ": " + strings.Join(details, "; ")
}

// Lines splits an Actions job log into lines, dropping the timestamp prefix and
// the echoed step script, which is colored and would match the markers.
func Lines(log string) []string {
	raw := strings.Split(log, "\n")
	lines := make([]string, 0, len(raw))
	for _, line := range raw {
		if strings.Contains(line, "\x1b[") {
			continue
		}
		if _, rest, ok := strings.Cut(strings.TrimRight(line, "\r"), " "); ok {
			lines = append(lines, rest)
		}
	}
	return lines
}
