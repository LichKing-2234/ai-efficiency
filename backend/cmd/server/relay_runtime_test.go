package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"io"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/config"
)

func TestResolveRuntimeRelayConfigPrefersStaticConfig(t *testing.T) {
	t.Helper()

	static := config.RelayConfig{
		Provider:       "sub2api",
		URL:            "http://static-relay.local",
		APIKey:         "static-api-key",
		AdminAPIKey:    "static-admin-key",
		Model:          "static-model",
		DefaultGroupID: "group-static",
	}

	called := false
	resolved, source, err := resolveRuntimeRelayConfig(context.Background(), static, func(context.Context) (*config.RelayConfig, error) {
		called = true
		return &config.RelayConfig{URL: "http://db-relay.local"}, nil
	})
	if err != nil {
		t.Fatalf("resolveRuntimeRelayConfig() error = %v", err)
	}
	if called {
		t.Fatal("expected DB primary loader not to be called when static relay config is present")
	}
	if source != relayConfigSourceStatic {
		t.Fatalf("source = %q, want %q", source, relayConfigSourceStatic)
	}
	if resolved.URL != static.URL || resolved.APIKey != static.APIKey || resolved.AdminAPIKey != static.AdminAPIKey {
		t.Fatalf("resolved config = %+v, want static %+v", resolved, static)
	}
}

func TestResolveRuntimeRelayConfigFallsBackToPrimaryProvider(t *testing.T) {
	t.Helper()

	static := config.RelayConfig{
		Provider:       "sub2api",
		DefaultGroupID: "group-from-config",
	}

	resolved, source, err := resolveRuntimeRelayConfig(context.Background(), static, func(context.Context) (*config.RelayConfig, error) {
		return &config.RelayConfig{
			Provider:    "sub2api",
			URL:         "http://db-relay.local",
			APIKey:      "db-admin-key",
			AdminAPIKey: "db-admin-key",
			Model:       "db-model",
		}, nil
	})
	if err != nil {
		t.Fatalf("resolveRuntimeRelayConfig() error = %v", err)
	}
	if source != relayConfigSourcePrimaryProvider {
		t.Fatalf("source = %q, want %q", source, relayConfigSourcePrimaryProvider)
	}
	if resolved.URL != "http://db-relay.local" {
		t.Fatalf("resolved.URL = %q, want %q", resolved.URL, "http://db-relay.local")
	}
	if resolved.AdminAPIKey != "db-admin-key" || resolved.APIKey != "db-admin-key" {
		t.Fatalf("resolved API keys = (%q, %q), want db-admin-key", resolved.APIKey, resolved.AdminAPIKey)
	}
	if resolved.Model != "db-model" {
		t.Fatalf("resolved.Model = %q, want %q", resolved.Model, "db-model")
	}
	if resolved.DefaultGroupID != "group-from-config" {
		t.Fatalf("resolved.DefaultGroupID = %q, want %q", resolved.DefaultGroupID, "group-from-config")
	}
}

func TestRelayConfigFromPrimaryProviderDecryptsAdminKey(t *testing.T) {
	t.Helper()

	encryptionKey := "0000000000000000000000000000000000000000000000000000000000000000"
	encryptedKey := encryptAESGCMForTest(t, "relay-admin-key", encryptionKey)

	provider := &ent.RelayProvider{
		Name:         "primary",
		BaseURL:      "http://relay-base.local",
		AdminURL:     "http://relay-admin.local",
		AdminAPIKey:  encryptedKey,
		DefaultModel: "gpt-5.4",
	}

	cfg, err := relayConfigFromPrimaryProvider(provider, encryptionKey)
	if err != nil {
		t.Fatalf("relayConfigFromPrimaryProvider() error = %v", err)
	}
	if cfg.URL != "http://relay-base.local" {
		t.Fatalf("cfg.URL = %q, want %q", cfg.URL, "http://relay-base.local")
	}
	if cfg.APIKey != "relay-admin-key" || cfg.AdminAPIKey != "relay-admin-key" {
		t.Fatalf("cfg API keys = (%q, %q), want relay-admin-key", cfg.APIKey, cfg.AdminAPIKey)
	}
	if cfg.Model != "gpt-5.4" {
		t.Fatalf("cfg.Model = %q, want %q", cfg.Model, "gpt-5.4")
	}
}

func encryptAESGCMForTest(t *testing.T, plaintext, keyHex string) string {
	t.Helper()

	key, err := hex.DecodeString(keyHex)
	if err != nil {
		t.Fatalf("decode key: %v", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("new cipher: %v", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("new gcm: %v", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		t.Fatalf("read nonce: %v", err)
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext)
}
