package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
)

var enableGlobalReportingHooks = hooks.EnableGlobal

type reportingActivationWarning struct {
	err error
}

func (w reportingActivationWarning) Error() string { return w.err.Error() }
func (w reportingActivationWarning) Unwrap() error { return w.err }

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
	if err := credentials.Protocol.Validate(); err != nil {
		return nil, fmt.Errorf("validate attribution protocol: %w", err)
	}
	config.ServerURL = strings.TrimRight(strings.TrimSpace(serverURL), "/")
	config.AuthSubject = strings.TrimSpace(authSubject)
	config.ReportingEnabled = credentials.ReportingEnabled
	config.OTelEnabled = credentials.OTelEnabled
	config.Protocol = credentials.Protocol
	if credentials.ReporterToken != "" {
		config.ReporterToken = credentials.ReporterToken
	}
	if credentials.OTLPToken != "" {
		config.OTLPToken = credentials.OTLPToken
	}
	if config.ReporterToken == "" {
		credentials, err = userClient.RotateAttributionInstallationCredentials(ctx, config.InstallationID)
		if err != nil {
			return nil, fmt.Errorf("recover unavailable installation credentials: %w", err)
		}
		if err := credentials.Protocol.Validate(); err != nil {
			return nil, fmt.Errorf("validate rotated attribution protocol: %w", err)
		}
		if credentials.Protocol != config.Protocol {
			return nil, fmt.Errorf("attribution protocol changed during credential recovery")
		}
		config.ReporterToken = credentials.ReporterToken
		config.OTLPToken = credentials.OTLPToken
		config.ReportingEnabled = credentials.ReportingEnabled
		config.OTelEnabled = credentials.OTelEnabled
		if config.ReporterToken == "" {
			return nil, fmt.Errorf("rotated installation credentials are unavailable")
		}
	}
	if err := reporting.Save(path, config); err != nil {
		return nil, err
	}
	return config, nil
}

func activateV2Reporting(ctx context.Context, userClient *client.Client, serverURL, authSubject string) (*reporting.Config, error) {
	config, err := ensureReportingEnrollment(ctx, userClient, serverURL, authSubject)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	if config.Protocol.V1WritePolicy == client.AttributionV1WritePolicyAccept {
		if _, err := attributionlocal.LoadCompactState(); os.IsNotExist(err) {
			if err := attributionlocal.InitializeCompactBaseline(ctx, now); err != nil {
				return config, fmt.Errorf("initialize Codex attribution baseline: %w", err)
			}
		} else if err != nil {
			return config, fmt.Errorf("load Codex attribution baseline: %w", err)
		}
	}

	enabled := true
	disabled := false
	response, err := userClient.SetAttributionInstallationEnabled(ctx, config.InstallationID, client.SetInstallationEnabledRequest{
		ReportingEnabled: &enabled,
		OTelEnabled:      &disabled,
	})
	if err != nil {
		return config, fmt.Errorf("enable reporting installation: %w", err)
	}
	if err := response.Protocol.Validate(); err != nil {
		return config, fmt.Errorf("validate enabled attribution protocol: %w", err)
	}
	if response.Protocol != config.Protocol {
		return config, fmt.Errorf("attribution protocol changed during activation")
	}
	config.ReportingEnabled = response.ReportingEnabled
	config.OTelEnabled = response.OTelEnabled
	if config.EnabledAt == nil {
		config.EnabledAt = &now
	}
	path, err := reporting.DefaultPath()
	if err != nil {
		return config, err
	}
	if err := reporting.Save(path, config); err != nil {
		return config, err
	}
	if !config.ReportingEnabled || config.OTelEnabled {
		return config, fmt.Errorf("server did not activate compact reporting with legacy Codex OTLP disabled")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return config, fmt.Errorf("resolve user home: %w", err)
	}
	if err := removeManagedCodexOTLPConfig(home, config); err != nil {
		return config, err
	}
	if err := enableGlobalReportingHooks(hooks.InstallOptions{NonInteractive: true, GeneratorVersion: buildinfo.Version}); err != nil {
		return config, reportingActivationWarning{err: fmt.Errorf("enable global managed hooks: %w", err)}
	}
	return config, nil
}

func loadEnabledReportingConfig() (*reporting.Config, bool) {
	config, err := reporting.Load("")
	if err != nil || config == nil || config.Protocol.Validate() != nil || !config.ReportingEnabled || strings.TrimSpace(config.ReporterToken) == "" {
		return nil, false
	}
	return config, true
}
