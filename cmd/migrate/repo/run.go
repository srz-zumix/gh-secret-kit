package repo

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/workflow"
)

// NewRunCmd creates the repo run command.
func NewRunCmd() *cobra.Command {
	return workflow.NewRunCmd()
}
