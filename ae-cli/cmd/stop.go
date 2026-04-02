package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/spf13/cobra"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the current efficiency tracking session",
	RunE: func(cmd *cobra.Command, args []string) error {
		mgr := session.NewManager(apiClient, cfg)

		state, err := mgr.Current()
		if err != nil {
			return fmt.Errorf("checking session: %w", err)
		}
		if state == nil {
			return fmt.Errorf("no active session")
		}

		proxyPID := 0
		if rt, err := session.ReadRuntimeBundle(state.ID); err == nil && rt != nil && rt.Proxy != nil {
			proxyPID = rt.Proxy.PID
		}

		// Kill tmux session if it exists
		if state.TmuxSession != "" && tmux.SessionExists(state.TmuxSession) {
			if err := tmux.KillSession(state.TmuxSession); err != nil {
				fmt.Printf("warning: failed to kill tmux session: %v\n", err)
			}
		}

		state, err = mgr.Stop()
		if err != nil {
			return fmt.Errorf("stopping session: %w", err)
		}

		fmt.Printf("Session stopped.\n")
		fmt.Printf("  ID:     %s\n", state.ID)
		fmt.Printf("  Repo:   %s\n", state.Repo)
		fmt.Printf("  Branch: %s\n", state.Branch)
		if proxyPID > 0 {
			fmt.Printf("  Proxy:  stopped pid %d\n", proxyPID)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}
