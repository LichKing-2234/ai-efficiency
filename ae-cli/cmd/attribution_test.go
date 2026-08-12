package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
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

func TestAttributionEnableFormalProtocolDoesNotClaimV1Baseline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			_, _ = w.Write([]byte(`{"code":0,"data":{"installation_id":"11111111-1111-4111-8111-111111111111","reporter_token":"reporter-secret","reporting_enabled":false,"otel_enabled":false,"protocol":{"ledger_epoch":"formal_v2","v1_write_policy":"upgrade_required","minimum_cli_version":"0.2.0-preview.5"}}}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/attribution/installations/"):
			_, _ = w.Write([]byte(`{"code":0,"data":{"installation_id":"11111111-1111-4111-8111-111111111111","reporting_enabled":true,"otel_enabled":false,"protocol":{"ledger_epoch":"formal_v2","v1_write_policy":"upgrade_required","minimum_cli_version":"0.2.0-preview.5"}}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)
	if err := auth.WriteToken(home+"/.ae-cli/token.json", &auth.TokenFile{
		AccessToken: "access-token", ExpiresAt: time.Now().Add(time.Hour), ServerURL: server.URL, AuthSubject: "user:123",
	}); err != nil {
		t.Fatal(err)
	}

	oldCfg := cfg
	oldClient := apiClient
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: server.URL, Token: "access-token"}}
	apiClient = client.New(server.URL, "access-token")
	var output bytes.Buffer
	attributionEnableCmd.SetOut(&output)
	attributionEnableCmd.SetErr(&output)
	if err := attributionEnableCmd.RunE(attributionEnableCmd, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Baseline recorded") {
		t.Fatalf("formal enable claimed a v1 baseline:\n%s", output.String())
	}
	if _, err := os.Stat(attributionlocal.CompactStatePath()); !os.IsNotExist(err) {
		t.Fatalf("formal enable created a v1 baseline: %v", err)
	}
}
