package migrator

import (
	"strings"
	"testing"
)

func TestGenerateCopyWorkflowYAML(t *testing.T) {
	config := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeRepo,
		Secrets:      []string{"FOO", "BAR"},
		Rename:       map[string]string{"BAR": "BAZ"},
		Destinations: []CopyDestination{
			{Target: "owner/dest", Host: "github.com", TokenSecret: "COPY_TOKEN_GITHUB_COM"},
			{Target: "owner/ghes-dest", Host: "github.example.com", TokenSecret: "COPY_TOKEN_GITHUB_EXAMPLE_COM"},
		},
	}

	out, err := GenerateCopyWorkflowYAML(config)
	if err != nil {
		t.Fatalf("GenerateCopyWorkflowYAML returned error: %v", err)
	}

	if !strings.Contains(out, "workflow_dispatch:") {
		t.Errorf("expected workflow_dispatch trigger, got:\n%s", out)
	}
	if strings.Contains(out, "pull_request") {
		t.Errorf("copy workflow should not contain a pull_request trigger, got:\n%s", out)
	}
	// Cleanup is done by the CLI, so the workflow must not request write access.
	if !strings.Contains(out, "contents: read") {
		t.Errorf("expected contents: read permission, got:\n%s", out)
	}
	if strings.Contains(out, "contents: write") {
		t.Errorf("copy workflow should not request contents: write, got:\n%s", out)
	}
	if strings.Contains(out, "Cleanup dispatch branch") {
		t.Errorf("copy workflow should not contain a cleanup step, got:\n%s", out)
	}

	// One step per destination and secret.
	for _, want := range []string{
		"Copy secret to owner/dest: FOO",
		"Copy secret to owner/dest: BAR",
		"Copy secret to owner/ghes-dest: FOO",
		"Copy secret to owner/ghes-dest: BAR",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected step %q, got:\n%s", want, out)
		}
	}

	if !strings.Contains(out, "SECRET_NAME: BAZ") {
		t.Errorf("expected renamed secret name, got:\n%s", out)
	}
	// The host is per step because destinations may live on different hosts.
	if !strings.Contains(out, "GH_HOST: github.com") || !strings.Contains(out, "GH_HOST: github.example.com") {
		t.Errorf("expected per-step GH_HOST for each destination, got:\n%s", out)
	}
	if !strings.Contains(out, "GH_TOKEN: ${{ secrets.COPY_TOKEN_GITHUB_COM }}") {
		t.Errorf("expected GH_TOKEN for the github.com destination, got:\n%s", out)
	}
	if !strings.Contains(out, "GH_ENTERPRISE_TOKEN: ${{ secrets.COPY_TOKEN_GITHUB_EXAMPLE_COM }}") {
		t.Errorf("expected GH_ENTERPRISE_TOKEN for the GHES destination, got:\n%s", out)
	}
	// Without --overwrite the script must skip secrets that already exist.
	if !strings.Contains(out, "already exists at destination, skipping") {
		t.Errorf("expected the existence check, got:\n%s", out)
	}
}

func TestGenerateCopyWorkflowYAMLEnvScope(t *testing.T) {
	config := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeEnv,
		SourceEnv:    "production",
		Secrets:      []string{"FOO"},
		Overwrite:    true,
		Destinations: []CopyDestination{
			{Target: "owner/dest", Host: "github.com", Env: "staging", TokenSecret: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	out, err := GenerateCopyWorkflowYAML(config)
	if err != nil {
		t.Fatalf("GenerateCopyWorkflowYAML returned error: %v", err)
	}

	// The job must be bound to the source environment to read its secrets.
	if !strings.Contains(out, "environment: production") {
		t.Errorf("expected the job to be bound to the source environment, got:\n%s", out)
	}
	if !strings.Contains(out, "DEST_ENV: staging") {
		t.Errorf("expected the destination environment, got:\n%s", out)
	}
	if !strings.Contains(out, "gh secret set FOO --env \"${DEST_ENV}\"") {
		t.Errorf("expected an env-scoped gh secret set, got:\n%s", out)
	}
	if strings.Contains(out, "already exists at destination, skipping") {
		t.Errorf("--overwrite should skip the existence check, got:\n%s", out)
	}
}

func TestGenerateCopyWorkflowYAMLOrgScope(t *testing.T) {
	config := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeOrg,
		Secrets:      []string{"FOO"},
		Destinations: []CopyDestination{
			{Target: "dest-org", Host: "github.com", TokenSecret: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	out, err := GenerateCopyWorkflowYAML(config)
	if err != nil {
		t.Fatalf("GenerateCopyWorkflowYAML returned error: %v", err)
	}

	if !strings.Contains(out, "gh secret set FOO --org \"${DESTINATION}\"") {
		t.Errorf("expected an org-scoped gh secret set, got:\n%s", out)
	}
}

func TestGenerateCopyWorkflowYAMLDestinationApp(t *testing.T) {
	config := CopyWorkflowConfig{
		WorkflowName:   "gh-secret-kit-copy",
		RunsOn:         "ubuntu-latest",
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppAgents,
		Secrets:        []string{"FOO"},
		Destinations: []CopyDestination{
			{Target: "owner/dest", Host: "github.com", TokenSecret: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	out, err := GenerateCopyWorkflowYAML(config)
	if err != nil {
		t.Fatalf("GenerateCopyWorkflowYAML returned error: %v", err)
	}

	if !strings.Contains(out, "gh secret set FOO -R \"${DESTINATION}\" --app agents") {
		t.Errorf("expected an agents-scoped gh secret set, got:\n%s", out)
	}
	if !strings.Contains(out, "gh secret list -R \"${DESTINATION}\" --app agents") {
		t.Errorf("expected the existence check to use the agents app, got:\n%s", out)
	}
	// The source is always read as an Actions secret.
	if !strings.Contains(out, "SECRET_VALUE: ${{ secrets.FOO }}") {
		t.Errorf("expected the source to be read from Actions secrets, got:\n%s", out)
	}
}

func TestGenerateCopyWorkflowYAMLValidation(t *testing.T) {
	if _, err := GenerateCopyWorkflowYAML(CopyWorkflowConfig{Secrets: []string{"FOO"}}); err == nil {
		t.Error("expected an error when no destination is specified")
	}
	if _, err := GenerateCopyWorkflowYAML(CopyWorkflowConfig{
		Destinations: []CopyDestination{{Target: "owner/dest"}},
	}); err == nil {
		t.Error("expected an error when no secret is specified")
	}
}
