package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to the AI Efficiency Platform via browser",
	Long:  "Opens a browser window for OAuth2 login. After approval, a token is saved locally.",
	RunE: func(cmd *cobra.Command, args []string) error {
		serverURL := buildinfo.ServerURL
		if serverURL == "" {
			return fmt.Errorf("server URL not configured")
		}

		result, err := auth.Login(context.Background(), auth.OAuthConfig{
			ServerURL: serverURL,
			ClientID:  "ae-cli",
			Timeout:   3 * time.Minute,
		})
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
		}

		tokenPath, err := auth.DefaultTokenPath()
		if err != nil {
			return fmt.Errorf("get token path: %w", err)
		}

		token := &auth.TokenFile{
			AccessToken:  result.AccessToken,
			RefreshToken: result.RefreshToken,
			ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second),
			ServerURL:    serverURL,
		}

		if err := auth.WriteToken(tokenPath, token); err != nil {
			return fmt.Errorf("save token: %w", err)
		}

		fmt.Printf("Login successful! Token saved to %s\n", tokenPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
