// Package runlog extracts copy progress and failures from GitHub Actions logs.
package runlog

import (
	"strings"

	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// Contains reports whether a marker was actually printed, not merely echoed as
// part of the step script before execution.
func Contains(log, marker string) bool {
	for _, line := range Lines(log) {
		if line == marker {
			return true
		}
	}
	return false
}

// ReportCopyResults reports the progress lines printed by the copy script.
func ReportCopyResults(log string) {
	for _, line := range Lines(log) {
		if strings.HasPrefix(line, "Successfully migrated secret:") ||
			strings.HasPrefix(line, "Copying secrets to ") ||
			strings.HasPrefix(line, "Secret ") {
			logger.Info(line)
		}
	}
}

// FailureDetail collects Actions errors for appending to a failure message.
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

// Lines drops timestamps and echoed step scripts, including uncolored script
// preambles. Script output outside those preambles is preserved.
func Lines(log string) []string {
	raw := strings.Split(log, "\n")
	lines := make([]string, 0, len(raw))
	inScript := false
	for _, rawLine := range raw {
		_, line, ok := strings.Cut(strings.TrimRight(rawLine, "\r"), " ")
		if !ok {
			continue
		}
		if strings.HasPrefix(line, "##[group]Run ") {
			inScript = true
			continue
		}
		if inScript {
			if strings.HasPrefix(line, "##[endgroup]") {
				inScript = false
			}
			continue
		}
		if strings.Contains(line, "\x1b[") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
