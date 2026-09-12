package dependabot

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/ghexec"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// secretAppFlag selects the Dependabot secret store of the gh CLI.
const secretAppFlag = "dependabot"

// ownerNamePattern matches the characters GitHub allows in an owner or a
// repository name. Names are validated before they are interpolated into an API
// path so a crafted name cannot escape it.
var ownerNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// validateRepository reports whether repo can be used to build an API path.
func validateRepository(repo repository.Repository, requireName bool) error {
	if !ownerNamePattern.MatchString(repo.Owner) {
		return fmt.Errorf("invalid repository owner %q", repo.Owner)
	}
	if requireName && !ownerNamePattern.MatchString(repo.Name) {
		return fmt.Errorf("invalid repository name %q", repo.Name)
	}
	return nil
}

// hostArgs returns the gh CLI arguments that target the host of repo. An empty
// host leaves the gh CLI default in place.
func hostArgs(repo repository.Repository) []string {
	if repo.Host == "" {
		return nil
	}
	return []string{"--hostname", repo.Host}
}

// repoFlag returns the [HOST/]OWNER/REPO value for the gh CLI "--repo" flag, so
// that the command targets the right host even on GitHub Enterprise Server.
func repoFlag(repo repository.Repository) string {
	slug := repo.Owner + "/" + repo.Name
	if repo.Host == "" {
		return slug
	}
	return repo.Host + "/" + slug
}

// listRepoSecretNames returns the names of the Dependabot secrets of the
// repository. Dependabot secret values are never returned by the API; only the
// names are available.
func listRepoSecretNames(ctx context.Context, repo repository.Repository) ([]string, error) {
	if err := validateRepository(repo, true); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("repos/%s/%s/dependabot/secrets", repo.Owner, repo.Name)
	return listSecretNames(ctx, repo, path)
}

// listOrgSecretNames returns the names of the Dependabot secrets of the
// organization that owns repo.
func listOrgSecretNames(ctx context.Context, repo repository.Repository) ([]string, error) {
	if err := validateRepository(repo, false); err != nil {
		return nil, err
	}
	path := fmt.Sprintf("orgs/%s/dependabot/secrets", repo.Owner)
	return listSecretNames(ctx, repo, path)
}

// listSecretNames reads the secret names from a Dependabot secrets API path.
func listSecretNames(ctx context.Context, repo repository.Repository, path string) ([]string, error) {
	args := append([]string{"api"}, hostArgs(repo)...)
	args = append(args, "--paginate", "--jq", ".secrets[].name", path)
	out, err := ghexec.Run(ctx, args...)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0)
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	return names, nil
}

// setRepoSecret stores a Dependabot secret on the repository. The value is
// passed through an owner-only temporary dotenv file instead of an argument, so
// it is not visible in the process list.
func setRepoSecret(ctx context.Context, repo repository.Repository, name, value string) error {
	if err := validateRepository(repo, true); err != nil {
		return err
	}
	path, err := writeSecretEnvFile(name, value)
	if err != nil {
		return err
	}
	defer func() {
		if rerr := os.Remove(path); rerr != nil {
			logger.Warn(fmt.Sprintf("failed to remove the temporary secret file %s: %v", path, rerr))
		}
	}()

	_, err = ghexec.Run(ctx, "secret", "set", "--app", secretAppFlag, "--repo", repoFlag(repo), "--env-file", path)
	return err
}

// deleteRepoSecret removes a Dependabot secret from the repository.
func deleteRepoSecret(ctx context.Context, repo repository.Repository, name string) error {
	if err := validateRepository(repo, true); err != nil {
		return err
	}
	_, err := ghexec.Run(ctx, "secret", "delete", "--app", secretAppFlag, "--repo", repoFlag(repo), name)
	return err
}

// writeSecretEnvFile writes a single NAME='VALUE' entry to an owner-only
// temporary file in the dotenv format that "gh secret set --env-file" reads.
// The value is single quoted so that no character in it is expanded.
func writeSecretEnvFile(name, value string) (string, error) {
	if strings.ContainsAny(value, "\n\r'") {
		return "", fmt.Errorf("the token for secret %s contains characters that cannot be transferred", name)
	}
	file, err := os.CreateTemp("", "gh-secret-kit-dependabot-*.env")
	if err != nil {
		return "", fmt.Errorf("failed to create the temporary secret file: %w", err)
	}
	path := file.Name()
	if _, err := fmt.Fprintf(file, "%s='%s'\n", name, value); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("failed to write the temporary secret file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("failed to close the temporary secret file: %w", err)
	}
	return path, nil
}
