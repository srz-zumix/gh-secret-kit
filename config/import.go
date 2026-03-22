package config

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v84/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/client"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// ImportOptions controls the behaviour of Importer.Import.
type ImportOptions struct {
	// TargetEnv, when non-empty, restricts imports to environments whose Name matches TargetEnv.
	// If no environment with the given name exists in cfgs, Import returns an error.
	TargetEnv string
	// Overwrite allows existing variables to be overwritten.
	Overwrite bool
	// DryRun prints planned changes without making any API calls.
	DryRun bool
}

// Importer applies a GitHub Actions environment configuration to a repository.
type Importer struct {
	ctx    context.Context
	client *client.GitHubClient
	Repo   repository.Repository
}

// NewImporter creates an Importer for the given repository.
func NewImporter(repo repository.Repository) (*Importer, error) {
	c, err := gh.NewGitHubClientWithRepo(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	return &Importer{
		ctx:    context.Background(),
		client: c,
		Repo:   repo,
	}, nil
}

// Import applies cfgs to the repository according to opts.
// When opts.TargetEnv is non-empty, only configs whose Name matches TargetEnv are imported.
// It returns the list of configs that were processed after any filtering.
// When opts.DryRun is true, planned changes are printed without making any API calls.
func (i *Importer) Import(cfgs []*EnvironmentConfig, opts ImportOptions) ([]*EnvironmentConfig, error) {
	targets := cfgs
	if opts.TargetEnv != "" {
		targets = make([]*EnvironmentConfig, 0, len(cfgs))
		for _, cfg := range cfgs {
			if cfg.Name == opts.TargetEnv {
				targets = append(targets, cfg)
			}
		}
		if len(targets) == 0 {
			return nil, fmt.Errorf("no environment named %q found in config", opts.TargetEnv)
		}
	}

	for _, cfg := range targets {
		if err := i.importOne(cfg, opts); err != nil {
			return nil, err
		}
	}
	return targets, nil
}

// importOne applies a single EnvironmentConfig to the repository.
func (i *Importer) importOne(cfg *EnvironmentConfig, opts ImportOptions) error {
	targetEnv := cfg.Name
	if targetEnv == "" {
		return fmt.Errorf("environment name is required in configuration")
	}

	if opts.DryRun {
		logger.Info("[dryrun] Would create/update environment", "env", targetEnv, "owner", i.Repo.Owner, "repo", i.Repo.Name)
		for _, p := range cfg.BranchPolicies {
			logger.Info("[dryrun] Would add branch policy", "name", p.Name, "type", p.Type)
		}
		for _, v := range cfg.Variables {
			logger.Info("[dryrun] Would set variable", "name", v.Name)
		}
		return nil
	}

	envReq, err := i.buildCreateUpdateRequest(cfg)
	if err != nil {
		return fmt.Errorf("failed to resolve reviewers: %w", err)
	}

	if _, err := gh.CreateUpdateEnvironment(i.ctx, i.client, i.Repo, targetEnv, envReq); err != nil {
		return fmt.Errorf("failed to create/update environment %q: %w", targetEnv, err)
	}
	logger.Info("Applied environment", "owner", i.Repo.Owner, "repo", i.Repo.Name, "env", targetEnv)

	// Apply custom branch policies when the env uses custom policies
	if cfg.DeploymentBranchPolicy != nil && cfg.DeploymentBranchPolicy.CustomBranchPolicies {
		for _, p := range cfg.BranchPolicies {
			refType := p.Type
			if refType == "" {
				refType = "branch"
			}
			if _, pErr := gh.CreateDeploymentBranchPolicy(i.ctx, i.client, i.Repo, targetEnv, p.Name, refType); pErr != nil {
				return fmt.Errorf("failed to add branch policy %q to environment %q: %w", p.Name, targetEnv, pErr)
			}
			logger.Info("Applied branch policy", "name", p.Name, "type", refType)
		}
	}

	// Apply variables
	for _, v := range cfg.Variables {
		actVar := &github.ActionsVariable{Name: v.Name, Value: v.Value}
		if err := gh.CreateOrUpdateEnvVariable(i.ctx, i.client, i.Repo, targetEnv, actVar, opts.Overwrite); err != nil {
			return fmt.Errorf("failed to set variable %q in environment %q: %w", v.Name, targetEnv, err)
		}
		logger.Info("Applied variable", "name", v.Name)
	}

	return nil
}

// buildCreateUpdateRequest constructs a CreateUpdateEnvironment from the config.
// Reviewer names are resolved to IDs via the GitHub API.
func (i *Importer) buildCreateUpdateRequest(cfg *EnvironmentConfig) (*github.CreateUpdateEnvironment, error) {
	waitTimer := cfg.WaitTimer
	preventSelfReview := cfg.PreventSelfReview
	canAdminsBypass := cfg.CanAdminsBypass

	req := &github.CreateUpdateEnvironment{
		CanAdminsBypass:   &canAdminsBypass,
		WaitTimer:         &waitTimer,
		PreventSelfReview: &preventSelfReview,
		Reviewers:         []*github.EnvReviewers{},
	}

	if cfg.DeploymentBranchPolicy != nil {
		req.DeploymentBranchPolicy = &github.BranchPolicy{
			ProtectedBranches:    &cfg.DeploymentBranchPolicy.ProtectedBranches,
			CustomBranchPolicies: &cfg.DeploymentBranchPolicy.CustomBranchPolicies,
		}
	}

	for _, rev := range cfg.Reviewers {
		reviewer := &github.EnvReviewers{Type: &rev.Type}
		switch rev.Type {
		case "User":
			user, err := gh.FindUser(i.ctx, i.client, rev.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to find user %q: %w", rev.Name, err)
			}
			reviewer.ID = user.ID
		case "Team":
			r := repository.Repository{Owner: i.Repo.Owner, Host: i.Repo.Host}
			team, err := gh.GetTeamBySlug(i.ctx, i.client, r, rev.Name)
			if err != nil {
				return nil, fmt.Errorf("failed to find team %q: %w", rev.Name, err)
			}
			reviewer.ID = team.ID
		default:
			continue
		}
		if reviewer.ID != nil {
			req.Reviewers = append(req.Reviewers, reviewer)
		}
	}

	return req, nil
}
