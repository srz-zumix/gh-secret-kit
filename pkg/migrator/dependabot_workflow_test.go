package migrator

import (
	"strings"
	"testing"
)

func TestGenerateDependabotCopyWorkflowYAML(t *testing.T) {
	config := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-dependabot-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeRepo,
		Secrets:      []string{"FOO", "BAR"},
		Rename:       map[string]string{"BAR": "BAZ"},
		Destinations: []CopyDestination{
			{Target: "owner/dest", Host: "github.com", TokenSecret: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	out, err := GenerateDependabotCopyWorkflowYAML(config, "gh-secret-kit-copy-1")
	if err != nil {
		t.Fatalf("GenerateDependabotCopyWorkflowYAML returned error: %v", err)
	}

	// The run must be triggered by the Dependabot pull request, not manually.
	if !strings.Contains(out, "pull_request:") {
		t.Errorf("expected a pull_request trigger, got:\n%s", out)
	}
	if strings.Contains(out, "workflow_dispatch") {
		t.Errorf("dependabot copy workflow should not contain a workflow_dispatch trigger, got:\n%s", out)
	}
	// The trigger is restricted to the temporary base branch.
	if !strings.Contains(out, "- gh-secret-kit-copy-1") {
		t.Errorf("expected the base branch filter, got:\n%s", out)
	}
	// Only Dependabot-triggered runs receive Dependabot secrets.
	if !strings.Contains(out, "if: github.actor == 'dependabot[bot]'") {
		t.Errorf("expected the dependabot actor gate, got:\n%s", out)
	}
	// Cleanup is done by the CLI, so the workflow must not request write access.
	if !strings.Contains(out, "contents: read") {
		t.Errorf("expected contents: read permission, got:\n%s", out)
	}

	for _, want := range []string{
		"Copy secret to owner/dest: FOO",
		"Copy secret to owner/dest: BAR",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected step %q, got:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "SECRET_NAME: BAZ") {
		t.Errorf("expected renamed secret name, got:\n%s", out)
	}
	if !strings.Contains(out, "GH_TOKEN: ${{ secrets.COPY_TOKEN_GITHUB_COM }}") {
		t.Errorf("expected GH_TOKEN for the github.com destination, got:\n%s", out)
	}
}

func TestGenerateDependabotCopyWorkflowYAMLDestinationApp(t *testing.T) {
	config := CopyWorkflowConfig{
		WorkflowName:   "gh-secret-kit-dependabot-copy",
		RunsOn:         "ubuntu-latest",
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppDependabot,
		Secrets:        []string{"FOO"},
		Destinations: []CopyDestination{
			{Target: "owner/dest", Host: "github.com", TokenSecret: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	out, err := GenerateDependabotCopyWorkflowYAML(config, "gh-secret-kit-copy-1")
	if err != nil {
		t.Fatalf("GenerateDependabotCopyWorkflowYAML returned error: %v", err)
	}

	if !strings.Contains(out, "gh secret set FOO -R \"${DESTINATION}\" --app dependabot") {
		t.Errorf("expected a dependabot-scoped gh secret set, got:\n%s", out)
	}
}

func TestGenerateDependabotCopyWorkflowYAMLOrgScope(t *testing.T) {
	config := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-dependabot-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeOrg,
		Secrets:      []string{"FOO"},
		Destinations: []CopyDestination{
			{Target: "dest-org", Host: "github.com", TokenSecret: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	out, err := GenerateDependabotCopyWorkflowYAML(config, "gh-secret-kit-copy-1")
	if err != nil {
		t.Fatalf("GenerateDependabotCopyWorkflowYAML returned error: %v", err)
	}

	if !strings.Contains(out, "gh secret set FOO --org \"${DESTINATION}\"") {
		t.Errorf("expected an org-scoped gh secret set, got:\n%s", out)
	}
}

func TestGenerateDependabotCopyWorkflowYAMLValidation(t *testing.T) {
	valid := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-dependabot-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeRepo,
		Secrets:      []string{"FOO"},
		Destinations: []CopyDestination{
			{Target: "owner/dest", Host: "github.com", TokenSecret: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	if _, err := GenerateDependabotCopyWorkflowYAML(valid, ""); err == nil {
		t.Error("expected an error for an empty base branch")
	}

	noSecrets := valid
	noSecrets.Secrets = nil
	if _, err := GenerateDependabotCopyWorkflowYAML(noSecrets, "gh-secret-kit-copy-1"); err == nil {
		t.Error("expected an error when no secret is specified")
	}

	noDestinations := valid
	noDestinations.Destinations = nil
	if _, err := GenerateDependabotCopyWorkflowYAML(noDestinations, "gh-secret-kit-copy-1"); err == nil {
		t.Error("expected an error when no destination is specified")
	}

	// Dependabot has no environment-scoped secrets.
	envScope := valid
	envScope.Scope = SecretScopeEnv
	if _, err := GenerateDependabotCopyWorkflowYAML(envScope, "gh-secret-kit-copy-1"); err == nil {
		t.Error("expected an error for the env scope")
	}
}

func TestGenerateDependabotUpdateTargetYAML(t *testing.T) {
	out, err := GenerateDependabotUpdateTargetYAML("gh-secret-kit-dependabot-copy-update-target")
	if err != nil {
		t.Fatalf("GenerateDependabotUpdateTargetYAML returned error: %v", err)
	}

	// The outdated action reference is what Dependabot updates.
	if !strings.Contains(out, "uses: "+dependabotUpdateTargetAction) {
		t.Errorf("expected the outdated action reference, got:\n%s", out)
	}
	// The workflow must never run: manual trigger only and a disabled job.
	if !strings.Contains(out, "workflow_dispatch:") {
		t.Errorf("expected a workflow_dispatch trigger, got:\n%s", out)
	}
	if !strings.Contains(out, "if: \"false\"") {
		t.Errorf("expected the job to be disabled, got:\n%s", out)
	}
}

func TestGenerateDependabotConfigYAML(t *testing.T) {
	out := GenerateDependabotConfigYAML()

	if !strings.Contains(out, "package-ecosystem: github-actions") {
		t.Errorf("expected the github-actions ecosystem, got:\n%s", out)
	}
	// A single pull request is enough to trigger the copy workflow.
	if !strings.Contains(out, "open-pull-requests-limit: 1") {
		t.Errorf("expected the pull request limit, got:\n%s", out)
	}
}
