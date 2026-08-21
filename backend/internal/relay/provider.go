package relay

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors for authentication outcomes.
var (
	ErrInvalidCredentials          = errors.New("relay: invalid credentials")
	ErrExtraVerificationRequired   = errors.New("relay: extra verification required")
	ErrAccountRelationshipsChanged = errors.New("relay: account relationships changed")
)

type userCredentialContextKey struct{}

type UserCredential struct {
	Login    string
	Password string
}

func WithUserCredentials(ctx context.Context, login, password string) context.Context {
	return context.WithValue(ctx, userCredentialContextKey{}, UserCredential{Login: login, Password: password})
}

func UserCredentialsFromContext(ctx context.Context) (login string, password string, ok bool) {
	if ctx == nil {
		return "", "", false
	}
	cred, ok := ctx.Value(userCredentialContextKey{}).(UserCredential)
	if !ok {
		return "", "", false
	}
	if cred.Login == "" || cred.Password == "" {
		return "", "", false
	}
	return cred.Login, cred.Password, true
}

// Provider defines the unified interface for relay server interactions.
type Provider interface {
	Ping(ctx context.Context) error
	Name() string

	Authenticate(ctx context.Context, username, password string) (*User, error)
	GetUser(ctx context.Context, userID int64) (*User, error)
	ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]Group, error)
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByUsername(ctx context.Context, username string) (*User, error)
	CreateUser(ctx context.Context, req CreateUserRequest) (*User, error)
	UpdateUser(ctx context.Context, userID int64, req UpdateUserRequest) (*User, error)

	ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error)
	ChatCompletionWithTools(ctx context.Context, req ChatCompletionRequest, tools []ToolDef) (*ChatCompletionWithToolsResponse, error)

	GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*UsageStats, error)
	ListUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error)
	CreateUserAPIKey(ctx context.Context, userID int64, req APIKeyCreateRequest) (*APIKeyWithSecret, error)
	UpdateUserAPIKeyStatus(ctx context.Context, keyID int64, status string) error
	RevokeUserAPIKey(ctx context.Context, keyID int64) error
	ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]UsageLog, error)

	GetUserUsageDashboard(ctx context.Context, login, password string, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error)
}

// PlatformChatCompleter is an optional extension for relay implementations that
// need platform-native protocol probes instead of a single OpenAI-compatible path.
type PlatformChatCompleter interface {
	ChatCompletionForPlatform(ctx context.Context, platform string, req ChatCompletionRequest) (*ChatCompletionResponse, error)
}

// ProtocolCompleter sends a probe through one explicitly selected client protocol.
type ProtocolCompleter interface {
	CompletionForProtocol(ctx context.Context, platform, protocol string, req ChatCompletionRequest) (*ChatCompletionResponse, error)
}

// PlatformModelLister is an optional extension for relay implementations that
// expose platform-native model-list endpoints.
type PlatformModelLister interface {
	ListModelsForPlatform(ctx context.Context, platform string) ([]ModelOption, error)
}

// PlatformGroupLister exposes provider-wide group display metadata. Callers
// must still resolve current user membership and entitlement separately.
type PlatformGroupLister interface {
	ListPlatformGroups(ctx context.Context) ([]Group, error)
}

// GroupReader resolves one group by stable ID, including inactive groups.
type GroupReader interface {
	GetGroup(ctx context.Context, groupID int64) (*Group, error)
}

// AccountRelationshipReader exposes only the safe account metadata needed to
// inspect group relationships for one platform.
type AccountRelationshipReader interface {
	ListAccountsForPlatform(ctx context.Context, platform string) ([]Account, error)
}

// AccountRelationshipUpdater changes one group relationship while protecting
// every unrelated binding with an expected full-account snapshot.
type AccountRelationshipUpdater interface {
	SetAccountGroupRelationship(ctx context.Context, accountID, groupID int64, expected []AccountGroupRelationship, desiredPriority *int) error
}

// GroupDuplicator creates an inactive copy of a source group. The operation
// key is passed through to the upstream idempotency contract.
type GroupDuplicator interface {
	DuplicateGroup(ctx context.Context, sourceGroupID int64, operationKey string) (*Group, error)
}

// GroupRenamer changes only the display name of an existing group.
type GroupRenamer interface {
	RenameGroup(ctx context.Context, groupID int64, name string) (*Group, error)
}

// GroupStatusUpdater activates or deactivates an existing group. It is kept as
// an optional capability because read-only relay implementations do not need it.
type GroupStatusUpdater interface {
	UpdateGroupStatus(ctx context.Context, groupID int64, status string) error
}

// APIKeyGroupBinder moves one admin-managed API key to a target group.
type APIKeyGroupBinder interface {
	BindAPIKeyToGroup(ctx context.Context, keyID, groupID int64) error
}

// UserDisabler is an optional extension for relay implementations that can
// disable upstream users without exposing provider-specific request details to
// admin/offboarding handlers.
type UserDisabler interface {
	DisableUser(ctx context.Context, userID int64) error
}

type SubjectUsageDashboardProvider interface {
	GetUsageDashboardForUser(ctx context.Context, relayUserID int64, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error)
}

// UserUsageOriginReader reads explicitly selected current-user usage branches
// under one request-scoped Relay session and deadline.
type UserUsageOriginReader interface {
	ReadUserUsageOrigin(ctx context.Context, request UserUsageOriginRequest) (*UserUsageOriginResult, error)
}

// GroupOAuthPoolUsageReader exposes privacy-safe OAuth account-pool snapshots
// for the effective access groups of one relay user.
type GroupOAuthPoolUsageReader interface {
	ReadGroupOAuthPoolUsage(ctx context.Context, groupIDs []int64) (UserUsageGroupPoolUsageState, error)
}

// RequestUsageReader reads exact provider-scoped usage rows for one Request ID.
// Callers deliberately request at most two rows so duplicates are detected,
// never summed.
type RequestUsageReader interface {
	ReadRequestUsage(ctx context.Context, requestID string, limit int) ([]RequestUsage, error)
}

type TeamUsageSummaryProvider interface {
	GetBatchUserUsageStats(ctx context.Context, userIDs []int64, params TeamUsageSummaryParams) (map[int64]TeamUserUsageStats, error)
}

type TeamMemberTrendProvider interface {
	GetUsageTrendForUsers(ctx context.Context, relayUserIDs []int64, params TeamMemberTrendParams) (map[int64][]UsageTrendPoint, error)
}

// ProviderWideTeamUsageProvider exposes bounded provider-wide roster and
// current-usage sources without changing the generic user-directory contract.
type ProviderWideTeamUsageProvider interface {
	GetProviderUserIDs(ctx context.Context) (ProviderDirectoryResult, error)
	GetProviderCurrentUsageStats(ctx context.Context, userIDs []int64) (ProviderCurrentStatsResult, error)
}

// ProviderWideTeamTrendProvider exposes validated raw usage rows before any
// request-specific authorization filter is applied.
type ProviderWideTeamTrendProvider interface {
	GetProviderUsageTrend(ctx context.Context, params TeamMemberTrendParams, limit int) (ProviderWideTrendResult, error)
}

type UserDirectoryProvider interface {
	ListUsers(ctx context.Context) ([]User, error)
}

// UserSubscriptionDirectoryProvider returns the provider directory together
// with each user's complete active subscription Group IDs in one bounded read.
type UserSubscriptionDirectoryProvider interface {
	ListUsersWithActiveSubscriptions(ctx context.Context) ([]User, map[int64][]int64, error)
}

type UserSubscriptionLister interface {
	ListUserSubscriptions(ctx context.Context, relayUserID int64) ([]UserSubscription, error)
}

type UserSubscriptionQuotaResetter interface {
	ResetSubscriptionQuotaForUser(ctx context.Context, relayUserID, groupID int64) error
}

type GroupRateMultiplierManager interface {
	ListGroupRateMultipliers(ctx context.Context, groupID int64) ([]UserGroupRateEntry, error)
	ReplaceGroupRateMultipliers(ctx context.Context, groupID int64, entries []GroupRateMultiplierInput) error
}

type GroupRateMultiplierReadResult struct {
	GroupID int64
	Entries []UserGroupRateEntry
	Err     error
}

type GroupRateMultiplierBatchReader interface {
	GroupRateMultipliersForGroups(ctx context.Context, groupIDs []int64) []GroupRateMultiplierReadResult
}
