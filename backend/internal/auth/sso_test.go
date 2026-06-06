package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

func TestSSOProviderName(t *testing.T) {
	p := NewSSOProvider(nil, zap.NewNop())
	if p.Name() != "sso" {
		t.Errorf("Name() = %q, want %q", p.Name(), "sso")
	}
}

func TestNewSSOProvider(t *testing.T) {
	p := NewSSOProvider(nil, zap.NewNop())
	if p == nil {
		t.Fatal("NewSSOProvider returned nil")
	}
}

func TestSSOProviderAuthenticateNilProvider(t *testing.T) {
	p := NewSSOProvider(nil, zap.NewNop())
	info, err := p.Authenticate(context.Background(), "user", "pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatal("expected nil UserInfo for nil relay provider")
	}
}

// mockRelayProvider is a minimal relay.Provider for testing SSO.
type mockRelayProvider struct {
	authErr             error
	authUser            *relay.User
	usersByEmail        map[string]relay.User
	usersByUsername     map[string]relay.User
	createUserCalls     []relay.CreateUserRequest
	createUserErr       error
	createUserResult    *relay.User
	findByEmailCalls    []string
	findByUsernameCalls []string
}

func (m *mockRelayProvider) Name() string                 { return "mock" }
func (m *mockRelayProvider) Ping(_ context.Context) error { return nil }
func (m *mockRelayProvider) GetUser(_ context.Context, _ int64) (*relay.User, error) {
	return nil, nil
}
func (m *mockRelayProvider) ListAllowedGroupsForUser(_ context.Context, _ int64) ([]relay.Group, error) {
	return nil, nil
}
func (m *mockRelayProvider) FindUserByEmail(_ context.Context, email string) (*relay.User, error) {
	m.findByEmailCalls = append(m.findByEmailCalls, email)
	if m.usersByEmail != nil {
		if u, ok := m.usersByEmail[email]; ok {
			copy := u
			return &copy, nil
		}
	}
	return nil, nil
}
func (m *mockRelayProvider) FindUserByUsername(_ context.Context, username string) (*relay.User, error) {
	m.findByUsernameCalls = append(m.findByUsernameCalls, username)
	if m.usersByUsername != nil {
		if u, ok := m.usersByUsername[username]; ok {
			copy := u
			return &copy, nil
		}
	}
	return nil, nil
}
func (m *mockRelayProvider) CreateUser(_ context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	m.createUserCalls = append(m.createUserCalls, req)
	if m.createUserErr != nil {
		return nil, m.createUserErr
	}
	if m.createUserResult != nil {
		return m.createUserResult, nil
	}
	return &relay.User{ID: 77, Username: req.Username, Email: req.Email, Role: "user"}, nil
}
func (m *mockRelayProvider) UpdateUser(_ context.Context, _ int64, _ relay.UpdateUserRequest) (*relay.User, error) {
	return nil, nil
}
func (m *mockRelayProvider) ChatCompletion(_ context.Context, _ relay.ChatCompletionRequest) (*relay.ChatCompletionResponse, error) {
	return nil, nil
}
func (m *mockRelayProvider) ChatCompletionWithTools(_ context.Context, _ relay.ChatCompletionRequest, _ []relay.ToolDef) (*relay.ChatCompletionWithToolsResponse, error) {
	return nil, nil
}
func (m *mockRelayProvider) GetUsageStats(_ context.Context, _ int64, _, _ time.Time) (*relay.UsageStats, error) {
	return nil, nil
}
func (m *mockRelayProvider) ListUserAPIKeys(_ context.Context, _ int64) ([]relay.APIKey, error) {
	return nil, nil
}
func (m *mockRelayProvider) CreateUserAPIKey(_ context.Context, _ int64, _ relay.APIKeyCreateRequest) (*relay.APIKeyWithSecret, error) {
	return nil, nil
}
func (m *mockRelayProvider) UpdateUserAPIKeyStatus(_ context.Context, _ int64, _ string) error {
	return nil
}
func (m *mockRelayProvider) RevokeUserAPIKey(_ context.Context, _ int64) error {
	return nil
}
func (m *mockRelayProvider) ListUsageLogsByAPIKeyExact(_ context.Context, _ int64, _, _ time.Time) ([]relay.UsageLog, error) {
	return nil, nil
}
func (m *mockRelayProvider) GetUserUsageStats(_ context.Context, _, _ string) (*relay.UserUsageStats, error) {
	return nil, nil
}
func (m *mockRelayProvider) GetUserUsageTrend(_ context.Context, _, _ string, _ relay.UsageTrendParams) (*relay.UsageTrendResponse, error) {
	return nil, nil
}
func (m *mockRelayProvider) GetUserUsageModels(_ context.Context, _, _ string, _ relay.UsageModelParams) (*relay.UsageModelResponse, error) {
	return nil, nil
}

func (m *mockRelayProvider) Authenticate(_ context.Context, _, _ string) (*relay.User, error) {
	if m.authErr != nil {
		return nil, m.authErr
	}
	return m.authUser, nil
}

func TestSSOProviderAuthenticateSuccess(t *testing.T) {
	mock := &mockRelayProvider{
		authUser: &relay.User{ID: 42, Username: "admin", Email: "admin@test.com"},
	}
	p := NewSSOProvider(mock, zap.NewNop())
	info, err := p.Authenticate(context.Background(), "admin", "pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected non-nil UserInfo")
	}
	if info.Username != "admin" || info.Email != "admin@test.com" {
		t.Fatalf("unexpected user info: %+v", info)
	}
	if info.RelayUserID == nil || *info.RelayUserID != 42 {
		t.Fatalf("expected RelayUserID=42, got %v", info.RelayUserID)
	}
}

func TestSSOProviderAuthenticateInvalidCredentials(t *testing.T) {
	mock := &mockRelayProvider{
		authErr: relay.ErrInvalidCredentials,
		usersByEmail: map[string]relay.User{
			"bad@example.com": {ID: 7, Username: "bad", Email: "bad@example.com"},
		},
	}
	p := NewSSOProvider(mock, zap.NewNop())
	info, err := p.Authenticate(context.Background(), "bad@example.com", "bad")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatal("expected nil UserInfo for invalid credentials")
	}
	if len(mock.createUserCalls) != 0 {
		t.Fatalf("expected existing relay user with bad password not to be recreated, got %+v", mock.createUserCalls)
	}
}

func TestSSOProviderAuthenticateCreatesMissingRelayUser(t *testing.T) {
	mock := &mockRelayProvider{authErr: relay.ErrInvalidCredentials}
	p := NewSSOProvider(mock, zap.NewNop())

	info, err := p.Authenticate(context.Background(), "alice@example.com", "test-password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info == nil {
		t.Fatal("expected provisioned UserInfo")
	}
	if info.Username != "alice" || info.Email != "alice@example.com" || info.AuthSource != "relay_sso" {
		t.Fatalf("unexpected user info: %+v", info)
	}
	if info.RelayUserID == nil || *info.RelayUserID != 77 {
		t.Fatalf("RelayUserID = %v, want 77", info.RelayUserID)
	}
	if info.RelayAuthPassword != "test-password" {
		t.Fatal("expected SSO password to be stored for relay JWT writes")
	}
	if len(mock.createUserCalls) != 1 {
		t.Fatalf("expected one CreateUser call, got %+v", mock.createUserCalls)
	}
	req := mock.createUserCalls[0]
	if req.Username != "alice" || req.Email != "alice@example.com" || req.Password != "test-password" {
		t.Fatalf("unexpected CreateUser request: %+v", req)
	}
}

func TestSSOProviderAuthenticateReturnsNilWhenMissingRelayUserCreateFails(t *testing.T) {
	mock := &mockRelayProvider{authErr: relay.ErrInvalidCredentials, createUserErr: errors.New("create failed")}
	p := NewSSOProvider(mock, zap.NewNop())

	info, err := p.Authenticate(context.Background(), "alice@example.com", "test-password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatalf("expected nil UserInfo when relay self-provisioning fails, got %+v", info)
	}
}

func TestSSOProviderAuthenticateExtraVerification(t *testing.T) {
	mock := &mockRelayProvider{authErr: relay.ErrExtraVerificationRequired}
	p := NewSSOProvider(mock, zap.NewNop())
	info, err := p.Authenticate(context.Background(), "user", "pass")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info != nil {
		t.Fatal("expected nil UserInfo for extra verification required")
	}
}
