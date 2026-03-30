package auth

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
)

type fakeRelayIdentityAPI struct {
	findByUsernameCalls []string
	createUserCalls     []relay.CreateUserRequest

	findResult *relay.User
}

func (f *fakeRelayIdentityAPI) FindUserByUsername(_ context.Context, username string) (*relay.User, error) {
	f.findByUsernameCalls = append(f.findByUsernameCalls, username)
	return f.findResult, nil
}

func (f *fakeRelayIdentityAPI) CreateUser(_ context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	f.createUserCalls = append(f.createUserCalls, req)
	return &relay.User{
		ID:       123,
		Username: req.Username,
		Email:    req.Email,
		Role:     "user",
	}, nil
}

func TestResolveOrProvisionRelayUser_UsesUsernameAsStableKey(t *testing.T) {
	api := &fakeRelayIdentityAPI{
		findResult: &relay.User{ID: 7, Username: "alice", Email: "alice@example.com"},
	}

	r := NewRelayIdentityResolver(api, "ldap.local")
	u, err := r.ResolveOrProvisionRelayUser(context.Background(), "alice", "completely-different@example.com")
	if err != nil {
		t.Fatalf("ResolveOrProvisionRelayUser() unexpected error: %v", err)
	}
	if u == nil || u.ID != 7 {
		t.Fatalf("expected existing relay user ID=7, got %+v", u)
	}
	if len(api.findByUsernameCalls) != 1 || api.findByUsernameCalls[0] != "alice" {
		t.Fatalf("expected FindUserByUsername called once with 'alice', got %+v", api.findByUsernameCalls)
	}
	if len(api.createUserCalls) != 0 {
		t.Fatalf("expected CreateUser not called when user exists, got %d calls", len(api.createUserCalls))
	}
}

func TestResolveOrProvisionRelayUser_ProvisionsLDAPFallbackEmail(t *testing.T) {
	api := &fakeRelayIdentityAPI{findResult: nil}

	r := NewRelayIdentityResolver(api, "ldap.local")
	u, err := r.ResolveOrProvisionRelayUser(context.Background(), "carol", "")
	if err != nil {
		t.Fatalf("ResolveOrProvisionRelayUser() unexpected error: %v", err)
	}
	if u == nil {
		t.Fatal("expected non-nil relay user")
	}
	if len(api.createUserCalls) != 1 {
		t.Fatalf("expected CreateUser called once, got %d", len(api.createUserCalls))
	}

	req := api.createUserCalls[0]
	if req.Username != "carol" {
		t.Fatalf("expected Username=carol, got %q", req.Username)
	}
	if req.Email != "carol@ldap.local" {
		t.Fatalf("expected fallback Email=carol@ldap.local, got %q", req.Email)
	}
	if req.Notes != "provisioned_by_ai_efficiency_ldap" {
		t.Fatalf("expected Notes provisioned_by_ai_efficiency_ldap, got %q", req.Notes)
	}
	if len(req.Password) < 32 {
		t.Fatalf("expected high-entropy password length >= 32, got %d", len(req.Password))
	}
}
