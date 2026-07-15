package main

import (
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/config"
)

func TestRedisClientOptionsBoundWorkItemCacheLatency(t *testing.T) {
	options := redisClientOptions(config.RedisConfig{Addr: "redis.example:6379", Password: "test-password", DB: 2})
	if options.Addr != "redis.example:6379" || options.Password != "test-password" || options.DB != 2 {
		t.Fatalf("Redis identity options = %+v", options)
	}
	if options.DialTimeout != 100*time.Millisecond ||
		options.ReadTimeout != 100*time.Millisecond ||
		options.WriteTimeout != 100*time.Millisecond ||
		options.PoolTimeout != 100*time.Millisecond {
		t.Fatalf("Redis timeout options = dial %s read %s write %s pool %s, want 100ms each", options.DialTimeout, options.ReadTimeout, options.WriteTimeout, options.PoolTimeout)
	}
	if !options.ContextTimeoutEnabled {
		t.Fatal("ContextTimeoutEnabled = false, want true")
	}
	if options.MaxRetries != -1 {
		t.Fatalf("MaxRetries = %d, want -1 to disable command retries", options.MaxRetries)
	}
	if options.DialerRetries != 1 {
		t.Fatalf("DialerRetries = %d, want 1", options.DialerRetries)
	}
}
