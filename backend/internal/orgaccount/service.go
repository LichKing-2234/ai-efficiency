package orgaccount

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// ProviderResolver resolves a concrete relay provider by id.
type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

// Service creates organization accounts: relay users that exist independently
// of any natural person and can only authenticate through Relay SSO.
type Service struct {
	entClient     *ent.Client
	resolver      ProviderResolver
	encryptionKey string
	logger        *zap.Logger
}

func NewService(entClient *ent.Client, resolver ProviderResolver, encryptionKey string, logger *zap.Logger) *Service {
	return &Service{
		entClient:     entClient,
		resolver:      resolver,
		encryptionKey: encryptionKey,
		logger:        logger,
	}
}

// CreateRequest is the admin-supplied input for a new organization account.
// The administrator chooses the password, like a normal registration flow.
type CreateRequest struct {
	Username   string
	Password   string
	ProviderID int
}

// Created is the identity of the provisioned account. The password is never
// returned because the caller supplied it.
type Created struct {
	UserID      int
	RelayUserID int64
	Username    string
}

// minPasswordLength mirrors the usual registration floor for administrator
// chosen passwords.
const minPasswordLength = 8

// orgAccountEmailDomain is the derived mailbox domain for organization accounts.
//
// ".invalid" is reserved by RFC 2606 and can never resolve or receive mail, so a
// derived address can never collide with a real company address (all of which
// use the corporate domain). The domain keeps a dot because some email
// validators (including the relay admin API) reject single-label hosts.
// OrgAccountEmailDomain is the mailbox domain used for organization accounts.
// It is not a corporate mailbox domain, so a derived address can never match an
// employee address.
const OrgAccountEmailDomain = "invalid.account"

// Create provisions the relay user first, then the local user that pins the
// account to SSO-only login. If the local write fails the relay user is
// disabled so no orphan account can authenticate.
func (s *Service) Create(ctx context.Context, req CreateRequest) (*Created, error) {
	username := strings.TrimSpace(req.Username)

	if username == "" {
		return nil, &ValidationError{Message: "username is required"}
	}
	if strings.ContainsAny(username, " \t\r\n") {
		return nil, &ValidationError{Message: "username must not contain whitespace"}
	}
	if strings.Contains(username, "@") {
		return nil, &ValidationError{Message: "username must not contain @"}
	}
	if req.ProviderID <= 0 {
		return nil, &ValidationError{Message: "no enabled relay provider"}
	}
	if s.resolver == nil {
		return nil, &ValidationError{Message: "relay provider resolver is not configured"}
	}
	if strings.TrimSpace(s.encryptionKey) == "" {
		return nil, fmt.Errorf("encryption key is required")
	}

	// The relay logs in by email, so the address has to be derivable by the
	// operator. It stays outside the corporate domain to avoid collisions.
	email := username + "@" + OrgAccountEmailDomain

	if err := s.assertNoConflict(ctx, username, email); err != nil {
		return nil, err
	}

	rp, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider %d: %w", req.ProviderID, err)
	}

	password := req.Password
	if strings.TrimSpace(password) == "" {
		return nil, &ValidationError{Message: "password is required"}
	}
	if len(password) < minPasswordLength {
		return nil, &ValidationError{Message: "password is too short"}
	}

	relayUser, err := rp.CreateUser(ctx, relay.CreateUserRequest{
		Username: username,
		Email:    email,
		Password: password,
		Role:     "user",
	})
	if err != nil {
		return nil, fmt.Errorf("create relay user: %w", err)
	}

	encrypted, err := pkg.Encrypt(password, s.encryptionKey)
	if err != nil {
		s.compensate(ctx, rp, relayUser.ID, "encrypt relay password")
		return nil, fmt.Errorf("encrypt relay password: %w", err)
	}

	created, err := s.entClient.User.Create().
		SetUsername(username).
		SetEmail(email).
		SetAuthSource(entuser.AuthSourceRelaySSO).
		SetRole(entuser.RoleUser).
		SetLoginPolicy(entuser.LoginPolicySSOOnly).
		SetRelayUserID(int(relayUser.ID)).
		SetRelayAuthPassword(encrypted).
		Save(ctx)
	if err != nil {
		s.compensate(ctx, rp, relayUser.ID, "create local user")
		return nil, fmt.Errorf("create local user: %w", err)
	}

	return &Created{
		UserID:      created.ID,
		RelayUserID: relayUser.ID,
		Username:    created.Username,
	}, nil
}

// compensate disables the relay user created in the failed attempt and logs it
// so an operator can clean it up. The relay API has no delete call.
func (s *Service) compensate(ctx context.Context, rp relay.Provider, relayUserID int64, stage string) {
	disabler, ok := rp.(relay.UserDisabler)
	if !ok {
		s.logger.Error("organization account compensation skipped: relay provider cannot disable users",
			zap.Int64("relay_user_id", relayUserID), zap.String("stage", stage))
		return
	}
	if err := disabler.DisableUser(ctx, relayUserID); err != nil {
		s.logger.Error("organization account compensation failed",
			zap.Int64("relay_user_id", relayUserID), zap.String("stage", stage), zap.Error(err))
		return
	}
	s.logger.Warn("organization account relay user disabled after local failure",
		zap.Int64("relay_user_id", relayUserID), zap.String("stage", stage))
}

// assertNoConflict rejects identities that collide with an existing platform
// user or with a directory member, so LDAP provisioning can never claim the
// organization account later.
func (s *Service) assertNoConflict(ctx context.Context, username, email string) error {
	exists, err := s.entClient.User.Query().
		Where(entuser.UsernameEQ(username)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check username: %w", err)
	}
	if exists {
		return &ConflictError{Message: "username already exists"}
	}

	exists, err = s.entClient.User.Query().
		Where(entuser.EmailEQ(email)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check email: %w", err)
	}
	if exists {
		return &ConflictError{Message: "email already exists"}
	}

	return nil
}

// ValidationError marks caller input that failed validation.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// ConflictError marks identities already taken.
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }
