package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestDoJSONSendsAndDecodesSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q, want application/json", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want bearer token", got)
		}
		var req map[string]string
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req["name"] != "alice" {
			t.Fatalf("request = %+v, want name alice", req)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	var out struct {
		Status string `json:"status"`
	}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token")
	err := DoJSON(context.Background(), server.Client(), http.MethodPost, server.URL, map[string]string{"name": "alice"}, &out, Options{Headers: headers})
	if err != nil {
		t.Fatalf("DoJSON() error = %v", err)
	}
	if out.Status != "ok" {
		t.Fatalf("Status = %q, want ok", out.Status)
	}
}

func TestDoFormSendsAndDecodesSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Fatalf("Content-Type = %q, want form", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("client_id"); got != "ae-cli" {
			t.Fatalf("client_id = %q, want ae-cli", got)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"device_code": "device-123"})
	}))
	defer server.Close()

	var out struct {
		DeviceCode string `json:"device_code"`
	}
	err := DoForm(context.Background(), server.Client(), http.MethodPost, server.URL, url.Values{"client_id": {"ae-cli"}}, &out, Options{})
	if err != nil {
		t.Fatalf("DoForm() error = %v", err)
	}
	if out.DeviceCode != "device-123" {
		t.Fatalf("DeviceCode = %q, want device-123", out.DeviceCode)
	}
}

func TestStatusErrorSummarizesOAuthErrorDescription(t *testing.T) {
	err := runErrorServer(t, http.StatusBadRequest, `{"error":"access_denied","error_description":"authorization denied by user"}`, Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Summary != "access_denied: authorization denied by user" {
		t.Fatalf("Summary = %q", statusErr.Summary)
	}
	if !strings.Contains(statusErr.Error(), "HTTP 400 Bad Request") ||
		!strings.Contains(statusErr.Error(), "access_denied: authorization denied by user") {
		t.Fatalf("Error() = %q", statusErr.Error())
	}
}

func TestStatusErrorSummarizesIngressMessage(t *testing.T) {
	err := runErrorServer(t, http.StatusForbidden, `{"message":"Your IP address is not allowed"}`, Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Summary != "Your IP address is not allowed" {
		t.Fatalf("Summary = %q", statusErr.Summary)
	}
	if !strings.Contains(statusErr.Error(), "HTTP 403 Forbidden") {
		t.Fatalf("Error() = %q, want HTTP status", statusErr.Error())
	}
}

func TestStatusErrorFallsBackToRawBody(t *testing.T) {
	err := runErrorServer(t, http.StatusBadGateway, "upstream unavailable", Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Summary != "upstream unavailable" {
		t.Fatalf("Summary = %q", statusErr.Summary)
	}
}

func TestStatusErrorReportsEmptyBody(t *testing.T) {
	err := runErrorServer(t, http.StatusInternalServerError, "", Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Summary != "empty response body" {
		t.Fatalf("Summary = %q", statusErr.Summary)
	}
}

func TestStatusErrorBodyLimit(t *testing.T) {
	err := runErrorServer(t, http.StatusBadRequest, "abcdef", Options{ErrorBodyLimit: 3})
	statusErr := assertStatusError(t, err)
	if statusErr.Body != "abc" || statusErr.Summary != "abc" {
		t.Fatalf("StatusError = %+v, want capped body abc", statusErr)
	}
}

func TestStatusErrorUsesDefaultBodyLimit(t *testing.T) {
	body := strings.Repeat("x", int(DefaultErrorBodyLimit)+10)
	want := strings.Repeat("x", int(DefaultErrorBodyLimit))
	err := runErrorServer(t, http.StatusBadRequest, body, Options{})
	statusErr := assertStatusError(t, err)
	if statusErr.Body != want || statusErr.Summary != want {
		t.Fatalf("StatusError body length = %d summary length = %d, want %d", len(statusErr.Body), len(statusErr.Summary), len(want))
	}
}

func TestStatusErrorDoesNotRenderAuthorizationHeader(t *testing.T) {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer test-token-value")
	err := runErrorServer(t, http.StatusForbidden, `{"message":"denied"}`, Options{Headers: headers})
	statusErr := assertStatusError(t, err)
	if strings.Contains(statusErr.Error(), "test-token-value") {
		t.Fatalf("Error() leaked authorization header: %q", statusErr.Error())
	}
}

func runErrorServer(t *testing.T, status int, body string, opts Options) error {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	var out map[string]any
	return DoJSON(context.Background(), server.Client(), http.MethodPost, server.URL, map[string]string{"request": "test"}, &out, opts)
}

func assertStatusError(t *testing.T, err error) *StatusError {
	t.Helper()
	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("err = %T %v, want *StatusError", err, err)
	}
	return statusErr
}
