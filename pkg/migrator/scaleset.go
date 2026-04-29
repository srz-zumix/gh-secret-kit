package migrator

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/actions/scaleset"
	"github.com/cli/go-gh/v2/pkg/auth"
	"github.com/cli/go-gh/v2/pkg/repository"
)

const (
	systemName = "gh-secret-kit"
	// DefaultRunnerGroupID is the ID of the default runner group
	DefaultRunnerGroupID = 1
)

// BuildGitHubConfigURL constructs the GitHub config URL from a repository.
// For organizations, returns https://host/owner.
// For repositories, returns https://host/owner/repo.
func BuildGitHubConfigURL(repo repository.Repository) string {
	host := repo.Host
	if host == "" {
		host = "github.com"
	}
	if repo.Name == "" {
		// Organization scope
		return fmt.Sprintf("https://%s/%s", host, repo.Owner)
	}
	// Repository scope
	return fmt.Sprintf("https://%s/%s/%s", host, repo.Owner, repo.Name)
}

// NewScaleSetClient creates a new scaleset client using PAT from gh auth
func NewScaleSetClient(configURL string) (*scaleset.Client, error) {
	u, err := url.Parse(configURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config URL: %w", err)
	}

	host := u.Host
	token, _ := auth.TokenForHost(host)
	if token == "" {
		return nil, fmt.Errorf("no GitHub token found for host '%s'; please run 'gh auth login' first", host)
	}

	return scaleset.NewClientWithPersonalAccessToken(
		scaleset.NewClientWithPersonalAccessTokenConfig{
			GitHubConfigURL:     configURL,
			PersonalAccessToken: token,
			SystemInfo: scaleset.SystemInfo{
				System:    systemName,
				Subsystem: "migrate",
				Version:   "0.1.0",
			},
		},
	)
}

// SetScaleSetSystemInfo updates the system info on the scaleset client with the scale set ID
func SetScaleSetSystemInfo(client *scaleset.Client, scaleSetID int) {
	client.SetSystemInfo(scaleset.SystemInfo{
		System:     systemName,
		Subsystem:  "migrate",
		Version:    "0.1.0",
		ScaleSetID: scaleSetID,
	})
}

// CreateRunnerScaleSet creates a new runner scale set with the given name as both name and label.
// runnerGroupID specifies which runner group the scale set belongs to (use DefaultRunnerGroupID for the default group).
func CreateRunnerScaleSet(ctx context.Context, client *scaleset.Client, name string, runnerGroupID int) (*scaleset.RunnerScaleSet, error) {
	return client.CreateRunnerScaleSet(ctx, &scaleset.RunnerScaleSet{
		Name:          name,
		RunnerGroupID: runnerGroupID,
		Labels: []scaleset.Label{
			{Name: name},
		},
		RunnerSetting: scaleset.RunnerSetting{
			DisableUpdate: true,
		},
	})
}

// FindRunnerScaleSet finds a runner scale set by name in the specified runner group.
// runnerGroupID specifies which runner group to search in (use DefaultRunnerGroupID for the default group).
func FindRunnerScaleSet(ctx context.Context, client *scaleset.Client, name string, runnerGroupID int) (*scaleset.RunnerScaleSet, error) {
	return client.GetRunnerScaleSet(ctx, runnerGroupID, name)
}

// GetRunnerScaleSetByID retrieves a runner scale set by its ID
func GetRunnerScaleSetByID(ctx context.Context, client *scaleset.Client, scaleSetID int) (*scaleset.RunnerScaleSet, error) {
	return client.GetRunnerScaleSetByID(ctx, scaleSetID)
}

// GetRunnerGroupByName retrieves a runner group by name
func GetRunnerGroupByName(ctx context.Context, client *scaleset.Client, groupName string) (*scaleset.RunnerGroup, error) {
	return client.GetRunnerGroupByName(ctx, groupName)
}

// FindRunnerGroupByName finds a runner group by name.
// Returns (nil, nil) if the runner group is not found.
func FindRunnerGroupByName(ctx context.Context, client *scaleset.Client, groupName string) (*scaleset.RunnerGroup, error) {
	group, err := client.GetRunnerGroupByName(ctx, groupName)
	if err != nil {
		if strings.Contains(err.Error(), "no runner group found with name") {
			return nil, nil
		}
		return nil, err
	}
	return group, nil
}

// DeleteRunnerScaleSet deletes a runner scale set by ID
func DeleteRunnerScaleSet(ctx context.Context, client *scaleset.Client, scaleSetID int) error {
	return client.DeleteRunnerScaleSet(ctx, scaleSetID)
}

// GenerateJITConfig generates a JIT runner configuration for the specified scale set
func GenerateJITConfig(ctx context.Context, client *scaleset.Client, scaleSetID int, runnerName string) (*scaleset.RunnerScaleSetJitRunnerConfig, error) {
	return client.GenerateJitRunnerConfig(
		ctx,
		&scaleset.RunnerScaleSetJitRunnerSetting{
			Name: runnerName,
		},
		scaleSetID,
	)
}
