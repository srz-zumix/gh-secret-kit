package migrator

import (
	"strings"
	"testing"
)

func TestGenerateCodespacesCopyScript(t *testing.T) {
	script, err := GenerateCodespacesCopyScript(CodespacesCopyConfig{
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppCodespaces,
		Secrets:        []string{"FOO", "BAR"},
		Rename:         map[string]string{"BAR": "BAZ"},
		Destinations: []CodespacesCopyDestination{
			{Target: "dst-owner/dst-repo", Host: "github.com", TokenEnv: "GH_SECRET_KIT_COPY_TOKEN_GITHUB_COM"},
		},
		TokenEnvFile: "/tmp/gh-secret-kit-copy.env",
	})
	if err != nil {
		t.Fatalf("GenerateCodespacesCopyScript() error = %v", err)
	}

	expected := []string{
		". /workspaces/.codespaces/shared/.env",
		"_token_file='/tmp/gh-secret-kit-copy.env'",
		". \"${_token_file}\"",
		"rm -f \"${_token_file}\"",
		"unset GITHUB_TOKEN GH_TOKEN GH_ENTERPRISE_TOKEN GH_HOST GH_REPO",
		"export GH_HOST='github.com'",
		"export GH_TOKEN=\"${GH_SECRET_KIT_COPY_TOKEN_GITHUB_COM}\"",
		"unset GH_ENTERPRISE_TOKEN",
		"export DESTINATION='dst-owner/dst-repo'",
		"SECRET_VALUE=\"${FOO-}\"",
		"SECRET_VALUE=\"${BAR-}\"",
		"gh secret list -R \"${DESTINATION}\" --app codespaces",
		"gh secret set FOO -R \"${DESTINATION}\" --app codespaces",
		"gh secret set BAZ -R \"${DESTINATION}\" --app codespaces",
	}
	for _, want := range expected {
		if !strings.Contains(script, want) {
			t.Errorf("script does not contain %q\n%s", want, script)
		}
	}

	// Each secret block runs in a subshell so that a skipped secret does not
	// abort the remaining ones.
	if got := strings.Count(script, "\n(\n"); got != 2 {
		t.Errorf("subshell count = %d, want 2\n%s", got, script)
	}
}

func TestGenerateCodespacesCopyScriptOrgScopeEnterpriseHost(t *testing.T) {
	script, err := GenerateCodespacesCopyScript(CodespacesCopyConfig{
		Scope:     SecretScopeOrg,
		Secrets:   []string{"FOO"},
		Overwrite: true,
		Destinations: []CodespacesCopyDestination{
			{Target: "dst-org", Host: "github.example.com", TokenEnv: "GH_SECRET_KIT_COPY_TOKEN_GITHUB_EXAMPLE_COM"},
		},
		TokenEnvFile: "/tmp/gh-secret-kit-copy.env",
	})
	if err != nil {
		t.Fatalf("GenerateCodespacesCopyScript() error = %v", err)
	}

	expected := []string{
		"export GH_HOST='github.example.com'",
		"export GH_ENTERPRISE_TOKEN=\"${GH_SECRET_KIT_COPY_TOKEN_GITHUB_EXAMPLE_COM}\"",
		"unset GH_TOKEN",
		"export DESTINATION='dst-org'",
		"gh secret set FOO --org \"${DESTINATION}\"",
	}
	for _, want := range expected {
		if !strings.Contains(script, want) {
			t.Errorf("script does not contain %q\n%s", want, script)
		}
	}
	// Actions is the gh CLI default, so no --app flag is expected.
	if strings.Contains(script, "--app") {
		t.Errorf("script should not contain --app\n%s", script)
	}
	// Overwrite skips the existence check.
	if strings.Contains(script, "gh secret list") {
		t.Errorf("script should not contain an existence check\n%s", script)
	}
}

func TestGenerateCodespacesCopyScriptValidation(t *testing.T) {
	validDestinations := []CodespacesCopyDestination{
		{Target: "dst-owner/dst-repo", Host: "github.com", TokenEnv: "TOKEN"},
	}

	tests := []struct {
		name   string
		config CodespacesCopyConfig
	}{
		{
			name: "no destination",
			config: CodespacesCopyConfig{
				Secrets:      []string{"FOO"},
				TokenEnvFile: "/tmp/token.env",
			},
		},
		{
			name: "no secret",
			config: CodespacesCopyConfig{
				Destinations: validDestinations,
				TokenEnvFile: "/tmp/token.env",
			},
		},
		{
			name: "no token file",
			config: CodespacesCopyConfig{
				Secrets:      []string{"FOO"},
				Destinations: validDestinations,
			},
		},
		{
			name: "invalid token file path",
			config: CodespacesCopyConfig{
				Secrets:      []string{"FOO"},
				Destinations: validDestinations,
				TokenEnvFile: "/tmp/token.env; rm -rf /",
			},
		},
		{
			name: "invalid destination app",
			config: CodespacesCopyConfig{
				DestinationApp: "invalid",
				Secrets:        []string{"FOO"},
				Destinations:   validDestinations,
				TokenEnvFile:   "/tmp/token.env",
			},
		},
		{
			name: "invalid secret name",
			config: CodespacesCopyConfig{
				Secrets:      []string{"FOO; rm -rf /"},
				Destinations: validDestinations,
				TokenEnvFile: "/tmp/token.env",
			},
		},
		{
			name: "invalid renamed secret name",
			config: CodespacesCopyConfig{
				Secrets:      []string{"FOO"},
				Rename:       map[string]string{"FOO": "BAR$(id)"},
				Destinations: validDestinations,
				TokenEnvFile: "/tmp/token.env",
			},
		},
		{
			name: "invalid destination target",
			config: CodespacesCopyConfig{
				Secrets:      []string{"FOO"},
				Destinations: []CodespacesCopyDestination{{Target: "dst owner/repo", Host: "github.com", TokenEnv: "TOKEN"}},
				TokenEnvFile: "/tmp/token.env",
			},
		},
		{
			name: "invalid destination host",
			config: CodespacesCopyConfig{
				Secrets:      []string{"FOO"},
				Destinations: []CodespacesCopyDestination{{Target: "dst-owner/dst-repo", Host: "github.com'", TokenEnv: "TOKEN"}},
				TokenEnvFile: "/tmp/token.env",
			},
		},
		{
			name: "invalid token environment variable name",
			config: CodespacesCopyConfig{
				Secrets:      []string{"FOO"},
				Destinations: []CodespacesCopyDestination{{Target: "dst-owner/dst-repo", Host: "github.com", TokenEnv: "TOKEN-1"}},
				TokenEnvFile: "/tmp/token.env",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := GenerateCodespacesCopyScript(tt.config); err == nil {
				t.Error("GenerateCodespacesCopyScript() expected an error, got nil")
			}
		})
	}
}
