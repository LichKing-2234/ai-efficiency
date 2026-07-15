package main

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	redis "github.com/redis/go-redis/v9"
)

func TestInitializeRepoInventoryWiresRevisionAndCache(t *testing.T) {
	client := testdb.Open(t)
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })

	cache, revisions, err := initializeRepoInventory(context.Background(), client, redisClient, "test")
	if err != nil {
		t.Fatalf("initializeRepoInventory() error = %v", err)
	}
	revision, err := revisions.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if parsed, err := uuid.Parse(revision); err != nil || parsed.String() != revision {
		t.Fatalf("revision = %q, want canonical UUID", revision)
	}

	inventory, err := cache.GetOrLoad(context.Background(), func(context.Context) ([]repo.InventoryProviderSummary, error) {
		return []repo.InventoryProviderSummary{}, nil
	})
	if err != nil || inventory == nil || len(inventory) != 0 {
		t.Fatalf("GetOrLoad() inventory = %#v, error = %v, want empty cached inventory", inventory, err)
	}
	wantKey := "ae:test:repos:inventory:v1:rev:" + revision
	if !server.Exists(wantKey) {
		t.Fatalf("Redis key %q missing after cache load", wantKey)
	}
}
