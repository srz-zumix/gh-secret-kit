package repo

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/workflow"
)

// NewDeleteCmd creates the repo delete command.
func NewDeleteCmd() *cobra.Command {
	return workflow.NewDeleteCmd()
}
