package relayplanning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/relaygroupmapping"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/adminusers"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/teamusage"
)

const (
	maxPlanningUsers    = 5000
	maxCandidateWorkers = 8
	defaultValidityDays = 365
	defaultRenewalDays  = 365
	maxRenewalDays      = 36500
	maxGroupNameRunes   = 100
)

type ProviderResolver interface {
	Resolve(context.Context, int) (relay.Provider, error)
}

type prewarmUsageReader interface {
	ReadAuthorizedStats(context.Context, teamusage.PrewarmReadRequest) (map[int64]relay.TeamUserUsageStats, teamusage.PrewarmReadOutcome, error)
}

type subscriptionAssigner interface {
	AssignSubscriptionForUser(context.Context, int64, int64, int) error
}

type subscriptionRemover interface {
	RemoveSubscriptionForUser(context.Context, int64, int64) error
}

type Service struct {
	client        *ent.Client
	resolver      ProviderResolver
	users         *adminusers.Service
	prewarmReader prewarmUsageReader
	now           func() time.Time
}

type providerRelationshipSnapshot struct {
	relationships []relay.UserRelationship
	byUserID      map[int64]relay.UserRelationship
}

type accountListResult struct {
	accounts []relay.Account
	err      error
}

type mappingProviderFacts struct {
	provider      relay.Provider
	groups        []relay.Group
	relationships *mappingRelationshipFacts
	accounts      map[string]*accountListResult
}

type mappingDirectoryFacts struct {
	available   map[string]bool
	departments []DepartmentSuggestion
}

type planningAPIKeyResult struct {
	once sync.Once
	keys []relay.APIKey
	err  error
}

type planningRequestFacts struct {
	relationships *providerRelationshipSnapshot
	accounts      accountListResult
	apiKeyMu      sync.Mutex
	apiKeys       map[int64]*planningAPIKeyResult
}

func newPlanningRequestFacts() *planningRequestFacts {
	return &planningRequestFacts{apiKeys: make(map[int64]*planningAPIKeyResult)}
}

func (facts *planningRequestFacts) userAPIKeys(ctx context.Context, provider relay.Provider, relayUserID int64) ([]relay.APIKey, error) {
	facts.apiKeyMu.Lock()
	result := facts.apiKeys[relayUserID]
	if result == nil {
		result = &planningAPIKeyResult{}
		facts.apiKeys[relayUserID] = result
	}
	facts.apiKeyMu.Unlock()
	result.once.Do(func() {
		result.keys, result.err = provider.ListUserAPIKeys(ctx, relayUserID)
	})
	return result.keys, result.err
}

func (facts *planningRequestFacts) activeUserAPIKeys(ctx context.Context, provider relay.Provider, relayUserID int64) ([]relay.APIKey, error) {
	keys, err := facts.userAPIKeys(ctx, provider, relayUserID)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(append([]relay.APIKey(nil), keys...), func(key relay.APIKey) bool {
		return !strings.EqualFold(strings.TrimSpace(key.Status), "active")
	}), nil
}

func NewService(client *ent.Client, resolver ProviderResolver, prewarmReader *teamusage.PrewarmReader) *Service {
	return &Service{client: client, resolver: resolver, users: adminusers.NewService(client), prewarmReader: prewarmReader, now: time.Now}
}

type PreviewRequest struct {
	ProviderID                    int                     `json:"provider_id"`
	DepartmentID                  string                  `json:"department_id"`
	Platform                      string                  `json:"platform"`
	TemplateGroupID               int64                   `json:"template_group_id"`
	SourceGroupID                 int64                   `json:"source_group_id"`
	WeeklyCostTarget              float64                 `json:"weekly_cost_target"`
	GroupCount                    int                     `json:"group_count"`
	SelectedUserIDs               []int                   `json:"selected_user_ids"`
	Assignments                   []Assignment            `json:"assignments,omitempty"`
	MemberSources                 map[string]int64        `json:"member_sources,omitempty"`
	AdoptRelayUserIDs             []int64                 `json:"adopt_relay_user_ids,omitempty"`
	RemovedUserIDs                []int                   `json:"removed_user_ids,omitempty"`
	MemberActions                 map[string]MemberAction `json:"member_actions,omitempty"`
	ExistingMappingID             int                     `json:"existing_mapping_id"`
	allowUnreviewedRemovalSources map[int]bool
}

type MemberAction struct {
	Mode          string `json:"mode"`
	FromMappingID int    `json:"from_mapping_id,omitempty"`
}

type UserSearchRequest struct {
	ProviderID int
	Platform   string
	Query      string
	Page       int
	PageSize   int
}

type UserSearchDepartment struct {
	ExternalID  string `json:"external_id"`
	Name        string `json:"name"`
	DisplayPath string `json:"display_path"`
}

type UserSearchItem struct {
	UserID             int                   `json:"user_id"`
	RelayUserID        int64                 `json:"relay_user_id,omitempty"`
	Username           string                `json:"username"`
	Email              string                `json:"email"`
	Department         *UserSearchDepartment `json:"department,omitempty"`
	Selectable         bool                  `json:"selectable"`
	DisabledReason     string                `json:"disabled_reason,omitempty"`
	ManagedAssignments []ManagedAssignment   `json:"managed_assignments,omitempty"`
}

type ManagedAssignment struct {
	MappingID      int    `json:"mapping_id"`
	DepartmentID   string `json:"department_id"`
	DepartmentName string `json:"department_name"`
	TargetGroupID  int64  `json:"target_group_id"`
}

type UserSearchPage struct {
	Items    []UserSearchItem `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

type Candidate struct {
	UserID                    int      `json:"user_id"`
	RelayUserID               int64    `json:"relay_user_id"`
	Username                  string   `json:"username"`
	Email                     string   `json:"email"`
	RangeCost                 float64  `json:"range_cost"`
	RangeTokens               int64    `json:"range_tokens"`
	UsageKnown                bool     `json:"usage_known"`
	GlobalTokenRank           int      `json:"global_token_rank"`
	CurrentGroupIDs           []int64  `json:"current_group_ids,omitempty"`
	MigratableKeyCount        int      `json:"migratable_key_count"`
	SourceMember              bool     `json:"source_member"`
	SourceGroupID             int64    `json:"source_group_id,omitempty"`
	CanAdd                    bool     `json:"can_add"`
	Selected                  bool     `json:"selected"`
	Eligible                  bool     `json:"eligible"`
	Warnings                  []string `json:"warnings,omitempty"`
	relationshipSubscriptions []relationshipSubscriptionFact
	relationshipAPIKeys       []relationshipAPIKeyFact
	relationshipGroupErr      error
	relationshipKeyErr        error
	replanUnavailableReason   replanRosterUnavailableReason
}

type Assignment struct {
	Index                    int             `json:"index"`
	TotalCost                float64         `json:"total_cost"`
	UserIDs                  []int           `json:"user_ids"`
	TargetGroupID            int64           `json:"target_group_id,omitempty"`
	TargetUnavailable        bool            `json:"target_unavailable,omitempty"`
	TargetGroupName          string          `json:"target_group_name,omitempty"`
	CurrentTargetGroupName   string          `json:"current_target_group_name,omitempty"`
	SuggestedTargetGroupName string          `json:"suggested_target_group_name,omitempty"`
	RenameSelected           bool            `json:"rename_selected,omitempty"`
	DesiredAccounts          []AccountIntent `json:"desired_accounts,omitempty"`
	Accounts                 []TargetAccount `json:"accounts,omitempty"`
}

type Plan struct {
	ProviderID                int                   `json:"provider_id"`
	DepartmentID              string                `json:"department_id"`
	DepartmentName            string                `json:"department_name"`
	Platform                  string                `json:"platform"`
	TemplateGroupID           int64                 `json:"template_group_id"`
	TemplateGroupName         string                `json:"template_group_name"`
	SourceGroupID             int64                 `json:"source_group_id"`
	SourceGroupName           string                `json:"source_group_name"`
	WeeklyCostTarget          float64               `json:"weekly_cost_target"`
	RecommendedCount          int                   `json:"recommended_group_count"`
	GroupCount                int                   `json:"group_count"`
	Candidates                []Candidate           `json:"candidates"`
	Assignments               []Assignment          `json:"assignments"`
	TemplateAccounts          []TargetAccount       `json:"template_accounts"`
	UnmanagedMembers          []UnmanagedMember     `json:"unmanaged_members,omitempty"`
	TargetSummaries           []TargetChangeSummary `json:"target_summaries"`
	Warnings                  []string              `json:"warnings,omitempty"`
	GeneratedAt               time.Time             `json:"generated_at"`
	MappingID                 int                   `json:"mapping_id,omitempty"`
	RelationshipFingerprint   string                `json:"relationship_fingerprint"`
	AccountsReviewed          bool                  `json:"accounts_reviewed"`
	relationshipSnapshot      relationshipSnapshot
	executionBlockers         []replanRosterBlocker
	unavailableTargetGroupIDs []int64
}

type TargetChangeSummary struct {
	Index           int                  `json:"index"`
	TargetGroupID   int64                `json:"target_group_id,omitempty"`
	TargetGroupName string               `json:"target_group_name"`
	Rename          *GroupRenameChange   `json:"rename,omitempty"`
	Accounts        []AccountChange      `json:"accounts"`
	Members         []MemberChange       `json:"members"`
	Subscriptions   []SubscriptionChange `json:"subscriptions"`
	APIKeys         []APIKeyChange       `json:"api_keys"`
}

type GroupRenameChange struct {
	FromName string `json:"from_name"`
	ToName   string `json:"to_name"`
}

type AccountChange struct {
	AccountID   int64  `json:"account_id"`
	Action      string `json:"action"`
	OldPriority int    `json:"old_priority,omitempty"`
	NewPriority int    `json:"new_priority,omitempty"`
}

type MemberChange struct {
	UserID      int    `json:"user_id,omitempty"`
	RelayUserID int64  `json:"relay_user_id,omitempty"`
	Action      string `json:"action"`
	FromGroupID int64  `json:"from_group_id,omitempty"`
	ToGroupID   int64  `json:"to_group_id,omitempty"`
}

type SubscriptionChange struct {
	UserID      int    `json:"user_id,omitempty"`
	RelayUserID int64  `json:"relay_user_id"`
	Action      string `json:"action"`
	GroupID     int64  `json:"group_id,omitempty"`
}

type APIKeyChange struct {
	UserID      int    `json:"user_id,omitempty"`
	RelayUserID int64  `json:"relay_user_id"`
	Action      string `json:"action"`
	Count       int    `json:"count"`
	FromGroupID int64  `json:"from_group_id,omitempty"`
	ToGroupID   int64  `json:"to_group_id,omitempty"`
}

type UnmanagedMember struct {
	RelayUserID    int64   `json:"relay_user_id"`
	Username       string  `json:"username"`
	Email          string  `json:"email"`
	TargetGroupIDs []int64 `json:"target_group_ids"`
	RangeCost      float64 `json:"range_cost"`
}

type Mapping struct {
	ID                           int                          `json:"id"`
	ProviderID                   int                          `json:"provider_id"`
	DepartmentID                 string                       `json:"department_id"`
	DepartmentName               string                       `json:"department_name"`
	Platform                     string                       `json:"platform"`
	TemplateGroupID              int64                        `json:"template_group_id"`
	TemplateGroupName            string                       `json:"template_group_name"`
	SourceGroupID                int64                        `json:"source_group_id"`
	SourceGroupName              string                       `json:"source_group_name"`
	GroupIDs                     []int64                      `json:"group_ids"`
	Status                       string                       `json:"status"`
	WeeklyCostTarget             float64                      `json:"weekly_cost_target"`
	MemberAssignments            map[string]int64             `json:"member_assignments,omitempty"`
	MemberSources                map[string]int64             `json:"member_sources,omitempty"`
	AccountManagementInitialized bool                         `json:"account_management_initialized"`
	DesiredAccounts              map[string][]AccountIntent   `json:"desired_accounts"`
	AccountPools                 []TargetAccountPool          `json:"account_pools"`
	OperationState               map[string]map[string]string `json:"operation_state,omitempty"`
	BaselineRevision             int64                        `json:"baseline_revision"`
	UnmanagedMembers             []UnmanagedMember            `json:"unmanaged_members,omitempty"`
	DepartmentSuggestions        []DepartmentSuggestion       `json:"department_suggestions,omitempty"`
	Warnings                     []string                     `json:"warnings,omitempty"`
	UpdatedAt                    time.Time                    `json:"updated_at"`
}

type MappingRenewalPreviewRequest struct {
	RenewalDays *int `json:"renewal_days"`
}

type MappingRenewalPreview struct {
	MappingID               int                    `json:"mapping_id"`
	ProviderID              int                    `json:"provider_id"`
	Platform                string                 `json:"platform"`
	RenewalDays             int                    `json:"renewal_days"`
	Members                 []MappingRenewalMember `json:"members"`
	GeneratedAt             time.Time              `json:"generated_at"`
	RelationshipFingerprint string                 `json:"relationship_fingerprint"`
}

type MappingRenewalExecuteRequest struct {
	RenewalDays                     int                            `json:"renewal_days"`
	Members                         []MappingRenewalReviewedMember `json:"members"`
	ExpectedRelationshipFingerprint string                         `json:"expected_relationship_fingerprint"`
	OperationKey                    string                         `json:"operation_key"`
	Retry                           bool                           `json:"retry,omitempty"`
}

type MappingRenewalReviewedMember struct {
	UserID        int    `json:"user_id"`
	TargetGroupID int64  `json:"target_group_id"`
	PlannedAction string `json:"planned_action"`
}

type MappingRenewalExecution struct {
	MappingID    int                          `json:"mapping_id"`
	RenewalDays  int                          `json:"renewal_days"`
	OperationKey string                       `json:"operation_key"`
	Members      []MappingRenewalMemberResult `json:"members"`
	Preview      *MappingRenewalPreview       `json:"preview,omitempty"`
	PreviewError string                       `json:"preview_error,omitempty"`
}

type MappingRenewalMemberResult struct {
	UserID        int    `json:"user_id"`
	RelayUserID   int64  `json:"relay_user_id"`
	TargetGroupID int64  `json:"target_group_id"`
	Action        string `json:"action"`
	Status        string `json:"status"`
	Error         string `json:"error,omitempty"`
}

type StaleMappingRenewalError struct {
	ExpectedFingerprint string
	CurrentFingerprint  string
	RefreshedPreview    *MappingRenewalPreview
	Differences         []string
}

func (e *StaleMappingRenewalError) Error() string {
	return "Relay relationships changed after Preview"
}

type MappingRenewalMember struct {
	UserID                  int                   `json:"user_id"`
	RelayUserID             int64                 `json:"relay_user_id"`
	Username                string                `json:"username"`
	Email                   string                `json:"email"`
	ExpectedTargetGroupID   int64                 `json:"expected_target_group_id"`
	ExpectedTargetGroupName string                `json:"expected_target_group_name"`
	Status                  string                `json:"status"`
	CurrentExpiry           *time.Time            `json:"current_expiry,omitempty"`
	PlannedAction           string                `json:"planned_action"`
	ResultingExpiry         *time.Time            `json:"resulting_expiry,omitempty"`
	Drift                   []MappingRenewalDrift `json:"drift,omitempty"`
	subscriptions           []relay.UserSubscription
	expectedSubscriptionID  int64
}

type MappingRenewalDrift struct {
	GroupID   int64      `json:"group_id"`
	GroupName string     `json:"group_name"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type AccountIntent struct {
	AccountID int64 `json:"account_id"`
	Priority  int   `json:"priority"`
}

type TargetAccount struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Platform    string `json:"platform"`
	Type        string `json:"type"`
	Status      string `json:"status"`
	Schedulable bool   `json:"schedulable"`
	Priority    int    `json:"priority"`
}

type TargetAccountPool struct {
	TargetGroupID int64           `json:"target_group_id"`
	Current       []TargetAccount `json:"current"`
	Desired       []AccountIntent `json:"desired"`
	Drift         bool            `json:"drift"`
}

type AccountSearchRequest struct {
	ProviderID int
	Platform   string
	Query      string
	Page       int
	PageSize   int
}

type AccountSearchPage struct {
	Items    []relay.Account `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type DepartmentSuggestion struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ExecuteRequest struct {
	PreviewRequest
	OperationKey                    string `json:"operation_key"`
	ExpectedRelationshipFingerprint string `json:"expected_relationship_fingerprint"`
	InitiatedByUserID               int    `json:"-"`
}

type StalePlanError struct {
	ExpectedFingerprint string
	CurrentFingerprint  string
	RefreshedPlan       *Plan
	Differences         []string
}

type ExistingMappingError struct {
	MappingID int
}

func (e *ExistingMappingError) Error() string {
	return "a Relay Group Mapping already exists for this Provider, department, and Platform"
}

type assignmentCandidateError struct {
	UserID     int
	Difference string
}

type redactedProviderReadError struct {
	cause error
}

func (e *redactedProviderReadError) Error() string {
	return "provider read failed"
}

func (e *redactedProviderReadError) Unwrap() error {
	return e.cause
}

func redactProviderReadError(err error) error {
	return &redactedProviderReadError{cause: err}
}

func (e *assignmentCandidateError) Error() string {
	return fmt.Sprintf("user %d cannot be added to a target group", e.UserID)
}

func (e *StalePlanError) Error() string {
	return "Relay relationships changed after Preview"
}

type GroupResult struct {
	Index       int    `json:"index"`
	ID          int64  `json:"id,omitempty"`
	Name        string `json:"name,omitempty"`
	CurrentName string `json:"current_name,omitempty"`
	Status      string `json:"status"`
	Rename      string `json:"rename,omitempty"`
	Creation    string `json:"creation,omitempty"`
	Error       string `json:"error,omitempty"`
}

type MemberResult struct {
	Action          string   `json:"action,omitempty"`
	UserID          int      `json:"user_id"`
	RelayUserID     int64    `json:"relay_user_id,omitempty"`
	TargetGroupID   int64    `json:"target_group_id,omitempty"`
	Subscription    string   `json:"subscription"`
	SourceRemoval   string   `json:"source_removal"`
	APIKeys         []string `json:"api_keys,omitempty"`
	Error           string   `json:"error,omitempty"`
	reviewedAPIKeys reviewedAPIKeySelection
	stepIdentity    string
}

type AccountResult struct {
	TargetGroupID   int64  `json:"target_group_id"`
	AccountID       int64  `json:"account_id,omitempty"`
	DesiredPriority *int   `json:"desired_priority,omitempty"`
	Status          string `json:"status"`
	Error           string `json:"error,omitempty"`
}

type MappingPersistenceResult struct {
	MappingID int    `json:"mapping_id"`
	Role      string `json:"role"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

type MappingPersistenceError struct {
	Cause   error
	Results []MappingPersistenceResult
}

type LegacyOperationConflictError struct {
	Reason string
}

func (e *LegacyOperationConflictError) Error() string {
	if e.Reason == "incomplete_identity" {
		return "legacy operation identity is incomplete; manual intervention is required"
	}
	if e.Reason == "active_operation" {
		return "mapping changes are blocked while a legacy operation is unresolved"
	}
	if e.Reason == "readback_mismatch" {
		return "legacy operation readback no longer matches its reviewed resources; manual intervention is required"
	}
	return "legacy operation can only resume its exact reviewed direction"
}

func (e *MappingPersistenceError) Error() string {
	return fmt.Sprintf("persist relay mapping changes: %v", e.Cause)
}

func (e *MappingPersistenceError) Unwrap() error {
	return e.Cause
}

type ExecutionResult struct {
	Plan     *Plan                      `json:"plan"`
	Groups   []GroupResult              `json:"groups"`
	Accounts []AccountResult            `json:"accounts"`
	Members  []MemberResult             `json:"members"`
	Mappings []MappingPersistenceResult `json:"mappings,omitempty"`
	Mapping  *Mapping                   `json:"mapping,omitempty"`
	Warnings []string                   `json:"warnings,omitempty"`
}

func (s *Service) SearchUsers(ctx context.Context, req UserSearchRequest) (*UserSearchPage, error) {
	if req.ProviderID <= 0 || strings.TrimSpace(req.Platform) == "" {
		return nil, fmt.Errorf("provider_id and platform are required")
	}
	p, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	page, err := s.users.List(ctx, adminusers.ListRequest{
		Filters:  adminusers.Filters{Query: req.Query},
		Page:     req.Page,
		PageSize: req.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("search users: %w", err)
	}
	items := make([]UserSearchItem, len(page.Users))
	jobs := make(chan int)
	workerCount := maxCandidateWorkers
	if len(items) < workerCount {
		workerCount = len(items)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				local := page.Users[index]
				item := UserSearchItem{UserID: local.ID, Username: local.Username, Email: local.Email}
				if department := page.DepartmentsByUserID[local.ID]; department != nil {
					item.Department = &UserSearchDepartment{ExternalID: department.ExternalID, Name: department.Name, DisplayPath: department.DisplayPath}
				}
				switch {
				case local.RelayUserID == nil || *local.RelayUserID <= 0:
					item.DisabledReason = "no relay mapping for the selected provider"
				default:
					item.RelayUserID = int64(*local.RelayUserID)
					remote, getErr := p.GetUser(ctx, item.RelayUserID)
					if getErr != nil {
						item.DisabledReason = "relay mapping could not be verified for the selected provider"
					} else if !sameRelayIdentity(local.Username, local.Email, item.RelayUserID, remote) {
						item.DisabledReason = "relay mapping is not valid for the selected provider"
					} else {
						item.Selectable = true
					}
				}
				items[index] = item
			}
		}()
	}
	for index := range items {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	mappings, mappingErr := s.client.RelayGroupMapping.Query().Where(
		relaygroupmapping.ProviderIDEQ(req.ProviderID),
		relaygroupmapping.PlatformEQ(strings.TrimSpace(req.Platform)),
	).All(ctx)
	if mappingErr != nil {
		return nil, fmt.Errorf("load managed assignments: %w", mappingErr)
	}
	for index := range items {
		key := strconv.Itoa(items[index].UserID)
		for _, mapping := range mappings {
			if targetGroupID := mapping.MemberAssignments[key]; targetGroupID > 0 {
				items[index].ManagedAssignments = append(items[index].ManagedAssignments, ManagedAssignment{MappingID: mapping.ID, DepartmentID: mapping.DepartmentExternalID, DepartmentName: mapping.DepartmentName, TargetGroupID: targetGroupID})
			}
		}
	}
	return &UserSearchPage{Items: items, Total: page.Total, Page: page.Page, PageSize: page.PageSize}, nil
}

func sameRelayIdentity(username, email string, relayUserID int64, remote *relay.User) bool {
	if remote == nil || remote.ID != relayUserID {
		return false
	}
	localEmail, remoteEmail := strings.TrimSpace(email), strings.TrimSpace(remote.Email)
	if localEmail != "" && remoteEmail != "" {
		return strings.EqualFold(localEmail, remoteEmail)
	}
	localUsername, remoteUsername := strings.TrimSpace(username), strings.TrimSpace(remote.Username)
	return localUsername != "" && remoteUsername != "" && strings.EqualFold(localUsername, remoteUsername)
}

func (s *Service) Preview(ctx context.Context, req PreviewRequest) (*Plan, error) {
	req = normalizeRequest(req)
	if err := validateRequest(req); err != nil {
		return nil, fmt.Errorf("validate relay planning request: %w", err)
	}
	if err := s.rejectExistingInitialMapping(ctx, req); err != nil {
		return nil, fmt.Errorf("reject existing Relay Group Mapping: %w", err)
	}
	providerConfig, err := s.client.RelayProvider.Get(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("load relay provider configuration: %w", err)
	}
	p, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	lister, ok := p.(relay.PlatformGroupLister)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support group listing")
	}
	facts := newPlanningRequestFacts()
	var groups []relay.Group
	var groupsErr, relationshipsErr error
	var relationshipReads sync.WaitGroup
	relationshipReads.Add(1)
	go func() {
		defer relationshipReads.Done()
		groups, groupsErr = lister.ListPlatformGroups(ctx)
	}()
	if req.ExistingMappingID > 0 {
		_, supported := p.(relay.UserRelationshipSnapshotReader)
		if supported {
			relationshipReads.Add(1)
			go func() {
				defer relationshipReads.Done()
				facts.relationships, relationshipsErr = loadProviderRelationshipSnapshot(ctx, p)
			}()
		}
	}
	if accountReader, supported := p.(relay.AccountRelationshipReader); supported {
		relationshipReads.Add(1)
		go func() {
			defer relationshipReads.Done()
			facts.accounts.accounts, facts.accounts.err = accountReader.ListAccountsForPlatform(ctx, req.Platform)
		}()
	} else {
		facts.accounts.err = fmt.Errorf("relay provider does not support account relationship reading")
	}
	relationshipReads.Wait()
	if groupsErr != nil {
		return nil, fmt.Errorf("list relay groups: %w", redactProviderReadError(groupsErr))
	}
	if relationshipsErr != nil {
		return nil, fmt.Errorf("list Relay user relationships: %w", relationshipsErr)
	}
	template, err := findSourceGroup(groups, req.TemplateGroupID, req.Platform)
	if err != nil {
		return nil, fmt.Errorf("resolve template group: %w", err)
	}
	var source relay.Group
	if req.SourceGroupID > 0 {
		source, err = findSourceGroup(groups, req.SourceGroupID, req.Platform)
		if err != nil {
			return nil, fmt.Errorf("resolve migration source group: %w", err)
		}
	}
	var mapping *ent.RelayGroupMapping
	if req.ExistingMappingID > 0 {
		mapping, err = s.client.RelayGroupMapping.Get(ctx, req.ExistingMappingID)
		if err != nil {
			return nil, fmt.Errorf("load relay group mapping assignments: %w", err)
		}
		reviewedSources := cloneInt64Map(req.MemberSources)
		for userID, groupID := range mapping.MemberSources {
			if _, overridden := reviewedSources[userID]; !overridden {
				reviewedSources[userID] = groupID
			}
		}
		req.MemberSources = reviewedSources
		managedTargets := make(map[int64]struct{}, len(mapping.GroupIds))
		for _, groupID := range mapping.GroupIds {
			managedTargets[groupID] = struct{}{}
		}
		for _, userID := range req.RemovedUserIDs {
			sourceGroupID, reviewed := req.MemberSources[strconv.Itoa(userID)]
			if !reviewed {
				if req.allowUnreviewedRemovalSources[userID] {
					continue
				}
				return nil, fmt.Errorf("removal source for user %d must be reviewed", userID)
			}
			if sourceGroupID == mapping.TemplateGroupID {
				return nil, fmt.Errorf("removal source for user %d cannot be the template group", userID)
			}
			if _, managedTarget := managedTargets[sourceGroupID]; sourceGroupID > 0 && managedTarget {
				return nil, fmt.Errorf("removal source for user %d cannot be a managed target group", userID)
			}
		}
	}
	if err := validateMemberSourceGroups(req.MemberSources, groups, req.Platform); err != nil {
		return nil, fmt.Errorf("validate member source groups: %w", err)
	}
	users, err := s.users.Targets(ctx, adminusers.Filters{DepartmentID: req.DepartmentID}, maxPlanningUsers)
	if err != nil {
		return nil, fmt.Errorf("load department users: %w", err)
	}
	selected := selectedSet(req.SelectedUserIDs)
	required := make(map[int]struct{}, len(selected)+len(req.RemovedUserIDs))
	for userID := range selected {
		required[userID] = struct{}{}
	}
	restrictToRequired := len(selected) > 0 || len(req.RemovedUserIDs) > 0
	for _, userID := range req.RemovedUserIDs {
		if userID > 0 {
			required[userID] = struct{}{}
		}
	}
	if !restrictToRequired && mapping != nil {
		required = make(map[int]struct{}, len(mapping.MemberAssignments))
		for rawUserID := range mapping.MemberAssignments {
			if userID, parseErr := strconv.Atoi(rawUserID); parseErr == nil && userID > 0 {
				required[userID] = struct{}{}
			}
		}
	}
	if len(required) > 0 {
		byID := make(map[int]*ent.User, len(users))
		for _, u := range users {
			byID[u.ID] = u
		}
		missing := make([]int, 0)
		for userID := range required {
			if byID[userID] == nil {
				missing = append(missing, userID)
			}
		}
		if len(missing) > 0 {
			extra, queryErr := s.client.User.Query().Where(user.IDIn(missing...)).All(ctx)
			if queryErr != nil {
				return nil, fmt.Errorf("load explicitly selected users: %w", queryErr)
			}
			for _, u := range extra {
				byID[u.ID] = u
			}
			if !restrictToRequired {
				users = append(users, extra...)
			}
		}
		if restrictToRequired {
			filtered := make([]*ent.User, 0, len(required))
			for userID := range required {
				if u := byID[userID]; u != nil {
					filtered = append(filtered, u)
				}
			}
			sort.Slice(filtered, func(i, j int) bool { return filtered[i].ID < filtered[j].ID })
			users = filtered
		}
	}
	candidates, err := s.buildCandidates(ctx, p, facts, req.ProviderID, providerConfig.ConfigurationVersion, users, source, groups, req.MemberSources, req.Platform, req.DepartmentID)
	if err != nil {
		return nil, fmt.Errorf("build relay planning candidates: %w", err)
	}
	if mapping != nil {
		classifyManagedRosterCandidates(mapping, candidates)
	}
	eligible := make([]Candidate, 0, len(candidates))
	selectedProvided := len(selected) > 0
	for index := range candidates {
		candidates[index].Selected = candidates[index].Eligible
		if selectedProvided {
			_, candidates[index].Selected = selected[candidates[index].UserID]
		}
		if candidates[index].Eligible && candidates[index].Selected {
			eligible = append(eligible, candidates[index])
		}
	}
	recommended, count := resolveGroupCount(req, eligible)
	if req.Assignments != nil {
		count = assignmentCount(req.Assignments)
	}
	assignments := allocate(eligible, count)
	var unmanagedMembers []UnmanagedMember
	var replanBlockers []replanRosterBlocker
	var unavailableTargetGroupIDs []int64
	if mapping != nil {
		groups, err = includePendingCreationGroups(ctx, p, groups, mapping.OperationState, req.Platform)
		if err != nil {
			return nil, fmt.Errorf("load pending relay planning targets: %w", err)
		}
		unmanagedMembers, err = s.loadUnmanagedMembers(ctx, p, facts, providerConfig.ConfigurationVersion, mapping)
		if err != nil {
			return nil, fmt.Errorf("load unmanaged relay members: %w", err)
		}
		rosterInput, inputErr := replanRosterInputFromPlan(mapping, candidates, unmanagedMembers, groups, req.Assignments, req.RemovedUserIDs)
		if inputErr != nil {
			return nil, fmt.Errorf("validate relay planning assignments: %w", inputErr)
		}
		roster, rosterErr := reviewReplanRoster(rosterInput)
		if rosterErr != nil {
			return nil, fmt.Errorf("validate relay planning assignments: %w", rosterErr)
		}
		assignments = assignmentsFromReplanRoster(roster, req.Assignments)
		replanBlockers = append(replanBlockers, roster.Blockers...)
		unavailableTargetGroupIDs = append(unavailableTargetGroupIDs, roster.UnavailableTargetGroupIDs...)
		if req.Assignments == nil {
			restoreRenameRetries(mapping.OperationState, assignments)
		}
		if len(req.RemovedUserIDs) > 0 {
			removed := selectedSet(req.RemovedUserIDs)
			for userID := range removed {
				key := strconv.Itoa(userID)
				_, managed := mapping.MemberAssignments[key]
				entry := mapping.OperationState["member:"+key]
				retryingRemoval := entry != nil && entry["action"] == "remove" && entry["target_group_id"] != "" && operationStateNeedsRetry(mapping.OperationState, "member:"+key)
				if !managed && !retryingRemoval {
					return nil, fmt.Errorf("user %d is not managed by this mapping", userID)
				}
			}
		}
	}
	if mapping == nil && req.Assignments != nil {
		assignments, err = validateAssignments(req.Assignments, candidates, count)
		if err != nil {
			return nil, fmt.Errorf("validate relay planning assignments: %w", err)
		}
		addUnmanagedCapacity(assignments, unmanagedMembers)
	}
	departmentName := ""
	if mapping != nil {
		departmentName = mapping.DepartmentName
	}
	if currentDepartmentName, nameErr := s.departmentName(ctx, req.DepartmentID); nameErr == nil {
		departmentName = currentDepartmentName
	} else if mapping == nil {
		return nil, fmt.Errorf("load relay planning department name: %w", nameErr)
	}
	if err := s.assignTargets(ctx, req, groups, departmentName, assignments); err != nil {
		return nil, fmt.Errorf("assign relay planning targets: %w", err)
	}
	if err := validateTargetGroupNames(assignments, groups); err != nil {
		return nil, fmt.Errorf("validate relay planning target names: %w", err)
	}
	templateAccounts, err := assignPreviewAccounts(facts.accounts, req.Platform, template.ID, mapping, assignments)
	if err != nil {
		return nil, fmt.Errorf("assign relay planning Accounts: %w", err)
	}
	warnings := make([]string, 0)
	if len(eligible) == 0 && (mapping == nil || len(mapping.MemberAssignments) == 0) {
		warnings = append(warnings, "no eligible member has a valid relay mapping and source-group membership")
	}
	if req.WeeklyCostTarget > 0 {
		for _, assignment := range assignments {
			if assignment.TotalCost > req.WeeklyCostTarget {
				name := assignment.TargetGroupName
				if name == "" {
					name = fmt.Sprintf("group %d", assignment.Index+1)
				}
				warnings = append(warnings, fmt.Sprintf("%s exceeds the planning target", name))
			}
		}
	}
	for _, candidate := range candidates {
		warnings = append(warnings, candidate.Warnings...)
	}
	warnings = append(warnings, replanRosterWarnings(replanBlockers)...)
	warnings = append(warnings, replanUnavailableTargetWarnings(unavailableTargetGroupIDs)...)
	plan := &Plan{
		ProviderID: req.ProviderID, DepartmentID: req.DepartmentID, Platform: req.Platform,
		TemplateGroupID: template.ID, TemplateGroupName: template.Name,
		SourceGroupID: source.ID, SourceGroupName: source.Name, WeeklyCostTarget: req.WeeklyCostTarget,
		RecommendedCount: recommended, GroupCount: count, Candidates: candidates, Assignments: assignments, TemplateAccounts: templateAccounts,
		UnmanagedMembers: unmanagedMembers,
		Warnings:         uniqueStrings(warnings), GeneratedAt: time.Now().UTC(),
		AccountsReviewed:          mapping == nil || mapping.AccountManagementInitialized || assignmentsReviewAccounts(req.Assignments),
		executionBlockers:         replanBlockers,
		unavailableTargetGroupIDs: unavailableTargetGroupIDs,
	}
	assigned := make(map[int]struct{})
	for _, assignment := range assignments {
		for _, userID := range assignment.UserIDs {
			assigned[userID] = struct{}{}
		}
	}
	for index := range plan.Candidates {
		if mapping != nil || req.Assignments != nil {
			_, plan.Candidates[index].Selected = assigned[plan.Candidates[index].UserID]
		}
	}
	plan.DepartmentName = departmentName
	if req.ExistingMappingID > 0 {
		plan.MappingID = req.ExistingMappingID
	}
	plan.RelationshipFingerprint, err = s.relationshipFingerprint(ctx, p, facts, req, plan, groups)
	if err != nil {
		return nil, fmt.Errorf("fingerprint relay relationships: %w", err)
	}
	plan.TargetSummaries = buildTargetChangeSummaries(req, plan)
	return plan, nil
}

func restoreRenameRetries(operationState map[string]map[string]string, assignments []Assignment) {
	for _, entry := range operationState {
		if entry["rename"] != "failed" || entry["target_group_name"] == "" {
			continue
		}
		groupID, err := strconv.ParseInt(entry["target_group_id"], 10, 64)
		if err != nil || groupID <= 0 {
			continue
		}
		for index := range assignments {
			if assignments[index].TargetGroupID == groupID {
				assignments[index].RenameSelected = true
				assignments[index].TargetGroupName = entry["target_group_name"]
				break
			}
		}
	}
}

func includePendingCreationGroups(ctx context.Context, provider relay.Provider, groups []relay.Group, operationState map[string]map[string]string, platform string) ([]relay.Group, error) {
	pending := pendingCreationTargetIDs(operationState)
	if len(pending) == 0 {
		return groups, nil
	}
	for _, group := range groups {
		delete(pending, group.ID)
	}
	if len(pending) == 0 {
		return groups, nil
	}
	reader, ok := provider.(relay.GroupReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support pending group reading")
	}
	groupIDs := make([]int64, 0, len(pending))
	for groupID := range pending {
		groupIDs = append(groupIDs, groupID)
	}
	sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
	for _, groupID := range groupIDs {
		group, err := reader.GetGroup(ctx, groupID)
		if err != nil {
			return nil, fmt.Errorf("get pending group %d: %w", groupID, redactProviderReadError(err))
		}
		if group == nil || group.ID != groupID {
			return nil, fmt.Errorf("get pending group %d: relay returned an unexpected group", groupID)
		}
		if !strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform)) {
			return nil, fmt.Errorf("pending group %d does not belong to platform %s", groupID, platform)
		}
		groups = append(groups, *group)
	}
	return groups, nil
}

func pendingCreationTargetIDs(operationState map[string]map[string]string) map[int64]struct{} {
	pending := make(map[int64]struct{})
	for key, entry := range operationState {
		if !strings.HasPrefix(key, "group:") || entry["creation"] != "pending" {
			continue
		}
		if targetGroupID, err := strconv.ParseInt(entry["target_group_id"], 10, 64); err == nil && targetGroupID > 0 {
			pending[targetGroupID] = struct{}{}
		}
	}
	return pending
}

func replanRosterInputFromPlan(mapping *ent.RelayGroupMapping, candidates []Candidate, unmanaged []UnmanagedMember, groups []relay.Group, reviewed []Assignment, removedUserIDs []int) (replanRosterInput, error) {
	savedAssignments := make(map[int]int64, len(mapping.MemberAssignments))
	for rawUserID, groupID := range mapping.MemberAssignments {
		userID, err := strconv.Atoi(rawUserID)
		if err == nil && userID > 0 && groupID > 0 {
			savedAssignments[userID] = groupID
		}
	}
	members := make([]replanRosterMember, 0, len(candidates))
	for _, candidate := range candidates {
		members = append(members, replanRosterMember{
			UserID:            candidate.UserID,
			Assignable:        candidate.CanAdd,
			UnavailableReason: candidate.replanUnavailableReason,
			RangeCost:         candidate.RangeCost,
			CurrentGroupIDs:   append([]int64(nil), candidate.CurrentGroupIDs...),
		})
	}
	unmanagedCosts := make(map[int64]float64)
	for _, member := range unmanaged {
		for _, groupID := range member.TargetGroupIDs {
			unmanagedCosts[groupID] += member.RangeCost
		}
	}
	reviewedTargets := make([]replanRosterTargetReview, 0, len(reviewed))
	for _, assignment := range reviewed {
		reviewedTargets = append(reviewedTargets, replanRosterTargetReview{Index: assignment.Index, UserIDs: append([]int(nil), assignment.UserIDs...)})
	}
	availableTargets := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(mapping.Platform)) {
			availableTargets[group.ID] = struct{}{}
		}
	}
	targetCount := len(mapping.GroupIds)
	if reviewed != nil {
		targetCount = assignmentCount(reviewed)
		if targetCount < len(mapping.GroupIds) {
			return replanRosterInput{}, fmt.Errorf("assignments must retain all %d existing target groups", len(mapping.GroupIds))
		}
		for _, assignment := range reviewed {
			if assignment.Index < 0 || assignment.Index >= targetCount {
				continue
			}
			switch {
			case assignment.Index < len(mapping.GroupIds) && assignment.TargetGroupID > 0 && assignment.TargetGroupID != mapping.GroupIds[assignment.Index]:
				return replanRosterInput{}, fmt.Errorf("assignment index %d must retain target group %d", assignment.Index, mapping.GroupIds[assignment.Index])
			case assignment.Index >= len(mapping.GroupIds) && assignment.TargetGroupID > 0:
				return replanRosterInput{}, fmt.Errorf("proposed assignment index %d cannot supply a target group ID", assignment.Index)
			}
		}
	}
	targets := make([]replanRosterTargetInput, targetCount)
	for index, groupID := range mapping.GroupIds {
		_, available := availableTargets[groupID]
		targets[index] = replanRosterTargetInput{GroupID: groupID, Available: available}
	}
	for index := len(mapping.GroupIds); index < len(targets); index++ {
		targets[index] = replanRosterTargetInput{Available: true}
	}
	return replanRosterInput{
		Targets:          targets,
		SavedAssignments: savedAssignments,
		Members:          members,
		UnmanagedCosts:   unmanagedCosts,
		HasReview:        reviewed != nil,
		ReviewedTargets:  reviewedTargets,
		RemovedUserIDs:   append([]int(nil), removedUserIDs...),
	}, nil
}

func assignmentsFromReplanRoster(roster replanRosterResult, reviewed []Assignment) []Assignment {
	assignments := make([]Assignment, len(roster.Targets))
	if reviewed != nil {
		for _, assignment := range reviewed {
			var desiredAccounts []AccountIntent
			if assignment.DesiredAccounts != nil {
				desiredAccounts = append([]AccountIntent(nil), assignment.DesiredAccounts...)
			}
			assignments[assignment.Index] = Assignment{
				Index:           assignment.Index,
				TargetGroupID:   assignment.TargetGroupID,
				TargetGroupName: strings.TrimSpace(assignment.TargetGroupName),
				RenameSelected:  assignment.RenameSelected,
				DesiredAccounts: desiredAccounts,
			}
		}
	}
	for _, target := range roster.Targets {
		assignments[target.Index].Index = target.Index
		assignments[target.Index].TargetGroupID = target.GroupID
		assignments[target.Index].TargetUnavailable = target.Unavailable
		assignments[target.Index].UserIDs = append([]int(nil), target.UserIDs...)
		assignments[target.Index].TotalCost = target.TotalCost
	}
	return assignments
}

func replanRosterWarnings(blockers []replanRosterBlocker) []string {
	warnings := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		warning, _ := replanRosterBlockerMessages(blocker)
		if warning != "" {
			warnings = append(warnings, warning)
		}
	}
	return warnings
}

func replanRosterDifferences(blockers []replanRosterBlocker) []string {
	differences := make([]string, 0, len(blockers))
	for _, blocker := range blockers {
		_, difference := replanRosterBlockerMessages(blocker)
		if difference != "" {
			differences = append(differences, difference)
		}
	}
	return uniqueStrings(differences)
}

func replanRosterBlockerMessages(blocker replanRosterBlocker) (warning, difference string) {
	switch blocker.Reason {
	case replanRosterUnavailableIdentity:
		return fmt.Sprintf("user %d has no relay mapping", blocker.UserID), replanRosterUnavailableDifference(blocker.Reason)
	case replanRosterUnavailableSubscription:
		return fmt.Sprintf("subscription relationships for user %d are unavailable", blocker.UserID), replanRosterUnavailableDifference(blocker.Reason)
	case replanRosterMissingTargetSubscription:
		return fmt.Sprintf("user %d is missing the expected managed Target subscription", blocker.UserID), replanRosterUnavailableDifference(blocker.Reason)
	case replanRosterMismatchedTargetAPIKey:
		return fmt.Sprintf("user %d has a reviewed API Key outside the expected managed Target", blocker.UserID), replanRosterUnavailableDifference(blocker.Reason)
	default:
		return "", ""
	}
}

func replanUnavailableTargetWarnings(groupIDs []int64) []string {
	warnings := make([]string, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		warnings = append(warnings, fmt.Sprintf("target group %d is unavailable", groupID))
	}
	return warnings
}

func replanUnavailableTargetDifferences(groupIDs []int64) []string {
	if len(groupIDs) == 0 {
		return nil
	}
	return []string{"a Target Group changed or is no longer available"}
}

func replanRosterUnavailableDifference(reason replanRosterUnavailableReason) string {
	switch reason {
	case replanRosterUnavailableSubscription:
		return "subscription relationships changed"
	case replanRosterMissingTargetSubscription:
		return "managed Target subscription relationships changed"
	case replanRosterMismatchedTargetAPIKey:
		return "managed Target API Key relationships changed"
	default:
		return "Relay user mappings changed or are no longer available"
	}
}

func classifyManagedRosterCandidates(mapping *ent.RelayGroupMapping, candidates []Candidate) {
	for index := range candidates {
		candidate := &candidates[index]
		expectedTargetID := mapping.MemberAssignments[strconv.Itoa(candidate.UserID)]
		if expectedTargetID <= 0 {
			continue
		}
		candidate.Warnings = slices.DeleteFunc(candidate.Warnings, func(warning string) bool {
			return warning == "user is not a member of the selected source group"
		})
		stateKey := "member:" + strconv.Itoa(candidate.UserID)
		if candidate.replanUnavailableReason != 0 || operationStateNeedsRetry(mapping.OperationState, stateKey) {
			continue
		}
		if !slices.Contains(candidate.CurrentGroupIDs, expectedTargetID) {
			candidate.replanUnavailableReason = replanRosterMissingTargetSubscription
			continue
		}
		entry := mapping.OperationState[stateKey]
		entryTargetID, _ := strconv.ParseInt(entry["target_group_id"], 10, 64)
		completedKeyIDs, _ := completedAPIKeySteps(strings.Split(entry["api_keys"], ","))
		if entry["action"] != "remove" && entryTargetID == expectedTargetID && !reviewedAPIKeysMatchTarget(completedKeyIDs, candidate.relationshipAPIKeys, expectedTargetID) {
			candidate.replanUnavailableReason = replanRosterMismatchedTargetAPIKey
			continue
		}
	}
}

func reviewedAPIKeysMatchTarget(reviewed map[int64]bool, current []relationshipAPIKeyFact, targetGroupID int64) bool {
	for keyID := range reviewed {
		matched := false
		for _, key := range current {
			if key.ID == keyID && key.GroupID == targetGroupID {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func addUnmanagedCapacity(assignments []Assignment, unmanaged []UnmanagedMember) {
	for _, member := range unmanaged {
		for _, targetGroupID := range member.TargetGroupIDs {
			for index := range assignments {
				if assignments[index].TargetGroupID == targetGroupID {
					assignments[index].TotalCost += member.RangeCost
					break
				}
			}
		}
	}
}

func duplicateAndRenameProposedTarget(
	ctx context.Context,
	duplicator relay.GroupDuplicator,
	renamer relay.GroupRenamer,
	templateGroupID int64,
	creationKey string,
	reservedGroupIDs []int64,
	assignment *Assignment,
	afterDuplicate func(GroupResult) error,
) (GroupResult, error) {
	result := GroupResult{Index: assignment.Index, Name: assignment.TargetGroupName, Status: "failed", Rename: "skipped", Creation: "failed"}
	if duplicator == nil {
		result.Error = "relay provider does not support group duplication"
		return result, nil
	}
	group, err := duplicator.DuplicateGroup(ctx, templateGroupID, creationKey)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	if group == nil || group.ID <= 0 || containsInt64(reservedGroupIDs, group.ID) {
		result.Error = "relay returned an unexpected group after duplication"
		return result, nil
	}
	assignment.TargetGroupID = group.ID
	assignment.CurrentTargetGroupName = group.Name
	result.ID = group.ID
	result.CurrentName = group.Name
	result.Creation = "pending"
	if afterDuplicate != nil {
		if err := afterDuplicate(result); err != nil {
			return result, err
		}
	}
	if group.Name == assignment.TargetGroupName {
		return result, nil
	}
	result.Rename = "failed"
	if renamer == nil {
		result.Error = "relay provider does not support group rename"
		return result, nil
	}
	renamed, err := renamer.RenameGroup(ctx, group.ID, assignment.TargetGroupName)
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	if renamed == nil || renamed.ID != group.ID || renamed.Name != assignment.TargetGroupName {
		result.Error = "relay returned an unexpected group after rename"
		return result, nil
	}
	assignment.CurrentTargetGroupName = renamed.Name
	result.Rename = "succeeded"
	return result, nil
}

func (s *Service) Execute(ctx context.Context, req ExecuteRequest) (*ExecutionResult, error) {
	if strings.TrimSpace(req.OperationKey) == "" {
		return nil, fmt.Errorf("operation_key is required")
	}
	plan, err := s.Preview(ctx, req.PreviewRequest)
	if err != nil {
		var existing *ExistingMappingError
		if errors.As(err, &existing) {
			return nil, fmt.Errorf("preview relay plan for execution: %w", err)
		}
		if stale := stalePlanFromPreviewError(req.ExpectedRelationshipFingerprint, err); stale != nil {
			return nil, stale
		}
		return nil, fmt.Errorf("preview relay plan for execution: %w", err)
	}
	if err := validateRelationshipFingerprint(req.ExpectedRelationshipFingerprint, plan); err != nil {
		return nil, fmt.Errorf("validate relay plan relationship fingerprint: %w", err)
	}
	durable, initialMapping, err := s.beginInitialDurableExecution(ctx, plan, req)
	if err != nil {
		return nil, err
	}
	defer durable.interrupt(ctx)
	if err := durable.dispatch(ctx); err != nil {
		return nil, err
	}
	p, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	duplicator, ok := p.(relay.GroupDuplicator)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support group duplication")
	}
	renamer, ok := p.(relay.GroupRenamer)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support group rename")
	}
	statusUpdater, _ := p.(relay.GroupStatusUpdater)
	accountReader, ok := p.(relay.AccountRelationshipReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support account relationship reading")
	}
	availableAccounts, err := accountReader.ListAccountsForPlatform(ctx, plan.Platform)
	if err != nil {
		return nil, fmt.Errorf("list account relationships: %w", err)
	}
	groupResults := make([]GroupResult, 0, plan.GroupCount)
	targetIDs := make(map[int]int64, plan.GroupCount)
	createdIDs := make(map[int]int64, plan.GroupCount)
	desiredAccounts := make(map[string][]AccountIntent, plan.GroupCount)
	accountResults := make([]AccountResult, 0)
	for index := 0; index < plan.GroupCount; index++ {
		assignment := &plan.Assignments[index]
		result, createErr := duplicateAndRenameProposedTarget(ctx, duplicator, renamer, plan.TemplateGroupID, fmt.Sprintf("%s-%d", req.OperationKey, index), nil, assignment, func(checkpoint GroupResult) error {
			return durable.verifyStep(ctx, fmt.Sprintf("target:%d:create", checkpoint.Index), map[string]any{"group_id": checkpoint.ID, "name": checkpoint.CurrentName})
		})
		if createErr != nil {
			return nil, fmt.Errorf("create target %d: %w", index, createErr)
		}
		if result.ID <= 0 {
			groupResults = append(groupResults, result)
			continue
		}
		createdIDs[index] = result.ID
		if result.Rename == "failed" {
			groupResults = append(groupResults, result)
			continue
		}
		desiredAccounts[strconv.FormatInt(result.ID, 10)] = append([]AccountIntent(nil), assignment.DesiredAccounts...)
		accountMapping := Mapping{ProviderID: plan.ProviderID, Platform: plan.Platform, GroupIDs: []int64{result.ID}, AccountManagementInitialized: true, DesiredAccounts: desiredAccounts}
		groupAccountResults, blocked := s.applyDesiredAccountRelationships(ctx, p, accountMapping, nil)
		accountResults = append(accountResults, groupAccountResults...)
		if reason := blocked[result.ID]; reason != "" {
			result.Error = reason
			groupResults = append(groupResults, result)
			continue
		}
		if statusUpdater == nil {
			result.Error = "relay provider does not support target group activation"
			groupResults = append(groupResults, result)
			continue
		}
		if activateErr := statusUpdater.UpdateGroupStatus(ctx, result.ID, "active"); activateErr != nil {
			result.Error = activateErr.Error()
			groupResults = append(groupResults, result)
			continue
		}
		result.Creation = "completed"
		result.Status = "succeeded"
		targetIDs[index] = result.ID
		groupResults = append(groupResults, result)
	}
	assigner, _ := p.(subscriptionAssigner)
	remover, _ := p.(subscriptionRemover)
	binder, _ := p.(relay.APIKeyGroupBinder)
	memberResults := make([]MemberResult, 0, len(plan.Candidates))
	for _, assignment := range plan.Assignments {
		targetID := targetIDs[assignment.Index]
		if targetID <= 0 {
			continue
		}
		for _, userID := range assignment.UserIDs {
			member := MemberResult{UserID: userID, TargetGroupID: targetID, Subscription: "skipped", SourceRemoval: "skipped"}
			candidate := candidateByUserID(plan.Candidates, userID)
			if candidate == nil {
				member.Error = "candidate disappeared from plan"
				memberResults = append(memberResults, member)
				continue
			}
			if !candidate.CanAdd {
				member.Error = "candidate cannot be added to a target group"
				memberResults = append(memberResults, member)
				continue
			}
			if candidate.SourceGroupID > 0 {
				member = executeMemberMigration(ctx, assigner, remover, binder, candidate, targetID, candidate.SourceGroupID, member)
			} else {
				member = executeMemberMigration(ctx, assigner, nil, nil, candidate, targetID, 0, member)
			}
			memberResults = append(memberResults, member)
		}
	}
	if len(req.AdoptRelayUserIDs) > 0 {
		adopted := make(map[int64]struct{}, len(req.AdoptRelayUserIDs))
		for _, relayUserID := range req.AdoptRelayUserIDs {
			if relayUserID > 0 {
				adopted[relayUserID] = struct{}{}
			}
		}
		for _, unmanaged := range plan.UnmanagedMembers {
			if _, requested := adopted[unmanaged.RelayUserID]; !requested {
				continue
			}
			for _, targetID := range unmanaged.TargetGroupIDs {
				member := MemberResult{RelayUserID: unmanaged.RelayUserID, TargetGroupID: targetID, Subscription: "failed", SourceRemoval: "skipped"}
				if assigner == nil {
					member.Error = "relay provider does not support subscription assignment"
				} else if assignErr := assigner.AssignSubscriptionForUser(ctx, unmanaged.RelayUserID, targetID, defaultValidityDays); assignErr != nil && !isAlreadyAssignedError(assignErr) {
					member.Error = assignErr.Error()
				} else {
					member.Subscription = "succeeded"
				}
				memberResults = append(memberResults, member)
			}
		}
	}
	groupIDList := make([]int64, plan.GroupCount)
	for index := 0; index < plan.GroupCount; index++ {
		groupIDList[index] = createdIDs[index]
	}
	state := executionState(req.OperationKey, groupResults, memberResults)
	mergeAccountResultsIntoState(state, accountResults)
	applied := operationStatus(state) == "active"
	var mapping *Mapping
	if applied {
		tx, txErr := s.client.Tx(ctx)
		if txErr != nil {
			err = txErr
		} else {
			mapping, err = saveMappingWithClient(ctx, tx.Client(), plan, groupIDList, state)
			var count int
			if err == nil {
				count, err = tx.Client().RelayGroupMapping.Update().
					Where(relaygroupmapping.IDEQ(mapping.ID), relaygroupmapping.BaselineRevisionEQ(initialMapping.BaselineRevision)).
					AddBaselineRevision(1).
					Save(ctx)
			}
			if err == nil && count != 1 {
				err = fmt.Errorf("Mapping baseline revision changed during execution")
			}
			if err == nil {
				row, loadErr := tx.Client().RelayGroupMapping.Get(ctx, mapping.ID)
				if loadErr != nil {
					err = loadErr
				} else {
					updated := mappingFromEnt(row)
					mapping = &updated
				}
			}
			if err == nil {
				err = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
		}
	} else {
		mapping = initialMapping
	}
	if err != nil {
		return nil, fmt.Errorf("save group mapping: %w", err)
	}
	if err := durable.finish(ctx, applied, map[string]any{"mapping_id": mapping.ID, "status": mapping.Status}); err != nil {
		return nil, fmt.Errorf("finish Relationship Operation: %w", err)
	}
	updatedMapping := *mapping
	currentAccounts, readbackErr := accountReader.ListAccountsForPlatform(ctx, plan.Platform)
	warnings := make([]string, 0, 1)
	if readbackErr != nil {
		currentAccounts = availableAccounts
		warnings = append(warnings, "new Target Account relationships could not be refreshed")
	}
	updatedMapping.AccountPools = accountPools(updatedMapping, currentAccounts)
	updatedMapping.Warnings = accountPoolWarnings(updatedMapping.AccountPools, updatedMapping.AccountManagementInitialized)
	return &ExecutionResult{Plan: plan, Groups: groupResults, Accounts: accountResults, Members: memberResults, Mapping: &updatedMapping, Warnings: warnings}, nil
}

func accountIntentsForGroup(accounts []relay.Account, platform string, groupID int64) []AccountIntent {
	intents := make([]AccountIntent, 0)
	for _, account := range accounts {
		if !strings.EqualFold(strings.TrimSpace(account.Platform), strings.TrimSpace(platform)) {
			continue
		}
		if priority := accountRelationshipPriority(account.GroupRelationships, groupID); priority > 0 {
			intents = append(intents, AccountIntent{AccountID: account.ID, Priority: priority})
		}
	}
	sort.SliceStable(intents, func(i, j int) bool {
		if intents[i].Priority == intents[j].Priority {
			return intents[i].AccountID < intents[j].AccountID
		}
		return intents[i].Priority < intents[j].Priority
	})
	return intents
}

func assignPreviewAccounts(result accountListResult, platform string, templateGroupID int64, mapping *ent.RelayGroupMapping, assignments []Assignment) ([]TargetAccount, error) {
	if result.err != nil {
		return nil, fmt.Errorf("list relay accounts: %w", redactProviderReadError(result.err))
	}
	accounts := result.accounts
	available := make(map[int64]relay.Account, len(accounts))
	for _, account := range accounts {
		if strings.EqualFold(strings.TrimSpace(account.Platform), strings.TrimSpace(platform)) {
			available[account.ID] = account
		}
	}
	templateAccounts := accountIntentsForGroup(accounts, platform, templateGroupID)
	_, templateSelection, err := normalizePreviewAccountIntents(templateAccounts, available, platform)
	if err != nil {
		return nil, fmt.Errorf("Template Group: %w", err)
	}
	var savedAccounts map[string][]AccountIntent
	if mapping != nil {
		savedAccounts = accountIntentsFromStorage(mapping.DesiredAccounts)
	}
	for index := range assignments {
		intents := assignments[index].DesiredAccounts
		if intents == nil {
			switch {
			case assignments[index].TargetGroupID == 0:
				intents = templateAccounts
			case mapping != nil && mapping.AccountManagementInitialized:
				intents = savedAccounts[strconv.FormatInt(assignments[index].TargetGroupID, 10)]
			case mapping != nil && assignments[index].TargetGroupID > 0:
				intents = accountIntentsForGroup(accounts, platform, assignments[index].TargetGroupID)
			default:
				intents = templateAccounts
			}
		}
		normalized, selected, err := normalizePreviewAccountIntents(intents, available, platform)
		if err != nil {
			return nil, fmt.Errorf("target %d: %w", assignments[index].Index+1, err)
		}
		assignments[index].DesiredAccounts = normalized
		assignments[index].Accounts = selected
	}
	return templateSelection, nil
}

func normalizePreviewAccountIntents(intents []AccountIntent, available map[int64]relay.Account, platform string) ([]AccountIntent, []TargetAccount, error) {
	normalized := append([]AccountIntent(nil), intents...)
	sort.SliceStable(normalized, func(i, j int) bool { return normalized[i].Priority < normalized[j].Priority })
	selected := make([]TargetAccount, 0, len(normalized))
	seenAccounts := make(map[int64]struct{}, len(normalized))
	for _, intent := range normalized {
		account, ok := available[intent.AccountID]
		if !ok {
			return nil, nil, fmt.Errorf("account %d is unavailable on platform %s", intent.AccountID, platform)
		}
		if _, duplicate := seenAccounts[intent.AccountID]; duplicate {
			return nil, nil, fmt.Errorf("account %d is duplicated", intent.AccountID)
		}
		if intent.Priority <= 0 {
			return nil, nil, fmt.Errorf("account priority must be positive")
		}
		seenAccounts[intent.AccountID] = struct{}{}
		selected = append(selected, TargetAccount{ID: account.ID, Name: account.Name, Platform: account.Platform, Type: account.Type, Status: account.Status, Schedulable: account.Schedulable, Priority: intent.Priority})
	}
	if normalized == nil {
		normalized = []AccountIntent{}
	}
	return normalized, selected, nil
}

type relationshipSnapshot struct {
	ProviderID      int                              `json:"provider_id"`
	Platform        string                           `json:"platform"`
	Groups          []relationshipGroupFact          `json:"groups"`
	PlannedRenames  []relationshipPlannedRenameFact  `json:"planned_renames"`
	Accounts        []relationshipAccountFact        `json:"accounts"`
	PlannedAccounts []relationshipPlannedAccountFact `json:"planned_accounts"`
	Mappings        []relationshipMappingFact        `json:"mappings"`
	Users           []relationshipUserFact           `json:"users"`
	Renewal         *relationshipRenewalFact         `json:"renewal,omitempty"`
}

type relationshipGroupFact struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
}

type relationshipPlannedRenameFact struct {
	TargetIndex   int    `json:"target_index"`
	TargetGroupID int64  `json:"target_group_id,omitempty"`
	CurrentName   string `json:"current_name,omitempty"`
	DesiredName   string `json:"desired_name"`
}

type relationshipAccountFact struct {
	ID            int64                            `json:"id"`
	Platform      string                           `json:"platform"`
	Relationships []relay.AccountGroupRelationship `json:"relationships"`
}

type relationshipMappingFact struct {
	ID                           int                              `json:"id"`
	ProviderID                   int                              `json:"provider_id"`
	Platform                     string                           `json:"platform"`
	GroupIDs                     []int64                          `json:"group_ids"`
	AccountManagementInitialized bool                             `json:"account_management_initialized"`
	DesiredAccounts              []relationshipDesiredAccountFact `json:"desired_accounts"`
	Members                      []relationshipMappingMemberFact  `json:"members"`
	ReviewedRemovalSources       []relationshipRemovalSourceFact  `json:"reviewed_removal_sources,omitempty"`
	RetryMoves                   []relationshipRetryMoveFact      `json:"retry_moves,omitempty"`
	RetryRemovals                []relationshipRetryRemovalFact   `json:"retry_removals,omitempty"`
}

type relationshipDesiredAccountFact struct {
	TargetGroupID int64 `json:"target_group_id"`
	AccountID     int64 `json:"account_id"`
	Priority      int   `json:"priority"`
}

type relationshipPlannedAccountFact struct {
	TargetIndex   int   `json:"target_index"`
	TargetGroupID int64 `json:"target_group_id,omitempty"`
	AccountID     int64 `json:"account_id"`
	Priority      int   `json:"priority"`
}

type relationshipMappingMemberFact struct {
	UserID        int   `json:"user_id"`
	TargetGroupID int64 `json:"target_group_id"`
	SourceGroupID int64 `json:"source_group_id"`
}

type relationshipRemovalSourceFact struct {
	UserID        int   `json:"user_id"`
	SourceGroupID int64 `json:"source_group_id"`
}

type relationshipRetryMoveFact struct {
	UserID        int   `json:"user_id"`
	FromMappingID int   `json:"from_mapping_id"`
	FromGroupID   int64 `json:"from_group_id"`
}

type relationshipRetryRemovalFact struct {
	UserID            int     `json:"user_id"`
	TargetGroupID     int64   `json:"target_group_id"`
	SourceGroupID     int64   `json:"source_group_id,omitempty"`
	ReviewedAPIKeyIDs []int64 `json:"reviewed_api_key_ids,omitempty"`
	ReviewedAPIKeySet bool    `json:"reviewed_api_key_set,omitempty"`
}

type reviewedAPIKeySelection struct {
	IDs    []int64
	Frozen bool
}

type relationshipUserFact struct {
	LocalUserID   int                            `json:"local_user_id,omitempty"`
	RelayUserID   int64                          `json:"relay_user_id"`
	Subscriptions []relationshipSubscriptionFact `json:"subscriptions"`
	APIKeys       []relationshipAPIKeyFact       `json:"api_keys"`
}

type relationshipSubscriptionFact struct {
	GroupID int64  `json:"group_id"`
	Status  string `json:"status"`
}

func relationshipSubscriptionFromRelay(subscription relay.UserSubscription) relationshipSubscriptionFact {
	return relationshipSubscriptionFact{GroupID: subscription.GroupID, Status: strings.ToLower(strings.TrimSpace(subscription.Status))}
}

type relationshipRenewalFact struct {
	Days    int                             `json:"days"`
	Members []relationshipRenewalMemberFact `json:"members"`
}

type relationshipRenewalMemberFact struct {
	UserID        int                            `json:"user_id"`
	RelayUserID   int64                          `json:"relay_user_id"`
	TargetGroupID int64                          `json:"target_group_id"`
	Status        string                         `json:"status"`
	PlannedAction string                         `json:"planned_action"`
	CurrentExpiry string                         `json:"current_expiry,omitempty"`
	Drift         []relationshipRenewalDriftFact `json:"drift"`
}

type relationshipRenewalDriftFact struct {
	GroupID   int64  `json:"group_id"`
	Status    string `json:"status"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

type relationshipAPIKeyFact struct {
	ID      int64 `json:"id"`
	GroupID int64 `json:"group_id"`
}

func (s *Service) relationshipFingerprint(ctx context.Context, provider relay.Provider, requestFacts *planningRequestFacts, req PreviewRequest, plan *Plan, groups []relay.Group) (string, error) {
	affectedUserIDs := make(map[int]struct{})
	desiredAccountIDs := make(map[int64]struct{})
	plannedAccounts := make([]relationshipPlannedAccountFact, 0)
	plannedRenames := make([]relationshipPlannedRenameFact, 0)
	reviewedAPIKeysByUser := make(map[int]reviewedAPIKeySelection)
	relevantGroupIDs := map[int64]struct{}{plan.TemplateGroupID: {}}
	if plan.SourceGroupID > 0 {
		relevantGroupIDs[plan.SourceGroupID] = struct{}{}
	}
	for _, sourceGroupID := range req.MemberSources {
		if sourceGroupID > 0 {
			relevantGroupIDs[sourceGroupID] = struct{}{}
		}
	}
	for _, assignment := range plan.Assignments {
		if assignment.TargetGroupID > 0 {
			relevantGroupIDs[assignment.TargetGroupID] = struct{}{}
		}
		if assignment.TargetGroupID == 0 || (assignment.RenameSelected && assignment.TargetGroupName != assignment.CurrentTargetGroupName) {
			plannedRenames = append(plannedRenames, relationshipPlannedRenameFact{TargetIndex: assignment.Index, TargetGroupID: assignment.TargetGroupID, CurrentName: assignment.CurrentTargetGroupName, DesiredName: assignment.TargetGroupName})
		}
		for _, userID := range assignment.UserIDs {
			affectedUserIDs[userID] = struct{}{}
		}
		if plan.AccountsReviewed {
			for _, intent := range assignment.DesiredAccounts {
				desiredAccountIDs[intent.AccountID] = struct{}{}
				plannedAccounts = append(plannedAccounts, relationshipPlannedAccountFact{TargetIndex: assignment.Index, TargetGroupID: assignment.TargetGroupID, AccountID: intent.AccountID, Priority: intent.Priority})
			}
		}
	}
	sort.Slice(plannedRenames, func(i, j int) bool {
		return plannedRenames[i].TargetIndex < plannedRenames[j].TargetIndex
	})
	sort.Slice(plannedAccounts, func(i, j int) bool {
		left, right := plannedAccounts[i], plannedAccounts[j]
		return left.TargetIndex < right.TargetIndex || (left.TargetIndex == right.TargetIndex && (left.Priority < right.Priority || (left.Priority == right.Priority && left.AccountID < right.AccountID)))
	})
	for _, userID := range req.RemovedUserIDs {
		affectedUserIDs[userID] = struct{}{}
	}

	mappingIDs := make(map[int]struct{})
	if req.ExistingMappingID > 0 {
		mappingIDs[req.ExistingMappingID] = struct{}{}
	}
	for rawUserID, action := range req.MemberActions {
		userID, err := strconv.Atoi(rawUserID)
		if err != nil || userID <= 0 {
			return "", fmt.Errorf("invalid member action user %q", rawUserID)
		}
		affectedUserIDs[userID] = struct{}{}
		switch action.Mode {
		case "move_here":
			if action.FromMappingID <= 0 {
				return "", fmt.Errorf("move_here for user %d requires a source mapping", userID)
			}
			mappingIDs[action.FromMappingID] = struct{}{}
		case "add_additionally":
		default:
			return "", fmt.Errorf("unsupported member action %q for user %d", action.Mode, userID)
		}
	}

	snapshot := relationshipSnapshot{ProviderID: req.ProviderID, Platform: strings.ToLower(strings.TrimSpace(req.Platform)), PlannedRenames: plannedRenames, PlannedAccounts: plannedAccounts}
	mappingIDList := make([]int, 0, len(mappingIDs))
	for mappingID := range mappingIDs {
		mappingIDList = append(mappingIDList, mappingID)
	}
	sort.Ints(mappingIDList)
	for _, mappingID := range mappingIDList {
		mapping, err := s.client.RelayGroupMapping.Get(ctx, mappingID)
		if err != nil {
			return "", fmt.Errorf("load related mapping %d: %w", mappingID, err)
		}
		if mapping.ProviderID != req.ProviderID || !strings.EqualFold(strings.TrimSpace(mapping.Platform), strings.TrimSpace(req.Platform)) {
			return "", fmt.Errorf("mapping %d does not belong to the selected provider and platform", mappingID)
		}
		fact := relationshipMappingFact{ID: mapping.ID, ProviderID: mapping.ProviderID, Platform: strings.ToLower(strings.TrimSpace(mapping.Platform)), GroupIDs: append([]int64(nil), mapping.GroupIds...), AccountManagementInitialized: mapping.AccountManagementInitialized}
		sort.Slice(fact.GroupIDs, func(i, j int) bool { return fact.GroupIDs[i] < fact.GroupIDs[j] })
		for _, groupID := range mapping.GroupIds {
			if groupID > 0 {
				relevantGroupIDs[groupID] = struct{}{}
			}
			for _, intent := range accountIntentsFromStorage(mapping.DesiredAccounts)[strconv.FormatInt(groupID, 10)] {
				fact.DesiredAccounts = append(fact.DesiredAccounts, relationshipDesiredAccountFact{TargetGroupID: groupID, AccountID: intent.AccountID, Priority: intent.Priority})
			}
		}
		if mapping.SourceGroupID > 0 {
			relevantGroupIDs[mapping.SourceGroupID] = struct{}{}
		}
		for userID := range affectedUserIDs {
			key := strconv.Itoa(userID)
			if targetGroupID := mapping.MemberAssignments[key]; targetGroupID > 0 {
				fact.Members = append(fact.Members, relationshipMappingMemberFact{UserID: userID, TargetGroupID: targetGroupID, SourceGroupID: mapping.MemberSources[key]})
			}
			stateKey := "member:" + key
			entry := mapping.OperationState[stateKey]
			if entry != nil && entry["action"] == "move_here" && operationStateNeedsRetry(mapping.OperationState, stateKey) {
				fromMappingID, mappingErr := strconv.Atoi(entry["from_mapping_id"])
				fromGroupID, groupErr := strconv.ParseInt(entry["from_group_id"], 10, 64)
				if mappingErr == nil && groupErr == nil && fromMappingID > 0 && fromGroupID > 0 {
					fact.RetryMoves = append(fact.RetryMoves, relationshipRetryMoveFact{UserID: userID, FromMappingID: fromMappingID, FromGroupID: fromGroupID})
				}
			}
			if entry != nil && entry["action"] == "remove" && operationStateNeedsRetry(mapping.OperationState, stateKey) {
				targetGroupID, targetErr := strconv.ParseInt(entry["target_group_id"], 10, 64)
				sourceGroupID, _ := strconv.ParseInt(entry["source_group_id"], 10, 64)
				if targetErr == nil && targetGroupID > 0 {
					selection := reviewedAPIKeySelectionFromState(entry)
					if current := reviewedAPIKeysByUser[userID]; current.Frozen || selection.Frozen {
						selection.IDs = mergeAPIKeyIDs(current.IDs, selection.IDs)
						selection.Frozen = current.Frozen || selection.Frozen
					}
					if selection.Frozen {
						reviewedAPIKeysByUser[userID] = selection
					}
					fact.RetryRemovals = append(fact.RetryRemovals, relationshipRetryRemovalFact{UserID: userID, TargetGroupID: targetGroupID, SourceGroupID: sourceGroupID, ReviewedAPIKeyIDs: selection.IDs, ReviewedAPIKeySet: selection.Frozen})
				}
			}
		}
		if mapping.ID == req.ExistingMappingID {
			for _, userID := range req.RemovedUserIDs {
				if sourceGroupID, reviewed := req.MemberSources[strconv.Itoa(userID)]; reviewed {
					fact.ReviewedRemovalSources = append(fact.ReviewedRemovalSources, relationshipRemovalSourceFact{UserID: userID, SourceGroupID: sourceGroupID})
				}
			}
			sort.Slice(fact.ReviewedRemovalSources, func(i, j int) bool {
				return fact.ReviewedRemovalSources[i].UserID < fact.ReviewedRemovalSources[j].UserID
			})
		}
		sort.Slice(fact.DesiredAccounts, func(i, j int) bool {
			left, right := fact.DesiredAccounts[i], fact.DesiredAccounts[j]
			return left.TargetGroupID < right.TargetGroupID || (left.TargetGroupID == right.TargetGroupID && (left.Priority < right.Priority || (left.Priority == right.Priority && left.AccountID < right.AccountID)))
		})
		sort.Slice(fact.Members, func(i, j int) bool { return fact.Members[i].UserID < fact.Members[j].UserID })
		sort.Slice(fact.RetryMoves, func(i, j int) bool { return fact.RetryMoves[i].UserID < fact.RetryMoves[j].UserID })
		sort.Slice(fact.RetryRemovals, func(i, j int) bool { return fact.RetryRemovals[i].UserID < fact.RetryRemovals[j].UserID })
		snapshot.Mappings = append(snapshot.Mappings, fact)
	}
	for rawUserID, action := range req.MemberActions {
		if action.Mode != "move_here" {
			continue
		}
		userID, _ := strconv.Atoi(rawUserID)
		found := false
		for index := range snapshot.Mappings {
			if snapshot.Mappings[index].ID == action.FromMappingID && mappingMemberGroup(&snapshot.Mappings[index], userID) > 0 {
				found = true
				break
			}
		}
		if !found && mappingRetryMoveGroup(snapshot.Mappings, req.ExistingMappingID, userID, action.FromMappingID) <= 0 {
			return "", fmt.Errorf("user %d is not managed by source mapping %d", userID, action.FromMappingID)
		}
	}

	for _, group := range groups {
		if _, relevant := relevantGroupIDs[group.ID]; !relevant {
			continue
		}
		snapshot.Groups = append(snapshot.Groups, relationshipGroupFact{ID: group.ID, Name: strings.TrimSpace(group.Name), Platform: strings.ToLower(strings.TrimSpace(group.Platform))})
	}
	sort.Slice(snapshot.Groups, func(i, j int) bool { return snapshot.Groups[i].ID < snapshot.Groups[j].ID })

	if requestFacts.accounts.err != nil {
		return "", fmt.Errorf("list account relationships: %w", redactProviderReadError(requestFacts.accounts.err))
	}
	accounts := requestFacts.accounts.accounts
	for _, mapping := range snapshot.Mappings {
		for _, desired := range mapping.DesiredAccounts {
			desiredAccountIDs[desired.AccountID] = struct{}{}
		}
	}
	for _, account := range accounts {
		fact := relationshipAccountFact{ID: account.ID, Platform: strings.ToLower(strings.TrimSpace(account.Platform))}
		for _, relationship := range account.GroupRelationships {
			if _, relevant := relevantGroupIDs[relationship.GroupID]; relevant {
				fact.Relationships = append(fact.Relationships, relationship)
			}
		}
		if len(fact.Relationships) == 0 {
			if _, desired := desiredAccountIDs[account.ID]; !desired {
				continue
			}
		}
		sort.Slice(fact.Relationships, func(i, j int) bool {
			return fact.Relationships[i].GroupID < fact.Relationships[j].GroupID || (fact.Relationships[i].GroupID == fact.Relationships[j].GroupID && fact.Relationships[i].Priority < fact.Relationships[j].Priority)
		})
		snapshot.Accounts = append(snapshot.Accounts, fact)
	}
	sort.Slice(snapshot.Accounts, func(i, j int) bool { return snapshot.Accounts[i].ID < snapshot.Accounts[j].ID })

	localUserIDs := make([]int, 0, len(affectedUserIDs))
	for userID := range affectedUserIDs {
		localUserIDs = append(localUserIDs, userID)
	}
	sort.Ints(localUserIDs)
	localUsers := make(map[int]*ent.User, len(localUserIDs))
	if len(localUserIDs) > 0 {
		items, err := s.client.User.Query().Where(user.IDIn(localUserIDs...)).All(ctx)
		if err != nil {
			return "", fmt.Errorf("load affected relay user bindings: %w", err)
		}
		for _, item := range items {
			localUsers[item.ID] = item
		}
	}
	userFacts := make([]relationshipUserFact, 0, len(localUserIDs)+len(req.AdoptRelayUserIDs))
	for _, localUserID := range localUserIDs {
		local := localUsers[localUserID]
		relayUserID := int64(0)
		if local != nil && local.RelayUserID != nil {
			relayUserID = int64(*local.RelayUserID)
		}
		userFacts = append(userFacts, relationshipUserFact{LocalUserID: localUserID, RelayUserID: relayUserID})
	}
	for _, relayUserID := range req.AdoptRelayUserIDs {
		if relayUserID > 0 {
			userFacts = append(userFacts, relationshipUserFact{RelayUserID: relayUserID})
		}
	}
	candidatesByUserID := make(map[int]Candidate, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		candidatesByUserID[candidate.UserID] = candidate
	}
	subscriptionLister, supportsSubscriptions := provider.(relay.UserSubscriptionLister)
	for index := range userFacts {
		if userFacts[index].RelayUserID <= 0 {
			continue
		}
		candidate, reusable := candidatesByUserID[userFacts[index].LocalUserID]
		reviewedAPIKeys := reviewedAPIKeysByUser[userFacts[index].LocalUserID]
		if reusable && candidate.RelayUserID == userFacts[index].RelayUserID {
			if candidate.relationshipGroupErr != nil {
				return "", fmt.Errorf("subscription relationships are unavailable for relay user %d: %w", userFacts[index].RelayUserID, redactProviderReadError(candidate.relationshipGroupErr))
			}
			if candidate.relationshipKeyErr != nil {
				return "", fmt.Errorf("API Key relationships are unavailable for relay user %d: %w", userFacts[index].RelayUserID, redactProviderReadError(candidate.relationshipKeyErr))
			}
			for _, subscription := range candidate.relationshipSubscriptions {
				if _, relevant := relevantGroupIDs[subscription.GroupID]; relevant {
					userFacts[index].Subscriptions = append(userFacts[index].Subscriptions, subscription)
				}
			}
			for _, key := range candidate.relationshipAPIKeys {
				_, relevantGroup := relevantGroupIDs[key.GroupID]
				if (reviewedAPIKeys.Frozen && slices.Contains(reviewedAPIKeys.IDs, key.ID)) || (!reviewedAPIKeys.Frozen && relevantGroup) {
					userFacts[index].APIKeys = append(userFacts[index].APIKeys, key)
				}
			}
		} else {
			var subscriptions []relay.UserSubscription
			if requestFacts.relationships != nil {
				relationship, found := requestFacts.relationships.byUserID[userFacts[index].RelayUserID]
				if !found {
					return "", fmt.Errorf("relay user %d is unavailable in the relationship snapshot", userFacts[index].RelayUserID)
				}
				subscriptions = relationship.Subscriptions
			} else {
				if !supportsSubscriptions {
					return "", fmt.Errorf("relay provider does not support subscription relationship reading")
				}
				var err error
				subscriptions, err = subscriptionLister.ListUserSubscriptions(ctx, userFacts[index].RelayUserID)
				if err != nil {
					return "", fmt.Errorf("subscription relationships are unavailable for relay user %d: %w", userFacts[index].RelayUserID, redactProviderReadError(err))
				}
			}
			for _, subscription := range subscriptions {
				groupID := subscription.GroupID
				if groupID <= 0 && subscription.Group != nil {
					groupID = subscription.Group.ID
				}
				if _, relevant := relevantGroupIDs[groupID]; relevant {
					subscription.GroupID = groupID
					userFacts[index].Subscriptions = append(userFacts[index].Subscriptions, relationshipSubscriptionFromRelay(subscription))
				}
			}
			keys, err := requestFacts.activeUserAPIKeys(ctx, provider, userFacts[index].RelayUserID)
			if err != nil {
				return "", fmt.Errorf("API Key relationships are unavailable for relay user %d: %w", userFacts[index].RelayUserID, redactProviderReadError(err))
			}
			for _, key := range keys {
				groupID := apiKeyGroupID(key)
				_, relevantGroup := relevantGroupIDs[groupID]
				if (reviewedAPIKeys.Frozen && slices.Contains(reviewedAPIKeys.IDs, key.ID)) || (!reviewedAPIKeys.Frozen && relevantGroup) {
					userFacts[index].APIKeys = append(userFacts[index].APIKeys, relationshipAPIKeyFact{ID: key.ID, GroupID: groupID})
				}
			}
		}
		if len(reviewedAPIKeys.IDs) > 0 {
			keys, err := requestFacts.userAPIKeys(ctx, provider, userFacts[index].RelayUserID)
			if err != nil {
				return "", fmt.Errorf("API Key relationships are unavailable for relay user %d: %w", userFacts[index].RelayUserID, redactProviderReadError(err))
			}
			indexes := make(map[int64]int, len(userFacts[index].APIKeys))
			for keyIndex, key := range userFacts[index].APIKeys {
				indexes[key.ID] = keyIndex
			}
			for _, key := range keys {
				if !slices.Contains(reviewedAPIKeys.IDs, key.ID) {
					continue
				}
				fact := relationshipAPIKeyFact{ID: key.ID, GroupID: apiKeyGroupID(key)}
				if keyIndex, exists := indexes[key.ID]; exists {
					userFacts[index].APIKeys[keyIndex] = fact
				} else {
					indexes[key.ID] = len(userFacts[index].APIKeys)
					userFacts[index].APIKeys = append(userFacts[index].APIKeys, fact)
				}
			}
		}
		sort.Slice(userFacts[index].Subscriptions, func(i, j int) bool {
			left, right := userFacts[index].Subscriptions[i], userFacts[index].Subscriptions[j]
			return left.GroupID < right.GroupID || (left.GroupID == right.GroupID && left.Status < right.Status)
		})
		sort.Slice(userFacts[index].APIKeys, func(i, j int) bool {
			left, right := userFacts[index].APIKeys[i], userFacts[index].APIKeys[j]
			return left.ID < right.ID || (left.ID == right.ID && left.GroupID < right.GroupID)
		})
	}
	sort.Slice(userFacts, func(i, j int) bool {
		if userFacts[i].LocalUserID == userFacts[j].LocalUserID {
			return userFacts[i].RelayUserID < userFacts[j].RelayUserID
		}
		return userFacts[i].LocalUserID < userFacts[j].LocalUserID
	})
	snapshot.Users = userFacts
	plan.relationshipSnapshot = snapshot

	return encodeRelationshipFingerprint(snapshot)
}

func encodeRelationshipFingerprint(snapshot relationshipSnapshot) (string, error) {
	identities := make([]struct {
		LocalUserID int   `json:"local_user_id,omitempty"`
		RelayUserID int64 `json:"relay_user_id"`
	}, len(snapshot.Users))
	subscriptions := make([]struct {
		LocalUserID   int                            `json:"local_user_id,omitempty"`
		RelayUserID   int64                          `json:"relay_user_id"`
		Subscriptions []relationshipSubscriptionFact `json:"subscriptions"`
		Renewal       *relationshipRenewalFact       `json:"renewal,omitempty"`
	}, len(snapshot.Users))
	apiKeys := make([]struct {
		LocalUserID int                      `json:"local_user_id,omitempty"`
		RelayUserID int64                    `json:"relay_user_id"`
		APIKeys     []relationshipAPIKeyFact `json:"api_keys"`
	}, len(snapshot.Users))
	for index, user := range snapshot.Users {
		identities[index].LocalUserID, identities[index].RelayUserID = user.LocalUserID, user.RelayUserID
		subscriptions[index].LocalUserID, subscriptions[index].RelayUserID, subscriptions[index].Subscriptions = user.LocalUserID, user.RelayUserID, user.Subscriptions
		apiKeys[index].LocalUserID, apiKeys[index].RelayUserID, apiKeys[index].APIKeys = user.LocalUserID, user.RelayUserID, user.APIKeys
	}
	if len(subscriptions) > 0 {
		subscriptions[0].Renewal = snapshot.Renewal
	} else if snapshot.Renewal != nil {
		subscriptions = append(subscriptions, struct {
			LocalUserID   int                            `json:"local_user_id,omitempty"`
			RelayUserID   int64                          `json:"relay_user_id"`
			Subscriptions []relationshipSubscriptionFact `json:"subscriptions"`
			Renewal       *relationshipRenewalFact       `json:"renewal,omitempty"`
		}{Renewal: snapshot.Renewal})
	}
	parts := []any{
		struct {
			ProviderID     int                             `json:"provider_id"`
			Platform       string                          `json:"platform"`
			Groups         []relationshipGroupFact         `json:"groups"`
			PlannedRenames []relationshipPlannedRenameFact `json:"planned_renames"`
		}{snapshot.ProviderID, snapshot.Platform, snapshot.Groups, snapshot.PlannedRenames},
		struct {
			Accounts        []relationshipAccountFact        `json:"accounts"`
			PlannedAccounts []relationshipPlannedAccountFact `json:"planned_accounts"`
		}{snapshot.Accounts, snapshot.PlannedAccounts},
		snapshot.Mappings,
		identities,
		subscriptions,
		apiKeys,
	}
	hashes := make([]string, len(parts))
	for index, part := range parts {
		encoded, err := json.Marshal(part)
		if err != nil {
			return "", fmt.Errorf("encode relationship fingerprint part %d: %w", index, err)
		}
		sum := sha256.Sum256(encoded)
		hashes[index] = hex.EncodeToString(sum[:])
	}
	return "v2:" + strings.Join(hashes, ":"), nil
}

func validateRelationshipFingerprint(expected string, plan *Plan) error {
	expected = strings.TrimSpace(expected)
	if expected == "" {
		return fmt.Errorf("expected_relationship_fingerprint is required")
	}
	if plan == nil || expected == plan.RelationshipFingerprint {
		return nil
	}
	differences := relationshipFingerprintDifferences(expected, plan.RelationshipFingerprint)
	if len(differences) == 0 {
		differences = []string{"Relay relationships changed after Preview; review the refreshed plan"}
	}
	return &StalePlanError{
		ExpectedFingerprint: expected,
		CurrentFingerprint:  plan.RelationshipFingerprint,
		RefreshedPlan:       plan,
		Differences:         differences,
	}
}

func relationshipFingerprintDifferences(expected, current string) []string {
	expectedParts := strings.Split(expected, ":")
	currentParts := strings.Split(current, ":")
	if len(expectedParts) != 7 || len(currentParts) != 7 || expectedParts[0] != "v2" || currentParts[0] != "v2" {
		return nil
	}
	labels := []string{
		"Group relationships changed",
		"Account relationships changed",
		"mapping relationships changed",
		"Relay user mappings changed",
		"subscription relationships changed",
		"API Key relationships changed",
	}
	differences := make([]string, 0, len(labels))
	for index, label := range labels {
		if len(expectedParts[index+1]) != sha256.Size*2 || len(currentParts[index+1]) != sha256.Size*2 {
			return nil
		}
		if expectedParts[index+1] != currentParts[index+1] {
			differences = append(differences, label)
		}
	}
	return differences
}

func stalePlanFromPreviewError(expected string, previewErr error) *StalePlanError {
	expected = strings.TrimSpace(expected)
	if expected == "" || previewErr == nil {
		return nil
	}
	difference := "reviewed Relay relationship facts changed or are no longer available"
	var rosterErr *replanRosterMemberError
	var candidateErr *assignmentCandidateError
	if errors.As(previewErr, &rosterErr) {
		difference = replanRosterUnavailableDifference(rosterErr.Reason)
	} else if errors.As(previewErr, &candidateErr) {
		difference = candidateErr.Difference
	} else {
		message := strings.ToLower(previewErr.Error())
		switch {
		case strings.Contains(message, "migration source") || strings.Contains(message, "source group"):
			difference = "migration source Group changed or is no longer available"
		case strings.Contains(message, "template group"):
			difference = "Template Group changed or is no longer available"
		case strings.Contains(message, "target group"):
			difference = "a Target Group changed or is no longer available"
		case strings.Contains(message, "user relationships") || strings.Contains(message, "subscription"):
			difference = "subscription relationships changed or are no longer available"
		case strings.Contains(message, "account"):
			difference = "Account relationships changed or are no longer available"
		}
	}
	return &StalePlanError{ExpectedFingerprint: expected, Differences: []string{difference}}
}

func buildTargetChangeSummaries(req PreviewRequest, plan *Plan) []TargetChangeSummary {
	if plan == nil {
		return nil
	}
	snapshot := plan.relationshipSnapshot
	summaries := make([]TargetChangeSummary, len(plan.Assignments))
	for index, assignment := range plan.Assignments {
		summaries[index] = TargetChangeSummary{
			Index: assignment.Index, TargetGroupID: assignment.TargetGroupID, TargetGroupName: assignment.TargetGroupName,
			Accounts: []AccountChange{}, Members: []MemberChange{}, Subscriptions: []SubscriptionChange{}, APIKeys: []APIKeyChange{},
		}
		if assignment.TargetGroupID > 0 && assignment.RenameSelected && assignment.TargetGroupName != assignment.CurrentTargetGroupName {
			summaries[index].Rename = &GroupRenameChange{FromName: assignment.CurrentTargetGroupName, ToName: assignment.TargetGroupName}
		}
	}
	var currentMapping *relationshipMappingFact
	for index := range snapshot.Mappings {
		if snapshot.Mappings[index].ID == req.ExistingMappingID {
			currentMapping = &snapshot.Mappings[index]
			break
		}
	}
	userFacts := make(map[int]relationshipUserFact)
	for _, fact := range snapshot.Users {
		if fact.LocalUserID > 0 {
			userFacts[fact.LocalUserID] = fact
		}
	}
	candidates := make(map[int]Candidate, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		candidates[candidate.UserID] = candidate
	}

	for index := range summaries {
		if currentMapping != nil && !plan.AccountsReviewed {
			continue
		}
		current := make(map[int64]int)
		desired := make(map[int64]int)
		targetGroupID := summaries[index].TargetGroupID
		for _, account := range snapshot.Accounts {
			for _, relationship := range account.Relationships {
				if relationship.GroupID == targetGroupID && targetGroupID > 0 {
					current[account.ID] = relationship.Priority
				}
			}
		}
		for _, intent := range plan.Assignments[index].DesiredAccounts {
			desired[intent.AccountID] = intent.Priority
		}
		accountIDs := make(map[int64]struct{}, len(current)+len(desired))
		for accountID := range current {
			accountIDs[accountID] = struct{}{}
		}
		for accountID := range desired {
			accountIDs[accountID] = struct{}{}
		}
		orderedAccountIDs := make([]int64, 0, len(accountIDs))
		for accountID := range accountIDs {
			orderedAccountIDs = append(orderedAccountIDs, accountID)
		}
		sort.Slice(orderedAccountIDs, func(i, j int) bool { return orderedAccountIDs[i] < orderedAccountIDs[j] })
		for _, accountID := range orderedAccountIDs {
			oldPriority, currentExists := current[accountID]
			newPriority, desiredExists := desired[accountID]
			switch {
			case !currentExists && desiredExists:
				summaries[index].Accounts = append(summaries[index].Accounts, AccountChange{AccountID: accountID, Action: "add", NewPriority: newPriority})
			case currentExists && !desiredExists:
				summaries[index].Accounts = append(summaries[index].Accounts, AccountChange{AccountID: accountID, Action: "remove", OldPriority: oldPriority})
			case oldPriority != newPriority:
				summaries[index].Accounts = append(summaries[index].Accounts, AccountChange{AccountID: accountID, Action: "reorder", OldPriority: oldPriority, NewPriority: newPriority})
			}
		}
	}

	for index, assignment := range plan.Assignments {
		summary := &summaries[index]
		for _, userID := range assignment.UserIDs {
			fact := userFacts[userID]
			currentGroupID := mappingMemberGroup(currentMapping, userID)
			action := req.MemberActions[strconv.Itoa(userID)]
			if currentGroupID == assignment.TargetGroupID && currentGroupID > 0 && action.Mode != "move_here" {
				continue
			}
			fromGroupID := int64(0)
			switch action.Mode {
			case "add_additionally":
				fromGroupID = 0
			case "move_here":
				for mappingIndex := range snapshot.Mappings {
					if snapshot.Mappings[mappingIndex].ID == action.FromMappingID {
						fromGroupID = mappingMemberGroup(&snapshot.Mappings[mappingIndex], userID)
						break
					}
				}
				if fromGroupID <= 0 {
					fromGroupID = mappingRetryMoveGroup(snapshot.Mappings, req.ExistingMappingID, userID, action.FromMappingID)
				}
			default:
				if currentGroupID > 0 {
					fromGroupID = currentGroupID
				} else {
					fromGroupID = candidates[userID].SourceGroupID
				}
			}
			memberAction := "add"
			if fromGroupID > 0 && fromGroupID != assignment.TargetGroupID {
				memberAction = "move"
			}
			summary.Members = append(summary.Members, MemberChange{UserID: userID, RelayUserID: fact.RelayUserID, Action: memberAction, FromGroupID: fromGroupID, ToGroupID: assignment.TargetGroupID})
			if !hasActiveSubscription(fact, assignment.TargetGroupID) {
				summary.Subscriptions = append(summary.Subscriptions, SubscriptionChange{UserID: userID, RelayUserID: fact.RelayUserID, Action: "add", GroupID: assignment.TargetGroupID})
			}
			if memberAction == "move" {
				if hasActiveSubscription(fact, fromGroupID) {
					summary.Subscriptions = append(summary.Subscriptions, SubscriptionChange{UserID: userID, RelayUserID: fact.RelayUserID, Action: "remove", GroupID: fromGroupID})
				}
				if count := relationshipAPIKeyCount(fact, fromGroupID); count > 0 {
					summary.APIKeys = append(summary.APIKeys, APIKeyChange{UserID: userID, RelayUserID: fact.RelayUserID, Action: "move", Count: count, FromGroupID: fromGroupID, ToGroupID: assignment.TargetGroupID})
				}
			}
		}
	}

	if currentMapping != nil {
		for _, userID := range req.RemovedUserIDs {
			key := strconv.Itoa(userID)
			targetGroupID := mappingMemberGroup(currentMapping, userID)
			unreviewedSource := req.allowUnreviewedRemovalSources[userID]
			sourceGroupID, sourceReviewed := req.MemberSources[key]
			if !sourceReviewed {
				sourceGroupID = mappingMemberSource(currentMapping, userID)
			}
			if targetGroupID <= 0 {
				var retrySourceGroupID int64
				targetGroupID, retrySourceGroupID = mappingRetryRemoval(currentMapping, userID)
				if !sourceReviewed {
					sourceGroupID = retrySourceGroupID
				}
			}
			summary := targetSummaryForGroup(summaries, targetGroupID)
			if summary == nil {
				continue
			}
			fact := userFacts[userID]
			summary.Members = append(summary.Members, MemberChange{UserID: userID, RelayUserID: fact.RelayUserID, Action: "remove", FromGroupID: targetGroupID, ToGroupID: sourceGroupID})
			if unreviewedSource {
				continue
			}
			if sourceGroupID > 0 && !hasActiveSubscription(fact, sourceGroupID) {
				summary.Subscriptions = append(summary.Subscriptions, SubscriptionChange{UserID: userID, RelayUserID: fact.RelayUserID, Action: "add", GroupID: sourceGroupID})
			}
			if hasActiveSubscription(fact, targetGroupID) {
				summary.Subscriptions = append(summary.Subscriptions, SubscriptionChange{UserID: userID, RelayUserID: fact.RelayUserID, Action: "remove", GroupID: targetGroupID})
			}
			if sourceGroupID > 0 {
				reviewedAPIKeyIDs, reviewedAPIKeySet := mappingRetryRemovalAPIKeySet(currentMapping, userID)
				count := relationshipAPIKeyCount(fact, targetGroupID)
				if reviewedAPIKeySet {
					count = relationshipAPIKeyCountForIDs(fact, targetGroupID, reviewedAPIKeyIDs)
				}
				if count > 0 {
					summary.APIKeys = append(summary.APIKeys, APIKeyChange{UserID: userID, RelayUserID: fact.RelayUserID, Action: "move", Count: count, FromGroupID: targetGroupID, ToGroupID: sourceGroupID})
				}
			}
		}
	}

	adopted := make(map[int64]struct{}, len(req.AdoptRelayUserIDs))
	for _, relayUserID := range req.AdoptRelayUserIDs {
		adopted[relayUserID] = struct{}{}
	}
	for _, unmanaged := range plan.UnmanagedMembers {
		if _, ok := adopted[unmanaged.RelayUserID]; !ok {
			continue
		}
		for _, targetGroupID := range unmanaged.TargetGroupIDs {
			if summary := targetSummaryForGroup(summaries, targetGroupID); summary != nil {
				summary.Members = append(summary.Members, MemberChange{RelayUserID: unmanaged.RelayUserID, Action: "add", ToGroupID: targetGroupID})
			}
		}
	}
	for index := range summaries {
		sortTargetChangeSummary(&summaries[index])
	}
	return summaries
}

func mappingMemberGroup(mapping *relationshipMappingFact, userID int) int64 {
	if mapping == nil {
		return 0
	}
	for _, member := range mapping.Members {
		if member.UserID == userID {
			return member.TargetGroupID
		}
	}
	return 0
}

func mappingRetryMoveGroup(mappings []relationshipMappingFact, mappingID, userID, fromMappingID int) int64 {
	for _, mapping := range mappings {
		if mapping.ID != mappingID {
			continue
		}
		for _, retry := range mapping.RetryMoves {
			if retry.UserID == userID && retry.FromMappingID == fromMappingID {
				return retry.FromGroupID
			}
		}
	}
	return 0
}

func mappingRetryRemoval(mapping *relationshipMappingFact, userID int) (int64, int64) {
	if mapping == nil {
		return 0, 0
	}
	for _, retry := range mapping.RetryRemovals {
		if retry.UserID == userID {
			return retry.TargetGroupID, retry.SourceGroupID
		}
	}
	return 0, 0
}

func mappingRetryRemovalAPIKeySet(mapping *relationshipMappingFact, userID int) ([]int64, bool) {
	if mapping == nil {
		return nil, false
	}
	for _, retry := range mapping.RetryRemovals {
		if retry.UserID == userID {
			return retry.ReviewedAPIKeyIDs, retry.ReviewedAPIKeySet
		}
	}
	return nil, false
}

func mappingMemberSource(mapping *relationshipMappingFact, userID int) int64 {
	if mapping == nil {
		return 0
	}
	for _, member := range mapping.Members {
		if member.UserID == userID {
			return member.SourceGroupID
		}
	}
	return 0
}

func hasActiveSubscription(user relationshipUserFact, groupID int64) bool {
	if groupID <= 0 {
		return false
	}
	for _, subscription := range user.Subscriptions {
		if subscription.GroupID == groupID && subscription.Status == "active" {
			return true
		}
	}
	return false
}

func relationshipAPIKeyCount(user relationshipUserFact, groupID int64) int {
	count := 0
	for _, key := range user.APIKeys {
		if key.GroupID == groupID {
			count++
		}
	}
	return count
}

func relationshipAPIKeyCountForIDs(user relationshipUserFact, groupID int64, keyIDs []int64) int {
	count := 0
	for _, key := range user.APIKeys {
		if key.GroupID == groupID && slices.Contains(keyIDs, key.ID) {
			count++
		}
	}
	return count
}

func targetSummaryForGroup(summaries []TargetChangeSummary, groupID int64) *TargetChangeSummary {
	for index := range summaries {
		if summaries[index].TargetGroupID == groupID {
			return &summaries[index]
		}
	}
	return nil
}

func sortTargetChangeSummary(summary *TargetChangeSummary) {
	sort.Slice(summary.Members, func(i, j int) bool {
		if summary.Members[i].UserID == summary.Members[j].UserID {
			return summary.Members[i].RelayUserID < summary.Members[j].RelayUserID
		}
		return summary.Members[i].UserID < summary.Members[j].UserID
	})
	sort.Slice(summary.Subscriptions, func(i, j int) bool {
		left, right := summary.Subscriptions[i], summary.Subscriptions[j]
		return left.RelayUserID < right.RelayUserID || (left.RelayUserID == right.RelayUserID && (left.GroupID < right.GroupID || (left.GroupID == right.GroupID && left.Action < right.Action)))
	})
	sort.Slice(summary.APIKeys, func(i, j int) bool {
		left, right := summary.APIKeys[i], summary.APIKeys[j]
		return left.RelayUserID < right.RelayUserID || (left.RelayUserID == right.RelayUserID && left.FromGroupID < right.FromGroupID)
	})
}

func (s *Service) ListMappings(ctx context.Context, providerID int) ([]Mapping, error) {
	query := s.client.RelayGroupMapping.Query().Order(ent.Asc(relaygroupmapping.FieldID))
	if providerID > 0 {
		query = query.Where(relaygroupmapping.ProviderIDEQ(providerID))
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay group mappings: %w", err)
	}
	if len(rows) == 0 {
		return []Mapping{}, nil
	}
	providerFacts := make(map[int]*mappingProviderFacts)
	platforms := make(map[int]map[string]string)
	for _, row := range rows {
		if platforms[row.ProviderID] == nil {
			platforms[row.ProviderID] = make(map[string]string)
		}
		platforms[row.ProviderID][strings.ToLower(strings.TrimSpace(row.Platform))] = row.Platform
		if providerFacts[row.ProviderID] != nil {
			continue
		}
		facts := &mappingProviderFacts{accounts: make(map[string]*accountListResult)}
		if s.resolver != nil {
			facts.provider, _ = s.resolver.Resolve(ctx, row.ProviderID)
		}
		providerFacts[row.ProviderID] = facts
	}

	var directoryFacts *mappingDirectoryFacts
	var dependencies sync.WaitGroup
	dependencies.Add(1)
	go func() {
		defer dependencies.Done()
		directoryFacts, _ = s.loadMappingDirectoryFacts(ctx, rows)
	}()
	for currentProviderID, facts := range providerFacts {
		if facts.provider == nil {
			continue
		}
		if lister, ok := facts.provider.(relay.PlatformGroupLister); ok {
			dependencies.Add(1)
			go func(facts *mappingProviderFacts) {
				defer dependencies.Done()
				facts.groups, _ = lister.ListPlatformGroups(ctx)
			}(facts)
		}
		dependencies.Add(1)
		go func(facts *mappingProviderFacts) {
			defer dependencies.Done()
			facts.relationships, _ = loadMappingRelationshipFacts(ctx, s.client, facts.provider)
		}(facts)
		reader, ok := facts.provider.(relay.AccountRelationshipReader)
		if !ok {
			continue
		}
		for normalizedPlatform, platform := range platforms[currentProviderID] {
			result := &accountListResult{}
			facts.accounts[normalizedPlatform] = result
			dependencies.Add(1)
			go func(result *accountListResult, platform string) {
				defer dependencies.Done()
				result.accounts, result.err = reader.ListAccountsForPlatform(ctx, platform)
			}(result, platform)
		}
	}
	dependencies.Wait()

	out := make([]Mapping, 0, len(rows))
	for _, row := range rows {
		mapping := mappingFromEnt(row)
		facts := providerFacts[mapping.ProviderID]
		mapping.Warnings = append(mapping.Warnings, mappingAvailabilityWarnings(mapping, facts.groups)...)
		mapping.Warnings = append(mapping.Warnings, mappingRelationshipWarnings(facts.relationships, mapping)...)
		if result := facts.accounts[strings.ToLower(strings.TrimSpace(mapping.Platform))]; result != nil {
			if result.err == nil {
				mapping.AccountPools = accountPools(mapping, result.accounts)
				mapping.Warnings = append(mapping.Warnings, accountPoolWarnings(mapping.AccountPools, mapping.AccountManagementInitialized)...)
				for _, pool := range mapping.AccountPools {
					if pool.Drift {
						mapping.Warnings = append(mapping.Warnings, fmt.Sprintf("target group %d account relationships drifted", pool.TargetGroupID))
					}
				}
			} else {
				mapping.Warnings = append(mapping.Warnings, fmt.Sprintf("account relationships are unavailable: %v", result.err))
			}
		}
		if directoryFacts == nil || !directoryFacts.available[mapping.DepartmentID] {
			mapping.Warnings = append(mapping.Warnings, fmt.Sprintf("department %s is unavailable", mapping.DepartmentID))
			mapping.DepartmentSuggestions = directoryFacts.suggestions(rows, mapping)
		}
		if len(mapping.GroupIDs) == 0 {
			mapping.Warnings = append(mapping.Warnings, "mapping has no target groups")
		}
		for _, groupID := range mapping.GroupIDs {
			if groupID <= 0 {
				mapping.Warnings = append(mapping.Warnings, "mapping contains an invalid target group")
				break
			}
		}
		out = append(out, mapping)
	}
	for index := range out {
		for otherIndex := index + 1; otherIndex < len(out); otherIndex++ {
			if out[index].ProviderID != out[otherIndex].ProviderID || out[index].Platform != out[otherIndex].Platform {
				continue
			}
			for userID := range out[index].MemberAssignments {
				if _, exists := out[otherIndex].MemberAssignments[userID]; !exists {
					continue
				}
				warning := fmt.Sprintf("user %s is assigned in multiple mappings", userID)
				out[index].Warnings = append(out[index].Warnings, warning)
				out[otherIndex].Warnings = append(out[otherIndex].Warnings, warning)
			}
		}
	}
	for index := range out {
		out[index].Warnings = uniqueStrings(out[index].Warnings)
	}
	return out, nil
}

func (s *Service) loadMappingDirectoryFacts(ctx context.Context, rows []*ent.RelayGroupMapping) (*mappingDirectoryFacts, error) {
	facts := &mappingDirectoryFacts{available: make(map[string]bool)}
	snapshot, found, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil {
		return facts, fmt.Errorf("load current Directory snapshot for mappings: %w", err)
	}
	if !found {
		return facts, nil
	}
	externalIDs := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		externalID := strings.TrimSpace(row.DepartmentExternalID)
		if externalID == "" {
			continue
		}
		if _, exists := seen[externalID]; exists {
			continue
		}
		seen[externalID] = struct{}{}
		externalIDs = append(externalIDs, externalID)
	}
	if len(externalIDs) > 0 {
		departments, queryErr := s.client.DirectoryDepartment.Query().Where(
			directorydepartment.SourceIDEQ(snapshot.SourceID),
			directorydepartment.ExternalIDIn(externalIDs...),
		).All(ctx)
		if queryErr != nil {
			return facts, fmt.Errorf("load mapped departments: %w", queryErr)
		}
		for _, department := range departments {
			facts.available[department.ExternalID] = true
		}
	}
	if len(facts.available) == len(externalIDs) {
		return facts, nil
	}
	departments, err := s.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(snapshot.SourceID)).Order(ent.Asc(directorydepartment.FieldName)).Limit(50).All(ctx)
	if err != nil {
		return facts, fmt.Errorf("load Directory department suggestions: %w", err)
	}
	facts.departments = make([]DepartmentSuggestion, 0, len(departments))
	for _, department := range departments {
		facts.departments = append(facts.departments, DepartmentSuggestion{ID: department.ExternalID, Name: department.Name})
	}
	return facts, nil
}

func (facts *mappingDirectoryFacts) suggestions(rows []*ent.RelayGroupMapping, mapping Mapping) []DepartmentSuggestion {
	if facts == nil {
		return nil
	}
	bound := make(map[string]struct{})
	for _, row := range rows {
		if row.ProviderID == mapping.ProviderID && strings.EqualFold(strings.TrimSpace(row.Platform), strings.TrimSpace(mapping.Platform)) {
			bound[row.DepartmentExternalID] = struct{}{}
		}
	}
	out := make([]DepartmentSuggestion, 0, len(facts.departments))
	for _, department := range facts.departments {
		if department.ID == mapping.DepartmentID {
			continue
		}
		if _, exists := bound[department.ID]; exists {
			continue
		}
		out = append(out, department)
	}
	return out
}

func (s *Service) GetMapping(ctx context.Context, id int) (*Mapping, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	row, err := s.client.RelayGroupMapping.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get relay group mapping: %w", err)
	}
	mapping := mappingFromEnt(row)
	if s.resolver != nil {
		if provider, resolveErr := s.resolver.Resolve(ctx, mapping.ProviderID); resolveErr == nil {
			if reader, ok := provider.(relay.AccountRelationshipReader); ok {
				if accounts, accountErr := reader.ListAccountsForPlatform(ctx, mapping.Platform); accountErr == nil {
					mapping.AccountPools = accountPools(mapping, accounts)
					mapping.Warnings = append(mapping.Warnings, accountPoolWarnings(mapping.AccountPools, mapping.AccountManagementInitialized)...)
				}
			}
		}
	}
	if groups, groupsErr := s.listPlatformGroups(ctx, mapping.ProviderID); groupsErr == nil {
		mapping.Warnings = mappingAvailabilityWarnings(mapping, groups)
	}
	if departmentErr := s.validateDepartment(ctx, mapping.DepartmentID); departmentErr != nil {
		mapping.Warnings = append(mapping.Warnings, fmt.Sprintf("department %s is unavailable", mapping.DepartmentID))
		mapping.DepartmentSuggestions = s.departmentSuggestions(ctx, mapping.ProviderID, mapping.Platform, mapping.DepartmentID)
	}
	mapping.Warnings = uniqueStrings(mapping.Warnings)
	return &mapping, nil
}

func (s *Service) PreviewMappingRenewal(ctx context.Context, id int, req MappingRenewalPreviewRequest) (*MappingRenewalPreview, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	renewalDays := defaultRenewalDays
	if req.RenewalDays != nil {
		if *req.RenewalDays <= 0 {
			return nil, fmt.Errorf("renewal_days must be positive")
		}
		if *req.RenewalDays > maxRenewalDays {
			return nil, fmt.Errorf("renewal_days must not exceed %d", maxRenewalDays)
		}
		renewalDays = *req.RenewalDays
	}
	mapping, err := s.client.RelayGroupMapping.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	if s.resolver == nil {
		return nil, fmt.Errorf("relay provider resolver is unavailable")
	}
	provider, err := s.resolver.Resolve(ctx, mapping.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	groupLister, ok := provider.(relay.PlatformGroupLister)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support group listing")
	}
	var groups []relay.Group
	var groupsErr error
	var relationships *providerRelationshipSnapshot
	var relationshipsErr error
	var reads sync.WaitGroup
	reads.Add(2)
	go func() {
		defer reads.Done()
		groups, groupsErr = groupLister.ListPlatformGroups(ctx)
	}()
	go func() {
		defer reads.Done()
		relationships, relationshipsErr = loadProviderRelationshipSnapshot(ctx, provider)
	}()
	reads.Wait()
	if groupsErr != nil {
		return nil, fmt.Errorf("list relay groups: %w", groupsErr)
	}
	if relationshipsErr != nil {
		return nil, fmt.Errorf("list Relay user relationships: %w", relationshipsErr)
	}
	groupsByID := make(map[int64]relay.Group, len(groups))
	for _, group := range groups {
		groupsByID[group.ID] = group
	}
	localUserIDs := make([]int, 0, len(mapping.MemberAssignments))
	for rawUserID, targetGroupID := range mapping.MemberAssignments {
		localUserID, parseErr := strconv.Atoi(rawUserID)
		if parseErr != nil || localUserID <= 0 || targetGroupID <= 0 {
			return nil, fmt.Errorf("mapping contains an invalid managed member assignment")
		}
		localUserIDs = append(localUserIDs, localUserID)
	}
	sort.Ints(localUserIDs)
	localUsers := make(map[int]*ent.User, len(localUserIDs))
	if len(localUserIDs) > 0 {
		items, queryErr := s.client.User.Query().Where(user.IDIn(localUserIDs...)).All(ctx)
		if queryErr != nil {
			return nil, fmt.Errorf("load managed mapping members: %w", queryErr)
		}
		for _, item := range items {
			localUsers[item.ID] = item
		}
	}
	now := s.currentTime()
	members := make([]MappingRenewalMember, 0, len(localUserIDs))
	for _, localUserID := range localUserIDs {
		local := localUsers[localUserID]
		if local == nil || local.RelayUserID == nil || *local.RelayUserID <= 0 {
			return nil, fmt.Errorf("managed mapping member %d has no Relay identity", localUserID)
		}
		relayUserID := int64(*local.RelayUserID)
		remote, found := relationships.byUserID[relayUserID]
		if !found || !sameRelayIdentity(local.Username, local.Email, relayUserID, &remote.User) {
			return nil, fmt.Errorf("managed mapping member %d has a stale Relay identity", localUserID)
		}
		subscriptions := append([]relay.UserSubscription(nil), remote.Subscriptions...)
		for index := range subscriptions {
			if subscriptions[index].GroupID <= 0 && subscriptions[index].Group != nil {
				subscriptions[index].GroupID = subscriptions[index].Group.ID
			}
		}
		sort.Slice(subscriptions, func(i, j int) bool {
			if subscriptions[i].GroupID == subscriptions[j].GroupID {
				return subscriptions[i].ID < subscriptions[j].ID
			}
			return subscriptions[i].GroupID < subscriptions[j].GroupID
		})
		targetGroupID := mapping.MemberAssignments[strconv.Itoa(localUserID)]
		targetGroup := groupsByID[targetGroupID]
		if targetGroup.ID <= 0 || !strings.EqualFold(strings.TrimSpace(targetGroup.Platform), strings.TrimSpace(mapping.Platform)) {
			return nil, fmt.Errorf("managed target group %d is unavailable on platform %s", targetGroupID, mapping.Platform)
		}
		member := MappingRenewalMember{
			UserID:                  local.ID,
			RelayUserID:             relayUserID,
			Username:                local.Username,
			Email:                   local.Email,
			ExpectedTargetGroupID:   targetGroupID,
			ExpectedTargetGroupName: strings.TrimSpace(targetGroup.Name),
			Drift:                   []MappingRenewalDrift{},
			subscriptions:           subscriptions,
		}
		var expected *relay.UserSubscription
		for index := range subscriptions {
			subscription := &subscriptions[index]
			if subscription.GroupID == targetGroupID && expected == nil {
				expected = subscription
				continue
			}
			if subscription.GroupID <= 0 {
				continue
			}
			group := groupsByID[subscription.GroupID]
			if subscription.Group != nil && strings.TrimSpace(subscription.Group.Name) != "" {
				group = *subscription.Group
			}
			member.Drift = append(member.Drift, MappingRenewalDrift{
				GroupID:   subscription.GroupID,
				GroupName: strings.TrimSpace(group.Name),
				Status:    renewalSubscriptionStatus(subscription, now),
				ExpiresAt: timePointer(subscription.ExpiresAt),
			})
		}
		member.Status = renewalSubscriptionStatus(expected, now)
		if expected != nil {
			member.CurrentExpiry = timePointer(expected.ExpiresAt)
			member.expectedSubscriptionID = expected.ID
		}
		switch member.Status {
		case "active":
			member.PlannedAction = "extend"
			result := projectedRenewalExpiry(expected.ExpiresAt, renewalDays)
			member.ResultingExpiry = &result
		case "expired":
			member.PlannedAction = "renew"
			result := projectedRenewalExpiry(now, renewalDays)
			member.ResultingExpiry = &result
		case "suspended":
			member.PlannedAction = "skip"
			member.ResultingExpiry = timePointer(expected.ExpiresAt)
		default:
			member.PlannedAction = "create"
			result := projectedRenewalExpiry(now, renewalDays)
			member.ResultingExpiry = &result
		}
		members = append(members, member)
	}
	fingerprint, err := encodeMappingRenewalFingerprint(mapping, members, groupsByID, renewalDays)
	if err != nil {
		return nil, fmt.Errorf("fingerprint mapping renewal relationships: %w", err)
	}
	return &MappingRenewalPreview{
		MappingID: mapping.ID, ProviderID: mapping.ProviderID, Platform: mapping.Platform,
		RenewalDays: renewalDays, Members: members, GeneratedAt: now, RelationshipFingerprint: fingerprint,
	}, nil
}

func loadProviderRelationshipSnapshot(ctx context.Context, provider relay.Provider) (*providerRelationshipSnapshot, error) {
	reader, ok := provider.(relay.UserRelationshipSnapshotReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support relationship snapshot reading")
	}
	relationships, err := reader.ListUserRelationships(ctx)
	if err != nil {
		return nil, fmt.Errorf("read provider relationship snapshot: %w", redactProviderReadError(err))
	}
	snapshot := &providerRelationshipSnapshot{relationships: relationships, byUserID: make(map[int64]relay.UserRelationship, len(relationships))}
	for index := range snapshot.relationships {
		relationship := &snapshot.relationships[index]
		if relationship.User.ID <= 0 {
			continue
		}
		for subscriptionIndex := range relationship.Subscriptions {
			subscription := &relationship.Subscriptions[subscriptionIndex]
			if subscription.GroupID <= 0 && subscription.Group != nil {
				subscription.GroupID = subscription.Group.ID
			}
		}
		snapshot.byUserID[relationship.User.ID] = *relationship
	}
	return snapshot, nil
}

func projectedRenewalExpiry(base time.Time, renewalDays int) time.Time {
	result := base.AddDate(0, 0, renewalDays)
	maximum := time.Date(2099, time.December, 31, 23, 59, 59, 0, time.UTC)
	if result.After(maximum) {
		return maximum
	}
	return result
}

func (s *Service) currentTime() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

func (s *Service) ExecuteMappingRenewal(ctx context.Context, id int, req MappingRenewalExecuteRequest) (*MappingRenewalExecution, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	if req.RenewalDays <= 0 || req.RenewalDays > maxRenewalDays {
		return nil, fmt.Errorf("renewal_days must be between 1 and %d", maxRenewalDays)
	}
	if strings.TrimSpace(req.ExpectedRelationshipFingerprint) == "" {
		return nil, fmt.Errorf("expected_relationship_fingerprint is required")
	}
	if strings.TrimSpace(req.OperationKey) == "" {
		return nil, fmt.Errorf("operation_key is required")
	}
	if len(req.Members) == 0 {
		return nil, fmt.Errorf("at least one reviewed mapping member is required")
	}
	row, err := s.client.RelayGroupMapping.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	if err := blockUnrelatedLegacyMutation(row); err != nil {
		return nil, err
	}
	preview, err := s.PreviewMappingRenewal(ctx, id, MappingRenewalPreviewRequest{RenewalDays: &req.RenewalDays})
	if err != nil {
		return nil, fmt.Errorf("refresh mapping renewal preview: %w", err)
	}
	if req.ExpectedRelationshipFingerprint != preview.RelationshipFingerprint {
		differences := relationshipFingerprintDifferences(req.ExpectedRelationshipFingerprint, preview.RelationshipFingerprint)
		if len(differences) == 0 {
			differences = []string{"Relay relationships changed after Preview; review the refreshed plan"}
		}
		return nil, &StaleMappingRenewalError{ExpectedFingerprint: req.ExpectedRelationshipFingerprint, CurrentFingerprint: preview.RelationshipFingerprint, RefreshedPreview: preview, Differences: differences}
	}
	currentMembers := make(map[int]MappingRenewalMember, len(preview.Members))
	for _, member := range preview.Members {
		currentMembers[member.UserID] = member
	}
	type executionItem struct {
		reviewed MappingRenewalReviewedMember
		current  MappingRenewalMember
	}
	items := make([]executionItem, 0, len(req.Members))
	seen := make(map[int]struct{}, len(req.Members))
	for _, reviewed := range req.Members {
		if reviewed.UserID <= 0 || reviewed.TargetGroupID <= 0 {
			return nil, fmt.Errorf("reviewed mapping member and target Group are required")
		}
		if _, duplicate := seen[reviewed.UserID]; duplicate {
			return nil, fmt.Errorf("mapping member %d is duplicated", reviewed.UserID)
		}
		seen[reviewed.UserID] = struct{}{}
		current, managed := currentMembers[reviewed.UserID]
		if !managed || current.ExpectedTargetGroupID != reviewed.TargetGroupID {
			return nil, fmt.Errorf("mapping member %d is not managed by target Group %d", reviewed.UserID, reviewed.TargetGroupID)
		}
		if !mappingRenewalActionCompatible(reviewed.PlannedAction, current.PlannedAction, req.Retry) {
			return nil, fmt.Errorf("mapping member %d planned action changed from %s to %s", reviewed.UserID, reviewed.PlannedAction, current.PlannedAction)
		}
		items = append(items, executionItem{reviewed: reviewed, current: current})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].reviewed.UserID < items[j].reviewed.UserID })
	provider, err := s.resolver.Resolve(ctx, preview.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	writer, ok := provider.(relay.IdempotentUserSubscriptionWriter)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support idempotent subscription writing")
	}
	result := &MappingRenewalExecution{MappingID: id, RenewalDays: req.RenewalDays, OperationKey: strings.TrimSpace(req.OperationKey), Members: make([]MappingRenewalMemberResult, len(items))}
	executeItem := func(index int) {
		item := items[index]
		action := item.reviewed.PlannedAction
		memberResult := MappingRenewalMemberResult{UserID: item.current.UserID, RelayUserID: item.current.RelayUserID, TargetGroupID: item.current.ExpectedTargetGroupID, Action: action}
		if item.current.PlannedAction == "skip" {
			memberResult.Action = "skip"
			memberResult.Status = "skipped"
			result.Members[index] = memberResult
			return
		}
		memberKey := mappingRenewalMemberOperationKey(result.OperationKey, id, item.current.UserID, item.current.ExpectedTargetGroupID, action)
		var writeErr error
		switch action {
		case "create":
			writeErr = writer.AssignSubscriptionForUserWithOperationKey(ctx, item.current.RelayUserID, item.current.ExpectedTargetGroupID, req.RenewalDays, memberKey)
		case "extend", "renew":
			if item.current.expectedSubscriptionID <= 0 {
				writeErr = fmt.Errorf("reviewed subscription identity is unavailable")
			} else {
				writeErr = writer.ExtendSubscriptionByIDWithOperationKey(ctx, item.current.expectedSubscriptionID, req.RenewalDays, memberKey)
			}
		default:
			writeErr = fmt.Errorf("unsupported renewal action %q", action)
		}
		if writeErr != nil {
			memberResult.Status = "failed"
			memberResult.Error = writeErr.Error()
		} else {
			memberResult.Status = "succeeded"
		}
		result.Members[index] = memberResult
	}
	jobs := make(chan int)
	workerCount := maxCandidateWorkers
	if len(items) < workerCount {
		workerCount = len(items)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for index := range jobs {
				executeItem(index)
			}
		}()
	}
	for index := range items {
		jobs <- index
	}
	close(jobs)
	workers.Wait()
	result.Preview, err = s.PreviewMappingRenewal(ctx, id, MappingRenewalPreviewRequest{RenewalDays: &req.RenewalDays})
	if err != nil {
		result.PreviewError = err.Error()
	}
	return result, nil
}

func mappingRenewalActionCompatible(reviewed, current string, retry bool) bool {
	if reviewed == current {
		return reviewed == "create" || reviewed == "extend" || reviewed == "renew" || reviewed == "skip"
	}
	if !retry {
		return false
	}
	if current == "skip" {
		return reviewed == "create" || reviewed == "extend" || reviewed == "renew"
	}
	if reviewed == "create" {
		return current == "extend" || current == "renew"
	}
	return (reviewed == "extend" || reviewed == "renew") && (current == "extend" || current == "renew")
}

func mappingRenewalMemberOperationKey(operationKey string, mappingID, userID int, targetGroupID int64, action string) string {
	canonical := fmt.Sprintf("mapping-renewal:v1:%s:%d:%d:%d:%s", strings.TrimSpace(operationKey), mappingID, userID, targetGroupID, action)
	sum := sha256.Sum256([]byte(canonical))
	return "mapping-renewal-v1-" + hex.EncodeToString(sum[:])
}

func renewalSubscriptionStatus(subscription *relay.UserSubscription, now time.Time) string {
	if subscription == nil {
		return "missing"
	}
	status := strings.ToLower(strings.TrimSpace(subscription.Status))
	if status == "suspended" {
		return "suspended"
	}
	if status == "expired" || subscription.ExpiresAt.IsZero() || !subscription.ExpiresAt.After(now) {
		return "expired"
	}
	return "active"
}

func timePointer(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value.UTC()
	return &copy
}

func encodeMappingRenewalFingerprint(mapping *ent.RelayGroupMapping, members []MappingRenewalMember, groupsByID map[int64]relay.Group, renewalDays int) (string, error) {
	snapshot := relationshipSnapshot{ProviderID: mapping.ProviderID, Platform: strings.ToLower(strings.TrimSpace(mapping.Platform)), Renewal: &relationshipRenewalFact{Days: renewalDays}}
	mappingFact := relationshipMappingFact{ID: mapping.ID, ProviderID: mapping.ProviderID, Platform: snapshot.Platform, GroupIDs: append([]int64(nil), mapping.GroupIds...)}
	sort.Slice(mappingFact.GroupIDs, func(i, j int) bool { return mappingFact.GroupIDs[i] < mappingFact.GroupIDs[j] })
	relevantGroupIDs := make(map[int64]struct{}, len(mapping.GroupIds))
	for _, groupID := range mapping.GroupIds {
		relevantGroupIDs[groupID] = struct{}{}
	}
	for _, member := range members {
		mappingFact.Members = append(mappingFact.Members, relationshipMappingMemberFact{UserID: member.UserID, TargetGroupID: member.ExpectedTargetGroupID, SourceGroupID: mapping.MemberSources[strconv.Itoa(member.UserID)]})
		userFact := relationshipUserFact{LocalUserID: member.UserID, RelayUserID: member.RelayUserID}
		for _, subscription := range member.subscriptions {
			if subscription.GroupID <= 0 {
				continue
			}
			relevantGroupIDs[subscription.GroupID] = struct{}{}
			userFact.Subscriptions = append(userFact.Subscriptions, relationshipSubscriptionFromRelay(subscription))
		}
		sort.Slice(userFact.Subscriptions, func(i, j int) bool {
			left, right := userFact.Subscriptions[i], userFact.Subscriptions[j]
			return left.GroupID < right.GroupID || (left.GroupID == right.GroupID && left.Status < right.Status)
		})
		snapshot.Users = append(snapshot.Users, userFact)
		renewalMember := relationshipRenewalMemberFact{UserID: member.UserID, RelayUserID: member.RelayUserID, TargetGroupID: member.ExpectedTargetGroupID, Status: member.Status, PlannedAction: member.PlannedAction, CurrentExpiry: canonicalRelationshipTime(member.CurrentExpiry), Drift: []relationshipRenewalDriftFact{}}
		for _, drift := range member.Drift {
			renewalMember.Drift = append(renewalMember.Drift, relationshipRenewalDriftFact{GroupID: drift.GroupID, Status: drift.Status, ExpiresAt: canonicalRelationshipTime(drift.ExpiresAt)})
		}
		snapshot.Renewal.Members = append(snapshot.Renewal.Members, renewalMember)
	}
	sort.Slice(mappingFact.Members, func(i, j int) bool { return mappingFact.Members[i].UserID < mappingFact.Members[j].UserID })
	snapshot.Mappings = []relationshipMappingFact{mappingFact}
	for groupID := range relevantGroupIDs {
		group := groupsByID[groupID]
		if group.ID <= 0 {
			continue
		}
		snapshot.Groups = append(snapshot.Groups, relationshipGroupFact{ID: group.ID, Name: strings.TrimSpace(group.Name), Platform: strings.ToLower(strings.TrimSpace(group.Platform))})
	}
	sort.Slice(snapshot.Groups, func(i, j int) bool { return snapshot.Groups[i].ID < snapshot.Groups[j].ID })
	return encodeRelationshipFingerprint(snapshot)
}

func canonicalRelationshipTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func (s *Service) AdoptCurrentAccounts(ctx context.Context, id int) (*Mapping, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	row, err := s.client.RelayGroupMapping.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	if err := blockUnrelatedLegacyMutation(row); err != nil {
		return nil, err
	}
	if s.resolver == nil {
		return nil, fmt.Errorf("relay provider resolver is unavailable")
	}
	provider, err := s.resolver.Resolve(ctx, row.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	reader, ok := provider.(relay.AccountRelationshipReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support account relationship reading")
	}
	accounts, err := reader.ListAccountsForPlatform(ctx, row.Platform)
	if err != nil {
		return nil, fmt.Errorf("list relay accounts: %w", err)
	}
	mapping := mappingFromEnt(row)
	pools := accountPools(mapping, accounts)
	desired := make(map[string][]map[string]int64, len(pools))
	for _, pool := range pools {
		key := strconv.FormatInt(pool.TargetGroupID, 10)
		desired[key] = make([]map[string]int64, 0, len(pool.Current))
		for _, account := range pool.Current {
			desired[key] = append(desired[key], map[string]int64{"account_id": account.ID, "priority": int64(account.Priority)})
		}
	}
	row, err = row.Update().SetAccountManagementInitialized(true).SetDesiredAccounts(desired).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("adopt current account relationships: %w", err)
	}
	mapping = mappingFromEnt(row)
	mapping.AccountPools = accountPools(mapping, accounts)
	mapping.Warnings = accountPoolWarnings(mapping.AccountPools, mapping.AccountManagementInitialized)
	return &mapping, nil
}

func (s *Service) SearchAccounts(ctx context.Context, req AccountSearchRequest) (*AccountSearchPage, error) {
	if req.ProviderID <= 0 || strings.TrimSpace(req.Platform) == "" {
		return nil, fmt.Errorf("provider_id and platform are required")
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 100 {
		req.PageSize = 100
	}
	provider, err := s.resolver.Resolve(ctx, req.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	reader, ok := provider.(relay.AccountRelationshipReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support account relationship reading")
	}
	accounts, err := reader.ListAccountsForPlatform(ctx, req.Platform)
	if err != nil {
		return nil, fmt.Errorf("list relay accounts: %w", err)
	}
	query := strings.ToLower(strings.TrimSpace(req.Query))
	filtered := make([]relay.Account, 0, len(accounts))
	for _, account := range accounts {
		if !strings.EqualFold(strings.TrimSpace(account.Platform), strings.TrimSpace(req.Platform)) {
			continue
		}
		if query != "" {
			haystack := strings.ToLower(fmt.Sprintf("%d %s %s %s", account.ID, account.Name, account.Type, account.Status))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		filtered = append(filtered, account)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		left, right := strings.ToLower(filtered[i].Name), strings.ToLower(filtered[j].Name)
		if left == right {
			return filtered[i].ID < filtered[j].ID
		}
		return left < right
	})
	total := len(filtered)
	start := (req.Page - 1) * req.PageSize
	if start > total {
		start = total
	}
	end := start + req.PageSize
	if end > total {
		end = total
	}
	return &AccountSearchPage{Items: filtered[start:end], Total: total, Page: req.Page, PageSize: req.PageSize}, nil
}

func (s *Service) SaveDesiredAccounts(ctx context.Context, id int, desired map[string][]AccountIntent) (*Mapping, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	row, err := s.client.RelayGroupMapping.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	if err := blockUnrelatedLegacyMutation(row); err != nil {
		return nil, err
	}
	provider, err := s.resolver.Resolve(ctx, row.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	reader, ok := provider.(relay.AccountRelationshipReader)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support account relationship reading")
	}
	accounts, err := reader.ListAccountsForPlatform(ctx, row.Platform)
	if err != nil {
		return nil, fmt.Errorf("list relay accounts: %w", err)
	}
	validAccounts := make(map[int64]relay.Account, len(accounts))
	for _, account := range accounts {
		if strings.EqualFold(strings.TrimSpace(account.Platform), strings.TrimSpace(row.Platform)) {
			validAccounts[account.ID] = account
		}
	}
	targets := make(map[string]struct{}, len(row.GroupIds))
	normalized := make(map[string][]AccountIntent, len(row.GroupIds))
	for _, groupID := range row.GroupIds {
		key := strconv.FormatInt(groupID, 10)
		targets[key] = struct{}{}
		normalized[key] = []AccountIntent{}
	}
	for groupID, intents := range desired {
		if _, ok := targets[groupID]; !ok {
			return nil, fmt.Errorf("target group %s is not managed by this mapping", groupID)
		}
		seenAccounts := make(map[int64]struct{}, len(intents))
		seenPriorities := make(map[int]struct{}, len(intents))
		for _, intent := range intents {
			if _, ok := validAccounts[intent.AccountID]; !ok {
				return nil, fmt.Errorf("account %d is unavailable on platform %s", intent.AccountID, row.Platform)
			}
			if _, duplicate := seenAccounts[intent.AccountID]; duplicate {
				return nil, fmt.Errorf("account %d is duplicated for target group %s", intent.AccountID, groupID)
			}
			if intent.Priority <= 0 || intent.Priority > len(intents) {
				return nil, fmt.Errorf("target group %s account priorities must be contiguous from 1", groupID)
			}
			if _, duplicate := seenPriorities[intent.Priority]; duplicate {
				return nil, fmt.Errorf("target group %s account priority %d is duplicated", groupID, intent.Priority)
			}
			seenAccounts[intent.AccountID] = struct{}{}
			seenPriorities[intent.Priority] = struct{}{}
		}
		normalized[groupID] = append([]AccountIntent(nil), intents...)
		sort.SliceStable(normalized[groupID], func(i, j int) bool { return normalized[groupID][i].Priority < normalized[groupID][j].Priority })
	}
	row, err = row.Update().SetAccountManagementInitialized(true).SetDesiredAccounts(accountIntentsToStorage(normalized)).Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("save desired account relationships: %w", err)
	}
	mapping := mappingFromEnt(row)
	mapping.AccountPools = accountPools(mapping, accounts)
	mapping.Warnings = accountPoolWarnings(mapping.AccountPools, mapping.AccountManagementInitialized)
	return &mapping, nil
}

func (s *Service) Rebind(ctx context.Context, id int, departmentID string, templateGroupID, sourceGroupID int64, groupIDs []int64, status string) (*Mapping, error) {
	if id <= 0 {
		return nil, fmt.Errorf("mapping id is required")
	}
	row, err := s.client.RelayGroupMapping.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	if err := blockUnrelatedLegacyMutation(row); err != nil {
		return nil, err
	}
	if templateGroupID <= 0 {
		templateGroupID = row.TemplateGroupID
	}
	if sourceGroupID <= 0 {
		sourceGroupID = row.SourceGroupID
	}
	if strings.TrimSpace(departmentID) == "" {
		departmentID = row.DepartmentExternalID
	}
	departmentID = strings.TrimSpace(departmentID)
	departmentName := row.DepartmentName
	if departmentID != row.DepartmentExternalID {
		name, nameErr := s.departmentName(ctx, departmentID)
		if nameErr != nil {
			return nil, fmt.Errorf("department %s is unavailable: %w", departmentID, nameErr)
		}
		departmentName = name
	}
	if templateGroupID <= 0 || sourceGroupID <= 0 {
		return nil, fmt.Errorf("template_group_id and source_group_id are required")
	}
	if status == "" {
		status = "active"
	}
	groups, groupsErr := s.listPlatformGroups(ctx, row.ProviderID)
	if groupsErr != nil {
		return nil, fmt.Errorf("validate relay group mapping groups: %w", groupsErr)
	}
	template, groupErr := findGroupForPlatform(groups, templateGroupID, row.Platform, "template")
	if groupErr != nil {
		return nil, groupErr
	}
	source, groupErr := findGroupForPlatform(groups, sourceGroupID, row.Platform, "migration source")
	if groupErr != nil {
		return nil, groupErr
	}
	seenGroups := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			return nil, fmt.Errorf("target group IDs must be positive")
		}
		if _, exists := seenGroups[groupID]; exists {
			return nil, fmt.Errorf("target group %d is duplicated", groupID)
		}
		seenGroups[groupID] = struct{}{}
		if _, targetErr := findGroupForPlatform(groups, groupID, row.Platform, "target"); targetErr != nil {
			return nil, targetErr
		}
	}
	row, err = s.client.RelayGroupMapping.UpdateOneID(id).
		SetDepartmentExternalID(departmentID).
		SetDepartmentName(departmentName).
		SetTemplateGroupID(templateGroupID).
		SetTemplateGroupName(template.Name).
		SetSourceGroupID(sourceGroupID).
		SetSourceGroupName(source.Name).
		SetGroupIds(append([]int64(nil), groupIDs...)).
		SetStatus(status).
		Save(ctx)
	if err != nil {
		return nil, fmt.Errorf("rebind relay group mapping: %w", err)
	}
	mapping := mappingFromEnt(row)
	if nameErr := s.validateDepartment(ctx, mapping.DepartmentID); nameErr != nil {
		mapping.Warnings = append(mapping.Warnings, fmt.Sprintf("department %s is unavailable", mapping.DepartmentID))
		mapping.DepartmentSuggestions = s.departmentSuggestions(ctx, mapping.ProviderID, mapping.Platform, mapping.DepartmentID)
	}
	return &mapping, nil
}

func (s *Service) listPlatformGroups(ctx context.Context, providerID int) ([]relay.Group, error) {
	if s.resolver == nil {
		return nil, fmt.Errorf("relay provider resolver is unavailable")
	}
	p, err := s.resolver.Resolve(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider: %w", err)
	}
	lister, ok := p.(relay.PlatformGroupLister)
	if !ok {
		return nil, fmt.Errorf("relay provider does not support group listing")
	}
	groups, err := lister.ListPlatformGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list relay groups: %w", err)
	}
	return groups, nil
}

func findGroupForPlatform(groups []relay.Group, id int64, platform, role string) (relay.Group, error) {
	for _, group := range groups {
		if group.ID != id {
			continue
		}
		if !strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform)) {
			return relay.Group{}, fmt.Errorf("%s group %d does not belong to platform %s", role, id, platform)
		}
		return group, nil
	}
	return relay.Group{}, fmt.Errorf("%s group %d is unavailable", role, id)
}

func (s *Service) validateDepartment(ctx context.Context, externalID string) error {
	_, err := s.departmentName(ctx, externalID)
	if err != nil {
		return fmt.Errorf("validate department %q: %w", externalID, err)
	}
	return nil
}

func (s *Service) departmentSuggestions(ctx context.Context, providerID int, platform, currentID string) []DepartmentSuggestion {
	snapshot, found, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil || !found {
		return nil
	}
	mappings, err := s.client.RelayGroupMapping.Query().Where(
		relaygroupmapping.ProviderIDEQ(providerID),
		relaygroupmapping.PlatformEQ(platform),
	).All(ctx)
	if err != nil {
		return nil
	}
	bound := make(map[string]struct{}, len(mappings))
	for _, mapping := range mappings {
		bound[mapping.DepartmentExternalID] = struct{}{}
	}
	departments, err := s.client.DirectoryDepartment.Query().Where(directorydepartment.SourceIDEQ(snapshot.SourceID)).Order(ent.Asc(directorydepartment.FieldName)).Limit(50).All(ctx)
	if err != nil {
		return nil
	}
	suggestions := make([]DepartmentSuggestion, 0, len(departments))
	for _, department := range departments {
		if department.ExternalID == currentID {
			continue
		}
		if _, exists := bound[department.ExternalID]; exists {
			continue
		}
		suggestions = append(suggestions, DepartmentSuggestion{ID: department.ExternalID, Name: department.Name})
	}
	return suggestions
}

func (s *Service) Replan(ctx context.Context, mappingID int, selected []int, assignments []Assignment, memberSources map[string]int64, removedUserIDs []int, memberActions map[string]MemberAction, adoptRelayUserIDs []int64) (*Plan, error) {
	row, err := s.client.RelayGroupMapping.Get(ctx, mappingID)
	if err != nil {
		return nil, fmt.Errorf("load relay group mapping: %w", err)
	}
	memberSources, allowUnreviewedRemovalSources, err := memberSourcesWithRemovalRetries(row.OperationState, memberSources, removedUserIDs)
	if err != nil {
		return nil, fmt.Errorf("restore removal retry source: %w", err)
	}
	memberActions = memberActionsWithRetries(row.OperationState, memberActions)
	return s.Preview(ctx, PreviewRequest{ProviderID: row.ProviderID, DepartmentID: row.DepartmentExternalID, Platform: row.Platform, TemplateGroupID: row.TemplateGroupID, SourceGroupID: row.SourceGroupID, WeeklyCostTarget: row.WeeklyCostTarget, GroupCount: len(row.GroupIds), SelectedUserIDs: selected, Assignments: assignments, MemberSources: memberSources, RemovedUserIDs: removedUserIDs, MemberActions: memberActions, AdoptRelayUserIDs: adoptRelayUserIDs, ExistingMappingID: mappingID, allowUnreviewedRemovalSources: allowUnreviewedRemovalSources})
}

func memberSourcesWithRemovalRetries(operationState map[string]map[string]string, memberSources map[string]int64, removedUserIDs []int) (map[string]int64, map[int]bool, error) {
	memberSources = cloneInt64Map(memberSources)
	allowUnreviewed := make(map[int]bool)
	for _, userID := range removedUserIDs {
		key := strconv.Itoa(userID)
		if entry := operationState["member:"+key]; entry != nil && entry["action"] == "remove" && operationStateNeedsRetry(operationState, "member:"+key) {
			if entry["source_reviewed"] == "true" || entry["source_group_id"] != "" {
				sourceGroupID := int64(0)
				if entry["source_group_id"] != "" {
					var err error
					sourceGroupID, err = strconv.ParseInt(entry["source_group_id"], 10, 64)
					if err != nil {
						return nil, nil, fmt.Errorf("stored removal source for user %d is invalid", userID)
					}
				}
				if reviewedSourceGroupID, reviewed := memberSources[key]; reviewed && reviewedSourceGroupID != sourceGroupID {
					return nil, nil, fmt.Errorf("removal source for user %d cannot change while retry is pending", userID)
				}
				memberSources[key] = sourceGroupID
			} else if _, reviewed := memberSources[key]; !reviewed {
				allowUnreviewed[userID] = true
			}
		}
	}
	return memberSources, allowUnreviewed, nil
}

func reviewedRemovalSource(mapping *Mapping, reviewedSources map[string]int64, userID int) (int64, bool) {
	key := strconv.Itoa(userID)
	if sourceGroupID, reviewed := reviewedSources[key]; reviewed {
		return sourceGroupID, true
	}
	if sourceGroupID, reviewed := mapping.MemberSources[key]; reviewed {
		return sourceGroupID, true
	}
	if previous := mapping.OperationState["member:"+key]; previous != nil && (previous["source_reviewed"] == "true" || previous["source_group_id"] != "") {
		sourceGroupID, _ := strconv.ParseInt(previous["source_group_id"], 10, 64)
		return sourceGroupID, true
	}
	return 0, false
}

func memberActionsWithRetries(operationState map[string]map[string]string, memberActions map[string]MemberAction) map[string]MemberAction {
	result := make(map[string]MemberAction, len(memberActions))
	for key, action := range memberActions {
		result[key] = action
	}
	for stateKey, entry := range operationState {
		if !strings.HasPrefix(stateKey, "member:") || entry["action"] != "move_here" || !operationStateNeedsRetry(operationState, stateKey) {
			continue
		}
		userID := strings.TrimPrefix(stateKey, "member:")
		if result[userID].Mode != "" {
			continue
		}
		fromMappingID, err := strconv.Atoi(entry["from_mapping_id"])
		if err == nil && fromMappingID > 0 {
			result[userID] = MemberAction{Mode: "move_here", FromMappingID: fromMappingID}
		}
	}
	return result
}

const legacyIntentVersion = 1

type legacyTargetIntent struct {
	Index           int             `json:"index"`
	TargetGroupID   int64           `json:"target_group_id,omitempty"`
	TargetGroupName string          `json:"target_group_name"`
	ExpectedStatus  string          `json:"expected_status"`
	Accounts        []AccountIntent `json:"accounts"`
}

type legacyMemberIntent struct {
	Action           string  `json:"action"`
	RelationshipType string  `json:"relationship_type"`
	LocalUserID      int     `json:"local_user_id"`
	RelayUserID      int64   `json:"relay_user_id"`
	TargetIndex      int     `json:"target_index"`
	SourceGroupID    int64   `json:"source_group_id,omitempty"`
	TargetGroupID    int64   `json:"target_group_id,omitempty"`
	APIKeyIDs        []int64 `json:"api_key_ids"`
	ExpectedResult   string  `json:"expected_result"`
}

type legacyReplanIntent struct {
	Version         int                  `json:"version"`
	MappingID       int                  `json:"mapping_id"`
	ProviderID      int                  `json:"provider_id"`
	Platform        string               `json:"platform"`
	TemplateGroupID int64                `json:"template_group_id"`
	SourceGroupID   int64                `json:"source_group_id,omitempty"`
	Targets         []legacyTargetIntent `json:"targets"`
	Members         []legacyMemberIntent `json:"members"`
	AdoptRelayUsers []int64              `json:"adopt_relay_users"`
}

func buildLegacyReplanIntent(mapping *Mapping, plan *Plan, req ExecuteRequest) (string, map[int]legacyMemberIntent, error) {
	intent := legacyReplanIntent{
		Version: legacyIntentVersion, MappingID: mapping.ID, ProviderID: mapping.ProviderID,
		Platform: strings.ToLower(strings.TrimSpace(mapping.Platform)), TemplateGroupID: mapping.TemplateGroupID,
		SourceGroupID: mapping.SourceGroupID, AdoptRelayUsers: append([]int64(nil), req.AdoptRelayUserIDs...),
	}
	sort.Slice(intent.AdoptRelayUsers, func(i, j int) bool { return intent.AdoptRelayUsers[i] < intent.AdoptRelayUsers[j] })
	memberIntents := make(map[int]legacyMemberIntent)
	pendingTargets := pendingCreationTargetIDs(mapping.OperationState)
	for _, assignment := range plan.Assignments {
		targetGroupID := assignment.TargetGroupID
		if _, pending := pendingTargets[targetGroupID]; pending {
			targetGroupID = 0
		}
		accounts := append([]AccountIntent(nil), assignment.DesiredAccounts...)
		sort.Slice(accounts, func(i, j int) bool {
			return accounts[i].Priority < accounts[j].Priority || (accounts[i].Priority == accounts[j].Priority && accounts[i].AccountID < accounts[j].AccountID)
		})
		intent.Targets = append(intent.Targets, legacyTargetIntent{Index: assignment.Index, TargetGroupID: targetGroupID, TargetGroupName: strings.TrimSpace(assignment.TargetGroupName), ExpectedStatus: "active", Accounts: accounts})
		for _, userID := range assignment.UserIDs {
			candidate := candidateByUserID(plan.Candidates, userID)
			if candidate == nil || candidate.RelayUserID <= 0 {
				return "", nil, fmt.Errorf("build legacy intent: user %d has no verified Relay identity", userID)
			}
			key := strconv.Itoa(userID)
			stateKey := "member:" + key
			entry := mapping.OperationState[stateKey]
			action := req.MemberActions[key].Mode
			fromGroupID := int64(0)
			if operationStateNeedsRetry(mapping.OperationState, stateKey) {
				fromGroupID, _ = strconv.ParseInt(entry["from_group_id"], 10, 64)
			}
			if fromGroupID <= 0 && action == "move_here" {
				fromGroupID = plannedMemberFromGroup(plan, userID, assignment.TargetGroupID)
			}
			if fromGroupID <= 0 {
				fromGroupID = mapping.MemberAssignments[key]
				if fromGroupID <= 0 && candidate.SourceMember {
					fromGroupID = mapping.SourceGroupID
				}
			}
			if action == "add_additionally" {
				fromGroupID = 0
			}
			if action == "" {
				switch {
				case fromGroupID > 0 && fromGroupID != assignment.TargetGroupID:
					action = "migrate"
				case fromGroupID == assignment.TargetGroupID:
					action = "retain"
				default:
					action = "add"
				}
			}
			intentSourceGroupID := fromGroupID
			expectedResult := "target_active;source_absent;reviewed_keys_on_target"
			if action == "retain" || action == "add" || action == "add_additionally" {
				intentSourceGroupID = 0
				expectedResult = "target_active"
			}
			selection := frozenLegacyAPIKeys(entry, candidate.relationshipAPIKeys, intentSourceGroupID)
			memberIntents[userID] = legacyMemberIntent{
				Action: action, RelationshipType: "managed_member", LocalUserID: userID, RelayUserID: candidate.RelayUserID,
				TargetIndex: assignment.Index, SourceGroupID: intentSourceGroupID, TargetGroupID: targetGroupID,
				APIKeyIDs: selection.IDs, ExpectedResult: expectedResult,
			}
		}
	}
	for _, userID := range req.RemovedUserIDs {
		candidate := candidateByUserID(plan.Candidates, userID)
		if candidate == nil || candidate.RelayUserID <= 0 {
			return "", nil, fmt.Errorf("build legacy intent: removed user %d has no verified Relay identity", userID)
		}
		key := strconv.Itoa(userID)
		entry := mapping.OperationState["member:"+key]
		targetGroupID := mapping.MemberAssignments[key]
		if targetGroupID <= 0 {
			targetGroupID, _ = strconv.ParseInt(entry["target_group_id"], 10, 64)
		}
		sourceGroupID, _ := reviewedRemovalSource(mapping, req.MemberSources, userID)
		selection := frozenLegacyAPIKeys(entry, candidate.relationshipAPIKeys, targetGroupID)
		memberIntents[userID] = legacyMemberIntent{
			Action: "remove", RelationshipType: "managed_member", LocalUserID: userID, RelayUserID: candidate.RelayUserID,
			TargetIndex: -1, SourceGroupID: sourceGroupID, TargetGroupID: targetGroupID,
			APIKeyIDs: selection.IDs, ExpectedResult: "target_absent;source_active_if_reviewed;reviewed_keys_on_source",
		}
	}
	userIDs := make([]int, 0, len(memberIntents))
	for userID := range memberIntents {
		userIDs = append(userIDs, userID)
	}
	sort.Ints(userIDs)
	for _, userID := range userIDs {
		intent.Members = append(intent.Members, memberIntents[userID])
	}
	encoded, err := json.Marshal(intent)
	if err != nil {
		return "", nil, fmt.Errorf("encode legacy operation intent: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("v%d:%x", legacyIntentVersion, sum), memberIntents, nil
}

func plannedMemberFromGroup(plan *Plan, userID int, targetGroupID int64) int64 {
	for _, target := range plan.TargetSummaries {
		if target.TargetGroupID != targetGroupID {
			continue
		}
		for _, member := range target.Members {
			if member.UserID == userID && member.FromGroupID > 0 {
				return member.FromGroupID
			}
		}
	}
	return 0
}

func frozenLegacyAPIKeys(entry map[string]string, current []relationshipAPIKeyFact, fromGroupID int64) reviewedAPIKeySelection {
	if entry != nil {
		if _, frozen := entry["reviewed_api_key_ids"]; frozen {
			return reviewedAPIKeySelection{IDs: parseAPIKeyIDs(entry["reviewed_api_key_ids"]), Frozen: true}
		}
		if ids := recordedAPIKeyStepIDs(strings.Split(entry["api_keys"], ",")); len(ids) > 0 {
			return reviewedAPIKeySelection{IDs: ids, Frozen: true}
		}
	}
	ids := make([]int64, 0)
	for _, key := range current {
		if key.GroupID == fromGroupID && key.ID > 0 {
			ids = append(ids, key.ID)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return reviewedAPIKeySelection{IDs: ids, Frozen: true}
}

func (s *Service) persistInitialLegacyRetryIntent(ctx context.Context, mapping *Mapping, plan *Plan, req ExecuteRequest) (*Mapping, error) {
	intentHash, members, err := buildLegacyReplanIntent(mapping, plan, req)
	if err != nil {
		return nil, err
	}
	state := cloneOperationState(mapping.OperationState)
	state["operation"]["intent_hash"] = intentHash
	for userID, intent := range members {
		entry := state["member:"+strconv.Itoa(userID)]
		if entry == nil {
			continue
		}
		entry["step_identity"] = legacyMemberStepIdentity(intent)
		entry["reviewed_api_key_ids"] = formatAPIKeyIDs(intent.APIKeyIDs)
	}
	row, err := s.client.RelayGroupMapping.UpdateOneID(mapping.ID).SetOperationState(state).SetStatus(operationStatus(state)).Save(ctx)
	if err != nil {
		return nil, err
	}
	updated := mappingFromEnt(row)
	return &updated, nil
}

func legacyMemberStepIdentity(intent legacyMemberIntent) string {
	encoded, _ := json.Marshal(intent)
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("v%d:%x", legacyIntentVersion, sum)
}

func validateLegacyRetryIntent(mapping *Mapping, intentHash string) error {
	if mapping.Status != "needs_retry" && operationStatus(mapping.OperationState) != "needs_retry" {
		return nil
	}
	stored := mapping.OperationState["operation"]["intent_hash"]
	if stored == "" {
		return &LegacyOperationConflictError{Reason: "incomplete_identity"}
	}
	if stored != intentHash {
		return &LegacyOperationConflictError{Reason: "edited_direction"}
	}
	for key, entry := range mapping.OperationState {
		if strings.HasPrefix(key, "member:") && operationStateNeedsRetry(mapping.OperationState, key) && entry["step_identity"] == "" {
			return &LegacyOperationConflictError{Reason: "incomplete_identity"}
		}
	}
	return nil
}

func validateLegacyRetryReadback(mapping *Mapping, plan *Plan, members map[int]legacyMemberIntent) error {
	if mapping.Status != "needs_retry" && operationStatus(mapping.OperationState) != "needs_retry" {
		return nil
	}
	for userID, intent := range members {
		candidate := candidateByUserID(plan.Candidates, userID)
		if candidate == nil {
			return &LegacyOperationConflictError{Reason: "readback_mismatch"}
		}
		for _, keyID := range intent.APIKeyIDs {
			found := false
			for _, key := range candidate.relationshipAPIKeys {
				if key.ID == keyID && (key.GroupID == intent.SourceGroupID || key.GroupID == intent.TargetGroupID) {
					found = true
					break
				}
			}
			if !found {
				return &LegacyOperationConflictError{Reason: "readback_mismatch"}
			}
		}
	}
	return nil
}

func blockUnrelatedLegacyMutation(row *ent.RelayGroupMapping) error {
	if row != nil && (row.Status == "needs_retry" || operationStatus(row.OperationState) == "needs_retry") {
		return &LegacyOperationConflictError{Reason: "active_operation"}
	}
	return nil
}

// ExecuteReplan preserves every existing Target ID while applying the reviewed
// member matrix and creating appended proposed Targets. Existing Target
// retirement and deactivation remain outside this operation.
func (s *Service) ExecuteReplan(ctx context.Context, mappingID int, req ExecuteRequest) (*ExecutionResult, error) {
	mapping, err := s.GetMapping(ctx, mappingID)
	if err != nil {
		return nil, fmt.Errorf("load mapping for replan execution: %w", err)
	}
	if strings.TrimSpace(req.OperationKey) == "" {
		return nil, fmt.Errorf("operation_key is required")
	}
	if (mapping.Status == "needs_retry" || operationStatus(mapping.OperationState) == "needs_retry") && mapping.OperationState["operation"]["intent_hash"] == "" {
		return nil, &LegacyOperationConflictError{Reason: "incomplete_identity"}
	}
	if req.ProviderID == 0 {
		req.ProviderID = mapping.ProviderID
	}
	if req.DepartmentID == "" {
		req.DepartmentID = mapping.DepartmentID
	}
	if req.Platform == "" {
		req.Platform = mapping.Platform
	}
	if req.SourceGroupID == 0 {
		req.SourceGroupID = mapping.SourceGroupID
	}
	if req.TemplateGroupID == 0 {
		req.TemplateGroupID = mapping.TemplateGroupID
	}
	if req.WeeklyCostTarget == 0 {
		req.WeeklyCostTarget = mapping.WeeklyCostTarget
	}
	req.GroupCount = len(mapping.GroupIDs)
	req.ExistingMappingID = mappingID
	req.MemberSources, _, err = memberSourcesWithRemovalRetries(mapping.OperationState, req.MemberSources, req.RemovedUserIDs)
	if err != nil {
		return nil, fmt.Errorf("restore removal retry source for execution: %w", err)
	}
	req.MemberActions = memberActionsWithRetries(mapping.OperationState, req.MemberActions)
	plan, err := s.Preview(ctx, req.PreviewRequest)
	if err != nil {
		if stale := stalePlanFromPreviewError(req.ExpectedRelationshipFingerprint, err); stale != nil {
			return nil, stale
		}
		return nil, fmt.Errorf("preview relay replan for execution: %w", err)
	}
	if err := validateRelationshipFingerprint(req.ExpectedRelationshipFingerprint, plan); err != nil {
		return nil, fmt.Errorf("validate relay replan relationship fingerprint: %w", err)
	}
	if len(plan.executionBlockers) > 0 || len(plan.unavailableTargetGroupIDs) > 0 {
		differences := replanRosterDifferences(plan.executionBlockers)
		differences = append(differences, replanUnavailableTargetDifferences(plan.unavailableTargetGroupIDs)...)
		return nil, &StalePlanError{
			ExpectedFingerprint: req.ExpectedRelationshipFingerprint,
			CurrentFingerprint:  plan.RelationshipFingerprint,
			RefreshedPlan:       plan,
			Differences:         differences,
		}
	}
	intentHash, memberIntents, err := buildLegacyReplanIntent(mapping, plan, req)
	if err != nil {
		return nil, err
	}
	if err := validateLegacyRetryIntent(mapping, intentHash); err != nil {
		return nil, err
	}
	if err := validateLegacyRetryReadback(mapping, plan, memberIntents); err != nil {
		return nil, err
	}
	durable, err := s.beginReplanDurableExecution(ctx, *mapping, plan, req)
	if err != nil {
		return nil, err
	}
	defer durable.interrupt(ctx)
	if err := durable.dispatch(ctx); err != nil {
		return nil, err
	}
	p, err := s.resolver.Resolve(ctx, mapping.ProviderID)
	if err != nil {
		return nil, fmt.Errorf("resolve relay provider for replan: %w", err)
	}
	assigner, _ := p.(subscriptionAssigner)
	remover, _ := p.(subscriptionRemover)
	binder, _ := p.(relay.APIKeyGroupBinder)
	renamer, supportsRename := p.(relay.GroupRenamer)
	duplicator, supportsDuplicate := p.(relay.GroupDuplicator)
	pendingCreation := pendingCreationTargetIDs(mapping.OperationState)
	creationRevision := mapping.UpdatedAt.UnixNano()
	preblockedTargets := make(map[int64]string)
	groupResults := make([]GroupResult, 0, len(plan.Assignments))
	for assignmentIndex := range plan.Assignments {
		assignment := &plan.Assignments[assignmentIndex]
		proposed := assignment.TargetGroupID == 0
		result := GroupResult{Index: assignment.Index, ID: assignment.TargetGroupID, Name: assignment.CurrentTargetGroupName, CurrentName: assignment.CurrentTargetGroupName, Status: "unchanged", Rename: "skipped"}
		if proposed {
			creationKey := fmt.Sprintf("replan-%d-%d-%d", mapping.ID, assignment.Index, creationRevision)
			var proposedDuplicator relay.GroupDuplicator
			if supportsDuplicate {
				proposedDuplicator = duplicator
			}
			var proposedRenamer relay.GroupRenamer
			if supportsRename {
				proposedRenamer = renamer
			}
			var createErr error
			result, createErr = duplicateAndRenameProposedTarget(ctx, proposedDuplicator, proposedRenamer, plan.TemplateGroupID, creationKey, mapping.GroupIDs, assignment, func(checkpoint GroupResult) error {
				mapping.GroupIDs = append(mapping.GroupIDs, checkpoint.ID)
				return durable.verifyStep(ctx, fmt.Sprintf("target:%d:create", checkpoint.Index), map[string]any{"group_id": checkpoint.ID, "name": checkpoint.CurrentName})
			})
			if createErr != nil {
				return nil, fmt.Errorf("checkpoint proposed target %d: %w", assignment.Index, createErr)
			}
		}
		if _, pending := pendingCreation[assignment.TargetGroupID]; pending {
			result.Creation = "pending"
		}
		if !proposed && assignment.RenameSelected && assignment.TargetGroupName != assignment.CurrentTargetGroupName {
			result.Name = assignment.TargetGroupName
			result.Status = "failed"
			result.Rename = "failed"
			switch {
			case !supportsRename:
				result.Error = "relay provider does not support group rename"
			default:
				renamed, renameErr := renamer.RenameGroup(ctx, assignment.TargetGroupID, assignment.TargetGroupName)
				if renameErr != nil {
					result.Error = renameErr.Error()
				} else if renamed == nil || renamed.ID != assignment.TargetGroupID || renamed.Name != assignment.TargetGroupName {
					result.Error = "relay returned an unexpected group after rename"
				} else {
					result.Status = "succeeded"
					result.Rename = "succeeded"
				}
			}
		}
		if result.Creation == "pending" && result.Rename == "failed" {
			preblockedTargets[result.ID] = result.Error
		}
		groupResults = append(groupResults, result)
	}
	if plan.AccountsReviewed {
		mapping.AccountManagementInitialized = true
		mapping.DesiredAccounts = desiredAccountsForGroupIDs(plan.Assignments, mapping.GroupIDs)
	}
	accountResults, blockedTargets := s.applyDesiredAccountRelationships(ctx, p, *mapping, preblockedTargets)
	statusUpdater, supportsStatusUpdate := p.(relay.GroupStatusUpdater)
	for index := range groupResults {
		result := &groupResults[index]
		if result.Creation != "pending" {
			continue
		}
		if reason := blockedTargets[result.ID]; reason != "" {
			result.Status = "failed"
			if result.Error == "" {
				result.Error = reason
			}
			continue
		}
		if !supportsStatusUpdate {
			reason := "relay provider does not support target group activation"
			blockedTargets[result.ID] = reason
			result.Status, result.Error = "failed", reason
			continue
		}
		if updateErr := statusUpdater.UpdateGroupStatus(ctx, result.ID, "active"); updateErr != nil {
			reason := fmt.Sprintf("activate target group %d: %v", result.ID, updateErr)
			blockedTargets[result.ID] = reason
			result.Status, result.Error = "failed", reason
			continue
		}
		result.Creation = "completed"
		result.Status = "succeeded"
	}
	memberResults := make([]MemberResult, 0, len(plan.Candidates))
	oldAssignments := mapping.MemberAssignments
	oldSources := mapping.MemberSources
	memberFromGroups := make(map[string]int64)
	type transferUpdate struct {
		mapping *ent.RelayGroupMapping
		userID  int
		result  MemberResult
	}
	transferUpdates := make([]transferUpdate, 0)
	for _, userID := range req.RemovedUserIDs {
		key := strconv.Itoa(userID)
		targetGroupID := oldAssignments[key]
		sourceGroupID, _ := reviewedRemovalSource(mapping, req.MemberSources, userID)
		if targetGroupID <= 0 {
			if previous := mapping.OperationState["member:"+key]; previous != nil && previous["action"] == "remove" && operationStateNeedsRetry(mapping.OperationState, "member:"+key) {
				targetGroupID, _ = strconv.ParseInt(previous["target_group_id"], 10, 64)
			}
		}
		if targetGroupID <= 0 {
			continue
		}
		intent := memberIntents[userID]
		member := MemberResult{Action: "remove", UserID: userID, TargetGroupID: targetGroupID, Subscription: "skipped", SourceRemoval: "skipped", reviewedAPIKeys: reviewedAPIKeySelection{IDs: append([]int64(nil), intent.APIKeyIDs...), Frozen: true}, stepIdentity: legacyMemberStepIdentity(intent)}
		candidate := candidateByUserID(plan.Candidates, userID)
		if candidate == nil || candidate.RelayUserID <= 0 {
			member.Error = "managed user has no valid Relay mapping"
			memberResults = append(memberResults, member)
			continue
		}
		member = completedMemberStepsFromState(mapping.OperationState, "member:"+key, member, candidate, intent)
		if sourceGroupID > 0 {
			member = executeMemberMigration(ctx, assigner, remover, binder, candidate, sourceGroupID, targetGroupID, member)
		} else if remover == nil {
			member.Error = "relay provider does not support subscription removal"
		} else if removeErr := remover.RemoveSubscriptionForUser(ctx, candidate.RelayUserID, targetGroupID); removeErr != nil && !isNotFoundError(removeErr) {
			member.SourceRemoval = "failed"
			member.Error = removeErr.Error()
		} else {
			member.SourceRemoval = "succeeded"
		}
		memberResults = append(memberResults, member)
	}
	for _, assignment := range plan.Assignments {
		if assignment.Index >= len(mapping.GroupIDs) {
			continue
		}
		targetID := mapping.GroupIDs[assignment.Index]
		if targetID <= 0 {
			continue
		}
		for _, userID := range assignment.UserIDs {
			candidate := candidateByUserID(plan.Candidates, userID)
			if candidate == nil {
				continue
			}
			key := strconv.Itoa(userID)
			action := req.MemberActions[key]
			retry := operationStateNeedsRetry(mapping.OperationState, "member:"+key)
			fromGroupID := int64(0)
			if retry {
				fromGroupID = oldSources[key]
				if previous := mapping.OperationState["member:"+key]; previous != nil {
					if previousFromGroupID, parseErr := strconv.ParseInt(previous["from_group_id"], 10, 64); parseErr == nil && previousFromGroupID > 0 {
						fromGroupID = previousFromGroupID
					}
				}
				if fromGroupID <= 0 && candidate.SourceMember {
					fromGroupID = mapping.SourceGroupID
				}
			} else {
				fromGroupID = oldAssignments[key]
				if fromGroupID <= 0 && candidate.SourceMember {
					fromGroupID = mapping.SourceGroupID
				}
			}
			intent := memberIntents[userID]
			member := MemberResult{Action: action.Mode, UserID: userID, TargetGroupID: targetID, Subscription: "skipped", SourceRemoval: "skipped", reviewedAPIKeys: reviewedAPIKeySelection{IDs: append([]int64(nil), intent.APIKeyIDs...), Frozen: true}, stepIdentity: legacyMemberStepIdentity(intent)}
			if retry {
				member = completedMemberStepsFromState(mapping.OperationState, "member:"+key, member, candidate, intent)
			}
			if reason := blockedTargets[targetID]; reason != "" {
				member.Error = reason
				memberResults = append(memberResults, member)
				continue
			}
			var transferSource *ent.RelayGroupMapping
			if action.Mode == "move_here" {
				transferSource, fromGroupID, err = s.resolveMoveSource(ctx, *mapping, userID, action)
				if err != nil {
					member.Error = err.Error()
					memberResults = append(memberResults, member)
					continue
				}
				if sourceGroupID := transferSource.MemberSources[key]; sourceGroupID > 0 {
					candidate.SourceGroupID = sourceGroupID
				}
				member.Action = "move_here"
				member = executeMemberMigration(ctx, assigner, remover, binder, candidate, targetID, fromGroupID, member)
				transferUpdates = append(transferUpdates, transferUpdate{mapping: transferSource, userID: userID, result: member})
			} else if action.Mode == "add_additionally" {
				candidate.SourceGroupID = 0
				member.Action = "add_additionally"
				member = executeMemberMigration(ctx, assigner, nil, nil, candidate, targetID, 0, member)
			} else if !retry && fromGroupID == targetID {
				member.Subscription = "unchanged"
				member.SourceRemoval = "skipped"
			} else if candidate.SourceMember {
				member = executeMemberMigration(ctx, assigner, remover, binder, candidate, targetID, fromGroupID, member)
			} else {
				member = executeMemberMigration(ctx, assigner, nil, nil, candidate, targetID, 0, member)
			}
			memberFromGroups[key] = fromGroupID
			memberResults = append(memberResults, member)
		}
	}
	if len(req.AdoptRelayUserIDs) > 0 {
		adopted := make(map[int64]struct{}, len(req.AdoptRelayUserIDs))
		for _, relayUserID := range req.AdoptRelayUserIDs {
			if relayUserID > 0 {
				adopted[relayUserID] = struct{}{}
			}
		}
		for _, unmanaged := range plan.UnmanagedMembers {
			if _, requested := adopted[unmanaged.RelayUserID]; !requested {
				continue
			}
			for _, targetID := range unmanaged.TargetGroupIDs {
				member := MemberResult{RelayUserID: unmanaged.RelayUserID, TargetGroupID: targetID, Subscription: "failed", SourceRemoval: "skipped"}
				if assigner == nil {
					member.Error = "relay provider does not support subscription assignment"
				} else if assignErr := assigner.AssignSubscriptionForUser(ctx, unmanaged.RelayUserID, targetID, defaultValidityDays); assignErr != nil && !isAlreadyAssignedError(assignErr) {
					member.Error = assignErr.Error()
				} else {
					member.Subscription = "succeeded"
				}
				memberResults = append(memberResults, member)
			}
		}
	}
	verifyRemovalRelationshipReadback(ctx, p, plan, mapping, req.MemberSources, memberResults)
	state := executionState(req.OperationKey, groupResults, memberResults)
	state["operation"]["intent_hash"] = intentHash
	mergeAccountResultsIntoState(state, accountResults)
	for _, member := range memberResults {
		if member.Action != "remove" || member.UserID <= 0 {
			continue
		}
		key := strconv.Itoa(member.UserID)
		if entry := state["member:"+key]; entry != nil {
			sourceGroupID, sourceReviewed := reviewedRemovalSource(mapping, req.MemberSources, member.UserID)
			if sourceReviewed {
				entry["source_reviewed"] = "true"
				entry["source_group_id"] = strconv.FormatInt(sourceGroupID, 10)
			}
		}
	}
	for key, fromGroupID := range memberFromGroups {
		if fromGroupID <= 0 {
			continue
		}
		if entry := state["member:"+key]; entry != nil {
			entry["from_group_id"] = strconv.FormatInt(fromGroupID, 10)
		}
	}
	for _, transfer := range transferUpdates {
		if entry := state[fmt.Sprintf("member:%d", transfer.userID)]; entry != nil {
			entry["from_mapping_id"] = strconv.Itoa(transfer.mapping.ID)
		}
	}
	type sourceMappingMutation struct {
		assignments map[string]int64
		sources     map[string]int64
		state       map[string]map[string]string
		revision    int64
	}
	sourceMutations := make(map[int]*sourceMappingMutation)
	for _, transfer := range transferUpdates {
		mutation := sourceMutations[transfer.mapping.ID]
		if mutation == nil {
			mutation = &sourceMappingMutation{
				assignments: cloneInt64Map(transfer.mapping.MemberAssignments),
				sources:     cloneInt64Map(transfer.mapping.MemberSources),
				state:       cloneOperationState(transfer.mapping.OperationState),
				revision:    transfer.mapping.BaselineRevision,
			}
			sourceMutations[transfer.mapping.ID] = mutation
		}
		delete(mutation.assignments, strconv.Itoa(transfer.userID))
		delete(mutation.sources, strconv.Itoa(transfer.userID))
		transferState := executionState(req.OperationKey, nil, []MemberResult{transfer.result})
		transferState["operation"]["intent_hash"] = intentHash
		if entry := transferState[fmt.Sprintf("member:%d", transfer.userID)]; entry != nil {
			entry["from_mapping_id"] = strconv.Itoa(transfer.mapping.ID)
			if fromGroupID := transfer.mapping.MemberAssignments[strconv.Itoa(transfer.userID)]; fromGroupID > 0 {
				entry["from_group_id"] = strconv.FormatInt(fromGroupID, 10)
			}
		}
		mutation.state = mergeOperationState(mutation.state, transferState)
	}
	sourceIDs := make([]int, 0, len(sourceMutations))
	for sourceID := range sourceMutations {
		sourceIDs = append(sourceIDs, sourceID)
	}
	sort.Ints(sourceIDs)
	mappingResults := make([]MappingPersistenceResult, 1, len(sourceIDs)+1)
	mappingResults[0] = MappingPersistenceResult{MappingID: mapping.ID, Role: "destination", Status: "pending"}
	for _, sourceID := range sourceIDs {
		mappingResults = append(mappingResults, MappingPersistenceResult{MappingID: sourceID, Role: "source", Status: "pending"})
	}
	applied := operationStatus(state) == "active"
	if !applied {
		for index := range mappingResults {
			mappingResults[index].Status = "skipped"
		}
		if err := durable.finish(ctx, false, map[string]any{"mapping_id": mapping.ID, "status": "interrupted"}); err != nil {
			return nil, fmt.Errorf("finish interrupted Relationship Operation: %w", err)
		}
		return &ExecutionResult{Plan: plan, Groups: groupResults, Accounts: accountResults, Members: memberResults, Mappings: mappingResults, Mapping: mapping}, nil
	}
	tx, err := s.client.Tx(ctx)
	if err != nil {
		mappingResults[0].Status, mappingResults[0].Error = "failed", err.Error()
		for index := 1; index < len(mappingResults); index++ {
			mappingResults[index].Status = "skipped"
		}
		return nil, &MappingPersistenceError{Cause: err, Results: mappingResults}
	}
	rollback := func(failedIndex int, cause error) error {
		_ = tx.Rollback()
		for index := range mappingResults {
			switch {
			case index < failedIndex:
				mappingResults[index].Status = "rolled_back"
			case index == failedIndex:
				mappingResults[index].Status, mappingResults[index].Error = "failed", cause.Error()
			default:
				mappingResults[index].Status = "skipped"
			}
		}
		return &MappingPersistenceError{Cause: cause, Results: mappingResults}
	}
	resultMapping, err := saveMappingWithClient(ctx, tx.Client(), plan, append([]int64(nil), mapping.GroupIDs...), state)
	if err != nil {
		return nil, rollback(0, err)
	}
	resultCount, err := tx.Client().RelayGroupMapping.Update().
		Where(relaygroupmapping.IDEQ(resultMapping.ID), relaygroupmapping.BaselineRevisionEQ(mapping.BaselineRevision)).
		AddBaselineRevision(1).
		Save(ctx)
	if err != nil {
		return nil, rollback(0, fmt.Errorf("advance destination Mapping baseline revision: %w", err))
	}
	if resultCount != 1 {
		return nil, rollback(0, fmt.Errorf("destination Mapping baseline revision changed during execution"))
	}
	resultRow, err := tx.Client().RelayGroupMapping.Get(ctx, resultMapping.ID)
	if err != nil {
		return nil, rollback(0, fmt.Errorf("reload destination Mapping: %w", err))
	}
	updatedResultMapping := mappingFromEnt(resultRow)
	resultMapping = &updatedResultMapping
	for index, sourceID := range sourceIDs {
		mutation := sourceMutations[sourceID]
		updatedSources, updateErr := tx.Client().RelayGroupMapping.Update().
			Where(relaygroupmapping.IDEQ(sourceID), relaygroupmapping.BaselineRevisionEQ(mutation.revision)).
			SetMemberAssignments(mutation.assignments).
			SetMemberSources(mutation.sources).
			SetOperationState(mutation.state).
			SetStatus(operationStatus(mutation.state)).
			AddBaselineRevision(1).
			Save(ctx)
		if updateErr != nil {
			return nil, rollback(index+1, fmt.Errorf("save source mapping %d transfer state: %w", sourceID, updateErr))
		}
		if updatedSources != 1 {
			return nil, rollback(index+1, fmt.Errorf("source Mapping %d baseline revision changed during execution", sourceID))
		}
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		for index := range mappingResults {
			mappingResults[index].Status, mappingResults[index].Error = "failed", err.Error()
		}
		return nil, &MappingPersistenceError{Cause: err, Results: mappingResults}
	}
	for index := range mappingResults {
		mappingResults[index].Status = "succeeded"
	}
	if err := durable.finish(ctx, true, map[string]any{"mapping_id": mapping.ID, "status": "applied"}); err != nil {
		return nil, fmt.Errorf("finish applied Relationship Operation: %w", err)
	}
	return &ExecutionResult{Plan: plan, Groups: groupResults, Accounts: accountResults, Members: memberResults, Mappings: mappingResults, Mapping: resultMapping}, nil
}

func (s *Service) resolveMoveSource(ctx context.Context, destination Mapping, userID int, action MemberAction) (*ent.RelayGroupMapping, int64, error) {
	if action.FromMappingID <= 0 || action.FromMappingID == destination.ID {
		return nil, 0, fmt.Errorf("move_here requires another source mapping")
	}
	source, err := s.client.RelayGroupMapping.Get(ctx, action.FromMappingID)
	if err != nil {
		return nil, 0, fmt.Errorf("load move source mapping: %w", err)
	}
	if source.ProviderID != destination.ProviderID || !strings.EqualFold(source.Platform, destination.Platform) {
		return nil, 0, fmt.Errorf("cross-provider or cross-platform transfer is not allowed")
	}
	key := strconv.Itoa(userID)
	fromGroupID := source.MemberAssignments[key]
	if fromGroupID <= 0 {
		if previous := destination.OperationState["member:"+key]; previous != nil && previous["from_mapping_id"] == strconv.Itoa(source.ID) {
			fromGroupID, _ = strconv.ParseInt(previous["from_group_id"], 10, 64)
		}
	}
	if fromGroupID <= 0 {
		return nil, 0, fmt.Errorf("user %d is not managed by source mapping %d", userID, source.ID)
	}
	return source, fromGroupID, nil
}

func (s *Service) applyDesiredAccountRelationships(ctx context.Context, provider relay.Provider, mapping Mapping, preblocked map[int64]string) ([]AccountResult, map[int64]string) {
	blocked := make(map[int64]string, len(preblocked))
	for targetGroupID, reason := range preblocked {
		blocked[targetGroupID] = reason
	}
	if !mapping.AccountManagementInitialized {
		return nil, blocked
	}
	reader, readerOK := provider.(relay.AccountRelationshipReader)
	updater, updaterOK := provider.(relay.AccountRelationshipUpdater)
	statusUpdater, statusUpdaterOK := provider.(relay.GroupStatusUpdater)
	if !readerOK || !updaterOK {
		for _, targetGroupID := range mapping.GroupIDs {
			blocked[targetGroupID] = "relay provider does not support account relationship updates"
		}
		return []AccountResult{{Status: "failed", Error: "relay provider does not support account relationship updates"}}, blocked
	}
	results := make([]AccountResult, 0)
	for _, targetGroupID := range mapping.GroupIDs {
		if blocked[targetGroupID] != "" {
			continue
		}
		accounts, err := reader.ListAccountsForPlatform(ctx, mapping.Platform)
		if err != nil {
			reason := fmt.Sprintf("list account relationships for target group %d: %v", targetGroupID, err)
			blocked[targetGroupID] = reason
			results = append(results, AccountResult{TargetGroupID: targetGroupID, Status: "failed", Error: reason})
			continue
		}
		accountsByID := make(map[int64]relay.Account, len(accounts))
		for _, account := range accounts {
			if strings.EqualFold(strings.TrimSpace(account.Platform), strings.TrimSpace(mapping.Platform)) {
				accountsByID[account.ID] = account
			}
		}
		desired := mapping.DesiredAccounts[strconv.FormatInt(targetGroupID, 10)]
		targetFailed := false
		if len(desired) == 0 {
			reason := "target group has no desired account relationship"
			blocked[targetGroupID] = reason
			results = append(results, AccountResult{TargetGroupID: targetGroupID, Status: "no_accounts", Error: reason})
		}
		desiredByID := make(map[int64]int, len(desired))
		for _, intent := range desired {
			desiredByID[intent.AccountID] = intent.Priority
			if _, ok := accountsByID[intent.AccountID]; !ok {
				reason := fmt.Sprintf("desired account %d is unavailable on platform %s", intent.AccountID, mapping.Platform)
				blocked[targetGroupID] = reason
				targetFailed = true
				results = append(results, AccountResult{TargetGroupID: targetGroupID, AccountID: intent.AccountID, DesiredPriority: intPointer(intent.Priority), Status: "failed", Error: reason})
			}
		}
		if targetFailed {
			continue
		}
		accountIDs := make(map[int64]struct{}, len(desired))
		for accountID := range desiredByID {
			accountIDs[accountID] = struct{}{}
		}
		for _, account := range accountsByID {
			if accountRelationshipPriority(account.GroupRelationships, targetGroupID) > 0 {
				accountIDs[account.ID] = struct{}{}
			}
		}
		orderedIDs := make([]int64, 0, len(accountIDs))
		for accountID := range accountIDs {
			orderedIDs = append(orderedIDs, accountID)
		}
		sort.Slice(orderedIDs, func(i, j int) bool { return orderedIDs[i] < orderedIDs[j] })
		for _, accountID := range orderedIDs {
			account, ok := accountsByID[accountID]
			if !ok {
				continue
			}
			currentPriority := accountRelationshipPriority(account.GroupRelationships, targetGroupID)
			desiredPriority, desiredExists := desiredByID[accountID]
			result := AccountResult{TargetGroupID: targetGroupID, AccountID: accountID, Status: "unchanged"}
			var desiredPointer *int
			if desiredExists {
				desiredPointer = intPointer(desiredPriority)
				result.DesiredPriority = desiredPointer
			}
			if (desiredExists && currentPriority == desiredPriority) || (!desiredExists && currentPriority == 0) {
				results = append(results, result)
				continue
			}
			if updateErr := updater.SetAccountGroupRelationship(ctx, accountID, targetGroupID, account.GroupRelationships, desiredPointer); updateErr != nil {
				result.Status = "failed"
				result.Error = updateErr.Error()
				targetFailed = true
				blocked[targetGroupID] = fmt.Sprintf("account relationships for target group %d need retry", targetGroupID)
			} else {
				result.Status = "succeeded"
			}
			results = append(results, result)
		}
		if len(desired) == 0 && !targetFailed {
			if !statusUpdaterOK {
				reason := "relay provider does not support target group deactivation"
				blocked[targetGroupID] = reason
				results = append(results, AccountResult{TargetGroupID: targetGroupID, Status: "failed", Error: reason})
			} else if updateErr := statusUpdater.UpdateGroupStatus(ctx, targetGroupID, "inactive"); updateErr != nil {
				reason := fmt.Sprintf("deactivate target group %d: %v", targetGroupID, updateErr)
				blocked[targetGroupID] = reason
				results = append(results, AccountResult{TargetGroupID: targetGroupID, Status: "failed", Error: reason})
			} else {
				results = append(results, AccountResult{TargetGroupID: targetGroupID, Status: "deactivated"})
			}
		}
	}
	return results, blocked
}

func accountRelationshipPriority(relationships []relay.AccountGroupRelationship, groupID int64) int {
	for _, relationship := range relationships {
		if relationship.GroupID == groupID {
			return relationship.Priority
		}
	}
	return 0
}

func intPointer(value int) *int {
	return &value
}

func mergeAccountResultsIntoState(state map[string]map[string]string, results []AccountResult) {
	for _, result := range results {
		key := fmt.Sprintf("account:%d:%d", result.TargetGroupID, result.AccountID)
		entry := map[string]string{"status": result.Status}
		if result.DesiredPriority != nil {
			entry["desired_priority"] = strconv.Itoa(*result.DesiredPriority)
		}
		if result.Error != "" {
			entry["error"] = result.Error
		}
		if result.Status == "failed" {
			state["operation"]["status"] = "needs_retry"
		}
		state[key] = entry
	}
}

func executionState(operationKey string, groups []GroupResult, members []MemberResult) map[string]map[string]string {
	state := map[string]map[string]string{
		"operation": {"key": operationKey, "status": "succeeded"},
	}
	for _, group := range groups {
		entry := map[string]string{"status": group.Status}
		if group.ID > 0 {
			entry["target_group_id"] = strconv.FormatInt(group.ID, 10)
		}
		if group.Name != "" {
			entry["target_group_name"] = group.Name
		}
		if group.CurrentName != "" {
			entry["current_target_group_name"] = group.CurrentName
		}
		if group.Rename != "" {
			entry["rename"] = group.Rename
		}
		if group.Creation != "" {
			entry["creation"] = group.Creation
		}
		if group.Error != "" {
			entry["error"] = group.Error
			state["operation"]["status"] = "needs_retry"
		}
		state[fmt.Sprintf("group:%d", group.Index)] = entry
	}
	for _, member := range members {
		entry := map[string]string{
			"subscription":   member.Subscription,
			"source_removal": member.SourceRemoval,
		}
		if member.Action != "" {
			entry["action"] = member.Action
		}
		if member.RelayUserID > 0 && member.Error == "" && member.Subscription == "succeeded" {
			entry["status"] = "adopted"
		}
		if member.TargetGroupID > 0 {
			entry["target_group_id"] = strconv.FormatInt(member.TargetGroupID, 10)
		}
		if len(member.APIKeys) > 0 {
			entry["api_keys"] = strings.Join(member.APIKeys, ",")
		}
		if member.reviewedAPIKeys.Frozen {
			entry["reviewed_api_key_ids"] = formatAPIKeyIDs(member.reviewedAPIKeys.IDs)
		}
		if member.stepIdentity != "" {
			entry["step_identity"] = member.stepIdentity
		}
		if member.Error != "" {
			entry["error"] = member.Error
			state["operation"]["status"] = "needs_retry"
		}
		if member.Subscription == "failed" || member.SourceRemoval == "failed" {
			state["operation"]["status"] = "needs_retry"
		}
		if member.RelayUserID > 0 {
			state[fmt.Sprintf("relay:%d:%d", member.RelayUserID, member.TargetGroupID)] = entry
		} else {
			state[fmt.Sprintf("member:%d", member.UserID)] = entry
		}
	}
	return state
}

func completedMemberStepsFromState(state map[string]map[string]string, key string, member MemberResult, candidate *Candidate, intent legacyMemberIntent) MemberResult {
	entry := state[key]
	if entry["step_identity"] == "" || entry["step_identity"] != member.stepIdentity {
		return member
	}
	subscriptionGroupID := intent.TargetGroupID
	removedGroupID := intent.SourceGroupID
	expectedKeyGroupID := intent.TargetGroupID
	if intent.Action == "remove" {
		subscriptionGroupID = intent.SourceGroupID
		removedGroupID = intent.TargetGroupID
		expectedKeyGroupID = intent.SourceGroupID
	}
	facts := relationshipUserFact{Subscriptions: candidate.relationshipSubscriptions}
	if entry["subscription"] == "succeeded" && (subscriptionGroupID <= 0 || hasActiveSubscription(facts, subscriptionGroupID)) {
		member.Subscription = "succeeded"
	}
	if entry["source_removal"] == "succeeded" && (removedGroupID <= 0 || !hasActiveSubscription(facts, removedGroupID)) {
		member.SourceRemoval = "succeeded"
	}
	completed, results := completedAPIKeySteps(strings.Split(entry["api_keys"], ","))
	for _, keyID := range intent.APIKeyIDs {
		if !completed[keyID] {
			continue
		}
		for _, current := range candidate.relationshipAPIKeys {
			if current.ID == keyID && current.GroupID == expectedKeyGroupID {
				member.APIKeys = append(member.APIKeys, apiKeyResultForID(results, keyID))
				break
			}
		}
	}
	return member
}

func apiKeyResultForID(results []string, keyID int64) string {
	for _, result := range results {
		if parsedID, _, ok := parseAPIKeyStep(result); ok && parsedID == keyID {
			return result
		}
	}
	return strconv.FormatInt(keyID, 10) + ":succeeded"
}

func executeMemberMigration(ctx context.Context, assigner subscriptionAssigner, remover subscriptionRemover, binder relay.APIKeyGroupBinder, candidate *Candidate, targetGroupID, fromGroupID int64, member MemberResult) MemberResult {
	if member.Subscription != "succeeded" {
		if hasActiveSubscription(relationshipUserFact{Subscriptions: candidate.relationshipSubscriptions}, targetGroupID) {
			member.Subscription = "unchanged"
		} else if assigner == nil {
			member.Error = "relay provider does not support subscription assignment"
			return member
		} else if err := assigner.AssignSubscriptionForUser(ctx, candidate.RelayUserID, targetGroupID, defaultValidityDays); err != nil && !isAlreadyAssignedError(err) {
			member.Error = err.Error()
			return member
		} else {
			member.Subscription = "succeeded"
		}
	}
	if fromGroupID <= 0 || fromGroupID == targetGroupID {
		return member
	}
	if binder != nil {
		completedKeyIDs, _ := completedAPIKeySteps(member.APIKeys)
		apiKeyError := false
		for _, key := range candidate.relationshipAPIKeys {
			if key.GroupID != fromGroupID || completedKeyIDs[key.ID] || (member.reviewedAPIKeys.Frozen && !slices.Contains(member.reviewedAPIKeys.IDs, key.ID)) {
				continue
			}
			if bindErr := binder.BindAPIKeyToGroup(ctx, key.ID, targetGroupID); bindErr != nil {
				member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:failed:%s", key.ID, bindErr))
				apiKeyError = true
			} else {
				member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:succeeded", key.ID))
			}
		}
		if apiKeyError {
			member.Error = "one or more API keys could not be moved"
			return member
		}
	}
	if member.SourceRemoval == "succeeded" {
		return member
	}
	if remover == nil {
		member.SourceRemoval = "skipped"
		return member
	}
	if err := remover.RemoveSubscriptionForUser(ctx, candidate.RelayUserID, fromGroupID); err != nil && !isNotFoundError(err) {
		member.SourceRemoval = "failed"
		member.Error = err.Error()
		return member
	}
	member.SourceRemoval = "succeeded"
	return member
}

type apiKeyStepResult struct {
	raw       string
	keyID     int64
	succeeded bool
}

func completedAPIKeySteps(results []string) (map[int64]bool, []string) {
	latest := make(map[int64]apiKeyStepResult)
	order := make([]int64, 0, len(results))
	for _, result := range results {
		keyID, status, ok := parseAPIKeyStep(result)
		if !ok {
			continue
		}
		if _, exists := latest[keyID]; !exists {
			order = append(order, keyID)
		}
		latest[keyID] = apiKeyStepResult{raw: result, keyID: keyID, succeeded: status == "succeeded"}
	}
	completed := make(map[int64]bool)
	kept := make([]string, 0, len(latest))
	for _, keyID := range order {
		step := latest[keyID]
		if !step.succeeded {
			continue
		}
		completed[step.keyID] = true
		kept = append(kept, step.raw)
	}
	return completed, kept
}

func recordedAPIKeyStepIDs(results []string) []int64 {
	seen := make(map[int64]struct{}, len(results))
	for _, result := range results {
		keyID, _, ok := parseAPIKeyStep(result)
		if ok {
			seen[keyID] = struct{}{}
		}
	}
	ids := make([]int64, 0, len(seen))
	for keyID := range seen {
		ids = append(ids, keyID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func reviewedAPIKeySelectionFromState(entry map[string]string) reviewedAPIKeySelection {
	if entry == nil {
		return reviewedAPIKeySelection{}
	}
	raw, frozen := entry["reviewed_api_key_ids"]
	ids := parseAPIKeyIDs(raw)
	if !frozen {
		ids = recordedAPIKeyStepIDs(strings.Split(entry["api_keys"], ","))
		frozen = len(ids) > 0
	}
	return reviewedAPIKeySelection{IDs: ids, Frozen: frozen}
}

func mergeAPIKeyIDs(left, right []int64) []int64 {
	seen := make(map[int64]struct{}, len(left)+len(right))
	for _, ids := range [][]int64{left, right} {
		for _, keyID := range ids {
			if keyID > 0 {
				seen[keyID] = struct{}{}
			}
		}
	}
	merged := make([]int64, 0, len(seen))
	for keyID := range seen {
		merged = append(merged, keyID)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i] < merged[j] })
	return merged
}

func parseAPIKeyIDs(value string) []int64 {
	seen := make(map[int64]struct{})
	for _, raw := range strings.Split(value, ",") {
		keyID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err == nil && keyID > 0 {
			seen[keyID] = struct{}{}
		}
	}
	ids := make([]int64, 0, len(seen))
	for keyID := range seen {
		ids = append(ids, keyID)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func formatAPIKeyIDs(ids []int64) string {
	values := make([]string, 0, len(ids))
	for _, keyID := range ids {
		if keyID > 0 {
			values = append(values, strconv.FormatInt(keyID, 10))
		}
	}
	return strings.Join(values, ",")
}

func parseAPIKeyStep(result string) (int64, string, bool) {
	parts := strings.SplitN(result, ":", 3)
	if len(parts) < 2 {
		return 0, "", false
	}
	keyID, err := strconv.ParseInt(parts[0], 10, 64)
	return keyID, parts[1], err == nil && keyID > 0
}

func verifyRemovalRelationshipReadback(ctx context.Context, provider relay.Provider, plan *Plan, mapping *Mapping, reviewedSources map[string]int64, members []MemberResult) {
	removedIndexes := make([]int, 0)
	candidates := make(map[int]*Candidate, len(plan.Candidates))
	for index := range plan.Candidates {
		candidates[plan.Candidates[index].UserID] = &plan.Candidates[index]
	}
	for index := range members {
		if members[index].Action == "remove" && members[index].Error == "" {
			removedIndexes = append(removedIndexes, index)
		}
	}
	if len(removedIndexes) == 0 {
		return
	}

	relationships := make(map[int64]relay.UserRelationship, len(removedIndexes))
	if reader, ok := provider.(relay.UserRelationshipSnapshotReader); ok {
		items, err := reader.ListUserRelationships(ctx)
		if err != nil {
			markRemovalReadbackUnavailable(members, removedIndexes)
			return
		}
		for _, item := range items {
			relationships[item.User.ID] = item
		}
	} else if reader, ok := provider.(relay.UserSubscriptionLister); ok {
		for _, index := range removedIndexes {
			candidate := candidates[members[index].UserID]
			if candidate == nil {
				continue
			}
			subscriptions, err := reader.ListUserSubscriptions(ctx, candidate.RelayUserID)
			if err != nil {
				markRemovalReadbackUnavailable(members, removedIndexes)
				return
			}
			relationships[candidate.RelayUserID] = relay.UserRelationship{User: relay.User{ID: candidate.RelayUserID}, Subscriptions: subscriptions}
		}
	} else {
		markRemovalReadbackUnavailable(members, removedIndexes)
		return
	}

	for _, index := range removedIndexes {
		member := &members[index]
		candidate := candidates[member.UserID]
		if candidate == nil {
			member.Error = "relationship readback failed: managed user disappeared"
			continue
		}
		problems := make([]string, 0, 3)
		relationship, found := relationships[candidate.RelayUserID]
		if !found {
			member.Error = "relationship readback failed: Relay user is unavailable"
			continue
		}
		facts := relationshipUserFact{RelayUserID: candidate.RelayUserID}
		for _, subscription := range relationship.Subscriptions {
			if subscription.GroupID <= 0 && subscription.Group != nil {
				subscription.GroupID = subscription.Group.ID
			}
			facts.Subscriptions = append(facts.Subscriptions, relationshipSubscriptionFromRelay(subscription))
		}
		sourceGroupID, _ := reviewedRemovalSource(mapping, reviewedSources, member.UserID)
		if sourceGroupID > 0 && !hasActiveSubscription(facts, sourceGroupID) {
			member.Subscription = "failed"
			problems = append(problems, fmt.Sprintf("Source Group %d subscription is not active", sourceGroupID))
		}
		for _, subscription := range facts.Subscriptions {
			if subscription.GroupID == member.TargetGroupID {
				member.SourceRemoval = "failed"
				problems = append(problems, fmt.Sprintf("Target Group %d subscription still exists", member.TargetGroupID))
				break
			}
		}

		expectedKeyGroupID := member.TargetGroupID
		if sourceGroupID > 0 {
			expectedKeyGroupID = sourceGroupID
		}
		expectedKeyIDs := make(map[int64]struct{})
		if entry := mapping.OperationState[fmt.Sprintf("member:%d", member.UserID)]; entry != nil {
			for _, keyID := range reviewedAPIKeySelectionFromState(entry).IDs {
				expectedKeyIDs[keyID] = struct{}{}
			}
		}
		for _, keyID := range member.reviewedAPIKeys.IDs {
			expectedKeyIDs[keyID] = struct{}{}
		}
		for _, keyID := range recordedAPIKeyStepIDs(member.APIKeys) {
			expectedKeyIDs[keyID] = struct{}{}
		}
		if !member.reviewedAPIKeys.Frozen {
			for _, key := range candidate.relationshipAPIKeys {
				if key.GroupID == member.TargetGroupID {
					expectedKeyIDs[key.ID] = struct{}{}
				}
			}
		}
		if len(expectedKeyIDs) > 0 {
			keys, err := provider.ListUserAPIKeys(ctx, candidate.RelayUserID)
			if err != nil {
				problems = append(problems, "API Key relationship readback is unavailable")
			} else {
				actualKeyGroups := make(map[int64]int64, len(keys))
				for _, key := range keys {
					actualKeyGroups[key.ID] = apiKeyGroupID(key)
				}
				for keyID := range expectedKeyIDs {
					if actualKeyGroups[keyID] != expectedKeyGroupID {
						member.APIKeys = append(member.APIKeys, fmt.Sprintf("%d:failed:relationship readback mismatch", keyID))
						problems = append(problems, fmt.Sprintf("API Key %d is not bound to Group %d", keyID, expectedKeyGroupID))
					}
				}
			}
		}
		if len(problems) > 0 {
			member.Error = "relationship readback failed: " + strings.Join(problems, "; ")
		}
	}
}

func markRemovalReadbackUnavailable(members []MemberResult, indexes []int) {
	for _, index := range indexes {
		members[index].Error = "relationship readback failed: subscription relationships are unavailable"
	}
}

func isAlreadyAssignedError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "already")
}

func isNotFoundError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "not found")
}

func eligibleCandidates(plan *Plan) []Candidate {
	out := make([]Candidate, 0, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		if candidate.Eligible {
			out = append(out, candidate)
		}
	}
	return out
}

func containsInt64(items []int64, target int64) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func apiKeyGroupID(key relay.APIKey) int64 {
	if key.GroupID > 0 {
		return key.GroupID
	}
	if key.Group != nil {
		return key.Group.ID
	}
	return 0
}

func validateRequest(req PreviewRequest) error {
	if req.ProviderID <= 0 || strings.TrimSpace(req.DepartmentID) == "" || strings.TrimSpace(req.Platform) == "" || req.TemplateGroupID <= 0 {
		return fmt.Errorf("provider_id, department_id, platform, and template_group_id are required")
	}
	if req.WeeklyCostTarget < 0 || math.IsNaN(req.WeeklyCostTarget) || math.IsInf(req.WeeklyCostTarget, 0) {
		return fmt.Errorf("weekly_cost_target must be a finite non-negative number")
	}
	return nil
}

func normalizeRequest(req PreviewRequest) PreviewRequest {
	req.DepartmentID = strings.TrimSpace(req.DepartmentID)
	req.Platform = strings.TrimSpace(req.Platform)
	if req.TemplateGroupID <= 0 {
		req.TemplateGroupID = req.SourceGroupID
	}
	if req.GroupCount < 0 {
		req.GroupCount = 0
	}
	return req
}

func (s *Service) rejectExistingInitialMapping(ctx context.Context, req PreviewRequest) error {
	if req.ExistingMappingID > 0 {
		return nil
	}
	mapping, err := s.client.RelayGroupMapping.Query().Where(
		relaygroupmapping.ProviderIDEQ(req.ProviderID),
		relaygroupmapping.DepartmentExternalIDEQ(req.DepartmentID),
		relaygroupmapping.PlatformEQ(req.Platform),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check existing Relay Group Mapping: %w", err)
	}
	return &ExistingMappingError{MappingID: mapping.ID}
}

func assignmentCount(assignments []Assignment) int {
	count := 0
	for _, assignment := range assignments {
		if assignment.Index >= count {
			count = assignment.Index + 1
		}
	}
	return count
}

func assignmentsReviewAccounts(assignments []Assignment) bool {
	for _, assignment := range assignments {
		if assignment.DesiredAccounts != nil {
			return true
		}
	}
	return false
}

func validateAssignments(assignments []Assignment, candidates []Candidate, count int) ([]Assignment, error) {
	if count <= 0 {
		return nil, fmt.Errorf("assignments must contain at least one target group")
	}
	if len(assignments) != count {
		return nil, fmt.Errorf("assignments must contain exactly %d target groups", count)
	}
	byUser := make(map[int]Candidate, len(candidates))
	for _, candidate := range candidates {
		byUser[candidate.UserID] = candidate
	}
	seenUsers := make(map[int]struct{})
	seenIndexes := make(map[int]struct{}, len(assignments))
	validated := make([]Assignment, count)
	for _, assignment := range assignments {
		if assignment.Index < 0 || assignment.Index >= count {
			return nil, fmt.Errorf("assignment index %d is out of range", assignment.Index)
		}
		if _, exists := seenIndexes[assignment.Index]; exists {
			return nil, fmt.Errorf("assignment index %d is duplicated", assignment.Index)
		}
		seenIndexes[assignment.Index] = struct{}{}
		var desiredAccounts []AccountIntent
		if assignment.DesiredAccounts != nil {
			desiredAccounts = append([]AccountIntent(nil), assignment.DesiredAccounts...)
		}
		validated[assignment.Index] = Assignment{Index: assignment.Index, TargetGroupID: assignment.TargetGroupID, TargetGroupName: strings.TrimSpace(assignment.TargetGroupName), RenameSelected: assignment.RenameSelected, UserIDs: make([]int, 0, len(assignment.UserIDs)), DesiredAccounts: desiredAccounts}
		for _, userID := range assignment.UserIDs {
			candidate, ok := byUser[userID]
			if !ok {
				return nil, &assignmentCandidateError{UserID: userID, Difference: "Relay user mappings changed"}
			}
			if !candidate.CanAdd {
				difference := "Relay user mappings changed"
				if candidate.relationshipGroupErr != nil {
					difference = "subscription relationships changed"
				}
				return nil, &assignmentCandidateError{UserID: userID, Difference: difference}
			}
			if _, exists := seenUsers[userID]; exists {
				return nil, fmt.Errorf("user %d is assigned more than once", userID)
			}
			seenUsers[userID] = struct{}{}
			validated[assignment.Index].UserIDs = append(validated[assignment.Index].UserIDs, userID)
			validated[assignment.Index].TotalCost += candidate.RangeCost
		}
	}
	for index := range validated {
		if _, ok := seenIndexes[index]; !ok {
			return nil, fmt.Errorf("assignment index %d is missing", index)
		}
	}
	return validated, nil
}

func findSourceGroup(groups []relay.Group, id int64, platform string) (relay.Group, error) {
	for _, group := range groups {
		if group.ID == id {
			if !strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform)) {
				return relay.Group{}, fmt.Errorf("source group platform does not match requested platform")
			}
			return group, nil
		}
	}
	return relay.Group{}, fmt.Errorf("group %d is unavailable", id)
}

func validateMemberSourceGroups(memberSources map[string]int64, groups []relay.Group, platform string) error {
	for rawUserID, groupID := range memberSources {
		userID, err := strconv.Atoi(rawUserID)
		if err != nil || userID <= 0 {
			return fmt.Errorf("member source user id %q is invalid", rawUserID)
		}
		if groupID < 0 {
			return fmt.Errorf("member source group for user %d must be non-negative", userID)
		}
		if groupID == 0 {
			continue
		}
		if _, err := findSourceGroup(groups, groupID, platform); err != nil {
			return fmt.Errorf("resolve source group for user %d: %w", userID, err)
		}
	}
	return nil
}

func (s *Service) buildCandidates(ctx context.Context, p relay.Provider, requestFacts *planningRequestFacts, providerID int, providerVersion int64, users []*ent.User, source relay.Group, groups []relay.Group, memberSources map[string]int64, platform, departmentID string) ([]Candidate, error) {
	allUsers, err := s.client.User.Query().Where(user.RelayUserIDNotNil()).Order(ent.Asc(user.FieldID)).Limit(maxPlanningUsers).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load users for global token ranking: %w", err)
	}
	ids := make([]int64, 0, len(allUsers))
	for _, u := range allUsers {
		if u.RelayUserID != nil && *u.RelayUserID > 0 {
			ids = append(ids, int64(*u.RelayUserID))
		}
	}
	stats, err := s.loadUsageStats(ctx, p, providerID, providerVersion, ids)
	if err != nil {
		return nil, fmt.Errorf("load 30-day usage: %w", err)
	}
	globalStats := stats
	if len(globalStats) == 0 {
		globalStats = stats
	}
	rankIDs := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := stats[id]; ok {
			rankIDs = append(rankIDs, id)
		}
	}
	sort.Slice(rankIDs, func(i, j int) bool {
		left, right := stats[rankIDs[i]], stats[rankIDs[j]]
		return usageTokens(left) > usageTokens(right) || (usageTokens(left) == usageTokens(right) && rankIDs[i] < rankIDs[j])
	})
	ranks := make(map[int64]int, len(rankIDs))
	for i, id := range rankIDs {
		ranks[id] = i + 1
	}
	out := make([]Candidate, len(users))
	jobs := make(chan struct {
		index int
		user  *ent.User
	}, len(users))
	for index, u := range users {
		jobs <- struct {
			index int
			user  *ent.User
		}{index: index, user: u}
	}
	close(jobs)
	workerCount := maxCandidateWorkers
	if len(users) < workerCount {
		workerCount = len(users)
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer workers.Done()
			for job := range jobs {
				candidateSource := source
				if sourceID, overridden := memberSources[strconv.Itoa(job.user.ID)]; overridden {
					candidateSource = relay.Group{}
					for _, group := range groups {
						if group.ID == sourceID {
							candidateSource = group
							break
						}
					}
				}
				out[job.index] = s.buildCandidate(ctx, p, requestFacts, job.user, candidateSource, platform, departmentID, globalStats, ranks)
			}
		}()
	}
	workers.Wait()
	sort.Slice(out, func(i, j int) bool {
		return out[i].RangeCost > out[j].RangeCost || (out[i].RangeCost == out[j].RangeCost && out[i].UserID < out[j].UserID)
	})
	return out, nil
}

func (s *Service) buildCandidate(ctx context.Context, p relay.Provider, requestFacts *planningRequestFacts, u *ent.User, source relay.Group, platform, departmentID string, globalStats map[int64]relay.TeamUserUsageStats, ranks map[int64]int) Candidate {
	candidate := Candidate{UserID: u.ID, Username: u.Username, Email: u.Email, Eligible: false, Selected: true, replanUnavailableReason: replanRosterUnavailableIdentity}
	if u.RelayUserID == nil || *u.RelayUserID <= 0 {
		candidate.Warnings = append(candidate.Warnings, fmt.Sprintf("user %d has no relay mapping", u.ID))
		return candidate
	}
	candidate.RelayUserID = int64(*u.RelayUserID)
	var remote *relay.User
	var identityErr error
	var relationship *relay.UserRelationship
	if requestFacts.relationships != nil {
		if current, found := requestFacts.relationships.byUserID[candidate.RelayUserID]; found {
			current := current
			remote = &current.User
			relationship = &current
		}
	} else {
		remote, identityErr = p.GetUser(ctx, candidate.RelayUserID)
	}
	facts := loadCandidateRelayFacts(ctx, p, requestFacts, relationship, candidate.RelayUserID, source, platform)
	if identityErr != nil {
		candidate.Warnings = append(candidate.Warnings, "relay mapping could not be verified for the selected provider")
		return candidate
	}
	if !sameRelayIdentity(u.Username, u.Email, candidate.RelayUserID, remote) {
		candidate.Warnings = append(candidate.Warnings, "relay mapping is not valid for the selected provider")
		return candidate
	}
	candidate.replanUnavailableReason = 0
	stat, usageKnown := globalStats[candidate.RelayUserID]
	candidate.UsageKnown = usageKnown
	candidate.RangeCost = usageCost(stat)
	candidate.RangeTokens = usageTokens(stat)
	candidate.GlobalTokenRank = ranks[candidate.RelayUserID]
	candidate.relationshipSubscriptions = facts.relationshipSubscriptions
	candidate.relationshipAPIKeys = facts.relationshipAPIKeys
	candidate.relationshipGroupErr = facts.groupErr
	candidate.relationshipKeyErr = facts.keyErr
	if facts.groupErr != nil {
		candidate.replanUnavailableReason = replanRosterUnavailableSubscription
		candidate.Warnings = append(candidate.Warnings, "subscription relationships are unavailable")
		return candidate
	}
	candidate.SourceMember = facts.eligible
	candidate.Eligible = facts.eligible || (source.ID <= 0 && facts.canAdd)
	candidate.CanAdd = facts.canAdd
	if facts.eligible && source.ID > 0 {
		candidate.SourceGroupID = source.ID
	}
	candidate.CurrentGroupIDs = facts.currentGroupIDs
	candidate.MigratableKeyCount = facts.migratableKeyCount
	if source.ID > 0 && !candidate.SourceMember {
		candidate.Warnings = append(candidate.Warnings, "user is not a member of the selected source group")
	} else if candidate.SourceGroupID > 0 && candidate.MigratableKeyCount == 0 {
		candidate.Warnings = append(candidate.Warnings, "no migratable AE-managed API key")
	}
	if !candidate.UsageKnown {
		candidate.Warnings = append(candidate.Warnings, "30-day usage is unknown; capacity may be underestimated")
	}
	if conflict, conflictErr := s.hasDepartmentConflict(ctx, u, departmentID); conflictErr == nil && conflict {
		candidate.Warnings = append(candidate.Warnings, "user belongs to multiple departments")
	}
	candidate.Selected = candidate.Eligible
	return candidate
}

type candidateRelayFacts struct {
	eligible                  bool
	canAdd                    bool
	currentGroupIDs           []int64
	migratableKeyCount        int
	relationshipSubscriptions []relationshipSubscriptionFact
	relationshipAPIKeys       []relationshipAPIKeyFact
	groupErr, keyErr          error
}

func loadCandidateRelayFacts(ctx context.Context, p relay.Provider, requestFacts *planningRequestFacts, relationship *relay.UserRelationship, userID int64, source relay.Group, platform string) candidateRelayFacts {
	var facts candidateRelayFacts
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		groupIDs := make(map[int64]struct{})
		usedSubscriptions := false
		var subscriptions []relay.UserSubscription
		if relationship != nil {
			subscriptions = relationship.Subscriptions
			usedSubscriptions = true
		} else if lister, ok := p.(relay.UserSubscriptionLister); ok {
			var err error
			subscriptions, err = lister.ListUserSubscriptions(ctx, userID)
			usedSubscriptions = err == nil
		}
		if usedSubscriptions {
			facts.canAdd = true
			for _, subscription := range subscriptions {
				groupID := subscription.GroupID
				if groupID <= 0 && subscription.Group != nil {
					groupID = subscription.Group.ID
				}
				if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") || groupID <= 0 {
					if groupID > 0 {
						subscription.GroupID = groupID
						facts.relationshipSubscriptions = append(facts.relationshipSubscriptions, relationshipSubscriptionFromRelay(subscription))
					}
					continue
				}
				subscription.GroupID = groupID
				facts.relationshipSubscriptions = append(facts.relationshipSubscriptions, relationshipSubscriptionFromRelay(subscription))
				if groupID == source.ID {
					facts.eligible = strings.EqualFold(strings.TrimSpace(source.Platform), strings.TrimSpace(platform))
				} else {
					groupIDs[groupID] = struct{}{}
				}
			}
		}
		if !usedSubscriptions {
			allowed, err := p.ListAllowedGroupsForUser(ctx, userID)
			facts.groupErr = err
			if err != nil {
				return
			}
			for _, group := range allowed {
				if group.ID > 0 {
					facts.relationshipSubscriptions = append(facts.relationshipSubscriptions, relationshipSubscriptionFact{GroupID: group.ID, Status: "active"})
				}
				if group.ID == source.ID {
					facts.eligible = strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(platform))
				} else if group.ID > 0 {
					groupIDs[group.ID] = struct{}{}
				}
			}
			facts.canAdd = true
		}
		if source.ID <= 0 && facts.canAdd {
			facts.eligible = true
		}
		facts.currentGroupIDs = make([]int64, 0, len(groupIDs))
		for groupID := range groupIDs {
			facts.currentGroupIDs = append(facts.currentGroupIDs, groupID)
		}
		sort.Slice(facts.currentGroupIDs, func(i, j int) bool { return facts.currentGroupIDs[i] < facts.currentGroupIDs[j] })
	}()
	go func() {
		defer workers.Done()
		keys, err := requestFacts.activeUserAPIKeys(ctx, p, userID)
		facts.keyErr = err
		if err != nil {
			return
		}
		for _, key := range keys {
			if groupID := apiKeyGroupID(key); groupID > 0 {
				facts.relationshipAPIKeys = append(facts.relationshipAPIKeys, relationshipAPIKeyFact{ID: key.ID, GroupID: groupID})
			}
			if source.ID > 0 && apiKeyGroupID(key) == source.ID {
				facts.migratableKeyCount++
			}
		}
	}()
	workers.Wait()
	sort.Slice(facts.relationshipSubscriptions, func(i, j int) bool {
		left, right := facts.relationshipSubscriptions[i], facts.relationshipSubscriptions[j]
		return left.GroupID < right.GroupID || (left.GroupID == right.GroupID && left.Status < right.Status)
	})
	sort.Slice(facts.relationshipAPIKeys, func(i, j int) bool {
		left, right := facts.relationshipAPIKeys[i], facts.relationshipAPIKeys[j]
		return left.ID < right.ID || (left.ID == right.ID && left.GroupID < right.GroupID)
	})
	return facts
}

func (s *Service) hasDepartmentConflict(ctx context.Context, u *ent.User, selectedDepartment string) (bool, error) {
	snapshot, found, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil {
		return false, fmt.Errorf("load directory snapshot for department conflict: %w", err)
	}
	if !found || u == nil {
		return false, nil
	}
	memberQuery := s.client.DirectoryMember.Query().Where(directorymember.SourceIDEQ(snapshot.SourceID))
	if u.RelayUserID != nil {
		memberQuery = memberQuery.Where(directorymember.Or(directorymember.MatchedUserIDEQ(u.ID), directorymember.EmailNormalizedEQ(strings.ToLower(strings.TrimSpace(u.Email)))))
	} else {
		memberQuery = memberQuery.Where(directorymember.EmailNormalizedEQ(strings.ToLower(strings.TrimSpace(u.Email))))
	}
	members, err := memberQuery.All(ctx)
	if err != nil {
		return false, fmt.Errorf("load directory members for department conflict: %w", err)
	}
	departments := make(map[string]struct{})
	for _, member := range members {
		if strings.TrimSpace(member.DepartmentExternalID) != "" {
			departments[member.DepartmentExternalID] = struct{}{}
		}
		membershipRows, membershipErr := s.client.DirectoryMemberDepartment.Query().Where(directorymemberdepartment.DirectoryMemberIDEQ(member.ID), directorymemberdepartment.SourceIDEQ(snapshot.SourceID)).All(ctx)
		if membershipErr != nil {
			return false, fmt.Errorf("load directory memberships for department conflict: %w", membershipErr)
		}
		for _, row := range membershipRows {
			departments[row.DepartmentExternalID] = struct{}{}
		}
	}
	if selectedDepartment != "" {
		delete(departments, selectedDepartment)
	}
	return len(departments) > 0, nil
}

func (s *Service) loadUsageStats(ctx context.Context, p relay.Provider, providerID int, providerVersion int64, ids []int64) (map[int64]relay.TeamUserUsageStats, error) {
	now := time.Now().UTC()
	params := thirtyDayUsageParams(now)
	if s.prewarmReader != nil && providerID > 0 && providerVersion > 0 {
		stats, outcome, err := s.prewarmReader.ReadAuthorizedStats(ctx, teamusage.PrewarmReadRequest{
			ProviderID: providerID, ProviderVersion: providerVersion,
			Params: teamusage.OverviewParams{
				StartDate: params.StartDate, EndDate: params.EndDate,
				Granularity: params.Granularity, Timezone: params.Timezone,
			},
			AuthorizedRelayUserIDs: ids,
		})
		if err == nil && outcome == teamusage.PrewarmReadFullHit && stats != nil {
			return stats, nil
		}
	}
	return usageStatsAt(ctx, p, ids, now)
}

func usageStats(ctx context.Context, p relay.Provider, ids []int64) (map[int64]relay.TeamUserUsageStats, error) {
	return usageStatsAt(ctx, p, ids, time.Now().UTC())
}

func usageStatsAt(ctx context.Context, p relay.Provider, ids []int64, now time.Time) (map[int64]relay.TeamUserUsageStats, error) {
	result := make(map[int64]relay.TeamUserUsageStats, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	params := thirtyDayUsageParams(now)
	if batch, ok := p.(relay.TeamUsageSummaryProvider); ok {
		var trend map[int64][]relay.UsageTrendPoint
		var trendErr error
		trendDone := make(chan struct{})
		if trendProvider, trendOK := p.(relay.TeamMemberTrendProvider); trendOK {
			go func() {
				trend, trendErr = trendProvider.GetUsageTrendForUsers(ctx, ids, relay.TeamMemberTrendParams{
					StartDate: params.StartDate, EndDate: params.EndDate, Granularity: params.Granularity, Timezone: params.Timezone,
				})
				close(trendDone)
			}()
		} else {
			close(trendDone)
		}
		for start := 0; start < len(ids); start += 500 {
			end := start + 500
			if end > len(ids) {
				end = len(ids)
			}
			stats, err := batch.GetBatchUserUsageStats(ctx, ids[start:end], params)
			if err != nil {
				return nil, fmt.Errorf("load batch user usage stats: %w", err)
			}
			for id, stat := range stats {
				result[id] = stat
			}
		}
		<-trendDone
		if trendErr == nil {
			mergeTrendUsage(result, trend)
		}
		return result, nil
	}
	for _, id := range ids {
		stat, err := p.GetUsageStats(ctx, id, now.Add(-30*24*time.Hour), now)
		if err != nil {
			return nil, fmt.Errorf("load usage stats for relay user %d: %w", id, err)
		}
		cost := stat.TotalCost
		tokens := stat.TotalTokens
		result[id] = relay.TeamUserUsageStats{UserID: id, RangeActualCost: &cost, RangeTotalTokens: &tokens, TotalActualCost: cost, TotalTokens: &tokens}
	}
	return result, nil
}

func thirtyDayUsageParams(now time.Time) relay.TeamUsageSummaryParams {
	now = now.UTC()
	return relay.TeamUsageSummaryParams{
		StartDate: now.AddDate(0, 0, -29).Format(time.DateOnly),
		EndDate:   now.Format(time.DateOnly), Granularity: "day", Timezone: "UTC",
	}
}

func mergeTrendUsage(stats map[int64]relay.TeamUserUsageStats, trends map[int64][]relay.UsageTrendPoint) {
	for userID, points := range trends {
		var tokens int64
		var actualCost float64
		for _, point := range points {
			if point.TotalTokens != nil {
				tokens += *point.TotalTokens
			}
			actualCost += point.ActualCost
		}
		stat := stats[userID]
		if stat.UserID == 0 {
			stat.UserID = userID
		}
		if stat.RangeTotalTokens == nil {
			value := tokens
			stat.RangeTotalTokens = &value
		}
		if stat.RangeActualCost == nil {
			value := actualCost
			stat.RangeActualCost = &value
		}
		stats[userID] = stat
	}
}

func usageTokens(stat relay.TeamUserUsageStats) int64 {
	if stat.RangeTotalTokens != nil {
		return *stat.RangeTotalTokens
	}
	if stat.TotalTokens != nil {
		return *stat.TotalTokens
	}
	return 0
}

func usageCost(stat relay.TeamUserUsageStats) float64 {
	if stat.RangeActualCost != nil {
		return *stat.RangeActualCost
	}
	return stat.TotalActualCost
}

func (s *Service) departmentName(ctx context.Context, externalID string) (string, error) {
	snapshot, found, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil {
		return "", fmt.Errorf("load current directory snapshot: %w", err)
	}
	if !found {
		return "", fmt.Errorf("current directory snapshot is unavailable")
	}
	row, err := s.client.DirectoryDepartment.Query().Where(
		directorydepartment.SourceIDEQ(snapshot.SourceID),
		directorydepartment.ExternalIDEQ(strings.TrimSpace(externalID)),
	).Only(ctx)
	if err != nil {
		return "", fmt.Errorf("load department %q: %w", externalID, err)
	}
	return row.Name, nil
}

func (s *Service) saveMapping(ctx context.Context, plan *Plan, groupIDs []int64, state map[string]map[string]string) (*Mapping, error) {
	return saveMappingWithClient(ctx, s.client, plan, groupIDs, state)
}

func saveMappingWithClient(ctx context.Context, client *ent.Client, plan *Plan, groupIDs []int64, state map[string]map[string]string) (*Mapping, error) {
	memberAssignments := make(map[string]int64)
	memberSources := make(map[string]int64)
	desiredAccounts := desiredAccountsForGroupIDs(plan.Assignments, groupIDs)
	for _, assignment := range plan.Assignments {
		if assignment.Index >= len(groupIDs) || groupIDs[assignment.Index] <= 0 {
			continue
		}
		for _, userID := range assignment.UserIDs {
			key := strconv.Itoa(userID)
			memberAssignments[key] = groupIDs[assignment.Index]
			if candidate := candidateByUserID(plan.Candidates, userID); candidate != nil && candidate.SourceGroupID > 0 {
				memberSources[key] = candidate.SourceGroupID
			}
		}
	}
	row, err := client.RelayGroupMapping.Query().Where(
		relaygroupmapping.ProviderIDEQ(plan.ProviderID),
		relaygroupmapping.DepartmentExternalIDEQ(plan.DepartmentID),
		relaygroupmapping.PlatformEQ(plan.Platform),
	).Only(ctx)
	if ent.IsNotFound(err) {
		create := client.RelayGroupMapping.Create().SetProviderID(plan.ProviderID).SetDepartmentExternalID(plan.DepartmentID).SetDepartmentName(plan.DepartmentName).SetPlatform(plan.Platform).SetTemplateGroupID(plan.TemplateGroupID).SetTemplateGroupName(plan.TemplateGroupName).SetSourceGroupID(plan.SourceGroupID).SetSourceGroupName(plan.SourceGroupName).SetGroupIds(groupIDs).SetMemberAssignments(memberAssignments).SetMemberSources(memberSources).SetOperationState(cloneOperationState(state)).SetWeeklyCostTarget(plan.WeeklyCostTarget).SetStatus(operationStatus(state))
		if plan.AccountsReviewed {
			create.SetAccountManagementInitialized(true).SetDesiredAccounts(accountIntentsToStorage(desiredAccounts))
		}
		row, err = create.Save(ctx)
	} else if err == nil {
		mergedState := mergeOperationState(row.OperationState, state)
		update := row.Update().SetDepartmentName(plan.DepartmentName).SetTemplateGroupID(plan.TemplateGroupID).SetTemplateGroupName(plan.TemplateGroupName).SetSourceGroupID(plan.SourceGroupID).SetSourceGroupName(plan.SourceGroupName).SetGroupIds(groupIDs).SetMemberAssignments(memberAssignments).SetMemberSources(memberSources).SetOperationState(mergedState).SetWeeklyCostTarget(plan.WeeklyCostTarget).SetStatus(operationStatus(mergedState))
		if plan.AccountsReviewed {
			update.SetAccountManagementInitialized(true).SetDesiredAccounts(accountIntentsToStorage(desiredAccounts))
		}
		row, err = update.Save(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("persist relay group mapping: %w", err)
	}
	mapping := mappingFromEnt(row)
	return &mapping, nil
}

func desiredAccountsForGroupIDs(assignments []Assignment, groupIDs []int64) map[string][]AccountIntent {
	desired := make(map[string][]AccountIntent, len(groupIDs))
	for _, assignment := range assignments {
		if assignment.Index < 0 || assignment.Index >= len(groupIDs) || groupIDs[assignment.Index] <= 0 {
			continue
		}
		desired[strconv.FormatInt(groupIDs[assignment.Index], 10)] = append([]AccountIntent(nil), assignment.DesiredAccounts...)
	}
	return desired
}

func mappingFromEnt(row *ent.RelayGroupMapping) Mapping {
	return Mapping{ID: row.ID, ProviderID: row.ProviderID, DepartmentID: row.DepartmentExternalID, DepartmentName: row.DepartmentName, Platform: row.Platform, TemplateGroupID: row.TemplateGroupID, TemplateGroupName: row.TemplateGroupName, SourceGroupID: row.SourceGroupID, SourceGroupName: row.SourceGroupName, GroupIDs: append([]int64(nil), row.GroupIds...), Status: row.Status, WeeklyCostTarget: row.WeeklyCostTarget, MemberAssignments: cloneInt64Map(row.MemberAssignments), MemberSources: cloneInt64Map(row.MemberSources), AccountManagementInitialized: row.AccountManagementInitialized, DesiredAccounts: accountIntentsFromStorage(row.DesiredAccounts), OperationState: cloneOperationState(row.OperationState), BaselineRevision: row.BaselineRevision, UpdatedAt: row.UpdatedAt}
}

func accountIntentsFromStorage(stored map[string][]map[string]int64) map[string][]AccountIntent {
	out := make(map[string][]AccountIntent, len(stored))
	for groupID, items := range stored {
		out[groupID] = make([]AccountIntent, 0, len(items))
		for _, item := range items {
			out[groupID] = append(out[groupID], AccountIntent{AccountID: item["account_id"], Priority: int(item["priority"])})
		}
	}
	return out
}

func accountIntentsToStorage(intents map[string][]AccountIntent) map[string][]map[string]int64 {
	out := make(map[string][]map[string]int64, len(intents))
	for groupID, items := range intents {
		out[groupID] = make([]map[string]int64, 0, len(items))
		for _, item := range items {
			out[groupID] = append(out[groupID], map[string]int64{"account_id": item.AccountID, "priority": int64(item.Priority)})
		}
	}
	return out
}

func accountPools(mapping Mapping, accounts []relay.Account) []TargetAccountPool {
	pools := make([]TargetAccountPool, 0, len(mapping.GroupIDs))
	for _, targetGroupID := range mapping.GroupIDs {
		key := strconv.FormatInt(targetGroupID, 10)
		pool := TargetAccountPool{TargetGroupID: targetGroupID, Current: []TargetAccount{}, Desired: append([]AccountIntent(nil), mapping.DesiredAccounts[key]...)}
		for _, account := range accounts {
			if !strings.EqualFold(strings.TrimSpace(account.Platform), strings.TrimSpace(mapping.Platform)) {
				continue
			}
			for _, relationship := range account.GroupRelationships {
				if relationship.GroupID != targetGroupID {
					continue
				}
				pool.Current = append(pool.Current, TargetAccount{ID: account.ID, Name: account.Name, Platform: account.Platform, Type: account.Type, Status: account.Status, Schedulable: account.Schedulable, Priority: relationship.Priority})
				break
			}
		}
		sort.SliceStable(pool.Current, func(i, j int) bool {
			if pool.Current[i].Priority == pool.Current[j].Priority {
				return pool.Current[i].ID < pool.Current[j].ID
			}
			return pool.Current[i].Priority < pool.Current[j].Priority
		})
		sort.SliceStable(pool.Desired, func(i, j int) bool {
			if pool.Desired[i].Priority == pool.Desired[j].Priority {
				return pool.Desired[i].AccountID < pool.Desired[j].AccountID
			}
			return pool.Desired[i].Priority < pool.Desired[j].Priority
		})
		if mapping.AccountManagementInitialized {
			current := make([]AccountIntent, len(pool.Current))
			for index, account := range pool.Current {
				current[index] = AccountIntent{AccountID: account.ID, Priority: account.Priority}
			}
			pool.Drift = !sameAccountIntents(current, pool.Desired)
		}
		pools = append(pools, pool)
	}
	return pools
}

func accountPoolWarnings(pools []TargetAccountPool, initialized bool) []string {
	groupsByAccount := make(map[int64][]int64)
	warnings := make([]string, 0)
	for _, pool := range pools {
		accountIDs := make(map[int64]struct{})
		if initialized {
			for _, account := range pool.Desired {
				accountIDs[account.AccountID] = struct{}{}
			}
		} else {
			for _, account := range pool.Current {
				accountIDs[account.ID] = struct{}{}
			}
		}
		if len(accountIDs) > 1 {
			warnings = append(warnings, fmt.Sprintf("target group %d has multiple Accounts", pool.TargetGroupID))
		}
		for accountID := range accountIDs {
			groupsByAccount[accountID] = append(groupsByAccount[accountID], pool.TargetGroupID)
		}
	}
	accountIDs := make([]int64, 0, len(groupsByAccount))
	for accountID := range groupsByAccount {
		accountIDs = append(accountIDs, accountID)
	}
	sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
	for _, accountID := range accountIDs {
		groupIDs := groupsByAccount[accountID]
		if len(groupIDs) < 2 {
			continue
		}
		sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
		labels := make([]string, len(groupIDs))
		for index, groupID := range groupIDs {
			labels[index] = strconv.FormatInt(groupID, 10)
		}
		warnings = append(warnings, fmt.Sprintf("account %d is reused across target groups %s", accountID, strings.Join(labels, ", ")))
	}
	return warnings
}

func sameAccountIntents(left, right []AccountIntent) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func mappingAvailabilityWarnings(mapping Mapping, groups []relay.Group) []string {
	if groups == nil {
		return nil
	}
	available := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		if strings.EqualFold(strings.TrimSpace(group.Platform), strings.TrimSpace(mapping.Platform)) {
			available[group.ID] = struct{}{}
		}
	}
	warnings := make([]string, 0)
	if mapping.TemplateGroupID > 0 {
		if _, ok := available[mapping.TemplateGroupID]; !ok {
			warnings = append(warnings, fmt.Sprintf("template group %d is unavailable", mapping.TemplateGroupID))
		}
	}
	if mapping.SourceGroupID > 0 {
		if _, ok := available[mapping.SourceGroupID]; !ok {
			warnings = append(warnings, fmt.Sprintf("migration source group %d is unavailable", mapping.SourceGroupID))
		}
	}
	for _, groupID := range mapping.GroupIDs {
		if groupID > 0 {
			if _, ok := available[groupID]; !ok {
				warnings = append(warnings, fmt.Sprintf("target group %d is unavailable", groupID))
			}
		}
	}
	return uniqueStrings(warnings)
}

type mappingRelationshipFacts struct {
	users                []relay.User
	localByRelay         map[int64]int
	activeGroupsByUserID map[int64][]int64
}

func loadMappingRelationshipFacts(ctx context.Context, client *ent.Client, provider relay.Provider) (*mappingRelationshipFacts, error) {
	if provider == nil || client == nil {
		return nil, nil
	}
	if _, ok := provider.(relay.UserRelationshipSnapshotReader); ok {
		snapshot, err := loadProviderRelationshipSnapshot(ctx, provider)
		if err != nil {
			return nil, fmt.Errorf("load provider relationship snapshot for mappings: %w", err)
		}
		return mappingRelationshipFactsFromSnapshot(ctx, client, snapshot)
	}
	subsLister, ok := provider.(relay.UserSubscriptionLister)
	directory, directoryOK := provider.(relay.UserDirectoryProvider)
	batchDirectory, batchOK := provider.(relay.UserSubscriptionDirectoryProvider)
	if !batchOK && (!ok || !directoryOK) {
		return nil, nil
	}
	var (
		users                []relay.User
		activeGroupsByUserID map[int64][]int64
		err                  error
	)
	if batchOK {
		users, activeGroupsByUserID, err = batchDirectory.ListUsersWithActiveSubscriptions(ctx)
	} else {
		users, err = directory.ListUsers(ctx)
	}
	if err != nil {
		return nil, err
	}
	localUsers, err := client.User.Query().Where(user.RelayUserIDNotNil()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load local Relay user bindings: %w", err)
	}
	localByRelay := make(map[int64]int, len(localUsers))
	for _, localUser := range localUsers {
		if localUser.RelayUserID != nil && *localUser.RelayUserID > 0 {
			localByRelay[int64(*localUser.RelayUserID)] = localUser.ID
		}
	}
	if !batchOK {
		type membershipResult struct {
			relayUserID int64
			groups      []int64
		}
		results := make([]membershipResult, len(users))
		jobs := make(chan int)
		workerCount := maxCandidateWorkers
		if len(users) < workerCount {
			workerCount = len(users)
		}
		var workers sync.WaitGroup
		workers.Add(workerCount)
		for worker := 0; worker < workerCount; worker++ {
			go func() {
				defer workers.Done()
				for index := range jobs {
					relayUser := users[index]
					subscriptions, listErr := subsLister.ListUserSubscriptions(ctx, relayUser.ID)
					if listErr != nil {
						continue
					}
					groups := make([]int64, 0)
					for _, subscription := range subscriptions {
						if strings.EqualFold(strings.TrimSpace(subscription.Status), "active") {
							groupID := subscription.GroupID
							if groupID <= 0 && subscription.Group != nil {
								groupID = subscription.Group.ID
							}
							if groupID > 0 {
								groups = append(groups, groupID)
							}
						}
					}
					results[index] = membershipResult{relayUserID: relayUser.ID, groups: groups}
				}
			}()
		}
		for index := range users {
			jobs <- index
		}
		close(jobs)
		workers.Wait()
		activeGroupsByUserID = make(map[int64][]int64, len(results))
		for _, result := range results {
			activeGroupsByUserID[result.relayUserID] = result.groups
		}
	}
	return &mappingRelationshipFacts{users: users, localByRelay: localByRelay, activeGroupsByUserID: activeGroupsByUserID}, nil
}

func mappingRelationshipFactsFromSnapshot(ctx context.Context, client *ent.Client, snapshot *providerRelationshipSnapshot) (*mappingRelationshipFacts, error) {
	localUsers, err := client.User.Query().Where(user.RelayUserIDNotNil()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load local Relay user bindings from relationship snapshot: %w", err)
	}
	facts := &mappingRelationshipFacts{
		users:                make([]relay.User, 0, len(snapshot.relationships)),
		localByRelay:         make(map[int64]int, len(localUsers)),
		activeGroupsByUserID: make(map[int64][]int64, len(snapshot.relationships)),
	}
	for _, localUser := range localUsers {
		if localUser.RelayUserID != nil && *localUser.RelayUserID > 0 {
			facts.localByRelay[int64(*localUser.RelayUserID)] = localUser.ID
		}
	}
	for _, relationship := range snapshot.relationships {
		facts.users = append(facts.users, relationship.User)
		facts.activeGroupsByUserID[relationship.User.ID] = relationship.ActiveSubscriptionGroupIDs()
	}
	return facts, nil
}

func mappingRelationshipWarnings(facts *mappingRelationshipFacts, mapping Mapping) []string {
	if facts == nil {
		return nil
	}
	activeGroups := make(map[int64]struct{}, len(mapping.GroupIDs))
	for _, groupID := range mapping.GroupIDs {
		activeGroups[groupID] = struct{}{}
	}
	expected := make(map[int]int64, len(mapping.MemberAssignments))
	for rawUserID, groupID := range mapping.MemberAssignments {
		if userID, parseErr := strconv.Atoi(rawUserID); parseErr == nil {
			expected[userID] = groupID
		}
	}
	actual := make(map[int]map[int64]struct{})
	warnings := make([]string, 0)
	for _, relayUser := range facts.users {
		groups := make([]int64, 0)
		for _, groupID := range facts.activeGroupsByUserID[relayUser.ID] {
			if _, managed := activeGroups[groupID]; managed {
				groups = append(groups, groupID)
			}
		}
		if len(groups) == 0 {
			continue
		}
		localID, known := facts.localByRelay[relayUser.ID]
		if !known {
			for _, groupID := range groups {
				if relayGroupAdopted(mapping.OperationState, relayUser.ID, groupID) {
					continue
				}
				warnings = append(warnings, fmt.Sprintf("unmanaged relay member %d in target group %d", relayUser.ID, groupID))
			}
			continue
		}
		actual[localID] = make(map[int64]struct{})
		for _, groupID := range groups {
			actual[localID][groupID] = struct{}{}
			if expectedGroup, expectedOK := expected[localID]; !expectedOK {
				warnings = append(warnings, fmt.Sprintf("unmanaged member %d in target group %d", localID, groupID))
			} else if expectedGroup != groupID {
				warnings = append(warnings, fmt.Sprintf("member %d is subscribed to target group %d instead of %d", localID, groupID, expectedGroup))
			}
		}
	}
	for userID, groupID := range expected {
		if _, exists := actual[userID][groupID]; !exists {
			warnings = append(warnings, fmt.Sprintf("mapping member %d is missing from target group %d", userID, groupID))
		}
	}
	return uniqueStrings(warnings)
}

func (s *Service) loadUnmanagedMembers(ctx context.Context, provider relay.Provider, requestFacts *planningRequestFacts, providerVersion int64, mapping *ent.RelayGroupMapping) ([]UnmanagedMember, error) {
	directory, directoryOK := provider.(relay.UserDirectoryProvider)
	subsLister, subsOK := provider.(relay.UserSubscriptionLister)
	batchDirectory, batchOK := provider.(relay.UserSubscriptionDirectoryProvider)
	if requestFacts.relationships == nil && !batchOK && (!directoryOK || !subsOK) {
		return nil, nil
	}
	var (
		remoteUsers        []relay.User
		activeGroupsByUser map[int64][]int64
		err                error
	)
	if requestFacts.relationships != nil {
		remoteUsers = make([]relay.User, 0, len(requestFacts.relationships.relationships))
		activeGroupsByUser = make(map[int64][]int64, len(requestFacts.relationships.relationships))
		for _, relationship := range requestFacts.relationships.relationships {
			remoteUsers = append(remoteUsers, relationship.User)
			activeGroupsByUser[relationship.User.ID] = relationship.ActiveSubscriptionGroupIDs()
		}
		batchOK = true
	} else if batchOK {
		remoteUsers, activeGroupsByUser, err = batchDirectory.ListUsersWithActiveSubscriptions(ctx)
	} else {
		remoteUsers, err = directory.ListUsers(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("list relay users: %w", redactProviderReadError(err))
	}
	localUsers, err := s.client.User.Query().Where(user.RelayUserIDNotNil()).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load local relay mappings: %w", err)
	}
	localByRelay := make(map[int64]struct{}, len(localUsers))
	for _, localUser := range localUsers {
		if localUser.RelayUserID != nil && *localUser.RelayUserID > 0 {
			localByRelay[int64(*localUser.RelayUserID)] = struct{}{}
		}
	}
	managedTargets := make(map[int64]struct{}, len(mapping.GroupIds))
	for _, groupID := range mapping.GroupIds {
		if groupID > 0 {
			managedTargets[groupID] = struct{}{}
		}
	}
	out := make([]UnmanagedMember, 0)
	relayIDs := make([]int64, 0)
	for _, remoteUser := range remoteUsers {
		if remoteUser.ID <= 0 {
			continue
		}
		if _, managed := localByRelay[remoteUser.ID]; managed {
			continue
		}
		targetIDs := make([]int64, 0)
		seen := make(map[int64]struct{})
		activeGroupIDs := activeGroupsByUser[remoteUser.ID]
		if !batchOK {
			subscriptions, listErr := subsLister.ListUserSubscriptions(ctx, remoteUser.ID)
			if listErr != nil {
				return nil, fmt.Errorf("list subscriptions for relay user %d: %w", remoteUser.ID, redactProviderReadError(listErr))
			}
			activeGroupIDs = make([]int64, 0, len(subscriptions))
			for _, subscription := range subscriptions {
				if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") {
					continue
				}
				groupID := subscription.GroupID
				if groupID <= 0 && subscription.Group != nil {
					groupID = subscription.Group.ID
				}
				activeGroupIDs = append(activeGroupIDs, groupID)
			}
		}
		for _, groupID := range activeGroupIDs {
			if _, managed := managedTargets[groupID]; !managed {
				continue
			}
			if _, exists := seen[groupID]; exists {
				continue
			}
			seen[groupID] = struct{}{}
			targetIDs = append(targetIDs, groupID)
		}
		adoptedTargetIDs := targetIDs[:0]
		for _, groupID := range targetIDs {
			if !relayGroupAdopted(mapping.OperationState, remoteUser.ID, groupID) {
				adoptedTargetIDs = append(adoptedTargetIDs, groupID)
			}
		}
		targetIDs = adoptedTargetIDs
		if len(targetIDs) == 0 {
			continue
		}
		sort.Slice(targetIDs, func(i, j int) bool { return targetIDs[i] < targetIDs[j] })
		out = append(out, UnmanagedMember{RelayUserID: remoteUser.ID, Username: remoteUser.Username, Email: remoteUser.Email, TargetGroupIDs: targetIDs})
		relayIDs = append(relayIDs, remoteUser.ID)
	}
	stats, statsErr := s.loadUsageStats(ctx, provider, mapping.ProviderID, providerVersion, relayIDs)
	if statsErr != nil {
		return nil, fmt.Errorf("load unmanaged member usage: %w", statsErr)
	}
	for index := range out {
		out[index].RangeCost = usageCost(stats[out[index].RelayUserID])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RelayUserID < out[j].RelayUserID })
	return out, nil
}

func cloneInt64Map(input map[string]int64) map[string]int64 {
	if len(input) == 0 {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneOperationState(input map[string]map[string]string) map[string]map[string]string {
	if len(input) == 0 {
		return map[string]map[string]string{}
	}
	out := make(map[string]map[string]string, len(input))
	for key, values := range input {
		out[key] = make(map[string]string, len(values))
		for field, value := range values {
			out[key][field] = value
		}
	}
	return out
}

func operationStatus(state map[string]map[string]string) string {
	if operation := state["operation"]; operation != nil && operation["status"] == "needs_retry" {
		return "needs_retry"
	}
	for key, entry := range state {
		if key == "operation" {
			continue
		}
		if entry["error"] != "" || entry["status"] == "failed" || entry["subscription"] == "failed" || entry["source_removal"] == "failed" {
			return "needs_retry"
		}
	}
	return "active"
}

func mergeOperationState(previous, current map[string]map[string]string) map[string]map[string]string {
	merged := cloneOperationState(previous)
	for key, values := range current {
		merged[key] = make(map[string]string, len(values))
		for field, value := range values {
			merged[key][field] = value
		}
	}
	if operation := merged["operation"]; operation != nil {
		operation["status"] = operationStatus(merged)
	}
	return merged
}

func operationStateNeedsRetry(state map[string]map[string]string, key string) bool {
	entry := state[key]
	if entry == nil {
		return false
	}
	return entry["error"] != "" || entry["status"] == "failed" || entry["subscription"] == "failed" || entry["source_removal"] == "failed" || strings.Contains(entry["api_keys"], ":failed:")
}

func relayGroupAdopted(state map[string]map[string]string, relayUserID, groupID int64) bool {
	entry := state[fmt.Sprintf("relay:%d:%d", relayUserID, groupID)]
	return entry != nil && entry["status"] == "adopted"
}

func allocate(candidates []Candidate, count int) []Assignment {
	if count < 1 {
		return make([]Assignment, 0)
	}
	assignments := make([]Assignment, count)
	for i := range assignments {
		assignments[i].Index = i
		assignments[i].UserIDs = make([]int, 0)
	}
	for _, candidate := range candidates {
		best := 0
		for i := 1; i < len(assignments); i++ {
			if assignments[i].TotalCost < assignments[best].TotalCost {
				best = i
			}
		}
		assignments[best].TotalCost += candidate.RangeCost
		assignments[best].UserIDs = append(assignments[best].UserIDs, candidate.UserID)
	}
	return assignments
}

func resolveGroupCount(req PreviewRequest, candidates []Candidate) (int, int) {
	if len(candidates) == 0 {
		if req.ExistingMappingID > 0 && req.GroupCount > 0 {
			return 0, req.GroupCount
		}
		return 0, 0
	}
	total := 0.0
	for _, candidate := range candidates {
		total += candidate.RangeCost
	}
	recommended := 1
	if req.WeeklyCostTarget > 0 && total > 0 {
		recommended = int(math.Ceil(total / req.WeeklyCostTarget))
	}
	if recommended > len(candidates) {
		recommended = len(candidates)
	}
	count := recommended
	if req.ExistingMappingID > 0 && req.GroupCount > 0 {
		count = req.GroupCount
	}
	return recommended, count
}

func (s *Service) assignTargets(ctx context.Context, req PreviewRequest, groups []relay.Group, departmentName string, assignments []Assignment) error {
	existingIDs := make([]int64, 0)
	if req.ExistingMappingID > 0 {
		mapping, err := s.client.RelayGroupMapping.Get(ctx, req.ExistingMappingID)
		if err != nil {
			return fmt.Errorf("load relay group mapping targets: %w", err)
		}
		existingIDs = mapping.GroupIds
	}
	groupByID := make(map[int64]relay.Group, len(groups))
	for _, group := range groups {
		groupByID[group.ID] = group
	}
	existingCount := len(existingIDs)
	if existingCount > len(assignments) {
		existingCount = len(assignments)
	}
	existingSuggestions := proposedExistingGroupNames(departmentName, req.Platform, groups, existingIDs)
	proposedNames := proposedGroupNames(departmentName, req.Platform, groups, len(assignments)-existingCount)
	for i := range assignments {
		if i < existingCount {
			groupID := existingIDs[i]
			currentName := groupByID[groupID].Name
			assignments[i].TargetGroupID = groupID
			assignments[i].CurrentTargetGroupName = currentName
			assignments[i].SuggestedTargetGroupName = existingSuggestions[groupID]
			if !assignments[i].RenameSelected {
				assignments[i].TargetGroupName = currentName
			} else if strings.TrimSpace(assignments[i].TargetGroupName) == "" {
				assignments[i].TargetGroupName = assignments[i].SuggestedTargetGroupName
			}
			continue
		}
		if strings.TrimSpace(assignments[i].TargetGroupName) == "" {
			assignments[i].TargetGroupName = proposedNames[i-existingCount]
		}
	}
	return nil
}

func proposedExistingGroupNames(departmentName, platform string, groups []relay.Group, existingIDs []int64) map[int64]string {
	used := make(map[string]int64, len(groups)+len(existingIDs))
	for _, group := range groups {
		used[group.Name] = group.ID
	}
	orderedIDs := append([]int64(nil), existingIDs...)
	sort.Slice(orderedIDs, func(i, j int) bool { return orderedIDs[i] < orderedIDs[j] })
	result := make(map[int64]string, len(orderedIDs))
	sequence := 1
	for _, groupID := range orderedIDs {
		for {
			name := proposedGroupName(departmentName, platform, sequence)
			sequence++
			if ownerID, exists := used[name]; exists && ownerID != groupID {
				continue
			}
			used[name] = groupID
			result[groupID] = name
			break
		}
	}
	return result
}

func proposedGroupNames(departmentName, platform string, groups []relay.Group, count int) []string {
	used := make(map[string]struct{}, len(groups)+count)
	for _, group := range groups {
		used[group.Name] = struct{}{}
	}
	names := make([]string, 0, count)
	sequence := 1
	for len(names) < count {
		for {
			name := proposedGroupName(departmentName, platform, sequence)
			sequence++
			if _, exists := used[name]; exists {
				continue
			}
			used[name] = struct{}{}
			names = append(names, name)
			break
		}
	}
	return names
}

func proposedGroupName(departmentName, platform string, sequence int) string {
	departmentName = normalizeTargetGroupName(departmentName)
	platform = strings.ToLower(strings.TrimSpace(platform))
	suffix := fmt.Sprintf("-%s-%02d", platform, sequence)
	base := []rune(departmentName)
	maxBase := maxGroupNameRunes - len([]rune(suffix))
	if len(base) > maxBase {
		base = base[:maxBase]
	}
	return string(base) + suffix
}

func normalizeTargetGroupName(name string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.TrimSpace(name))
}

func validateTargetGroupNames(assignments []Assignment, groups []relay.Group) error {
	owners := make(map[string]int64, len(groups))
	for _, group := range groups {
		owners[group.Name] = group.ID
	}
	seen := make(map[string]struct{}, len(assignments))
	for index := range assignments {
		if assignments[index].TargetUnavailable {
			continue
		}
		if strings.IndexFunc(assignments[index].TargetGroupName, unicode.IsControl) >= 0 {
			return fmt.Errorf("target %d name must not contain control characters", assignments[index].Index+1)
		}
		name := normalizeTargetGroupName(assignments[index].TargetGroupName)
		if name == "" {
			return fmt.Errorf("target %d name is required", assignments[index].Index+1)
		}
		if len([]rune(name)) > maxGroupNameRunes {
			return fmt.Errorf("target %d name must not exceed %d characters", assignments[index].Index+1, maxGroupNameRunes)
		}
		if ownerID := owners[name]; ownerID > 0 && ownerID != assignments[index].TargetGroupID {
			return fmt.Errorf("target group name %q is already in use", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("target group name %q is duplicated", name)
		}
		seen[name] = struct{}{}
		assignments[index].TargetGroupName = name
	}
	return nil
}

func candidateByUserID(candidates []Candidate, id int) *Candidate {
	for i := range candidates {
		if candidates[i].UserID == id {
			return &candidates[i]
		}
	}
	return nil
}

func selectedSet(ids []int) map[int]struct{} {
	set := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id > 0 {
			set[id] = struct{}{}
		}
	}
	return set
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
