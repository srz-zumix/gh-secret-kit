package dependabot

import (
	"context"
	"errors"
	"fmt"

	"github.com/cli/go-gh/v2/pkg/repository"
	"github.com/google/go-github/v90/github"
	"github.com/srz-zumix/go-gh-extension/pkg/gh"
	"github.com/srz-zumix/go-gh-extension/pkg/logger"
)

// defaultBranchRefCondition is the ruleset ref condition that follows whichever
// branch is currently the repository default.
const defaultBranchRefCondition = "~DEFAULT_BRANCH"

// suspendedRuleset remembers the enforcement a ruleset had before suspension.
type suspendedRuleset struct {
	id          int64
	name        string
	enforcement github.RulesetEnforcement
}

// suspendDefaultBranchRulesets disables the repository rulesets that follow the
// default branch. The copy makes a temporary branch the default and commits the
// Dependabot configuration to it directly, which such rulesets would reject.
// Rulesets pinned to a fixed branch name are left untouched because the
// temporary branch never matches them. The returned function restores the
// original enforcement of every ruleset that was disabled.
func suspendDefaultBranchRulesets(ctx context.Context, client *gh.GitHubClient, repo repository.Repository) (func(context.Context) error, error) {
	// Parent (organization) rulesets are excluded: they cannot be updated
	// through the repository endpoint.
	rulesets, err := gh.ListRepositoryRulesets(ctx, client, repo, false)
	if err != nil {
		return nil, fmt.Errorf("failed to list the source repository's rulesets: %w", err)
	}
	var suspended []suspendedRuleset
	restore := func(restoreCtx context.Context) error {
		var result error
		for _, ruleset := range suspended {
			logger.Info(fmt.Sprintf("Restoring ruleset %s to %s...", ruleset.name, ruleset.enforcement))
			update := &github.RepositoryRuleset{Name: ruleset.name, Enforcement: ruleset.enforcement}
			if _, err := gh.UpdateRepositoryRuleset(restoreCtx, client, repo, ruleset.id, update); err != nil {
				result = errors.Join(result, fmt.Errorf("failed to restore ruleset %s (ID %d) to %s; re-enable it manually: %w",
					ruleset.name, ruleset.id, ruleset.enforcement, err))
			}
		}
		return result
	}
	for _, summary := range rulesets {
		if summary.Enforcement == github.RulesetEnforcementDisabled {
			continue
		}
		detail, err := gh.GetRepositoryRuleset(ctx, client, repo, summary.GetID(), false)
		if err != nil {
			return nil, errors.Join(
				fmt.Errorf("failed to get ruleset %s (ID %d): %w", summary.Name, summary.GetID(), err),
				restore(ctx),
			)
		}
		if !followsDefaultBranch(detail) {
			continue
		}
		logger.Warn(fmt.Sprintf("Temporarily disabling ruleset %s because it follows the default branch", detail.Name))
		// Record the ruleset before the request: an interrupted response can
		// hide a successful update, and restoring an active ruleset is safe.
		suspended = append(suspended, suspendedRuleset{id: detail.GetID(), name: detail.Name, enforcement: detail.Enforcement})
		update := &github.RepositoryRuleset{Name: detail.Name, Enforcement: github.RulesetEnforcementDisabled}
		if _, err := gh.UpdateRepositoryRuleset(ctx, client, repo, detail.GetID(), update); err != nil {
			return nil, errors.Join(
				fmt.Errorf("failed to disable ruleset %s (ID %d): %w", detail.Name, detail.GetID(), err),
				restore(ctx),
			)
		}
	}
	return restore, nil
}

// followsDefaultBranch reports whether the ruleset applies to whichever branch
// is currently the repository default.
func followsDefaultBranch(ruleset *github.RepositoryRuleset) bool {
	// An omitted target means the default, which is the branch target.
	if ruleset == nil || (ruleset.Target != nil && *ruleset.Target != github.RulesetTargetBranch) {
		return false
	}
	if !gh.HasAnyRulesetRule(ruleset.Rules) {
		return false
	}
	refName := ruleset.GetConditions().GetRefName()
	if refName == nil {
		return false
	}
	for _, exclude := range refName.Exclude {
		if exclude == defaultBranchRefCondition {
			return false
		}
	}
	for _, include := range refName.Include {
		if include == defaultBranchRefCondition {
			return true
		}
	}
	return false
}
