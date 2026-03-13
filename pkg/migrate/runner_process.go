package migrate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

const (
	// DefaultRunnerVersion is the default GitHub Actions runner version to download
	DefaultRunnerVersion = "2.332.0"
	// RunnerReadyMessage is the message printed by the runner when it's ready for jobs
	RunnerReadyMessage = "Listening for Jobs"
	// RunnerStartTimeout is the maximum time to wait for the runner to become ready
	RunnerStartTimeout = 120 * time.Second
	// runnerDirName is the subdirectory name for runner binary storage
	runnerDirName = "runner"
)

// RunnerBinaryInfo holds information about the runner binary download
type RunnerBinaryInfo struct {
	OS       string
	Arch     string
	Version  string
	URL      string
	Filename string
}

// FetchLatestRunnerVersion fetches the latest GitHub Actions runner version from the GitHub API.
// Falls back to DefaultRunnerVersion if the API call fails.
func FetchLatestRunnerVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/repos/actions/runner/releases/latest", nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch latest runner version: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	// tag_name is "vX.Y.Z", strip the leading "v"
	version := strings.TrimPrefix(release.TagName, "v")
	if version == "" {
		return "", fmt.Errorf("empty tag_name in response")
	}
	return version, nil
}

// DetectRunnerBinary detects the appropriate runner binary for the current platform.
// If version is empty, it fetches the latest release version from GitHub.
func DetectRunnerBinary(version string) (*RunnerBinaryInfo, error) {
	if version == "" {
		var err error
		version, err = FetchLatestRunnerVersion(context.Background())
		if err != nil {
			version = DefaultRunnerVersion
		}
	}

	osName := ""
	switch runtime.GOOS {
	case "darwin":
		osName = "osx"
	case "linux":
		osName = "linux"
	case "windows":
		osName = "win"
	default:
		return nil, fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}

	archName := ""
	switch runtime.GOARCH {
	case "amd64":
		archName = "x64"
	case "arm64":
		archName = "arm64"
	default:
		return nil, fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}

	ext := "tar.gz"
	if runtime.GOOS == "windows" {
		ext = "zip"
	}

	filename := fmt.Sprintf("actions-runner-%s-%s-%s.%s", osName, archName, version, ext)
	downloadURL := fmt.Sprintf("https://github.com/actions/runner/releases/download/v%s/%s", version, filename)

	return &RunnerBinaryInfo{
		OS:       osName,
		Arch:     archName,
		Version:  version,
		URL:      downloadURL,
		Filename: filename,
	}, nil
}

// RunnerDirPath returns the default directory path for the runner binary
func RunnerDirPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	return filepath.Join(homeDir, ".gh-secret-kit", runnerDirName), nil
}

// GenerateRunnerName generates a unique runner name
func GenerateRunnerName() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("gh-secret-kit-%s", hex.EncodeToString(b))
}

// DownloadRunnerBinary downloads and extracts the runner binary to the specified directory
func DownloadRunnerBinary(ctx context.Context, downloadURL, destDir string) error {
	// Create destination directory
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	// Check if runner binary already exists
	runScript := "run.sh"
	if runtime.GOOS == "windows" {
		runScript = "run.cmd"
	}
	if _, err := os.Stat(filepath.Join(destDir, runScript)); err == nil {
		// Runner binary already exists, skip download
		return nil
	}

	// Download the runner binary
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download runner binary: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download runner binary: HTTP %d", resp.StatusCode)
	}

	// Extract the archive
	switch {
	case strings.HasSuffix(downloadURL, ".tar.gz"):
		return extractTarGz(resp.Body, destDir)
	case strings.HasSuffix(downloadURL, ".zip"):
		return extractZipFromReader(resp.Body, destDir)
	default:
		return fmt.Errorf("unsupported archive format for URL: %s (only .tar.gz and .zip are supported)", downloadURL)
	}
}

// extractTarGz extracts a tar.gz archive to a destination directory
func extractTarGz(r io.Reader, destDir string) (err error) {
	gzr, gzrErr := gzip.NewReader(r)
	if gzrErr != nil {
		return fmt.Errorf("failed to create gzip reader: %w", gzrErr)
	}
	defer func() {
		if cerr := gzr.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close gzip reader: %w", cerr)
		}
	}()

	tr := tar.NewReader(gzr)
	cleanDest := filepath.Clean(destDir)

	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		target := filepath.Join(destDir, header.Name)

		// Security check: prevent path traversal
		if !strings.HasPrefix(filepath.Clean(target), cleanDest) {
			return fmt.Errorf("invalid file path in archive: %s", header.Name)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("failed to create parent directory for %s: %w", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to write file %s: %w", target, err)
			}
			if err := f.Close(); err != nil {
				return fmt.Errorf("failed to close file %s: %w", target, err)
			}
		case tar.TypeSymlink:
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink %s: %w", target, err)
			}
		}
	}

	return nil
}

// extractZipFromReader writes the reader contents to a temporary file and then extracts the ZIP archive.
// archive/zip requires io.ReaderAt, so the response body must be buffered to disk first.
func extractZipFromReader(r io.Reader, destDir string) error {
	tmpFile, err := os.CreateTemp("", "runner-*.zip")
	if err != nil {
		return fmt.Errorf("failed to create temp file for zip: %w", err)
	}
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	if _, err := io.Copy(tmpFile, r); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write zip to temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	return extractZip(tmpFile.Name(), destDir)
}

// extractZip extracts a ZIP archive to a destination directory.
func extractZip(zipPath, destDir string) (err error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip archive: %w", err)
	}
	defer func() {
		if cerr := zr.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close zip archive: %w", cerr)
		}
	}()

	cleanDest := filepath.Clean(destDir)

	for _, f := range zr.File {
		target := filepath.Join(destDir, f.Name)

		// Security check: prevent path traversal
		if !strings.HasPrefix(filepath.Clean(target), cleanDest) {
			return fmt.Errorf("invalid file path in archive: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, f.Mode()); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", target, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return fmt.Errorf("failed to create parent directory for %s: %w", target, err)
		}

		src, err := f.Open()
		if err != nil {
			return fmt.Errorf("failed to open zip entry %s: %w", f.Name, err)
		}

		dst, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, f.Mode())
		if err != nil {
			_ = src.Close()
			return fmt.Errorf("failed to create file %s: %w", target, err)
		}

		if _, err := io.Copy(dst, src); err != nil {
			_ = src.Close()
			_ = dst.Close()
			return fmt.Errorf("failed to write file %s: %w", target, err)
		}

		_ = src.Close()
		if err := dst.Close(); err != nil {
			return fmt.Errorf("failed to close file %s: %w", target, err)
		}
	}

	return nil
}

// ConfigureRunner configures the runner using config.sh with explicit labels.
// runnerBinDir must be a per-runner instance directory created by
// CreateRunnerInstanceDir; config.sh writes .runner and .credentials into
// runnerBinDir (the script's own directory).
// Pass an empty string for workDir to use runnerBinDir.
// This is used instead of JIT config on GHES where JIT runners may not inherit
// scale set labels.
func ConfigureRunner(runnerBinDir, workDir, configURL, token, name, labels string) error {
	if workDir == "" {
		workDir = runnerBinDir
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return fmt.Errorf("failed to create runner work directory: %w", err)
	}

	configScript := "config.sh"
	if runtime.GOOS == "windows" {
		configScript = "config.cmd"
	}

	scriptPath := filepath.Join(runnerBinDir, configScript)
	args := []string{
		"--url", configURL,
		"--token", token,
		"--name", name,
		"--labels", labels,
		"--ephemeral",
		"--disableupdate",
		"--unattended",
		"--replace",
	}

	cmd := exec.Command(scriptPath, args...)
	cmd.Dir = workDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to configure runner: %w\noutput: %s", err, string(output))
	}

	return nil
}

// StartRunner starts the runner binary (run.sh / run.cmd).
// runnerBinDir is the directory containing the runner binaries (run.sh).
// workDir is the working directory for the runner process; pass an empty string
// to use runnerBinDir.
// jitConfig is the base64-encoded JIT configuration passed via the
// ACTIONS_RUNNER_INPUT_JITCONFIG environment variable. When empty, the runner
// reads its registration from the .runner/.credentials files in workDir
// (config.sh mode).
// logPath is the file path where stdout/stderr of the runner process are
// written; pass an empty string to default to <workDir>/runner.log.
// stdout/stderr are redirected to logPath and cmd.Start() is called, so the
// process runs concurrently with the caller. The caller (or the watcher
// goroutine started by watchRunner) is responsible for calling cmd.Wait().
func StartRunner(runnerBinDir, workDir, jitConfig, logPath string) (*exec.Cmd, error) {
	if workDir == "" {
		workDir = runnerBinDir
	}

	runScript := "run.sh"
	if runtime.GOOS == "windows" {
		runScript = "run.cmd"
	}

	scriptPath := filepath.Join(runnerBinDir, runScript)
	cmd := exec.Command(scriptPath)
	cmd.Dir = workDir
	if jitConfig != "" {
		cmd.Env = append(os.Environ(), fmt.Sprintf("ACTIONS_RUNNER_INPUT_JITCONFIG=%s", jitConfig))
	}

	if logPath == "" {
		logPath = filepath.Join(workDir, "runner.log")
	}

	// Redirect stdout/stderr to a log file.
	// The file is closed in the parent after cmd.Start() — the child inherits
	// its own file descriptor and keeps writing to the log independently.
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create runner log file: %w", err)
	}

	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("failed to start runner process: %w", err)
	}

	// Close the parent's file descriptor now that the child has inherited it.
	_ = logFile.Close()

	return cmd, nil
}

// WaitForRunnerReady waits for the runner to become ready by polling
// the runner's log file for the readiness message.
func WaitForRunnerReady(ctx context.Context, logPath string, timeout time.Duration) error {
	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			// Read last log content for debugging
			data, _ := os.ReadFile(logPath)
			return fmt.Errorf("runner did not become ready within %v; last log:\n%s", timeout, string(data))
		case <-ticker.C:
			data, err := os.ReadFile(logPath)
			if err != nil {
				continue // Log file might not exist yet
			}
			if strings.Contains(string(data), RunnerReadyMessage) {
				return nil
			}
		}
	}
}

// StopRunner stops a running runner process by PID
func StopRunner(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find runner process (PID %d): %w", pid, err)
	}

	// Try graceful shutdown with SIGINT first
	if err := process.Signal(os.Interrupt); err != nil {
		// Process might already be dead or we can't signal it
		_ = process.Kill()
	}
	return nil
}

// RemoveRunner deregisters a previously configured runner by running
// config.sh remove --unattended in instanceDir. The .runner and .credentials
// files stored in instanceDir are used for authentication, so no extra token is
// required. Returns nil if no .runner file exists (nothing to remove).
func RemoveRunner(instanceDir string) error {
	// Skip if the runner was never configured in this directory
	if _, err := os.Stat(filepath.Join(instanceDir, ".runner")); os.IsNotExist(err) {
		return nil
	}

	configScript := "config.sh"
	if runtime.GOOS == "windows" {
		configScript = "config.cmd"
	}

	scriptPath := filepath.Join(instanceDir, configScript)
	if _, err := os.Stat(scriptPath); os.IsNotExist(err) {
		// config.sh is missing even though .runner exists: the runner may still be
		// registered on the server side but cannot be deregistered locally.
		return fmt.Errorf("runner config file %s not found in %s; runner may still be registered on the server", configScript, instanceDir)
	}

	cmd := exec.Command(scriptPath, "remove", "--unattended")
	cmd.Dir = instanceDir

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to remove runner: %w; output: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// RemoveRunnerInstances calls RemoveRunner on every subdirectory of instancesBaseDir
// that contains a .runner file. Errors are logged as warnings so that cleanup
// continues even if some runners cannot be deregistered.
func RemoveRunnerInstances(instancesBaseDir string) {
	entries, err := os.ReadDir(instancesBaseDir)
	if os.IsNotExist(err) {
		return // nothing to do
	}
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		instanceDir := filepath.Join(instancesBaseDir, entry.Name())
		if err := RemoveRunner(instanceDir); err != nil {
			// Non-fatal: log and continue
			logger.Warn("failed to remove runner instance in %s: %v", instanceDir, err)
		}
	}
}

// CleanupRunnerDir removes the runner directory and all its contents
func CleanupRunnerDir(dir string) error {
	return os.RemoveAll(dir)
}

// RunnerInstancesBaseDir returns the base directory for per-runner instance
// subdirectories. It is intentionally a sibling of runnerDir (not a child)
// to avoid infinite recursion when WalkDir is used in CreateRunnerInstanceDir.
func RunnerInstancesBaseDir(runnerDir string) string {
	return runnerDir + "-instances"
}

// CreateRunnerInstanceDir creates a per-runner directory by hard-linking all
// files from templateDir into instanceDir. instanceDir MUST NOT be a
// subdirectory of templateDir to avoid infinite WalkDir recursion.
// Each concurrent runner must have its own instance directory so that
// config.sh can write independent .runner/.credentials files without
// conflicting with other running instances.
// Hard links share the same inode, so this operation uses virtually no extra
// disk space. If a hard link cannot be created (e.g., cross-filesystem), the
// file is copied instead.
func CreateRunnerInstanceDir(templateDir, instanceDir string) error {
	return filepath.WalkDir(templateDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(templateDir, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(instanceDir, rel)

		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}

		// Skip runner config files that belong to a specific instance
		switch d.Name() {
		case ".runner", ".credentials", ".credentials_rsaparams":
			return nil
		}

		// Hard-link the file; fall back to copy if hard link fails
		if linkErr := os.Link(path, dst); linkErr != nil {
			return copyFile(path, dst)
		}
		return nil
	})
}

// copyFile copies a single file from src to dst, preserving its mode bits.
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}
