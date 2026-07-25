package redisruntime

import (
	"time"

	"github.com/ai-efficiency/backend/internal/config"
	redis "github.com/redis/go-redis/v9"
)

// NewClient constructs the shared bounded Redis runtime client.
func NewClient(cfg config.RedisConfig) *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:                  cfg.Addr,
		Password:              cfg.Password,
		DB:                    cfg.DB,
		MaxRetries:            -1,
		DialTimeout:           time.Second,
		DialerRetries:         1,
		ReadTimeout:           2 * time.Second,
		WriteTimeout:          2 * time.Second,
		PoolTimeout:           time.Second,
		MinIdleConns:          4,
		ContextTimeoutEnabled: true,
	})
}
