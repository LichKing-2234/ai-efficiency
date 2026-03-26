package auth

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ai-efficiency/backend/internal/config"
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
