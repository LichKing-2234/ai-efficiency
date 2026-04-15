package cmd

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/spf13/cobra"
)

var (
	loginForce         bool
	loginDevice        bool
	loginFlow          = auth.Login
	loginDeviceFlow    = auth.LoginDevice
	headlessBrowserEnv = auth.IsHeadlessLinux
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Login to the AI Efficiency Platform",
	Long:  "Uses browser PKCE by default and supports OAuth device authorization with --device.",
	RunE: func(cmd *cobra.Command, args []string) error {
		serverURL := resolveLoginServerURL(cfg, buildinfo.ServerURL)
		if serverURL == "" {
			return fmt.Errorf("server URL not configured")
		}

		tokenPath, err := auth.DefaultTokenPath()
		if err != nil {
			return fmt.Errorf("get token path: %w", err)
		}
		if !loginForce {
			if token, err := auth.ReadToken(tokenPath); err == nil && token.IsValid() {
				cmd.Println("Already logged in. Use --force to re-login.")
				return nil
			}
		}

		oauthCfg := auth.OAuthConfig{
			ServerURL: serverURL,
			ClientID:  "ae-cli",
			Timeout:   3 * time.Minute,
			Output:    cmd.OutOrStdout(),
		}

		var result *auth.OAuthResult
		switch {
		case loginDevice:
			result, err = loginDeviceFlow(context.Background(), oauthCfg)
		case headlessBrowserEnv(os.Getenv, runtime.GOOS):
			return fmt.Errorf("No browser environment detected. Use 'ae-cli login --device'.")
		default:
			result, err = loginFlow(context.Background(), oauthCfg)
		}
		if err != nil {
			return fmt.Errorf("login failed: %w", err)
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

		fmt.Fprintf(cmd.OutOrStdout(), "Login successful! Token saved to %s\n", tokenPath)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
	loginCmd.Flags().BoolVar(&loginForce, "force", false, "Force re-login even if already logged in")
	loginCmd.Flags().BoolVar(&loginDevice, "device", false, "Use OAuth device authorization flow")
}

func resolveLoginServerURL(cfg *config.Config, fallback string) string {
	if cfg != nil {
		if configured := strings.TrimSpace(cfg.Server.URL); configured != "" {
			return configured
		}
	}
	return strings.TrimSpace(fallback)
}
