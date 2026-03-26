package relay

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors for authentication outcomes.
var (
	ErrInvalidCredentials        = errors.New("relay: invalid credentials")
	ErrExtraVerificationRequired = errors.New("relay: extra verification required")
)

// Provider defines the unified interface for relay server interactions.
type Provider interface {
	Ping(ctx context.Context) error
	Name() string

	Authenticate(ctx context.Context, username, password string) (*User, error)
	GetUser(ctx context.Context, userID int64) (*User, error)
	FindUserByEmail(ctx context.Context, email string) (*User, error)

	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
	ChatCompletionWithTools(ctx context.Context, req ChatCompletionRequest, tools []ToolDef) (*ChatCompletionWithToolsResponse, error)

	GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*UsageStats, error)
	ListUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error)
	CreateUserAPIKey(ctx context.Context, userID int64, keyName string) (*APIKeyWithSecret, error)
}
