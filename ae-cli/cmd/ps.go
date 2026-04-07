package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/spf13/cobra"
)

var listPanes = tmux.ListPanes

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

		panes, err := listPanes(state.TmuxSession)
		if err != nil {
			return fmt.Errorf("listing panes: %w", err)
		}

		items, err := session.PruneToolPanes(state.ID, func(paneID string) bool {
			for _, pane := range panes {
				if pane.ID == paneID {
					return true
				}
			}
			return false
		})
		if err != nil {
			return fmt.Errorf("loading tool panes: %w", err)
		}
		paneByID := map[string]tmux.Pane{}
		for _, pane := range panes {
			paneByID[pane.ID] = pane
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "%-16s %-12s %-20s %s\n", "LABEL", "PANE ID", "COMMAND", "ACTIVE")
		for _, rec := range items {
			pane := paneByID[rec.PaneID]
			active := ""
			if pane.Active {
				active = "*"
			}
			fmt.Fprintf(out, "%-16s %-12s %-20s %s\n", session.FormatToolPaneLabel(rec), rec.PaneID, pane.Tool, active)
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(psCmd)
}
