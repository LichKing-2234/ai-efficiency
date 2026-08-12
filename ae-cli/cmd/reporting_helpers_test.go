package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/google/uuid"
)

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
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporting_enabled":true,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`, installationID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations/"+installationID+"/credentials/rotate":
			rotateCalls++
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporter_token":"test-reporter-token","otlp_token":"test-otlp-token","reporting_enabled":true,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`, installationID)
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
				_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporter_token":"first-reporter-token","otlp_token":"first-otlp-token","reporting_enabled":true,"otel_enabled":true,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`, installationID)
				return
			}
			if body.InstallationID != installationID {
				t.Errorf("ensure installation_id = %q, want stable %q", body.InstallationID, installationID)
				w.WriteHeader(http.StatusConflict)
				return
			}
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporting_enabled":true,"otel_enabled":true,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`, installationID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations/"+installationID+"/credentials/rotate":
			rotateCalls++
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporter_token":"rotated-reporter-token","otlp_token":"rotated-otlp-token","reporting_enabled":true,"otel_enabled":true,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`, installationID)
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

func TestEnsureReportingEnrollmentDoesNotRotateWhenOnlyLegacyOTLPTokenIsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installationID := uuid.NewString()
	if err := reporting.Save("", &reporting.Config{
		Version: 1, InstallationID: installationID, ServerURL: "https://ae.example.com", AuthSubject: "user:123",
		ReporterToken: "existing-reporter-token",
	}); err != nil {
		t.Fatal(err)
	}
	var rotateCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporting_enabled":false,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`, installationID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations/"+installationID+"/credentials/rotate":
			rotateCalls++
			http.Error(w, "unexpected rotation", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	got, err := ensureReportingEnrollment(context.Background(), client.New(server.URL, "user-access-token"), server.URL, "user:123")
	if err != nil {
		t.Fatal(err)
	}
	if rotateCalls != 0 || got.ReporterToken != "existing-reporter-token" || got.OTLPToken != "" {
		t.Fatalf("rotate_calls=%d config=%+v", rotateCalls, got)
	}
}

func TestActivateV2ReportingRetainsEnabledStateAndRetriesHooks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installationID := uuid.NewString()
	if err := reporting.Save("", &reporting.Config{Version: 1, InstallationID: installationID}); err != nil {
		t.Fatal(err)
	}
	var ensureCalls, enableCalls, rotateCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			ensureCalls++
			reporter := ""
			if ensureCalls == 1 {
				reporter = "reporter-token"
			}
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporter_token":%q,"reporting_enabled":%t,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`, installationID, reporter, ensureCalls > 1)
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/attribution/installations/"+installationID:
			enableCalls++
			_, _ = fmt.Fprintf(w, `{"code":0,"data":{"installation_id":%q,"reporting_enabled":true,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`, installationID)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations/"+installationID+"/credentials/rotate":
			rotateCalls++
			http.Error(w, "unexpected rotation", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	oldEnableHooks := enableGlobalReportingHooks
	var hookCalls int
	enableGlobalReportingHooks = func(hooks.InstallOptions) error {
		hookCalls++
		if hookCalls == 1 {
			return errors.New("hook install failed")
		}
		return nil
	}
	t.Cleanup(func() { enableGlobalReportingHooks = oldEnableHooks })
	userClient := client.New(server.URL, "user-access-token")

	if _, err := activateV2Reporting(context.Background(), userClient, server.URL, "user:123"); err == nil {
		t.Fatal("first activation error = nil, want hook warning")
	} else {
		var warning reportingActivationWarning
		if !errors.As(err, &warning) {
			t.Fatalf("first activation error = %T %v, want reportingActivationWarning", err, err)
		}
	}
	persisted, err := reporting.Load("")
	if err != nil || !persisted.ReportingEnabled || persisted.ReporterToken != "reporter-token" {
		t.Fatalf("persisted partial activation = %+v err=%v", persisted, err)
	}
	if _, err := activateV2Reporting(context.Background(), userClient, server.URL, "user:123"); err != nil {
		t.Fatalf("retry activation: %v", err)
	}
	if ensureCalls != 2 || enableCalls != 2 || hookCalls != 2 || rotateCalls != 0 {
		t.Fatalf("ensure=%d enable=%d hooks=%d rotate=%d", ensureCalls, enableCalls, hookCalls, rotateCalls)
	}
}
