package migrator

import (
	"strings"
	"testing"
)

func TestGenerateDependabotCopyScript(t *testing.T) {
	script, err := GenerateDependabotCopyScript(DependabotCopyConfig{
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppDependabot,
		Secrets:        []string{"FOO", "BAR"},
		Rename:         map[string]string{"BAR": "BAZ"},
		Destinations: []DependabotCopyDestination{
			{Target: "dst-owner/dst-repo", Host: "github.com", TokenEnv: "COPY_TOKEN_GITHUB_COM"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateDependabotCopyScript() error = %v", err)
	}

	for _, expected := range []string{
		"unset GITHUB_TOKEN GH_TOKEN GH_ENTERPRISE_TOKEN GH_HOST GH_REPO",
		"export GH_HOST='github.com'",
		"export GH_TOKEN=\"${COPY_TOKEN_GITHUB_COM}\"",
		"SECRET_VALUE=\"${FOO-}\"",
		"SECRET_VALUE=\"${BAR-}\"",
		"--app dependabot",
		DependabotCopyDoneMarker,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("GenerateDependabotCopyScript() missing %q:\n%s", expected, script)
		}
	}
	if !strings.Contains(script, "BAZ") {
		t.Errorf("GenerateDependabotCopyScript() did not apply the rename mapping:\n%s", script)
	}
}

func TestGenerateDependabotCopyScriptValidation(t *testing.T) {
	base := DependabotCopyConfig{
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppDependabot,
		Secrets:        []string{"FOO"},
		Destinations: []DependabotCopyDestination{
			{Target: "dst-owner/dst-repo", Host: "github.com", TokenEnv: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	tests := []struct {
		name   string
		modify func(*DependabotCopyConfig)
	}{
		{"no destination", func(c *DependabotCopyConfig) { c.Destinations = nil }},
		{"no secret", func(c *DependabotCopyConfig) { c.Secrets = nil }},
		{"invalid app", func(c *DependabotCopyConfig) { c.DestinationApp = SecretApp("bogus") }},
		{"reserved secret name", func(c *DependabotCopyConfig) { c.Secrets = []string{"GH_TOKEN"} }},
		{"secret collides with token variable", func(c *DependabotCopyConfig) {
			c.Secrets = []string{"COPY_TOKEN_GITHUB_COM"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := base
			tt.modify(&config)
			if _, err := GenerateDependabotCopyScript(config); err == nil {
				t.Errorf("GenerateDependabotCopyScript() expected an error, got nil")
			}
		})
	}
}

func TestGenerateDependabotCopyWorkflowYAML(t *testing.T) {
	config := DependabotCopyConfig{
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppDependabot,
		Secrets:        []string{"SECRET_A"},
		Destinations: []DependabotCopyDestination{
			{Target: "owner/repo", Host: "github.com", TokenEnv: "DST_TOKEN"},
		},
	}
	yaml, err := GenerateDependabotCopyWorkflowYAML(config, "echo hello\n")
	if err != nil {
		t.Fatalf("GenerateDependabotCopyWorkflowYAML() error = %v", err)
	}
	for _, expected := range []string{
		"on:",
		"push:",
		"- dependabot/**",
		DependabotCopyJobName + ":",
		"if: github.actor == 'dependabot[bot]'",
		"runs-on: ubuntu-latest",
		"echo hello",
		"SECRET_A: ${{ secrets.SECRET_A }}",
		"DST_TOKEN: ${{ secrets.DST_TOKEN }}",
	} {
		if !strings.Contains(yaml, expected) {
			t.Errorf("GenerateDependabotCopyWorkflowYAML() missing %q:\n%s", expected, yaml)
		}
	}
	if strings.Contains(yaml, "\"on\":") {
		t.Errorf("GenerateDependabotCopyWorkflowYAML() left the quoted \"on\" key:\n%s", yaml)
	}
}

func TestGenerateDependabotBaitWorkflowYAML(t *testing.T) {
	yaml, err := GenerateDependabotBaitWorkflowYAML()
	if err != nil {
		t.Fatalf("GenerateDependabotBaitWorkflowYAML() error = %v", err)
	}
	for _, expected := range []string{
		"on:",
		"workflow_dispatch: {}",
		"uses: actions/checkout@v1",
	} {
		if !strings.Contains(yaml, expected) {
			t.Errorf("GenerateDependabotBaitWorkflowYAML() missing %q:\n%s", expected, yaml)
		}
	}
	if strings.Contains(yaml, "\"on\":") {
		t.Errorf("GenerateDependabotBaitWorkflowYAML() left the quoted \"on\" key:\n%s", yaml)
	}
}

func TestGenerateDependabotConfigYAML(t *testing.T) {
	config, err := GenerateDependabotConfigYAML("gh-secret-kit-dependabot-copy")
	if err != nil {
		t.Fatalf("GenerateDependabotConfigYAML() error = %v", err)
	}
	for _, expected := range []string{
		"version: 2",
		"package-ecosystem: github-actions",
		"dependency-name: actions/checkout",
		"gh-secret-kit-dependabot-copy",
	} {
		if !strings.Contains(config, expected) {
			t.Errorf("GenerateDependabotConfigYAML() missing %q:\n%s", expected, config)
		}
	}
	if _, err := GenerateDependabotConfigYAML("bad\nbranch"); err == nil {
		t.Errorf("GenerateDependabotConfigYAML() expected an error for a branch name with a newline")
	}
}
