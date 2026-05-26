package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

const relaySSOProvisioningNote = "provisioned_by_ai_efficiency_sso"

// SSOProvider authenticates users against the relay server.
type SSOProvider struct {
	relayProvider relay.Provider
	logger        *zap.Logger
}

// NewSSOProvider creates a new SSO provider.
func NewSSOProvider(relayProvider relay.Provider, logger *zap.Logger) *SSOProvider {
	return &SSOProvider{
		relayProvider: relayProvider,
		logger:        logger,
	}
}

// Name returns the provider name.
func (p *SSOProvider) Name() string {
	return "sso"
}

// Authenticate verifies credentials against the relay server.
func (p *SSOProvider) Authenticate(ctx context.Context, username, password string) (*UserInfo, error) {
	if p.relayProvider == nil {
		return nil, nil
	}

	relayUser, err := p.relayProvider.Authenticate(ctx, username, password)
	if err != nil {
		if errors.Is(err, relay.ErrInvalidCredentials) {
			relayUser, provisionErr := p.provisionMissingRelayUserForSSO(ctx, username, password)
			if provisionErr != nil {
				p.logger.Warn("relay SSO: missing-user provisioning failed", zap.String("username", username), zap.Error(provisionErr))
				return nil, nil
			}
			if relayUser != nil {
				return relayUserInfo(relayUser, password), nil
			}
			p.logger.Debug("relay SSO: invalid credentials", zap.String("username", username))
			return nil, nil
		}
		if errors.Is(err, relay.ErrExtraVerificationRequired) {
			p.logger.Warn("relay SSO: extra verification required, skipping", zap.String("username", username))
			return nil, nil
		}
		p.logger.Warn("relay SSO: authentication error", zap.Error(err))
		return nil, nil
	}

	return relayUserInfo(relayUser, password), nil
}

func (p *SSOProvider) provisionMissingRelayUserForSSO(ctx context.Context, username, password string) (*relay.User, error) {
	username = strings.TrimSpace(username)
	password = strings.TrimSpace(password)
	if username == "" || password == "" || !strings.Contains(username, "@") {
		return nil, nil
	}
	existing, err := p.relayProvider.FindUserByEmail(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("find relay user by email: %w", err)
	}
	if existing != nil {
		return nil, nil
	}
	canonicalUsername := relayProvisionUsername(username)
	if canonicalUsername != "" {
		existing, err = p.relayProvider.FindUserByUsername(ctx, canonicalUsername)
		if err != nil {
			return nil, fmt.Errorf("find relay user by username: %w", err)
		}
		if existing != nil {
			return nil, nil
		}
	}
	created, err := p.relayProvider.CreateUser(ctx, relay.CreateUserRequest{
		Username:    canonicalUsername,
		Email:       username,
		Password:    password,
		Notes:       relaySSOProvisioningNote,
		Concurrency: defaultRelayUserConcurrency,
	})
	if err != nil {
		return nil, fmt.Errorf("create relay user: %w", err)
	}
	return created, nil
}

func relayUserInfo(relayUser *relay.User, password string) *UserInfo {
	if relayUser == nil {
		return nil
	}
	relayID := int(relayUser.ID)
	role := relayUser.Role
	if role == "" {
		role = "user"
	}
	return &UserInfo{
		Username:          relayUser.Username,
		Email:             relayUser.Email,
		AuthSource:        "relay_sso",
		Role:              role,
		RelayUserID:       &relayID,
		RelayAuthPassword: password,
	}
}
