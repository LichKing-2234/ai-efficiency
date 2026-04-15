package auth

import (
	"context"
	"crypto/tls"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/go-ldap/ldap/v3"
	"go.uber.org/zap"
)

// LDAPProvider authenticates users against an LDAP directory.
type LDAPProvider struct {
	cfgPtr *atomic.Pointer[config.LDAPConfig]
	logger *zap.Logger
}

// NewLDAPProvider creates a new LDAP provider backed by a dynamic config pointer.
func NewLDAPProvider(cfgPtr *atomic.Pointer[config.LDAPConfig], logger *zap.Logger) *LDAPProvider {
	return &LDAPProvider{
		cfgPtr: cfgPtr,
		logger: logger,
	}
}

// Name returns the provider name.
func (p *LDAPProvider) Name() string {
	return "ldap"
}

// Authenticate verifies credentials against the LDAP directory.
func (p *LDAPProvider) Authenticate(ctx context.Context, username, password string) (*UserInfo, error) {
	cfg := p.cfgPtr.Load()
	if cfg == nil || cfg.URL == "" {
		return nil, fmt.Errorf("ldap: not configured")
	}

	// Connect to LDAP
	conn, err := ldap.DialURL(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("ldap dial: %w", err)
	}
	defer conn.Close()

	// Upgrade to TLS if configured
	if cfg.TLS {
		if err := conn.StartTLS(&tls.Config{InsecureSkipVerify: false}); err != nil {
			return nil, fmt.Errorf("ldap starttls: %w", err)
		}
	}

	// Bind with service account to search
	if err := conn.Bind(cfg.BindDN, cfg.BindPassword); err != nil {
		return nil, fmt.Errorf("ldap service bind: %w", err)
	}

	// Search for user
	userFilter := cfg.UserFilter
	if userFilter == "" {
		userFilter = "(uid=%s)"
	}
	// Support Java-style {0} placeholder
	if strings.Contains(userFilter, "{0}") {
		userFilter = strings.ReplaceAll(userFilter, "{0}", "%s")
	}
	if !strings.HasPrefix(userFilter, "(") {
		userFilter = "(" + userFilter + ")"
	}
	filter := fmt.Sprintf(userFilter, ldap.EscapeFilter(username))
	searchReq := ldap.NewSearchRequest(
		cfg.BaseDN,
		ldap.ScopeWholeSubtree,
		ldap.NeverDerefAliases,
		1, 0, false,
		filter,
		[]string{"dn", "uid", "mail", "cn"},
		nil,
	)

	result, err := conn.Search(searchReq)
	if err != nil {
		return nil, fmt.Errorf("ldap search: %w", err)
	}
	if len(result.Entries) == 0 {
		return nil, fmt.Errorf("ldap: user not found")
	}

	entry := result.Entries[0]

	// Bind with user credentials to verify password
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, fmt.Errorf("ldap: invalid credentials")
	}

	stableUsername := ldapStableUsername(username, entry)
	email := ldapDerivedEmail(entry.GetAttributeValue("mail"), username, stableUsername)

	p.logger.Info("LDAP authentication successful",
		zap.String("username", stableUsername),
		zap.String("login", strings.TrimSpace(username)),
	)

	return &UserInfo{
		Username:          stableUsername,
		Email:             email,
		Role:              "user",
		AuthSource:        "ldap",
		RelayAuthPassword: password,
	}, nil
}

func ldapStableUsername(loginInput string, entry *ldap.Entry) string {
	uid := strings.TrimSpace(entry.GetAttributeValue("uid"))
	if uid != "" {
		return uid
	}
	return strings.TrimSpace(loginInput)
}

func ldapDerivedEmail(mailAttr, loginInput, stableUsername string) string {
	// Prefer the LDAP attribute itself; otherwise:
	// - if login input looks like an email, preserve it
	// - else derive from the stable username
	loginInput = strings.TrimSpace(loginInput)
	fallbackBase := stableUsername
	if strings.Contains(loginInput, "@") {
		fallbackBase = loginInput
	}
	return ensureNonEmptyEmail(mailAttr, fallbackBase, "")
}
