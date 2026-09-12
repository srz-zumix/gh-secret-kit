package migrator

import (
	"fmt"
	"strings"
)

// DependabotCopyDoneMarker is printed by the generated script once every secret
// has been processed. It carries no secret value, so it can be looked for in the
// workflow job log to detect that the copy finished.
const DependabotCopyDoneMarker = "gh-secret-kit: dependabot secret copy finished"

// DependabotCopyStepName is the name of the workflow step that runs the copy
// script.
const DependabotCopyStepName = "Copy Dependabot secrets"

// DependabotCopyJobName is the job name of the generated copy workflow.
const DependabotCopyJobName = "copy-dependabot-secrets"

// DependabotActor is the login GitHub uses for the runs Dependabot triggers.
// Only those runs are given the Dependabot secrets.
const DependabotActor = "dependabot[bot]"

// DependabotBranchPrefix is the prefix of the branches Dependabot pushes. The
// generated workflow is limited to them so that no other push can run it.
const DependabotBranchPrefix = "dependabot/"

// DependabotConfigPath is the path of the Dependabot configuration. Committing
// it to the default branch makes Dependabot check for updates immediately,
// which is what starts the copy.
const DependabotConfigPath = ".github/dependabot.yml"

// DependabotBaitDependency is the action the generated bait workflow pins to an
// outdated version, so the Dependabot check always finds an update to open a
// pull request for. The github-actions ecosystem is used because it needs no
// external package registry.
const DependabotBaitDependency = "actions/checkout"

// DependabotBaitVersion is the outdated version the bait workflow pins.
const DependabotBaitVersion = "v1"

// DependabotCopyDestination is a single destination of a Dependabot secret copy.
type DependabotCopyDestination = EnvCopyDestination

// DependabotCopyConfig holds configuration for generating the workflow that
// copies Dependabot secrets from a Dependabot-triggered workflow run.
//
// The field list matches envCopyConfig so it can be converted to it.
type DependabotCopyConfig struct {
	// Scope selects whether the destination secrets are written at the
	// repository or the organization level.
	Scope SecretScope
	// DestinationApp selects the destination secret store. Empty means actions.
	DestinationApp SecretApp
	Secrets        []string
	Rename         map[string]string // OLD_NAME -> NEW_NAME
	Overwrite      bool
	// Destinations reference token environment variables that are themselves
	// Dependabot secrets, because a Dependabot-triggered run is given no other
	// secrets.
	Destinations []DependabotCopyDestination
}

// GenerateDependabotCopyScript generates the shell script that runs in a
// Dependabot-triggered workflow run and copies the Dependabot secrets exposed to
// it to every destination. The values never leave the runner: the script reads
// them from the environment and writes them with the gh CLI.
func GenerateDependabotCopyScript(config DependabotCopyConfig) (string, error) {
	envConfig := envCopyConfig(config)
	if err := validateEnvCopyConfig(envConfig); err != nil {
		return "", err
	}

	var script strings.Builder
	script.WriteString("set -euo pipefail\n\n")
	script.WriteString("# The Dependabot secrets are mapped into the step environment by the generated\n")
	script.WriteString("# workflow, so the values are read directly from the environment.\n")
	writeEnvCopyBlocks(&script, envConfig)
	fmt.Fprintf(&script, "\necho '%s'\n", DependabotCopyDoneMarker)

	return script.String(), nil
}

// GenerateDependabotCopyWorkflowYAML generates the workflow that runs the copy
// script. Only a push made by Dependabot is given the Dependabot secrets, so the
// workflow is limited to the branches Dependabot pushes and to runs it started.
func GenerateDependabotCopyWorkflowYAML(config DependabotCopyConfig, script string) (string, error) {
	if err := validateEnvCopyConfig(envCopyConfig(config)); err != nil {
		return "", err
	}

	workflow := WorkflowYAML{
		Name: "gh-secret-kit dependabot secret copy",
		On: map[string]any{
			"push": map[string]any{
				"branches": []string{DependabotBranchPrefix + "**"},
			},
		},
		// A Dependabot-triggered run only gets a read-only GITHUB_TOKEN anyway.
		// The destination is written with the token secret instead.
		Permissions: map[string]string{"contents": "read"},
		Jobs: map[string]Job{
			DependabotCopyJobName: {
				If:     fmt.Sprintf("github.actor == '%s'", DependabotActor),
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{
						Name: DependabotCopyStepName,
						Run:  script,
						Env:  envCopySecretEnv(config.Secrets, config.Destinations),
					},
				},
			},
		},
	}
	return marshalWorkflow(&workflow)
}

// GenerateDependabotBaitWorkflowYAML generates a workflow that is never run but
// pins an outdated action, so the Dependabot check has an update to open a pull
// request for. Pushing that pull request branch is what runs the copy workflow.
func GenerateDependabotBaitWorkflowYAML() (string, error) {
	workflow := WorkflowYAML{
		Name: "gh-secret-kit dependabot secret copy trigger",
		// workflow_dispatch never fires on its own, so this workflow only exists
		// to hold the outdated action reference.
		On: map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		Permissions: map[string]string{"contents": "read"},
		Jobs: map[string]Job{
			"noop": {
				// The job is never meant to run; "if: false" keeps it from doing
				// anything even if the workflow is dispatched by hand.
				If:     "false",
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{
						Name: "Outdated action reference kept for the Dependabot check",
						Uses: DependabotBaitDependency + "@" + DependabotBaitVersion,
					},
				},
			},
		},
	}
	return marshalWorkflow(&workflow)
}

// GenerateDependabotConfigYAML generates the Dependabot configuration that makes
// the check open a single pull request for the outdated action of the bait
// workflow. The branch name is included as a comment so that committing the file
// always changes its content, which is what triggers the check.
func GenerateDependabotConfigYAML(branch string) (string, error) {
	if strings.ContainsAny(branch, "\r\n") {
		return "", fmt.Errorf("invalid branch name %q", branch)
	}
	const tmpl = `# Temporary configuration written by gh-secret-kit for %s.
# It is removed together with the temporary branch once the copy finishes.
version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    open-pull-requests-limit: 1
    allow:
      - dependency-name: %s
`
	return fmt.Sprintf(tmpl, branch, DependabotBaitDependency), nil
}
