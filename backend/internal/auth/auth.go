package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// UserInfo represents authenticated user information.
type UserInfo struct {
	ID                int    `json:"id"`
	Username          string `json:"username"`
	Email             string `json:"email"`
	Role              string `json:"role"`
	AuthSource        string `json:"auth_source"`
	RelayUserID       *int   `json:"relay_user_id,omitempty"`
	RelayAuthPassword string `json:"-"`
}

// TokenPair contains access and refresh tokens.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// LoginRequest represents a login request.
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Source   string `json:"source"` // "sso" or "ldap", empty = try both
}

// AuthProvider is the interface for authentication providers.
type AuthProvider interface {
	Authenticate(ctx context.Context, username, password string) (*UserInfo, error)
	Name() string
}

// Service handles authentication logic.
type Service struct {
	providers             []AuthProvider
	entClient             *ent.Client
	jwtSecret             []byte
	encryptionKey         string
	accessTokenTTL        time.Duration
	refreshTokenTTL       time.Duration
	relayIdentityResolver *RelayIdentityResolver
	logger                *zap.Logger
}

// NewService creates a new auth service.
func NewService(entClient *ent.Client, jwtSecret string, accessTTL, refreshTTL int, logger *zap.Logger, encryptionKeys ...string) *Service {
	if len(jwtSecret) < 16 {
		logger.Fatal("JWT secret must be at least 16 characters", zap.Int("length", len(jwtSecret)))
	}
	if accessTTL <= 0 {
		accessTTL = 7200
	}
	if refreshTTL <= 0 {
		refreshTTL = 604800
	}
	return &Service{
		entClient:       entClient,
		jwtSecret:       []byte(jwtSecret),
		encryptionKey:   firstNonEmptyString(encryptionKeys...),
		accessTokenTTL:  time.Duration(accessTTL) * time.Second,
		refreshTokenTTL: time.Duration(refreshTTL) * time.Second,
		logger:          logger,
	}
}

func (s *Service) SetRelayIdentityResolver(r *RelayIdentityResolver) {
	s.relayIdentityResolver = r
}

// RegisterProvider adds an auth provider.
func (s *Service) RegisterProvider(p AuthProvider) {
	s.providers = append(s.providers, p)
}

func (s *Service) defaultLoginProviders() []AuthProvider {
	if len(s.providers) < 2 {
		return s.providers
	}

	ordered := make([]AuthProvider, 0, len(s.providers))
	for _, p := range s.providers {
		if strings.EqualFold(p.Name(), "ldap") {
			ordered = append(ordered, p)
		}
	}
	for _, p := range s.providers {
		if !strings.EqualFold(p.Name(), "ldap") {
			ordered = append(ordered, p)
		}
	}
	return ordered
}

// Login authenticates a user and returns a token pair.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*TokenPair, *UserInfo, error) {
	var userInfo *UserInfo
	var lastErr error

	if req.Source != "" {
		// Try specific provider
		found := false
		for _, p := range s.providers {
			if strings.EqualFold(p.Name(), req.Source) {
				found = true
				userInfo, lastErr = p.Authenticate(ctx, req.Username, req.Password)
				break
			}
		}
		if !found {
			return nil, nil, fmt.Errorf("unknown auth source: %s", req.Source)
		}
	} else {
		// Default login prefers LDAP and falls back to the remaining providers.
		for _, p := range s.defaultLoginProviders() {
			userInfo, lastErr = p.Authenticate(ctx, req.Username, req.Password)
			if userInfo != nil {
				break
			}
		}
	}

	if userInfo == nil {
		if lastErr != nil {
			return nil, nil, fmt.Errorf("authentication failed: %w", lastErr)
		}
		return nil, nil, fmt.Errorf("authentication failed: invalid credentials")
	}

	// Ensure local user exists
	localUser, err := s.ensureLocalUser(ctx, userInfo)
	if err != nil {
		return nil, nil, fmt.Errorf("ensure local user: %w", err)
	}
	userInfo.ID = localUser.ID
	userInfo.Role = string(localUser.Role)

	// Generate tokens
	tokens, err := s.generateTokenPair(userInfo)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	return tokens, userInfo, nil
}

// RefreshToken validates a refresh token and issues a new token pair.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, *UserInfo, error) {
	claims, err := s.validateToken(refreshToken, "refresh")
	if err != nil {
		return nil, nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	userID, ok := claims["user_id"].(float64)
	if !ok {
		return nil, nil, fmt.Errorf("invalid token claims")
	}

	// Fetch user from DB
	u, err := s.entClient.User.Get(ctx, int(userID))
	if err != nil {
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	if err := s.ensureTokenValidForUser(u, claims); err != nil {
		return nil, nil, fmt.Errorf("invalid refresh token: %w", err)
	}

	userInfo := &UserInfo{
		ID:         u.ID,
		Username:   u.Username,
		Email:      u.Email,
		Role:       string(u.Role),
		AuthSource: string(u.AuthSource),
	}

	tokens, err := s.generateTokenPair(userInfo)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}

	return tokens, userInfo, nil
}

// ValidateAccessToken validates an access token and returns claims.
func (s *Service) ValidateAccessToken(ctx context.Context, tokenStr string) (jwt.MapClaims, error) {
	claims, err := s.validateToken(tokenStr, "access")
	if err != nil {
		return nil, err
	}
	if err := s.ensureTokenNotRevoked(ctx, claims); err != nil {
		return nil, err
	}
	return claims, nil
}

// GenerateTokenPairForUser generates a token pair for the given user info.
// Exported for integration testing.
func (s *Service) GenerateTokenPairForUser(info *UserInfo) (*TokenPair, error) {
	return s.generateTokenPair(info)
}

func (s *Service) RevokeUserTokens(ctx context.Context, userID int, revokedAt time.Time) error {
	if s == nil || s.entClient == nil {
		return fmt.Errorf("auth service is not configured")
	}
	if userID <= 0 {
		return fmt.Errorf("user id is required")
	}
	if revokedAt.IsZero() {
		revokedAt = time.Now()
	}
	if _, err := s.entClient.User.UpdateOneID(userID).SetTokenValidAfter(revokedAt).Save(ctx); err != nil {
		return fmt.Errorf("revoke user tokens: %w", err)
	}
	return nil
}

func (s *Service) ensureLocalUser(ctx context.Context, info *UserInfo) (*ent.User, error) {
	ldapLogin := strings.EqualFold(info.AuthSource, "ldap")
	if ldapLogin {
		info.RelayAuthPassword = ""
	}

	// Try to find existing user by username
	u, err := s.entClient.User.Query().
		Where(entuser.UsernameEQ(info.Username)).
		Only(ctx)
	if err == nil {
		return s.syncExistingLocalUser(ctx, u, info)
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}

	if strings.TrimSpace(info.Email) != "" {
		u, err = s.entClient.User.Query().
			Where(entuser.EmailEQ(info.Email)).
			Only(ctx)
		if err == nil {
			return s.syncExistingLocalUser(ctx, u, info)
		}
		if err != nil && !ent.IsNotFound(err) {
			return nil, err
		}
	}

	if info.RelayUserID == nil && s.relayIdentityResolver != nil {
		relayUser, relayPassword, err := s.relayIdentityResolver.ResolveOrProvisionForLDAP(ctx, info.Username, info.Email)
		if err != nil {
			return nil, fmt.Errorf("resolve relay identity: %w", err)
		}
		relayID := int(relayUser.ID)
		info.RelayUserID = &relayID
		if strings.TrimSpace(relayPassword) != "" {
			info.RelayAuthPassword = relayPassword
		} else {
			relayPassword, err := s.relayIdentityResolver.ResetGeneratedPasswordForLDAPProvisionedUser(ctx, relayUser)
			if err != nil {
				return nil, fmt.Errorf("reset relay auth password: %w", err)
			}
			if strings.TrimSpace(relayPassword) != "" {
				info.RelayAuthPassword = relayPassword
			}
		}
		info.Email = ensureNonEmptyEmail(info.Email, relayUser.Email, "")
		info.Role = roleFromRelayIdentity(info.Role, relayUser.Role)
	}

	// LDAP may not provide a `mail` attribute; avoid creating a local user with an empty
	// email because Ent schema marks it NotEmpty.
	if strings.TrimSpace(info.Email) == "" && strings.EqualFold(info.AuthSource, "ldap") {
		info.Email = ensureNonEmptyEmail(info.Email, info.Username, "")
	}

	// Create new user
	create := s.entClient.User.Create().
		SetUsername(info.Username).
		SetEmail(info.Email).
		SetAuthSource(entuser.AuthSource(info.AuthSource)).
		SetRole(entuser.Role(info.Role))

	if info.RelayUserID != nil {
		create.SetRelayUserID(*info.RelayUserID)
	}
	if encrypted, err := s.encryptRelayAuthPassword(info.RelayAuthPassword); err != nil {
		return nil, err
	} else if encrypted != "" {
		create.SetRelayAuthPassword(encrypted)
	}

	return create.Save(ctx)
}

func (s *Service) syncExistingLocalUser(ctx context.Context, u *ent.User, info *UserInfo) (*ent.User, error) {
	ldapLogin := strings.EqualFold(info.AuthSource, "ldap")
	if ldapLogin {
		info.RelayAuthPassword = ""
	}
	var resolvedRelayUser *relay.User

	if info.RelayUserID == nil && u.RelayUserID != nil {
		info.RelayUserID = u.RelayUserID
	}

	// LDAP path: always re-resolve the relay identity so we can repair historical
	// misbindings caused by stale/ignored lookup filters on the relay side.
	if ldapLogin && s.relayIdentityResolver != nil {
		relayUser, relayPassword, err := s.relayIdentityResolver.ResolveOrProvisionForLDAP(ctx, info.Username, info.Email)
		if err != nil {
			return nil, fmt.Errorf("resolve relay identity: %w", err)
		}
		relayID := int(relayUser.ID)
		resolvedRelayUser = relayUser
		info.RelayUserID = &relayID
		if strings.TrimSpace(relayPassword) != "" {
			info.RelayAuthPassword = relayPassword
		}
		info.Email = ensureNonEmptyEmail(info.Email, relayUser.Email, "")
		info.Role = roleFromRelayIdentity(info.Role, relayUser.Role)
	}

	relayBindingMoved := false
	if info.RelayUserID != nil && (u.RelayUserID == nil || *u.RelayUserID != *info.RelayUserID) {
		if u.RelayUserID != nil {
			relayBindingMoved = true
			s.logger.Warn("repairing stored relay user binding",
				zap.String("username", info.Username),
				zap.Int("old_relay_user_id", *u.RelayUserID),
				zap.Int("new_relay_user_id", *info.RelayUserID),
			)
		}
		updated, err := u.Update().SetRelayUserID(*info.RelayUserID).Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("persist relay user id: %w", err)
		}
		u = updated
	}
	if ldapLogin && relayBindingMoved && strings.TrimSpace(info.RelayAuthPassword) == "" && s.relayIdentityResolver != nil {
		relayPassword, err := s.relayIdentityResolver.ResetGeneratedPasswordForLDAPProvisionedUser(ctx, resolvedRelayUser)
		if err != nil {
			return nil, fmt.Errorf("reset relay auth password: %w", err)
		}
		if strings.TrimSpace(relayPassword) != "" {
			info.RelayAuthPassword = relayPassword
		}
	}

	if strings.TrimSpace(info.AuthSource) != "" && string(u.AuthSource) != info.AuthSource {
		updated, err := u.Update().
			SetAuthSource(entuser.AuthSource(info.AuthSource)).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("sync user auth source: %w", err)
		}
		u = updated
	}

	// Sync role from auth provider on each login
	if string(u.Role) != info.Role && info.Role != "" {
		updated, err := u.Update().
			SetRole(entuser.Role(info.Role)).
			Save(ctx)
		if err != nil {
			return nil, fmt.Errorf("sync user role: %w", err)
		}
		u = updated
	}
	if err := s.persistRelayAuthPassword(ctx, u.ID, info.RelayAuthPassword); err != nil {
		return nil, err
	}
	if info.RelayAuthPassword != "" {
		reloaded, err := s.entClient.User.Get(ctx, u.ID)
		if err != nil {
			return nil, fmt.Errorf("reload user: %w", err)
		}
		u = reloaded
	}
	return u, nil
}

func roleFromRelayIdentity(currentRole, relayRole string) string {
	relayRole = strings.ToLower(strings.TrimSpace(relayRole))
	switch relayRole {
	case string(entuser.RoleAdmin), string(entuser.RoleUser):
		return relayRole
	default:
		return currentRole
	}
}

func (s *Service) encryptRelayAuthPassword(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return "", nil
	}
	if strings.TrimSpace(s.encryptionKey) == "" {
		return "", fmt.Errorf("encrypt relay auth password: encryption key is required")
	}
	encrypted, err := pkg.Encrypt(password, s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("encrypt relay auth password: %w", err)
	}
	return encrypted, nil
}

func (s *Service) DecryptRelayAuthPassword(ciphertext string) (string, error) {
	ciphertext = strings.TrimSpace(ciphertext)
	if ciphertext == "" {
		return "", nil
	}
	if strings.TrimSpace(s.encryptionKey) == "" {
		return "", fmt.Errorf("decrypt relay auth password: encryption key is required")
	}
	plaintext, err := pkg.Decrypt(ciphertext, s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("decrypt relay auth password: %w", err)
	}
	return plaintext, nil
}

func (s *Service) persistRelayAuthPassword(ctx context.Context, userID int, password string) error {
	encrypted, err := s.encryptRelayAuthPassword(password)
	if err != nil {
		return err
	}
	if encrypted == "" {
		return nil
	}
	if _, err := s.entClient.User.UpdateOneID(userID).SetRelayAuthPassword(encrypted).Save(ctx); err != nil {
		return fmt.Errorf("persist relay auth password: %w", err)
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func (s *Service) generateTokenPair(info *UserInfo) (*TokenPair, error) {
	now := time.Now()

	// Access token
	accessClaims := jwt.MapClaims{
		"user_id":  info.ID,
		"username": info.Username,
		"role":     info.Role,
		"type":     "access",
		"iat":      now.Unix(),
		"exp":      now.Add(s.accessTokenTTL).Unix(),
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("sign access token: %w", err)
	}

	// Refresh token
	refreshClaims := jwt.MapClaims{
		"user_id": info.ID,
		"type":    "refresh",
		"iat":     now.Unix(),
		"exp":     now.Add(s.refreshTokenTTL).Unix(),
	}
	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshStr, err := refreshToken.SignedString(s.jwtSecret)
	if err != nil {
		return nil, fmt.Errorf("sign refresh token: %w", err)
	}

	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresIn:    int(s.accessTokenTTL.Seconds()),
	}, nil
}

func (s *Service) validateToken(tokenStr, expectedType string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	tokenType, _ := claims["type"].(string)
	if tokenType != expectedType {
		return nil, fmt.Errorf("wrong token type: expected %s, got %s", expectedType, tokenType)
	}

	return claims, nil
}

func (s *Service) ensureTokenNotRevoked(ctx context.Context, claims jwt.MapClaims) error {
	if s == nil || s.entClient == nil {
		return nil
	}
	userID, err := userIDFromClaims(claims)
	if err != nil {
		return err
	}
	u, err := s.entClient.User.Get(ctx, userID)
	if err != nil {
		return fmt.Errorf("get user: %w", err)
	}
	return s.ensureTokenValidForUser(u, claims)
}

func (s *Service) ensureTokenValidForUser(u *ent.User, claims jwt.MapClaims) error {
	if u == nil || u.TokenValidAfter == nil {
		return nil
	}
	issuedAt, err := issuedAtFromClaims(claims)
	if err != nil {
		return err
	}
	if issuedAt.Before(*u.TokenValidAfter) {
		return fmt.Errorf("token revoked")
	}
	return nil
}

func userIDFromClaims(claims jwt.MapClaims) (int, error) {
	switch v := claims["user_id"].(type) {
	case float64:
		if v <= 0 {
			return 0, fmt.Errorf("invalid token claims")
		}
		return int(v), nil
	case int:
		if v <= 0 {
			return 0, fmt.Errorf("invalid token claims")
		}
		return v, nil
	case int64:
		if v <= 0 {
			return 0, fmt.Errorf("invalid token claims")
		}
		return int(v), nil
	default:
		return 0, fmt.Errorf("invalid token claims")
	}
}

func issuedAtFromClaims(claims jwt.MapClaims) (time.Time, error) {
	switch v := claims["iat"].(type) {
	case float64:
		return time.Unix(int64(v), 0), nil
	case int64:
		return time.Unix(v, 0), nil
	case int:
		return time.Unix(int64(v), 0), nil
	default:
		return time.Time{}, fmt.Errorf("invalid token issued-at claim")
	}
}
