package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/spf13/cobra"
)

var (
	cfgFile   string
	serverURL string
	cfg       *config.Config
	apiClient *client.Client
)

var refreshAccessToken = auth.RefreshAccessToken

var rootCmd = &cobra.Command{
	Use:   "ae-cli",
	Short: "AI Efficiency Platform CLI",
	Long:  "ae-cli is a command-line tool for interacting with the AI Efficiency Platform.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if commandSkipsConfig(cmd) {
			return nil
		}

		var err error
		cfg, err = config.Load(cfgFile)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		if serverURL != "" {
			cfg.Server.URL = serverURL
		}

		tokenPath, _ := auth.DefaultTokenPath()
		if tf := readTokenFile(tokenPath); tf != nil && cfg.Server.URL == "" && tf.ServerURL != "" {
			cfg.Server.URL = tf.ServerURL
		}

		tf, ok := resolveTokenFileWithRefresh(cfg.Server.URL, tokenPath)
		if ok && cfg.Server.URL == "" && tf.ServerURL != "" {
			cfg.Server.URL = tf.ServerURL
		}

		token := cfg.Server.Token
		if ok {
			token = tf.AccessToken
		} else {
			token = resolveToken(cfg.Server.Token, tokenPath)
		}
		apiClient = client.New(cfg.Server.URL, token)
		return nil
	},
}

func commandSkipsConfig(cmd *cobra.Command) bool {
	for current := cmd; current != nil; current = current.Parent() {
		switch current.Name() {
		case "version", "update":
			return true
		}
	}
	return false
}

// resolveToken returns the best available token.
// A valid token.json (from OAuth login) takes precedence. Falls back to configToken.
func resolveToken(configToken, tokenPath string) string {
	tf, ok := resolveTokenFile(tokenPath)
	if ok {
		return tf.AccessToken
	}
	return configToken
}

func resolveTokenFile(tokenPath string) (*auth.TokenFile, bool) {
	tf := readTokenFile(tokenPath)
	if tf == nil {
		return nil, false
	}
	if tf.IsValid() {
		return tf, true
	}
	return nil, false
}

func readTokenFile(tokenPath string) *auth.TokenFile {
	if tokenPath == "" {
		var err error
		tokenPath, err = auth.DefaultTokenPath()
		if err != nil {
			return nil
		}
	}

	tf, err := auth.ReadToken(tokenPath)
	if err != nil {
		return nil
	}
	return tf
}

func resolveTokenFileWithRefresh(serverURL, tokenPath string) (*auth.TokenFile, bool) {
	tf := readTokenFile(tokenPath)
	if tf == nil {
		return nil, false
	}
	if tf.IsValid() && !tf.NeedsRefresh() {
		return tf, true
	}

	targetServerURL := strings.TrimSpace(serverURL)
	if targetServerURL == "" {
		targetServerURL = strings.TrimSpace(tf.ServerURL)
	}
	if targetServerURL == "" || strings.TrimSpace(tf.RefreshToken) == "" {
		if tf.IsValid() {
			return tf, true
		}
		return nil, false
	}

	refreshed, err := refreshAccessToken(context.Background(), targetServerURL, tf.RefreshToken)
	if err != nil {
		if tf.IsValid() {
			return tf, true
		}
		return nil, false
	}

	nextRefreshToken := strings.TrimSpace(refreshed.RefreshToken)
	if nextRefreshToken == "" {
		nextRefreshToken = tf.RefreshToken
	}
	expiresIn := refreshed.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 7200
	}

	next := &auth.TokenFile{
		AccessToken:  refreshed.AccessToken,
		RefreshToken: nextRefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(expiresIn) * time.Second),
		ServerURL:    targetServerURL,
		AuthSubject:  firstNonEmpty(auth.SubjectFromAccessToken(refreshed.AccessToken), tf.StableAuthSubject()),
	}
	_ = auth.WriteToken(tokenPath, next)
	return next, true
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is ~/.ae-cli/config.yaml)")
	rootCmd.PersistentFlags().StringVar(&serverURL, "server", "", "efficiency platform server URL")
}
