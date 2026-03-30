package auth

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/go-ldap/ldap/v3"
	"go.uber.org/zap"
)

// newTestLDAPProvider wraps a config value into an atomic pointer for testing.
func newTestLDAPProvider(cfg config.LDAPConfig) *LDAPProvider {
	var ptr atomic.Pointer[config.LDAPConfig]
	ptr.Store(&cfg)
	return NewLDAPProvider(&ptr, zap.NewNop())
}

func TestLDAPProviderName(t *testing.T) {
	p := newTestLDAPProvider(config.LDAPConfig{})
	if p.Name() != "ldap" {
		t.Errorf("Name() = %q, want %q", p.Name(), "ldap")
	}
}

func TestNewLDAPProvider(t *testing.T) {
	p := newTestLDAPProvider(config.LDAPConfig{})
	if p == nil {
		t.Fatal("NewLDAPProvider returned nil")
	}
}

func TestLDAPProviderAuthenticateNotConfigured(t *testing.T) {
	p := newTestLDAPProvider(config.LDAPConfig{URL: ""})
	_, err := p.Authenticate(context.Background(), "user", "pass")
	if err == nil {
		t.Fatal("expected error for empty URL")
	}
	if !strings.Contains(err.Error(), "ldap: not configured") {
		t.Errorf("error = %q, want it to contain %q", err.Error(), "ldap: not configured")
	}
}

func TestLDAPStableUsername_PrefersUID(t *testing.T) {
	entry := &ldap.Entry{
		DN: "uid=alice,dc=example,dc=com",
		Attributes: []*ldap.EntryAttribute{
			{Name: "uid", Values: []string{"alice"}},
		},
	}

	got := ldapStableUsername("alice@example.com", entry)
	if got != "alice" {
		t.Fatalf("ldapStableUsername() = %q, want %q", got, "alice")
	}
}

func TestLDAPStableUsername_FallsBackToLoginInput(t *testing.T) {
	entry := &ldap.Entry{
		DN:         "cn=NoUid,dc=example,dc=com",
		Attributes: []*ldap.EntryAttribute{},
	}

	got := ldapStableUsername("  bob  ", entry)
	if got != "bob" {
		t.Fatalf("ldapStableUsername() = %q, want %q", got, "bob")
	}
}

func TestLDAPDerivedEmail_PreservesLoginEmailWhenMailMissing(t *testing.T) {
	got := ldapDerivedEmail("", "alias@example.com", "canonicaluid")
	if got != "alias@example.com" {
		t.Fatalf("ldapDerivedEmail() = %q, want %q", got, "alias@example.com")
	}
}

func TestLDAPDerivedEmail_FallsBackToStableUsernameWhenMailMissing(t *testing.T) {
	got := ldapDerivedEmail("", "bob", "canonicaluid")
	if got != "canonicaluid@ldap.local" {
		t.Fatalf("ldapDerivedEmail() = %q, want %q", got, "canonicaluid@ldap.local")
	}
}
