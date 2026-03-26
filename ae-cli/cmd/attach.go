package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach to the tmux session",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.NewManager(apiClient, cfg)
		state, err := mgr.Current()
		if err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if state == nil {
			return fmt.Errorf("no active session — run 'ae-cli start' first")
		}
		if state.TmuxSession == "" {
			return fmt.Errorf("session has no tmux session")
		}

		tmuxCmd := exec.Command("tmux", "attach-session", "-t", state.TmuxSession)
		tmuxCmd.Stdin = os.Stdin
		tmuxCmd.Stdout = os.Stdout
		tmuxCmd.Stderr = os.Stderr

		if err := tmuxCmd.Run(); err != nil {
			return fmt.Errorf("attaching to tmux: %w", err)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(attachCmd)
}
