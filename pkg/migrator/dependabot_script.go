package migrator

import (
	"fmt"
	"regexp"
	"strings"
)

// DependabotCopyDoneMarker is printed after every secret has been processed.
const DependabotCopyDoneMarker = "gh-secret-kit: dependabot secret copy finished"

// DependabotCopyStepName is the workflow step that runs the copy script.
const DependabotCopyStepName = "Copy Dependabot secrets"

// DependabotCopyJobName is the job name of the generated copy workflow.
const DependabotCopyJobName = "copy-dependabot-secrets"

// DependabotActor is the login GitHub uses for runs triggered by Dependabot.
const DependabotActor = "dependabot[bot]"

// DependabotBranchPrefix limits the workflow to branches Dependabot pushes.
const DependabotBranchPrefix = "dependabot/"

// DependabotConfigPath is the configuration Dependabot reads on the default branch.
const DependabotConfigPath = ".github/dependabot.yml"

// DependabotBaitDependency is the action the disabled bait workflow references.
const DependabotBaitDependency = "actions/checkout"

// DependabotBaitVersion ensures Dependabot has an outdated reference to update.
const DependabotBaitVersion = "v1"

// DependabotCopyDestination is a single destination of a Dependabot secret copy.
type DependabotCopyDestination = EnvCopyDestination

// DependabotCopyConfig mirrors envCopyConfig for the shared copy script.
type DependabotCopyConfig struct {
	Scope          SecretScope
	DestinationApp SecretApp
	Secrets        []string
	Rename         map[string]string // OLD_NAME -> NEW_NAME
	Overwrite      bool
	Destinations   []DependabotCopyDestination
}

var dependabotWorkflowNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// ValidateDependabotWorkflowOptions validates the file base name and runner label
// before either value is used in repository files or workflow expressions.
func ValidateDependabotWorkflowOptions(workflowName, runnerLabel string) error {
	if !dependabotWorkflowNamePattern.MatchString(workflowName) {
		return fmt.Errorf("invalid --workflow-name %q: use letters, digits, hyphens or underscores, starting with a letter or digit", workflowName)
	}
	if strings.TrimSpace(runnerLabel) == "" || strings.ContainsAny(runnerLabel, "\r\n") || strings.Contains(runnerLabel, "${{") {
		return fmt.Errorf("invalid --runner-label %q: expected a nonempty literal runner label", runnerLabel)
	}
	return nil
}

// GenerateDependabotCopyScript copies secrets from the runner environment using
// the shared Agents/Codespaces copy implementation.
func GenerateDependabotCopyScript(config DependabotCopyConfig) (string, error) {
	if config.Scope != SecretScopeRepo && config.Scope != SecretScopeOrg {
		return "", fmt.Errorf("unsupported scope %q for Dependabot secrets: expected repo or org", config.Scope)
	}
	envConfig := envCopyConfig(config)
	if err := validateEnvCopyConfig(envConfig); err != nil {
		return "", err
	}

	var script strings.Builder
	script.WriteString("set -euo pipefail\n\n")
	writeEnvCopyBlocks(&script, envConfig)
	fmt.Fprintf(&script, "\necho '%s'\n", DependabotCopyDoneMarker)
	return script.String(), nil
}

// GenerateDependabotCopyWorkflowYAML runs the copy only on Dependabot pushes.
// The unique run name lets the CLI distinguish this invocation's runs from
// earlier or concurrent copies, even when they use the same workflow filename.
func GenerateDependabotCopyWorkflowYAML(config DependabotCopyConfig, script, workflowName, runnerLabel, runName string) (string, error) {
	if err := ValidateDependabotWorkflowOptions(workflowName, runnerLabel); err != nil {
		return "", err
	}
	if strings.TrimSpace(runName) == "" || strings.ContainsAny(runName, "\r\n") || strings.Contains(runName, "${{") {
		return "", fmt.Errorf("invalid copy run name %q", runName)
	}
	if config.Scope != SecretScopeRepo && config.Scope != SecretScopeOrg {
		return "", fmt.Errorf("unsupported scope %q for Dependabot secrets: expected repo or org", config.Scope)
	}
	if err := validateEnvCopyConfig(envCopyConfig(config)); err != nil {
		return "", err
	}

	workflow := WorkflowYAML{
		Name:    workflowName,
		RunName: runName,
		On: map[string]any{
			"push": map[string]any{
				"branches": []string{DependabotBranchPrefix + "**"},
			},
		},
		Permissions: map[string]string{"contents": "read"},
		Jobs: map[string]Job{
			DependabotCopyJobName: {
				If:     fmt.Sprintf("github.actor == '%s'", DependabotActor),
				RunsOn: runnerLabel,
				Steps: []Step{
					{
						Name:  DependabotCopyStepName,
						Shell: "bash",
						Run:   script,
						Env:   envCopySecretEnv(config.Secrets, config.Destinations),
					},
				},
			},
		},
	}
	return marshalWorkflow(&workflow)
}

// GenerateDependabotBaitWorkflowYAML supplies an outdated action reference.
// The job is disabled even when the workflow is dispatched manually.
func GenerateDependabotBaitWorkflowYAML() (string, error) {
	workflow := WorkflowYAML{
		Name: "gh-secret-kit dependabot secret copy trigger",
		On: map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		Permissions: map[string]string{"contents": "read"},
		Jobs: map[string]Job{
			"noop": {
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

// GenerateDependabotConfigYAML restricts updates to checkout. The branch comment
// changes the configuration on each new temporary branch to trigger a check.
func GenerateDependabotConfigYAML(branch string) (string, error) {
	if branch == "" || strings.ContainsAny(branch, "\r\n") {
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
