package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/spf13/cobra"
)

var attributionCmd = &cobra.Command{
	Use:   "attribution",
	Short: "Manage compact Codex Token attribution",
}

var attributionEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable compact Codex attribution and machine-level Git hooks",
	RunE: func(cmd *cobra.Command, args []string) error {
		if usableToken() == "" || apiClient == nil {
			return fmt.Errorf("not logged in; run ae-cli login")
		}
		tokenPath, _ := auth.DefaultTokenPath()
		token := readTokenFile(tokenPath)
		if token == nil {
			return fmt.Errorf("OAuth login state is required")
		}
		reportingConfig, err := activateV2Reporting(context.Background(), apiClient, token.ServerURL, token.StableAuthSubject())
		var warning reportingActivationWarning
		if errors.As(err, &warning) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: compact reporting is enabled, but global hooks could not be enabled: %v\n", warning.err)
			fmt.Fprintln(cmd.ErrOrStderr(), "Use 'ae-cli init --hooks repo' in repositories that need the fallback.")
		} else if err != nil {
			return fmt.Errorf("activate reporting: %w", err)
		}
		// The same step login performs. Enabling attribution without it leaves
		// the claim sync returning at its first guard — silently, with no gap
		// reason and nothing doctor reports — while this command says delivery
		// is active. Both entry points now have to earn that sentence.
		relayProviderID := ensureRelayProvider(context.Background(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		fmt.Fprintf(cmd.OutOrStdout(), "Compact Codex attribution enabled for installation %s.\n", reportingConfig.InstallationID)
		if relayProviderID <= 0 {
			fmt.Fprintln(cmd.OutOrStdout(), "V2 delivery is not active yet: no relay provider is recorded for this machine.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "V2 delivery active; a v1 baseline is not required.")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Global Git hooks: %s\n", globalHookSummary())
		fmt.Fprintln(cmd.OutOrStdout(), "Codex Request ID source: local Codex logs")
		return nil
	},
}

var attributionStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show compact attribution setup status",
	RunE: func(cmd *cobra.Command, args []string) error {
		config, err := reporting.Load("")
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintln(cmd.OutOrStdout(), "Compact attribution: not enrolled")
				return nil
			}
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Installation: %s\n", config.InstallationID)
		fmt.Fprintf(cmd.OutOrStdout(), "V2 reporting: %t\n", config.ReportingEnabled)
		fmt.Fprintf(cmd.OutOrStdout(), "Global Git hooks: %s\n", globalHookSummary())
		if attrCtx, detectErr := detectAttributionContext(); detectErr == nil {
			task, loadErr := hooks.LoadSyncTask(attrCtx.workspaceID)
			if loadErr == nil {
				printSyncTaskStatus(cmd.OutOrStdout(), task)
			}
		}
		if err := printMachineSyncTaskStatus(cmd.OutOrStdout()); err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Machine Sync Tasks: unavailable (%v)\n", err)
		}
		if err := printV2ClaimDeliveryStatus(cmd.OutOrStdout()); err != nil {
			fmt.Fprintf(cmd.OutOrStdout(), "V2 Claim Delivery: unavailable (%v)\n", err)
		}
		return nil
	},
}

func removeManagedCodexOTLPConfig(home string, config *reporting.Config) error {
	if config == nil {
		return fmt.Errorf("reporting config is required")
	}
	endpoint := codexOTLPEndpoint(config.ServerURL)
	if _, _, err := toolconfig.DisableCodexOTLP(home, endpoint, config.OTLPToken); err != nil {
		return fmt.Errorf("disable Codex OTLP: %w", err)
	}
	return nil
}

func codexOTLPEndpoint(serverURL string) string {
	return strings.TrimRight(strings.TrimSpace(serverURL), "/") + "/api/v1/attribution/otel/v1/traces"
}

func globalHookSummary() string {
	status, err := hooks.StatusForRepo(hooks.StatusOptions{CWD: ".", Binding: currentHookBinding()})
	if err != nil {
		return "unknown"
	}
	return string(status.EffectiveMode)
}

func init() {
	attributionCmd.AddCommand(attributionEnableCmd, attributionStatusCmd)
	rootCmd.AddCommand(attributionCmd)
}
