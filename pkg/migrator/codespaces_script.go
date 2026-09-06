package migrator

import (
	"fmt"
	"strings"
)

// CodespacesCopyDestination is a single destination of a Codespaces secret copy.
type CodespacesCopyDestination = EnvCopyDestination

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

// GenerateCodespacesCopyScript generates the shell script that runs inside a
// codespace and copies the Codespaces secrets exposed to it to every
// destination. The values never leave the codespace: the script reads them from
// the environment and writes them with the gh CLI.
func GenerateCodespacesCopyScript(config CodespacesCopyConfig) (string, error) {
	envConfig := envCopyConfig{
		Scope:          config.Scope,
		DestinationApp: config.DestinationApp,
		Secrets:        config.Secrets,
		Rename:         config.Rename,
		Overwrite:      config.Overwrite,
		Destinations:   config.Destinations,
	}
	if err := validateEnvCopyConfig(envConfig); err != nil {
		return "", err
	}
	if config.TokenEnvFile == "" {
		return "", fmt.Errorf("no token file specified")
	}
	if !shellLiteralPattern.MatchString(config.TokenEnvFile) {
		return "", fmt.Errorf("invalid token file path %q", config.TokenEnvFile)
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

	writeEnvCopyBlocks(&script, envConfig)

	return script.String(), nil
}
