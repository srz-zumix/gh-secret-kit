package migrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

type planConfig struct {
	Source      string
	Destination string
	RunnerLabel string
}

// PlanResult represents the migration plan result
type PlanResult struct {
	RunnerSetup   string
	RepoMigrates  []string
	EnvMigrates   []string
	OrgMigrate    string
	RunnerTeardown string
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

	_ = cmd.MarkFlagRequired("dst")

	return cmd
}

func runPlan(ctx context.Context, config *planConfig) error {
	// Parse source organization
	srcOwnerRepo, err := parser.Repository(parser.RepositoryOwnerWithHost(config.Source))
	if err != nil {
		return fmt.Errorf("failed to parse source organization: %w", err)
	}
	srcOrg := srcOwnerRepo.Owner

	// Parse destination organization
	dstOwnerRepo, err := parser.Repository(parser.RepositoryOwnerWithHost(config.Destination))
	if err != nil {
		return fmt.Errorf("failed to parse destination organization: %w", err)
	}
	if dstOwnerRepo.Host == "" && srcOwnerRepo.Host != "" {
		dstOwnerRepo.Host = srcOwnerRepo.Host
	}
	dstOrg := dstOwnerRepo.Owner

	// Create clients
	srcClient, err := gh.NewGitHubClientWithRepo(srcOwnerRepo)
	if err != nil {
		return fmt.Errorf("failed to create source GitHub client: %w", err)
	}

	dstClient := srcClient
	if dstOwnerRepo.Host != srcOwnerRepo.Host {
		dstClient, err = gh.NewGitHubClientWithRepo(dstOwnerRepo)
		if err != nil {
			return fmt.Errorf("failed to create destination GitHub client: %w", err)
		}
	}

	logger.Info(fmt.Sprintf("Scanning source organization: %s", srcOrg))
	logger.Info(fmt.Sprintf("Checking against destination organization: %s", dstOrg))

	// Get source repositories with secrets
	srcRepos, err := gh.ListOwnerRepositories(ctx, srcClient, srcOrg)
	if err != nil {
		return fmt.Errorf("failed to list source repositories: %w", err)
	}

	logger.Info(fmt.Sprintf("Found %d repositories in source, scanning for secrets...", len(srcRepos)))

	var result PlanResult

	orgArg := srcOrg
	if srcOwnerRepo.Host != "" {
		orgArg = fmt.Sprintf("%s/%s", srcOwnerRepo.Host, srcOrg)
	}

	runnerCmd := fmt.Sprintf("gh secret-kit migrate runner %s", shellQuote(orgArg))
	if config.RunnerLabel != "" && config.RunnerLabel != types.DefaultRunnerLabel {
		result.RunnerSetup = fmt.Sprintf("%s setup --runner-label %s", runnerCmd, shellQuote(config.RunnerLabel))
		result.RunnerTeardown = fmt.Sprintf("%s teardown --runner-label %s", runnerCmd, shellQuote(config.RunnerLabel))
	} else {
		result.RunnerSetup = fmt.Sprintf("%s setup", runnerCmd)
		result.RunnerTeardown = fmt.Sprintf("%s teardown", runnerCmd)
	}
	// Try to identify the current repository as the preferred -s for "migrate org all".
	// If the current repo belongs to srcOrg and has a matching destination repo, it is
	// used as the org migration source (priority a). Otherwise the first repository in
	// srcOrg that has a matching destination repo is used (priority b).
	var currentRepoName string
	if currentRepo, err := parser.Repository(); err == nil && currentRepo.Owner == srcOrg {
		currentRepoName = currentRepo.Name
	}

	// orgMigrationSrc is the "owner/repo" string passed as -s to "migrate org all".
	var orgMigrationSrc string
	var orgSourceFixed bool // true once the current repo has been selected

	for _, srcRepo := range srcRepos {
		if srcRepo.GetFullName() == "" {
			continue
		}

		repoName := srcRepo.GetName()
		srcRepoRef, err := parser.Repository(parser.RepositoryInput(srcRepo.GetFullName()))
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping %s: failed to parse repository: %v", srcRepo.GetFullName(), err))
			continue
		}

		// Check if destination repository exists
		dstRepoRef := repository.Repository{
			Owner: dstOrg,
			Name:  repoName,
			Host:  dstOwnerRepo.Host,
		}
		dstRepoInfo, dstErr := gh.GetRepository(ctx, dstClient, dstRepoRef)
		if dstErr != nil {
			logger.Debug(fmt.Sprintf("Skipping %s: no matching repository in destination", repoName))
			continue
		}

		// Update org migration source selection: prefer current repo (a), then first
		// matching repo regardless of secrets (b).
		if orgMigrationSrc == "" {
			orgMigrationSrc = srcRepo.GetFullName()
		}
		if !orgSourceFixed && repoName == currentRepoName {
			orgMigrationSrc = srcRepo.GetFullName()
			orgSourceFixed = true
		}

		// Get source repo secrets
		secrets, err := gh.ListRepoSecrets(ctx, srcClient, srcRepoRef)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping %s: failed to list secrets: %v", srcRepo.GetFullName(), err))
			continue
		}

		// Get source env secrets
		envSecrets, err := gh.CollectEnvSecrets(ctx, srcClient, srcRepo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping environments for %s: %v", srcRepo.GetFullName(), err))
		}

		// Generate repo migration command if repo has secrets
		if len(secrets) > 0 {
			cmd := buildRepoMigrateCmd(srcRepoRef, dstRepoRef, config)
			result.RepoMigrates = append(result.RepoMigrates, cmd)
			logger.Info(fmt.Sprintf("Found matching repo with secrets: %s (%d secrets)", repoName, len(secrets)))
		}

		// Generate env migration commands
		if len(envSecrets) > 0 {
			dstEnvSecrets, err := gh.CollectEnvSecrets(ctx, dstClient, dstRepoInfo)
			if err != nil {
				logger.Warn(fmt.Sprintf("Skipping env check for %s: %v", repoName, err))
			}

			for envName, srcEnvSecs := range envSecrets {
				// Check if destination has the same environment
				if _, exists := dstEnvSecrets[envName]; exists {
					cmd := buildEnvMigrateCmd(srcRepoRef, dstRepoRef, envName, config)
					result.EnvMigrates = append(result.EnvMigrates, cmd)
					logger.Info(fmt.Sprintf("Found matching env with secrets: %s/%s (%d secrets)", repoName, envName, len(srcEnvSecs)))
				} else {
					logger.Debug(fmt.Sprintf("Skipping env %s/%s: no matching environment in destination", repoName, envName))
				}
			}
		}
	}

	// Check org secrets independently of repo/env secrets.
	// A source repo is required to run the migration workflow, so skip only when
	// no repository in srcOrg has a counterpart in dstOrg.
	if orgMigrationSrc != "" {
		srcOrgSecrets, err := gh.ListOrgSecrets(ctx, srcClient, srcOwnerRepo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Failed to list org secrets: %v", err))
		} else if len(srcOrgSecrets) > 0 {
			orgSrcRepo, _ := parser.Repository(parser.RepositoryInput(orgMigrationSrc))
			cmd := buildOrgMigrateCmd(orgSrcRepo, dstOrg, dstOwnerRepo.Host, config)
			result.OrgMigrate = cmd
			logger.Info(fmt.Sprintf("Found org secrets: %d secrets", len(srcOrgSecrets)))
		}
	}

	// Output the plan
	printPlan(&result)

	return nil
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes,
// so the value is safe to embed in a POSIX shell script regardless of spaces
// or special characters.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func buildRepoMigrateCmd(src, dst repository.Repository, config *planConfig) string {
	var parts []string
	parts = append(parts, "gh secret-kit migrate repo all")
	parts = append(parts, fmt.Sprintf("-s %s", shellQuote(src.Owner+"/"+src.Name)))
	parts = append(parts, fmt.Sprintf("-d %s", shellQuote(dst.Owner+"/"+dst.Name)))
	if dst.Host != "" && dst.Host != src.Host {
		parts = append(parts, fmt.Sprintf("--dst-host %s", shellQuote(dst.Host)))
	}
	if config.RunnerLabel != "" && config.RunnerLabel != types.DefaultRunnerLabel {
		parts = append(parts, fmt.Sprintf("--runner-label %s", shellQuote(config.RunnerLabel)))
	}
	return strings.Join(parts, " ")
}

func buildEnvMigrateCmd(src, dst repository.Repository, envName string, config *planConfig) string {
	var parts []string
	parts = append(parts, "gh secret-kit migrate env all")
	parts = append(parts, fmt.Sprintf("-s %s", shellQuote(src.Owner+"/"+src.Name)))
	parts = append(parts, fmt.Sprintf("--src-env %s", shellQuote(envName)))
	parts = append(parts, fmt.Sprintf("-d %s", shellQuote(dst.Owner+"/"+dst.Name)))
	parts = append(parts, fmt.Sprintf("--dst-env %s", shellQuote(envName)))
	if dst.Host != "" && dst.Host != src.Host {
		parts = append(parts, fmt.Sprintf("--dst-host %s", shellQuote(dst.Host)))
	}
	if config.RunnerLabel != "" && config.RunnerLabel != types.DefaultRunnerLabel {
		parts = append(parts, fmt.Sprintf("--runner-label %s", shellQuote(config.RunnerLabel)))
	}
	return strings.Join(parts, " ")
}

func buildOrgMigrateCmd(srcRepo repository.Repository, dstOrg string, dstHost string, config *planConfig) string {
	var parts []string
	parts = append(parts, "gh secret-kit migrate org all")
	parts = append(parts, fmt.Sprintf("-s %s", shellQuote(srcRepo.Owner+"/"+srcRepo.Name)))
	parts = append(parts, fmt.Sprintf("-d %s", shellQuote(dstOrg)))
	if dstHost != "" && dstHost != srcRepo.Host {
		parts = append(parts, fmt.Sprintf("--dst-host %s", shellQuote(dstHost)))
	}
	if config.RunnerLabel != "" && config.RunnerLabel != types.DefaultRunnerLabel {
		parts = append(parts, fmt.Sprintf("--runner-label %s", shellQuote(config.RunnerLabel)))
	}
	return strings.Join(parts, " ")
}

func printPlan(result *PlanResult) {
	if len(result.RepoMigrates) == 0 && len(result.EnvMigrates) == 0 && result.OrgMigrate == "" {
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
		for _, cmd := range result.RepoMigrates {
			fmt.Println(cmd)
		}
		fmt.Println()
	}

	if len(result.EnvMigrates) > 0 {
		fmt.Println("# Environment secret migrations")
		for _, cmd := range result.EnvMigrates {
			fmt.Println(cmd)
		}
		fmt.Println()
	}

	if result.OrgMigrate != "" {
		fmt.Println("# Organization secret migration")
		fmt.Println(result.OrgMigrate)
		fmt.Println()
	}

	fmt.Println("# Runner teardown (run after all migrations complete, or Ctrl+C the setup terminal)")
	fmt.Println("# " + result.RunnerTeardown)
}
