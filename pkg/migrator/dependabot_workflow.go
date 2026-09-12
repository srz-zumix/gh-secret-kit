package migrator

import (
	"fmt"
)

// DependabotActor is the GitHub Actions actor of workflow runs triggered by
// Dependabot. Only such runs receive Dependabot secrets in the secrets context.
const DependabotActor = "dependabot[bot]"

// DependabotConfigPath is the path of the Dependabot configuration file.
// Dependabot only reads it from the default branch.
const DependabotConfigPath = ".github/dependabot.yml"

// DependabotUpdateTargetSuffix is appended to the copy workflow name to build
// the name of the workflow that carries the outdated action reference.
const DependabotUpdateTargetSuffix = "-update-target"

// dependabotUpdateTargetAction is an intentionally outdated action reference,
// so that Dependabot always finds at least one github-actions update and opens
// the pull request that triggers the copy workflow. The workflow that carries
// it never runs.
const dependabotUpdateTargetAction = "actions/checkout@v2"

// GenerateDependabotCopyWorkflowYAML generates a pull_request-triggered GitHub
// Actions workflow that copies every configured Dependabot secret to every
// destination. Workflow runs triggered by Dependabot receive Dependabot secrets
// instead of Actions secrets in the secrets context, so the job is gated on the
// dependabot[bot] actor and the trigger is restricted to pull requests that
// target the temporary base branch.
func GenerateDependabotCopyWorkflowYAML(config CopyWorkflowConfig, baseBranch string) (string, error) {
	if err := validateCopyWorkflowConfig(config); err != nil {
		return "", err
	}
	// Dependabot has no environment-scoped secrets.
	if config.Scope == SecretScopeEnv || config.SourceEnv != "" {
		return "", fmt.Errorf("unsupported scope %q for Dependabot secrets: expected repo or org", SecretScopeEnv)
	}
	if baseBranch == "" {
		return "", fmt.Errorf("no base branch specified")
	}

	workflow := WorkflowYAML{
		Name: config.WorkflowName,
		On: map[string]any{
			"pull_request": map[string]any{
				"branches": []string{baseBranch},
			},
		},
		Permissions: map[string]string{"contents": "read"},
		Jobs: map[string]Job{
			"copy-secrets": {
				// Only Dependabot-triggered runs receive Dependabot secrets.
				If:     fmt.Sprintf("github.actor == '%s'", DependabotActor),
				RunsOn: config.RunsOn,
				Steps:  buildCopySteps(config),
			},
		},
	}
	return marshalWorkflow(&workflow)
}

// GenerateDependabotUpdateTargetYAML generates a workflow that references an
// intentionally outdated action release, guaranteeing that Dependabot finds a
// github-actions update on the temporary branch. The workflow itself never
// runs: it only has a manual trigger and its single job is disabled.
func GenerateDependabotUpdateTargetYAML(workflowName string) (string, error) {
	workflow := WorkflowYAML{
		Name: workflowName,
		// workflow_dispatch never fires on its own, but GitHub Actions requires
		// a trigger for the file to be a valid workflow.
		On: map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		Permissions: map[string]string{"contents": "read"},
		Jobs: map[string]Job{
			"update-target": {
				If:     "false",
				RunsOn: "ubuntu-latest",
				Steps: []Step{
					{
						Name: "Outdated action for Dependabot to update",
						Uses: dependabotUpdateTargetAction,
					},
				},
			},
		},
	}
	return marshalWorkflow(&workflow)
}

// GenerateDependabotConfigYAML generates the Dependabot configuration that
// makes Dependabot scan the workflow files of the branch it lives on.
// Committing it to the default branch triggers an immediate update check, and
// the pull request the check opens triggers the copy workflow.
func GenerateDependabotConfigYAML() string {
	return `version: 2
updates:
  - package-ecosystem: github-actions
    directory: /
    schedule:
      interval: daily
    open-pull-requests-limit: 1
`
}
