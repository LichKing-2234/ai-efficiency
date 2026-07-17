package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestSetupRouterRecoveryUsesSanitizedFixedZapFields(t *testing.T) {
	const encryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"
	secrets := []string{
		"alice@example.com",
		"private-credential",
		"private-panic-value",
		"private-route-value",
	}

	previousErrorWriter := gin.DefaultErrorWriter
	var ginDump bytes.Buffer
	gin.DefaultErrorWriter = &ginDump
	t.Cleanup(func() { gin.DefaultErrorWriter = previousErrorWriter })

	env := setupTestEnv(t)
	core, observed := observer.New(zap.InfoLevel)
	router := SetupRouterWithOptions(
		env.client,
		nil,
		env.authSvc,
		repo.NewService(env.client, encryptionKey, zap.NewNop()),
		webhook.NewHandler(env.client, nil, zap.NewNop()),
		nil,
		nil,
		encryptionKey,
		"",
		middleware.CORS(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		RouterOptions{RequestLogger: zap.New(core), Release: "test-release"},
	)

	method := strings.Repeat("CUSTOM", 64)
	router.Handle(method, "/panic/:id", func(*gin.Context) {
		panic("private-panic-value")
	})
	request := httptest.NewRequest(method, "/panic/private-route-value?email=alice@example.com", nil)
	request.Header.Set(telemetry.HeaderRequestID, "recovery-request")
	request.Header.Set("X-Private-Credential", "private-credential")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if ginDump.Len() != 0 {
		t.Fatalf("Gin recovery dump = %q, want empty", ginDump.String())
	}

	var recoveryFields map[string]any
	for _, entry := range observed.All() {
		if entry.Message == "http_recovery" {
			recoveryFields = entry.ContextMap()
		}
		serialized := fmt.Sprint(entry.Message, entry.ContextMap())
		for _, secret := range secrets {
			if strings.Contains(serialized, secret) {
				t.Fatalf("captured log contains private value %q: %s", secret, serialized)
			}
		}
	}
	if recoveryFields == nil {
		t.Fatalf("recovery event missing from logs: %+v", observed.All())
	}
	if len(recoveryFields) != 7 {
		t.Fatalf("recovery fields = %#v, want exactly 7 fixed fields", recoveryFields)
	}
	wantFields := map[string]string{
		"event":        "http_recovery",
		"error_class":  "panic",
		"route":        "/panic/:id",
		"method":       "OTHER",
		"status_class": "5xx",
		"release":      "test-release",
		"request_id":   "recovery-request",
	}
	for key, want := range wantFields {
		if got, ok := recoveryFields[key].(string); !ok || got != want {
			t.Fatalf("recovery field %s = %#v, want %q", key, recoveryFields[key], want)
		}
	}
}
