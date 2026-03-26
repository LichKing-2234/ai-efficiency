package cmd

import (
	"fmt"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/spf13/cobra"
)

var logoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Logout and remove saved token",
	RunE: func(cmd *cobra.Command, args []string) error {
		tokenPath, err := auth.DefaultTokenPath()
		if err != nil {
			return fmt.Errorf("get token path: %w", err)
		}

		if err := auth.DeleteToken(tokenPath); err != nil {
			return fmt.Errorf("delete token: %w", err)
		}

		fmt.Println("Logged out successfully.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(logoutCmd)
}
