package migrator

import (
	"fmt"
	"regexp"
	"strings"
)

// CodespacesCopyDestination is a single destination of a Codespaces secret copy.
type CodespacesCopyDestination struct {
	// Target is OWNER/REPO, or ORG when the scope is SecretScopeOrg.
	Target string
	Host   string
	// TokenEnv is the name of the environment variable, defined in the token
	// file, that holds the token for Host.
	TokenEnv string
}

// CodespacesCopyConfig holds configuration for generating the script that copies
// Codespaces secrets from inside a codespace.
type CodespacesCopyConfig struct {
	// Scope selects whether the destination secrets are written at the
	// repository or the organization level.
	Scope SecretScope
	// DestinationApp selects the destination secret store. Empty means actions.
	DestinationApp SecretApp
	Secrets        []string
	Rename         map[string]string // OLD_NAME -> NEW_NAME
	Overwrite      bool
	Destinations   []CodespacesCopyDestination
	// TokenEnvFile is the path, inside the codespace, of the file that defines
	// the destination token environment variables. It is deleted once sourced.
	TokenEnvFile string
}

// secretNamePattern matches the names GitHub allows for secrets. The generated
// script dereferences secret names as shell variables, so anything else is
// rejected before it can reach the script.
var secretNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// shellLiteralPattern matches values that are embedded into the script as
// single-quoted literals. Restricting the character set keeps the generated
// script free of shell metacharacters even for hand-written arguments.
var shellLiteralPattern = regexp.MustCompile(`^[A-Za-z0-9._/-]+$`)

// GenerateCodespacesCopyScript generates the shell script that runs inside a
// codespace and copies the Codespaces secrets exposed to it to every
// destination. The values never leave the codespace: the script reads them from
// the environment and writes them with the gh CLI.
func GenerateCodespacesCopyScript(config CodespacesCopyConfig) (string, error) {
	if len(config.Destinations) == 0 {
		return "", fmt.Errorf("no destination specified")
	}
	if len(config.Secrets) == 0 {
		return "", fmt.Errorf("no secret specified")
	}
	if config.TokenEnvFile == "" {
		return "", fmt.Errorf("no token file specified")
	}
	if !shellLiteralPattern.MatchString(config.TokenEnvFile) {
		return "", fmt.Errorf("invalid token file path %q", config.TokenEnvFile)
	}
	if err := ValidateSecretApp(config.DestinationApp); err != nil {
		return "", err
	}
	if err := validateSecretNames(config.Secrets, config.Rename); err != nil {
		return "", err
	}
	for _, dest := range config.Destinations {
		if !shellLiteralPattern.MatchString(dest.Target) {
			return "", fmt.Errorf("invalid destination %q", dest.Target)
		}
		if !shellLiteralPattern.MatchString(dest.Host) {
			return "", fmt.Errorf("invalid destination host %q", dest.Host)
		}
		if !secretNamePattern.MatchString(dest.TokenEnv) {
			return "", fmt.Errorf("invalid token environment variable name %q", dest.TokenEnv)
		}
	}

	var script strings.Builder
	script.WriteString("#!/usr/bin/env bash\n")
	script.WriteString("set -euo pipefail\n\n")

	script.WriteString("# Codespaces secrets are exported into login shell sessions only, so load the\n")
	script.WriteString("# codespace environment explicitly in case this script runs without one.\n")
	script.WriteString("if [ -f /workspaces/.codespaces/shared/.env ]; then\n")
	script.WriteString("  set -a\n")
	script.WriteString("  . /workspaces/.codespaces/shared/.env\n")
	script.WriteString("  set +a\n")
	script.WriteString("fi\n\n")

	fmt.Fprintf(&script, "_token_file='%s'\n", config.TokenEnvFile)
	script.WriteString("if [ ! -f \"${_token_file}\" ]; then\n")
	script.WriteString("  echo \"destination token file not found: ${_token_file}\" >&2\n")
	script.WriteString("  exit 1\n")
	script.WriteString("fi\n")
	script.WriteString("set -a\n")
	script.WriteString(". \"${_token_file}\"\n")
	script.WriteString("set +a\n")
	script.WriteString("rm -f \"${_token_file}\"\n\n")

	// The codespace ships its own GITHUB_TOKEN, which would otherwise take
	// precedence over the destination token for github.com destinations.
	script.WriteString("unset GITHUB_TOKEN GH_TOKEN GH_ENTERPRISE_TOKEN GH_HOST GH_REPO\n")

	for _, dest := range config.Destinations {
		script.WriteString("\n")
		fmt.Fprintf(&script, "echo \"Copying secrets to %s (%s)\"\n", dest.Target, dest.Host)
		fmt.Fprintf(&script, "export GH_HOST='%s'\n", dest.Host)
		if dest.Host == "github.com" {
			fmt.Fprintf(&script, "export GH_TOKEN=\"${%s}\"\n", dest.TokenEnv)
			script.WriteString("unset GH_ENTERPRISE_TOKEN\n")
		} else {
			fmt.Fprintf(&script, "export GH_ENTERPRISE_TOKEN=\"${%s}\"\n", dest.TokenEnv)
			script.WriteString("unset GH_TOKEN\n")
		}
		fmt.Fprintf(&script, "export DESTINATION='%s'\n", dest.Target)

		for _, secretName := range config.Secrets {
			destSecretName := secretName
			if newName, ok := config.Rename[secretName]; ok {
				destSecretName = newName
			}
			script.WriteString("\n")
			// A missing secret must not abort the script under "set -u".
			fmt.Fprintf(&script, "SECRET_VALUE=\"${%s-}\"\n", secretName)
			// The shared per-secret script uses "exit" to skip a secret, so run
			// it in a subshell to keep the remaining secrets going.
			script.WriteString("(\n")
			script.WriteString(generateSecretMigrationScript(secretScriptConfig{
				Scope:          config.Scope,
				DestinationApp: config.DestinationApp,
				Overwrite:      config.Overwrite,
			}, secretName, destSecretName))
			script.WriteString(")\n")
		}
	}

	return script.String(), nil
}
