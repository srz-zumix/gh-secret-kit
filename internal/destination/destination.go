// Package destination resolves and verifies the destinations that secrets are
// copied to, together with the token used for each destination host.
package destination

import (
	"context"
	"fmt"
	"strings"

	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/parser"
)

// Destination is a copy destination resolved from a command line argument.
type Destination struct {
	// Arg is the original command line argument, used in error messages.
	Arg  string
	Repo repository.Repository
	// Target is OWNER/REPO, or ORG when orgLevel is true.
	Target string
	Host   string
}

// Resolve parses each destination argument and resolves its host, falling back
// to the source host and then github.com. When orgLevel is true, each argument
// is [HOST/]ORG instead of [HOST/]OWNER/REPO.
func Resolve(args []string, orgLevel bool, sourceRepo repository.Repository) ([]*Destination, error) {
	destinations := make([]*Destination, 0, len(args))
	for _, arg := range args {
		var destRepo repository.Repository
		var err error
		if orgLevel {
			destRepo, err = parser.Repository(parser.RepositoryOwnerWithHost(arg))
		} else {
			destRepo, err = parser.Repository(parser.RepositoryInput(arg))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to parse destination %q: %w", arg, err)
		}

		host := destRepo.Host
		if host == "" {
			host = sourceRepo.Host
		}
		if host == "" {
			host = "github.com"
		}
		destRepo.Host = host

		target := destRepo.Owner
		if !orgLevel {
			if destRepo.Name == "" {
				return nil, fmt.Errorf("destination %q must be in [HOST/]OWNER/REPO format", arg)
			}
			target = destRepo.Owner + "/" + destRepo.Name
		}

		destinations = append(destinations, &Destination{
			Arg:    arg,
			Repo:   destRepo,
			Target: target,
			Host:   host,
		})
	}
	return destinations, nil
}

// ResolveTokens returns a token per distinct destination host, taken from
// override or from the local gh authentication.
func ResolveTokens(override string, destinations []*Destination) (map[string]string, error) {
	hosts := make(map[string]struct{})
	for _, dest := range destinations {
		hosts[dest.Host] = struct{}{}
	}

	tokens := make(map[string]string, len(hosts))
	if override != "" {
		if len(hosts) > 1 {
			return nil, fmt.Errorf("--dst-token cannot be used when the destinations span multiple hosts")
		}
		for host := range hosts {
			tokens[host] = override
		}
		return tokens, nil
	}

	for host := range hosts {
		token, _ := auth.TokenForHost(host)
		if token == "" {
			return nil, fmt.Errorf("no token found for destination host %s; run \"gh auth login --hostname %s\" or pass --dst-token", host, host)
		}
		tokens[host] = token
	}
	return tokens, nil
}

// NameForHost builds a secret or environment variable name unique to a
// destination host by appending the host in the character set those names allow.
func NameForHost(base, host string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(host) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	return base + "_" + b.String()
}

// Verify checks that each destination exists and is reachable with the resolved
// token, so a typo does not waste a copy run.
func Verify(ctx context.Context, orgLevel bool, destinations []*Destination, hostTokens map[string]string) error {
	for _, dest := range destinations {
		destClient, err := gh.NewGitHubClientWithToken(dest.Repo, hostTokens[dest.Host])
		if err != nil {
			return fmt.Errorf("failed to create GitHub client for destination %q: %w", dest.Arg, err)
		}
		if orgLevel {
			if _, err := destClient.GetOrg(ctx, dest.Repo.Owner); err != nil {
				return fmt.Errorf("destination organization %q not found or inaccessible: %w", dest.Target, err)
			}
			continue
		}
		if _, err := gh.GetRepository(ctx, destClient, dest.Repo); err != nil {
			return fmt.Errorf("destination repository %q not found or inaccessible: %w", dest.Target, err)
		}
	}
	return nil
}
