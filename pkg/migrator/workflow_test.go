package migrator

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGenerateWorkflowYAMLDispatchMode(t *testing.T) {
	config := WorkflowConfig{
		WorkflowName:  "my-workflow",
		Source:        "owner/repo",
		Destination:   "owner/dest",
		Secrets:       []string{"FOO"},
		Scope:         SecretScopeRepo,
		DispatchMode:  true,
		RunsOn:        []string{"self-hosted", "linux"},
		CleanupBranch: "tmp-dispatch-branch",
	}

	out, err := GenerateWorkflowYAML(config)
	if err != nil {
		t.Fatalf("GenerateWorkflowYAML returned error: %v", err)
	}

	if !strings.Contains(out, "workflow_dispatch:") {
		t.Errorf("expected workflow_dispatch trigger, got:\n%s", out)
	}
	if strings.Contains(out, "pull_request") {
		t.Errorf("dispatch mode should not contain pull_request trigger, got:\n%s", out)
	}
	if strings.Contains(out, "github.event.label.name") {
		t.Errorf("dispatch mode should not set a label condition, got:\n%s", out)
	}
	if !strings.Contains(out, "Cleanup dispatch branch") {
		t.Errorf("expected cleanup step, got:\n%s", out)
	}
	if !strings.Contains(out, "tmp-dispatch-branch") {
		t.Errorf("expected cleanup branch name in script, got:\n%s", out)
	}
	// runs-on should preserve the list form.
	if !strings.Contains(out, "self-hosted") || !strings.Contains(out, "- linux") {
		t.Errorf("expected list runs-on to be preserved, got:\n%s", out)
	}
}

func TestParseRunsOnFromWorkflow(t *testing.T) {
	yaml := `name: ci
on:
  workflow_dispatch:
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo build
  deploy:
    runs-on:
      - self-hosted
      - linux
    steps:
      - run: echo deploy
`

	t.Run("named job string", func(t *testing.T) {
		got, err := ParseRunsOnFromWorkflow(yaml, "build")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "ubuntu-latest" {
			t.Errorf("expected ubuntu-latest, got %v", got)
		}
	})

	t.Run("named job list", func(t *testing.T) {
		got, err := ParseRunsOnFromWorkflow(yaml, "deploy")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		list, ok := got.([]any)
		if !ok {
			t.Fatalf("expected list, got %T", got)
		}
		if len(list) != 2 || list[0] != "self-hosted" || list[1] != "linux" {
			t.Errorf("unexpected list runs-on: %v", list)
		}
	})

	t.Run("fallback to first job", func(t *testing.T) {
		got, err := ParseRunsOnFromWorkflow(yaml, "missing")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// "build" sorts before "deploy".
		if got != "ubuntu-latest" {
			t.Errorf("expected fallback to build job, got %v", got)
		}
	})

	t.Run("no jobs", func(t *testing.T) {
		if _, err := ParseRunsOnFromWorkflow("name: x\non:\n  push:\n", ""); err == nil {
			t.Error("expected error for workflow with no jobs")
		}
	})
}

func TestGenerateBrokenWorkflowYAML(t *testing.T) {
	out, err := GenerateBrokenWorkflowYAML("my-workflow")
	if err != nil {
		t.Fatalf("GenerateBrokenWorkflowYAML returned error: %v", err)
	}
	if !strings.Contains(out, "workflow_dispatch:") {
		t.Errorf("expected workflow_dispatch trigger, got:\n%s", out)
	}
	if !strings.Contains(out, "my-workflow") {
		t.Errorf("expected workflow name, got:\n%s", out)
	}
	// The generated YAML must be invalid so the dispatch attempt registers it.
	var v any
	if err := yaml.Unmarshal([]byte(out), &v); err == nil {
		t.Errorf("expected invalid YAML, but it parsed successfully:\n%s", out)
	}
}
