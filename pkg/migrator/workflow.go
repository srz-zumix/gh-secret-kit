package migrator

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// SecretScope indicates whether the migration targets repo or org secrets
type SecretScope string

const (
	// SecretScopeRepo targets repository secrets
	SecretScopeRepo SecretScope = "repo"
	// SecretScopeOrg targets organization secrets
	SecretScopeOrg SecretScope = "org"
	// SecretScopeEnv targets environment secrets
	SecretScopeEnv SecretScope = "env"
)

// WorkflowConfig holds configuration for generating migration workflow
type WorkflowConfig struct {
	WorkflowName           string
	RunnerLabel            string
	TriggerLabel           string
	Source                 string
	Destination            string
	DestinationHost        string
	SourceEnv              string
	DestinationEnv         string
	Secrets                []string
	Rename                 map[string]string // OLD_NAME -> NEW_NAME
	Overwrite              bool
	DestinationTokenSecret string
	Scope                  SecretScope
	// DispatchMode generates a workflow_dispatch-triggered workflow instead of a
	// label-triggered one. Used by the "dispatch" command which rewrites the
	// currently running workflow and re-triggers it via workflow_dispatch.
	DispatchMode bool
	// RunsOn overrides the job runs-on value. When nil, RunnerLabel is used.
	// It accepts any YAML value (string or list) so the original runner setting
	// can be reused verbatim.
	RunsOn any
	// CleanupBranch, when set in DispatchMode, makes the generated workflow
	// delete the given branch on the source repository after a successful run.
	CleanupBranch string
}

// WorkflowYAML represents the structure of a GitHub Actions workflow
type WorkflowYAML struct {
	Name        string            `yaml:"name"`
	On          map[string]any    `yaml:"on"`
	Permissions map[string]string `yaml:"permissions,omitempty"`
	Jobs        map[string]Job    `yaml:"jobs"`
}

// Job represents a job in a workflow
type Job struct {
	If          string            `yaml:"if,omitempty"`
	RunsOn      any               `yaml:"runs-on"`
	Environment string            `yaml:"environment,omitempty"`
	Steps       []Step            `yaml:"steps"`
	Env         map[string]string `yaml:"env,omitempty"`
}

// Step represents a step in a job
type Step struct {
	Name string            `yaml:"name,omitempty"`
	Uses string            `yaml:"uses,omitempty"`
	Run  string            `yaml:"run,omitempty"`
	Env  map[string]string `yaml:"env,omitempty"`
	If   string            `yaml:"if,omitempty"`
}

// GenerateWorkflowYAML generates a GitHub Actions workflow YAML for secret migration
func GenerateWorkflowYAML(config WorkflowConfig) (string, error) {
	var onTrigger map[string]any
	if config.DispatchMode {
		// workflow_dispatch has no required configuration; an empty mapping is
		// rendered as "workflow_dispatch:" which is valid GitHub Actions syntax.
		onTrigger = map[string]any{
			"workflow_dispatch": map[string]any{},
		}
	} else {
		onTrigger = map[string]any{
			"pull_request": map[string]any{
				"types": []string{"labeled"},
			},
		}
	}
	workflow := WorkflowYAML{
		Name: config.WorkflowName,
		On:   onTrigger,
		Jobs: make(map[string]Job),
	}
	// Dispatch-mode workflows delete the temporary branch using github.token.
	// Repositories whose default GITHUB_TOKEN permissions are read-only would
	// otherwise fail at the cleanup step, so request the minimum required scope
	// explicitly.
	if config.DispatchMode {
		workflow.Permissions = map[string]string{
			"contents": "write",
		}
	}

	steps := []Step{
		{
			Name: "Checkout repository",
			Uses: "actions/checkout@v6",
		},
	}

	// Always set GH_HOST at job level so gh CLI commands target the correct host
	ghHost := config.DestinationHost
	if ghHost == "" {
		ghHost = "github.com"
	}

	// Generate secrets migration steps
	for _, secretName := range config.Secrets {
		destSecretName := secretName
		if newName, ok := config.Rename[secretName]; ok {
			destSecretName = newName
		}

		// Build the step that migrates each secret
		stepEnv := map[string]string{
			"SECRET_VALUE": fmt.Sprintf("${{ secrets.%s }}", secretName),
			"SECRET_NAME":  destSecretName,
			"DESTINATION":  config.Destination,
		}

		if config.DestinationTokenSecret != "" {
			secretRef := fmt.Sprintf("${{ secrets.%s }}", config.DestinationTokenSecret)
			if ghHost == "github.com" {
				stepEnv["GH_TOKEN"] = secretRef
			} else {
				stepEnv["GH_ENTERPRISE_TOKEN"] = secretRef
			}
		}

		if config.DestinationEnv != "" {
			stepEnv["DEST_ENV"] = config.DestinationEnv
		}

		runScript := generateSecretMigrationScript(secretScriptConfig{
			Scope:          config.Scope,
			DestinationEnv: config.DestinationEnv,
			Overwrite:      config.Overwrite,
		}, secretName, destSecretName)

		step := Step{
			Name: fmt.Sprintf("Migrate secret: %s", secretName),
			Run:  runScript,
			Env:  stepEnv,
		}

		steps = append(steps, step)
	}

	// In dispatch mode, append a final step that deletes the temporary dispatch
	// branch from the source repository. Because steps stop on the first
	// failure, this only runs when every migration step succeeded.
	if config.DispatchMode && config.CleanupBranch != "" {
		steps = append(steps, Step{
			Name: "Cleanup dispatch branch",
			Run:  generateCleanupBranchScript(config.CleanupBranch),
			Env: map[string]string{
				"GH_TOKEN":            "${{ github.token }}",
				"GH_ENTERPRISE_TOKEN": "${{ github.token }}",
			},
		})
	}

	job := Job{
		RunsOn:      config.RunnerLabel,
		Environment: config.SourceEnv,
		Steps:       steps,
	}
	// Allow the original runner setting (string or list) to be reused verbatim.
	if config.RunsOn != nil {
		job.RunsOn = config.RunsOn
	}
	if !config.DispatchMode && config.TriggerLabel != "" {
		job.If = fmt.Sprintf("github.event.label.name == '%s'", config.TriggerLabel)
	}
	job.Env = map[string]string{
		"GH_HOST": ghHost,
	}

	workflow.Jobs["migrate-secrets"] = job

	return marshalWorkflow(&workflow)
}

// marshalWorkflow renders a workflow as GitHub Actions YAML with 2-space indent.
func marshalWorkflow(workflow *WorkflowYAML) (string, error) {
	var buf bytes.Buffer
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(workflow); err != nil {
		return "", fmt.Errorf("failed to marshal workflow to YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	// "on" is a YAML reserved keyword (boolean true), so the marshaler quotes it.
	// Replace the quoted key with the unquoted form for valid GitHub Actions syntax.
	return strings.Replace(buf.String(), "\"on\":", "on:", 1), nil
}

// secretScriptConfig holds the subset of settings that the per-secret script
// depends on, so the migration and copy workflow generators can share it.
type secretScriptConfig struct {
	Scope          SecretScope
	DestinationEnv string
	Overwrite      bool
}

// generateSecretMigrationScript generates the script to migrate a single secret
func generateSecretMigrationScript(config secretScriptConfig, srcName, destName string) string {
	var script strings.Builder

	// Determine gh secret subcommand flags based on scope
	// repo scope: gh secret set NAME -R owner/repo
	// org scope:  gh secret set NAME --org org-name
	scopeFlag := "-R \"${DESTINATION}\""
	listScopeFlag := "-R \"${DESTINATION}\""
	if config.Scope == SecretScopeOrg {
		scopeFlag = "--org \"${DESTINATION}\""
		listScopeFlag = "--org \"${DESTINATION}\""
	}

	// Check if secret value is empty
	script.WriteString("if [ -z \"${SECRET_VALUE}\" ]; then\n")
	fmt.Fprintf(&script, "  echo \"Secret %s is empty or does not exist, skipping...\"\n", srcName)
	script.WriteString("  exit 0\n")
	script.WriteString("fi\n\n")

	if !config.Overwrite {
		// Check if destination secret already exists
		script.WriteString("# Check if secret already exists at destination\n")
		if config.DestinationEnv != "" {
			fmt.Fprintf(&script, "if gh secret list --env \"${DEST_ENV}\" -R \"${DESTINATION}\" | grep -q \"^%s\"; then\n", destName)
		} else {
			fmt.Fprintf(&script, "if gh secret list %s | grep -q \"^%s\"; then\n", listScopeFlag, destName)
		}
		fmt.Fprintf(&script, "  echo \"Secret %s already exists at destination, skipping...\"\n", destName)
		script.WriteString("  exit 0\n")
		script.WriteString("fi\n\n")
	}

	// Set the secret at destination
	fmt.Fprintf(&script, "# Set secret %s at destination\n", destName)
	script.WriteString("echo \"${SECRET_VALUE}\" | \\\n")
	if config.DestinationEnv != "" {
		fmt.Fprintf(&script, "  gh secret set %s --env \"${DEST_ENV}\" -R \"${DESTINATION}\"\n", destName)
	} else {
		fmt.Fprintf(&script, "  gh secret set %s %s\n", destName, scopeFlag)
	}

	fmt.Fprintf(&script, "echo \"Successfully migrated secret: %s -> %s\"\n", srcName, destName)

	return script.String()
}

// generateCleanupBranchScript generates the script that deletes the temporary
// dispatch branch from the source repository after a successful migration.
// The source host is derived from GITHUB_SERVER_URL so the delete targets the
// correct host even when the job-level GH_HOST points at the destination.
func generateCleanupBranchScript(branch string) string {
	// Single-quote the branch name so that shell metacharacters (e.g. $, `,
	// command substitutions) in a user-supplied branch name are not evaluated.
	// Any embedded single quotes are escaped using the '\'' idiom.
	quoted := "'" + strings.ReplaceAll(branch, "'", "'\\''") + "'"
	var script strings.Builder
	script.WriteString("host=\"${GITHUB_SERVER_URL#http://}\"\n")
	script.WriteString("host=\"${host#https://}\"\n")
	// Assign to a local variable so the quoted literal is expanded only once;
	// subsequent references via ${_branch} inside double quotes are safe.
	fmt.Fprintf(&script, "_branch=%s\n", quoted)
	script.WriteString("GH_HOST=\"${host}\" gh api -X DELETE \"repos/${GITHUB_REPOSITORY}/git/refs/heads/${_branch}\"\n")
	script.WriteString("echo \"Deleted dispatch branch: ${_branch}\"\n")
	return script.String()
}

// ParseRunsOnFromWorkflow extracts the runs-on value of the named job from a
// workflow YAML document. When jobName is empty or not found, the runs-on of
// the first job (in sorted order for determinism) is returned. The returned
// value may be a string or a list, matching the original YAML.
//
// A minimal struct is used for unmarshaling so that unrelated top-level fields
// (e.g. "permissions: read-all") with types that don't match WorkflowYAML's
// richer field definitions cannot cause a parse error.
func ParseRunsOnFromWorkflow(workflowYAML, jobName string) (any, error) {
	// workflowJobsOnly contains only the fields needed to extract runs-on.
	// Using a dedicated struct avoids type-mismatch errors from top-level fields
	// such as "permissions: read-all" (string) vs. WorkflowYAML's
	// "permissions: map[string]string" which would reject that valid syntax.
	type jobMinimal struct {
		RunsOn any `yaml:"runs-on"`
	}
	type workflowJobsOnly struct {
		Jobs map[string]jobMinimal `yaml:"jobs"`
	}
	var wf workflowJobsOnly
	if err := yaml.Unmarshal([]byte(workflowYAML), &wf); err != nil {
		return nil, fmt.Errorf("failed to parse workflow YAML: %w", err)
	}
	if len(wf.Jobs) == 0 {
		return nil, fmt.Errorf("workflow has no jobs")
	}
	if jobName != "" {
		if job, ok := wf.Jobs[jobName]; ok {
			return job.RunsOn, nil
		}
	}
	keys := make([]string, 0, len(wf.Jobs))
	for k := range wf.Jobs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return wf.Jobs[keys[0]].RunsOn, nil
}

// EncodeWorkflowContent encodes workflow content to base64 for GitHub API
func EncodeWorkflowContent(content string) string {
	return base64.StdEncoding.EncodeToString([]byte(content))
}

// GenerateStubWorkflowYAML generates a minimal workflow YAML with a pull_request trigger.
// This stub is pushed to a temporary branch, then a PR is opened to fire the pull_request event so GitHub
// recognizes the workflow. The PR is closed immediately and the branch is deleted afterwards.
func GenerateStubWorkflowYAML(workflowName string) (string, error) {
	const tmpl = `name: %s
on:
  pull_request:
    types:
      - labeled
jobs:
  placeholder:
    runs-on: ubuntu-latest
    steps:
      - name: Placeholder
        run: echo "This is a stub workflow for gh-secret-kit migrate."
`
	return fmt.Sprintf(tmpl, workflowName), nil
}

// GenerateBrokenWorkflowYAML generates a workflow that has a workflow_dispatch
// trigger but is intentionally invalid YAML. Pushing this file and attempting a
// dispatch registers the workflow on the branch (GitHub responds with an error
// but still creates the workflow), which makes a subsequent dispatch of the
// corrected workflow succeed without it being present on the default branch.
func GenerateBrokenWorkflowYAML(workflowName string) (string, error) {
	const tmpl = `name: %s
on:
  workflow_dispatch:
jobs:
  broken:
    runs-on: ubuntu-latest
    steps:
      - run: echo "registering"
broken: [
`
	return fmt.Sprintf(tmpl, workflowName), nil
}
