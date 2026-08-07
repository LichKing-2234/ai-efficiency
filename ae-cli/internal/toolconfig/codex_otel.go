package toolconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

type CodexOTLPInspection struct {
	Configured            bool
	EndpointMatches       bool
	ProtocolJSON          bool
	CredentialAvailable   bool
	CredentialMatches     bool
	PromptLoggingDisabled bool
	TraceOnly             bool
}

func (i CodexOTLPInspection) Healthy() bool {
	return i.Configured && i.EndpointMatches && i.ProtocolJSON && i.CredentialMatches && i.PromptLoggingDisabled && i.TraceOnly
}

// ConfigureCodexOTLP enables Codex native trace-safe OTLP/HTTP JSON export.
// Prompt logging remains disabled and no local Collector is installed.
func ConfigureCodexOTLP(homeDir, endpoint, token string) (string, error) {
	endpoint = strings.TrimSpace(endpoint)
	token = strings.TrimSpace(token)
	if endpoint == "" || token == "" {
		return "", fmt.Errorf("Codex OTLP endpoint and token are required")
	}
	path := filepath.Join(homeDir, ".codex", "config.toml")
	config := map[string]any{}
	if payload, err := os.ReadFile(path); err == nil && len(payload) > 0 {
		if err := toml.Unmarshal(payload, &config); err != nil {
			return "", fmt.Errorf("parse Codex config: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	otel := ensureNestedMap(config, "otel")
	otel["environment"] = "production"
	otel["log_user_prompt"] = false
	otel["trace_exporter"] = map[string]any{
		"otlp-http": map[string]any{
			"endpoint": endpoint,
			"protocol": "json",
			"headers": map[string]any{
				"Authorization": "Bearer " + token,
			},
		},
	}
	payload, err := toml.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("marshal Codex config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func InspectCodexOTLP(homeDir, expectedEndpoint, expectedToken string) (CodexOTLPInspection, error) {
	var inspection CodexOTLPInspection
	path := filepath.Join(homeDir, ".codex", "config.toml")
	payload, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return inspection, nil
	}
	if err != nil {
		return inspection, err
	}
	var config map[string]any
	if err := toml.Unmarshal(payload, &config); err != nil {
		return inspection, fmt.Errorf("parse Codex config: %w", err)
	}
	otel, ok := config["otel"].(map[string]any)
	if !ok {
		return inspection, nil
	}
	inspection.PromptLoggingDisabled = otel["log_user_prompt"] == false
	_, hasMetrics := otel["metrics_exporter"]
	_, hasLogs := otel["log_exporter"]
	inspection.TraceOnly = !hasMetrics && !hasLogs
	traceExporters, ok := otel["trace_exporter"].(map[string]any)
	if !ok {
		return inspection, nil
	}
	otlpHTTP, ok := traceExporters["otlp-http"].(map[string]any)
	if !ok {
		return inspection, nil
	}
	inspection.Configured = true
	endpoint, _ := otlpHTTP["endpoint"].(string)
	protocol, _ := otlpHTTP["protocol"].(string)
	inspection.EndpointMatches = strings.TrimSpace(endpoint) == strings.TrimSpace(expectedEndpoint)
	inspection.ProtocolJSON = strings.EqualFold(strings.TrimSpace(protocol), "json")
	headers, _ := otlpHTTP["headers"].(map[string]any)
	authorization, _ := headers["Authorization"].(string)
	inspection.CredentialAvailable = strings.TrimSpace(authorization) != ""
	inspection.CredentialMatches = strings.TrimSpace(expectedToken) != "" && strings.TrimSpace(authorization) == "Bearer "+strings.TrimSpace(expectedToken)
	return inspection, nil
}
