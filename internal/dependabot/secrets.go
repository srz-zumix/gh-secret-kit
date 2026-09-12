package dependabot

import (
	"context"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
)

// The Dependabot secrets endpoints are not wrapped by go-gh-extension yet, so
// thin wrappers in the same style live here. API call errors are returned
// unwrapped, matching the gh/client convention.

// listDependabotRepoSecrets lists all Dependabot secrets of a repository.
func listDependabotRepoSecrets(ctx context.Context, g *gh.GitHubClient, repo repository.Repository) ([]*github.Secret, error) {
	client := g.GetClient()
	opts := &github.ListOptions{PerPage: 100}
	var all []*github.Secret
	for {
		secrets, resp, err := client.Dependabot.ListRepoSecrets(ctx, repo.Owner, repo.Name, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, secrets.Secrets...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// listDependabotOrgSecrets lists all Dependabot secrets of an organization.
func listDependabotOrgSecrets(ctx context.Context, g *gh.GitHubClient, repo repository.Repository) ([]*github.Secret, error) {
	client := g.GetClient()
	opts := &github.ListOptions{PerPage: 100}
	var all []*github.Secret
	for {
		secrets, resp, err := client.Dependabot.ListOrgSecrets(ctx, repo.Owner, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, secrets.Secrets...)
		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
}

// setDependabotRepoSecret encrypts a plaintext value with the repository
// Dependabot public key and stores it as a repository Dependabot secret.
func setDependabotRepoSecret(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, name, value string) error {
	client := g.GetClient()
	publicKey, _, err := client.Dependabot.GetRepoPublicKey(ctx, repo.Owner, repo.Name)
	if err != nil {
		return err
	}
	eSecret, err := gh.EncryptSecret(publicKey, name, value)
	if err != nil {
		return err
	}
	_, err = client.Dependabot.CreateOrUpdateRepoSecret(ctx, repo.Owner, repo.Name, name, github.SecretRequest{
		KeyID:          eSecret.KeyID,
		EncryptedValue: eSecret.EncryptedValue,
	})
	return err
}

// deleteDependabotRepoSecret deletes a repository Dependabot secret.
func deleteDependabotRepoSecret(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, name string) error {
	client := g.GetClient()
	_, err := client.Dependabot.DeleteRepoSecret(ctx, repo.Owner, repo.Name, name)
	return err
}
