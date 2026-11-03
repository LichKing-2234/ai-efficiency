package auth

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/authstate"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func newSharedOIDCTestStore(t *testing.T) authstate.Store {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return authstate.NewRedisStore(client, "test-ns")
}

func newOIDCTestServiceAgainst(t *testing.T, issuer string, state authstate.Store) *OIDCService {
	t.Helper()
	service, err := NewOIDCServiceWithStore(context.Background(), OIDCConfig{
		Issuer:       issuer,
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  issuer + "/auth/oidc/callback",
	}, state)
	if err != nil {
		t.Fatalf("build OIDC service: %v", err)
	}
	return service
}

// TestOIDCPendingStateIsSharedAcrossInstances proves a flow begun on one
// process can be completed by another process sharing the same auth state
// store, which is the property multi-replica and rolling deployments need.
func TestOIDCPendingStateIsSharedAcrossInstances(t *testing.T) {
	state := newSharedOIDCTestStore(t)
	first, _, issuer := newLogoutTestServiceWithStore(t, state)
	second := newOIDCTestServiceAgainst(t, issuer, state)

	callback := issuer + "/auth/oidc/callback"
	beginState, authURL, err := first.Begin(context.Background(), "/usage", callback)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	if beginState == "" || !strings.Contains(authURL, "state="+beginState) {
		t.Fatalf("Begin() state=%q url=%q", beginState, authURL)
	}

	// The fake issuer has no token endpoint handler, so a cross-process Complete
	// fails during code exchange only when the other instance found the pending
	// state. A state-store miss would fail before the exchange.
	_, _, err = second.Complete(context.Background(), beginState, "code", nil)
	if err == nil || !strings.Contains(err.Error(), "OIDC code exchange") {
		t.Fatalf("cross-instance Complete() error = %v, want code exchange failure", err)
	}

	if _, _, err := second.Complete(context.Background(), beginState, "code", nil); err == nil || !strings.Contains(err.Error(), "invalid OIDC authorization state") {
		t.Fatalf("replayed Complete() error = %v, want invalid state", err)
	}
}

func TestOIDCHandoffStateIsSharedAcrossInstances(t *testing.T) {
	ctx := context.Background()
	state := newSharedOIDCTestStore(t)
	first, _, issuer := newLogoutTestServiceWithStore(t, state)
	second := newOIDCTestServiceAgainst(t, issuer, state)

	payload, err := json.Marshal(oidcHandoff{
		User:     UserInfo{ID: 42, Username: "alice", Email: "alice@example.com", Role: "user"},
		Redirect: "/usage",
	})
	if err != nil {
		t.Fatalf("encode handoff: %v", err)
	}
	if err := state.Set(ctx, oidcHandoffKey("handoff-shared"), payload, time.Minute); err != nil {
		t.Fatalf("store handoff: %v", err)
	}

	user, redirect, err := second.Take(ctx, "handoff-shared")
	if err != nil {
		t.Fatalf("cross-instance Take() error = %v", err)
	}
	if user.ID != 42 || user.Username != "alice" || redirect != "/usage" {
		t.Fatalf("Take() user=%+v redirect=%q", user, redirect)
	}
	if _, _, err := first.Take(ctx, "handoff-shared"); err == nil {
		t.Fatal("replayed Take() succeeded, want one-use rejection")
	}
}

// TestOIDCLogoutReplayRejectedAcrossInstances proves back-channel logout replay
// protection is shared, not per process.
func TestOIDCLogoutReplayRejectedAcrossInstances(t *testing.T) {
	state := newSharedOIDCTestStore(t)
	first, key, issuer := newLogoutTestServiceWithStore(t, state)
	second := newOIDCTestServiceAgainst(t, issuer, state)

	raw := signLogoutToken(t, key, logoutClaims(issuer, "jti-shared-replay"))
	if _, err := first.VerifyLogoutToken(context.Background(), raw); err != nil {
		t.Fatalf("first VerifyLogoutToken() error = %v", err)
	}
	if _, err := second.VerifyLogoutToken(context.Background(), raw); err == nil || !strings.Contains(err.Error(), "already processed") {
		t.Fatalf("cross-instance replay error = %v, want already processed", err)
	}
}
