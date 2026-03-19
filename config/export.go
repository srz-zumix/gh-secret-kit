package config

import (
	"context"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/gh/client"
)

// ExportOptions controls the behaviour of Exporter.Export.
type ExportOptions struct {
	// EnvName is the name of the environment to export.
	// When empty, all environments in the repository are exported.
	EnvName string
}

// Exporter fetches and converts a GitHub Actions environment to an EnvironmentConfig.
type Exporter struct {
	ctx    context.Context
	client *client.GitHubClient
	Repo   repository.Repository
}

// NewExporter creates an Exporter for the given repository.
func NewExporter(repo repository.Repository) (*Exporter, error) {
	c, err := gh.NewGitHubClientWithRepo(repo)
	if err != nil {
		return nil, fmt.Errorf("failed to create GitHub client: %w", err)
	}
	return &Exporter{
		ctx:    context.Background(),
		client: c,
		Repo:   repo,
	}, nil
}

// Export retrieves environments according to opts and returns their EnvironmentConfigs.
// When opts.EnvName is empty, all environments in the repository are exported.
func (e *Exporter) Export(opts ExportOptions) ([]*EnvironmentConfig, error) {
	if opts.EnvName != "" {
		cfg, err := e.exportOne(opts.EnvName)
		if err != nil {
			return nil, err
		}
		return []*EnvironmentConfig{cfg}, nil
	}

	environments, err := gh.ListEnvironments(e.ctx, e.client, e.Repo)
	if err != nil {
		return nil, fmt.Errorf("failed to list environments: %w", err)
	}

	cfgs := make([]*EnvironmentConfig, 0, len(environments))
	for _, env := range environments {
		cfg, err := e.exportOne(*env.Name)
		if err != nil {
			return nil, err
		}
		cfgs = append(cfgs, cfg)
	}
	return cfgs, nil
}

// exportOne retrieves a single environment and returns its EnvironmentConfig,
// including deployment branch policies and variables.
func (e *Exporter) exportOne(envName string) (*EnvironmentConfig, error) {
	environment, err := gh.GetEnvironment(e.ctx, e.client, e.Repo, envName)
	if err != nil {
		return nil, fmt.Errorf("failed to get environment %q: %w", envName, err)
	}

	policies, err := gh.ListDeploymentCustomBranchPolicies(e.ctx, e.client, e.Repo, environment)
	if err != nil {
		return nil, fmt.Errorf("failed to list deployment branch policies for environment %q: %w", envName, err)
	}

	vars, err := gh.ListEnvVariables(e.ctx, e.client, e.Repo, envName)
	if err != nil {
		return nil, fmt.Errorf("failed to list variables from environment %q: %w", envName, err)
	}

	return EnvironmentConfigFromGitHub(environment, policies, vars), nil
}
