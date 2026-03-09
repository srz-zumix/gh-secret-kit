package migrate

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
	"github.com/srz-zumix/go-gh-extension/pkg/render"
)

var (
	listRepo     string
	listPlain    bool
)

// NewListCmd creates the migrate list command
func NewListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list [org]",
		Short: "List repositories that have secrets registered",
		Long: `List repositories that have at least one secret registered (repository-level or environment-level).

When called without arguments, the current repository's owner is used as the
organization. You can pass an explicit org name (or HOST/ORG) as the first argument.

Use -R/--repo to check a single specific repository instead of scanning an organization.

The output shows the scope of each secret group: "repository" for repository-level secrets,
or "env:<name>" for environment-level secrets.`,
		RunE: runMigrateList,
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&listRepo, "repo", "R", "", "Check a single repository (e.g., owner/repo). When specified, org scan is skipped.")
	f.BoolVar(&listPlain, "plain", false, "Print tab-separated repository name and scope, one per line")

	return cmd
}

func runMigrateList(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// -R/--repo specified: check only that single repository
	if listRepo != "" {
		logger.Debug(fmt.Sprintf("Repo: %s", listRepo))
		return runMigrateListRepo(ctx, listRepo)
	}

	// Org mode: use explicit org arg, or fall back to current repository's owner
	org := ""
	if len(args) > 0 {
		org = args[0]
	}
	logger.Debug(fmt.Sprintf("Org: %s", org))
	return runMigrateListOrg(ctx, org)
}

func runMigrateListOrg(ctx context.Context, source string) error {
	ownerRepo, err := parser.Repository(parser.RepositoryOwnerWithHost(source))
	if err != nil {
		return fmt.Errorf("failed to parse organization: %w", err)
	}

	client, err := gh.NewGitHubClientWithRepo(ownerRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	logger.Debug(fmt.Sprintf("Listing repositories for owner: %s", ownerRepo.Owner))

	repos, err := gh.ListOwnerRepositories(ctx, client, ownerRepo.Owner)
	if err != nil {
		return fmt.Errorf("failed to list repositories for %s: %w", ownerRepo.Owner, err)
	}

	logger.Debug(fmt.Sprintf("Found %d repositories total, scanning for secrets...", len(repos)))

	var results []gh.RepoWithSecrets
	for _, repo := range repos {
		if repo.GetFullName() == "" {
			continue
		}
		repoRef, err := parser.Repository(parser.RepositoryInput(repo.GetFullName()))
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping %s: failed to parse repository: %v", repo.GetFullName(), err))
			continue
		}
		secrets, err := gh.ListRepoSecrets(ctx, client, repoRef)
		if err != nil {
			logger.Warn(fmt.Sprintf("Skipping %s: failed to list secrets: %v", repo.GetFullName(), err))
			continue
		}

		envSecrets, err := gh.CollectEnvSecrets(ctx, client, repo)
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

	renderResults(results)
	return nil
}

func runMigrateListRepo(ctx context.Context, source string) error {
	repoRef, err := parser.Repository(parser.RepositoryInput(source))
	if err != nil {
		return fmt.Errorf("failed to parse source repository: %w", err)
	}

	client, err := gh.NewGitHubClientWithRepo(repoRef)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	repoInfo, err := gh.GetRepository(ctx, client, repoRef)
	if err != nil {
		return fmt.Errorf("failed to get repository %s/%s: %w", repoRef.Owner, repoRef.Name, err)
	}

	secrets, err := gh.ListRepoSecrets(ctx, client, repoRef)
	if err != nil {
		return fmt.Errorf("failed to list secrets for %s/%s: %w", repoRef.Owner, repoRef.Name, err)
	}

	envSecrets, err := gh.CollectEnvSecrets(ctx, client, repoInfo)
	if err != nil {
		logger.Warn(fmt.Sprintf("Skipping environments for %s/%s: %v", repoRef.Owner, repoRef.Name, err))
	}

	var results []gh.RepoWithSecrets
	if len(secrets) > 0 || len(envSecrets) > 0 {
		results = append(results, gh.RepoWithSecrets{
			Repository: repoInfo,
			Secrets:    secrets,
			EnvSecrets: envSecrets,
		})
	}

	renderResults(results)
	return nil
}

func renderResults(results []gh.RepoWithSecrets) {
	if listPlain {
		for _, r := range results {
			name := r.Repository.GetFullName()
			if r.SecretCount() > 0 {
				fmt.Printf("%s\t%s\n", name, "repository")
			}
			for envName := range r.EnvSecrets {
				fmt.Printf("%s\t%s\n", name, "env:"+envName)
			}
		}
		return
	}
	r := render.NewRenderer(nil)
	r.RenderRepositoriesWithScopedSecretCount(results)
}
