package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// OIDCConfig describes a standard authorization-code OIDC client. Deployment
// values stay outside the open-source application defaults.
type OIDCConfig struct {
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

type oidcPending struct {
	nonce, verifier, redirect string
	callback                  string
	expires                   time.Time
}

type oidcCompleted struct {
	tokens   *TokenPair
	user     *UserInfo
	redirect string
	expires  time.Time
}

// OIDCService owns provider discovery, state/PKCE and one-use browser handoffs.
type OIDCService struct {
	config     OIDCConfig
	httpClient *http.Client
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	oauth      oauth2.Config
	mu         sync.Mutex
	pending    map[string]oidcPending
	done       map[string]oidcCompleted
}

func NewOIDCService(ctx context.Context, cfg OIDCConfig) (*OIDCService, error) {
	cfg.Issuer = strings.TrimSpace(cfg.Issuer)
	if cfg.Issuer == "" || cfg.Issuer == "/" || strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" || strings.TrimSpace(cfg.RedirectURL) == "" {
		return nil, fmt.Errorf("OIDC requires issuer, client id, client secret and redirect URL")
	}
	if len(cfg.RedirectURLs) == 0 {
		cfg.RedirectURLs = []string{cfg.RedirectURL}
	}
	if cfg.HTTPClient != nil {
		ctx = oidc.ClientContext(ctx, cfg.HTTPClient)
	}
	provider, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("OIDC discovery: %w", err)
	}
	if len(cfg.Scopes) == 0 {
		cfg.Scopes = []string{oidc.ScopeOpenID, "profile", "email"}
	}
	s := &OIDCService{
		config:     cfg,
		httpClient: cfg.HTTPClient,
		provider:   provider,
		verifier:   provider.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret,
			Endpoint: provider.Endpoint(), RedirectURL: cfg.RedirectURL, Scopes: cfg.Scopes,
		},
		pending: make(map[string]oidcPending),
		done:    make(map[string]oidcCompleted),
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

func (s *OIDCService) Begin(redirect, callbackURL string) (state, urlValue string, err error) {
	if callbackURL == "" {
		callbackURL = s.config.RedirectURL
	}
	allowed := false
	for _, value := range s.config.RedirectURLs {
		if callbackURL == value {
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
	s.mu.Lock()
	now := time.Now()
	for key, value := range s.pending {
		if now.After(value.expires) {
			delete(s.pending, key)
		}
	}
	if len(s.pending) >= 128 {
		s.mu.Unlock()
		return "", "", fmt.Errorf("OIDC login capacity reached")
	}
	s.pending[state] = oidcPending{nonce: nonce, verifier: verifier, redirect: redirect, callback: callbackURL, expires: time.Now().Add(5 * time.Minute)}
	s.mu.Unlock()
	oauth := s.oauth
	oauth.RedirectURL = callbackURL
	return state, oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), nil
}

func (s *OIDCService) Complete(ctx context.Context, state, code string, authService *Service) (string, string, error) {
	if s.httpClient != nil {
		ctx = oidc.ClientContext(ctx, s.httpClient)
	}
	s.mu.Lock()
	pending, ok := s.pending[state]
	delete(s.pending, state)
	s.mu.Unlock()
	if !ok || time.Now().After(pending.expires) || strings.TrimSpace(code) == "" {
		return "", "", fmt.Errorf("invalid OIDC authorization state")
	}
	oauth := s.oauth
	oauth.RedirectURL = pending.callback
	token, err := oauth.Exchange(ctx, code, oauth2.VerifierOption(pending.verifier))
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
	if idToken.Nonce != pending.nonce {
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
	tokens, user, err := authService.LoginOIDC(ctx, identity)
	if err != nil {
		return "", "", err
	}
	handoff, err := randomOIDCValue()
	if err != nil {
		return "", "", err
	}
	s.mu.Lock()
	now := time.Now()
	for key, value := range s.done {
		if now.After(value.expires) {
			delete(s.done, key)
		}
	}
	s.done[handoff] = oidcCompleted{tokens: tokens, user: user, redirect: pending.redirect, expires: time.Now().Add(2 * time.Minute)}
	s.mu.Unlock()
	return handoff, pending.redirect, nil
}

func (s *OIDCService) Take(handoff string) (*TokenPair, *UserInfo, string, error) {
	s.mu.Lock()
	value, ok := s.done[handoff]
	if ok {
		delete(s.done, handoff)
	}
	s.mu.Unlock()
	if !ok || time.Now().After(value.expires) {
		return nil, nil, "", fmt.Errorf("OIDC handoff expired")
	}
	return value.tokens, value.user, value.redirect, nil
}

func (s *Service) SetOIDCService(provider *OIDCService) { s.oidc = provider }
func (s *Service) OIDCEnabled() bool                    { return s != nil && s.oidc != nil }
func (s *Service) OIDC() *OIDCService {
	if s == nil {
		return nil
	}
	return s.oidc
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
	info := &UserInfo{ID: u.ID, Username: u.Username, Email: u.Email, Role: string(u.Role), AuthSource: string(entuser.AuthSourceOidc)}
	tokens, err := s.generateTokenPair(info)
	if err != nil {
		return nil, nil, err
	}
	return tokens, info, nil
}

func (s *Service) linkOrCreateOIDCUser(ctx context.Context, identity OIDCIdentity) (*ent.User, error) {
	if !s.oidc.config.AllowEmailLink || !identity.EmailVerified {
		return nil, fmt.Errorf("OIDC identity is not bound and email linking is not allowed")
	}
	u, err := s.entClient.User.Query().Where(entuser.EmailEQ(identity.Email)).Only(ctx)
	if err == nil {
		if u.OidcIssuer != nil || u.OidcSubject != nil {
			return nil, fmt.Errorf("email already belongs to another external identity")
		}
		return u.Update().SetAuthSource(entuser.AuthSourceOidc).SetOidcIssuer(identity.Issuer).SetOidcSubject(identity.Subject).Save(ctx)
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	return s.entClient.User.Create().SetUsername(identity.Username).SetEmail(identity.Email).SetAuthSource(entuser.AuthSourceOidc).SetRole(entuser.RoleUser).SetOidcIssuer(identity.Issuer).SetOidcSubject(identity.Subject).Save(ctx)
}
