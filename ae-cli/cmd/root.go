package cmd

import (
	"fmt"
	"os"

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

var rootCmd = &cobra.Command{
	Use:   "ae-cli",
	Short: "AI Efficiency Platform CLI",
	Long:  "ae-cli is a command-line tool for interacting with the AI Efficiency Platform.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Name() == "version" {
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

		token := resolveToken(cfg.Server.Token, "")
		apiClient = client.New(cfg.Server.URL, token)
		return nil
	},
}

// resolveToken returns the best available token.
// A valid token.json (from OAuth login) takes precedence. Falls back to configToken.
func resolveToken(configToken, tokenPath string) string {
	if tokenPath == "" {
		var err error
		tokenPath, err = auth.DefaultTokenPath()
		if err != nil {
			if configToken != "" {
				return configToken
			}
			return ""
		}
	}

	tf, err := auth.ReadToken(tokenPath)
	if err == nil && tf.IsValid() {
		return tf.AccessToken
	}

	return configToken
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
