package orgaccount

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

const testEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// fakeRelay embeds relay.Provider so it satisfies the interface while only
// overriding the two calls the service actually uses.
type fakeRelay struct {
	relay.Provider
	nextID     int64
	createErr  error
	disableErr error
	created    []relay.CreateUserRequest
	disabled   []int64
}

func (f *fakeRelay) CreateUser(ctx context.Context, req relay.CreateUserRequest) (*relay.User, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	return &relay.User{ID: f.nextID}, nil
}

func (f *fakeRelay) DisableUser(ctx context.Context, userID int64) error {
	f.disabled = append(f.disabled, userID)
	return f.disableErr
}

type fakeResolver struct {
	provider relay.Provider
	err      error
}

func (r fakeResolver) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return r.provider, r.err
}

func newTestService(t *testing.T, rp relay.Provider) (*Service, *ent.Client) {
	t.Helper()
	client := testdb.Open(t)
	svc := NewService(client, fakeResolver{provider: rp}, testEncryptionKey, zap.NewNop())
	return svc, client
}

func TestCreateOrganizationAccount(t *testing.T) {
	rp := &fakeRelay{nextID: 4242}
	svc, client := newTestService(t, rp)

	created, err := svc.Create(context.Background(), CreateRequest{
		Username:   "team-alpha",
		Password:   "relay-password-1",
		ProviderID: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if created.RelayUserID != 4242 {
		t.Errorf("relay user id = %d, want 4242", created.RelayUserID)
	}
	if len(rp.created) != 1 {
		t.Fatalf("relay CreateUser calls = %d, want 1", len(rp.created))
	}
	if rp.created[0].Role != "user" {
		t.Errorf("relay role = %q, want user", rp.created[0].Role)
	}
	if len(rp.created[0].AllowedGroups) != 0 {
		t.Errorf("allowed groups = %v, want none", rp.created[0].AllowedGroups)
	}

	u, err := client.User.Get(context.Background(), created.UserID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if u.LoginPolicy != entuser.LoginPolicySSOOnly {
		t.Errorf("login policy = %v, want sso_only", u.LoginPolicy)
	}
	if u.AuthSource != entuser.AuthSourceRelaySSO {
		t.Errorf("auth source = %v, want relay_sso", u.AuthSource)
	}
	if u.Role != entuser.RoleUser {
		t.Errorf("role = %v, want user", u.Role)
	}
	if !strings.HasSuffix(u.Email, "@invalid.account") {
		t.Errorf("derived email = %q, want @org-account.invalid suffix", u.Email)
	}
	if u.Email != "team-alpha@invalid.account" {
		t.Errorf("derived email = %q, want team-alpha@invalid.account", u.Email)
	}
	if u.RelayAuthPassword == nil || *u.RelayAuthPassword == "" {
		t.Error("relay auth password was not stored")
	}
	if u.RelayAuthPassword != nil && *u.RelayAuthPassword == "relay-password-1" {
		t.Error("relay auth password must be stored encrypted")
	}
}

func TestCreateRejectsDuplicateUsername(t *testing.T) {
	rp := &fakeRelay{nextID: 1}
	svc, client := newTestService(t, rp)

	if _, err := client.User.Create().
		SetUsername("team-alpha").
		SetEmail("existing@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		Save(context.Background()); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	_, err := svc.Create(context.Background(), CreateRequest{
		Username:   "team-alpha",
		Password:   "relay-password-1",
		ProviderID: 1,
	})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("error = %v, want ConflictError", err)
	}
	if len(rp.created) != 0 {
		t.Errorf("relay CreateUser calls = %d, want 0", len(rp.created))
	}
}

// Derived addresses live under a reserved TLD, so a directory member sharing a
// corporate email can never block or claim the account.
func TestCreateIgnoresDirectoryMemberEmails(t *testing.T) {
	rp := &fakeRelay{nextID: 1}
	svc, client := newTestService(t, rp)

	if _, err := client.DirectoryMember.Create().
		SetSourceID(1).
		SetEmailNormalized("team-alpha@org-account.invalid").
		SetDisplayName("alice").
		SetLastSeenRunID(1).
		Save(context.Background()); err != nil {
		t.Fatalf("seed directory member: %v", err)
	}

	created, err := svc.Create(context.Background(), CreateRequest{
		Username:   "team-alpha",
		Password:   "relay-password-1",
		ProviderID: 1,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	u, err := client.User.Get(context.Background(), created.UserID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if !strings.HasSuffix(u.Email, "@invalid.account") {
		t.Errorf("derived email = %q, want @org-account.invalid suffix", u.Email)
	}
}

// When the local write fails after the relay user exists, the relay user is
// disabled so no orphan account can authenticate.
func TestCompensateDisablesRelayUser(t *testing.T) {
	rp := &fakeRelay{nextID: 7}
	svc, _ := newTestService(t, rp)

	svc.compensate(context.Background(), rp, 7, "unit-test")

	if len(rp.disabled) != 1 || rp.disabled[0] != 7 {
		t.Fatalf("disabled = %v, want [7]", rp.disabled)
	}
}

func TestCreateFailsWhenRelayUnavailable(t *testing.T) {
	rp := &fakeRelay{createErr: errors.New("relay down")}
	svc, _ := newTestService(t, rp)

	_, err := svc.Create(context.Background(), CreateRequest{
		Username:   "team-alpha",
		Password:   "relay-password-1",
		ProviderID: 1,
	})
	if err == nil {
		t.Fatal("expected create to fail")
	}
}

func TestCreateValidatesInput(t *testing.T) {
	rp := &fakeRelay{nextID: 1}
	svc, _ := newTestService(t, rp)

	cases := []CreateRequest{
		{Username: "", Password: "password-1", ProviderID: 1},
		{Username: "team alpha", Password: "password-1", ProviderID: 1},
		{Username: "team@alpha", Password: "password-1", ProviderID: 1},
		{Username: "team-alpha", Password: "", ProviderID: 1},
		{Username: "team-alpha", Password: "short", ProviderID: 1},
		{Username: "team-alpha", Password: "password-1", ProviderID: 0},
	}
	for _, req := range cases {
		_, err := svc.Create(context.Background(), req)
		var validation *ValidationError
		if !errors.As(err, &validation) {
			t.Errorf("request %+v: error = %v, want ValidationError", req, err)
		}
	}
	if len(rp.created) != 0 {
		t.Errorf("relay CreateUser calls = %d, want 0", len(rp.created))
	}
}
