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
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/spf13/cobra"
)

var (
	loginForce         bool
	loginDevice        bool
	loginFlow          = auth.Login
	loginDeviceFlow    = auth.LoginDevice
	headlessBrowserEnv = auth.IsHeadlessLinux
	activateAfterLogin = activateV2Reporting
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
				activationServerURL := strings.TrimSpace(token.ServerURL)
				if activationServerURL == "" {
					activationServerURL = serverURL
				}
				if _, activationErr := activateAfterLogin(context.Background(), client.New(activationServerURL, token.AccessToken), activationServerURL, token.StableAuthSubject()); activationErr != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "Warning: login remains valid, but reporting activation is degraded: %v\n", activationErr)
				}
				cmd.Println("Already logged in. Use --force to re-login.")
				// Existing logins are the population that would otherwise never
				// get Pilot: they have no reason to run login again once it is
				// working, so the early return has to carry the setup too.
				ensurePilot(context.Background(), cmd.OutOrStdout(), cmd.ErrOrStderr())
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
			AuthSubject:  auth.SubjectFromAccessToken(result.AccessToken),
		}

		if err := invalidateMismatchedReportingConfig(serverURL, token.AuthSubject); err != nil {
			return fmt.Errorf("invalidate prior reporting credentials: %w", err)
		}
		if err := auth.WriteToken(tokenPath, token); err != nil {
			return fmt.Errorf("save token: %w", err)
		}
		if _, activationErr := activateAfterLogin(context.Background(), client.New(serverURL, result.AccessToken), serverURL, token.AuthSubject); activationErr != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: login succeeded, but reporting activation is degraded: %v\n", activationErr)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Login successful! Token saved to %s\n", tokenPath)
		ensurePilot(context.Background(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		fmt.Fprintln(cmd.OutOrStdout(), "Run 'ae-cli discover' to configure supported local AI tools.")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
	loginCmd.Flags().BoolVar(&loginForce, "force", false, "Force re-login even if already logged in")
	loginCmd.Flags().BoolVar(&loginDevice, "device", false, "Use OAuth device authorization flow")
}

func invalidateMismatchedReportingConfig(serverURL, authSubject string) error {
	current, err := reporting.Load("")
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return reporting.Delete("")
	}
	wantServer := strings.TrimRight(strings.TrimSpace(serverURL), "/")
	wantSubject := strings.TrimSpace(authSubject)
	currentServer := strings.TrimRight(strings.TrimSpace(current.ServerURL), "/")
	currentSubject := strings.TrimSpace(current.AuthSubject)
	if wantServer != "" && wantSubject != "" && currentServer == wantServer && currentSubject == wantSubject {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve user home: %w", err)
	}
	endpoint := codexOTLPEndpoint(currentServer)
	if _, _, err := toolconfig.DisableCodexOTLP(home, endpoint, current.OTLPToken); err != nil {
		return fmt.Errorf("remove prior Codex OTLP credentials: %w", err)
	}
	return reporting.Delete("")
}

func resolveLoginServerURL(cfg *config.Config, fallback string) string {
	if cfg != nil {
		if configured := strings.TrimSpace(cfg.Server.URL); configured != "" {
			return configured
		}
	}
	return strings.TrimSpace(fallback)
}
