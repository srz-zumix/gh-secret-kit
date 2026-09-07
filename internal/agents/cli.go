package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/srz-zumix/go-gh-extension/pkg/ghexec"
)

// agentTaskFields are the JSON fields requested from the gh CLI.
const agentTaskFields = "id,repository,state,createdAt,pullRequestNumber,pullRequestUrl"

// agentTask is a Copilot coding agent session as reported by the gh CLI.
type agentTask struct {
	ID                string    `json:"id"`
	Repository        string    `json:"repository"`
	State             string    `json:"state"`
	CreatedAt         time.Time `json:"createdAt"`
	PullRequestNumber int       `json:"pullRequestNumber"`
	PullRequestURL    string    `json:"pullRequestUrl"`
}

// isFinished reports whether the session will not produce any more output.
func (t agentTask) isFinished() bool {
	switch strings.ToLower(t.State) {
	case "completed", "failed", "cancelled", "canceled", "timed_out":
		return true
	}
	return false
}

// listAgentTasks lists the agent tasks visible to the authenticated user.
func listAgentTasks(ctx context.Context, limit int) ([]agentTask, error) {
	out, err := ghexec.Run(ctx, "agent-task", "list", "--json", agentTaskFields, "--limit", fmt.Sprint(limit))
	if err != nil {
		return nil, fmt.Errorf("failed to list agent tasks: %w", err)
	}
	var tasks []agentTask
	if err := json.Unmarshal([]byte(out), &tasks); err != nil {
		return nil, fmt.Errorf("failed to parse the agent task list: %w", err)
	}
	return tasks, nil
}

// createAgentTask starts a Copilot coding agent session on the repository, with
// base as the pull request base branch.
func createAgentTask(ctx context.Context, repo, base, prompt string) error {
	if _, err := ghexec.Run(ctx, "agent-task", "create", "--repo", repo, "--base", base, prompt); err != nil {
		return fmt.Errorf("failed to create an agent task on %s: %w", repo, err)
	}
	return nil
}

// viewAgentTask reads the current state of an agent task.
func viewAgentTask(ctx context.Context, repo, id string) (*agentTask, error) {
	out, err := ghexec.Run(ctx, "agent-task", "view", "--repo", repo, id, "--json", agentTaskFields)
	if err != nil {
		return nil, fmt.Errorf("failed to view agent task %s: %w", id, err)
	}
	var task agentTask
	if err := json.Unmarshal([]byte(out), &task); err != nil {
		return nil, fmt.Errorf("failed to parse agent task %s: %w", id, err)
	}
	return &task, nil
}
