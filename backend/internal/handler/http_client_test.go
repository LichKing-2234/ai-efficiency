package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestProviderHandlerUsesInjectedHTTPClient(t *testing.T) {
	const encryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"
	encryptedKey, err := encryptAESGCM("test-api-key", encryptionKey)
	if err != nil {
		t.Fatalf("encryptAESGCM() error = %v", err)
	}
	var calls int
	injected := &http.Client{
		Timeout: 19 * time.Second,
		Transport: handlerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return syntheticHandlerResponse(req, http.StatusOK, ""), nil
		}),
	}
	h := NewProviderHandler(nil, encryptionKey, zap.NewNop(), injected)
	if h.httpClient != injected || h.httpClient.Timeout != 19*time.Second {
		t.Fatal("NewProviderHandler() did not retain the injected HTTP client")
	}
	provider := h.getOrCreateRelayProvider(&ent.RelayProvider{
		ID:           1,
		Name:         "Relay Alpha",
		BaseURL:      "https://relay.example.com",
		AdminAPIKey:  encryptedKey,
		DefaultModel: "model-alpha",
	})

	if err := provider.Ping(context.Background()); err != nil {
		t.Fatalf("Ping() error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("injected transport calls = %d, want 1", calls)
	}
}

func TestSettingsHandlerUsesInjectedHTTPClient(t *testing.T) {
	var calls int
	injected := &http.Client{
		Timeout: 17 * time.Second,
		Transport: handlerRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			return syntheticHandlerResponse(req, http.StatusOK, `{"choices":[{"message":{"content":"pong"}}]}`), nil
		}),
	}
	h := NewSettingsHandlerWithHTTPClient("", config.RelayConfig{
		URL:         "https://relay.example.com",
		AdminAPIKey: "test-api-key",
		Model:       "model-alpha",
	}, zap.NewNop(), injected)
	if h.httpClient != injected || h.httpClient.Timeout != 17*time.Second {
		t.Fatal("NewSettingsHandlerWithHTTPClient() did not retain the injected HTTP client")
	}
	router := gin.New()
	router.POST("/test", h.TestLLMConnection)

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/test", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if calls != 1 {
		t.Fatalf("injected transport calls = %d, want 1", calls)
	}
}

func TestSettingsHandlerPreservesRuntimeUpdaterConstructor(t *testing.T) {
	runtime := &recordingRelayRuntime{}
	h := NewSettingsHandler("", config.RelayConfig{}, zap.NewNop(), runtime)
	if h.relayRuntime != runtime {
		t.Fatal("NewSettingsHandler() did not retain the legacy relay runtime updater")
	}
}

type recordingRelayRuntime struct{}

func (*recordingRelayRuntime) SetAdminAPIKey(string) {}
func (*recordingRelayRuntime) SetModel(string)       {}

type handlerRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn handlerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func syntheticHandlerResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}
