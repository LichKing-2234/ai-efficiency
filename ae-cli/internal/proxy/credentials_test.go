package proxy

import (
	"context"
	"testing"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type fakeCredentialClient struct {
	calls        int
	lastSession  string
	lastPlatform string
}

func (f *fakeCredentialClient) GetSessionProviderCredential(_ context.Context, sessionID, platform string) (*client.ProviderCredential, error) {
	f.calls++
	f.lastSession = sessionID
	f.lastPlatform = platform
	return &client.ProviderCredential{
		ProviderName: "sub2api",
		Platform:     platform,
		APIKeyID:     900,
		APIKey:       "sk-" + platform,
		BaseURL:      "http://relay.local/v1",
	}, nil
}

func TestCredentialCacheFetchesOncePerPlatform(t *testing.T) {
	fetcher := &fakeCredentialClient{}
	cache := newCredentialCache(fetcher)

	first, err := cache.Get(context.Background(), "sess-1", "openai")
	if err != nil {
		t.Fatalf("Get first: %v", err)
	}
	second, err := cache.Get(context.Background(), "sess-1", "openai")
	if err != nil {
		t.Fatalf("Get second: %v", err)
	}

	if fetcher.calls != 1 {
		t.Fatalf("calls = %d, want 1", fetcher.calls)
	}
	if first.APIKey != second.APIKey {
		t.Fatalf("cached credentials mismatch: %+v %+v", first, second)
	}
}

func TestCredentialCacheSeparatesPlatforms(t *testing.T) {
	fetcher := &fakeCredentialClient{}
	cache := newCredentialCache(fetcher)

	if _, err := cache.Get(context.Background(), "sess-1", "openai"); err != nil {
		t.Fatalf("Get openai: %v", err)
	}
	if _, err := cache.Get(context.Background(), "sess-1", "anthropic"); err != nil {
		t.Fatalf("Get anthropic: %v", err)
	}

	if fetcher.calls != 2 {
		t.Fatalf("calls = %d, want 2", fetcher.calls)
	}
	if fetcher.lastPlatform != "anthropic" {
		t.Fatalf("lastPlatform = %q, want %q", fetcher.lastPlatform, "anthropic")
	}
}
