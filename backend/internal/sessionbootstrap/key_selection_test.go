package sessionbootstrap

import (
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

func TestSelectReusableKeyPrefersUsernameThenLastUsed(t *testing.T) {
	now := time.Now()
	older := now.Add(-2 * time.Hour)
	newer := now.Add(-30 * time.Minute)

	keys := []relay.APIKey{
		{
			ID:         1,
			Name:       "alice",
			Status:     "active",
			Group:      &relay.Group{Platform: "openai"},
			LastUsedAt: &older,
		},
		{
			ID:         2,
			Name:       "alice",
			Status:     "active",
			Group:      &relay.Group{Platform: "openai"},
			LastUsedAt: &newer,
		},
		{
			ID:         3,
			Name:       "alice",
			Status:     "active",
			Group:      &relay.Group{Platform: "anthropic"},
			LastUsedAt: &now,
		},
	}

	got := selectReusableKey(keys, "openai", "alice", "alice")
	if got == nil || got.ID != 2 {
		t.Fatalf("selected key = %+v, want id=2", got)
	}
}

func TestSelectReusableKeyFallsBackToEmailPrefix(t *testing.T) {
	now := time.Now()
	keys := []relay.APIKey{
		{
			ID:         10,
			Name:       "alice",
			Status:     "disabled",
			Group:      &relay.Group{Platform: "openai"},
			LastUsedAt: &now,
		},
		{
			ID:         11,
			Name:       "a.smith",
			Status:     "active",
			Group:      &relay.Group{Platform: "openai"},
			LastUsedAt: &now,
		},
	}

	got := selectReusableKey(keys, "openai", "alice", "a.smith")
	if got == nil || got.ID != 11 {
		t.Fatalf("selected key = %+v, want id=11", got)
	}
}

func TestSelectReusableKeyReturnsNilWhenPlatformDoesNotMatch(t *testing.T) {
	keys := []relay.APIKey{
		{
			ID:     20,
			Name:   "alice",
			Status: "active",
			Group:  &relay.Group{Platform: "anthropic"},
		},
	}

	got := selectReusableKey(keys, "openai", "alice", "alice")
	if got != nil {
		t.Fatalf("selected key = %+v, want nil", got)
	}
}

func TestSelectReusableKeyFallsBackToCreatedAtWhenLastUsedMissing(t *testing.T) {
	older := time.Date(2026, 4, 7, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)

	keys := []relay.APIKey{
		{
			ID:        30,
			Name:      "alice",
			Status:    "active",
			Group:     &relay.Group{Platform: "openai"},
			CreatedAt: older,
		},
		{
			ID:        31,
			Name:      "alice",
			Status:    "active",
			Group:     &relay.Group{Platform: "openai"},
			CreatedAt: newer,
		},
	}

	got := selectReusableKey(keys, "openai", "alice", "alice")
	if got == nil || got.ID != 31 {
		t.Fatalf("selected key = %+v, want id=31", got)
	}
}

func TestSelectReusableKeyUsesCreatedAtAsTieBreakerForLastUsed(t *testing.T) {
	lastUsed := time.Date(2026, 4, 7, 12, 0, 0, 0, time.UTC)
	older := time.Date(2026, 4, 7, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 4, 7, 10, 0, 0, 0, time.UTC)

	keys := []relay.APIKey{
		{
			ID:         40,
			Name:       "alice",
			Status:     "active",
			Group:      &relay.Group{Platform: "openai"},
			LastUsedAt: &lastUsed,
			CreatedAt:  older,
		},
		{
			ID:         41,
			Name:       "alice",
			Status:     "active",
			Group:      &relay.Group{Platform: "openai"},
			LastUsedAt: &lastUsed,
			CreatedAt:  newer,
		},
	}

	got := selectReusableKey(keys, "openai", "alice", "alice")
	if got == nil || got.ID != 41 {
		t.Fatalf("selected key = %+v, want id=41", got)
	}
}

func TestPreferredKeyNameUsesEmailPrefixWhenUsernameIsEmailAlias(t *testing.T) {
	got := preferredKeyName("luxuhui@shengwang.cn", "luxuhui@shengwang.cn")
	if got != "luxuhui" {
		t.Fatalf("preferredKeyName = %q, want %q", got, "luxuhui")
	}
}
