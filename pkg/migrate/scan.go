package migrate

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// OrgContext holds parsed organization info and a GitHub client.
type OrgContext struct {
	OwnerRepo repository.Repository
	Client    *gh.GitHubClient
}

// ParseOrg parses an org string and creates a GitHub client.
func ParseOrg(orgStr string) (*OrgContext, error) {
	ownerRepo, err := parser.Repository(parser.RepositoryOwnerWithHost(orgStr))
	if err != nil {
		return nil, fmt.Errorf("failed to parse organization: %w", err)
	}
	client, err := gh.NewGitHubClientWithRepo(ownerRepo)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	return &OrgContext{OwnerRepo: ownerRepo, Client: client}, nil
}

// ParseOrgPair parses source and destination org strings and creates clients.
// If the destination has no host but the source does, the source host is inherited.
func ParseOrgPair(srcStr, dstStr string) (src, dst *OrgContext, err error) {
	srcOwnerRepo, err := parser.Repository(parser.RepositoryOwnerWithHost(srcStr))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse source organization: %w", err)
	}
	srcClient, err := gh.NewGitHubClientWithRepo(srcOwnerRepo)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create source GitHub client: %w", err)
	}
	src = &OrgContext{OwnerRepo: srcOwnerRepo, Client: srcClient}

	dstOwnerRepo, err := parser.Repository(parser.RepositoryOwnerWithHost(dstStr))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse destination organization: %w", err)
	}
	if dstOwnerRepo.Host == "" && srcOwnerRepo.Host != "" {
		dstOwnerRepo.Host = srcOwnerRepo.Host
	}
	dstClient := srcClient
	if dstOwnerRepo.Host != srcOwnerRepo.Host {
		dstClient, err = gh.NewGitHubClientWithRepo(dstOwnerRepo)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create destination GitHub client: %w", err)
		}
	}
	dst = &OrgContext{OwnerRepo: dstOwnerRepo, Client: dstClient}

	return src, dst, nil
}

// EnvMatch represents a matched environment pair with its secret metadata.
type EnvMatch struct {
	Name        string
	SecretCount int
	SecretNames []string
}

// RepoMatch represents a matched src/dst repo pair with secret metadata.
type RepoMatch struct {
	SrcRepoRef      repository.Repository
	SrcFullName     string
	SrcName         string
	DstRepoRef      repository.Repository
	RepoSecretCount int
	RepoSecretNames []string
	EnvMatches      []EnvMatch
}

// ScanMatchingRepos scans the source org and finds repos that have matching
// repos in the destination org, returning secret metadata for each pair.
// Repos with no secrets are still included so callers can track all matches.
func ScanMatchingRepos(ctx context.Context, src, dst *OrgContext) ([]RepoMatch, error) {
	repos, err := gh.ListOwnerRepositories(ctx, src.Client, src.OwnerRepo.Owner)
	if err != nil {
		return nil, fmt.Errorf("failed to list source repositories: %w", err)
	}

	logger.Info(fmt.Sprintf("Found %d repositories in source, scanning for secrets...", len(repos)))

	var results []RepoMatch
	for _, srcRepo := range repos {
		fullName := srcRepo.GetFullName()
		if fullName == "" {
			continue
		}

		repoName := srcRepo.GetName()
		srcRepoRef, err := gh.GetRepositoryFromGitHubRepository(srcRepo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping %s: failed to parse repository: %v", fullName, err))
			continue
		}

		// Check if destination repository exists
		dstRepoRef := repository.Repository{
			Owner: dst.OwnerRepo.Owner,
			Name:  repoName,
			Host:  dst.OwnerRepo.Host,
		}
		dstRepoInfo, dstErr := gh.GetRepository(ctx, dst.Client, dstRepoRef)
		if dstErr != nil {
			logger.Debug(fmt.Sprintf("Skipping %s: no matching repository in destination", repoName))
			continue
		}

		// Get source repo secrets
		secrets, err := gh.ListRepoSecrets(ctx, src.Client, srcRepoRef)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping %s: failed to list secrets: %v", fullName, err))
			continue
		}
		var repoSecretNames []string
		for _, s := range secrets {
			repoSecretNames = append(repoSecretNames, s.Name)
		}

		// Get source env secrets and match with destination
		envSecrets, err := gh.CollectEnvSecrets(ctx, src.Client, srcRepo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping environments for %s: %v", fullName, err))
		}

		var envMatches []EnvMatch
		if len(envSecrets) > 0 {
			dstEnvSecrets, err := gh.CollectEnvSecrets(ctx, dst.Client, dstRepoInfo)
			if err != nil {
				logger.Warn(fmt.Sprintf("Skipping env check for %s: %v", repoName, err))
			} else {
				for envName, srcEnvSecs := range envSecrets {
					if _, exists := dstEnvSecrets[envName]; exists {
						var envSecretNames []string
						for _, s := range srcEnvSecs {
							envSecretNames = append(envSecretNames, s.Name)
						}
						envMatches = append(envMatches, EnvMatch{
							Name:        envName,
							SecretCount: len(srcEnvSecs),
							SecretNames: envSecretNames,
						})
					} else {
						logger.Debug(fmt.Sprintf("Skipping env %s/%s: no matching environment in destination", repoName, envName))
					}
				}
			}
		}

		results = append(results, RepoMatch{
			SrcRepoRef:      srcRepoRef,
			SrcFullName:     fullName,
			SrcName:         repoName,
			DstRepoRef:      dstRepoRef,
			RepoSecretCount: len(secrets),
			RepoSecretNames: repoSecretNames,
			EnvMatches:      envMatches,
		})
	}

	return results, nil
}

// ScanOrgRepos scans an org's repositories and returns those with secrets.
func ScanOrgRepos(ctx context.Context, oc *OrgContext) ([]gh.RepoWithSecrets, error) {
	repos, err := gh.ListOwnerRepositories(ctx, oc.Client, oc.OwnerRepo.Owner)
	if err != nil {
		return nil, fmt.Errorf("failed to list repositories for %s: %w", oc.OwnerRepo.Owner, err)
	}

	logger.Debug(fmt.Sprintf("Found %d repositories total, scanning for secrets...", len(repos)))

	var results []gh.RepoWithSecrets
	for _, repo := range repos {
		if repo.GetFullName() == "" {
			continue
		}
		repoRef, err := gh.GetRepositoryFromGitHubRepository(repo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping %s: failed to parse repository: %v", repo.GetFullName(), err))
			continue
		}
		secrets, err := gh.ListRepoSecrets(ctx, oc.Client, repoRef)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping %s: failed to list secrets: %v", repo.GetFullName(), err))
			continue
		}

		envSecrets, err := gh.CollectEnvSecrets(ctx, oc.Client, repo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping environments for %s: %v", repo.GetFullName(), err))
		}
		if len(secrets) > 0 || len(envSecrets) > 0 {
			results = append(results, gh.RepoWithSecrets{
				Repository: repo,
				Secrets:    secrets,
				EnvSecrets: envSecrets,
			})
		}
	}

	return results, nil
}
