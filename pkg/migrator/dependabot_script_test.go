package migrator

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func dependabotCopyFixture() DependabotCopyConfig {
	return DependabotCopyConfig{
		Scope:          SecretScopeRepo,
		DestinationApp: SecretAppDependabot,
		Secrets:        []string{"FOO", "BAR"},
		Rename:         map[string]string{"BAR": "BAZ"},
		Destinations: []DependabotCopyDestination{
			{Target: "owner/repo", Host: "github.com", TokenEnv: "COPY_TOKEN_GITHUB_COM"},
		},
	}
}

func TestGenerateDependabotCopyScript(t *testing.T) {
	config := dependabotCopyFixture()
	config.Destinations = append(config.Destinations, DependabotCopyDestination{
		Target: "other/repo", Host: "enterprise.example", TokenEnv: "COPY_TOKEN_ENTERPRISE",
	})
	script, err := GenerateDependabotCopyScript(config)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"set -euo pipefail",
		"unset GITHUB_TOKEN GH_TOKEN GH_ENTERPRISE_TOKEN GH_HOST GH_REPO",
		"export GH_HOST='github.com'",
		"export GH_TOKEN=\"${COPY_TOKEN_GITHUB_COM}\"",
		"export GH_HOST='enterprise.example'",
		"export GH_ENTERPRISE_TOKEN=\"${COPY_TOKEN_ENTERPRISE}\"",
		"export DESTINATION='owner/repo'",
		"export DESTINATION='other/repo'",
		"SECRET_VALUE=\"${FOO-}\"",
		"SECRET_VALUE=\"${BAR-}\"",
		"gh secret set BAZ -R \"${DESTINATION}\" --app dependabot",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
	if strings.Count(script, DependabotCopyDoneMarker) != 1 {
		t.Error("script must contain exactly one completion marker")
	}
	if !strings.HasSuffix(script, "echo '"+DependabotCopyDoneMarker+"'\n") {
		t.Error("completion marker must be the final statement")
	}
	command := exec.Command("bash", "-n")
	command.Stdin = strings.NewReader(script)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("invalid Bash syntax: %v\n%s", err, output)
	}
}

func TestDependabotCopyScriptScopeAndStore(t *testing.T) {
	for _, scope := range []SecretScope{SecretScopeRepo, SecretScopeOrg} {
		for _, app := range []SecretApp{SecretAppActions, SecretAppAgents, SecretAppCodespaces, SecretAppDependabot} {
			for _, overwrite := range []bool{false, true} {
				name := string(scope) + "/" + string(app)
				if overwrite {
					name += "/overwrite"
				}
				t.Run(name, func(t *testing.T) {
					config := dependabotCopyFixture()
					config.Scope = scope
					config.DestinationApp = app
					config.Overwrite = overwrite
					scopeFlag := "-R \"${DESTINATION}\""
					if scope == SecretScopeOrg {
						config.Destinations[0].Target = "destination-org"
						scopeFlag = "--org \"${DESTINATION}\""
					}
					appFlag := ""
					if app != SecretAppActions {
						appFlag = " --app " + string(app)
					}
					script, err := GenerateDependabotCopyScript(config)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(script, "gh secret set BAZ "+scopeFlag+appFlag) {
						t.Errorf("script omitted destination scope, store, or rename:\n%s", script)
					}
					if strings.Contains(script, "gh secret list") == overwrite {
						t.Errorf("destination existence check does not match overwrite=%v", overwrite)
					}
				})
			}
		}
	}
}

func TestDependabotCopyScriptCompletion(t *testing.T) {
	cases := []struct {
		name       string
		prelude    string
		wantError  bool
		wantCopied bool
	}{
		{"copies remaining secrets after missing value", "unset FOO\nBAR=value\n", false, true},
		{"copies remaining secrets after empty value", "FOO=\nBAR=value\n", false, true},
		{"skips existing and copies next", "FOO=value\nBAR=value\nexisting=FOO\n", false, true},
		{"all absent values are skipped", "unset FOO BAR\n", false, false},
		{"failed write cannot print completion", "FOO=value\nBAR=value\nfail_name=BAZ\n", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := dependabotCopyFixture()
			script, err := GenerateDependabotCopyScript(config)
			if err != nil {
				t.Fatal(err)
			}
			prelude := `COPY_TOKEN_GITHUB_COM=placeholder
gh() {
  if [ "$1 $2" = "secret list" ]; then
    printf '%s\n' "${existing:-}"
    return 0
  fi
  if [ "$1 $2" = "secret set" ]; then
    if [ "$3" = "${fail_name:-}" ]; then
      return 1
    fi
    cat >/dev/null
    printf 'copied %s\n' "$3"
    return 0
  fi
  return 2
}
`
			command := exec.Command("bash", "-c", prelude+tc.prelude+script)
			command.Env = append(os.Environ(), "BASH_ENV=")
			output, err := command.CombinedOutput()
			if (err != nil) != tc.wantError {
				t.Fatalf("error = %v, wantError = %v\n%s", err, tc.wantError, output)
			}
			got := string(output)
			if strings.Contains(got, DependabotCopyDoneMarker) == tc.wantError {
				t.Errorf("completion marker does not match success:\n%s", got)
			}
			if strings.Contains(got, "copied BAZ") != tc.wantCopied {
				t.Errorf("remaining secret copy mismatch:\n%s", got)
			}
		})
	}
}

func TestDependabotCopyConfigValidation(t *testing.T) {
	cases := []struct {
		name   string
		modify func(*DependabotCopyConfig)
	}{
		{"empty scope", func(c *DependabotCopyConfig) { c.Scope = "" }},
		{"environment scope", func(c *DependabotCopyConfig) { c.Scope = SecretScopeEnv }},
		{"unknown scope", func(c *DependabotCopyConfig) { c.Scope = "other" }},
		{"no destinations", func(c *DependabotCopyConfig) { c.Destinations = nil }},
		{"no secrets", func(c *DependabotCopyConfig) { c.Secrets = nil }},
		{"invalid store", func(c *DependabotCopyConfig) { c.DestinationApp = "other" }},
		{"invalid source name", func(c *DependabotCopyConfig) { c.Secrets = []string{"has-dash"} }},
		{"invalid renamed name", func(c *DependabotCopyConfig) { c.Rename = map[string]string{"FOO": "has-dash"} }},
		{"reserved source name", func(c *DependabotCopyConfig) { c.Secrets = []string{"GH_TOKEN"} }},
		{"special source name", func(c *DependabotCopyConfig) { c.Secrets = []string{"RANDOM"} }},
		{"source token collision", func(c *DependabotCopyConfig) { c.Secrets = []string{"COPY_TOKEN_GITHUB_COM"} }},
		{"reserved token variable", func(c *DependabotCopyConfig) { c.Destinations[0].TokenEnv = "GITHUB_TOKEN" }},
		{"unsafe token variable", func(c *DependabotCopyConfig) { c.Destinations[0].TokenEnv = "${{ secrets.TOKEN }}" }},
		{"unsafe destination", func(c *DependabotCopyConfig) { c.Destinations[0].Target = "owner/repo'; echo unsafe" }},
		{"unsafe host", func(c *DependabotCopyConfig) { c.Destinations[0].Host = "${{ github.server_url }}" }},
		{"cross-host token collision", func(c *DependabotCopyConfig) {
			c.Destinations = append(c.Destinations, DependabotCopyDestination{
				Target: "other/repo", Host: "enterprise.example", TokenEnv: "COPY_TOKEN_GITHUB_COM",
			})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := dependabotCopyFixture()
			tc.modify(&config)
			if _, err := GenerateDependabotCopyScript(config); err == nil {
				t.Error("script accepted invalid configuration")
			}
			if _, err := GenerateDependabotCopyWorkflowYAML(config, "echo hello\n", "copy", "self-hosted", "copy-run-123"); err == nil {
				t.Error("workflow accepted invalid configuration")
			}
		})
	}
}

func TestGenerateDependabotCopyWorkflowYAML(t *testing.T) {
	config := dependabotCopyFixture()
	script, err := GenerateDependabotCopyScript(config)
	if err != nil {
		t.Fatal(err)
	}
	text, err := GenerateDependabotCopyWorkflowYAML(config, script, "custom-copy_123", "self-hosted", "copy-run-123")
	if err != nil {
		t.Fatal(err)
	}
	var workflow WorkflowYAML
	if err := yaml.Unmarshal([]byte(text), &workflow); err != nil {
		t.Fatal(err)
	}
	if workflow.Name != "custom-copy_123" || workflow.RunName != "copy-run-123" {
		t.Errorf("workflow metadata not preserved: name=%q run-name=%q", workflow.Name, workflow.RunName)
	}
	if len(workflow.On) != 1 {
		t.Errorf("expected only a push trigger, got %v", workflow.On)
	}
	push, ok := workflow.On["push"].(map[string]any)
	if !ok || !reflect.DeepEqual(push["branches"], []any{"dependabot/**"}) {
		t.Errorf("push must select only Dependabot branches, got %v", workflow.On)
	}
	if !reflect.DeepEqual(workflow.Permissions, map[string]string{"contents": "read"}) {
		t.Errorf("unexpected permissions: %v", workflow.Permissions)
	}
	if len(workflow.Jobs) != 1 {
		t.Fatalf("expected one copy job, got %v", workflow.Jobs)
	}
	job, ok := workflow.Jobs[DependabotCopyJobName]
	if !ok {
		t.Fatal("copy job missing")
	}
	if job.If != "github.actor == 'dependabot[bot]'" || job.RunsOn != "self-hosted" {
		t.Errorf("unexpected actor gate or runner: %+v", job)
	}
	if len(job.Steps) != 1 {
		t.Fatalf("expected one self-contained copy step, got %d", len(job.Steps))
	}
	step := job.Steps[0]
	if step.Name != DependabotCopyStepName || step.Shell != "bash" || step.Uses != "" || step.Run != script {
		t.Errorf("unexpected copy step: %+v", step)
	}
	wantEnv := map[string]string{
		"FOO":                   "${{ secrets.FOO }}",
		"BAR":                   "${{ secrets.BAR }}",
		"COPY_TOKEN_GITHUB_COM": "${{ secrets.COPY_TOKEN_GITHUB_COM }}",
	}
	if !reflect.DeepEqual(step.Env, wantEnv) {
		t.Errorf("secret environment = %v, want %v", step.Env, wantEnv)
	}
	for _, forbidden := range []string{"pull_request:", "workflow_dispatch:", "uses:", "\"on\":"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("copy workflow contains %q", forbidden)
		}
	}
}

func TestDependabotWorkflowMetadataValidation(t *testing.T) {
	config := dependabotCopyFixture()
	cases := []struct {
		name     string
		workflow string
		runner   string
		runName  string
	}{
		{"empty filename", "", "self-hosted", "run-1"},
		{"filename traversal", "../copy", "self-hosted", "run-1"},
		{"absolute filename", "/copy", "self-hosted", "run-1"},
		{"nested filename", "nested/copy", "self-hosted", "run-1"},
		{"filename extension", "copy.yml", "self-hosted", "run-1"},
		{"filename expression", "${{ github.ref }}", "self-hosted", "run-1"},
		{"filename newline", "copy\nother", "self-hosted", "run-1"},
		{"empty runner", "copy", "", "run-1"},
		{"whitespace runner", "copy", " \t", "run-1"},
		{"runner expression", "copy", "${{ secrets.RUNNER }}", "run-1"},
		{"runner newline", "copy", "runner\nother", "run-1"},
		{"runner carriage return", "copy", "runner\rother", "run-1"},
		{"empty run name", "copy", "self-hosted", ""},
		{"whitespace run name", "copy", "self-hosted", " \t"},
		{"run name expression", "copy", "self-hosted", "${{ secrets.TOKEN }}"},
		{"run name newline", "copy", "self-hosted", "run\nother"},
		{"run name carriage return", "copy", "self-hosted", "run\rother"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := GenerateDependabotCopyWorkflowYAML(config, "echo hello\n", tc.workflow, tc.runner, tc.runName); err == nil {
				t.Error("workflow accepted unsafe metadata")
			}
		})
	}
	if err := ValidateDependabotWorkflowOptions("copy_123-v2", "self-hosted"); err != nil {
		t.Errorf("valid custom workflow options rejected: %v", err)
	}
}

func TestGenerateDependabotBaitWorkflowYAML(t *testing.T) {
	text, err := GenerateDependabotBaitWorkflowYAML()
	if err != nil {
		t.Fatal(err)
	}
	var workflow WorkflowYAML
	if err := yaml.Unmarshal([]byte(text), &workflow); err != nil {
		t.Fatal(err)
	}
	if len(workflow.On) != 1 || workflow.On["workflow_dispatch"] == nil {
		t.Errorf("bait must be manual-only: %v", workflow.On)
	}
	if len(workflow.Jobs) != 1 {
		t.Fatalf("expected one disabled bait job, got %d", len(workflow.Jobs))
	}
	for _, job := range workflow.Jobs {
		if job.If != "false" {
			t.Errorf("bait job can execute: if=%q", job.If)
		}
		if job.RunsOn != "ubuntu-latest" {
			t.Errorf("unexpected bait runner: %v", job.RunsOn)
		}
		if len(job.Steps) != 1 || job.Steps[0].Uses != "actions/checkout@v1" {
			t.Errorf("bait must reference only checkout@v1: %v", job.Steps)
		}
	}
}

func TestGenerateDependabotConfigYAML(t *testing.T) {
	first, err := GenerateDependabotConfigYAML("copy-run-1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateDependabotConfigYAML("copy-run-2")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(first, "copy-run-1") || !strings.Contains(second, "copy-run-2") {
		t.Error("configuration must change for each temporary branch")
	}
	var config struct {
		Version int `yaml:"version"`
		Updates []struct {
			Ecosystem string `yaml:"package-ecosystem"`
			Directory string `yaml:"directory"`
			Schedule  struct {
				Interval string `yaml:"interval"`
			} `yaml:"schedule"`
			Limit int `yaml:"open-pull-requests-limit"`
			Allow []struct {
				Name string `yaml:"dependency-name"`
			} `yaml:"allow"`
		} `yaml:"updates"`
	}
	if err := yaml.Unmarshal([]byte(first), &config); err != nil {
		t.Fatal(err)
	}
	if config.Version != 2 || len(config.Updates) != 1 {
		t.Fatalf("unexpected Dependabot configuration: %+v", config)
	}
	update := config.Updates[0]
	if update.Ecosystem != "github-actions" || update.Directory != "/" || update.Schedule.Interval != "daily" || update.Limit != 1 {
		t.Errorf("unexpected update configuration: %+v", update)
	}
	if len(update.Allow) != 1 || update.Allow[0].Name != "actions/checkout" {
		t.Errorf("only checkout should be eligible for updates: %v", update.Allow)
	}
	for _, branch := range []string{"", "copy\nbranch", "copy\rbranch"} {
		if _, err := GenerateDependabotConfigYAML(branch); err == nil {
			t.Errorf("accepted invalid configuration branch %q", branch)
		}
	}
}

func TestDependabotMetadataDoesNotChangeExistingGenerators(t *testing.T) {
	cases := []struct {
		name     string
		generate func() (string, error)
	}{
		{"copy", func() (string, error) {
			return GenerateCopyWorkflowYAML(CopyWorkflowConfig{
				WorkflowName: "existing-copy",
				RunsOn:       "self-hosted",
				Scope:        SecretScopeRepo,
				Secrets:      []string{"FOO"},
				Destinations: []CopyDestination{{Target: "owner/repo", Host: "github.com", TokenSecret: "COPY_TOKEN"}},
			})
		}},
		{"migration", func() (string, error) {
			return GenerateWorkflowYAML(WorkflowConfig{
				WorkflowName: "existing-migration",
				RunnerLabel:  "self-hosted",
				Scope:        SecretScopeRepo,
				Secrets:      []string{"FOO"},
				Destination:  "owner/repo",
			})
		}},
		{"stub", func() (string, error) { return GenerateStubWorkflowYAML("existing-stub") }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, err := tc.generate()
			if err != nil {
				t.Fatal(err)
			}
			for _, unwanted := range []string{"run-name:", "shell:"} {
				if strings.Contains(text, unwanted) {
					t.Errorf("existing generator acquired optional metadata %q", unwanted)
				}
			}
			var workflow WorkflowYAML
			if err := yaml.Unmarshal([]byte(text), &workflow); err != nil {
				t.Fatal(err)
			}
			if workflow.RunName != "" {
				t.Errorf("existing run-name changed: %q", workflow.RunName)
			}
			for _, job := range workflow.Jobs {
				for _, step := range job.Steps {
					if step.Shell != "" {
						t.Errorf("existing shell changed: %q", step.Shell)
					}
				}
			}
		})
	}
}
