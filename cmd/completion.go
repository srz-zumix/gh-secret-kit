package cmd

import (
	"github.com/srz-zumix/go-gh-extension/pkg/completion"
)

func init() {
	rootCmd.AddCommand(completion.NewCompletionCmd())
}
