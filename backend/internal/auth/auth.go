package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/golang-jwt/jwt/v5"
	"go.uber.org/zap"
)

// UserInfo represents authenticated user information.
type UserInfo struct {
	ID          int    `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Role        string `json:"role"`
	AuthSource  string `json:"auth_source"`
	RelayUserID *int   `json:"relay_user_id,omitempty"`
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
		// Try all providers in order
		for _, p := range s.providers {
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
func (s *Service) ValidateAccessToken(tokenStr string) (jwt.MapClaims, error) {
	return s.validateToken(tokenStr, "access")
}

// GenerateTokenPairForUser generates a token pair for the given user info.
// Exported for integration testing.
func (s *Service) GenerateTokenPairForUser(info *UserInfo) (*TokenPair, error) {
	return s.generateTokenPair(info)
}

func (s *Service) ensureLocalUser(ctx context.Context, info *UserInfo) (*ent.User, error) {
	// Try to find existing user by username
	u, err := s.entClient.User.Query().
		Where(entuser.UsernameEQ(info.Username)).
		Only(ctx)
	if err == nil {
		// If we already have a relay user ID stored, copy it to the session user info.
		if info.RelayUserID == nil && u.RelayUserID != nil {
			info.RelayUserID = u.RelayUserID
		}

		// LDAP path: provision relay-side identity when we don't have it yet.
		if info.RelayUserID == nil && s.relayIdentityResolver != nil {
			relayUser, relayPassword, err := s.relayIdentityResolver.ResolveOrProvisionWithPassword(ctx, info.Username, info.Email)
			if err != nil {
				return nil, fmt.Errorf("resolve relay identity: %w", err)
			}
			relayID := int(relayUser.ID)
			info.RelayUserID = &relayID
			if strings.TrimSpace(relayPassword) != "" {
				info.RelayAuthPassword = relayPassword
			}
			info.Email = ensureNonEmptyEmail(info.Email, relayUser.Email, "")
			u, err = u.Update().SetRelayUserID(relayID).Save(ctx)
			if err != nil {
				return nil, fmt.Errorf("persist relay user id: %w", err)
			}
		}

		// Sync role from auth provider on each login
		if string(u.Role) != info.Role && info.Role != "" {
			u, err = u.Update().
				SetRole(entuser.Role(info.Role)).
				Save(ctx)
			if err != nil {
				return nil, fmt.Errorf("sync user role: %w", err)
			}
		}
		if err := s.persistRelayAuthPassword(ctx, u.ID, info.RelayAuthPassword); err != nil {
			return nil, err
		}
		if info.RelayAuthPassword != "" {
			u, err = s.entClient.User.Get(ctx, u.ID)
			if err != nil {
				return nil, fmt.Errorf("reload user: %w", err)
			}
		}
		return u, nil
	}
	if !ent.IsNotFound(err) {
		return nil, err
	}

	if info.RelayUserID == nil && s.relayIdentityResolver != nil {
		relayUser, relayPassword, err := s.relayIdentityResolver.ResolveOrProvisionWithPassword(ctx, info.Username, info.Email)
		if err != nil {
			return nil, fmt.Errorf("resolve relay identity: %w", err)
		}
		relayID := int(relayUser.ID)
		info.RelayUserID = &relayID
		if strings.TrimSpace(relayPassword) != "" {
			info.RelayAuthPassword = relayPassword
		}
		info.Email = ensureNonEmptyEmail(info.Email, relayUser.Email, "")
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
