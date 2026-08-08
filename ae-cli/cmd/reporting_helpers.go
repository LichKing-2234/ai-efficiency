package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
)

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

func loadEnabledReportingConfig() (*reporting.Config, bool) {
	config, err := reporting.Load("")
	if err != nil || config == nil || !config.ReportingEnabled || strings.TrimSpace(config.ReporterToken) == "" {
		return nil, false
	}
	return config, true
}
