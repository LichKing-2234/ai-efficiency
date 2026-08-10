package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/spf13/cobra"
)

var attributionEnableOTel bool

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
		result, err := activateCompactAttribution(context.Background(), apiClient, token.ServerURL, token.StableAuthSubject(), attributionEnableOTel)
		if err != nil {
			return err
		}
		reportingConfig := result.Config
		printCompactHookWarning(cmd.ErrOrStderr(), result.HookWarning)
		fmt.Fprintf(cmd.OutOrStdout(), "Compact Codex attribution enabled for installation %s.\n", reportingConfig.InstallationID)
		if result.BaselineCreated {
			fmt.Fprintln(cmd.OutOrStdout(), "Baseline recorded; existing Token atoms will not be backfilled.")
		} else {
			fmt.Fprintln(cmd.OutOrStdout(), "Existing attribution baseline preserved.")
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Global Git hooks: %s\n", globalHookSummary())
		fmt.Fprintf(cmd.OutOrStdout(), "Codex Request ID correlation: %t\n", reportingConfig.OTelEnabled)
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
		fmt.Fprintf(cmd.OutOrStdout(), "Codex OTLP: %t\n", config.OTelEnabled)
		state, stateErr := attributionlocal.LoadCompactState()
		if stateErr == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "Tracking started: %s\n", state.EnabledAt.Format(time.RFC3339))
			fmt.Fprintf(cmd.OutOrStdout(), "Seen Token atoms: %d\n", len(state.SeenAtoms))
			fmt.Fprintf(cmd.OutOrStdout(), "Pending compact buckets: %d\n", len(state.Pending))
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Global Git hooks: %s\n", globalHookSummary())
		return nil
	},
}

func reconcileCodexOTLPConfig(home string, config *reporting.Config, enabled bool) error {
	if config == nil {
		return fmt.Errorf("reporting config is required")
	}
	endpoint := codexOTLPEndpoint(config.ServerURL)
	if enabled {
		if _, err := toolconfig.ConfigureCodexOTLP(home, endpoint, config.OTLPToken); err != nil {
			return fmt.Errorf("configure Codex OTLP: %w", err)
		}
		return nil
	}
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
	attributionEnableCmd.Flags().BoolVar(&attributionEnableOTel, "otel", true, "enable direct Codex trace-safe OTLP Request ID correlation")
	attributionCmd.AddCommand(attributionEnableCmd, attributionStatusCmd)
	rootCmd.AddCommand(attributionCmd)
}
