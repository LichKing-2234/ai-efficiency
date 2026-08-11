package cmd

import (
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
)

func TestReconcileCodexOTLPConfigDisablesManagedExporter(t *testing.T) {
	home := t.TempDir()
	config := &reporting.Config{
		ServerURL: "https://ae.example.com",
		OTLPToken: "test-otlp-token",
	}
	endpoint := config.ServerURL + "/api/v1/attribution/otel/v1/traces"
	if _, err := toolconfig.ConfigureCodexOTLP(home, endpoint, config.OTLPToken); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedCodexOTLPConfig(home, config); err != nil {
		t.Fatal(err)
	}
	inspection, err := toolconfig.InspectCodexOTLP(home, endpoint, config.OTLPToken)
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Configured || inspection.CredentialAvailable {
		t.Fatalf("managed Codex OTLP exporter survived disable: %+v", inspection)
	}
}

func TestReconcileCodexOTLPConfigPreservesUserManagedExporter(t *testing.T) {
	home := t.TempDir()
	config := &reporting.Config{ServerURL: "https://ae.example.com", OTLPToken: "ae-token"}
	userEndpoint := "https://telemetry.example.org/v1/traces"
	if _, err := toolconfig.ConfigureCodexOTLP(home, userEndpoint, "user-token"); err != nil {
		t.Fatal(err)
	}
	if err := removeManagedCodexOTLPConfig(home, config); err != nil {
		t.Fatal(err)
	}
	inspection, err := toolconfig.InspectCodexOTLP(home, userEndpoint, "user-token")
	if err != nil {
		t.Fatal(err)
	}
	if !inspection.Healthy() {
		t.Fatalf("user-managed Codex OTLP exporter was changed: %+v", inspection)
	}
}
