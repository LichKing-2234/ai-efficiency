package proxy

import (
	"context"
	"fmt"
	"sync"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type credentialFetcher interface {
	GetSessionProviderCredential(ctx context.Context, sessionID, platform string) (*client.ProviderCredential, error)
}

type credentialCache struct {
	mu      sync.Mutex
	entries map[string]*client.ProviderCredential
	fetcher credentialFetcher
}

func newCredentialCache(fetcher credentialFetcher) *credentialCache {
	return &credentialCache{
		entries: map[string]*client.ProviderCredential{},
		fetcher: fetcher,
	}
}

func (c *credentialCache) Get(ctx context.Context, sessionID, platform string) (*client.ProviderCredential, error) {
	c.mu.Lock()
	if cred, ok := c.entries[platform]; ok {
		c.mu.Unlock()
		return cred, nil
	}
	c.mu.Unlock()

	if c.fetcher == nil {
		return nil, fmt.Errorf("credential fetcher is not configured")
	}

	cred, err := c.fetcher.GetSessionProviderCredential(ctx, sessionID, platform)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.entries[platform] = cred
	c.mu.Unlock()
	return cred, nil
}
