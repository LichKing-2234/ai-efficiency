package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
)

type compactActivationResult struct {
	Config          *reporting.Config
	BaselineCreated bool
	HookWarning     error
}

type compactActivationFunc func(context.Context, *client.Client, string, string, bool) (compactActivationResult, error)

func ensureReportingEnrollment(ctx context.Context, userClient *client.Client, serverURL, authSubject string) (*reporting.Config, error) {
	if userClient == nil {
		return nil, fmt.Errorf("user client is required")
	}
	path, err := reporting.DefaultPath()
	if err != nil {
		return nil, err
	}
	config, err := reporting.LoadOrCreate(path)
	if err != nil {
		return nil, err
	}
	hostname, _ := os.Hostname()
	credentials, err := userClient.EnsureAttributionInstallation(ctx, client.EnsureInstallationRequest{
		InstallationID: config.InstallationID,
		Label:          strings.TrimSpace(hostname),
		ClientVersion:  buildinfo.Version,
	})
	if err != nil {
		return nil, err
	}
	config.ServerURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	config.AuthSubject = strings.TrimSpace(authSubject)
	config.ReportingEnabled = credentials.ReportingEnabled
	config.OTelEnabled = credentials.OTelEnabled
	if credentials.ReporterToken != "" {
		config.ReporterToken = credentials.ReporterToken
	}
	if credentials.OTLPToken != "" {
		config.OTLPToken = credentials.OTLPToken
	}
	if config.ReporterToken == "" || config.OTLPToken == "" {
		credentials, err = userClient.RotateAttributionInstallationCredentials(ctx, config.InstallationID)
		if err != nil {
			return nil, fmt.Errorf("recover unavailable installation credentials: %w", err)
		}
		config.ReporterToken = credentials.ReporterToken
		config.OTLPToken = credentials.OTLPToken
		config.ReportingEnabled = credentials.ReportingEnabled
		config.OTelEnabled = credentials.OTelEnabled
		if config.ReporterToken == "" || config.OTLPToken == "" {
			return nil, fmt.Errorf("rotated installation credentials are unavailable")
		}
	}
	if err := reporting.Save(path, config); err != nil {
		return nil, err
	}
	return config, nil
}

func activateCompactAttribution(ctx context.Context, userClient *client.Client, serverURL, authSubject string, otelEnabled bool) (compactActivationResult, error) {
	var result compactActivationResult
	config, err := ensureReportingEnrollment(ctx, userClient, serverURL, authSubject)
	if err != nil {
		return result, fmt.Errorf("enroll reporting installation: %w", err)
	}

	now := time.Now().UTC()
	enabledAt := now
	state, err := attributionlocal.LoadCompactState()
	if os.IsNotExist(err) {
		if err := attributionlocal.InitializeCompactBaseline(ctx, now); err != nil {
			return result, fmt.Errorf("initialize Codex attribution baseline: %w", err)
		}
		result.BaselineCreated = true
	} else if err != nil {
		return result, fmt.Errorf("load Codex attribution baseline: %w", err)
	} else {
		enabledAt = state.EnabledAt
	}

	enabled := true
	response, err := userClient.SetAttributionInstallationEnabled(ctx, config.InstallationID, client.SetInstallationEnabledRequest{
		ReportingEnabled: &enabled,
		OTelEnabled:      &otelEnabled,
	})
	if err != nil {
		return result, fmt.Errorf("enable reporting installation: %w", err)
	}
	config.ReportingEnabled = response.ReportingEnabled
	config.OTelEnabled = response.OTelEnabled
	if config.EnabledAt == nil {
		config.EnabledAt = &enabledAt
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return result, fmt.Errorf("resolve user home: %w", err)
	}
	if err := reconcileCodexOTLPConfig(home, config, config.OTelEnabled); err != nil {
		return result, err
	}
	if err := reporting.Save("", config); err != nil {
		return result, err
	}
	result.Config = config
	result.HookWarning = hooks.EnableGlobal(hooks.InstallOptions{NonInteractive: true, GeneratorVersion: buildinfo.Version})
	return result, nil
}

func runAutomaticAttributionActivation(ctx context.Context, activate compactActivationFunc, userClient *client.Client, serverURL, authSubject string, stderr io.Writer) {
	result, err := activate(ctx, userClient, serverURL, authSubject, true)
	if err != nil {
		fmt.Fprintf(stderr, "Warning: automatic attribution setup is degraded: %v\n", err)
		return
	}
	printCompactHookWarning(stderr, result.HookWarning)
}

func printCompactHookWarning(stderr io.Writer, err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(stderr, "Warning: compact reporting is enabled, but global hooks could not be enabled: %v\n", err)
	fmt.Fprintln(stderr, "Use 'ae-cli init --hooks repo' in repositories that need the fallback.")
}

func loadEnabledReportingConfig() (*reporting.Config, bool) {
	config, err := reporting.Load("")
	if err != nil || config == nil || !config.ReportingEnabled || strings.TrimSpace(config.ReporterToken) == "" {
		return nil, false
	}
	return config, true
}
