package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/spf13/cobra"
)

var killPane = tmux.KillPane

var killCmd = &cobra.Command{
	Use:   "kill <pane-id>",
	Short: "Kill a specific tool pane",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		paneID := args[0]

		if err := killPane(paneID); err != nil {
			return fmt.Errorf("killing pane %s: %w", paneID, err)
		}

		mgr := session.NewManager(apiClient, cfg)
		if state, err := mgr.Current(); err == nil && state != nil {
			_ = session.RemoveToolPaneByPaneID(state.ID, paneID)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Pane %s killed.\n", paneID)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(killCmd)
}
