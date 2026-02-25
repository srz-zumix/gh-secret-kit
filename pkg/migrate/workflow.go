package migrate

import (
	"encoding/base64"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// WorkflowConfig holds configuration for generating migration workflow
type WorkflowConfig struct {
	WorkflowName     string
	RunnerLabel      string
	Source           string
	Destination      string
	SourceEnv        string
	DestinationEnv   string
	Secrets          []string
	Rename           map[string]string // OLD_NAME -> NEW_NAME
	Overwrite        bool
	DestinationToken string
}

// WorkflowYAML represents the structure of a GitHub Actions workflow
type WorkflowYAML struct {
	Name string                 `yaml:"name"`
	On   map[string]interface{} `yaml:"on"`
	Jobs map[string]Job         `yaml:"jobs"`
}

// Job represents a job in a workflow
type Job struct {
	RunsOn string            `yaml:"runs-on"`
	Steps  []Step            `yaml:"steps"`
	Env    map[string]string `yaml:"env,omitempty"`
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
	workflow := WorkflowYAML{
		Name: config.WorkflowName,
		On: map[string]interface{}{
			"workflow_dispatch": map[string]interface{}{},
		},
		Jobs: make(map[string]Job),
	}

	steps := []Step{
		{
			Name: "Checkout repository",
			Uses: "actions/checkout@v4",
		},
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

		if config.DestinationToken != "" {
			stepEnv["GH_TOKEN"] = config.DestinationToken
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

		if !config.Overwrite {
			// Add check to skip if secret already exists
			step.If = fmt.Sprintf("${{ env.SECRET_VALUE != '' }}")
		}

		steps = append(steps, step)
	}

	job := Job{
		RunsOn: config.RunnerLabel,
		Steps:  steps,
	}

	workflow.Jobs["migrate-secrets"] = job

	// Marshal to YAML
	yamlBytes, err := yaml.Marshal(&workflow)
	if err != nil {
		return "", fmt.Errorf("failed to marshal workflow to YAML: %w", err)
	}

	return string(yamlBytes), nil
}

// generateSecretMigrationScript generates the script to migrate a single secret
func generateSecretMigrationScript(config WorkflowConfig, srcName, destName string) string {
	var script strings.Builder

	// Check if secret value is empty
	script.WriteString("if [ -z \"$SECRET_VALUE\" ]; then\n")
	script.WriteString(fmt.Sprintf("  echo \"Secret %s is empty or does not exist, skipping...\"\n", srcName))
	script.WriteString("  exit 0\n")
	script.WriteString("fi\n\n")

	if !config.Overwrite {
		// Check if destination secret already exists
		script.WriteString("# Check if secret already exists at destination\n")
		if config.DestinationEnv != "" {
			script.WriteString(fmt.Sprintf("if gh secret list --env $DEST_ENV -R $DESTINATION | grep -q \"^%s\"; then\n", destName))
		} else {
			script.WriteString(fmt.Sprintf("if gh secret list -R $DESTINATION | grep -q \"^%s\"; then\n", destName))
		}
		script.WriteString(fmt.Sprintf("  echo \"Secret %s already exists at destination, skipping...\"\n", destName))
		script.WriteString("  exit 0\n")
		script.WriteString("fi\n\n")
	}

	// Set the secret at destination
	script.WriteString(fmt.Sprintf("# Set secret %s at destination\n", destName))
	script.WriteString("echo \"$SECRET_VALUE\" | \\\n")
	if config.DestinationEnv != "" {
		script.WriteString(fmt.Sprintf("  gh secret set %s --env $DEST_ENV -R $DESTINATION\n", destName))
	} else {
		script.WriteString(fmt.Sprintf("  gh secret set %s -R $DESTINATION\n", destName))
	}

	script.WriteString(fmt.Sprintf("echo \"Successfully migrated secret: %s -> %s\"\n", srcName, destName))

	return script.String()
}

// EncodeWorkflowContent encodes workflow content to base64 for GitHub API
func EncodeWorkflowContent(content string) string {
	return base64.StdEncoding.EncodeToString([]byte(content))
}
