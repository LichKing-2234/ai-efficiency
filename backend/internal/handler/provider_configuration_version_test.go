package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/internal/relayruntime"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type providerInvalidationRecorder struct {
	events []relayruntime.InvalidationEvent
	err    error
}

func (r *providerInvalidationRecorder) Publish(_ context.Context, event relayruntime.InvalidationEvent) error {
	r.events = append(r.events, event)
	return r.err
}

func (r *providerInvalidationRecorder) Subscribe(ctx context.Context, _ func(relayruntime.InvalidationEvent)) error {
	<-ctx.Done()
	return ctx.Err()
}

func newProviderRuntimeForHandlerTest(t *testing.T, env *testEnv, bus *providerInvalidationRecorder) *relayruntime.Manager {
	t.Helper()
	runtime, err := relayruntime.NewManager(env.client, userUsageTestEncryptionKey, zap.NewNop(), relayruntime.Options{Bus: bus})
	if err != nil {
		t.Fatalf("new provider runtime: %v", err)
	}
	return runtime
}

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

	bus := &providerInvalidationRecorder{}
	h := NewProviderHandler(env.client, userUsageTestEncryptionKey, zap.NewNop(), newProviderRuntimeForHandlerTest(t, env, bus))
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
	if len(bus.events) != 1 || bus.events[0].ProviderID != provider.ID || bus.events[0].ConfigurationVersion != 2 {
		t.Fatalf("invalidation events = %#v", bus.events)
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

	bus := &providerInvalidationRecorder{}
	h := NewProviderHandler(env.client, userUsageTestEncryptionKey, zap.NewNop(), newProviderRuntimeForHandlerTest(t, env, bus))
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
	if len(bus.events) != 0 {
		t.Fatalf("failed update published invalidations: %#v", bus.events)
	}
}

func TestProviderInvalidationPublishFailureDoesNotRollBackUpdate(t *testing.T) {
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

	bus := &providerInvalidationRecorder{err: fmt.Errorf("redis unavailable")}
	h := NewProviderHandler(env.client, userUsageTestEncryptionKey, zap.NewNop(), newProviderRuntimeForHandlerTest(t, env, bus))
	router := gin.New()
	router.PUT("/providers/:id", h.Update)
	req := httptest.NewRequest(http.MethodPut, "/providers/"+providerIDString(provider.ID), bytes.NewBufferString(`{"display_name":"Updated"}`))
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
	if updated.ConfigurationVersion != 2 || updated.DisplayName != "Updated" {
		t.Fatalf("updated provider = version %d display %q", updated.ConfigurationVersion, updated.DisplayName)
	}
	if len(bus.events) != 1 {
		t.Fatalf("publish attempts = %d, want 1", len(bus.events))
	}
}

func TestProviderInvalidationPublishesAfterCreateAndDelete(t *testing.T) {
	env := setupTestEnv(t)
	bus := &providerInvalidationRecorder{}
	h := NewProviderHandler(env.client, userUsageTestEncryptionKey, zap.NewNop(), newProviderRuntimeForHandlerTest(t, env, bus))
	router := gin.New()
	router.POST("/providers", h.Create)
	router.DELETE("/providers/:id", h.Delete)

	createReq := httptest.NewRequest(http.MethodPost, "/providers", bytes.NewBufferString(`{
		"name":"relay-secondary",
		"display_name":"Relay Secondary",
		"base_url":"https://relay-secondary.example.com",
		"admin_api_key":"test-admin-key",
		"enabled":true
	}`))
	createReq.Header.Set("Content-Type", "application/json")
	createResponse := httptest.NewRecorder()
	router.ServeHTTP(createResponse, createReq)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body=%s", createResponse.Code, createResponse.Body.String())
	}
	providers, err := env.client.RelayProvider.Query().All(context.Background())
	if err != nil || len(providers) != 1 {
		t.Fatalf("created providers = %d, err=%v", len(providers), err)
	}
	created := providers[0]
	if len(bus.events) != 1 || bus.events[0].ProviderID != created.ID || bus.events[0].ConfigurationVersion != 1 {
		t.Fatalf("create invalidations = %#v", bus.events)
	}

	deleteReq := httptest.NewRequest(http.MethodDelete, "/providers/"+providerIDString(created.ID), nil)
	deleteResponse := httptest.NewRecorder()
	router.ServeHTTP(deleteResponse, deleteReq)
	if deleteResponse.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200, body=%s", deleteResponse.Code, deleteResponse.Body.String())
	}
	if len(bus.events) != 2 || bus.events[1].ProviderID != created.ID || bus.events[1].ConfigurationVersion != 2 {
		t.Fatalf("delete invalidations = %#v", bus.events)
	}
}

func providerIDString(id int) string {
	return fmt.Sprintf("%d", id)
}
