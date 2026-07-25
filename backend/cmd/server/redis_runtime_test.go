package main

import (
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/redisruntime"
)

func TestServerRedisClientUsesSharedBoundedTransport(t *testing.T) {
	client := redisruntime.NewClient(config.RedisConfig{Addr: "redis.example:6379", Password: "test-password", DB: 2})
	t.Cleanup(func() { _ = client.Close() })
	options := client.Options()
	if options.Addr != "redis.example:6379" || options.Password != "test-password" || options.DB != 2 {
		t.Fatalf("Redis identity options = %+v", options)
	}
	if options.DialTimeout != time.Second || options.PoolTimeout != time.Second {
		t.Fatalf("Redis dial/pool options = %s/%s, want one second", options.DialTimeout, options.PoolTimeout)
	}
	if options.ReadTimeout != 2*time.Second || options.WriteTimeout != 2*time.Second {
		t.Fatalf("Redis read/write options = %s/%s, want two-second ceilings", options.ReadTimeout, options.WriteTimeout)
	}
	if options.MinIdleConns < 4 {
		t.Fatalf("Redis MinIdleConns = %d, want at least 4", options.MinIdleConns)
	}
	if !options.ContextTimeoutEnabled {
		t.Fatal("ContextTimeoutEnabled = false, want true")
	}
	if options.MaxRetries != 0 {
		t.Fatalf("effective MaxRetries = %d, want 0 retries", options.MaxRetries)
	}
	if options.DialerRetries != 1 {
		t.Fatalf("DialerRetries = %d, want 1", options.DialerRetries)
	}
}
