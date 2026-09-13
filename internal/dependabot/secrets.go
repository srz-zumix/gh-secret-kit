package dependabot

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// Dependabot endpoints are not wrapped by go-gh-extension yet. These thin
// wrappers return API errors unchanged, like its gh/client helpers.
func listDependabotRepoSecrets(ctx context.Context, g *gh.GitHubClient, repo repository.Repository) ([]*github.Secret, error) {
	opts := &github.ListOptions{PerPage: 100}
	var all []*github.Secret
	for {
		secrets, resp, err := g.GetClient().Dependabot.ListRepoSecrets(ctx, repo.Owner, repo.Name, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, secrets.Secrets...)
		if resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

func listDependabotOrgSecrets(ctx context.Context, g *gh.GitHubClient, repo repository.Repository) ([]*github.Secret, error) {
	opts := &github.ListOptions{PerPage: 100}
	var all []*github.Secret
	for {
		secrets, resp, err := g.GetClient().Dependabot.ListOrgSecrets(ctx, repo.Owner, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, secrets.Secrets...)
		if resp.NextPage == 0 {
			return all, nil
		}
		opts.Page = resp.NextPage
	}
}

func setDependabotRepoSecret(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, name, value string) error {
	client := g.GetClient()
	publicKey, _, err := client.Dependabot.GetRepoPublicKey(ctx, repo.Owner, repo.Name)
	if err != nil {
		return err
	}
	encrypted, err := gh.EncryptSecret(publicKey, name, value)
	if err != nil {
		return err
	}
	_, err = client.Dependabot.CreateOrUpdateRepoSecret(ctx, repo.Owner, repo.Name, name, github.SecretRequest{
		KeyID:          encrypted.KeyID,
		EncryptedValue: encrypted.EncryptedValue,
	})
	return err
}

func deleteDependabotRepoSecret(ctx context.Context, g *gh.GitHubClient, repo repository.Repository, name string) error {
	_, err := g.GetClient().Dependabot.DeleteRepoSecret(ctx, repo.Owner, repo.Name, name)
	return err
}

// registerTokenSecrets preflights every name before storing any destination
// token, and rolls back partial registration even if the caller is cancelled.
func registerTokenSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, tokenSecretNames, hostTokens map[string]string) error {
	existing, err := listDependabotRepoSecrets(ctx, client, repo)
	if err != nil {
		return fmt.Errorf("failed to list the source repository's Dependabot secrets: %w", err)
	}
	present := make(map[string]bool, len(existing))
	for _, secret := range existing {
		present[strings.ToUpper(secret.GetName())] = true
	}
	requested := make(map[string]bool, len(tokenSecretNames))
	for host, name := range tokenSecretNames {
		key := strings.ToUpper(name)
		if present[key] {
			return fmt.Errorf("the source repository already has a Dependabot secret named %q: use --token-secret-name to pick another name", name)
		}
		if requested[key] {
			return fmt.Errorf("multiple destination hosts resolve to the temporary Dependabot secret %q", name)
		}
		if hostTokens[host] == "" {
			return fmt.Errorf("no destination token provided for host %s", host)
		}
		requested[key] = true
	}

	created := make(map[string]string, len(tokenSecretNames))
	for _, host := range sortedKeys(tokenSecretNames) {
		name := tokenSecretNames[host]
		logger.Info(fmt.Sprintf("Registering the temporary Dependabot secret %s...", name))
		// Include the attempted write: an interrupted response can hide a
		// successful write, and preflight established that the name was absent.
		created[host] = name
		if err := setDependabotRepoSecret(ctx, client, repo, name, hostTokens[host]); err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
			defer cancel()
			return errors.Join(
				fmt.Errorf("failed to register the temporary Dependabot secret %s: %w", name, err),
				removeTokenSecrets(cleanupCtx, client, repo, created),
			)
		}
	}
	return nil
}

// Remove credentials even if run cleanup failed: this prevents later queued
// jobs from receiving them, without invalidating tokens in already-running jobs.
func cleanupTokenSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, tokenSecretNames map[string]string, keep bool) error {
	if keep {
		logger.Warn("Keeping temporary Dependabot token secrets")
		return nil
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	return removeTokenSecrets(cleanupCtx, client, repo, tokenSecretNames)
}

func removeTokenSecrets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository, tokenSecretNames map[string]string) error {
	var result error
	for _, host := range sortedKeys(tokenSecretNames) {
		name := tokenSecretNames[host]
		logger.Info(fmt.Sprintf("Deleting the temporary Dependabot secret %s...", name))
		if err := deleteDependabotRepoSecret(ctx, client, repo, name); err != nil && !gh.IsHTTPNotFound(err) {
			result = errors.Join(result, fmt.Errorf("failed to delete the temporary Dependabot secret %s: %w", name, err))
		}
	}
	return result
}
