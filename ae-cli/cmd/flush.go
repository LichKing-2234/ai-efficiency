package cmd

import (
	"context"
	"os"

	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/spf13/cobra"
)

var flushCmd = &cobra.Command{
	Use:    "flush",
	Short:  "Replay any locally queued hook events (hidden)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		h := hooks.NewHandler(hooks.UnsupportedUploader{})
		return h.Flush(context.Background(), cwd)
	},
}

func init() {
	rootCmd.AddCommand(flushCmd)
}
