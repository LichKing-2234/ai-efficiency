package cmd

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestResolveLoginServerURLPrefersLoadedConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			URL: "http://localhost:18081",
		},
	}

	got := resolveLoginServerURL(cfg, "http://localhost:8081")
	if got != "http://localhost:18081" {
		t.Fatalf("resolveLoginServerURL() = %q, want %q", got, "http://localhost:18081")
	}
}

func TestResolveLoginServerURLFallsBackToBuildInfoDefault(t *testing.T) {
	got := resolveLoginServerURL(nil, "http://localhost:8081")
	if got != "http://localhost:8081" {
		t.Fatalf("resolveLoginServerURL() = %q, want %q", got, "http://localhost:8081")
	}
}

func TestResolveLoginServerURLIgnoresBlankConfiguredValue(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			URL: "   ",
		},
	}

	got := resolveLoginServerURL(cfg, "  http://localhost:8081  ")
	if got != "http://localhost:8081" {
		t.Fatalf("resolveLoginServerURL() = %q, want %q", got, "http://localhost:8081")
	}
}

func TestLoginCommandSkipsOAuthWhenValidTokenExists(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldForce := loginForce
	oldLogin := loginFlow
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		loginForce = oldForce
		loginFlow = oldLogin
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginForce = false

	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		t.Fatalf("DefaultTokenPath: %v", err)
	}
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		ServerURL:    "http://localhost:18081",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	called := false
	loginFlow = func(ctx context.Context, cfg auth.OAuthConfig) (*auth.OAuthResult, error) {
		called = true
		return nil, nil
	}

	buf := new(bytes.Buffer)
	loginCmd.SetOut(buf)

	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login RunE: %v", err)
	}
	if called {
		t.Fatal("expected OAuth login flow to be skipped when a valid token already exists")
	}
	if got := buf.String(); !strings.Contains(got, "Already logged in. Use --force to re-login.") {
		t.Fatalf("output = %q, want already logged in message", got)
	}
}

func TestLoginCommandActivatesReportingWhenValidTokenExists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	var ensureCalls, enableCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			ensureCalls++
			var request client.EnsureInstallationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode ensure request: %v", err)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"installation_id":"` + request.InstallationID + `","reporter_token":"reporter-secret","otlp_token":"legacy-otlp-secret","created":true,"reporting_enabled":false,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/attribution/installations/"):
			enableCalls++
			var request client.SetInstallationEnabledRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatalf("decode enable request: %v", err)
			}
			if request.ReportingEnabled == nil || !*request.ReportingEnabled || request.OTelEnabled == nil || *request.OTelEnabled {
				t.Fatalf("enable request = %+v, want reporting=true otel=false", request)
			}
			installationID := strings.TrimPrefix(r.URL.Path, "/api/v1/attribution/installations/")
			_, _ = w.Write([]byte(`{"code":0,"data":{"installation_id":"` + installationID + `","reporting_enabled":true,"otel_enabled":false,"protocol":{"ledger_epoch":"shadow_v2","v1_write_policy":"accept"}}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldLogin := loginFlow
	t.Cleanup(func() {
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		loginFlow = oldLogin
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: server.URL}}
	loginForce = false
	loginDevice = false
	loginFlow = func(context.Context, auth.OAuthConfig) (*auth.OAuthResult, error) {
		t.Fatal("valid login must not start OAuth")
		return nil, nil
	}
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken: "access-token", ExpiresAt: time.Now().Add(time.Hour), ServerURL: server.URL, AuthSubject: "user:123",
	}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	loginCmd.SetOut(&stdout)
	loginCmd.SetErr(&stderr)
	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login RunE: %v", err)
	}
	if ensureCalls != 1 || enableCalls != 1 {
		t.Fatalf("reporting calls ensure=%d enable=%d, want 1/1; stdout=%q stderr=%q", ensureCalls, enableCalls, stdout.String(), stderr.String())
	}
	reportingConfig, err := reporting.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if !reportingConfig.ReportingEnabled || reportingConfig.ReporterToken != "reporter-secret" || reportingConfig.EnabledAt == nil {
		t.Fatalf("reporting config = %+v, want enabled reporter config with baseline time", reportingConfig)
	}
	if _, err := attributionlocal.LoadCompactState(); err != nil {
		t.Fatalf("load compact baseline: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".ae-cli", "git-hooks", "post-commit")); err != nil {
		t.Fatalf("global managed hook not installed: %v", err)
	}
}

func TestLoginCommandDoesNotReactivateRevokedReportingInstallation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	installationID := "11111111-1111-4111-8111-111111111111"
	if err := reporting.Save("", &reporting.Config{Version: 1, InstallationID: installationID}); err != nil {
		t.Fatal(err)
	}
	var ensureCalls, enableCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			ensureCalls++
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":409,"message":"reporting installation is revoked"}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/attribution/installations/"):
			enableCalls++
			t.Fatal("revoked installation reached enable endpoint")
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldLogin := loginFlow
	t.Cleanup(func() {
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		loginFlow = oldLogin
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: server.URL}}
	loginForce = false
	loginDevice = false
	loginFlow = func(context.Context, auth.OAuthConfig) (*auth.OAuthResult, error) {
		t.Fatal("valid login must not start OAuth")
		return nil, nil
	}
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken: "access-token", ExpiresAt: time.Now().Add(time.Hour), ServerURL: server.URL, AuthSubject: "user:123",
	}); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	loginCmd.SetOut(&stdout)
	loginCmd.SetErr(&stderr)
	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("valid login should remain usable: %v", err)
	}
	if ensureCalls != 1 || enableCalls != 0 || !strings.Contains(stderr.String(), "reporting activation is degraded") {
		t.Fatalf("ensure=%d enable=%d stderr=%q", ensureCalls, enableCalls, stderr.String())
	}
}

func TestLoginCommandPersistsFormalProtocolWithoutCreatingV1Baseline(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/installations":
			var request client.EnsureInstallationRequest
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"code":0,"data":{"installation_id":"` + request.InstallationID + `","reporter_token":"reporter-secret","created":true,"reporting_enabled":false,"otel_enabled":false,"protocol":{"ledger_epoch":"formal_v2","v1_write_policy":"upgrade_required","minimum_cli_version":"0.2.0-preview.5"}}}`))
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/attribution/installations/"):
			installationID := strings.TrimPrefix(r.URL.Path, "/api/v1/attribution/installations/")
			_, _ = w.Write([]byte(`{"code":0,"data":{"installation_id":"` + installationID + `","reporting_enabled":true,"otel_enabled":false,"protocol":{"ledger_epoch":"formal_v2","v1_write_policy":"upgrade_required","minimum_cli_version":"0.2.0-preview.5"}}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldLogin := loginFlow
	oldActivate := activateAfterLogin
	t.Cleanup(func() {
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		loginFlow = oldLogin
		activateAfterLogin = oldActivate
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: server.URL}}
	loginForce = false
	loginDevice = false
	activateAfterLogin = activateV2Reporting
	loginFlow = func(context.Context, auth.OAuthConfig) (*auth.OAuthResult, error) {
		t.Fatal("valid login must not start OAuth")
		return nil, nil
	}
	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{AccessToken: "access-token", ExpiresAt: time.Now().Add(time.Hour), ServerURL: server.URL, AuthSubject: "user:123"}); err != nil {
		t.Fatal(err)
	}

	loginCmd.SetOut(new(bytes.Buffer))
	loginCmd.SetErr(new(bytes.Buffer))
	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatal(err)
	}
	path, err := reporting.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"ledger_epoch": "formal_v2"`, `"v1_write_policy": "upgrade_required"`, `"minimum_cli_version": "0.2.0-preview.5"`} {
		if !bytes.Contains(payload, []byte(want)) {
			t.Fatalf("reporting config missing %s:\n%s", want, payload)
		}
	}
	if _, err := attributionlocal.LoadCompactState(); !os.IsNotExist(err) {
		t.Fatalf("formal activation created or loaded a v1 baseline: %v", err)
	}
}

func TestLoginCommandForceBypassesExistingToken(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldForce := loginForce
	oldLogin := loginFlow
	oldHeadless := headlessBrowserEnv
	oldEnroll := activateAfterLogin
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		loginForce = oldForce
		loginFlow = oldLogin
		headlessBrowserEnv = oldHeadless
		activateAfterLogin = oldEnroll
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginForce = true
	headlessBrowserEnv = func(func(string) string, string) bool { return false }

	tokenPath, err := auth.DefaultTokenPath()
	if err != nil {
		t.Fatalf("DefaultTokenPath: %v", err)
	}
	if err := auth.WriteToken(tokenPath, &auth.TokenFile{
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		ServerURL:    "http://localhost:18081",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}

	called := false
	loginFlow = func(ctx context.Context, cfg auth.OAuthConfig) (*auth.OAuthResult, error) {
		called = true
		return &auth.OAuthResult{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			ExpiresIn:    3600,
		}, nil
	}
	activateAfterLogin = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		return &reporting.Config{}, nil
	}

	buf := new(bytes.Buffer)
	loginCmd.SetOut(buf)

	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login RunE: %v", err)
	}
	if !called {
		t.Fatal("expected OAuth login flow to run when --force is enabled")
	}

	saved, err := auth.ReadToken(tokenPath)
	if err != nil {
		t.Fatalf("ReadToken: %v", err)
	}
	if saved.AccessToken != "new-access-token" {
		t.Fatalf("access_token = %q, want %q", saved.AccessToken, "new-access-token")
	}
}

func TestLoginCommandKeepsSuccessfulLoginWhenEnrollmentDegrades(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldLogin := loginFlow
	oldHeadless := headlessBrowserEnv
	oldEnroll := activateAfterLogin
	t.Cleanup(func() {
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		loginFlow = oldLogin
		headlessBrowserEnv = oldHeadless
		activateAfterLogin = oldEnroll
	})
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginForce = true
	loginDevice = false
	headlessBrowserEnv = func(func(string) string, string) bool { return false }
	loginFlow = func(context.Context, auth.OAuthConfig) (*auth.OAuthResult, error) {
		return &auth.OAuthResult{AccessToken: "new-access-token", RefreshToken: "new-refresh-token", ExpiresIn: 3600}, nil
	}
	activateAfterLogin = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		return nil, context.DeadlineExceeded
	}

	stdout := new(bytes.Buffer)
	stderr := new(bytes.Buffer)
	loginCmd.SetOut(stdout)
	loginCmd.SetErr(stderr)
	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login should succeed despite degraded enrollment: %v", err)
	}
	if !strings.Contains(stdout.String(), "Login successful!") || !strings.Contains(stderr.String(), "login succeeded, but reporting activation is degraded") {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	tokenPath, _ := auth.DefaultTokenPath()
	saved, err := auth.ReadToken(tokenPath)
	if err != nil || saved.AccessToken != "new-access-token" {
		t.Fatalf("saved login token = %+v err=%v", saved, err)
	}
}

func TestLoginCommandClearsPriorAccountReportingCredentialsBeforeDegradedEnrollment(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldLogin := loginFlow
	oldHeadless := headlessBrowserEnv
	oldEnroll := activateAfterLogin
	t.Cleanup(func() {
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		loginFlow = oldLogin
		headlessBrowserEnv = oldHeadless
		activateAfterLogin = oldEnroll
	})

	serverURL := "http://localhost:18081"
	oldEndpoint := serverURL + "/api/v1/attribution/otel/v1/traces"
	if _, err := toolconfig.ConfigureCodexOTLP(tmpHome, oldEndpoint, "old-otlp-token"); err != nil {
		t.Fatal(err)
	}
	if err := reporting.Save("", &reporting.Config{
		InstallationID:   "11111111-1111-4111-8111-111111111111",
		ServerURL:        serverURL,
		AuthSubject:      "user:123",
		ReporterToken:    "old-reporter-token",
		OTLPToken:        "old-otlp-token",
		ReportingEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	cfg = &config.Config{Server: config.ServerConfig{URL: serverURL}}
	loginForce = true
	loginDevice = false
	headlessBrowserEnv = func(func(string) string, string) bool { return false }
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"user_id":456,"type":"access"}`))
	loginFlow = func(context.Context, auth.OAuthConfig) (*auth.OAuthResult, error) {
		return &auth.OAuthResult{AccessToken: "header." + payload + ".signature", RefreshToken: "new-refresh-token", ExpiresIn: 3600}, nil
	}
	activateAfterLogin = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		return nil, context.DeadlineExceeded
	}

	loginCmd.SetOut(new(bytes.Buffer))
	loginCmd.SetErr(new(bytes.Buffer))
	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login should succeed despite degraded enrollment: %v", err)
	}
	if _, err := reporting.Load(""); !os.IsNotExist(err) {
		t.Fatalf("prior account reporting credentials survived account switch: %v", err)
	}
	inspection, err := toolconfig.InspectCodexOTLP(tmpHome, oldEndpoint, "old-otlp-token")
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Configured || inspection.CredentialAvailable {
		t.Fatalf("prior account Codex OTLP credentials survived account switch: %+v", inspection)
	}
}

func TestLoginCommandUsesDeviceFlowWhenFlagSet(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldBrowser := loginFlow
	oldDeviceFlow := loginDeviceFlow
	oldHeadless := headlessBrowserEnv
	oldEnroll := activateAfterLogin
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		loginFlow = oldBrowser
		loginDeviceFlow = oldDeviceFlow
		headlessBrowserEnv = oldHeadless
		activateAfterLogin = oldEnroll
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginDevice = true
	loginForce = true
	headlessBrowserEnv = func(func(string) string, string) bool { return false }

	calledBrowser := false
	calledDevice := false
	loginFlow = func(ctx context.Context, cfg auth.OAuthConfig) (*auth.OAuthResult, error) {
		calledBrowser = true
		return nil, nil
	}
	loginDeviceFlow = func(ctx context.Context, cfg auth.OAuthConfig) (*auth.OAuthResult, error) {
		calledDevice = true
		return &auth.OAuthResult{
			AccessToken:  "device-access-token",
			RefreshToken: "device-refresh-token",
			ExpiresIn:    3600,
		}, nil
	}
	activateAfterLogin = func(context.Context, *client.Client, string, string) (*reporting.Config, error) {
		return &reporting.Config{}, nil
	}

	if err := loginCmd.RunE(loginCmd, nil); err != nil {
		t.Fatalf("login RunE: %v", err)
	}
	if calledBrowser {
		t.Fatal("browser flow should not run when --device is set")
	}
	if !calledDevice {
		t.Fatal("device flow should run when --device is set")
	}
}

func TestLoginCommandSuggestsDeviceFlowInHeadlessLinux(t *testing.T) {
	tmpHome := t.TempDir()
	oldHome := os.Getenv("HOME")
	oldCfg := cfg
	oldForce := loginForce
	oldDevice := loginDevice
	oldHeadless := headlessBrowserEnv
	oldBrowser := loginFlow
	defer func() {
		_ = os.Setenv("HOME", oldHome)
		cfg = oldCfg
		loginForce = oldForce
		loginDevice = oldDevice
		headlessBrowserEnv = oldHeadless
		loginFlow = oldBrowser
	}()

	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("Setenv(HOME): %v", err)
	}
	cfg = &config.Config{Server: config.ServerConfig{URL: "http://localhost:18081"}}
	loginForce = true
	loginDevice = false
	headlessBrowserEnv = func(func(string) string, string) bool { return true }
	loginFlow = func(context.Context, auth.OAuthConfig) (*auth.OAuthResult, error) {
		t.Fatal("browser flow should not run in headless mode")
		return nil, nil
	}

	err := loginCmd.RunE(loginCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "ae-cli login --device") {
		t.Fatalf("err = %v, want device-flow guidance", err)
	}
}
