package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	entuser "github.com/ai-efficiency/backend/ent/user"
)

// countingProvider records how many times Authenticate was called so tests can
// assert that a denied login never reaches the LDAP provider.
type countingProvider struct {
	name  string
	calls int
	info  *UserInfo
	err   error
}

func (p *countingProvider) Name() string { return p.name }

func (p *countingProvider) Authenticate(ctx context.Context, username, password string) (*UserInfo, error) {
	p.calls++
	return p.info, p.err
}

func newSSOOnlyUser(t *testing.T, svc *Service, username string) {
	t.Helper()
	_, err := svc.entClient.User.Create().
		SetUsername(username).
		SetEmail(username + "@invalid.account").
		SetAuthSource(entuser.AuthSourceRelaySSO).
		SetLoginPolicy(entuser.LoginPolicySSOOnly).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create sso_only user: %v", err)
	}
}

func TestLoginSSOOnlyAccountSkipsLDAP(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	newSSOOnlyUser(t, svc, "team-alpha")

	sso := &countingProvider{name: "sso", info: &UserInfo{ID: 900, Username: "team-alpha", Email: "team-alpha@example.com", AuthSource: "relay_sso"}}
	ldap := &countingProvider{name: "ldap", err: errors.New("ldap should not run")}
	svc.RegisterProvider(ldap)
	svc.RegisterProvider(sso)

	_, _, err := svc.Login(context.Background(), LoginRequest{Username: "team-alpha", Password: "relay-password"})
	if err != nil {
		t.Fatalf("login error: %v", err)
	}
	if ldap.calls != 0 {
		t.Errorf("ldap provider calls = %d, want 0", ldap.calls)
	}
	if sso.calls != 1 {
		t.Errorf("sso provider calls = %d, want 1", sso.calls)
	}
}

func TestLoginSSOOnlyAccountRejectsExplicitLDAP(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	newSSOOnlyUser(t, svc, "team-beta")

	sso := &countingProvider{name: "sso", info: &UserInfo{Username: "team-beta"}}
	ldap := &countingProvider{name: "ldap", info: &UserInfo{Username: "team-beta"}}
	svc.RegisterProvider(ldap)
	svc.RegisterProvider(sso)

	_, _, err := svc.Login(context.Background(), LoginRequest{Username: "team-beta", Password: "ldap-password", Source: "ldap"})
	if !errors.Is(err, ErrSSOOnlyAccount) {
		t.Fatalf("error = %v, want ErrSSOOnlyAccount", err)
	}
	if ldap.calls != 0 {
		t.Errorf("ldap provider calls = %d, want 0", ldap.calls)
	}
	if sso.calls != 0 {
		t.Errorf("sso provider calls = %d, want 0", sso.calls)
	}
}

func TestLoginSSOOnlyAccountAllowsExplicitSSO(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	newSSOOnlyUser(t, svc, "team-gamma")

	sso := &countingProvider{name: "sso", info: &UserInfo{Username: "team-gamma", AuthSource: "relay_sso"}}
	ldap := &countingProvider{name: "ldap", err: errors.New("ldap should not run")}
	svc.RegisterProvider(ldap)
	svc.RegisterProvider(sso)

	if _, _, err := svc.Login(context.Background(), LoginRequest{Username: "team-gamma", Password: "relay-password", Source: "sso"}); err != nil {
		t.Fatalf("login error: %v", err)
	}
	if ldap.calls != 0 {
		t.Errorf("ldap provider calls = %d, want 0", ldap.calls)
	}
}

// Existing accounts keep the LDAP-first default order.
func TestLoginDefaultPolicyStillPrefersLDAP(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	_, err := svc.entClient.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	ldap := &countingProvider{name: "ldap", info: &UserInfo{Username: "alice", AuthSource: "ldap"}}
	sso := &countingProvider{name: "sso", err: errors.New("sso should not run")}
	svc.RegisterProvider(sso)
	svc.RegisterProvider(ldap)

	if _, _, err := svc.Login(context.Background(), LoginRequest{Username: "alice", Password: "secret"}); err != nil {
		t.Fatalf("login error: %v", err)
	}
	if ldap.calls != 1 {
		t.Errorf("ldap provider calls = %d, want 1", ldap.calls)
	}
	if sso.calls != 0 {
		t.Errorf("sso provider calls = %d, want 0", sso.calls)
	}
}

// An unknown username must not be blocked by the policy check.
func TestLoginUnknownUserKeepsDefaultBehaviour(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)

	ldap := &countingProvider{name: "ldap", err: errors.New("no such user")}
	svc.RegisterProvider(ldap)

	_, _, err := svc.Login(context.Background(), LoginRequest{Username: "nobody", Password: "x"})
	if err == nil {
		t.Fatal("expected login failure")
	}
	if strings.Contains(err.Error(), ErrSSOOnlyAccount.Error()) {
		t.Fatalf("unexpected sso-only rejection for unknown user: %v", err)
	}
	if ldap.calls != 1 {
		t.Errorf("ldap provider calls = %d, want 1", ldap.calls)
	}
}

// The relay authenticates by email, so a username login must be translated to
// the stored mailbox before the SSO provider runs.
func TestLoginSSOOnlyAccountTranslatesUsernameToEmail(t *testing.T) {
	svc, _ := newTestServiceWithDB(t)
	newSSOOnlyUser(t, svc, "testaccount")

	var gotUsername string
	recorder := &recordingProvider{name: "sso", info: &UserInfo{Username: "testaccount", Email: "testaccount@invalid.account", AuthSource: "relay_sso"}, record: &gotUsername}
	svc.RegisterProvider(recorder)

	if _, _, err := svc.Login(context.Background(), LoginRequest{Username: "testaccount", Password: "relay-password-1"}); err != nil {
		t.Fatalf("login error: %v", err)
	}
	if gotUsername != "testaccount@invalid.account" {
		t.Fatalf("provider received %q, want the stored email", gotUsername)
	}
}

type recordingProvider struct {
	name   string
	info   *UserInfo
	record *string
}

func (p *recordingProvider) Name() string { return p.name }

func (p *recordingProvider) Authenticate(ctx context.Context, username, password string) (*UserInfo, error) {
	*p.record = username
	return p.info, nil
}
