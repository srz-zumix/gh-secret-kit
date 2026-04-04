package migrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

type planConfig struct {
	Source       string
	Destination  string
	RunnerLabel  string
	NoDeployKeys bool
	Overwrite    bool
	Unarchive    bool
}

// PlanEntry represents a single migration command with an optional comment listing secrets.
type PlanEntry struct {
	Comment string // secret names comment (may be empty)
	Cmd     string
}

// EnvPlanEntry represents a migration plan entry for an environment.
// Output rules:
//   - HasReviewers=true: all commands emitted as comments (reviewer names may not exist in dst org)
//   - HasReviewers=false, DstEnvExists=false: ExportImportCmd executable (creates env), MigrateAllCmd executable
//   - HasReviewers=false, DstEnvExists=true, Overwrite=false: ExportImportCmd commented out, EnvVariableCopyCmd executable, MigrateAllCmd executable
//   - HasReviewers=false, DstEnvExists=true, Overwrite=true: ExportImportCmd executable (handles vars), MigrateAllCmd executable
type EnvPlanEntry struct {
	SecretComment      string // # secrets: ... (may be empty)
	VariablesComment   string // # variables: ... (may be empty)
	ExportImportCmd    string // env export | import pipeline
	EnvVariableCopyCmd string // env variable copy command (non-empty when variables exist)
	MigrateAllCmd      string // migrate env all command
	HasReviewers       bool
	DstEnvExists       bool // true when the destination environment already exists
	Overwrite          bool // true when --overwrite was specified
}

// PlanResult represents the migration plan result
type PlanResult struct {
	RunnerSetup            string
	RepoMigrates           []PlanEntry
	EnvMigrates            []EnvPlanEntry
	OrgMigrate             PlanEntry
	RepoVariableCopies     []PlanEntry
	OrgVariableCopy        PlanEntry
	DeployKeyMigrates      []PlanEntry
	DstDeployKeySettingCmd string // non-empty when dst org has deploy keys disabled
	RunnerTeardown         string
}

// NewPlanCmd creates the migrate plan command
func NewPlanCmd() *cobra.Command {
	var config planConfig
	cmd := &cobra.Command{
		Use:   "plan [org]",
		Short: "Generate migration commands for matching repositories",
		Long: `Scan source organization for repositories with secrets, check if matching
repositories exist in the destination organization, and output the migration
commands for all matching pairs.

This command does not perform any migration; it only outputs the commands
that would be needed to migrate secrets from source to destination.

The output includes:
- runner setup command
- repo all commands for each matching repository with repository secrets
- env all commands for each matching repository/environment pair
- org all command if org secrets exist
- variable copy commands for each matching repository with repository variables
- variable copy command for the source organization if org variables exist
- runner teardown command

Arguments:
  [org]  Source organization name (e.g., org or HOST/org). Defaults to current repository owner.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Use first argument as source if provided
			if len(args) > 0 {
				config.Source = args[0]
			}
			return runPlan(context.Background(), &config)
		},
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination organization (e.g., org or HOST/org)")
	f.StringVar(&config.RunnerLabel, "runner-label", types.DefaultRunnerLabel, "Runner label for the workflow")
	f.BoolVar(&config.NoDeployKeys, "no-deploy-keys", false, "Skip deploy key scanning (avoids extra API calls per repository)")
	f.BoolVar(&config.Overwrite, "overwrite", false, "Add --overwrite to generated migration and copy commands that support it and make env export | env import pipelines executable for existing destination environments")
	f.BoolVar(&config.Unarchive, "unarchive", false, "Add --unarchive to generated migration commands")

	_ = cmd.MarkFlagRequired("dst")

	return cmd
}

func runPlan(ctx context.Context, config *planConfig) error {
	src, dst, err := migrator.ParseOrgPair(config.Source, config.Destination)
	if err != nil {
		return err
	}

	srcOrg := src.OwnerRepo.Owner
	dstOrg := dst.OwnerRepo.Owner

	logger.Info(fmt.Sprintf("Scanning source organization: %s", srcOrg))
	logger.Info(fmt.Sprintf("Checking against destination organization: %s", dstOrg))

	matches, err := migrator.ScanMatchingRepos(ctx, src, dst)
	if err != nil {
		return err
	}

	var result PlanResult

	orgArg := srcOrg
	if src.OwnerRepo.Host != "" {
		orgArg = fmt.Sprintf("%s/%s", src.OwnerRepo.Host, srcOrg)
	}

	runnerCmd := "gh secret-kit migrate runner"
	if config.RunnerLabel != "" && config.RunnerLabel != types.DefaultRunnerLabel {
		if orgArg != "" {
			result.RunnerSetup = fmt.Sprintf("%s setup --runner-label %s %s", runnerCmd, shellQuote(config.RunnerLabel), shellQuote(orgArg))
			result.RunnerTeardown = fmt.Sprintf("%s teardown --runner-label %s %s", runnerCmd, shellQuote(config.RunnerLabel), shellQuote(orgArg))
		} else {
			result.RunnerSetup = fmt.Sprintf("%s setup --runner-label %s", runnerCmd, shellQuote(config.RunnerLabel))
			result.RunnerTeardown = fmt.Sprintf("%s teardown --runner-label %s", runnerCmd, shellQuote(config.RunnerLabel))
		}
	} else {
		if orgArg != "" {
			result.RunnerSetup = fmt.Sprintf("%s setup %s", runnerCmd, shellQuote(orgArg))
			result.RunnerTeardown = fmt.Sprintf("%s teardown %s", runnerCmd, shellQuote(orgArg))
		} else {
			result.RunnerSetup = fmt.Sprintf("%s setup", runnerCmd)
			result.RunnerTeardown = fmt.Sprintf("%s teardown", runnerCmd)
		}
	}

	// Detect current repo for orgMigrationSrc preference
	var currentRepoName string
	if currentRepo, err := parser.Repository(); err == nil && currentRepo.Owner == srcOrg {
		currentRepoName = currentRepo.Name
	}

	// orgMigrationSrc is the source repo passed as -s to "migrate org all".
	// We store the full repository.Repository (not just its name) so the host
	// is preserved when building the command string.
	var orgMigrationSrc repository.Repository
	var orgMigrationSrcSet bool
	var orgSourceFixed bool // true once the current repo has been selected

	for _, m := range matches {
		// Update org migration source selection: prefer current repo (a), then first
		// matching repo regardless of secrets (b).
		if !orgMigrationSrcSet {
			orgMigrationSrc = m.SrcRepoRef
			orgMigrationSrcSet = true
		}
		if !orgSourceFixed && m.SrcName == currentRepoName {
			orgMigrationSrc = m.SrcRepoRef
			orgSourceFixed = true
		}

		if m.RepoSecretCount > 0 {
			cmd := buildRepoMigrateCmd(m.SrcRepoRef, m.DstRepoRef, m.RepoSecretNames, config)
			result.RepoMigrates = append(result.RepoMigrates, cmd)
			logger.Info(fmt.Sprintf("Found matching repo with secrets: %s (%d secrets)", m.SrcName, m.RepoSecretCount))
		}

		for _, env := range m.EnvMatches {
			cmd := buildEnvPlanEntry(m.SrcRepoRef, m.DstRepoRef, env.Name, env.SecretNames, env.VariableNames, env.HasReviewers, env.DstEnvExists, config)
			result.EnvMigrates = append(result.EnvMigrates, cmd)
			logger.Info(fmt.Sprintf("Found matching env: %s/%s (%d secrets, %d variables)", m.SrcName, env.Name, env.SecretCount, env.VariableCount))
		}

		if m.RepoVariableCount > 0 {
			cmd := buildRepoVariableCopyCmd(m.SrcRepoRef, m.DstRepoRef, m.RepoVariableNames, config)
			result.RepoVariableCopies = append(result.RepoVariableCopies, cmd)
			logger.Info(fmt.Sprintf("Found matching repo with variables: %s (%d variables)", m.SrcName, m.RepoVariableCount))
		}

		// Deploy key migration is only meaningful when src and dst are on different hosts.
		// Skipped when --no-deploy-keys is set to avoid extra API calls per repository.
		if !config.NoDeployKeys && m.SrcRepoRef.Host != m.DstRepoRef.Host {
			keys, err := gh.ListDeployKeys(ctx, src.Client, m.SrcRepoRef)
			if err != nil {
				logger.Warn(fmt.Sprintf("Skipping deploy keys for %s: %v", m.SrcName, err))
			} else if len(keys) > 0 {
				cmd := buildDeployKeyMigrateCmd(m.SrcRepoRef, m.DstRepoRef)
				result.DeployKeyMigrates = append(result.DeployKeyMigrates, cmd)
				logger.Info(fmt.Sprintf("Found matching repo with deploy keys: %s (%d keys)", m.SrcName, len(keys)))
			}
		}
	}

	// When there are deploy key migrations, check whether the destination org has deploy keys enabled.
	if len(result.DeployKeyMigrates) > 0 && result.DstDeployKeySettingCmd == "" {
		enabled, err := gh.GetOrgDeployKeysEnabled(ctx, dst.Client, dst.OwnerRepo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Could not check deploy keys setting for destination org: %v", err))
		} else if !enabled {
			dstOrgArg := dst.OwnerRepo.Owner
			if dst.OwnerRepo.Host != "" {
				dstOrgArg = dst.OwnerRepo.Host + "/" + dst.OwnerRepo.Owner
			}
			result.DstDeployKeySettingCmd = fmt.Sprintf("gh secret-kit deploy-key setting --set enable %s", shellQuote(dstOrgArg))
			logger.Info("Deploy keys are disabled in the destination organization")
		}
	}

	// Check org secrets independently of repo/env secrets.
	// A source repo is required to run the migration workflow, so skip only when
	// no repository in srcOrg has a counterpart in dstOrg.
	if orgMigrationSrcSet {
		srcOrgSecrets, err := gh.ListOrgSecrets(ctx, src.Client, src.OwnerRepo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Failed to list org secrets: %v", err))
		} else if len(srcOrgSecrets) > 0 {
			var orgSecretNames []string
			for _, s := range srcOrgSecrets {
				orgSecretNames = append(orgSecretNames, s.Name)
			}
			cmd := buildOrgMigrateCmd(orgMigrationSrc, dst.OwnerRepo, orgSecretNames, config)
			result.OrgMigrate = cmd
			logger.Info(fmt.Sprintf("Found org secrets: %d secrets", len(srcOrgSecrets)))
		}
	}

	// Org variables do not require a workflow source repository, so they are always scanned.
	srcOrgVariables, err := gh.ListOrgVariables(ctx, src.Client, src.OwnerRepo)
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to list org variables: %v", err))
	} else if len(srcOrgVariables) > 0 {
		var orgVariableNames []string
		for _, v := range srcOrgVariables {
			orgVariableNames = append(orgVariableNames, v.Name)
		}
		cmd := buildOrgVariableCopyCmd(src.OwnerRepo, dst.OwnerRepo, orgVariableNames, config)
		result.OrgVariableCopy = cmd
		logger.Info(fmt.Sprintf("Found org variables: %d variables", len(srcOrgVariables)))
	}

	// Output the plan
	printPlan(&result)

	return nil
}

const secretsCommentMaxWidth = 80

func secretsComment(names []string) string {
	if len(names) == 0 {
		return ""
	}
	const prefix = "# secrets: "
	const cont = "#          " // same width as prefix, aligned with first secret name

	var lines []string
	currentPrefix := prefix
	currentLen := len(prefix)
	var current []string

	for _, name := range names {
		needed := len(name)
		if len(current) > 0 {
			needed += 2 // ", " separator
		}
		if len(current) > 0 && currentLen+needed > secretsCommentMaxWidth {
			lines = append(lines, currentPrefix+strings.Join(current, ", "))
			current = current[:0]
			currentPrefix = cont
			currentLen = len(cont)
			needed = len(name)
		}
		current = append(current, name)
		currentLen += needed
	}
	if len(current) > 0 {
		lines = append(lines, currentPrefix+strings.Join(current, ", "))
	}
	return strings.Join(lines, "\n")
}

func buildRepoMigrateCmd(src, dst repository.Repository, secretNames []string, config *planConfig) PlanEntry {
	var parts []string
	parts = append(parts, "gh secret-kit migrate repo all")
	parts = append(parts, fmt.Sprintf("-s %s", shellQuote(repoArg(src))))
	parts = append(parts, fmt.Sprintf("-d %s", shellQuote(repoArg(dst))))
	if config.RunnerLabel != "" && config.RunnerLabel != types.DefaultRunnerLabel {
		parts = append(parts, fmt.Sprintf("--runner-label %s", shellQuote(config.RunnerLabel)))
	}
	if config.Overwrite {
		parts = append(parts, "--overwrite")
	}
	if config.Unarchive {
		parts = append(parts, "--unarchive")
	}
	return PlanEntry{Comment: secretsComment(secretNames), Cmd: strings.Join(parts, " ")}
}

func buildEnvPlanEntry(src, dst repository.Repository, envName string, secretNames []string, variableNames []string, hasReviewers bool, dstEnvExists bool, config *planConfig) EnvPlanEntry {
	// Build migrate env all command (handles secrets via workflow)
	var migrateParts []string
	migrateParts = append(migrateParts, "gh secret-kit migrate env all")
	migrateParts = append(migrateParts, fmt.Sprintf("-s %s", shellQuote(repoArg(src))))
	migrateParts = append(migrateParts, fmt.Sprintf("--src-env %s", shellQuote(envName)))
	migrateParts = append(migrateParts, fmt.Sprintf("-d %s", shellQuote(repoArg(dst))))
	migrateParts = append(migrateParts, fmt.Sprintf("--dst-env %s", shellQuote(envName)))
	if config.RunnerLabel != "" && config.RunnerLabel != types.DefaultRunnerLabel {
		migrateParts = append(migrateParts, fmt.Sprintf("--runner-label %s", shellQuote(config.RunnerLabel)))
	}
	if config.Overwrite {
		migrateParts = append(migrateParts, "--overwrite")
	}
	if config.Unarchive {
		migrateParts = append(migrateParts, "--unarchive")
	}

	// Build env variable copy command (used when destination env already exists and --overwrite is not set)
	var envVariableCopyCmd string
	if len(variableNames) > 0 {
		var varParts []string
		varParts = append(varParts, "gh secret-kit env variable copy")
		varParts = append(varParts, shellQuote(repoArg(dst)))
		varParts = append(varParts, fmt.Sprintf("--repo %s", shellQuote(repoArg(src))))
		varParts = append(varParts, fmt.Sprintf("--src-env %s", shellQuote(envName)))
		varParts = append(varParts, fmt.Sprintf("--dst-env %s", shellQuote(envName)))
		if config.Overwrite {
			varParts = append(varParts, "--overwrite")
		}
		envVariableCopyCmd = strings.Join(varParts, " ")
	}
	if config.Unarchive {
		migrateParts = append(migrateParts, "--unarchive")
	}

	// Build env export | import pipeline (handles settings and variables)
	exportImportCmd := fmt.Sprintf(
		"gh secret-kit env export --env %s -R %s | gh secret-kit env import - -R %s",
		shellQuote(envName),
		shellQuote(repoArg(src)),
		shellQuote(repoArg(dst)),
	)
	if config.Overwrite {
		exportImportCmd += " --overwrite"
	}

	return EnvPlanEntry{
		SecretComment:      secretsComment(secretNames),
		VariablesComment:   variablesComment(variableNames),
		ExportImportCmd:    exportImportCmd,
		EnvVariableCopyCmd: envVariableCopyCmd,
		MigrateAllCmd:      strings.Join(migrateParts, " "),
		HasReviewers:       hasReviewers,
		DstEnvExists:       dstEnvExists,
		Overwrite:          config.Overwrite,
	}
}

func buildOrgMigrateCmd(srcRepo repository.Repository, dstOrg repository.Repository, secretNames []string, config *planConfig) PlanEntry {
	var parts []string
	parts = append(parts, "gh secret-kit migrate org all")
	parts = append(parts, fmt.Sprintf("-s %s", shellQuote(repoArg(srcRepo))))
	dstOrgArg := dstOrg.Owner
	if dstOrg.Host != "" {
		dstOrgArg = dstOrg.Host + "/" + dstOrg.Owner
	}
	parts = append(parts, fmt.Sprintf("-d %s", shellQuote(dstOrgArg)))
	if config.RunnerLabel != "" && config.RunnerLabel != types.DefaultRunnerLabel {
		parts = append(parts, fmt.Sprintf("--runner-label %s", shellQuote(config.RunnerLabel)))
	}
	if config.Overwrite {
		parts = append(parts, "--overwrite")
	}
	if config.Unarchive {
		parts = append(parts, "--unarchive")
	}
	return PlanEntry{Comment: secretsComment(secretNames), Cmd: strings.Join(parts, " ")}
}

func variablesComment(names []string) string {
	if len(names) == 0 {
		return ""
	}
	const prefix = "# variables: "
	const cont = "#            " // aligned with first variable name

	var lines []string
	currentPrefix := prefix
	currentLen := len(prefix)
	var current []string

	for _, name := range names {
		needed := len(name)
		if len(current) > 0 {
			needed += 2 // ", " separator
		}
		if len(current) > 0 && currentLen+needed > secretsCommentMaxWidth {
			lines = append(lines, currentPrefix+strings.Join(current, ", "))
			current = current[:0]
			currentPrefix = cont
			currentLen = len(cont)
			needed = len(name)
		}
		current = append(current, name)
		currentLen += needed
	}
	if len(current) > 0 {
		lines = append(lines, currentPrefix+strings.Join(current, ", "))
	}
	return strings.Join(lines, "\n")
}

func buildDeployKeyMigrateCmd(src, dst repository.Repository) PlanEntry {
	var parts []string
	parts = append(parts, "gh secret-kit deploy-key migrate")
	parts = append(parts, fmt.Sprintf("--repo %s", shellQuote(repoArg(src))))
	parts = append(parts, shellQuote(repoArg(dst)))
	return PlanEntry{Cmd: strings.Join(parts, " ")}
}

func buildRepoVariableCopyCmd(src, dst repository.Repository, varNames []string, config *planConfig) PlanEntry {
	var parts []string
	parts = append(parts, "gh secret-kit variable copy")
	parts = append(parts, shellQuote(repoArg(dst)))
	parts = append(parts, fmt.Sprintf("--repo %s", shellQuote(repoArg(src))))
	if config.Overwrite {
		parts = append(parts, "--overwrite")
	}
	return PlanEntry{Comment: variablesComment(varNames), Cmd: strings.Join(parts, " ")}
}

func buildOrgVariableCopyCmd(srcOrg, dstOrg repository.Repository, varNames []string, config *planConfig) PlanEntry {
	var parts []string
	parts = append(parts, "gh secret-kit variable copy")
	dstOrgArg := dstOrg.Owner
	if dstOrg.Host != "" {
		dstOrgArg = dstOrg.Host + "/" + dstOrg.Owner
	}
	parts = append(parts, shellQuote(dstOrgArg))
	srcOrgArg := srcOrg.Owner
	if srcOrg.Host != "" {
		srcOrgArg = srcOrg.Host + "/" + srcOrg.Owner
	}
	parts = append(parts, fmt.Sprintf("--owner %s", shellQuote(srcOrgArg)))
	if config.Overwrite {
		parts = append(parts, "--overwrite")
	}
	return PlanEntry{Comment: variablesComment(varNames), Cmd: strings.Join(parts, " ")}
}

func printPlan(result *PlanResult) {
	if len(result.RepoMigrates) == 0 && len(result.EnvMigrates) == 0 && result.OrgMigrate.Cmd == "" &&
		len(result.RepoVariableCopies) == 0 && result.OrgVariableCopy.Cmd == "" && len(result.DeployKeyMigrates) == 0 {
		fmt.Println("# No matching repositories or environments found for migration")
		return
	}

	fmt.Println("#!/bin/bash")
	fmt.Println("set -e")
	fmt.Println()

	fmt.Println("# Runner setup (run in a SEPARATE terminal, keeps listening until Ctrl+C)")
	fmt.Println("# " + result.RunnerSetup)
	fmt.Println()

	if len(result.RepoMigrates) > 0 {
		fmt.Println("# Repository secret migrations")
		for _, entry := range result.RepoMigrates {
			if entry.Comment != "" {
				fmt.Println(entry.Comment)
			}
			fmt.Println(entry.Cmd)
		}
		fmt.Println()
	}

	if len(result.EnvMigrates) > 0 {
		fmt.Println("# Environment secret migrations")
		for _, entry := range result.EnvMigrates {
			switch {
			case entry.HasReviewers:
				// Reviewer names may not exist in the destination org – emit everything as
				// comments so the operator can manually adjust before running.
				fmt.Println("# NOTE: environment has reviewers - manual resolution required")
				if entry.SecretComment != "" {
					fmt.Println(entry.SecretComment)
				}
				fmt.Println("# " + entry.ExportImportCmd)
				fmt.Println("# " + entry.MigrateAllCmd)
			case !entry.DstEnvExists:
				// Destination environment does not exist: export|import creates it first,
				// then migrate env all can migrate secrets.
				if entry.SecretComment != "" {
					fmt.Println(entry.SecretComment)
				}
				fmt.Println(entry.ExportImportCmd)
				fmt.Println(entry.MigrateAllCmd)
			default:
				// Destination environment already exists: comment out export|import to avoid
				// overwriting existing settings, unless --overwrite was specified in which
				// case export|import (with --overwrite) handles variables too.
				if entry.SecretComment != "" {
					fmt.Println(entry.SecretComment)
				}
				if entry.Overwrite {
					fmt.Println(entry.ExportImportCmd)
				} else {
					fmt.Println("# " + entry.ExportImportCmd)
					if entry.VariablesComment != "" {
						fmt.Println(entry.VariablesComment)
					}
					if entry.EnvVariableCopyCmd != "" {
						fmt.Println(entry.EnvVariableCopyCmd)
					}
				}
				fmt.Println(entry.MigrateAllCmd)
			}
		}
		fmt.Println()
	}

	if result.OrgMigrate.Cmd != "" {
		fmt.Println("# Organization secret migration")
		if result.OrgMigrate.Comment != "" {
			fmt.Println(result.OrgMigrate.Comment)
		}
		fmt.Println(result.OrgMigrate.Cmd)
		fmt.Println()
	}

	if len(result.RepoVariableCopies) > 0 {
		fmt.Println("# Repository variable copies")
		for _, entry := range result.RepoVariableCopies {
			if entry.Comment != "" {
				fmt.Println(entry.Comment)
			}
			fmt.Println(entry.Cmd)
		}
		fmt.Println()
	}

	if len(result.DeployKeyMigrates) > 0 {
		fmt.Println("# Deploy key migrations (cross-host only)")
		if result.DstDeployKeySettingCmd != "" {
			fmt.Println("# NOTE: deploy keys are disabled in the destination org - enable before migrating")
			fmt.Println(result.DstDeployKeySettingCmd)
		}
		for _, entry := range result.DeployKeyMigrates {
			fmt.Println(entry.Cmd)
		}
		fmt.Println()
	}

	if result.OrgVariableCopy.Cmd != "" {
		fmt.Println("# Organization variable copy")
		if result.OrgVariableCopy.Comment != "" {
			fmt.Println(result.OrgVariableCopy.Comment)
		}
		fmt.Println(result.OrgVariableCopy.Cmd)
		fmt.Println()
	}

	fmt.Println("# Runner teardown (run after all migrations complete, or Ctrl+C the setup terminal)")
	fmt.Println("# " + result.RunnerTeardown)
}
