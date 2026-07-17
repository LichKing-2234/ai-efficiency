package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestProviderConfigurationVersionStartsAtOneAndIncrementsWithUpdate(t *testing.T) {
	env := setupTestEnv(t)
	encryptedKey, err := encryptAESGCM("test-api-key", userUsageTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt admin key: %v", err)
	}
	provider, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey(encryptedKey).
		SetDefaultModel("model-v1").
		SetIsPrimary(true).
		SetEnabled(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if provider.ConfigurationVersion != 1 {
		t.Fatalf("configuration version = %d, want 1", provider.ConfigurationVersion)
	}

	h := NewProviderHandler(env.client, userUsageTestEncryptionKey, zap.NewNop())
	router := gin.New()
	router.PUT("/providers/:id", h.Update)

	req := httptest.NewRequest(http.MethodPut, "/providers/"+providerIDString(provider.ID), bytes.NewBufferString(`{
		"base_url":"https://relay-v2.example.com",
		"default_model":"model-v2"
	}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200, body=%s", w.Code, w.Body.String())
	}

	updated, err := env.client.RelayProvider.Get(context.Background(), provider.ID)
	if err != nil {
		t.Fatalf("reload provider: %v", err)
	}
	if updated.ConfigurationVersion != 2 {
		t.Fatalf("configuration version = %d, want 2", updated.ConfigurationVersion)
	}
	if updated.BaseURL != "https://relay-v2.example.com" || updated.DefaultModel != "model-v2" {
		t.Fatalf("provider fields = %q/%q", updated.BaseURL, updated.DefaultModel)
	}
}

func TestProviderConfigurationVersionDoesNotChangeWhenUpdateBindingFails(t *testing.T) {
	env := setupTestEnv(t)
	provider, err := env.client.RelayProvider.Create().
		SetName("primary").
		SetDisplayName("Primary").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("encrypted-test-key").
		SetIsPrimary(true).
		SetEnabled(true).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	h := NewProviderHandler(env.client, userUsageTestEncryptionKey, zap.NewNop())
	router := gin.New()
	router.PUT("/providers/:id", h.Update)
	req := httptest.NewRequest(http.MethodPut, "/providers/"+providerIDString(provider.ID), bytes.NewBufferString(`{"enabled":`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("update status = %d, want 400, body=%s", w.Code, w.Body.String())
	}

	unchanged, err := env.client.RelayProvider.Get(context.Background(), provider.ID)
	if err != nil {
		t.Fatalf("reload provider: %v", err)
	}
	if unchanged.ConfigurationVersion != 1 {
		t.Fatalf("configuration version = %d, want 1", unchanged.ConfigurationVersion)
	}
}

func providerIDString(id int) string {
	return fmt.Sprintf("%d", id)
}
