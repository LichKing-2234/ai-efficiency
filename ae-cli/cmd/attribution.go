package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
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
		fmt.Fprintf(cmd.OutOrStdout(), "Compact Codex attribution enabled for installation %s.\n", reportingConfig.InstallationID)
		if reportingConfig.Protocol.V1WritePolicy == client.AttributionV1WritePolicyAccept {
			fmt.Fprintln(cmd.OutOrStdout(), "Baseline recorded; existing Token atoms will not be backfilled.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Formal v2 delivery active; a v1 baseline is not required.")
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
		fmt.Fprintf(cmd.OutOrStdout(), "Compact reporting: %t\n", config.ReportingEnabled)
		fmt.Fprintf(cmd.OutOrStdout(), "Legacy AE Codex OTLP: %t\n", config.OTelEnabled)
		state, stateErr := attributionlocal.LoadCompactState()
		if stateErr == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Tracking started: %s\n", state.EnabledAt.Format(time.RFC3339))
			fmt.Fprintf(cmd.OutOrStdout(), "Seen Token atoms: %d\n", len(state.SeenAtoms))
			fmt.Fprintf(cmd.OutOrStdout(), "Pending compact buckets: %d\n", len(state.Pending))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Global Git hooks: %s\n", globalHookSummary())
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
