package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/spf13/cobra"
)

var (
	discoverProviderName string
	discoverDryRun       bool
	discoverToolNames    []string

	discoverInstalledTools   = toolconfig.DetectInstalledTools
	configureDiscoveredTools = toolconfig.ConfigureTools
	listProvidersForDiscover = defaultListProvidersForDiscover
	activateAfterDiscover    = activateCompactAttribution
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
	discoverCmd.Flags().StringArrayVar(&discoverToolNames, "tool", nil, "tool to configure even when not detected (codex, claude, gemini); may be repeated or comma-separated")
}

func runDiscover(cmd *cobra.Command, args []string) (returnErr error) {
	configToken := ""
	if cfg != nil {
		configToken = cfg.Server.Token
	}
	if resolveToken(configToken, "") == "" {
		return fmt.Errorf("not logged in — run 'ae-cli login'")
	}
	if !discoverDryRun {
		serverURL := ""
		if cfg != nil {
			serverURL = strings.TrimSpace(cfg.Server.URL)
		}
		authSubject := ""
		if token := readTokenFile(""); token != nil {
			serverURL = firstNonEmpty(token.ServerURL, serverURL)
			authSubject = token.StableAuthSubject()
		}
		defer func() {
			if returnErr == nil {
				runAutomaticAttributionActivation(context.Background(), activateAfterDiscover, apiClient, serverURL, authSubject, cmd.ErrOrStderr())
			}
		}()
	}

	providers, err := listProvidersForDiscover(context.Background())
	if err != nil {
		return err
	}
	selected, err := toolconfig.SelectProvider(mapProviders(providers), discoverProviderName)
	if err != nil {
		return err
	}

	tools, err := resolveDiscoverTools(discoverToolNames)
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
	if len(result.Configured) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No supported local tools matched provider %s credentials.\n", selected.Name)
		return nil
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
	if hasConfiguredTool(result, "gemini") && !discoverDryRun {
		fmt.Fprintln(cmd.OutOrStdout())
		fmt.Fprintln(cmd.OutOrStdout(), "Gemini uses shell environment variables.")
		fmt.Fprintln(cmd.OutOrStdout(), "For the current terminal, run:")
		fmt.Fprintf(cmd.OutOrStdout(), "  %s\n", geminiShellReloadCommand(result))
		fmt.Fprintln(cmd.OutOrStdout(), "Set GEMINI_MODEL so Gemini starts with the preview model directly.")
		fmt.Fprintln(cmd.OutOrStdout(), `  export GEMINI_MODEL="gemini-3.1-pro-preview"`)
		fmt.Fprintln(cmd.OutOrStdout(), "Do not switch models manually inside Gemini.")
	}
	return nil
}

func resolveDiscoverTools(explicit []string) ([]toolconfig.InstalledTool, error) {
	if len(explicit) == 0 {
		return discoverInstalledTools(defaultDiscoverToolNames)
	}

	supported := make(map[string]struct{}, len(defaultDiscoverToolNames))
	for _, name := range defaultDiscoverToolNames {
		supported[name] = struct{}{}
	}
	seen := make(map[string]struct{}, len(explicit))
	tools := make([]toolconfig.InstalledTool, 0, len(explicit))
	for _, occurrence := range explicit {
		for _, raw := range strings.Split(occurrence, ",") {
			name := strings.TrimSpace(raw)
			if _, ok := supported[name]; !ok {
				return nil, fmt.Errorf("unsupported tool %q; supported tools: %s", raw, strings.Join(defaultDiscoverToolNames, ", "))
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			tools = append(tools, toolconfig.InstalledTool{Name: name})
		}
	}
	return tools, nil
}

func hasConfiguredTool(result toolconfig.Result, name string) bool {
	for _, item := range result.Configured {
		if item.Name == name {
			return true
		}
	}
	return false
}

func geminiShellReloadCommand(result toolconfig.Result) string {
	for _, item := range result.Configured {
		if item.Name != "gemini" {
			continue
		}
		for _, path := range item.Paths {
			switch filepath.Base(path) {
			case ".zshrc":
				return `source "$HOME/.zshrc"`
			case ".bashrc":
				return `source "$HOME/.bashrc"`
			case ".profile":
				return `source "$HOME/.profile"`
			}
		}
	}
	return `source "$HOME/.zshrc"`
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
			Credentials:  mapProviderCredentials(item.Credentials),
		})
	}
	return out
}

func mapProviderCredentials(items []client.ProviderCredentialInfo) []toolconfig.PlatformCredential {
	out := make([]toolconfig.PlatformCredential, 0, len(items))
	for _, item := range items {
		out = append(out, toolconfig.PlatformCredential{
			Platform: item.Platform,
			GroupID:  item.GroupID,
			APIKey:   item.APIKey,
			APIKeyID: item.APIKeyID,
			Status:   item.Status,
		})
	}
	return out
}
