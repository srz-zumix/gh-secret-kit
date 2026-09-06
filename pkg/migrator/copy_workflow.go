package migrator

import (
	"fmt"
)

// CopyDestination describes a single destination of a secret copy.
type CopyDestination struct {
	// Target is OWNER/REPO, or ORG when the scope is SecretScopeOrg.
	Target string
	// Host is the GitHub host of the destination (e.g. github.com).
	Host string
	// Env is the destination environment name for SecretScopeEnv.
	Env string
	// TokenSecret is the name of the source repository secret holding the token
	// for Host.
	TokenSecret string
}

// CopyWorkflowConfig holds configuration for generating a secret copy workflow.
type CopyWorkflowConfig struct {
	WorkflowName string
	// RunsOn is the job runs-on value. It accepts any YAML value (string or list).
	RunsOn any
	Scope  SecretScope
	// DestinationApp selects the destination secret store. Empty means actions.
	DestinationApp SecretApp
	SourceEnv      string
	Secrets        []string
	Rename         map[string]string // OLD_NAME -> NEW_NAME
	Overwrite      bool
	Destinations   []CopyDestination
}

// GenerateCopyWorkflowYAML generates a workflow_dispatch-triggered GitHub
// Actions workflow that copies every configured secret to every destination.
// Each destination gets its own token and host, so both are set per step
// instead of at job level. The temporary branch, token secrets, and run history
// are cleaned up by the CLI, so the workflow needs no write permission.
func GenerateCopyWorkflowYAML(config CopyWorkflowConfig) (string, error) {
	if len(config.Destinations) == 0 {
		return "", fmt.Errorf("no destination specified")
	}
	if len(config.Secrets) == 0 {
		return "", fmt.Errorf("no secret specified")
	}
	if err := ValidateSecretApp(config.DestinationApp); err != nil {
		return "", err
	}
	if err := validateSecretNames(config.Secrets, config.Rename); err != nil {
		return "", err
	}

	workflow := WorkflowYAML{
		Name: config.WorkflowName,
		On: map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		Permissions: map[string]string{"contents": "read"},
		Jobs:        make(map[string]Job),
	}

	var steps []Step
	for _, dest := range config.Destinations {
		host := dest.Host
		if host == "" {
			host = "github.com"
		}
		for _, secretName := range config.Secrets {
			destSecretName := secretName
			if newName, ok := config.Rename[secretName]; ok {
				destSecretName = newName
			}

			stepEnv := map[string]string{
				"SECRET_VALUE": fmt.Sprintf("${{ secrets.%s }}", secretName),
				"SECRET_NAME":  destSecretName,
				"DESTINATION":  dest.Target,
				"GH_HOST":      host,
			}
			if dest.TokenSecret != "" {
				secretRef := fmt.Sprintf("${{ secrets.%s }}", dest.TokenSecret)
				if host == "github.com" {
					stepEnv["GH_TOKEN"] = secretRef
				} else {
					stepEnv["GH_ENTERPRISE_TOKEN"] = secretRef
				}
			}
			if dest.Env != "" {
				stepEnv["DEST_ENV"] = dest.Env
			}

			steps = append(steps, Step{
				Name: fmt.Sprintf("Copy secret to %s: %s", dest.Target, secretName),
				Run: generateSecretMigrationScript(secretScriptConfig{
					Scope:          config.Scope,
					DestinationApp: config.DestinationApp,
					DestinationEnv: dest.Env,
					Overwrite:      config.Overwrite,
				}, secretName, destSecretName),
				Env: stepEnv,
			})
		}
	}

	workflow.Jobs["copy-secrets"] = Job{
		RunsOn: config.RunsOn,
		// Environment secrets are only exposed to a job bound to that environment.
		Environment: config.SourceEnv,
		Steps:       steps,
	}

	return marshalWorkflow(&workflow)
}
