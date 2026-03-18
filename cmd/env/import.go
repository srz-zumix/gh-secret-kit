package env

import (
	"fmt"
	"os"

	"github.com/cli/cli/v2/pkg/cmdutil"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/config"
	"github.com/srz-zumix/go-gh-extension/pkg/cmdflags"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

type ImportOptions struct {
	Exporter cmdutil.Exporter
}

// NewImportCmd creates the env import command
func NewImportCmd() *cobra.Command {
	var repo, dstEnv, format string
	var overwrite, dryrun bool
	var opts ImportOptions

	cmd := &cobra.Command{
		Use:   "import <input>",
		Short: "Import a GitHub Actions environment configuration from a file",
		Long: `Read and apply a GitHub Actions environment configuration (settings, deployment branch
policies, and variables) from a YAML file or stdin.

The repository is specified via --repo (defaults to the current repository).
If --env is set, only environments whose name matches the value are imported; if omitted, all environments in the config are considered and their names are used as-is (environments are not renamed).
Specify "-" as input to read from stdin.
Use --dryrun to preview what would be applied without making any changes.
Use --format to specify the output format (yaml or json; default: yaml).

Note: Secrets are not included in the import because their values are not accessible via the GitHub API.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			input := args[0]

			cfgs, err := config.ReadEnvironmentConfigs(input)
			if err != nil {
				return fmt.Errorf("failed to read environment config: %w", err)
			}

			r, err := parser.Repository(parser.RepositoryInput(repo))
			if err != nil {
				return fmt.Errorf("failed to parse repository: %w", err)
			}

			cfgImporter, err := config.NewImporter(r)
			if err != nil {
				return err
			}

			importOpts := config.ImportOptions{
				TargetEnv: dstEnv,
				Overwrite: overwrite,
				DryRun:    dryrun,
			}
			imported, err := cfgImporter.Import(cfgs, importOpts)
			if err != nil {
				return fmt.Errorf("failed to import environment config: %w", err)
			}

			renderer := render.NewRenderer(opts.Exporter)
			if opts.Exporter != nil {
				return renderer.RenderExportedData(imported)
			}
			if format == "yaml" {
				if err := config.WriteEnvironmentConfigs(imported, os.Stdout); err != nil {
					return fmt.Errorf("error writing environment config: %w", err)
				}
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&repo, "repo", "R", "", "Destination repository (e.g., owner/repo; defaults to current repository)")
	f.StringVar(&dstEnv, "env", "", "Filter environments to import by name; if empty, all environments in the config file are considered (does not rename environments)")
	f.BoolVar(&overwrite, "overwrite", false, "Overwrite existing variables at destination (default: false)")
	f.BoolVarP(&dryrun, "dryrun", "n", false, "Preview changes without applying them")

	cmdutil.AddFormatFlags(cmd, &opts.Exporter)
	cmdflags.SetupFormatFlagWithNonJSONFormats(cmd, &opts.Exporter, &format, "", []string{"yaml"})

	return cmd
}
