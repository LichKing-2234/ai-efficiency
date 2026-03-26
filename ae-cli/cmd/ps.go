package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/spf13/cobra"
)

var psCmd = &cobra.Command{
	Use:   "ps",
	Short: "List running tool panes in the tmux session",
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

		panes, err := tmux.ListPanes(state.TmuxSession)
		if err != nil {
			return fmt.Errorf("listing panes: %w", err)
		}

		fmt.Printf("%-12s %-20s %s\n", "PANE ID", "COMMAND", "ACTIVE")
		for _, p := range panes {
			active := ""
			if p.Active {
				active = "*"
			}
			fmt.Printf("%-12s %-20s %s\n", p.ID, p.Tool, active)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}
