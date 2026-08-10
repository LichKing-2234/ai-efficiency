package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
)

func TestActivateCompactAttributionIsIdempotentAndPreservesItsBaseline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, "gitconfig"))
	var installationID string
	var ensureCalls, enableCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			ensureCalls++
			var body client.EnsureInstallationRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode ensure request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			installationID = body.InstallationID
			if ensureCalls == 1 {
				_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporter_token":"test-reporter-token","otlp_token":"test-otlp-token"}}`, installationID)
				return
			}
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporting_enabled":true,"otel_enabled":true}}`, installationID)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/attribution/installations/"+installationID:
			enableCalls++
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporting_enabled":true,"otel_enabled":true}}`, installationID)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	userClient := client.New(server.URL, "test-user-token")
	first, err := activateCompactAttribution(context.Background(), userClient, server.URL, "user:123", true)
	if err != nil {
		t.Fatal(err)
	}
	state, err := attributionlocal.LoadCompactState()
	if err != nil {
		t.Fatal(err)
	}
	baseline := state.EnabledAt
	if !first.BaselineCreated || len(state.SeenAtoms) != 0 || baseline.After(time.Now().UTC()) {
		t.Fatalf("first activation result=%+v state=%+v", first, state)
	}

	second, err := activateCompactAttribution(context.Background(), userClient, server.URL, "user:123", true)
	if err != nil {
		t.Fatal(err)
	}
	state, err = attributionlocal.LoadCompactState()
	if err != nil {
		t.Fatal(err)
	}
	config, err := reporting.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if second.BaselineCreated || !state.EnabledAt.Equal(baseline) || len(state.SeenAtoms) != 0 {
		t.Fatalf("second activation result=%+v state=%+v", second, state)
	}
	if ensureCalls != 2 || enableCalls != 2 || !config.ReportingEnabled || !config.OTelEnabled {
		t.Fatalf("ensure=%d enable=%d config=%+v", ensureCalls, enableCalls, config)
	}
}

func TestEnsureReportingEnrollmentRotatesWhenLocalCredentialsAreMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var installationID string
	var ensureCalls, rotateCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			ensureCalls++
			var body struct {
				InstallationID string `json:"installation_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode ensure request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			installationID = body.InstallationID
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporting_enabled":true,"otel_enabled":false}}`, installationID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations/"+installationID+"/credentials/rotate":
			rotateCalls++
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporter_token":"test-reporter-token","otlp_token":"test-otlp-token","reporting_enabled":true,"otel_enabled":false}}`, installationID)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config, err := ensureReportingEnrollment(context.Background(), client.New(server.URL, "user-access-token"), server.URL, "user:123")
	if err != nil {
		t.Fatal(err)
	}
	if ensureCalls != 1 || rotateCalls != 1 || config.ReporterToken != "test-reporter-token" || config.OTLPToken != "test-otlp-token" {
		t.Fatalf("ensure=%d rotate=%d config=%+v", ensureCalls, rotateCalls, config)
	}
	path, err := os.Stat(filepath.Join(home, ".ae-cli", "reporting.json"))
	if err != nil {
		t.Fatal(err)
	}
	if path.Mode().Perm() != 0o600 {
		t.Fatalf("reporting config mode = %o, want 600", path.Mode().Perm())
	}
}

func TestEnsureReportingEnrollmentReusesStableInstallationAndRotatesAfterCredentialFileLoss(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var installationID string
	var ensureCalls, rotateCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			ensureCalls++
			var body struct {
				InstallationID string `json:"installation_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode ensure request: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if installationID == "" {
				installationID = body.InstallationID
				_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporter_token":"first-reporter-token","otlp_token":"first-otlp-token","reporting_enabled":true,"otel_enabled":true}}`, installationID)
				return
			}
			if body.InstallationID != installationID {
				t.Errorf("ensure installation_id = %q, want stable %q", body.InstallationID, installationID)
				w.WriteHeader(http.StatusConflict)
				return
			}
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporting_enabled":true,"otel_enabled":true}}`, installationID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations/"+installationID+"/credentials/rotate":
			rotateCalls++
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporter_token":"rotated-reporter-token","otlp_token":"rotated-otlp-token","reporting_enabled":true,"otel_enabled":true}}`, installationID)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	userClient := client.New(server.URL, "user-access-token")

	first, err := ensureReportingEnrollment(context.Background(), userClient, server.URL, "user:123")
	if err != nil {
		t.Fatal(err)
	}
	second, err := ensureReportingEnrollment(context.Background(), userClient, server.URL, "user:123")
	if err != nil {
		t.Fatal(err)
	}
	if first.InstallationID != second.InstallationID || second.ReporterToken != "first-reporter-token" || rotateCalls != 0 {
		t.Fatalf("repeated enrollment first=%+v second=%+v rotate_calls=%d", first, second, rotateCalls)
	}
	if err := os.Remove(filepath.Join(home, ".ae-cli", "reporting.json")); err != nil {
		t.Fatalf("remove credential config: %v", err)
	}
	recovered, err := ensureReportingEnrollment(context.Background(), userClient, server.URL, "user:123")
	if err != nil {
		t.Fatal(err)
	}
	if recovered.InstallationID != installationID || recovered.ReporterToken != "rotated-reporter-token" || recovered.OTLPToken != "rotated-otlp-token" {
		t.Fatalf("recovered config = %+v", recovered)
	}
	if ensureCalls != 3 || rotateCalls != 1 {
		t.Fatalf("ensure_calls=%d rotate_calls=%d, want 3/1", ensureCalls, rotateCalls)
	}
}
