// Package codespaces copies Codespaces development environment secrets by
// running the copy inside an ephemeral codespace.
package codespaces

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/gh-secret-kit/internal/destination"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// remoteTokenFile is the path, inside the codespace, of the file that carries
// the destination tokens. It is absolute because the working directory of a
// codespace ssh session is not guaranteed.
const remoteTokenFile = "/tmp/gh-secret-kit-copy.env"

// remoteScriptFile is the path, inside the codespace, of the generated copy
// script.
const remoteScriptFile = "/tmp/gh-secret-kit-copy.sh"

// CopyConfig holds configuration for the Codespaces secret copy.
//
// Codespaces secret values cannot be read through the API; they are only
// exposed as environment variables inside a running codespace. The copy
// therefore creates an ephemeral codespace on the source repository and runs
// the copy from within it, so the values never reach the local machine. The
// codespace is deleted once the copy finishes.
type CopyConfig struct {
	Source string
	// Destinations are [HOST/]OWNER/REPO references, or [HOST/]ORG when Scope
	// is SecretScopeOrg.
	Destinations   []string
	Secrets        []string
	ExcludeSecrets []string
	Rename         []string
	Overwrite      bool
	// Scope selects both the source Codespaces secret level and the destination
	// level. Only repo and org are supported.
	Scope migrator.SecretScope
	// IncludeUserSecrets adds the Codespaces secrets of the authenticated user
	// to the copy. They require a token with the codespace scope.
	IncludeUserSecrets bool
	// DestinationApp selects which secret store the destination secrets are
	// written to. Empty means migrator.SecretAppCodespaces.
	DestinationApp migrator.SecretApp
	// DestinationToken overrides the token resolved from the local gh
	// authentication for the destination host. It cannot be used when the
	// destinations span multiple hosts.
	DestinationToken string
	// TokenEnvName is the base name of the environment variable that holds the
	// destination token inside the codespace. The destination host is appended.
	TokenEnvName string
	// Branch is the source repository branch the codespace is created from.
	Branch string
	// DevcontainerPath is the devcontainer.json used for the codespace.
	DevcontainerPath string
	Machine          string
	IdleTimeout      string
	RetentionPeriod  string
	// KeepCodespace leaves the codespace running after the copy, for debugging.
	KeepCodespace bool
}

// RunCopy copies the Codespaces secrets available to the source repository to
// every destination, by running the copy inside an ephemeral codespace.
func RunCopy(ctx context.Context, config *CopyConfig) error {
	logger.Info("Copying Codespaces secrets")

	scope := config.Scope
	if scope == "" {
		scope = migrator.SecretScopeRepo
	}
	if scope != migrator.SecretScopeRepo && scope != migrator.SecretScopeOrg {
		return fmt.Errorf("unsupported --scope %q for Codespaces secrets: expected repo or org", scope)
	}

	app := config.DestinationApp
	if app == "" {
		app = migrator.SecretAppCodespaces
	}
	if err := migrator.ValidateSecretApp(app); err != nil {
		return err
	}

	sourceRepo, err := parser.Repository(parser.RepositoryInput(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}
	if sourceRepo.Host != "" && sourceRepo.Host != "github.com" {
		return fmt.Errorf("codespaces is only available on github.com, but the source host is %s", sourceRepo.Host)
	}
	if sourceRepo.Name == "" {
		return fmt.Errorf("source must be in [HOST/]OWNER/REPO format")
	}

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	secrets, err := collectSecrets(ctx, client, sourceRepo, config, scope)
	if err != nil {
		return err
	}
	if len(secrets) == 0 {
		logger.Info("No Codespaces secrets found to copy, skipping")
		return nil
	}

	orgLevel := scope == migrator.SecretScopeOrg
	destinations, err := destination.Resolve(config.Destinations, orgLevel, sourceRepo)
	if err != nil {
		return err
	}
	hostTokens, err := destination.ResolveTokens(config.DestinationToken, destinations)
	if err != nil {
		return err
	}
	if err := destination.Verify(ctx, orgLevel, destinations, hostTokens); err != nil {
		return err
	}

	renameMap, err := migrator.ParseRenameMappings(config.Rename)
	if err != nil {
		return err
	}

	tokenEnvNames := make(map[string]string, len(hostTokens))
	for host := range hostTokens {
		tokenEnvNames[host] = destination.NameForHost(config.TokenEnvName, host)
	}

	scriptConfig := migrator.CodespacesCopyConfig{
		Scope:          scope,
		DestinationApp: app,
		Secrets:        secrets,
		Rename:         renameMap,
		Overwrite:      config.Overwrite,
		TokenEnvFile:   remoteTokenFile,
		Destinations:   buildScriptDestinations(destinations, tokenEnvNames),
	}
	script, err := migrator.GenerateCodespacesCopyScript(scriptConfig)
	if err != nil {
		return fmt.Errorf("failed to generate the copy script: %w", err)
	}

	tokenFile, err := writeTokenFile(destinations, tokenEnvNames, hostTokens)
	if err != nil {
		return err
	}
	defer func() {
		if rerr := os.Remove(tokenFile); rerr != nil && !os.IsNotExist(rerr) {
			logger.Warn(fmt.Sprintf("Failed to remove temporary token file %s: %v", tokenFile, rerr))
		}
	}()

	// "gh codespace create" prompts for a machine type when it is omitted, which
	// fails because the codespace is created without a terminal.
	machine := config.Machine
	if machine == "" {
		machine, err = resolveMachine(ctx, client, sourceRepo, config.Branch)
		if err != nil {
			return err
		}
	}

	logger.Info(fmt.Sprintf("Copying %d Codespaces secrets to %d destinations", len(secrets), len(destinations)))
	logger.Info("Creating a codespace on the source repository...")
	name, err := createCodespace(ctx, createOptions{
		Repo:             sourceRepo,
		Branch:           config.Branch,
		Machine:          machine,
		DevcontainerPath: config.DevcontainerPath,
		IdleTimeout:      config.IdleTimeout,
		RetentionPeriod:  config.RetentionPeriod,
		DisplayName:      "gh-secret-kit copy",
	})
	if err != nil {
		return err
	}
	if config.KeepCodespace {
		logger.Warn(fmt.Sprintf("Keeping codespace %s; delete it with \"gh codespace delete -c %s\"", name, name))
	} else {
		defer deleteCodespace(context.WithoutCancel(ctx), name)
	}

	logger.Info("Sending the destination tokens to the codespace...")
	if err := copyToCodespace(ctx, name, tokenFile, remoteTokenFile); err != nil {
		return err
	}

	logger.Info("Running the copy inside the codespace...")
	out, err := execInCodespace(ctx, name, remoteCommand(script))
	if out = strings.TrimSpace(out); out != "" {
		logger.Info(out)
	}
	if err != nil {
		return fmt.Errorf("failed to run the copy inside the codespace: %w", err)
	}

	logger.Info("Codespaces secrets copied successfully")
	return nil
}

// remoteCommand wraps the generated script so it is transferred as a single
// base64 argument and executed by a login shell, which is where Codespaces
// secrets are exported. The script carries no secret values.
func remoteCommand(script string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(script))
	return fmt.Sprintf(
		"umask 077; printf %%s '%s' | base64 -d > %s || exit 1; chmod 600 %s; "+
			"bash -l %s; rc=$?; rm -f %s %s; exit $rc",
		encoded, remoteScriptFile, remoteTokenFile,
		remoteScriptFile, remoteScriptFile, remoteTokenFile,
	)
}

// buildScriptDestinations converts the resolved destinations into the form
// expected by the script generator.
func buildScriptDestinations(destinations []*destination.Destination, tokenEnvNames map[string]string) []migrator.CodespacesCopyDestination {
	result := make([]migrator.CodespacesCopyDestination, 0, len(destinations))
	for _, dest := range destinations {
		result = append(result, migrator.CodespacesCopyDestination{
			Target:   dest.Target,
			Host:     dest.Host,
			TokenEnv: tokenEnvNames[dest.Host],
		})
	}
	return result
}

// writeTokenFile writes the destination tokens to an owner-only local file that
// is copied into the codespace and deleted by the copy script.
func writeTokenFile(destinations []*destination.Destination, tokenEnvNames, hostTokens map[string]string) (string, error) {
	var content strings.Builder
	written := make(map[string]struct{}, len(hostTokens))
	for _, dest := range destinations {
		if _, done := written[dest.Host]; done {
			continue
		}
		written[dest.Host] = struct{}{}
		token := hostTokens[dest.Host]
		if strings.ContainsAny(token, "\n\r'") {
			return "", fmt.Errorf("the token for destination host %s contains characters that cannot be transferred", dest.Host)
		}
		fmt.Fprintf(&content, "%s='%s'\n", tokenEnvNames[dest.Host], token)
	}

	file, err := os.CreateTemp("", "gh-secret-kit-copy-*.env")
	if err != nil {
		return "", fmt.Errorf("failed to create the temporary token file: %w", err)
	}
	path := file.Name()
	if _, err := file.WriteString(content.String()); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("failed to write the temporary token file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("failed to close the temporary token file: %w", err)
	}
	return path, nil
}

// collectSecrets resolves the Codespaces secret names to copy and applies the
// include and exclude filters.
func collectSecrets(ctx context.Context, client *gh.GitHubClient, sourceRepo repository.Repository, config *CopyConfig, scope migrator.SecretScope) ([]string, error) {
	names := config.Secrets
	if len(names) == 0 {
		var secrets []*github.Secret
		var err error
		if scope == migrator.SecretScopeOrg {
			logger.Info("No specific secrets specified, fetching org Codespaces secrets from source...")
			secrets, err = gh.ListCodespacesOrgSecrets(ctx, client, sourceRepo)
		} else {
			logger.Info("No specific secrets specified, fetching repo Codespaces secrets from source...")
			secrets, err = gh.ListCodespacesRepoSecrets(ctx, client, sourceRepo)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to fetch Codespaces secrets from source: %w", err)
		}
		names = secretNames(secrets)

		if config.IncludeUserSecrets {
			logger.Info("Fetching the Codespaces secrets of the authenticated user...")
			userSecrets, err := gh.ListCodespacesUserSecrets(ctx, client)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch the Codespaces secrets of the authenticated user: %w", err)
			}
			names = mergeNames(names, secretNames(userSecrets))
		}
	}
	return migrator.ExcludeSecrets(names, config.ExcludeSecrets), nil
}

// resolveMachine picks the smallest machine type available for the repository,
// which is enough for a copy that only runs the gh CLI.
func resolveMachine(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, ref string) (string, error) {
	machines, err := gh.ListCodespacesRepoMachineTypes(ctx, client, repo, ref)
	if err != nil {
		return "", fmt.Errorf("failed to fetch the machine types available for the source repository: %w", err)
	}
	var selected *github.CodespacesMachine
	for _, machine := range machines {
		if selected == nil || machine.GetCPUs() < selected.GetCPUs() {
			selected = machine
		}
	}
	if selected == nil {
		return "", fmt.Errorf("no machine type is available for the source repository")
	}
	logger.Info(fmt.Sprintf("Using machine type %s (%s)", selected.GetName(), selected.GetDisplayName()))
	return selected.GetName(), nil
}

// secretNames extracts the names of the given secrets.
func secretNames(secrets []*github.Secret) []string {
	names := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		names = append(names, secret.GetName())
	}
	return names
}

// mergeNames appends the names of extra that are not already in base.
func mergeNames(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base))
	for _, name := range base {
		seen[name] = struct{}{}
	}
	for _, name := range extra {
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		base = append(base, name)
	}
	return base
}
