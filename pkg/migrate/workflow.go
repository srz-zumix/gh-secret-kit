package migrate

import (
	"encoding/base64"
	"fmt"
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
	WorkflowName     string
	RunnerLabel      string
	TriggerLabel     string
	Source           string
	Destination      string
	DestinationHost  string
	SourceEnv        string
	DestinationEnv   string
	Secrets          []string
	Rename           map[string]string // OLD_NAME -> NEW_NAME
	Overwrite             bool
	DestinationTokenSecret string
	Scope                  SecretScope
}

// WorkflowYAML represents the structure of a GitHub Actions workflow
type WorkflowYAML struct {
	Name string                 `yaml:"name"`
	On   map[string]interface{} `yaml:"on"`
	Jobs map[string]Job         `yaml:"jobs"`
}

// Job represents a job in a workflow
type Job struct {
	If          string            `yaml:"if,omitempty"`
	RunsOn      string            `yaml:"runs-on"`
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
	onTrigger := map[string]interface{}{
		"pull_request": map[string]interface{}{
			"types": []string{"labeled"},
		},
	}
	workflow := WorkflowYAML{
		Name: config.WorkflowName,
		On:   onTrigger,
		Jobs: make(map[string]Job),
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

		runScript := generateSecretMigrationScript(config, secretName, destSecretName)

		step := Step{
			Name: fmt.Sprintf("Migrate secret: %s", secretName),
			Run:  runScript,
			Env:  stepEnv,
		}

		steps = append(steps, step)
	}

	job := Job{
		RunsOn:      config.RunnerLabel,
		Environment: config.SourceEnv,
		Steps:       steps,
	}
	if config.TriggerLabel != "" {
		job.If = fmt.Sprintf("github.event.label.name == '%s'", config.TriggerLabel)
	}
	job.Env = map[string]string{
		"GH_HOST": ghHost,
	}

	workflow.Jobs["migrate-secrets"] = job

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(&workflow)
	if err != nil {
		return "", fmt.Errorf("failed to marshal workflow to YAML: %w", err)
	}

	// "on" is a YAML reserved keyword (boolean true), so the marshaler quotes it.
	// Replace the quoted key with the unquoted form for valid GitHub Actions syntax.
	result := strings.Replace(string(yamlBytes), "\"on\":", "on:", 1)

	return result, nil
}

// generateSecretMigrationScript generates the script to migrate a single secret
func generateSecretMigrationScript(config WorkflowConfig, srcName, destName string) string {
	var script strings.Builder

	// Determine gh secret subcommand flags based on scope
	// repo scope: gh secret set NAME -R owner/repo
	// org scope:  gh secret set NAME --org org-name
	scopeFlag := "-R $DESTINATION"
	listScopeFlag := "-R $DESTINATION"
	if config.Scope == SecretScopeOrg {
		scopeFlag = "--org $DESTINATION"
		listScopeFlag = "--org $DESTINATION"
	}

	// Check if secret value is empty
	script.WriteString("if [ -z \"$SECRET_VALUE\" ]; then\n")
	fmt.Fprintf(&script, "  echo \"Secret %s is empty or does not exist, skipping...\"\n", srcName)
	script.WriteString("  exit 0\n")
	script.WriteString("fi\n\n")

	if !config.Overwrite {
		// Check if destination secret already exists
		script.WriteString("# Check if secret already exists at destination\n")
		if config.DestinationEnv != "" {
			fmt.Fprintf(&script, "if gh secret list --env $DEST_ENV -R $DESTINATION | grep -q \"^%s\"; then\n", destName)
		} else {
			fmt.Fprintf(&script, "if gh secret list %s | grep -q \"^%s\"; then\n", listScopeFlag, destName)
		}
		fmt.Fprintf(&script, "  echo \"Secret %s already exists at destination, skipping...\"\n", destName)
		script.WriteString("  exit 0\n")
		script.WriteString("fi\n\n")
	}

	// Set the secret at destination
	fmt.Fprintf(&script, "# Set secret %s at destination\n", destName)
	script.WriteString("echo \"$SECRET_VALUE\" | \\\n")
	if config.DestinationEnv != "" {
		fmt.Fprintf(&script, "  gh secret set %s --env $DEST_ENV -R $DESTINATION\n", destName)
	} else {
		fmt.Fprintf(&script, "  gh secret set %s %s\n", destName, scopeFlag)
	}

	fmt.Fprintf(&script, "echo \"Successfully migrated secret: %s -> %s\"\n", srcName, destName)

	return script.String()
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
