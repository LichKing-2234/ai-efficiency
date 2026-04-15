package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/config"
)

const (
	relayConfigSourceNone            = "none"
	relayConfigSourceStatic          = "static_config"
	relayConfigSourcePrimaryProvider = "primary_provider"
)

func resolveRuntimeRelayConfig(ctx context.Context, static config.RelayConfig, loadPrimary func(context.Context) (*config.RelayConfig, error)) (config.RelayConfig, string, error) {
	if strings.TrimSpace(static.URL) != "" {
		return static, relayConfigSourceStatic, nil
	}
	if loadPrimary == nil {
		return static, relayConfigSourceNone, nil
	}

	primary, err := loadPrimary(ctx)
	if err != nil {
		return config.RelayConfig{}, relayConfigSourceNone, err
	}
	if primary == nil || strings.TrimSpace(primary.URL) == "" {
		return static, relayConfigSourceNone, nil
	}

	resolved := *primary
	if strings.TrimSpace(resolved.Provider) == "" {
		resolved.Provider = strings.TrimSpace(static.Provider)
	}
	if strings.TrimSpace(resolved.Provider) == "" {
		resolved.Provider = "sub2api"
	}
	if strings.TrimSpace(resolved.Model) == "" {
		resolved.Model = strings.TrimSpace(static.Model)
	}
	if strings.TrimSpace(resolved.AdminURL) == "" {
		resolved.AdminURL = strings.TrimSpace(static.AdminURL)
	}
	if strings.TrimSpace(resolved.AdminURL) == "" {
		resolved.AdminURL = strings.TrimSpace(resolved.URL)
	}
	if strings.TrimSpace(resolved.AdminAPIKey) == "" {
		resolved.AdminAPIKey = strings.TrimSpace(resolved.APIKey)
	}
	if strings.TrimSpace(resolved.APIKey) == "" {
		resolved.APIKey = strings.TrimSpace(resolved.AdminAPIKey)
	}
	if strings.TrimSpace(resolved.DefaultGroupID) == "" {
		resolved.DefaultGroupID = strings.TrimSpace(static.DefaultGroupID)
	}
	return resolved, relayConfigSourcePrimaryProvider, nil
}

func loadPrimaryRelayConfig(ctx context.Context, entClient *ent.Client, encryptionKey string) (*config.RelayConfig, error) {
	if entClient == nil {
		return nil, nil
	}

	primary, err := entClient.RelayProvider.Query().
		Where(relayprovider.IsPrimaryEQ(true), relayprovider.EnabledEQ(true)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	cfg, err := relayConfigFromPrimaryProvider(primary, encryptionKey)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

func relayConfigFromPrimaryProvider(p *ent.RelayProvider, encryptionKey string) (config.RelayConfig, error) {
	if p == nil {
		return config.RelayConfig{}, nil
	}

	adminKey := strings.TrimSpace(p.AdminAPIKey)
	if adminKey != "" && strings.TrimSpace(encryptionKey) != "" {
		decrypted, err := decryptRelayAdminAPIKey(adminKey, encryptionKey)
		if err != nil {
			return config.RelayConfig{}, fmt.Errorf("decrypt relay provider admin api key: %w", err)
		}
		adminKey = decrypted
	}

	return config.RelayConfig{
		Provider:    firstNonEmpty(strings.TrimSpace(p.RelayType), "sub2api"),
		URL:         strings.TrimSpace(p.BaseURL),
		AdminURL:    firstNonEmpty(strings.TrimSpace(p.AdminURL), strings.TrimSpace(p.BaseURL)),
		APIKey:      adminKey,
		AdminAPIKey: adminKey,
		Model:       strings.TrimSpace(p.DefaultModel),
	}, nil
}

func decryptRelayAdminAPIKey(ciphertextHex, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", err
	}
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
