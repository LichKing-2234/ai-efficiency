package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/spf13/cobra"
)

var (
	discoverProviderName string
	discoverDryRun       bool

	discoverInstalledTools   = toolconfig.DetectInstalledTools
	configureDiscoveredTools = toolconfig.ConfigureTools
	listProvidersForDiscover = defaultListProvidersForDiscover
	defaultDiscoverToolNames = []string{"codex", "claude", "gemini"}
)

var discoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Configure supported local AI tools from the current relay provider",
	RunE:  runDiscover,
}

func init() {
	rootCmd.AddCommand(discoverCmd)
	discoverCmd.Flags().StringVar(&discoverProviderName, "provider", "", "relay provider name to use instead of the primary provider")
	discoverCmd.Flags().BoolVar(&discoverDryRun, "dry-run", false, "show what would be configured without writing files")
}

func runDiscover(cmd *cobra.Command, args []string) error {
	configToken := ""
	if cfg != nil {
		configToken = cfg.Server.Token
	}
	if resolveToken(configToken, "") == "" {
		return fmt.Errorf("not logged in — run 'ae-cli login'")
	}

	providers, err := listProvidersForDiscover(context.Background())
	if err != nil {
		return err
	}
	selected, err := toolconfig.SelectProvider(mapProviders(providers), discoverProviderName)
	if err != nil {
		return err
	}

	tools, err := discoverInstalledTools(defaultDiscoverToolNames)
	if err != nil {
		return err
	}
	if len(tools) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No supported local tools detected on PATH.")
		return nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}

	result, err := configureDiscoveredTools(toolconfig.Options{
		HomeDir:   homeDir,
		ShellPath: os.Getenv("SHELL"),
		Provider:  selected,
		Tools:     tools,
		DryRun:    discoverDryRun,
	})
	if err != nil {
		return err
	}

	mode := "Configured"
	if discoverDryRun {
		mode = "Would configure"
	}
	fmt.Fprintf(cmd.OutOrStdout(), "%s provider %s for %d tool(s):\n", mode, selected.Name, len(result.Configured))
	for _, item := range result.Configured {
		fmt.Fprintf(cmd.OutOrStdout(), "  - %s\n", item.Name)
		for _, path := range item.Paths {
			fmt.Fprintf(cmd.OutOrStdout(), "    %s\n", path)
		}
	}
	return nil
}

func defaultListProvidersForDiscover(ctx context.Context) ([]client.ProviderInfo, error) {
	if apiClient == nil {
		return nil, fmt.Errorf("API client is not configured")
	}
	return apiClient.ListProviders(ctx)
}

func mapProviders(items []client.ProviderInfo) []toolconfig.Provider {
	out := make([]toolconfig.Provider, 0, len(items))
	for _, item := range items {
		out = append(out, toolconfig.Provider{
			Name:         item.Name,
			DisplayName:  item.DisplayName,
			BaseURL:      item.BaseURL,
			APIKey:       item.APIKey,
			APIKeyID:     item.APIKeyID,
			DefaultModel: item.DefaultModel,
			IsPrimary:    item.IsPrimary,
		})
	}
	return out
}
