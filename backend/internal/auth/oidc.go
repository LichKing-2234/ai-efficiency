package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/authstate"
	"github.com/coreos/go-oidc/v3/oidc"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
)

var (
	// ErrOIDCIdentityConflict reports a local account bound to a different
	// subject under the currently configured issuer. A binding from another
	// issuer is treated as stale environment state and is re-bound after a
	// verified-email sign-in; a same-issuer subject conflict is never re-bound.
	ErrOIDCIdentityConflict = errors.New("OIDC identity conflicts with another external identity")
	// ErrOIDCEmailLinkUnavailable reports an unbound identity whose verified
	// email cannot be linked or provisioned under the current configuration.
	ErrOIDCEmailLinkUnavailable = errors.New("OIDC identity is not bound and email linking is not allowed")
)

const (
	oidcPendingTTL     = 5 * time.Minute
	oidcHandoffTTL     = 2 * time.Minute
	oidcLogoutJTITTL   = 10 * time.Minute
	oidcPendingKeyBase = "oidc:pending:"
	oidcHandoffKeyBase = "oidc:handoff:"
	oidcLogoutKeyBase  = "oidc:logout-jti:"
)

// OIDCConfig describes a standard authorization-code OIDC client. Deployment
// values stay outside the open-source application defaults.
type OIDCConfig struct {
	Enabled        bool
	Label          string
	Issuer         string
	ClientID       string
	ClientSecret   string
	RedirectURL    string
	RedirectURLs   []string
	AllowEmailLink bool
	Scopes         []string
	HTTPClient     *http.Client
}

type OIDCIdentity struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Username      string
}

// OIDCDiscovery is the provider metadata surfaced to administrators.
type OIDCDiscovery struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	UserinfoEndpoint      string `json:"userinfo_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

// OIDCLogoutIdentity identifies the subject of a verified back-channel logout
// token.
type OIDCLogoutIdentity struct {
	Issuer    string
	Subject   string
	SessionID string
}

type oidcPending struct {
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Redirect string `json:"redirect"`
	Callback string `json:"callback"`
}

type oidcHandoff struct {
	User     UserInfo `json:"user"`
	Redirect string   `json:"redirect"`
}

// OIDCService owns provider discovery, state/PKCE and one-use browser handoffs.
// Durable provider configuration lives in this process; the short-lived state
// that joins a browser redirect back to the application lives in the auth state
// store so any replica can finish the flow.
type OIDCService struct {
	config     OIDCConfig
	httpClient *http.Client
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	oauth      oauth2.Config
	discovery  OIDCDiscovery
	keySet     *oidc.RemoteKeySet
	state      authstate.Store
}

func NewOIDCService(ctx context.Context, cfg OIDCConfig) (*OIDCService, error) {
	return NewOIDCServiceWithStore(ctx, cfg, authstate.NewMemoryStore())
}

// NewOIDCServiceWithStore builds the OIDC client with an explicit auth state
// store. Deployments pass the shared Redis store; tests and single-process
// deployments may keep the memory store.
func NewOIDCServiceWithStore(ctx context.Context, cfg OIDCConfig, state authstate.Store) (*OIDCService, error) {
	if state == nil {
		state = authstate.NewMemoryStore()
	}
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	if cfg.Issuer == "" || cfg.Issuer == "/" || strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" || strings.TrimSpace(cfg.RedirectURL) == "" {
		return nil, fmt.Errorf("OIDC requires issuer, client id, client secret and redirect URL")
	}
	redirectURLs := make([]string, 0, len(cfg.RedirectURLs))
	for _, value := range cfg.RedirectURLs {
		for _, item := range strings.Split(value, ",") {
			if item = strings.TrimSpace(item); item != "" {
				redirectURLs = append(redirectURLs, item)
			}
		}
	}
	if len(redirectURLs) == 0 {
		redirectURLs = []string{cfg.RedirectURL}
	}
	cfg.RedirectURLs = redirectURLs
	if cfg.HTTPClient != nil {
		ctx = oidc.ClientContext(ctx, cfg.HTTPClient)
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	var discovery OIDCDiscovery
	if err := provider.Claims(&discovery); err != nil {
		return nil, fmt.Errorf("OIDC discovery claims: %w", err)
	}
	if strings.TrimSpace(discovery.Issuer) == "" {
		discovery.Issuer = cfg.Issuer
	}
	endpoint := provider.Endpoint()
	if discovery.AuthorizationEndpoint == "" {
		discovery.AuthorizationEndpoint = endpoint.AuthURL
	}
	if discovery.TokenEndpoint == "" {
		discovery.TokenEndpoint = endpoint.TokenURL
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	keysetCtx := context.Background()
	if cfg.HTTPClient != nil {
		keysetCtx = oidc.ClientContext(keysetCtx, cfg.HTTPClient)
	}
	var keySet *oidc.RemoteKeySet
	if strings.TrimSpace(discovery.JWKSURI) != "" {
		keySet = oidc.NewRemoteKeySet(keysetCtx, discovery.JWKSURI)
	}
	s := &OIDCService{
		config:     cfg,
		httpClient: cfg.HTTPClient,
		provider:   provider,
		verifier:   provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
			Endpoint: endpoint, RedirectURL: cfg.RedirectURL, Scopes: cfg.Scopes,
		},
		discovery: discovery,
		keySet:    keySet,
		state:     state,
	}
	return s, nil
}

func (s *OIDCService) Enabled() bool { return s != nil }

func randomOIDCValue() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func oidcPendingKey(state string) string { return oidcPendingKeyBase + state }
func oidcHandoffKey(id string) string    { return oidcHandoffKeyBase + id }
func oidcLogoutJTIKey(jti string) string {
	return oidcLogoutKeyBase + fmt.Sprintf("%x", sha256.Sum256([]byte(jti)))
}

func (s *OIDCService) Begin(ctx context.Context, redirect, callbackURL string) (state, urlValue string, err error) {
	if callbackURL == "" {
		callbackURL = s.config.RedirectURL
	}
	allowed := false
	for _, value := range s.config.RedirectURLs {
		if callbackURL == value {
			allowed = true
			break
		}
		requested, requestedErr := url.Parse(callbackURL)
		configured, configuredErr := url.Parse(value)
		if requestedErr == nil && configuredErr == nil && requested.Host == configured.Host && configured.Scheme == "https" {
			callbackURL = value
			allowed = true
			break
		}
	}
	if !allowed {
		return "", "", fmt.Errorf("OIDC callback URL is not configured")
	}
	state, err = randomOIDCValue()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomOIDCValue()
	if err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	if redirect == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		redirect = "/"
	}
	pending, err := json.Marshal(oidcPending{Nonce: nonce, Verifier: verifier, Redirect: redirect, Callback: callbackURL})
	if err != nil {
		return "", "", fmt.Errorf("encode OIDC pending state: %w", err)
	}
	if err := s.state.Set(ctx, oidcPendingKey(state), pending, oidcPendingTTL); err != nil {
		return "", "", fmt.Errorf("store OIDC pending state: %w", err)
	}
	oauth := s.oauth
	oauth.RedirectURL = callbackURL
	return state, oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

func (s *OIDCService) Complete(ctx context.Context, state, code string, authService *Service) (string, string, error) {
	if s.httpClient != nil {
		ctx = oidc.ClientContext(ctx, s.httpClient)
	}
	rawPending, found, err := s.state.Take(ctx, oidcPendingKey(state))
	if err != nil {
		return "", "", fmt.Errorf("load OIDC pending state: %w", err)
	}
	if !found || strings.TrimSpace(code) == "" {
		return "", "", fmt.Errorf("invalid OIDC authorization state")
	}
	var pending oidcPending
	if err := json.Unmarshal(rawPending, &pending); err != nil {
		return "", "", fmt.Errorf("decode OIDC pending state: %w", err)
	}
	oauth := s.oauth
	oauth.RedirectURL = pending.Callback
	token, err := oauth.Exchange(ctx, code, oauth2.VerifierOption(pending.Verifier))
	if err != nil {
		return "", "", fmt.Errorf("OIDC code exchange: %w", err)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		return "", "", fmt.Errorf("OIDC response missing id token")
	}
	idToken, err := s.verifier.Verify(ctx, rawID)
	if err != nil {
		return "", "", fmt.Errorf("OIDC token verification: %w", err)
	}
	if idToken.Nonce != pending.Nonce {
		return "", "", fmt.Errorf("OIDC nonce mismatch")
	}
	var claims struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", "", fmt.Errorf("OIDC claims: %w", err)
	}
	userinfo, err := s.provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
	if err != nil {
		return "", "", fmt.Errorf("OIDC userinfo: %w", err)
	}
	if userinfo.Subject != idToken.Subject {
		return "", "", fmt.Errorf("OIDC subject mismatch")
	}
	var userClaims struct {
		Email             string `json:"email"`
		EmailVerified     bool   `json:"email_verified"`
		PreferredUsername string `json:"preferred_username"`
		Name              string `json:"name"`
	}
	if err := userinfo.Claims(&userClaims); err != nil {
		return "", "", fmt.Errorf("OIDC userinfo claims: %w", err)
	}
	if userClaims.Email != "" {
		if claims.Email != "" && !strings.EqualFold(strings.TrimSpace(claims.Email), strings.TrimSpace(userClaims.Email)) {
			return "", "", fmt.Errorf("OIDC email mismatch between ID token and UserInfo")
		}
		claims.Email = userClaims.Email
		claims.EmailVerified = claims.EmailVerified && userClaims.EmailVerified
	}
	if !strings.Contains(claims.Email, "@") {
		return "", "", fmt.Errorf("OIDC email missing")
	}
	username := claims.PreferredUsername
	if username == "" {
		username = userClaims.PreferredUsername
	}
	if username == "" {
		username = claims.Email
	}
	identity := OIDCIdentity{Issuer: s.config.Issuer, Subject: idToken.Subject, Email: strings.TrimSpace(claims.Email), EmailVerified: claims.EmailVerified, Username: username}
	// The token pair generated during sign-in is intentionally discarded; only
	// the resolved local identity is handed to the browser, and a fresh pair is
	// issued when the one-use handoff is consumed.
	_, user, err := authService.LoginOIDC(ctx, identity)
	if err != nil {
		return "", "", err
	}
	handoff, err := randomOIDCValue()
	if err != nil {
		return "", "", err
	}
	payload, err := json.Marshal(oidcHandoff{User: *user, Redirect: pending.Redirect})
	if err != nil {
		return "", "", fmt.Errorf("encode OIDC handoff: %w", err)
	}
	if err := s.state.Set(ctx, oidcHandoffKey(handoff), payload, oidcHandoffTTL); err != nil {
		return "", "", fmt.Errorf("store OIDC handoff: %w", err)
	}
	return handoff, pending.Redirect, nil
}

// Take consumes a one-use browser handoff and returns the resolved identity. It
// deliberately does not store tokens in the shared state store; the caller
// issues the token pair when the handoff is consumed.
func (s *OIDCService) Take(ctx context.Context, handoff string) (*UserInfo, string, error) {
	raw, found, err := s.state.Take(ctx, oidcHandoffKey(handoff))
	if err != nil {
		return nil, "", fmt.Errorf("load OIDC handoff: %w", err)
	}
	if !found {
		return nil, "", fmt.Errorf("OIDC handoff expired")
	}
	var payload oidcHandoff
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, "", fmt.Errorf("decode OIDC handoff: %w", err)
	}
	user := payload.User
	return &user, payload.Redirect, nil
}

func (s *Service) SetOIDCService(provider *OIDCService) { s.oidc.Store(provider) }

func (s *Service) OIDCEnabled() bool {
	return s != nil && s.oidc.Load() != nil
}

func (s *Service) OIDC() *OIDCService {
	if s == nil {
		return nil
	}
	return s.oidc.Load()
}

// OIDCLabel returns the administrator-configured login button label.
func (s *Service) OIDCLabel() string {
	provider := s.OIDC()
	if provider == nil {
		return ""
	}
	return provider.config.Label
}

// Config returns the runtime OIDC configuration, including the client secret.
func (s *OIDCService) Config() OIDCConfig {
	if s == nil {
		return OIDCConfig{}
	}
	return s.config
}

// Discovery returns the discovered provider metadata.
func (s *OIDCService) Discovery() OIDCDiscovery {
	if s == nil {
		return OIDCDiscovery{}
	}
	return s.discovery
}

const backchannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"

// VerifyLogoutToken validates an OIDC Back-Channel Logout token issued by the
// configured provider. Signature verification uses the provider JWKS; the
// claims checks follow OpenID Connect Back-Channel Logout 1.0.
func (s *OIDCService) VerifyLogoutToken(ctx context.Context, raw string) (OIDCLogoutIdentity, error) {
	if s == nil || s.keySet == nil {
		return OIDCLogoutIdentity{}, fmt.Errorf("OIDC back-channel logout is not configured")
	}
	payload, err := s.keySet.VerifySignature(ctx, raw)
	if err != nil {
		return OIDCLogoutIdentity{}, fmt.Errorf("verify logout token signature: %w", err)
	}
	var claims struct {
		Issuer    string                     `json:"iss"`
		Audience  json.RawMessage            `json:"aud"`
		IssuedAt  int64                      `json:"iat"`
		Expiry    int64                      `json:"exp"`
		JTI       string                     `json:"jti"`
		Subject   string                     `json:"sub"`
		SessionID string                     `json:"sid"`
		Nonce     string                     `json:"nonce"`
		Events    map[string]json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return OIDCLogoutIdentity{}, fmt.Errorf("decode logout token claims: %w", err)
	}
	if claims.Issuer != s.config.Issuer {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token issuer mismatch")
	}
	if !audienceContains(claims.Audience, s.config.ClientID) {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token audience mismatch")
	}
	if _, ok := claims.Events[backchannelLogoutEvent]; !ok {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token events claim missing")
	}
	if strings.TrimSpace(claims.Nonce) != "" {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token must not carry a nonce")
	}
	now := time.Now()
	if claims.IssuedAt == 0 || now.Sub(time.Unix(claims.IssuedAt, 0)).Abs() > 10*time.Minute {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token issued at is not recent")
	}
	if claims.Expiry != 0 && now.After(time.Unix(claims.Expiry, 0)) {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token is expired")
	}
	if strings.TrimSpace(claims.JTI) == "" {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token jti is missing")
	}
	if strings.TrimSpace(claims.Subject) == "" {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token subject is missing")
	}
	fresh, err := s.recordLogoutJTI(ctx, claims.JTI)
	if err != nil {
		return OIDCLogoutIdentity{}, fmt.Errorf("record logout token jti: %w", err)
	}
	if !fresh {
		return OIDCLogoutIdentity{}, fmt.Errorf("logout token was already processed")
	}
	return OIDCLogoutIdentity{Issuer: claims.Issuer, Subject: claims.Subject, SessionID: claims.SessionID}, nil
}

func (s *OIDCService) recordLogoutJTI(ctx context.Context, jti string) (bool, error) {
	return s.state.SetIfAbsent(ctx, oidcLogoutJTIKey(jti), []byte("1"), oidcLogoutJTITTL)
}

func audienceContains(raw json.RawMessage, clientID string) bool {
	if len(raw) == 0 {
		return false
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return single == clientID
	}
	var multiple []string
	if err := json.Unmarshal(raw, &multiple); err == nil {
		for _, value := range multiple {
			if value == clientID {
				return true
			}
		}
	}
	return false
}

func (s *Service) LoginOIDC(ctx context.Context, identity OIDCIdentity) (*TokenPair, *UserInfo, error) {
	if !s.OIDCEnabled() {
		return nil, nil, fmt.Errorf("OIDC disabled")
	}
	if identity.Issuer == "" || identity.Subject == "" || identity.Email == "" {
		return nil, nil, fmt.Errorf("OIDC identity is incomplete")
	}
	u, err := s.entClient.User.Query().Where(entuser.OidcIssuerEQ(identity.Issuer), entuser.OidcSubjectEQ(identity.Subject)).Only(ctx)
	if ent.IsNotFound(err) {
		u, err = s.linkOrCreateOIDCUser(ctx, identity)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("resolve OIDC identity: %w", err)
	}
	if u.RelayDisabledAt != nil {
		return nil, nil, fmt.Errorf("local account disabled")
	}
	u, err = s.ensureOIDCRelayIdentity(ctx, u)
	if err != nil {
		return nil, nil, err
	}
	info := &UserInfo{ID: u.ID, Username: u.Username, Email: u.Email, Role: string(u.Role), AuthSource: string(entuser.AuthSourceOidc)}
	if u.RelayUserID != nil {
		relayID := *u.RelayUserID
		info.RelayUserID = &relayID
	}
	tokens, err := s.generateTokenPair(info)
	if err != nil {
		return nil, nil, err
	}
	return tokens, info, nil
}

// ensureOIDCRelayIdentity binds a verified OIDC sign-in to its Relay identity.
// The first sign-in of a local user without a Relay binding resolves an exact
// Relay email match, then the canonical or legacy username, and provisions a
// Relay user with a generated password when none exists. Existing bindings are
// never replaced. A failed resolution fails the sign-in so the next attempt
// retries instead of continuing with a half-onboarded account.
func (s *Service) ensureOIDCRelayIdentity(ctx context.Context, u *ent.User) (*ent.User, error) {
	if u == nil || u.RelayUserID != nil || s.relayIdentityResolver == nil {
		return u, nil
	}
	relayUser, relayPassword, err := s.relayIdentityResolver.ResolveOrProvisionForOIDC(ctx, u.Username, u.Email)
	if err != nil {
		return nil, fmt.Errorf("resolve relay identity: %w", err)
	}
	if relayUser == nil || relayUser.ID <= 0 {
		return nil, fmt.Errorf("resolve relay identity: missing relay user id")
	}
	update := u.Update().SetRelayUserID(int(relayUser.ID))
	if password := strings.TrimSpace(relayPassword); password != "" {
		encrypted, err := s.encryptRelayAuthPassword(password)
		if err != nil {
			return nil, err
		}
		if encrypted != "" {
			update.SetRelayAuthPassword(encrypted)
		}
	}
	updated, err := update.Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("persist relay user binding: %w", err)
	}
	s.logger.Info("bound relay identity on OIDC sign-in",
		zap.Int("user_id", updated.ID),
		zap.Int64("relay_user_id", relayUser.ID),
	)
	return updated, nil
}

func (s *Service) linkOrCreateOIDCUser(ctx context.Context, identity OIDCIdentity) (*ent.User, error) {
	provider := s.OIDC()
	if provider == nil || !provider.config.AllowEmailLink || !identity.EmailVerified {
		return nil, fmt.Errorf("OIDC identity is not bound and email linking is not allowed: %w", ErrOIDCEmailLinkUnavailable)
	}
	u, err := s.entClient.User.Query().Where(entuser.EmailEQ(identity.Email)).Only(ctx)
	if err == nil {
		if u.OidcIssuer != nil || u.OidcSubject != nil {
			storedIssuer := ""
			if u.OidcIssuer != nil {
				storedIssuer = strings.TrimSpace(*u.OidcIssuer)
			}
			if storedIssuer != "" && storedIssuer == strings.TrimSpace(identity.Issuer) {
				return nil, fmt.Errorf("email already belongs to a different external identity under the same issuer: %w", ErrOIDCIdentityConflict)
			}
			// The stored binding points at a different issuer, which can only be
			// stale state from another environment or an earlier client. This
			// deployment verifies and trusts only its configured issuer, and the
			// verified email belongs to this account, so replace the stale binding
			// instead of locking the account out.
			s.logger.Info("rebound OIDC identity after issuer change",
				zap.Int("user_id", u.ID),
				zap.String("previous_issuer", storedIssuer),
				zap.String("issuer", identity.Issuer),
			)
		}
		return u.Update().SetAuthSource(entuser.AuthSourceOidc).SetOidcIssuer(identity.Issuer).SetOidcSubject(identity.Subject).Save(ctx)
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	return s.entClient.User.Create().SetUsername(identity.Username).SetEmail(identity.Email).SetAuthSource(entuser.AuthSourceOidc).SetRole(entuser.RoleUser).SetOidcIssuer(identity.Issuer).SetOidcSubject(identity.Subject).Save(ctx)
}
