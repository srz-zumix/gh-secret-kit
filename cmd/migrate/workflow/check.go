package workflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	migratePackage "github.com/srz-zumix/gh-secret-kit/pkg/migrate"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// RunCheck compares secrets between source and destination based on the configured scope
func RunCheck(ctx context.Context, config *CheckConfig) error {
	logger.Info("Checking migration status")
	logger.Debug(fmt.Sprintf("Source: %s, Destination: %s, Scope: %s", config.Source, config.Destination, config.Scope))

	// Parse source based on scope
	var sourceRepo repository.Repository
	var err error
	switch config.Scope {
	case migratePackage.SecretScopeOrg:
		sourceRepo, err = parser.Repository(parser.RepositoryOwnerWithHost(config.Source))
	default:
		sourceRepo, err = parser.Repository(parser.RepositoryInput(config.Source))
	}
	if err != nil {
		return fmt.Errorf("failed to parse source: %w", err)
	}

	// Initialize source GitHub client
	srcClient, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create source GitHub client: %w", err)
	}

	// Determine destination host: use explicit flag, fall back to source host
	destHost := config.DestinationHost
	if destHost == "" {
		destHost = sourceRepo.Host
	}
	if destHost == "" {
		destHost = "github.com"
	}

	// Parse destination based on scope
	var destRepo repository.Repository
	switch config.Scope {
	case migratePackage.SecretScopeOrg:
		destRepo, err = parser.Repository(parser.RepositoryOwnerWithHost(config.Destination))
	default:
		destRepo, err = parser.Repository(parser.RepositoryInput(config.Destination))
	}
	if err != nil {
		return fmt.Errorf("failed to parse destination: %w", err)
	}
	// Always apply the resolved destination host
	destRepo.Host = destHost

	// Initialize destination GitHub client
	var destClient *gh.GitHubClient
	if config.DestinationToken != "" {
		destClient, err = gh.NewGitHubClientWithToken(destRepo, config.DestinationToken)
	} else {
		destClient, err = gh.NewGitHubClientWithRepo(destRepo)
	}
	if err != nil {
		return fmt.Errorf("failed to create destination GitHub client: %w", err)
	}

	// Build rename mapping
	renameMap := make(map[string]string)
	for _, mapping := range config.Rename {
		parts := strings.SplitN(mapping, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid rename mapping format: %s (expected OLD_NAME=NEW_NAME)", mapping)
		}
		renameMap[parts[0]] = parts[1]
	}

	// Collect source secret names
	sourceNames, err := listSecretNamesByScope(ctx, srcClient, sourceRepo, config.Scope, config.SourceEnv)
	if err != nil {
		return fmt.Errorf("failed to list source secrets: %w", err)
	}

	// Apply --secrets filter
	if len(config.Secrets) > 0 {
		filter := make(map[string]struct{}, len(config.Secrets))
		for _, s := range config.Secrets {
			filter[s] = struct{}{}
		}
		filtered := sourceNames[:0]
		for _, n := range sourceNames {
			if _, ok := filter[n]; ok {
				filtered = append(filtered, n)
			}
		}
		sourceNames = filtered
	}

	// Collect destination secret names into a lookup set
	destNames, err := listSecretNamesByScope(ctx, destClient, destRepo, config.Scope, config.DestinationEnv)
	if err != nil {
		return fmt.Errorf("failed to list destination secrets: %w", err)
	}
	destSet := make(map[string]struct{}, len(destNames))
	for _, n := range destNames {
		destSet[n] = struct{}{}
	}

	// Compare
	type checkResult struct {
		srcName  string
		destName string
		migrated bool
	}
	results := make([]checkResult, 0, len(sourceNames))
	missingCount := 0
	for _, srcName := range sourceNames {
		destName := srcName
		if mapped, ok := renameMap[srcName]; ok {
			destName = mapped
		}
		_, migrated := destSet[destName]
		if !migrated {
			missingCount++
		}
		results = append(results, checkResult{srcName: srcName, destName: destName, migrated: migrated})
	}

	// Print results
	fmt.Printf("%-40s %-40s %s\n", "SOURCE", "DESTINATION", "STATUS")
	fmt.Printf("%-40s %-40s %s\n", strings.Repeat("-", 40), strings.Repeat("-", 40), "--------")
	for _, r := range results {
		status := "✓ migrated"
		if !r.migrated {
			status = "✗ missing"
		}
		fmt.Printf("%-40s %-40s %s\n", r.srcName, r.destName, status)
	}
	fmt.Println()
	migratedCount := len(results) - missingCount
	fmt.Printf("Result: %d/%d secrets migrated\n", migratedCount, len(results))

	if missingCount > 0 {
		return fmt.Errorf("%d secret(s) not yet migrated", missingCount)
	}
	return nil
}

// listSecretNamesByScope lists secret names based on the specified scope
func listSecretNamesByScope(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, scope migratePackage.SecretScope, env string) ([]string, error) {
	switch scope {
	case migratePackage.SecretScopeEnv:
		repoInfo, err := gh.GetRepository(ctx, client, repo)
		if err != nil {
			return nil, fmt.Errorf("failed to get repository info: %w", err)
		}
		secrets, err := gh.ListEnvSecrets(ctx, client, repoInfo, env)
		if err != nil {
			return nil, err
		}
		names := make([]string, len(secrets))
		for i, s := range secrets {
			names[i] = s.Name
		}
		return names, nil
	case migratePackage.SecretScopeOrg:
		secrets, err := gh.ListOrgSecrets(ctx, client, repo)
		if err != nil {
			return nil, err
		}
		names := make([]string, len(secrets))
		for i, s := range secrets {
			names[i] = s.Name
		}
		return names, nil
	default:
		secrets, err := gh.ListRepoSecrets(ctx, client, repo)
		if err != nil {
			return nil, err
		}
		names := make([]string, len(secrets))
		for i, s := range secrets {
			names[i] = s.Name
		}
		logger.Debug(fmt.Sprintf("Found %d secrets in %s/%s", len(names), repo.Owner, repo.Name))
		return names, nil
	}
}
