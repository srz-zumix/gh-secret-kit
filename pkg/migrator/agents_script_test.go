package migrator

import (
	"strings"
	"testing"
)

func TestGenerateAgentsCopyScript(t *testing.T) {
	script, err := GenerateAgentsCopyScript(AgentsCopyConfig{
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppAgents,
		Secrets:        []string{"FOO", "BAR"},
		Rename:         map[string]string{"BAR": "BAZ"},
		Destinations: []AgentsCopyDestination{
			{Target: "dst-owner/dst-repo", Host: "github.com", TokenEnv: "COPY_TOKEN_GITHUB_COM"},
		},
	})
	if err != nil {
		t.Fatalf("GenerateAgentsCopyScript() error = %v", err)
	}

	// The agent environment exposes the values directly, so no token file is
	// transferred and no codespace environment is sourced.
	for _, unexpected := range []string{"_token_file", "/workspaces/.codespaces/shared/.env"} {
		if strings.Contains(script, unexpected) {
			t.Errorf("GenerateAgentsCopyScript() should not contain %q:\n%s", unexpected, script)
		}
	}
	for _, expected := range []string{
		"unset GITHUB_TOKEN GH_TOKEN GH_ENTERPRISE_TOKEN GH_HOST GH_REPO",
		"export GH_HOST='github.com'",
		"export GH_TOKEN=\"${COPY_TOKEN_GITHUB_COM}\"",
		"SECRET_VALUE=\"${FOO-}\"",
		"SECRET_VALUE=\"${BAR-}\"",
		"--app agents",
		AgentsCopyDoneMarker,
	} {
		if !strings.Contains(script, expected) {
			t.Errorf("GenerateAgentsCopyScript() missing %q:\n%s", expected, script)
		}
	}
	if !strings.Contains(script, "BAZ") {
		t.Errorf("GenerateAgentsCopyScript() did not apply the rename mapping:\n%s", script)
	}
}

func TestGenerateAgentsCopyScriptValidation(t *testing.T) {
	base := AgentsCopyConfig{
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppAgents,
		Secrets:        []string{"FOO"},
		Destinations: []AgentsCopyDestination{
			{Target: "dst-owner/dst-repo", Host: "github.com", TokenEnv: "COPY_TOKEN_GITHUB_COM"},
		},
	}

	tests := []struct {
		name   string
		modify func(*AgentsCopyConfig)
	}{
		{"no destination", func(c *AgentsCopyConfig) { c.Destinations = nil }},
		{"no secret", func(c *AgentsCopyConfig) { c.Secrets = nil }},
		{"invalid app", func(c *AgentsCopyConfig) { c.DestinationApp = SecretApp("bogus") }},
		{"reserved secret name", func(c *AgentsCopyConfig) { c.Secrets = []string{"GH_TOKEN"} }},
		{"secret collides with token variable", func(c *AgentsCopyConfig) {
			c.Secrets = []string{"COPY_TOKEN_GITHUB_COM"}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := base
			tt.modify(&config)
			if _, err := GenerateAgentsCopyScript(config); err == nil {
				t.Errorf("GenerateAgentsCopyScript() expected an error, got nil")
			}
		})
	}
}

func TestGenerateAgentsSetupStepsYAML(t *testing.T) {
	config := AgentsCopyConfig{
		Secrets: []string{"SECRET_A"},
		Destinations: []AgentsCopyDestination{
			{Target: "owner/repo", Host: "github.com", TokenEnv: "DST_TOKEN"},
		},
	}
	yaml, err := GenerateAgentsSetupStepsYAML(config, "echo hello\n")
	if err != nil {
		t.Fatalf("GenerateAgentsSetupStepsYAML() error = %v", err)
	}
	for _, expected := range []string{
		"name: Copilot Setup Steps",
		"on:",
		"workflow_dispatch: {}",
		AgentsSetupStepsJobName + ":",
		"runs-on: ubuntu-latest",
		"echo hello",
		"SECRET_A: ${{ secrets.SECRET_A }}",
		"DST_TOKEN: ${{ secrets.DST_TOKEN }}",
	} {
		if !strings.Contains(yaml, expected) {
			t.Errorf("GenerateAgentsSetupStepsYAML() missing %q:\n%s", expected, yaml)
		}
	}
	if strings.Contains(yaml, "\"on\":") {
		t.Errorf("GenerateAgentsSetupStepsYAML() left the quoted \"on\" key:\n%s", yaml)
	}
}
