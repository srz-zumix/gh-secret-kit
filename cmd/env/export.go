package env

import (
	"fmt"
	"os"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/pkg/config"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

// NewExportCmd creates the env export command
func NewExportCmd() *cobra.Command {
	var repo, output, envName, format string
	var exporter cmdutil.Exporter

	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export GitHub Actions environment configurations to a file",
		Long: `Export GitHub Actions environment configurations (settings, deployment branch policies, and variables)
to a YAML or JSON file, or to stdout.

The repository is specified via --repo (defaults to the current repository).
Use --env to export a specific environment; omit it to export all environments.
Use --output to write to a file; omit it to write to stdout.

Note: Secrets cannot be exported because their values are not accessible via the GitHub API.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			r, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to parse repository: %w", err)
			}

			cfgExporter, err := config.NewExporter(r)
			if err != nil {
				return err
			}

			envCfgs, err := cfgExporter.Export(config.ExportOptions{EnvName: envName})
			if err != nil {
				return err
			}
			return writeEnvConfigs(envCfgs, output, exporter)
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Source repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&envName, "env", "", "Environment name to export (defaults to all environments)")
	f.StringVarP(&output, "output", "o", "", "Output file path (defaults to stdout)")

	_ = cmdflags.AddFormatFlags(cmd, &exporter, &format, "", []string{"yaml"})

	return cmd
}

func writeEnvConfigs(cfgs []*config.EnvironmentConfig, output string, exporter cmdutil.Exporter) error {
	if exporter != nil {
		renderer := render.NewRenderer(exporter)
		return renderer.RenderExportedData(cfgs)
	}

	if output != "" {
		if err := config.WriteEnvironmentConfigsToFile(cfgs, output); err != nil {
			return fmt.Errorf("failed to write environment configs to %q: %w", output, err)
		}
		fmt.Fprintf(os.Stderr, "Exported %d environment(s) to %s\n", len(cfgs), output)
		return nil
	}

	return config.WriteEnvironmentConfigs(cfgs, os.Stdout)
}
