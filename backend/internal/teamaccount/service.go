// Package teamaccount manages AI accounts owned by a directory department.
//
// A team account is a Relay user that exists independently of every natural
// person: the platform stores its encrypted Relay password so it can mint API
// keys on the account's behalf, and the people who may operate it are recorded
// as owners and managers instead of being derived from the account itself.
package teamaccount

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/predicate"
	entteamaccount "github.com/ai-efficiency/backend/ent/teamaccount"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/directoryfacts"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

// DefaultConcurrency is the Relay concurrency granted to a team account. Relay
// provisioning defaults to a per-person value that is too small for a shared
// account, so the platform always states it explicitly.
const DefaultConcurrency = 20

// SubscriptionValidityDays is how long a provisioned subscription lasts.
const SubscriptionValidityDays = 365

// EmailDomain is the mailbox domain used for team accounts. It reuses the
// organization-account domain so every synthetic identity lives in one place.
const EmailDomain = "invalid.account"

const teamUsernamePrefix = "team-"

// ProviderResolver resolves a concrete Relay provider by id.
type ProviderResolver interface {
	Resolve(ctx context.Context, providerID int) (relay.Provider, error)
}

// RepresentativeScope resolves the departments a user represents.
type RepresentativeScope interface {
	Resolve(ctx context.Context, actorUserID int) (*representativescope.Scope, error)
}

// Service owns team account lifecycle and authorization.
type Service struct {
	client        *ent.Client
	resolver      ProviderResolver
	scopes        RepresentativeScope
	encryptionKey string
	logger        *zap.Logger
}

// New builds a Service.
func New(client *ent.Client, resolver ProviderResolver, scopes RepresentativeScope, encryptionKey string, logger *zap.Logger) *Service {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Service{
		client:        client,
		resolver:      resolver,
		scopes:        scopes,
		encryptionKey: strings.TrimSpace(encryptionKey),
		logger:        logger,
	}
}

// ValidationError marks caller input that failed validation.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

// ConflictError marks a state conflict, such as an existing account.
type ConflictError struct{ Message string }

func (e *ConflictError) Error() string { return e.Message }

// PermissionError marks an actor that may not perform the operation.
type PermissionError struct{ Message string }

func (e *PermissionError) Error() string { return e.Message }

// NotFoundError marks a missing account or key.
type NotFoundError struct{ Message string }

func (e *NotFoundError) Error() string { return e.Message }

// Account is the console projection of a team account.
type Account struct {
	ID                    int        `json:"id"`
	Name                  string     `json:"name"`
	DepartmentExternalID  string     `json:"department_external_id"`
	DepartmentDisplayPath string     `json:"department_display_path"`
	RelayUsername         string     `json:"relay_username"`
	RelayEmail            string     `json:"relay_email"`
	RelayUserID           int64      `json:"relay_user_id"`
	OwnerUserID           int        `json:"owner_user_id"`
	OwnerUsername         string     `json:"owner_username,omitempty"`
	OwnerEmail            string     `json:"owner_email,omitempty"`
	Status                string     `json:"status"`
	CreatedAt             time.Time  `json:"created_at"`
	Keys                  []Key      `json:"keys,omitempty"`
	Managers              []Manager  `json:"managers,omitempty"`
	AvailableGroups       []GroupRef `json:"available_groups,omitempty"`
}

// GroupRef is one Relay access group with its display name.
type GroupRef struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// Key is one issued API key.
type Key struct {
	ID           int        `json:"id"`
	RelayKeyID   int64      `json:"relay_api_key_id"`
	RelayGroupID int64      `json:"relay_group_id"`
	Name         string     `json:"name"`
	Preview      string     `json:"secret_preview"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty"`
}

// Manager is a person allowed to operate the account.
type Manager struct {
	UserID   int    `json:"user_id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

// CreateInput is the request to provision a team account.
type CreateInput struct {
	Name                 string
	DepartmentExternalID string
	OwnerUserID          int
	ActorUserID          int
	IsAdmin              bool
	// AdditionalGroupIDs are Relay groups the account should be subscribed to
	// on top of the provider defaults. A non-administrator may only choose
	// groups they are subscribed to themselves.
	AdditionalGroupIDs []int64
}

// GroupOption is one access group the console may offer for an account.
type GroupOption struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

// GroupOptions is the access-group picker payload: every known group, the ones
// the provider grants by default, and the ones the caller may grant.
type GroupOptions struct {
	Groups          []GroupOption `json:"groups"`
	DefaultGroupIDs []int64       `json:"default_group_ids"`
	GrantableIDs    []int64       `json:"grantable_group_ids"`
	// OwnerGroupIDs are the groups the chosen account owner is subscribed to;
	// the console preselects them so an account starts with its owner's access.
	OwnerGroupIDs []int64 `json:"owner_group_ids"`
}

type defaultSubscriptionReader interface {
	DefaultSubscriptionGroupIDs(ctx context.Context) ([]int64, error)
}

// GroupOptionsForActor returns the access groups an account creator may choose
// from. A representative may only grant groups they are subscribed to
// themselves; an administrator may grant any group.
func (s *Service) GroupOptionsForActor(ctx context.Context, actorUserID int, isAdmin bool, ownerUserID int) (*GroupOptions, error) {
	providerID, err := s.primaryProviderID(ctx)
	if err != nil {
		return nil, err
	}
	if providerID == 0 {
		return nil, &ValidationError{Message: "no enabled relay provider is configured"}
	}
	provider, err := s.resolver.Resolve(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}

	options := &GroupOptions{Groups: []GroupOption{}, DefaultGroupIDs: []int64{}, GrantableIDs: []int64{}, OwnerGroupIDs: []int64{}}
	if reader, ok := provider.(defaultSubscriptionReader); ok {
		if defaults, err := reader.DefaultSubscriptionGroupIDs(ctx); err == nil {
			options.DefaultGroupIDs = defaults
		}
	}
	if lister, ok := provider.(interface {
		ListPlatformGroups(ctx context.Context) ([]relay.Group, error)
	}); ok {
		groups, err := lister.ListPlatformGroups(ctx)
		if err != nil {
			return nil, fmt.Errorf("list relay groups: %w", err)
		}
		for _, group := range groups {
			options.Groups = append(options.Groups, GroupOption{ID: group.ID, Name: group.Name, Platform: group.Platform})
		}
	}

	if !isAdmin {
		scope, err := s.scopes.Resolve(ctx, actorUserID)
		if err != nil {
			return nil, fmt.Errorf("resolve representative scope: %w", err)
		}
		if scope == nil || !scope.IsRepresentative {
			return nil, &PermissionError{Message: "only a department representative can create a team account"}
		}
	}

	grantable := map[int64]bool{}
	if isAdmin {
		for _, group := range options.Groups {
			grantable[group.ID] = true
		}
	} else {
		actor, err := s.client.User.Get(ctx, actorUserID)
		if err != nil {
			return nil, fmt.Errorf("load creator: %w", err)
		}
		if actor.RelayUserID != nil {
			own, err := s.activeSubscriptionGroupIDSet(ctx, provider, int64(*actor.RelayUserID))
			if err != nil {
				return nil, err
			}
			for id := range own {
				grantable[id] = true
			}
		}
	}
	if isAdmin && ownerUserID > 0 {
		owner, err := s.client.User.Get(ctx, ownerUserID)
		if err != nil {
			return nil, &ValidationError{Message: "owner does not exist"}
		}
		if owner.RelayUserID != nil {
			own, err := s.activeSubscriptionGroupIDSet(ctx, provider, int64(*owner.RelayUserID))
			if err != nil {
				return nil, err
			}
			for id := range own {
				options.OwnerGroupIDs = append(options.OwnerGroupIDs, id)
			}
			sort.Slice(options.OwnerGroupIDs, func(i, j int) bool { return options.OwnerGroupIDs[i] < options.OwnerGroupIDs[j] })
		}
	}

	for id := range grantable {
		options.GrantableIDs = append(options.GrantableIDs, id)
	}
	sort.Slice(options.GrantableIDs, func(i, j int) bool { return options.GrantableIDs[i] < options.GrantableIDs[j] })
	return options, nil
}

// Create provisions a Relay user and records the owning department.
func (s *Service) Create(ctx context.Context, in CreateInput) (*Account, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, &ValidationError{Message: "account name is required"}
	}

	departmentID, err := s.resolveDepartment(ctx, in)
	if err != nil {
		return nil, err
	}

	if _, err := s.client.TeamAccount.Query().Where(teamaccountDepartmentEQ(departmentID)).Only(ctx); err == nil {
		return nil, &ConflictError{Message: "this department already has a team account"}
	} else if !ent.IsNotFound(err) {
		return nil, fmt.Errorf("check existing team account: %w", err)
	}

	providerID, err := s.primaryProviderID(ctx)
	if err != nil {
		return nil, err
	}
	if providerID == 0 {
		return nil, &ValidationError{Message: "no enabled relay provider is configured"}
	}

	username := deriveUsername(departmentID)
	email := username + "@" + EmailDomain
	if err := s.assertIdentityFree(ctx, username, email); err != nil {
		return nil, err
	}

	password, err := generatedPassword()
	if err != nil {
		return nil, err
	}
	encrypted, err := s.encrypt(password)
	if err != nil {
		return nil, err
	}

	provider, err := s.resolver.Resolve(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}

	if err := s.assertAdditionalGroupsAllowed(ctx, provider, in); err != nil {
		return nil, err
	}

	relayUser, err := provider.CreateUser(ctx, relay.CreateUserRequest{
		Username:    username,
		Email:       email,
		Password:    password,
		Role:        "user",
		Notes:       "provisioned_by_ai_efficiency_team",
		Concurrency: DefaultConcurrency,
	})
	if err != nil {
		return nil, fmt.Errorf("create relay user: %w", err)
	}

	if err := s.assignAdditionalGroups(ctx, provider, relayUser.ID, in); err != nil {
		s.disableRelayUser(ctx, provider, relayUser.ID)
		return nil, err
	}

	ownerID := in.OwnerUserID
	if ownerID == 0 {
		ownerID = in.ActorUserID
	}
	if err := s.assertUserExists(ctx, ownerID); err != nil {
		s.disableRelayUser(ctx, provider, relayUser.ID)
		return nil, err
	}
	if err := s.assertPersonUser(ctx, ownerID); err != nil {
		s.disableRelayUser(ctx, provider, relayUser.ID)
		return nil, err
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		s.disableRelayUser(ctx, provider, relayUser.ID)
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	created, err := tx.TeamAccount.Create().
		SetDepartmentExternalID(departmentID).
		SetDepartmentDisplayPath(s.departmentLabel(ctx, departmentID, name)).
		SetName(name).
		SetRelayProviderID(providerID).
		SetRelayUserID(relayUser.ID).
		SetRelayUsername(username).
		SetRelayEmail(email).
		SetRelayAuthPassword(encrypted).
		SetOwnerUserID(ownerID).
		SetStatus("active").
		SetNillableCreatedByUserID(&in.ActorUserID).
		Save(ctx)
	if err != nil {
		s.disableRelayUser(ctx, provider, relayUser.ID)
		return nil, fmt.Errorf("record team account: %w", err)
	}
	if _, err := tx.TeamAccountManager.Create().
		SetTeamAccountID(created.ID).
		SetUserID(ownerID).
		SetRole("owner").
		SetNillableAddedByUserID(&in.ActorUserID).
		Save(ctx); err != nil {
		s.disableRelayUser(ctx, provider, relayUser.ID)
		return nil, fmt.Errorf("record owner: %w", err)
	}
	if err := recordEvent(ctx, tx, created.ID, in.ActorUserID, "account_created", map[string]any{
		"department_external_id": departmentID,
		"relay_user_id":          relayUser.ID,
		"additional_group_ids":   in.AdditionalGroupIDs,
		"owner_user_id":          ownerID,
	}); err != nil {
		s.disableRelayUser(ctx, provider, relayUser.ID)
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		s.disableRelayUser(ctx, provider, relayUser.ID)
		return nil, fmt.Errorf("commit team account: %w", err)
	}

	return s.Get(ctx, in.ActorUserID, in.IsAdmin, created.ID)
}

// departmentLabel resolves the human-readable directory path (names, not ids)
// for a department, falling back to the account name when the department is not
// in the current snapshot.
func (s *Service) departmentLabel(ctx context.Context, departmentID, fallback string) string {
	paths := s.departmentDisplayPaths(ctx, []string{departmentID})
	if label := paths[departmentID]; label != "" {
		return label
	}
	return fallback
}

// departmentDisplayPaths maps department external ids to their display paths
// built from the current directory hierarchy.
func (s *Service) departmentDisplayPaths(ctx context.Context, departmentIDs []string) map[string]string {
	out := map[string]string{}
	view, ok, err := directoryfacts.New(s.client).Current(ctx)
	if err != nil || !ok {
		return out
	}
	facts, err := view.Load(ctx, directoryfacts.Query{
		DepartmentIDs:              departmentIDs,
		IncludeDepartmentAncestors: true,
	})
	if err != nil {
		return out
	}
	hierarchy := facts.Hierarchy()
	for _, departmentID := range departmentIDs {
		if label := hierarchy.DisplayPath(departmentID); label != "" {
			out[departmentID] = label
		}
	}
	return out
}

func (s *Service) resolveDepartment(ctx context.Context, in CreateInput) (string, error) {
	departmentID := strings.TrimSpace(in.DepartmentExternalID)
	if in.IsAdmin {
		if departmentID == "" {
			return "", &ValidationError{Message: "department is required"}
		}
		return departmentID, nil
	}
	scope, err := s.scopes.Resolve(ctx, in.ActorUserID)
	if err != nil {
		return "", fmt.Errorf("resolve representative scope: %w", err)
	}
	if scope == nil || !scope.IsRepresentative {
		return "", &PermissionError{Message: "only a department representative can create a team account"}
	}
	if departmentID == "" {
		if len(scope.RepresentedDepartmentIDs) != 1 {
			return "", &ValidationError{Message: "department is required when representing more than one department"}
		}
		return scope.RepresentedDepartmentIDs[0], nil
	}
	for _, candidate := range scope.RepresentedDepartmentIDs {
		if candidate == departmentID {
			return departmentID, nil
		}
	}
	return "", &PermissionError{Message: "you do not represent this department"}
}

func (s *Service) assertIdentityFree(ctx context.Context, username, email string) error {
	exists, err := s.client.User.Query().Where(entuser.UsernameEQ(username)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check username: %w", err)
	}
	if exists {
		return &ConflictError{Message: "username already exists"}
	}
	exists, err = s.client.User.Query().Where(entuser.EmailEQ(email)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check email: %w", err)
	}
	if exists {
		return &ConflictError{Message: "email already exists"}
	}
	return nil
}

func (s *Service) assertUserExists(ctx context.Context, userID int) error {
	if userID <= 0 {
		return &ValidationError{Message: "owner is required"}
	}
	exists, err := s.client.User.Query().Where(entuser.IDEQ(userID)).Exist(ctx)
	if err != nil {
		return fmt.Errorf("check owner: %w", err)
	}
	if !exists {
		return &ValidationError{Message: "owner does not exist"}
	}
	return nil
}

func (s *Service) primaryProviderID(ctx context.Context) (int, error) {
	rows, err := s.client.RelayProvider.Query().Where(relayProviderEnabled()).Order(ent.Asc("id")).All(ctx)
	if err != nil {
		return 0, fmt.Errorf("list relay providers: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	for _, row := range rows {
		if row.IsPrimary {
			return row.ID, nil
		}
	}
	return rows[0].ID, nil
}

// activeSubscriptionGroupIDSet returns the groups the Relay user currently holds
// an active subscription for; a Relay key can only be bound to those.
func (s *Service) activeSubscriptionGroupIDSet(ctx context.Context, provider relay.Provider, relayUserID int64) (map[int64]bool, error) {
	lister, ok := provider.(interface {
		ListUserSubscriptions(ctx context.Context, relayUserID int64) ([]relay.UserSubscription, error)
	})
	if !ok {
		return nil, fmt.Errorf("relay provider cannot list subscriptions")
	}
	subs, err := lister.ListUserSubscriptions(ctx, relayUserID)
	if err != nil {
		return nil, fmt.Errorf("list subscriptions: %w", err)
	}
	out := make(map[int64]bool, len(subs))
	for _, sub := range subs {
		if strings.EqualFold(strings.TrimSpace(sub.Status), "active") {
			out[sub.GroupID] = true
		}
	}
	return out, nil
}

// assertAdditionalGroupsAllowed rejects grants that exceed the creator's own
// subscriptions before any Relay identity is created.
func (s *Service) assertAdditionalGroupsAllowed(ctx context.Context, provider relay.Provider, in CreateInput) error {
	if len(in.AdditionalGroupIDs) == 0 || in.IsAdmin {
		return nil
	}
	actor, err := s.client.User.Get(ctx, in.ActorUserID)
	if err != nil {
		return fmt.Errorf("load creator: %w", err)
	}
	if actor.RelayUserID == nil {
		return &PermissionError{Message: "your account has no Relay identity yet, so no group can be granted"}
	}
	own, err := s.activeSubscriptionGroupIDSet(ctx, provider, int64(*actor.RelayUserID))
	if err != nil {
		return err
	}
	for _, groupID := range in.AdditionalGroupIDs {
		if groupID > 0 && !own[groupID] {
			return &PermissionError{Message: fmt.Sprintf("you are not subscribed to group %d, so you cannot grant it", groupID)}
		}
	}
	return nil
}

// assignAdditionalGroups subscribes the new account to the extra groups the
// caller asked for. Defaults assigned by the Relay stay untouched, so a team
// account starts with the same groups as any personal account.
func (s *Service) assignAdditionalGroups(ctx context.Context, provider relay.Provider, relayUserID int64, in CreateInput) error {
	if len(in.AdditionalGroupIDs) == 0 {
		return nil
	}
	assigner, ok := provider.(interface {
		AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error
	})
	if !ok {
		return fmt.Errorf("relay provider cannot assign subscriptions")
	}

	existing, err := s.activeSubscriptionGroupIDSet(ctx, provider, relayUserID)
	if err != nil {
		return err
	}
	seen := map[int64]bool{}
	for _, groupID := range in.AdditionalGroupIDs {
		if groupID <= 0 || seen[groupID] {
			continue
		}
		seen[groupID] = true
		if existing[groupID] {
			continue
		}
		if err := assigner.AssignSubscriptionForUser(ctx, relayUserID, groupID, SubscriptionValidityDays); err != nil {
			return fmt.Errorf("assign group %d: %w", groupID, err)
		}
	}
	return nil
}

func (s *Service) disableRelayUser(ctx context.Context, provider relay.Provider, relayUserID int64) {
	disabler, ok := provider.(interface {
		DisableUser(ctx context.Context, userID int64) error
	})
	if !ok {
		s.logger.Warn("team account compensation skipped: provider cannot disable users", zap.Int64("relay_user_id", relayUserID))
		return
	}
	if err := disabler.DisableUser(ctx, relayUserID); err != nil {
		s.logger.Error("team account compensation failed", zap.Int64("relay_user_id", relayUserID), zap.Error(err))
		return
	}
	s.logger.Warn("team account relay user disabled after local failure", zap.Int64("relay_user_id", relayUserID))
}

// UserOption is one person that can be added as an account manager.
type UserOption struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

// SearchUserOptions finds people an account member may add as a manager. The
// caller must already manage the account (administrators skip that check).
func (s *Service) SearchUserOptions(ctx context.Context, actorUserID int, includeAll bool, accountID int, query string) ([]UserOption, error) {
	if _, err := s.loadAccount(ctx, accountID); err != nil {
		return nil, err
	}
	if !includeAll {
		if err := s.assertMember(ctx, actorUserID, accountID); err != nil {
			return nil, err
		}
	}
	keyword := strings.TrimSpace(query)
	selectors := []predicate.User{
		entuser.UsernameContainsFold(keyword),
		entuser.EmailContainsFold(keyword),
	}
	users, err := s.client.User.Query().
		Where(entuser.Or(selectors...)).
		Order(ent.Asc(entuser.FieldID)).
		Limit(200).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}

	var allowed map[string]bool
	if !includeAll {
		account, err := s.loadAccount(ctx, accountID)
		if err != nil {
			return nil, err
		}
		allowed, err = s.departmentMemberEmails(ctx, account.DepartmentExternalID)
		if err != nil {
			return nil, err
		}
	}

	identities, err := s.organizationIdentities(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UserOption, 0, 20)
	for _, user := range users {
		if identities[strings.ToLower(strings.TrimSpace(user.Username))] ||
			identities[strings.ToLower(strings.TrimSpace(user.Email))] {
			continue
		}
		if allowed != nil && !allowed[strings.ToLower(strings.TrimSpace(user.Email))] {
			continue
		}
		out = append(out, UserOption{ID: user.ID, Username: user.Username, Email: user.Email})
		if len(out) == 20 {
			break
		}
	}
	return out, nil
}

// departmentMemberEmails collects the normalized emails of everyone in the
// department, using both the primary department and additional memberships.
func (s *Service) departmentMemberEmails(ctx context.Context, departmentID string) (map[string]bool, error) {
	out := map[string]bool{}
	if strings.TrimSpace(departmentID) == "" {
		return out, nil
	}
	members, err := s.client.DirectoryMember.Query().
		Where(directorymember.DepartmentExternalIDEQ(departmentID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load department members: %w", err)
	}
	for _, member := range members {
		out[strings.ToLower(strings.TrimSpace(member.EmailNormalized))] = true
	}
	memberships, err := s.client.DirectoryMemberDepartment.Query().
		Where(directorymemberdepartment.DepartmentExternalIDEQ(departmentID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load department memberships: %w", err)
	}
	for _, membership := range memberships {
		out[strings.ToLower(strings.TrimSpace(membership.MemberEmailNormalized))] = true
	}
	delete(out, "")
	return out, nil
}

// organizationIdentity is the set of relay names and mailboxes owned by team
// accounts. They are sign-in surfaces, never people, so they must never be
// granted management rights.
func (s *Service) organizationIdentities(ctx context.Context) (map[string]bool, error) {
	rows, err := s.client.TeamAccount.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load organization accounts: %w", err)
	}
	out := map[string]bool{}
	for _, row := range rows {
		out[strings.ToLower(strings.TrimSpace(row.RelayUsername))] = true
		out[strings.ToLower(strings.TrimSpace(row.RelayEmail))] = true
	}
	delete(out, "")
	return out, nil
}

// assertPersonUser rejects organization-account identities as managers or
// owners: an account can never be handed to another account.
func (s *Service) assertPersonUser(ctx context.Context, userID int) error {
	user, err := s.client.User.Get(ctx, userID)
	if err != nil {
		if ent.IsNotFound(err) {
			return &ValidationError{Message: "user does not exist"}
		}
		return fmt.Errorf("load user: %w", err)
	}
	identities, err := s.organizationIdentities(ctx)
	if err != nil {
		return err
	}
	if identities[strings.ToLower(strings.TrimSpace(user.Username))] ||
		identities[strings.ToLower(strings.TrimSpace(user.Email))] {
		return &ValidationError{Message: "organization accounts cannot be managers or owners"}
	}
	return nil
}

// assertPersonInDepartment rejects targets outside the account's department for
// non-administrators, so a representative cannot hand the account to someone
// they do not represent.
func (s *Service) assertPersonInDepartment(ctx context.Context, departmentID string, userID int) error {
	user, err := s.client.User.Get(ctx, userID)
	if err != nil {
		if ent.IsNotFound(err) {
			return &ValidationError{Message: "user does not exist"}
		}
		return fmt.Errorf("load user: %w", err)
	}
	allowed, err := s.departmentMemberEmails(ctx, departmentID)
	if err != nil {
		return err
	}
	if !allowed[strings.ToLower(strings.TrimSpace(user.Email))] {
		return &PermissionError{Message: "user is not a member of this department"}
	}
	return nil
}

// List returns the accounts the actor owns or manages.
func (s *Service) List(ctx context.Context, actorUserID int, includeAll bool) ([]Account, error) {
	var rows []*ent.TeamAccount
	var err error
	if includeAll {
		rows, err = s.client.TeamAccount.Query().Order(ent.Asc("id")).All(ctx)
	} else {
		managerRows, mErr := s.client.TeamAccountManager.Query().
			Where(teamAccountManagerUserID(actorUserID)).
			Order(ent.Asc("team_account_id")).
			All(ctx)
		if mErr != nil {
			return nil, fmt.Errorf("list account memberships: %w", mErr)
		}
		ids := make([]int, 0, len(managerRows))
		for _, row := range managerRows {
			ids = append(ids, row.TeamAccountID)
		}
		if len(ids) == 0 {
			return []Account{}, nil
		}
		rows, err = s.client.TeamAccount.Query().Where(teamAccountIDIn(ids)).Order(ent.Asc("id")).All(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("list team accounts: %w", err)
	}
	accounts := make([]Account, 0, len(rows))
	for _, row := range rows {
		accounts = append(accounts, toAccount(row))
	}
	s.hydrateOwners(ctx, accounts)
	s.hydrateDepartments(ctx, accounts)
	return accounts, nil
}

// hydrateDepartments replaces the stored label with the current directory path
// so the console shows where the account sits in the org tree.
func (s *Service) hydrateDepartments(ctx context.Context, accounts []Account) {
	ids := make([]string, 0, len(accounts))
	seen := map[string]bool{}
	for _, account := range accounts {
		if account.DepartmentExternalID == "" || seen[account.DepartmentExternalID] {
			continue
		}
		seen[account.DepartmentExternalID] = true
		ids = append(ids, account.DepartmentExternalID)
	}
	if len(ids) == 0 {
		return
	}
	byExternalID := s.departmentDisplayPaths(ctx, ids)
	if len(byExternalID) == 0 {
		return
	}
	for index := range accounts {
		if label := byExternalID[accounts[index].DepartmentExternalID]; label != "" {
			accounts[index].DepartmentDisplayPath = label
		}
	}
}

// hydrateOwners attaches human-readable owner identities so the console never
// has to show a raw user id.
func (s *Service) hydrateOwners(ctx context.Context, accounts []Account) {
	ids := make([]int, 0, len(accounts))
	seen := map[int]bool{}
	for _, account := range accounts {
		if account.OwnerUserID <= 0 || seen[account.OwnerUserID] {
			continue
		}
		seen[account.OwnerUserID] = true
		ids = append(ids, account.OwnerUserID)
	}
	if len(ids) == 0 {
		return
	}
	users, err := s.client.User.Query().Where(entuser.IDIn(ids...)).All(ctx)
	if err != nil {
		return
	}
	byID := make(map[int]*ent.User, len(users))
	for _, user := range users {
		byID[user.ID] = user
	}
	for index := range accounts {
		user := byID[accounts[index].OwnerUserID]
		if user == nil {
			continue
		}
		accounts[index].OwnerUsername = user.Username
		accounts[index].OwnerEmail = user.Email
	}
}

// Get returns one account with keys and managers after an access check.
func (s *Service) Get(ctx context.Context, actorUserID int, includeAll bool, accountID int) (*Account, error) {
	row, err := s.loadAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !includeAll {
		if err := s.assertMember(ctx, actorUserID, accountID); err != nil {
			return nil, err
		}
	}
	account := toAccount(row)
	// Never expose the raw directory-id path that older rows stored.
	account.DepartmentDisplayPath = s.departmentLabel(ctx, row.DepartmentExternalID, account.DepartmentDisplayPath)

	keys, err := s.client.TeamAccountKey.Query().
		Where(teamAccountKeyAccountID(accountID)).
		Order(ent.Desc("id")).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	account.Keys = make([]Key, 0, len(keys))
	for _, key := range keys {
		account.Keys = append(account.Keys, toKey(key))
	}

	managers, err := s.listManagers(ctx, accountID)
	if err != nil {
		return nil, err
	}
	account.Managers = managers

	provider, err := s.resolver.Resolve(ctx, row.RelayProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	groupIDs, err := s.activeSubscriptionGroupIDSet(ctx, provider, row.RelayUserID)
	if err != nil {
		return nil, err
	}
	account.AvailableGroups = s.groupRefs(ctx, provider, groupIDs)
	if owner, err := s.client.User.Get(ctx, row.OwnerUserID); err == nil {
		account.OwnerUsername = owner.Username
		account.OwnerEmail = owner.Email
	}
	return &account, nil
}

func (s *Service) listManagers(ctx context.Context, accountID int) ([]Manager, error) {
	rows, err := s.client.TeamAccountManager.Query().
		Where(teamAccountManagerAccountID(accountID)).
		Order(ent.Asc("id")).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list managers: %w", err)
	}
	out := make([]Manager, 0, len(rows))
	for _, row := range rows {
		u, err := s.client.User.Get(ctx, row.UserID)
		if err != nil {
			continue
		}
		out = append(out, Manager{UserID: row.UserID, Username: u.Username, Email: u.Email, Role: string(row.Role)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UserID < out[j].UserID })
	return out, nil
}

func (s *Service) loadAccount(ctx context.Context, accountID int) (*ent.TeamAccount, error) {
	row, err := s.client.TeamAccount.Get(ctx, accountID)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, &NotFoundError{Message: "team account not found"}
		}
		return nil, fmt.Errorf("load team account: %w", err)
	}
	return row, nil
}

func (s *Service) assertMember(ctx context.Context, actorUserID, accountID int) error {
	ok, err := s.client.TeamAccountManager.Query().
		Where(teamAccountManagerAccountID(accountID), teamAccountManagerUserID(actorUserID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check membership: %w", err)
	}
	if !ok {
		return &PermissionError{Message: "you do not manage this team account"}
	}
	return nil
}

func (s *Service) assertOwner(ctx context.Context, actorUserID int, includeAll bool, accountID int) error {
	if includeAll {
		return nil
	}
	row, err := s.client.TeamAccountManager.Query().
		Where(teamAccountManagerAccountID(accountID), teamAccountManagerUserID(actorUserID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return &PermissionError{Message: "you do not manage this team account"}
		}
		return fmt.Errorf("check ownership: %w", err)
	}
	if row.Role != "owner" {
		return &PermissionError{Message: "only the account owner can do this"}
	}
	return nil
}

// CreateKey mints one API key for a mapped group and returns the secret once.
func (s *Service) CreateKey(ctx context.Context, actorUserID int, includeAll bool, accountID int, groupID int64, name string) (*Key, string, error) {
	row, err := s.loadAccount(ctx, accountID)
	if err != nil {
		return nil, "", err
	}
	if !includeAll {
		if err := s.assertMember(ctx, actorUserID, accountID); err != nil {
			return nil, "", err
		}
	}
	if row.Status != "active" {
		return nil, "", &ConflictError{Message: "team account is not active"}
	}
	provider, err := s.resolver.Resolve(ctx, row.RelayProviderID)
	if err != nil {
		return nil, "", fmt.Errorf("resolve relay provider: %w", err)
	}
	groupID, err = s.ensureSubscribedGroup(ctx, provider, row.RelayUserID, groupID)
	if err != nil {
		return nil, "", err
	}
	password, err := s.decryptPassword(row)
	if err != nil {
		return nil, "", err
	}
	keyName := strings.TrimSpace(name)
	if keyName == "" {
		keyName = fmt.Sprintf("%s-%d", row.RelayUsername, groupID)
	}

	login := strings.TrimSpace(row.RelayEmail)
	if login == "" {
		login = row.RelayUsername + "@" + EmailDomain
	}
	authCtx := relay.WithUserCredentials(ctx, login, password)
	created, err := provider.CreateUserAPIKey(authCtx, row.RelayUserID, relay.APIKeyCreateRequest{
		Name:    keyName,
		GroupID: strconv.FormatInt(groupID, 10),
	})
	if err != nil {
		return nil, "", fmt.Errorf("create relay api key: %w", err)
	}

	keyRow, err := s.client.TeamAccountKey.Create().
		SetTeamAccountID(row.ID).
		SetRelayAPIKeyID(created.ID).
		SetRelayGroupID(groupID).
		SetName(keyName).
		SetSecretPreview(previewSecret(created.Secret)).
		SetStatus("active").
		SetNillableCreatedByUserID(&actorUserID).
		Save(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("record api key: %w", err)
	}
	_ = s.recordEvent(ctx, row.ID, actorUserID, "key_created", map[string]any{
		"key_id":   created.ID,
		"group_id": groupID,
		"name":     keyName,
	})

	key := toKey(keyRow)
	return &key, created.Secret, nil
}

// ensureSubscribedGroup resolves the group a key should bind to and rejects
// groups the account is not subscribed to.
func (s *Service) ensureSubscribedGroup(ctx context.Context, provider relay.Provider, relayUserID int64, groupID int64) (int64, error) {
	subs, err := s.activeSubscriptionGroupIDSet(ctx, provider, relayUserID)
	if err != nil {
		return 0, err
	}
	if len(subs) == 0 {
		return 0, &ValidationError{Message: "this account has no active Relay subscription yet"}
	}
	if groupID == 0 {
		for id := range subs {
			return id, nil
		}
	}
	if !subs[groupID] {
		return 0, &ValidationError{Message: "this account is not subscribed to that group"}
	}
	return groupID, nil
}

// groupRefs resolves display names for the subscribed groups.
func (s *Service) groupRefs(ctx context.Context, provider relay.Provider, ids map[int64]bool) []GroupRef {
	out := make([]GroupRef, 0, len(ids))
	names := map[int64]relay.Group{}
	if lister, ok := provider.(interface {
		ListPlatformGroups(ctx context.Context) ([]relay.Group, error)
	}); ok {
		if groups, err := lister.ListPlatformGroups(ctx); err == nil {
			for _, group := range groups {
				names[group.ID] = group
			}
		}
	}
	for id := range ids {
		ref := GroupRef{ID: id, Name: fmt.Sprintf("#%d", id)}
		if group, ok := names[id]; ok {
			ref.Name = group.Name
			ref.Platform = group.Platform
		}
		out = append(out, ref)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RevokeKey disables one key at the Relay and records the change.
func (s *Service) RevokeKey(ctx context.Context, actorUserID int, includeAll bool, accountID, keyID int) error {
	row, err := s.loadAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if !includeAll {
		if err := s.assertMember(ctx, actorUserID, accountID); err != nil {
			return err
		}
	}
	keyRow, err := s.client.TeamAccountKey.Query().
		Where(teamAccountKeyAccountID(accountID), teamAccountKeyIDEQ(keyID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return &NotFoundError{Message: "key not found"}
		}
		return fmt.Errorf("load key: %w", err)
	}
	if keyRow.Status == "revoked" {
		return nil
	}
	provider, err := s.resolver.Resolve(ctx, row.RelayProviderID)
	if err != nil {
		return fmt.Errorf("resolve relay provider: %w", err)
	}
	if err := s.disableRelayKey(ctx, row, provider, keyRow.RelayAPIKeyID); err != nil {
		return err
	}
	now := time.Now()
	if _, err := s.client.TeamAccountKey.UpdateOneID(keyRow.ID).
		SetStatus("revoked").
		SetRevokedAt(now).
		Save(ctx); err != nil {
		return fmt.Errorf("mark key revoked: %w", err)
	}
	return s.recordEvent(ctx, accountID, actorUserID, "key_revoked", map[string]any{"key_id": keyRow.RelayAPIKeyID})
}

// Credentials is the account's Relay login identity with the decrypted
// password. The mailbox is included because Relay authenticates by email.
type Credentials struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Credentials returns the account name, mailbox, and decrypted Relay password.
func (s *Service) Credentials(ctx context.Context, actorUserID int, includeAll bool, accountID int) (*Credentials, error) {
	row, err := s.loadAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !includeAll {
		if err := s.assertMember(ctx, actorUserID, accountID); err != nil {
			return nil, err
		}
	}
	password, err := s.decryptPassword(row)
	if err != nil {
		return nil, err
	}
	email := strings.TrimSpace(row.RelayEmail)
	if email == "" {
		email = row.RelayUsername + "@" + EmailDomain
	}
	_ = s.recordEvent(ctx, accountID, actorUserID, "credentials_revealed", map[string]any{"user_id": actorUserID})
	return &Credentials{Username: row.RelayUsername, Email: email, Password: password}, nil
}

// AddManager grants one user management access.
func (s *Service) AddManager(ctx context.Context, actorUserID int, includeAll bool, accountID, userID int) error {
	if _, err := s.loadAccount(ctx, accountID); err != nil {
		return err
	}
	if err := s.assertOwner(ctx, actorUserID, includeAll, accountID); err != nil {
		return err
	}
	if err := s.assertUserExists(ctx, userID); err != nil {
		return err
	}
	if err := s.assertPersonUser(ctx, userID); err != nil {
		return err
	}
	if !includeAll {
		account, err := s.loadAccount(ctx, accountID)
		if err != nil {
			return err
		}
		if err := s.assertPersonInDepartment(ctx, account.DepartmentExternalID, userID); err != nil {
			return err
		}
	}
	exists, err := s.client.TeamAccountManager.Query().
		Where(teamAccountManagerAccountID(accountID), teamAccountManagerUserID(userID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check manager: %w", err)
	}
	if exists {
		return nil
	}
	if _, err := s.client.TeamAccountManager.Create().
		SetTeamAccountID(accountID).
		SetUserID(userID).
		SetRole("manager").
		SetNillableAddedByUserID(&actorUserID).
		Save(ctx); err != nil {
		return fmt.Errorf("add manager: %w", err)
	}
	return s.recordEvent(ctx, accountID, actorUserID, "manager_added", map[string]any{"user_id": userID})
}

// RemoveManager revokes one user's management access. The owner cannot be
// removed; transfer ownership first.
func (s *Service) RemoveManager(ctx context.Context, actorUserID int, includeAll bool, accountID, userID int) error {
	if _, err := s.loadAccount(ctx, accountID); err != nil {
		return err
	}
	if err := s.assertOwner(ctx, actorUserID, includeAll, accountID); err != nil {
		return err
	}
	row, err := s.client.TeamAccountManager.Query().
		Where(teamAccountManagerAccountID(accountID), teamAccountManagerUserID(userID)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("load manager: %w", err)
	}
	if row.Role == "owner" {
		return &ValidationError{Message: "transfer ownership before removing the owner"}
	}
	if err := s.client.TeamAccountManager.DeleteOneID(row.ID).Exec(ctx); err != nil {
		return fmt.Errorf("remove manager: %w", err)
	}
	return s.recordEvent(ctx, accountID, actorUserID, "manager_removed", map[string]any{"user_id": userID})
}

// Transfer replaces the owner and drops the previous owner's access in one
// transaction.
func (s *Service) Transfer(ctx context.Context, actorUserID int, includeAll bool, accountID, newOwnerUserID int) error {
	row, err := s.loadAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if err := s.assertOwner(ctx, actorUserID, includeAll, accountID); err != nil {
		return err
	}
	if err := s.assertUserExists(ctx, newOwnerUserID); err != nil {
		return err
	}
	if err := s.assertPersonUser(ctx, newOwnerUserID); err != nil {
		return err
	}
	if !includeAll {
		if err := s.assertPersonInDepartment(ctx, row.DepartmentExternalID, newOwnerUserID); err != nil {
			return err
		}
	}
	previousOwner := row.OwnerUserID

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.TeamAccount.UpdateOneID(accountID).SetOwnerUserID(newOwnerUserID).Save(ctx); err != nil {
		return fmt.Errorf("update owner: %w", err)
	}
	if _, err := tx.TeamAccountManager.Delete().
		Where(teamAccountManagerAccountID(accountID), teamAccountManagerUserID(previousOwner)).
		Exec(ctx); err != nil {
		return fmt.Errorf("remove previous owner: %w", err)
	}
	exists, err := tx.TeamAccountManager.Query().
		Where(teamAccountManagerAccountID(accountID), teamAccountManagerUserID(newOwnerUserID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check new owner: %w", err)
	}
	if exists {
		if _, err := tx.TeamAccountManager.Update().
			Where(teamAccountManagerAccountID(accountID), teamAccountManagerUserID(newOwnerUserID)).
			SetRole("owner").
			Save(ctx); err != nil {
			return fmt.Errorf("promote new owner: %w", err)
		}
	} else if _, err := tx.TeamAccountManager.Create().
		SetTeamAccountID(accountID).
		SetUserID(newOwnerUserID).
		SetRole("owner").
		SetNillableAddedByUserID(&actorUserID).
		Save(ctx); err != nil {
		return fmt.Errorf("record new owner: %w", err)
	}
	if err := recordEvent(ctx, tx, accountID, actorUserID, "owner_transferred", map[string]any{
		"previous_owner_user_id": previousOwner,
		"new_owner_user_id":      newOwnerUserID,
	}); err != nil {
		return err
	}
	return tx.Commit()
}

// SetStatus suspends, restores, or orphans an account.
func (s *Service) SetStatus(ctx context.Context, actorUserID int, accountID int, status string) error {
	switch status {
	case "active", "suspended", "orphaned":
	default:
		return &ValidationError{Message: "unsupported status"}
	}
	if _, err := s.loadAccount(ctx, accountID); err != nil {
		return err
	}
	row, err := s.loadAccount(ctx, accountID)
	if err != nil {
		return err
	}
	if _, err := s.client.TeamAccount.UpdateOneID(accountID).SetStatus(entteamaccount.Status(status)).Save(ctx); err != nil {
		return fmt.Errorf("update status: %w", err)
	}
	if status == "suspended" || status == "orphaned" {
		if err := s.revokeActiveKeys(ctx, actorUserID, row); err != nil {
			return err
		}
	}
	action := "account_suspended"
	switch status {
	case "active":
		action = "account_restored"
	case "orphaned":
		action = "account_orphaned"
	}
	return s.recordEvent(ctx, accountID, actorUserID, action, map[string]any{"status": status})
}

// revokeActiveKeys disables every active key so a suspended account can no
// longer call Relay even though its identity still exists.
func (s *Service) revokeActiveKeys(ctx context.Context, actorUserID int, row *ent.TeamAccount) error {
	keys, err := s.client.TeamAccountKey.Query().
		Where(teamAccountKeyAccountID(row.ID), teamAccountKeyStatus("active")).
		All(ctx)
	if err != nil {
		return fmt.Errorf("list active keys: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	provider, err := s.resolver.Resolve(ctx, row.RelayProviderID)
	if err != nil {
		return fmt.Errorf("resolve relay provider: %w", err)
	}
	now := time.Now()
	for _, key := range keys {
		if err := s.disableRelayKey(ctx, row, provider, key.RelayAPIKeyID); err != nil {
			return err
		}
		if _, err := s.client.TeamAccountKey.UpdateOneID(key.ID).
			SetStatus("revoked").
			SetRevokedAt(now).
			Save(ctx); err != nil {
			return fmt.Errorf("mark key revoked: %w", err)
		}
		_ = s.recordEvent(ctx, row.ID, actorUserID, "key_revoked", map[string]any{"key_id": key.RelayAPIKeyID, "reason": "account_suspended"})
	}
	return nil
}

// disableRelayKey marks one Relay key inactive using the account's own
// credentials: the Relay build exposes no administrator revoke endpoint.
func (s *Service) disableRelayKey(ctx context.Context, row *ent.TeamAccount, provider relay.Provider, relayKeyID int64) error {
	password, err := s.decryptPassword(row)
	if err != nil {
		return err
	}
	login := strings.TrimSpace(row.RelayEmail)
	if login == "" {
		login = row.RelayUsername + "@" + EmailDomain
	}
	authCtx := relay.WithUserCredentials(ctx, login, password)
	if err := provider.UpdateUserAPIKeyStatus(authCtx, relayKeyID, "inactive"); err != nil {
		return fmt.Errorf("disable relay api key: %w", err)
	}
	return nil
}

// UsagePoint is one day of usage.
type UsagePoint struct {
	Date   string  `json:"date"`
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"cost"`
}

// UsageKey is one key's usage in the window.
type UsageKey struct {
	ID     int     `json:"id"`
	Name   string  `json:"name"`
	Group  int64   `json:"relay_group_id"`
	Tokens int64   `json:"tokens"`
	Cost   float64 `json:"cost"`
}

// Usage is the aggregated usage response.
type Usage struct {
	TotalTokens int64        `json:"total_tokens"`
	TotalCost   float64      `json:"total_cost"`
	Days        []UsagePoint `json:"days"`
	Keys        []UsageKey   `json:"keys"`
}

// AggregateUsage reports Relay usage for the account, split by day and key.
func (s *Service) AggregateUsage(ctx context.Context, actorUserID int, includeAll bool, accountID int, from, to time.Time) (*Usage, error) {
	row, err := s.loadAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if !includeAll {
		if err := s.assertMember(ctx, actorUserID, accountID); err != nil {
			return nil, err
		}
	}
	provider, err := s.resolver.Resolve(ctx, row.RelayProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}

	usage := &Usage{Days: []UsagePoint{}, Keys: []UsageKey{}}

	keys, err := s.client.TeamAccountKey.Query().
		Where(teamAccountKeyAccountID(accountID)).
		Order(ent.Asc("id")).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list keys: %w", err)
	}
	byDay := map[string]*UsagePoint{}
	for _, key := range keys {
		logs, err := provider.ListUsageLogsByAPIKeyExact(ctx, key.RelayAPIKeyID, from, to)
		if err != nil {
			return nil, fmt.Errorf("load usage logs for key %d: %w", key.ID, err)
		}
		item := UsageKey{ID: key.ID, Name: key.Name, Group: key.RelayGroupID}
		for _, log := range logs {
			usage.TotalTokens += log.TotalTokens
			usage.TotalCost += log.TotalCost
			item.Tokens += log.TotalTokens
			item.Cost += log.TotalCost
			date := log.CreatedAt.Format("2006-01-02")
			point, ok := byDay[date]
			if !ok {
				point = &UsagePoint{Date: date}
				byDay[date] = point
			}
			point.Tokens += log.TotalTokens
			point.Cost += log.TotalCost
		}
		usage.Keys = append(usage.Keys, item)
	}
	for _, point := range byDay {
		usage.Days = append(usage.Days, *point)
	}
	sort.Slice(usage.Days, func(i, j int) bool { return usage.Days[i].Date < usage.Days[j].Date })
	return usage, nil
}

// Events returns the audit trail, newest first.
func (s *Service) Events(ctx context.Context, actorUserID int, includeAll bool, accountID int, limit int) ([]map[string]any, error) {
	if _, err := s.loadAccount(ctx, accountID); err != nil {
		return nil, err
	}
	if !includeAll {
		if err := s.assertMember(ctx, actorUserID, accountID); err != nil {
			return nil, err
		}
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	rows, err := s.client.TeamAccountEvent.Query().
		Where(teamAccountEventAccountID(accountID)).
		Order(ent.Desc("id")).Limit(limit).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	actorIDs := make([]int, 0, len(rows))
	seen := map[int]bool{}
	for _, row := range rows {
		if row.ActorUserID == nil || seen[*row.ActorUserID] {
			continue
		}
		seen[*row.ActorUserID] = true
		actorIDs = append(actorIDs, *row.ActorUserID)
	}
	actors := map[int]*ent.User{}
	if len(actorIDs) > 0 {
		users, err := s.client.User.Query().Where(entuser.IDIn(actorIDs...)).All(ctx)
		if err == nil {
			for _, user := range users {
				actors[user.ID] = user
			}
		}
	}

	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entry := map[string]any{
			"id":            row.ID,
			"action":        row.Action,
			"actor_user_id": row.ActorUserID,
			"payload":       row.Payload,
			"created_at":    row.CreatedAt,
		}
		if row.ActorUserID != nil {
			if user := actors[*row.ActorUserID]; user != nil {
				entry["actor_username"] = user.Username
				entry["actor_email"] = user.Email
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *Service) recordEvent(ctx context.Context, accountID, actorUserID int, action string, payload map[string]any) error {
	_, err := s.client.TeamAccountEvent.Create().
		SetTeamAccountID(accountID).
		SetNillableActorUserID(&actorUserID).
		SetAction(action).
		SetPayload(payload).
		Save(ctx)
	if err != nil {
		s.logger.Warn("record team account event failed", zap.String("action", action), zap.Error(err))
	}
	return nil
}

func recordEvent(ctx context.Context, tx *ent.Tx, accountID, actorUserID int, action string, payload map[string]any) error {
	_, err := tx.TeamAccountEvent.Create().
		SetTeamAccountID(accountID).
		SetNillableActorUserID(&actorUserID).
		SetAction(action).
		SetPayload(payload).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("record event %s: %w", action, err)
	}
	return nil
}

func (s *Service) encrypt(password string) (string, error) {
	if s.encryptionKey == "" {
		return "", &ValidationError{Message: "encryption key is not configured"}
	}
	encrypted, err := pkg.Encrypt(password, s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("encrypt relay password: %w", err)
	}
	return encrypted, nil
}

func (s *Service) decryptPassword(row *ent.TeamAccount) (string, error) {
	if row.RelayAuthPassword == nil || strings.TrimSpace(*row.RelayAuthPassword) == "" {
		return "", &ValidationError{Message: "relay password is not stored for this account"}
	}
	if s.encryptionKey == "" {
		return "", &ValidationError{Message: "encryption key is not configured"}
	}
	password, err := pkg.Decrypt(strings.TrimSpace(*row.RelayAuthPassword), s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("decrypt relay password: %w", err)
	}
	return password, nil
}

func generatedPassword() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func deriveUsername(departmentExternalID string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(departmentExternalID) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "dept"
	}
	if len(slug) > 32 {
		slug = slug[:32]
	}
	return teamUsernamePrefix + slug
}

func previewSecret(secret string) string {
	secret = strings.TrimSpace(secret)
	if len(secret) <= 10 {
		return secret
	}
	return secret[:6] + "..." + secret[len(secret)-4:]
}

func toAccount(row *ent.TeamAccount) Account {
	return Account{
		ID:                    row.ID,
		Name:                  row.Name,
		DepartmentExternalID:  row.DepartmentExternalID,
		DepartmentDisplayPath: row.DepartmentDisplayPath,
		RelayUsername:         row.RelayUsername,
		RelayEmail:            row.RelayEmail,
		RelayUserID:           row.RelayUserID,
		OwnerUserID:           row.OwnerUserID,
		Status:                string(row.Status),
		CreatedAt:             row.CreatedAt,
	}
}

func toKey(row *ent.TeamAccountKey) Key {
	key := Key{
		ID:           row.ID,
		RelayKeyID:   row.RelayAPIKeyID,
		RelayGroupID: row.RelayGroupID,
		Name:         row.Name,
		Preview:      row.SecretPreview,
		Status:       string(row.Status),
		CreatedAt:    row.CreatedAt,
	}
	if row.RevokedAt != nil {
		key.RevokedAt = row.RevokedAt
	}
	return key
}
