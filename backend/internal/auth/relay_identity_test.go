package auth

import (
	"context"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
)

type fakeRelayIdentityAPI struct {
	findByUsernameCalls []string
	createUserCalls     []relay.CreateUserRequest
	updateUserCalls     []struct {
		userID int64
		req    relay.UpdateUserRequest
	}

	findResult *relay.User
}

type relayIdentityLookupAPI struct {
	findFn   func(context.Context, string) (*relay.User, error)
	createFn func(context.Context, relay.CreateUserRequest) (*relay.User, error)
	updateFn func(context.Context, int64, relay.UpdateUserRequest) (*relay.User, error)
}

func (r relayIdentityLookupAPI) FindUserByUsername(ctx context.Context, username string) (*relay.User, error) {
	return r.findFn(ctx, username)
}

func (r relayIdentityLookupAPI) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	return r.createFn(ctx, req)
}

func (r relayIdentityLookupAPI) UpdateUser(ctx context.Context, userID int64, req relay.UpdateUserRequest) (*relay.User, error) {
	return r.updateFn(ctx, userID, req)
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

func (f *fakeRelayIdentityAPI) UpdateUser(_ context.Context, userID int64, req relay.UpdateUserRequest) (*relay.User, error) {
	f.updateUserCalls = append(f.updateUserCalls, struct {
		userID int64
		req    relay.UpdateUserRequest
	}{userID: userID, req: req})
	return &relay.User{
		ID:       userID,
		Username: "alice",
		Email:    "alice@example.com",
		Role:     "user",
	}, nil
}

func TestResolveOrProvisionRelayUser_UsesUsernameAsStableKey(t *testing.T) {
	api := &fakeRelayIdentityAPI{
		findResult: &relay.User{ID: 7, Username: "alice", Email: "alice@example.com"},
	}

	r := NewRelayIdentityResolver(api, "ldap.local")
	u, err := r.ResolveOrProvision(context.Background(), "alice", "completely-different@example.com")
	if err != nil {
		t.Fatalf("ResolveOrProvision() unexpected error: %v", err)
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

func TestResolveOrProvisionRelayUser_ProvisionsCanonicalUsernameAndDefaultConcurrency(t *testing.T) {
	api := &fakeRelayIdentityAPI{findResult: nil}

	r := NewRelayIdentityResolver(api, "ldap.local")
	u, _, err := r.ResolveOrProvisionWithPassword(context.Background(), "carol@agora.io", "carol@agora.io", "ldap-pass")
	if err != nil {
		t.Fatalf("ResolveOrProvisionWithPassword() unexpected error: %v", err)
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
	if req.Email != "carol@agora.io" {
		t.Fatalf("expected Email=carol@agora.io, got %q", req.Email)
	}
	if req.Password != "ldap-pass" {
		t.Fatalf("expected Password to use current LDAP password, got %q", req.Password)
	}
	if req.Concurrency != 5 {
		t.Fatalf("expected Concurrency=5, got %d", req.Concurrency)
	}
	if req.Notes != "provisioned_by_ai_efficiency_ldap" {
		t.Fatalf("expected Notes provisioned_by_ai_efficiency_ldap, got %q", req.Notes)
	}
}

func TestResolveOrProvisionRelayUser_UpdatesExistingUserPasswordAndConcurrency(t *testing.T) {
	api := &fakeRelayIdentityAPI{
		findResult: &relay.User{ID: 7, Username: "alice", Email: "alice@example.com", Concurrency: 0},
	}

	r := NewRelayIdentityResolver(api, "ldap.local")
	u, password, err := r.ResolveOrProvisionWithPassword(context.Background(), "alice@agora.io", "alice@agora.io", "ldap-pass")
	if err != nil {
		t.Fatalf("ResolveOrProvisionWithPassword() unexpected error: %v", err)
	}
	if u == nil || u.ID != 7 {
		t.Fatalf("expected existing relay user ID=7, got %+v", u)
	}
	if password != "ldap-pass" {
		t.Fatalf("returned password = %q, want %q", password, "ldap-pass")
	}
	if len(api.createUserCalls) != 0 {
		t.Fatalf("expected CreateUser not called when user exists, got %d calls", len(api.createUserCalls))
	}
	if len(api.updateUserCalls) != 1 {
		t.Fatalf("expected UpdateUser called once, got %d", len(api.updateUserCalls))
	}
	if api.updateUserCalls[0].userID != 7 {
		t.Fatalf("expected UpdateUser userID=7, got %d", api.updateUserCalls[0].userID)
	}
	if api.updateUserCalls[0].req.Password != "ldap-pass" {
		t.Fatalf("expected UpdateUser password ldap-pass, got %q", api.updateUserCalls[0].req.Password)
	}
	if api.updateUserCalls[0].req.Concurrency == nil || *api.updateUserCalls[0].req.Concurrency != 5 {
		t.Fatalf("expected UpdateUser concurrency=5, got %+v", api.updateUserCalls[0].req.Concurrency)
	}
}

func TestResolveOrProvisionRelayUser_FallsBackToLegacyEmailUsernameAndRenames(t *testing.T) {
	api := &fakeRelayIdentityAPI{
		findResult: nil,
	}

	rawUsername := "alice@agora.io"
	legacy := &relay.User{ID: 7, Username: rawUsername, Email: rawUsername, Concurrency: 1}
	api.findByUsernameCalls = nil
	api.findResult = nil

	r := NewRelayIdentityResolver(api, "ldap.local")
	// Override lookup behavior: canonical lookup misses, legacy full-email lookup hits.
	lookupCount := 0
	api2 := &fakeRelayIdentityAPI{}
	r.api = relayIdentityLookupAPI{
		findFn: func(_ context.Context, username string) (*relay.User, error) {
			lookupCount++
			api2.findByUsernameCalls = append(api2.findByUsernameCalls, username)
			if username == "alice" {
				return nil, nil
			}
			if username == rawUsername {
				return legacy, nil
			}
			return nil, nil
		},
		createFn: api2.CreateUser,
		updateFn: api2.UpdateUser,
	}

	u, _, err := r.ResolveOrProvisionWithPassword(context.Background(), rawUsername, rawUsername, "ldap-pass")
	if err != nil {
		t.Fatalf("ResolveOrProvisionWithPassword() unexpected error: %v", err)
	}
	if u == nil || u.ID != 7 {
		t.Fatalf("expected existing relay user ID=7, got %+v", u)
	}
	if lookupCount != 2 {
		t.Fatalf("expected 2 lookups, got %d", lookupCount)
	}
	if got := api2.findByUsernameCalls; len(got) != 2 || got[0] != "alice" || got[1] != rawUsername {
		t.Fatalf("unexpected lookup sequence: %+v", got)
	}
	if len(api2.updateUserCalls) != 1 {
		t.Fatalf("expected UpdateUser called once, got %d", len(api2.updateUserCalls))
	}
	if api2.updateUserCalls[0].req.Username != "alice" {
		t.Fatalf("expected UpdateUser username rename to alice, got %q", api2.updateUserCalls[0].req.Username)
	}
}
