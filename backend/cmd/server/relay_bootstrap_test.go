package main

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestEnsurePrimaryRelayProviderFromConfigCreatesPrimaryWhenMissing(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()

	cfg := config.RelayConfig{
		Provider:    "sub2api",
		URL:         "https://sub2api.agoraio.cn/",
		AdminAPIKey: "admin-test",
		Model:       "gpt-5.4",
	}

	if err := ensurePrimaryRelayProviderFromConfig(ctx, client, cfg, "0000000000000000000000000000000000000000000000000000000000000000"); err != nil {
		t.Fatalf("ensurePrimaryRelayProviderFromConfig() unexpected error: %v", err)
	}

	p, err := client.RelayProvider.Query().
		Where(relayprovider.IsPrimaryEQ(true), relayprovider.EnabledEQ(true)).
		Only(ctx)
	if err != nil {
		t.Fatalf("query primary provider: %v", err)
	}
	if p.Name != "sub2api" {
		t.Fatalf("name = %q, want sub2api", p.Name)
	}
	if p.BaseURL != "https://sub2api.agoraio.cn/" {
		t.Fatalf("base_url = %q, want https://sub2api.agoraio.cn/", p.BaseURL)
	}
	if p.DefaultModel != "gpt-5.4" {
		t.Fatalf("default_model = %q, want gpt-5.4", p.DefaultModel)
	}
}

func TestEnsurePrimaryRelayProviderFromConfigDoesNotOverwriteExistingPrimary(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()

	client.RelayProvider.Create().
		SetName("existing").
		SetDisplayName("Existing").
		SetBaseURL("https://existing.example.com/").
		SetAdminAPIKey("encrypted-existing").
		SetRelayType("sub2api").
		SetDefaultModel("claude-sonnet-4-20250514").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	cfg := config.RelayConfig{
		Provider:    "sub2api",
		URL:         "https://sub2api.agoraio.cn/",
		AdminAPIKey: "admin-test",
		Model:       "gpt-5.4",
	}

	if err := ensurePrimaryRelayProviderFromConfig(ctx, client, cfg, "0000000000000000000000000000000000000000000000000000000000000000"); err != nil {
		t.Fatalf("ensurePrimaryRelayProviderFromConfig() unexpected error: %v", err)
	}

	count, err := client.RelayProvider.Query().Count(ctx)
	if err != nil {
		t.Fatalf("count providers: %v", err)
	}
	if count != 1 {
		t.Fatalf("provider count = %d, want 1", count)
	}

	p := client.RelayProvider.Query().OnlyX(ctx)
	if p.Name != "existing" {
		t.Fatalf("name = %q, want existing", p.Name)
	}
}
