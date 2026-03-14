package runner

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/cmd/migrate/types"
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
  org   Organization name (optional). When omitted, uses the current repository's owner.`,
		RunE: runPrune,
		Args: cobra.MaximumNArgs(1),
	}

	f := cmd.Flags()
	f.StringVarP(&pruneRepo, "repo", "R", "", "Source repository (owner/repo); when omitted uses the first argument as org or falls back to the current repository")
	f.StringVar(&pruneRunnerOpts.RunnerLabel, "runner-label", types.DefaultRunnerLabel, "Only remove runners that have this label (empty string matches all gh-secret-kit runners)")
	f.BoolVarP(&pruneDryRun, "dry-run", "n", false, "Print runners that would be removed without deleting them")

	return cmd
}

func runPrune(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	sourceRepo, err := resolveSourceRepo(pruneRepo, args, pruneRunnerOpts.RunnerLabel)
	if err != nil {
		return err
	}

	client, err := gh.NewGitHubClientWithRepo(sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to create GitHub client: %w", err)
	}

	runners, err := gh.ListRunners(ctx, client, sourceRepo)
	if err != nil {
		return fmt.Errorf("failed to list runners: %w", err)
	}

	const namePrefix = "gh-secret-kit-"
	removed := 0
	skipped := 0

	for _, runner := range runners {
		name := runner.GetName()

		// Only target runners created by gh-secret-kit
		if !strings.HasPrefix(name, namePrefix) {
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
