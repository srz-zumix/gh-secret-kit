package migrator

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v84/github"
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
	Name         string
	SecretCount  int
	SecretNames  []string
	HasReviewers bool // true when the source environment has required_reviewers protection rules
	DstEnvExists bool // true when the destination environment already exists
}

// RepoMatch represents a matched src/dst repo pair with secret and variable metadata.
type RepoMatch struct {
	SrcRepoRef        repository.Repository
	SrcFullName       string
	SrcName           string
	DstRepoRef        repository.Repository
	RepoSecretCount   int
	RepoSecretNames   []string
	EnvMatches        []EnvMatch
	RepoVariableCount int
	RepoVariableNames []string
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
		_ = dstRepoInfo

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

		// Get source env secrets and match with destination.
		// collectEnvSecretsWithReviewers fetches environments only once to avoid
		// the extra ListEnvironments call that CollectEnvSecrets would make.
		srcEnvSecrets, srcEnvReviewers, err := collectEnvSecretsWithReviewers(ctx, src.Client, srcRepoRef, srcRepo)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping environments for %s: %v", fullName, err))
		}

		var envMatches []EnvMatch
		if len(srcEnvSecrets) > 0 {
			// List destination environments to check existence (secrets are not needed here).
			dstEnvSet := make(map[string]bool)
			dstEnvs, dstEnvListErr := gh.ListEnvironments(ctx, dst.Client, dstRepoRef)
			if dstEnvListErr != nil {
				logger.Warn(fmt.Sprintf("Could not list destination environments for %s: %v", repoName, dstEnvListErr))
			} else {
				for _, e := range dstEnvs {
					dstEnvSet[e.GetName()] = true
				}
			}

			for envName, srcEnvSecs := range srcEnvSecrets {
				var envSecretNames []string
				for _, s := range srcEnvSecs {
					envSecretNames = append(envSecretNames, s.Name)
				}
				envMatches = append(envMatches, EnvMatch{
					Name:         envName,
					SecretCount:  len(srcEnvSecs),
					SecretNames:  envSecretNames,
					HasReviewers: srcEnvReviewers[envName],
					DstEnvExists: dstEnvSet[envName],
				})
				if !dstEnvSet[envName] {
					logger.Debug(fmt.Sprintf("Env %s/%s: no matching environment in destination (will use export|import to create)", repoName, envName))
				}
			}
		}

		// Get source repo variables
		variables, err := gh.ListRepoVariables(ctx, src.Client, srcRepoRef)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping variables for %s: failed to list variables: %v", fullName, err))
		}
		var repoVariableNames []string
		for _, v := range variables {
			repoVariableNames = append(repoVariableNames, v.Name)
		}

		results = append(results, RepoMatch{
			SrcRepoRef:        srcRepoRef,
			SrcFullName:       fullName,
			SrcName:           repoName,
			DstRepoRef:        dstRepoRef,
			RepoSecretCount:   len(secrets),
			RepoSecretNames:   repoSecretNames,
			EnvMatches:        envMatches,
			RepoVariableCount: len(variables),
			RepoVariableNames: repoVariableNames,
		})
	}

	return results, nil
}

// collectEnvSecretsWithReviewers lists all environments once and concurrently
// collects both secrets and reviewer presence without a duplicate ListEnvironments call.
// It returns a map of env name -> secrets and a map of env name -> hasReviewers.
func collectEnvSecretsWithReviewers(
	ctx context.Context,
	g *gh.GitHubClient,
	repoRef repository.Repository,
	repo *github.Repository,
) (map[string][]*github.Secret, map[string]bool, error) {
	envs, err := gh.ListEnvironments(ctx, g, repoRef)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list environments: %w", err)
	}
	if len(envs) == 0 {
		return nil, nil, nil
	}

	envSecrets := make(map[string][]*github.Secret)
	envReviewers := make(map[string]bool)
	for _, env := range envs {
		name := env.GetName()
		envReviewers[name] = envHasReviewers(env)
		secrets, err := gh.ListEnvSecrets(ctx, g, repo, name)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to list secrets for environment %s: %w", name, err)
		}
		if len(secrets) > 0 {
			envSecrets[name] = secrets
		}
	}
	return envSecrets, envReviewers, nil
}

// envHasReviewers reports whether the environment has at least one required reviewer.
func envHasReviewers(env *github.Environment) bool {
	for _, rule := range env.ProtectionRules {
		if rule.Type == nil {
			continue
		}
		if *rule.Type == "required_reviewers" && len(rule.Reviewers) > 0 {
			return true
		}
	}
	return false
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
