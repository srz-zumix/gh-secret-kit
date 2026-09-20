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
	out, err := GenerateCopyWorkflowYAML(CopyWorkflowConfig{
		Secrets:        []string{"FOO"},
		Destinations:   []CopyDestination{{Target: "owner/dest"}},
		DestinationApp: SecretApp("agents; echo injected"),
	})
	if err == nil {
		t.Error("expected an error for an unsupported destination app")
	}
	if strings.Contains(out, "injected") {
		t.Errorf("unsupported app value must not reach the generated workflow, got:\n%s", out)
	}

	// An unsafe secret name must be rejected before any YAML is generated.
	out, err = GenerateCopyWorkflowYAML(CopyWorkflowConfig{
		Secrets:      []string{"FOO; rm -rf /"},
		Destinations: []CopyDestination{{Target: "owner/dest", Host: "github.com", TokenSecret: "COPY_TOKEN"}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid secret name") {
		t.Errorf("expected an invalid secret name error, got: %v", err)
	}
	if out != "" {
		t.Errorf("unsafe secret name must not reach the generated workflow, got:\n%s", out)
	}

	// An unsafe renamed destination name must be rejected as well.
	out, err = GenerateCopyWorkflowYAML(CopyWorkflowConfig{
		Secrets:      []string{"FOO"},
		Rename:       map[string]string{"FOO": "BAR$(id)"},
		Destinations: []CopyDestination{{Target: "owner/dest", Host: "github.com", TokenSecret: "COPY_TOKEN"}},
	})
	if err == nil || !strings.Contains(err.Error(), "invalid secret name") {
		t.Errorf("expected an invalid secret name error, got: %v", err)
	}
	if out != "" {
		t.Errorf("unsafe renamed name must not reach the generated workflow, got:\n%s", out)
	}
}

func TestGenerateCopyWorkflowYAMLOrgAccessSkip(t *testing.T) {
	// A secret whose selected repository access could not be reproduced is
	// marked Skip, and must never be copied with a broadening "private"
	// default, so no step is generated for it.
	config := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeOrg,
		Secrets:      []string{"KEEP", "DROP"},
		Destinations: []CopyDestination{
			{
				Target:      "dest-org",
				Host:        "github.com",
				TokenSecret: "COPY_TOKEN_GITHUB_COM",
				OrgAccess: map[string]OrgSecretAccess{
					"KEEP": {Visibility: "all"},
					"DROP": {Skip: true},
				},
			},
		},
	}

	out, err := GenerateCopyWorkflowYAML(config)
	if err != nil {
		t.Fatalf("GenerateCopyWorkflowYAML returned error: %v", err)
	}
	if !strings.Contains(out, "gh secret set KEEP --org \"${DESTINATION}\" --visibility all") {
		t.Errorf("expected the non-skipped secret to be copied with its visibility, got:\n%s", out)
	}
	if strings.Contains(out, "gh secret set DROP") {
		t.Errorf("a skipped secret must not be copied at all, got:\n%s", out)
	}
}

func TestGenerateCopyWorkflowYAMLAllSkipped(t *testing.T) {
	// When every secret/destination pair is skipped, generation must fail
	// rather than emit a job with no steps that looks successful.
	config := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeOrg,
		Secrets:      []string{"DROP"},
		Destinations: []CopyDestination{
			{
				Target:      "dest-org",
				Host:        "github.com",
				TokenSecret: "COPY_TOKEN_GITHUB_COM",
				OrgAccess:   map[string]OrgSecretAccess{"DROP": {Skip: true}},
			},
		},
	}

	if _, err := GenerateCopyWorkflowYAML(config); err == nil {
		t.Error("expected an error when every secret is skipped")
	}
}

func TestValidateOrgSecretAccessSkip(t *testing.T) {
	// A skipped access is always valid; it is never turned into a command.
	if err := ValidateOrgSecretAccess(OrgSecretAccess{Skip: true}); err != nil {
		t.Errorf("expected a skipped access to be valid, got: %v", err)
	}
}

func TestValidateOrgSecretAccessSelectedRequiresRepos(t *testing.T) {
	// gh secret set rejects "--visibility selected" without "--repos", so a
	// selected access with no repositories must be rejected before rendering.
	if err := ValidateOrgSecretAccess(OrgSecretAccess{Visibility: "selected"}); err == nil {
		t.Error("expected an error for selected visibility with no repositories")
	}
	// A selected access with at least one repository stays valid.
	if err := ValidateOrgSecretAccess(OrgSecretAccess{Visibility: "selected", Repos: []string{"repo-a"}}); err != nil {
		t.Errorf("expected a selected access with repositories to be valid, got: %v", err)
	}
}

func TestGenerateCopyWorkflowYAMLSelectedWithoutReposRejected(t *testing.T) {
	// The generator must not emit "--visibility selected" without "--repos",
	// which gh secret set rejects. Such input is rejected up front.
	config := CopyWorkflowConfig{
		WorkflowName: "gh-secret-kit-copy",
		RunsOn:       "ubuntu-latest",
		Scope:        SecretScopeOrg,
		Secrets:      []string{"FOO"},
		Destinations: []CopyDestination{
			{
				Target:      "dest-org",
				Host:        "github.com",
				TokenSecret: "COPY_TOKEN_GITHUB_COM",
				OrgAccess:   map[string]OrgSecretAccess{"FOO": {Visibility: "selected"}},
			},
		},
	}
	if _, err := GenerateCopyWorkflowYAML(config); err == nil {
		t.Error("expected an error for selected visibility without repositories")
	}
}
