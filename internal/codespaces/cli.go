package codespaces

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	ghcli "github.com/cli/go-gh/v2"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// createOptions are the codespace settings that the copy command exposes.
type createOptions struct {
	Repo             repository.Repository
	Branch           string
	Machine          string
	DevcontainerPath string
	IdleTimeout      string
	RetentionPeriod  string
	DisplayName      string
}

// runGH executes the gh CLI and returns its standard output. GH_HOST and
// GH_REPO are dropped from the environment because Codespaces only exists on
// github.com and an inherited override would target the wrong host.
func runGH(ctx context.Context, args ...string) (string, error) {
	path, err := ghcli.Path()
	if err != nil {
		return "", fmt.Errorf("failed to locate the gh CLI: %w", err)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = ghEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return stdout.String(), fmt.Errorf("%w: %s", err, msg)
		}
		return stdout.String(), err
	}
	return stdout.String(), nil
}

// ghEnv returns the current environment without the host overrides.
func ghEnv() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		if key == "GH_HOST" || key == "GH_REPO" {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}

// createCodespace creates a codespace for the source repository and returns its
// name. gh blocks until the codespace is available.
func createCodespace(ctx context.Context, opts createOptions) (string, error) {
	args := []string{"codespace", "create", "--repo", opts.Repo.Owner + "/" + opts.Repo.Name, "--default-permissions"}
	if opts.Branch != "" {
		args = append(args, "--branch", opts.Branch)
	}
	if opts.Machine != "" {
		args = append(args, "--machine", opts.Machine)
	}
	if opts.DevcontainerPath != "" {
		args = append(args, "--devcontainer-path", opts.DevcontainerPath)
	}
	if opts.IdleTimeout != "" {
		args = append(args, "--idle-timeout", opts.IdleTimeout)
	}
	if opts.RetentionPeriod != "" {
		args = append(args, "--retention-period", opts.RetentionPeriod)
	}
	if opts.DisplayName != "" {
		args = append(args, "--display-name", opts.DisplayName)
	}

	out, err := runGH(ctx, args...)
	if err != nil {
		return "", fmt.Errorf("failed to create a codespace: %w", err)
	}
	name := strings.TrimSpace(out)
	// gh may print progress lines before the name; the name is the last line.
	if lines := strings.Split(name, "\n"); len(lines) > 0 {
		name = strings.TrimSpace(lines[len(lines)-1])
	}
	if name == "" {
		return "", fmt.Errorf("failed to determine the created codespace name")
	}
	return name, nil
}

// copyToCodespace copies a local file to remotePath inside the codespace.
// "-e" is required for an absolute remote path: without it the path is escaped
// and the remote scp treats the quotes as part of the file name.
func copyToCodespace(ctx context.Context, name, localPath, remotePath string) error {
	if _, err := runGH(ctx, "codespace", "cp", "-e", "--codespace", name, localPath, "remote:"+remotePath); err != nil {
		return fmt.Errorf("failed to copy %s into the codespace: %w", remotePath, err)
	}
	return nil
}

// execInCodespace runs a command in the codespace over SSH and returns its
// standard output.
func execInCodespace(ctx context.Context, name, command string) (string, error) {
	return runGH(ctx, "codespace", "ssh", "--codespace", name, command)
}

// deleteCodespace removes the codespace, logging instead of failing so that the
// remaining cleanup still runs.
func deleteCodespace(ctx context.Context, name string) {
	logger.Info(fmt.Sprintf("Deleting codespace %s...", name))
	if _, err := runGH(ctx, "codespace", "delete", "--codespace", name, "--force"); err != nil {
		logger.Warn(fmt.Sprintf("failed to delete codespace %s: %v", name, err))
	}
}
