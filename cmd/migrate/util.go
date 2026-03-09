package migrate

import (
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
)

// shellQuote wraps s in single quotes, escaping any embedded single quotes,
// so the value is safe to embed in a POSIX shell script regardless of spaces
// or special characters.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// repoArg returns the "HOST/OWNER/REPO" or "OWNER/REPO" string for a repository.
func repoArg(r repository.Repository) string {
	if r.Host != "" {
		return r.Host + "/" + r.Owner + "/" + r.Name
	}
	return r.Owner + "/" + r.Name
}
