package migrator

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateDumpWorkflowYAML(t *testing.T) {
	config := DumpWorkflowConfig{
		WorkflowName:  "my-dump-workflow",
		Secrets:       []string{"FOO", "BAR"},
		Output:        "/tmp/secrets-dump.env",
		RunsOn:        []string{"self-hosted", "linux"},
		CleanupBranch: "tmp-dump-branch",
	}

	out, err := GenerateDumpWorkflowYAML(config)
	if err != nil {
		t.Fatalf("GenerateDumpWorkflowYAML returned error: %v", err)
	}

	if !strings.Contains(out, "workflow_dispatch:") {
		t.Errorf("expected workflow_dispatch trigger, got:\n%s", out)
	}
	if !strings.Contains(out, "contents: write") {
		t.Errorf("expected contents: write permission, got:\n%s", out)
	}
	if !strings.Contains(out, "Initialize dump file") {
		t.Errorf("expected init step, got:\n%s", out)
	}
	if !strings.Contains(out, "umask 077") || !strings.Contains(out, ": > \"${OUTPUT_FILE}\"") || !strings.Contains(out, "chmod 600") {
		t.Errorf("expected file truncation with mode 0600, got:\n%s", out)
	}
	for _, name := range config.Secrets {
		if !strings.Contains(out, "Dump secret: "+name) {
			t.Errorf("expected step for secret %s, got:\n%s", name, out)
		}
		if !strings.Contains(out, "${{ secrets."+name+" }}") {
			t.Errorf("expected secret expression for %s, got:\n%s", name, out)
		}
	}
	if !strings.Contains(out, "base64") {
		t.Errorf("expected base64 encoding, got:\n%s", out)
	}
	if !strings.Contains(out, "tr -d '\\n'") {
		t.Errorf("expected base64 output to be flattened to a single line, got:\n%s", out)
	}
	if strings.Contains(out, "echo \"${SECRET_VALUE}\"") {
		t.Errorf("raw secret value must never be echoed, got:\n%s", out)
	}
	if !strings.Contains(out, "Cleanup dispatch branch") {
		t.Errorf("expected cleanup step, got:\n%s", out)
	}
	if !strings.Contains(out, "tmp-dump-branch") {
		t.Errorf("expected cleanup branch name in script, got:\n%s", out)
	}
	if !strings.Contains(out, "self-hosted") || !strings.Contains(out, "- linux") {
		t.Errorf("expected list runs-on to be preserved, got:\n%s", out)
	}

	// Cleanup must be the last step so it only runs when every dump step succeeded.
	dumpIdx := strings.Index(out, "Dump secret: BAR")
	cleanupIdx := strings.Index(out, "Cleanup dispatch branch")
	if dumpIdx == -1 || cleanupIdx == -1 || cleanupIdx < dumpIdx {
		t.Errorf("expected cleanup step after all dump steps, got:\n%s", out)
	}

	var v any
	if err := yaml.Unmarshal([]byte(out), &v); err != nil {
		t.Errorf("expected valid YAML, got error: %v\n%s", err, out)
	}
}

func TestGenerateDumpWorkflowYAMLEmptySecret(t *testing.T) {
	out, err := GenerateDumpWorkflowYAML(DumpWorkflowConfig{
		WorkflowName: "my-dump-workflow",
		Secrets:      []string{"FOO"},
		Output:       "secrets-dump.env",
	})
	if err != nil {
		t.Fatalf("GenerateDumpWorkflowYAML returned error: %v", err)
	}
	if !strings.Contains(out, "is empty or does not exist, skipping") {
		t.Errorf("expected empty-value skip handling, got:\n%s", out)
	}
	if strings.Contains(out, "Cleanup dispatch branch") {
		t.Errorf("expected no cleanup step without CleanupBranch, got:\n%s", out)
	}
}
