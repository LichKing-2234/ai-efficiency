package auth

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/golang-jwt/jwt/v5"
)

// HCIProvider is separate from password providers: it never tries LDAP or Relay.
type HCIProvider struct{ secret, proxySecret []byte }

func NewHCIProvider(secret string, proxySecret ...string) *HCIProvider {
	p := &HCIProvider{secret: []byte(secret)}
	if len(proxySecret) > 0 {
		p.proxySecret = []byte(proxySecret[0])
	}
	return p
}

// ConfigureHCI is opt-in and fails closed without a verification boundary.
func (s *Service) ConfigureHCI(enabled bool, jwtSecret, proxySecret, frontendURL string) error {
	s.hci = nil
	if !enabled {
		return nil
	}
	if len(jwtSecret) > 0 && len(jwtSecret) < 32 {
		return fmt.Errorf("HCI JWT key must be at least 32 bytes")
	}
	if jwtSecret == "" && len(proxySecret) < 32 {
		return fmt.Errorf("HCI header login requires an ingress proof secret of at least 32 bytes")
	}
	u, err := url.Parse(frontendURL)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("HCI requires a valid frontend origin")
	}
	s.hciOrigin = u.Scheme + "://" + u.Host
	s.hci = NewHCIProvider(jwtSecret, proxySecret)
	return nil
}
func (s *Service) HCIEnabled() bool { return s != nil && s.hci != nil }

func (p *HCIProvider) AuthenticateHeaders(r *http.Request) (*UserInfo, error) {
	var email string
	if len(p.secret) > 0 {
		// A configured signature check is mandatory; missing JWT must not downgrade.
		raw := r.Header.Get("X-Hci-Auth-Jwt")
		tok, err := jwt.Parse(raw, func(t *jwt.Token) (any, error) { return p.secret, nil }, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !tok.Valid {
			return nil, fmt.Errorf("invalid HCI assertion")
		}
		claims, ok := tok.Claims.(jwt.MapClaims)
		if !ok {
			return nil, fmt.Errorf("invalid HCI claims")
		}
		user, ok := claims["user"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("missing HCI identity")
		}
		email, _ = user["email"].(string)
	} else {
		// The Web route overwrites this proof with a deployment-owned secret after
		// HCI authentication. Direct Service callers cannot forge ingress identity.
		proof := r.Header.Get("X-AE-HCI-Proxy-Secret")
		if len(p.proxySecret) < 32 || subtle.ConstantTimeCompare([]byte(proof), p.proxySecret) != 1 || r.Header.Get("X-Consumer-Username") != "agora-hciauth-v1" {
			return nil, fmt.Errorf("untrusted HCI ingress")
		}
		email = r.Header.Get("X-Oauth-Email")
	}
	email = strings.TrimSpace(email)
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return nil, fmt.Errorf("invalid HCI email")
	}
	return &UserInfo{Username: email, Email: email, Role: "user", AuthSource: "hci"}, nil
}

func (s *Service) LoginHCI(ctx context.Context, r *http.Request) (*TokenPair, *UserInfo, error) {
	if !s.HCIEnabled() {
		return nil, nil, fmt.Errorf("HCI disabled")
	}
	if r.Method != http.MethodPost || r.Header.Get("Origin") != s.hciOrigin || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return nil, nil, fmt.Errorf("invalid HCI browser request")
	}
	info, err := s.hci.AuthenticateHeaders(r)
	if err != nil {
		return nil, nil, err
	}
	u, err := s.hciLocalUser(ctx, info.Email)
	if err != nil {
		return nil, nil, err
	}
	info.ID = u.ID
	info.Username = u.Username
	info.Email = u.Email
	info.Role = string(u.Role)
	info.RelayUserID = u.RelayUserID
	pair, err := s.generateTokenPair(info)
	return pair, info, err
}

func (s *Service) hciLocalUser(ctx context.Context, email string) (*ent.User, error) {
	// Company identity matches only email, never a user-controlled username.
	u, err := s.entClient.User.Query().Where(entuser.EmailEqualFold(email)).Only(ctx)
	if ent.IsNotFound(err) {
		u, err = s.entClient.User.Create().SetUsername(email).SetEmail(email).SetAuthSource(entuser.AuthSourceHci).SetRole(entuser.RoleUser).Save(ctx)
		if ent.IsConstraintError(err) {
			u, err = s.entClient.User.Query().Where(entuser.EmailEqualFold(email)).Only(ctx)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("resolve HCI local identity: %w", err)
	}
	if u.RelayDisabledAt != nil {
		return nil, fmt.Errorf("local account disabled")
	}
	// Preserve local roles, usernames and Relay bindings/passwords. In particular,
	// signing in must not reset Relay credentials or downgrade an existing admin.
	if u.AuthSource != entuser.AuthSourceHci {
		return u.Update().SetAuthSource(entuser.AuthSourceHci).Save(ctx)
	}
	return u, nil
}
