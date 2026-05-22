package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/pkg"
)

func ensurePrimaryRelayProviderFromConfig(ctx context.Context, entClient *ent.Client, cfg config.RelayConfig, encryptionKey string) error {
	if entClient == nil {
		return nil
	}
	if strings.TrimSpace(cfg.URL) == "" {
		return nil
	}

	primary, err := entClient.RelayProvider.Query().
		Where(relayprovider.IsPrimaryEQ(true), relayprovider.EnabledEQ(true)).
		First(ctx)
	if err == nil && primary != nil {
		return nil
	}
	if err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("query primary relay provider: %w", err)
	}

	adminKey := strings.TrimSpace(cfg.AdminAPIKey)
	if adminKey == "" {
		adminKey = strings.TrimSpace(cfg.APIKey)
	}
	encryptedAdminKey := adminKey
	if strings.TrimSpace(adminKey) != "" && strings.TrimSpace(encryptionKey) != "" {
		encrypted, encErr := pkg.Encrypt(adminKey, encryptionKey)
		if encErr != nil {
			return fmt.Errorf("encrypt relay admin api key: %w", encErr)
		}
		encryptedAdminKey = encrypted
	}

	name := firstNonEmpty(strings.TrimSpace(cfg.Provider), "sub2api")
	displayName := firstNonEmpty(strings.TrimSpace(cfg.Provider), name)
	adminURL := firstNonEmpty(strings.TrimSpace(cfg.AdminURL), strings.TrimSpace(cfg.URL))
	model := firstNonEmpty(strings.TrimSpace(cfg.Model), "claude-sonnet-4-20250514")

	_, err = entClient.RelayProvider.Create().
		SetName(name).
		SetDisplayName(displayName).
		SetBaseURL(strings.TrimSpace(cfg.URL)).
		SetAdminURL(adminURL).
		SetRelayType(name).
		SetAdminAPIKey(encryptedAdminKey).
		SetDefaultModel(model).
		SetIsPrimary(true).
		SetEnabled(true).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("create primary relay provider from config: %w", err)
	}
	return nil
}
