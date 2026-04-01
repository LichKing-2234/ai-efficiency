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

type userCredentialContextKey struct{}

type UserCredential struct {
	Login    string
	Password string
}

func WithUserCredentials(ctx context.Context, login, password string) context.Context {
	return context.WithValue(ctx, userCredentialContextKey{}, UserCredential{Login: login, Password: password})
}

func UserCredentialsFromContext(ctx context.Context) (login string, password string, ok bool) {
	if ctx == nil {
		return "", "", false
	}
	cred, ok := ctx.Value(userCredentialContextKey{}).(UserCredential)
	if !ok {
		return "", "", false
	}
	if cred.Login == "" || cred.Password == "" {
		return "", "", false
	}
	return cred.Login, cred.Password, true
}

// Provider defines the unified interface for relay server interactions.
type Provider interface {
	Ping(ctx context.Context) error
	Name() string

	Authenticate(ctx context.Context, username, password string) (*User, error)
	GetUser(ctx context.Context, userID int64) (*User, error)
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByUsername(ctx context.Context, username string) (*User, error)
	CreateUser(ctx context.Context, req CreateUserRequest) (*User, error)

	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
	ChatCompletionWithTools(ctx context.Context, req ChatCompletionRequest, tools []ToolDef) (*ChatCompletionWithToolsResponse, error)

	GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*UsageStats, error)
	ListUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error)
	CreateUserAPIKey(ctx context.Context, userID int64, req APIKeyCreateRequest) (*APIKeyWithSecret, error)
	RevokeUserAPIKey(ctx context.Context, keyID int64) error
	ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]UsageLog, error)
}
