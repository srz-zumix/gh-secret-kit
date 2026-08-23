package migrate

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/types"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/workflow"
)

// NewDeleteRunsCmd creates the migrate delete-runs command.
func NewDeleteRunsCmd() *cobra.Command {
	return workflow.NewDeleteRunsCmd(types.DefaultWorkflowName)
}
