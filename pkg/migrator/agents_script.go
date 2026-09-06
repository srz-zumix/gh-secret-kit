package migrator

import (
	"fmt"
	"sort"
	"strings"
)

// AgentsSetupStepsJobName is the job name required by GitHub Copilot for the
// copilot-setup-steps workflow. Any other name is ignored by Copilot.
const AgentsSetupStepsJobName = "copilot-setup-steps"

// AgentsSetupStepsPath is the path, on the default branch, of the workflow that
// GitHub Copilot runs before starting a coding agent session.
const AgentsSetupStepsPath = ".github/workflows/copilot-setup-steps.yml"

// AgentsCopyDoneMarker is printed by the generated script once every secret has
// been processed. It carries no secret value, so it can be looked for in the
// setup steps log to detect that the copy finished.
const AgentsCopyDoneMarker = "gh-secret-kit: agents secret copy finished"

// AgentsCopyStepName is the name of the setup step that runs the copy script.
const AgentsCopyStepName = "Copy Agents secrets"

// AgentsCopyDestination is a single destination of an Agents secret copy.
type AgentsCopyDestination = EnvCopyDestination

// AgentsCopyConfig holds configuration for generating the script that copies
// Agents secrets from inside a Copilot coding agent environment.
type AgentsCopyConfig struct {
	// Scope selects whether the destination secrets are written at the
	// repository or the organization level.
	Scope SecretScope
	// DestinationApp selects the destination secret store. Empty means actions.
	DestinationApp SecretApp
	Secrets        []string
	Rename         map[string]string // OLD_NAME -> NEW_NAME
	Overwrite      bool
	// Destinations reference token environment variables that are themselves
	// Agents secrets, because only Agents secrets are exposed to the agent.
	Destinations []AgentsCopyDestination
}

// GenerateAgentsCopyScript generates the shell script that runs in the Copilot
// coding agent environment and copies the Agents secrets exposed to it to every
// destination. The values never leave that environment: the script reads them
// from the environment and writes them with the gh CLI.
func GenerateAgentsCopyScript(config AgentsCopyConfig) (string, error) {
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

	var script strings.Builder
	script.WriteString("set -euo pipefail\n\n")
	script.WriteString("# The Agents secrets are mapped into the step environment by the generated\n")
	script.WriteString("# workflow, so the values are read directly from the environment.\n")
	writeEnvCopyBlocks(&script, envConfig)
	fmt.Fprintf(&script, "\necho '%s'\n", AgentsCopyDoneMarker)

	return script.String(), nil
}

// agentsCopyStepEnv maps every environment variable the copy script reads to the
// matching Agents secret. GitHub Actions does not expose secrets as environment
// variables on its own, so the mapping has to be explicit.
func agentsCopyStepEnv(config AgentsCopyConfig) map[string]string {
	env := make(map[string]string, len(config.Secrets)+len(config.Destinations))
	names := make([]string, 0, len(config.Secrets)+len(config.Destinations))
	names = append(names, config.Secrets...)
	for _, dest := range config.Destinations {
		names = append(names, dest.TokenEnv)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" {
			continue
		}
		env[name] = fmt.Sprintf("${{ secrets.%s }}", name)
	}
	return env
}

// GenerateAgentsSetupStepsYAML generates the copilot-setup-steps workflow that
// runs the copy script inside the Copilot coding agent environment.
func GenerateAgentsSetupStepsYAML(config AgentsCopyConfig, script string) (string, error) {
	workflow := WorkflowYAML{
		Name: "Copilot Setup Steps",
		// The workflow is only run by Copilot, but GitHub Actions requires a
		// trigger for the file to be a valid workflow. workflow_dispatch is used
		// because it never fires on its own.
		On: map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		Permissions: map[string]string{
			"contents": "read",
		},
		Jobs: map[string]Job{
			AgentsSetupStepsJobName: {
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{
						Name: AgentsCopyStepName,
						Run:  script,
						Env:  agentsCopyStepEnv(config),
					},
				},
			},
		},
	}
	return marshalWorkflow(&workflow)
}
