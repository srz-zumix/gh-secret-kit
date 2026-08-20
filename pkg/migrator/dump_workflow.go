package migrator

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// DumpWorkflowConfig holds configuration for generating the (undocumented)
// secret dump workflow.
type DumpWorkflowConfig struct {
	WorkflowName string
	Secrets      []string
	// Output is the path (relative to the runner's working directory, or
	// absolute) of the file to write NAME=BASE64_VALUE lines to.
	Output string
	// RunsOn overrides the job runs-on value. It accepts any YAML value
	// (string or list) so the original runner setting can be reused verbatim.
	RunsOn any
	// CleanupBranch makes the generated workflow delete the given branch on
	// the source repository after a successful run.
	CleanupBranch string
}

// GenerateDumpWorkflowYAML generates a workflow_dispatch-triggered GitHub
// Actions workflow that writes each configured secret's value to Output on
// the runner, one "NAME=BASE64_VALUE" line per secret. The output file is
// truncated and recreated with mode 0600 on every run.
func GenerateDumpWorkflowYAML(config DumpWorkflowConfig) (string, error) {
	workflow := WorkflowYAML{
		Name: config.WorkflowName,
		On: map[string]any{
			"workflow_dispatch": map[string]any{},
		},
		// The cleanup step deletes the temporary dispatch branch using
		// github.token, which requires write access to contents.
		Permissions: map[string]string{
			"contents": "write",
		},
		Jobs: make(map[string]Job),
	}

	steps := []Step{
		{
			Name: "Initialize dump file",
			Run:  generateInitDumpFileScript(config.Output),
			Env: map[string]string{
				"OUTPUT_FILE": config.Output,
			},
		},
	}

	for _, secretName := range config.Secrets {
		steps = append(steps, Step{
			Name: fmt.Sprintf("Dump secret: %s", secretName),
			Run:  generateDumpSecretScript(secretName),
			Env: map[string]string{
				"SECRET_VALUE": fmt.Sprintf("${{ secrets.%s }}", secretName),
				"SECRET_NAME":  secretName,
				"OUTPUT_FILE":  config.Output,
			},
		})
	}

	if config.CleanupBranch != "" {
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
		RunsOn: config.RunsOn,
		Steps:  steps,
	}

	workflow.Jobs["dump-secrets"] = job

	var buf strings.Builder
	encoder := yaml.NewEncoder(&buf)
	encoder.SetIndent(2)
	if err := encoder.Encode(&workflow); err != nil {
		return "", fmt.Errorf("failed to marshal workflow to YAML: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return "", fmt.Errorf("failed to close YAML encoder: %w", err)
	}

	// "on" is a YAML reserved keyword (boolean true), so the marshaler quotes it.
	// Replace the quoted key with the unquoted form for valid GitHub Actions syntax.
	result := strings.Replace(buf.String(), "\"on\":", "on:", 1)

	return result, nil
}

// generateInitDumpFileScript generates the script that creates the parent
// directory of OUTPUT_FILE (if needed) and truncates/recreates it with mode
// 0600, so every run starts from an empty file regardless of prior contents.
func generateInitDumpFileScript(output string) string {
	var script strings.Builder
	script.WriteString("mkdir -p \"$(dirname \"${OUTPUT_FILE}\")\"\n")
	script.WriteString("umask 077\n")
	script.WriteString(": > \"${OUTPUT_FILE}\"\n")
	script.WriteString("chmod 600 \"${OUTPUT_FILE}\"\n")
	fmt.Fprintf(&script, "echo \"Initialized dump file: %s\"\n", output)
	return script.String()
}

// generateDumpSecretScript generates the script that appends a single
// "NAME=BASE64_VALUE" line for one secret to OUTPUT_FILE. The value is
// base64-encoded so that GitHub Actions' secret masking (which would
// otherwise replace the raw value with "***" in any log output) does not
// corrupt the written value, and so that embedded newlines or other special
// characters in the secret survive round-tripping through the line-based
// file format.
func generateDumpSecretScript(srcName string) string {
	var script strings.Builder
	script.WriteString("if [ -z \"${SECRET_VALUE}\" ]; then\n")
	fmt.Fprintf(&script, "  echo \"Secret %s is empty or does not exist, skipping...\"\n", srcName)
	script.WriteString("else\n")
	script.WriteString("  encoded=$(printf '%s' \"${SECRET_VALUE}\" | base64 | tr -d '\\n')\n")
	script.WriteString("  echo \"${SECRET_NAME}=${encoded}\" >> \"${OUTPUT_FILE}\"\n")
	fmt.Fprintf(&script, "  echo \"Dumped secret: %s\"\n", srcName)
	script.WriteString("fi\n")
	return script.String()
}
