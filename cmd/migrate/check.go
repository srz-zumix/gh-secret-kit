package migrate

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/workflow"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

type checkConfig struct {
	Source      string
	Destination string
}

// CheckResult represents the check result for one target
type CheckResult struct {
	Scope   string
	Source  string
	Dest    string
	Env     string
	Err     error
}

// NewCheckCmd creates the migrate check command
func NewCheckCmd() *cobra.Command {
	var config checkConfig
	cmd := &cobra.Command{
		Use:   "check [org]",
		Short: "Check migration status for all plan targets",
		Long: `Scan the source and destination organizations, identify matching repository
and environment pairs that have secrets, and run the migration check for each.

This command verifies whether secrets from the source have been successfully
migrated to the destination. It checks:
- Repository secrets for all matching repositories
- Environment secrets for all matching environments
- Organization secrets (if any)

The output shows each check result. Exits with a non-zero status if any
secrets have not been migrated yet.

Arguments:
  [org]  Source organization name (e.g., org or HOST/org). Defaults to current repository owner.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				config.Source = args[0]
			}
			return runCheck(context.Background(), &config)
		},
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&config.Destination, "dst", "d", "", "Destination organization (e.g., org or HOST/org)")

	_ = cmd.MarkFlagRequired("dst")

	return cmd
}

func runCheck(ctx context.Context, config *checkConfig) error {
	src, dst, err := migrate.ParseOrgPair(config.Source, config.Destination)
	if err != nil {
		return err
	}

	logger.Info(fmt.Sprintf("Scanning source organization: %s", src.OwnerRepo.Owner))
	logger.Info(fmt.Sprintf("Checking against destination organization: %s", dst.OwnerRepo.Owner))

	matches, err := migrate.ScanMatchingRepos(ctx, src, dst)
	if err != nil {
		return err
	}

	var results []CheckResult

	for _, m := range matches {
		if m.RepoSecretCount > 0 {
			logger.Info(fmt.Sprintf("Checking repo secrets: %s (%d secrets)", m.SrcName, m.RepoSecretCount))
			checkCfg := &workflow.CheckConfig{
				Source:          repoArg(m.SrcRepoRef),
				Destination:     repoArg(m.DstRepoRef),
				Scope:           migrate.SecretScopeRepo,
			}
			cerr := workflow.RunCheck(ctx, checkCfg)
			results = append(results, CheckResult{
				Scope:  "repo",
				Source: m.SrcFullName,
				Dest:   fmt.Sprintf("%s/%s", m.DstRepoRef.Owner, m.DstRepoRef.Name),
				Err:    cerr,
			})
		}

		for _, env := range m.EnvMatches {
			logger.Info(fmt.Sprintf("Checking env secrets: %s/%s (%d secrets)", m.SrcName, env.Name, env.SecretCount))
			checkCfg := &workflow.CheckConfig{
				Source:          repoArg(m.SrcRepoRef),
					Destination:     repoArg(m.DstRepoRef),
				SourceEnv:       env.Name,
				DestinationEnv:  env.Name,
				Scope:           migrate.SecretScopeEnv,
			}
			cerr := workflow.RunCheck(ctx, checkCfg)
			results = append(results, CheckResult{
				Scope:  "env",
				Source: m.SrcFullName,
				Dest:   fmt.Sprintf("%s/%s", m.DstRepoRef.Owner, m.DstRepoRef.Name),
				Env:    env.Name,
				Err:    cerr,
			})
		}
	}

	// Check org secrets
	srcOrgSecrets, err := gh.ListOrgSecrets(ctx, src.Client, src.OwnerRepo)
	if err != nil {
		logger.Warn(fmt.Sprintf("Failed to list org secrets: %v", err))
	} else if len(srcOrgSecrets) > 0 {
		srcOrg := src.OwnerRepo.Owner
		logger.Info(fmt.Sprintf("Checking org secrets: %s (%d secrets)", srcOrg, len(srcOrgSecrets)))
		dstOrgArg := dst.OwnerRepo.Owner
		if dst.OwnerRepo.Host != "" && dst.OwnerRepo.Host != src.OwnerRepo.Host {
			dstOrgArg = dst.OwnerRepo.Host + "/" + dst.OwnerRepo.Owner
		}
		checkCfg := &workflow.CheckConfig{
			Source:      srcOrg,
			Destination: dstOrgArg,
			Scope:       migrate.SecretScopeOrg,
		}
		if src.OwnerRepo.Host != "" {
			checkCfg.Source = fmt.Sprintf("%s/%s", src.OwnerRepo.Host, srcOrg)
		}
		cerr := workflow.RunCheck(ctx, checkCfg)
		results = append(results, CheckResult{
			Scope:  "org",
			Source: srcOrg,
			Dest:   dst.OwnerRepo.Owner,
			Err:    cerr,
		})
	}

	// Print summary
	if len(results) == 0 {
		fmt.Println("No matching repositories, environments, or org secrets found to check")
		return nil
	}

	fmt.Println()
	fmt.Println("=== Migration Check Summary ===")
	failCount := 0
	for _, r := range results {
		label := r.Scope + ": " + r.Source + " -> " + r.Dest
		if r.Env != "" {
			label += " (env: " + r.Env + ")"
		}
		if r.Err != nil {
			fmt.Printf("  ✗ %s: %v\n", label, r.Err)
			failCount++
		} else {
			fmt.Printf("  ✓ %s\n", label)
		}
	}
	fmt.Println()
	fmt.Printf("Result: %d/%d checks passed\n", len(results)-failCount, len(results))

	if failCount > 0 {
		return fmt.Errorf("%d check(s) failed", failCount)
	}
	return nil
}
