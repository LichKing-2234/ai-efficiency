package cmd

import (
	"fmt"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/proxy"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:    "start",
	Short:  "Legacy session workflow entrypoint (retired)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return legacyWorkflowRetiredError()
	},
}

var proxyServeInternalCmd = &cobra.Command{
	Use:    "proxy-serve-internal",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath, err := cmd.Flags().GetString("config")
		if err != nil {
			return err
		}
		if strings.TrimSpace(configPath) == "" {
			return fmt.Errorf("--config is required")
		}
		return proxy.ServeFromConfigFile(configPath)
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
	proxyServeInternalCmd.Flags().String("config", "", "internal proxy runtime config path")
	rootCmd.AddCommand(proxyServeInternalCmd)
}
