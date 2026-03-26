package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/dispatcher"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <tool> [args...]",
	Short: "Run an AI tool within the current session",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		toolName := args[0]
		extraArgs := args[1:]

		mgr := session.NewManager(apiClient, cfg)
		state, err := mgr.Current()
		if err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if state == nil {
			return fmt.Errorf("no active session — run 'ae-cli start' first")
		}

		d := dispatcher.New(cfg, apiClient)
		if err := d.Run(state.ID, toolName, extraArgs, state.TmuxSession); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
}
