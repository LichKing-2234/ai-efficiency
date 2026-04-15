package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoginDevicePollsUntilTokenIssued(t *testing.T) {
	polls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/device/code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "device-123",
				"user_code":        "ABCD-EFGH",
				"verification_uri": server.URL + "/oauth/device",
				"expires_in":       900,
				"interval":         5,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
				return
			}
			if polls == 2 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "slow_down"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-token",
				"refresh_token": "refresh-token",
				"token_type":    "Bearer",
				"expires_in":    7200,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var out bytes.Buffer
	var sleeps []time.Duration
	result, err := LoginDevice(context.Background(), OAuthConfig{
		ServerURL:  server.URL,
		ClientID:   "ae-cli",
		Timeout:    30 * time.Second,
		HTTPClient: server.Client(),
		Output:     &out,
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
		},
	})
	if err != nil {
		t.Fatalf("LoginDevice() error = %v", err)
	}
	if result.AccessToken != "access-token" {
		t.Fatalf("AccessToken = %q, want access-token", result.AccessToken)
	}
	if len(sleeps) != 2 || sleeps[0] != 5*time.Second || sleeps[1] != 10*time.Second {
		t.Fatalf("sleeps = %v, want [5s 10s]", sleeps)
	}
	if !strings.Contains(out.String(), "ABCD-EFGH") {
		t.Fatalf("output = %q, want user code", out.String())
	}
}

func TestLoginDeviceReturnsServerErrors(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth/device/code" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "device-123",
				"user_code":        "ABCD-EFGH",
				"verification_uri": server.URL + "/oauth/device",
				"expires_in":       900,
				"interval":         5,
			})
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "access_denied"})
	}))
	defer server.Close()

	_, err := LoginDevice(context.Background(), OAuthConfig{
		ServerURL:  server.URL,
		ClientID:   "ae-cli",
		Timeout:    5 * time.Second,
		HTTPClient: server.Client(),
		Output:     &bytes.Buffer{},
		Sleep:      func(time.Duration) {},
	})
	if err == nil || !strings.Contains(err.Error(), "access_denied") {
		t.Fatalf("err = %v, want access_denied", err)
	}
}

func TestLoginDeviceUsesMinimumPollInterval(t *testing.T) {
	polls := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/device/code":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "device-123",
				"user_code":        "ABCD-EFGH",
				"verification_uri": server.URL + "/oauth/device",
				"expires_in":       900,
				"interval":         0,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/token":
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "access-token",
				"refresh_token": "refresh-token",
				"token_type":    "Bearer",
				"expires_in":    7200,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	var sleeps []time.Duration
	_, err := LoginDevice(context.Background(), OAuthConfig{
		ServerURL:  server.URL,
		ClientID:   "ae-cli",
		Timeout:    30 * time.Second,
		HTTPClient: server.Client(),
		Output:     &bytes.Buffer{},
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
		},
	})
	if err != nil {
		t.Fatalf("LoginDevice() error = %v", err)
	}
	if len(sleeps) == 0 || sleeps[0] != time.Second {
		t.Fatalf("sleeps = %v, want first sleep of 1s", sleeps)
	}
}

func TestIsHeadlessLinux(t *testing.T) {
	if !IsHeadlessLinux(func(string) string { return "" }, "linux") {
		t.Fatal("expected linux with empty DISPLAY/WAYLAND_DISPLAY to be headless")
	}
	if IsHeadlessLinux(func(key string) string {
		if key == "DISPLAY" {
			return ":0"
		}
		return ""
	}, "linux") {
		t.Fatal("expected DISPLAY to disable headless hint")
	}
	if IsHeadlessLinux(func(string) string { return "" }, "darwin") {
		t.Fatal("expected non-linux OS not to trigger headless hint")
	}
}
