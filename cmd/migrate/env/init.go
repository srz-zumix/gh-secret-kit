package env

import (
	"github.com/spf13/cobra"
	"github.com/srz-zumix/gh-secret-kit/internal/migrate/workflow"
)

// NewInitCmd creates the env init command.
func NewInitCmd() *cobra.Command {
	return workflow.NewInitCmd()
}
