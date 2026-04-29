package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/go-github/v84/github"
	"github.com/spf13/cobra"
	runnerinternal "github.com/srz-zumix/gh-secret-kit/internal/migrate/runner"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/pkg/migrator"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

var (
	pruneRepo       string
	pruneRunnerOpts types.RunnerOptions
	pruneDryRun     bool
)

// NewPruneCmd creates the runner prune command
func NewPruneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prune [[HOST]/ORG]",
		Short: "Remove leftover self-hosted runners registered by gh-secret-kit",
		Long: `Remove self-hosted runners whose names start with "gh-secret-kit-" that
were left behind by previous runs. Runners that are currently online and busy
are skipped to avoid disrupting running jobs; idle and offline runners may be
removed.

By default, only runners matching the runner-label are removed. Use
--runner-label "" to target all gh-secret-kit runners regardless of label.

Use --dry-run to preview which runners would be removed without actually
deleting them.

Arguments:
  [HOST]/ORG   Organization name, optionally prefixed with a GitHub host. When omitted, uses the current repository's owner.`,
		RunE: runPrune,
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&pruneRepo, "repo", "R", "", "Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository")
	f.StringVar(&pruneRunnerOpts.RunnerLabel, "runner-label", types.DefaultRunnerLabel, "Only remove runners that have this label (empty string matches all gh-secret-kit runners)")
	f.StringVar(&pruneRunnerOpts.RunnerGroup, "runner-group", "", "Only remove runners belonging to this runner group name (org-level only; cannot be combined with --repo)")
	f.BoolVarP(&pruneDryRun, "dry-run", "n", false, "Print runners that would be removed without deleting them")

	// Runner groups are org/enterprise only; combining with --repo (repo-level runner) makes no sense.
	cmd.MarkFlagsMutuallyExclusive("repo", "runner-group")

	return cmd
}

func runPrune(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	sourceRepo, err := runnerinternal.ResolveSourceRepo(pruneRepo, args, pruneRunnerOpts.RunnerLabel)
	if err != nil {
		return err
	}

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	var runners []*github.Runner
	if pruneRunnerOpts.RunnerGroup != "" {
		// Runner groups are an organization/enterprise feature and are not available for repository-level runners.
		if sourceRepo.Name != "" {
			return fmt.Errorf("--runner-group is not supported for repository runners (runner groups are org/enterprise only)")
		}
		group, err := gh.FindOrgRunnerGroup(ctx, client, sourceRepo, pruneRunnerOpts.RunnerGroup)
		if err != nil {
			return fmt.Errorf("failed to find runner group %q: %w", pruneRunnerOpts.RunnerGroup, err)
		}
		if group == nil {
			return fmt.Errorf("runner group %q not found", pruneRunnerOpts.RunnerGroup)
		}
		runners, err = gh.ListOrgRunnerGroupRunners(ctx, client, sourceRepo, group.GetID())
		if err != nil {
			return fmt.Errorf("failed to list runners for group %q: %w", pruneRunnerOpts.RunnerGroup, err)
		}
	} else {
		runners, err = gh.ListRunners(ctx, client, sourceRepo)
		if err != nil {
			return fmt.Errorf("failed to list runners: %w", err)
		}
	}

	removed := 0
	skipped := 0

	for _, runner := range runners {
		name := runner.GetName()

		// Only target runners created by gh-secret-kit
		if !strings.HasPrefix(name, migrator.RunnerNamePrefix) {
			continue
		}

		// Filter by label when runner-label flag is non-empty
		if pruneRunnerOpts.RunnerLabel != "" {
			matched := false
			for _, l := range runner.Labels {
				if l.GetName() == pruneRunnerOpts.RunnerLabel {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		status := runner.GetStatus() // "online" or "offline"
		busy := runner.GetBusy()

		if pruneDryRun {
			logger.Info(fmt.Sprintf("[dry-run] Would remove runner: %s (ID: %d, status: %s, busy: %v)",
				name, runner.GetID(), status, busy))
			removed++
			continue
		}

		if busy {
			logger.Warn(fmt.Sprintf("Skipping busy runner: %s (ID: %d)", name, runner.GetID()))
			skipped++
			continue
		}

		logger.Info(fmt.Sprintf("Removing runner: %s (ID: %d, status: %s)", name, runner.GetID(), status))
		if err := gh.RemoveRunner(ctx, client, sourceRepo, runner.GetID()); err != nil {
			logger.Warn(fmt.Sprintf("Failed to remove runner %s (ID: %d): %v", name, runner.GetID(), err))
			skipped++
			continue
		}
		logger.Info(fmt.Sprintf("Removed runner: %s", name))
		removed++
	}

	logger.Info(fmt.Sprintf("Done: removed=%d, skipped=%d", removed, skipped))
	return nil
}
