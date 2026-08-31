package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/relayplanning"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type relayPlanningResolverFunc func(context.Context, int) (relay.Provider, error)

func (f relayPlanningResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type relayPlanningSearchProvider struct {
	relay.Provider
	users                     map[int64]*relay.User
	directoryUsers            []relay.User
	directoryError            error
	activeSubscriptionIDs     map[int64][]int64
	groups                    []relay.Group
	groupError                error
	pendingGroupError         error
	subscriptions             map[int64][]relay.UserSubscription
	keys                      map[int64][]relay.APIKey
	usage                     map[int64]relay.TeamUserUsageStats
	subscriptionError         error
	keyError                  error
	relationshipError         error
	relationshipReadbackError error
	allowedGroupsError        error
	assigned                  []string
	removed                   []string
	removeFailures            map[int64]error
	bound                     []string
	mutateWrites              bool
	writeAcksOnly             bool
	accounts                  []relay.Account
	accountError              error
	accountReads              int
	accountUpdates            int
	accountFailures           map[int64]error
	renameFailures            map[int64]error
	inactiveGroupIDs          map[int64]bool
	enforceAccountSnapshot    bool
	events                    []string
	subscriptionReads         atomic.Int64
	keyReads                  atomic.Int64
	directoryReads            atomic.Int64
	relationshipReads         atomic.Int64
	relationshipPageReads     atomic.Int64
	relationshipPages         [][]int64
	userReads                 atomic.Int64
	groupReads                atomic.Int64
	dependencyStarted         chan string
	dependencyRelease         chan struct{}
	renewalWrites             []relayPlanningRenewalWrite
	renewalFailures           map[int64]error
	renewalAmbiguous          map[int64]error
	renewalAppliedKeys        map[string]bool
	renewalMu                 sync.Mutex
	renewalLegacyExtends      atomic.Int64
	renewalDirectExtends      atomic.Int64
}

type relayPlanningFallbackProvider struct {
	relay.Provider
	backing *relayPlanningSearchProvider
}

type relayPlanningSub2API interface {
	relay.Provider
	relay.UserRelationshipSnapshotReader
	relay.UserSubscriptionLister
	relay.APIKeyGroupBinder
	AssignSubscriptionForUser(context.Context, int64, int64, int) error
	RemoveSubscriptionForUser(context.Context, int64, int64) error
}

type relayPlanningSub2APIProvider struct {
	relayPlanningSub2API
	facts *relayPlanningSearchProvider
}

func (p *relayPlanningSub2APIProvider) ListPlatformGroups(ctx context.Context) ([]relay.Group, error) {
	return p.facts.ListPlatformGroups(ctx)
}

func (p *relayPlanningSub2APIProvider) ListAccountsForPlatform(ctx context.Context, platform string) ([]relay.Account, error) {
	return p.facts.ListAccountsForPlatform(ctx, platform)
}

func (p *relayPlanningSub2APIProvider) SetAccountGroupRelationship(ctx context.Context, accountID, groupID int64, expected []relay.AccountGroupRelationship, desiredPriority *int) error {
	return p.facts.SetAccountGroupRelationship(ctx, accountID, groupID, expected, desiredPriority)
}

func (p *relayPlanningSub2APIProvider) UpdateGroupStatus(ctx context.Context, groupID int64, status string) error {
	return p.facts.UpdateGroupStatus(ctx, groupID, status)
}

func (p *relayPlanningSub2APIProvider) GetBatchUserUsageStats(ctx context.Context, userIDs []int64, params relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	return p.facts.GetBatchUserUsageStats(ctx, userIDs, params)
}

func (p *relayPlanningFallbackProvider) ListPlatformGroups(ctx context.Context) ([]relay.Group, error) {
	return p.backing.ListPlatformGroups(ctx)
}

func (p *relayPlanningFallbackProvider) ListAccountsForPlatform(ctx context.Context, platform string) ([]relay.Account, error) {
	return p.backing.ListAccountsForPlatform(ctx, platform)
}

func (p *relayPlanningFallbackProvider) ListUserSubscriptions(ctx context.Context, userID int64) ([]relay.UserSubscription, error) {
	return p.backing.ListUserSubscriptions(ctx, userID)
}

func (p *relayPlanningFallbackProvider) ListUsersWithActiveSubscriptions(ctx context.Context) ([]relay.User, map[int64][]int64, error) {
	return p.backing.ListUsersWithActiveSubscriptions(ctx)
}

func (p *relayPlanningFallbackProvider) GetBatchUserUsageStats(ctx context.Context, userIDs []int64, params relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	return p.backing.GetBatchUserUsageStats(ctx, userIDs, params)
}

type relayPlanningRenewalWrite struct {
	Action       string
	UserID       int64
	GroupID      int64
	Days         int
	OperationKey string
}

func TestRelayPlanningInitialEndpointsRejectExistingMapping(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-existing-mapping-test").
		SetDisplayName("Relay Planning Existing Mapping Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetGroupIds([]int64{101}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) {
		return provider, nil
	}), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/preview", handler.Preview)
	router.POST("/admin/relay-planning/execute", handler.Execute)

	requests := []struct {
		name    string
		path    string
		payload string
	}{
		{
			name:    "Preview",
			path:    "/admin/relay-planning/preview",
			payload: fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"weekly_cost_target":2500,"existing_mapping_id":%d}`, providerConfig.ID, mapping.ID),
		},
		{
			name:    "Execute",
			path:    "/admin/relay-planning/execute",
			payload: fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"weekly_cost_target":2500,"existing_mapping_id":%d,"operation_key":"existing-mapping-1","expected_relationship_fingerprint":"v2:reviewed"}`, providerConfig.ID, mapping.ID),
		},
	}
	for _, item := range requests {
		t.Run(item.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, item.path, strings.NewReader(item.payload))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusConflict {
				t.Fatalf("status = %d, want 409, body=%s", response.Code, response.Body.String())
			}
			var body struct {
				Details struct {
					ErrorCode string `json:"error_code"`
					MappingID int    `json:"mapping_id"`
				} `json:"details"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode conflict response: %v", err)
			}
			if body.Details.ErrorCode != "existing_mapping" || body.Details.MappingID != mapping.ID {
				t.Fatalf("details = %+v, want existing_mapping for mapping %d", body.Details, mapping.ID)
			}
		})
	}
	if len(provider.events) != 0 {
		t.Fatalf("existing Mapping conflict wrote Relay state: %v", provider.events)
	}
	if count := client.RelayGroupMapping.Query().CountX(ctx); count != 1 {
		t.Fatalf("mapping count = %d, want the existing Mapping only", count)
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if !reflect.DeepEqual(persisted.GroupIds, []int64{101}) {
		t.Fatalf("persisted target Groups = %v, want the original [101]", persisted.GroupIds)
	}
}

func (p *relayPlanningSearchProvider) GetUser(_ context.Context, userID int64) (*relay.User, error) {
	p.userReads.Add(1)
	return p.users[userID], nil
}

func (p *relayPlanningSearchProvider) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	p.groupReads.Add(1)
	p.waitForDependency("groups")
	if p.groupError != nil {
		return nil, p.groupError
	}
	groups := make([]relay.Group, 0, len(p.groups))
	for _, group := range p.groups {
		if !p.inactiveGroupIDs[group.ID] {
			groups = append(groups, group)
		}
	}
	return groups, nil
}

func (p *relayPlanningSearchProvider) GetGroup(_ context.Context, groupID int64) (*relay.Group, error) {
	if p.pendingGroupError != nil {
		return nil, p.pendingGroupError
	}
	for index := range p.groups {
		if p.groups[index].ID == groupID {
			group := p.groups[index]
			return &group, nil
		}
	}
	return nil, fmt.Errorf("group %d not found", groupID)
}

func (p *relayPlanningSearchProvider) ListUsers(context.Context) ([]relay.User, error) {
	p.directoryReads.Add(1)
	if p.directoryError != nil {
		return nil, p.directoryError
	}
	return append([]relay.User(nil), p.directoryUsers...), nil
}

func (p *relayPlanningSearchProvider) ListUsersWithActiveSubscriptions(context.Context) ([]relay.User, map[int64][]int64, error) {
	p.directoryReads.Add(1)
	if p.directoryError != nil {
		return nil, nil, p.directoryError
	}
	users := append([]relay.User(nil), p.directoryUsers...)
	groups := make(map[int64][]int64, len(p.activeSubscriptionIDs))
	for userID, groupIDs := range p.activeSubscriptionIDs {
		groups[userID] = append([]int64(nil), groupIDs...)
	}
	return users, groups, nil
}

func (p *relayPlanningSearchProvider) ListUserRelationships(context.Context) ([]relay.UserRelationship, error) {
	p.relationshipReads.Add(1)
	p.waitForDependency("relationships")
	if p.relationshipError != nil {
		return nil, p.relationshipError
	}
	if p.relationshipReadbackError != nil && (len(p.assigned) > 0 || len(p.removed) > 0 || len(p.bound) > 0) {
		return nil, p.relationshipReadbackError
	}
	if p.subscriptionError != nil {
		return nil, p.subscriptionError
	}
	if len(p.relationshipPages) > 0 {
		relationships := make([]relay.UserRelationship, 0, len(p.users))
		for _, page := range p.relationshipPages {
			p.relationshipPageReads.Add(1)
			for _, userID := range page {
				if user := p.users[userID]; user != nil {
					relationships = append(relationships, relay.UserRelationship{User: *user, Subscriptions: append([]relay.UserSubscription(nil), p.subscriptions[userID]...)})
				}
			}
		}
		return relationships, nil
	}
	p.relationshipPageReads.Add(1)
	if len(p.users) == 0 && len(p.directoryUsers) > 0 {
		relationships := make([]relay.UserRelationship, 0, len(p.directoryUsers))
		for _, user := range p.directoryUsers {
			subscriptions := make([]relay.UserSubscription, 0, len(p.activeSubscriptionIDs[user.ID]))
			for _, groupID := range p.activeSubscriptionIDs[user.ID] {
				subscriptions = append(subscriptions, relay.UserSubscription{UserID: user.ID, GroupID: groupID, Status: "active"})
			}
			relationships = append(relationships, relay.UserRelationship{User: user, Subscriptions: subscriptions})
		}
		return relationships, nil
	}
	userIDs := make([]int64, 0, len(p.users))
	for userID := range p.users {
		userIDs = append(userIDs, userID)
	}
	sort.Slice(userIDs, func(i, j int) bool { return userIDs[i] < userIDs[j] })
	relationships := make([]relay.UserRelationship, 0, len(userIDs))
	for _, userID := range userIDs {
		user := *p.users[userID]
		relationships = append(relationships, relay.UserRelationship{User: user, Subscriptions: append([]relay.UserSubscription(nil), p.subscriptions[userID]...)})
	}
	return relationships, nil
}

func (p *relayPlanningSearchProvider) ListUserSubscriptions(_ context.Context, userID int64) ([]relay.UserSubscription, error) {
	p.subscriptionReads.Add(1)
	if p.subscriptionError != nil {
		return nil, p.subscriptionError
	}
	return append([]relay.UserSubscription(nil), p.subscriptions[userID]...), nil
}

func (p *relayPlanningSearchProvider) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	if p.allowedGroupsError != nil {
		return nil, p.allowedGroupsError
	}
	return nil, nil
}

func (p *relayPlanningSearchProvider) ListUserAPIKeys(_ context.Context, userID int64) ([]relay.APIKey, error) {
	p.keyReads.Add(1)
	if p.keyError != nil {
		return nil, p.keyError
	}
	return append([]relay.APIKey(nil), p.keys[userID]...), nil
}

func (p *relayPlanningSearchProvider) GetBatchUserUsageStats(_ context.Context, userIDs []int64, _ relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	result := make(map[int64]relay.TeamUserUsageStats)
	for _, userID := range userIDs {
		if item, ok := p.usage[userID]; ok {
			result[userID] = item
		}
	}
	return result, nil
}

func (p *relayPlanningSearchProvider) DuplicateGroup(context.Context, int64, string) (*relay.Group, error) {
	p.events = append(p.events, "duplicate:100")
	if p.inactiveGroupIDs == nil {
		p.inactiveGroupIDs = make(map[int64]bool)
	}
	p.inactiveGroupIDs[100] = true
	for index := range p.groups {
		if p.groups[index].ID == 100 {
			return &p.groups[index], nil
		}
	}
	p.groups = append(p.groups, relay.Group{ID: 100, Name: "Group Alpha Copy", Platform: "openai"})
	return &p.groups[len(p.groups)-1], nil
}

func (p *relayPlanningSearchProvider) RenameGroup(_ context.Context, groupID int64, name string) (*relay.Group, error) {
	p.events = append(p.events, fmt.Sprintf("rename:%d:%s", groupID, name))
	if err := p.renameFailures[groupID]; err != nil {
		return nil, err
	}
	for index := range p.groups {
		if p.groups[index].ID == groupID {
			p.groups[index].Name = name
			return &p.groups[index], nil
		}
	}
	p.groups = append(p.groups, relay.Group{ID: groupID, Name: name, Platform: "openai"})
	return &p.groups[len(p.groups)-1], nil
}

func (p *relayPlanningSearchProvider) UpdateGroupStatus(_ context.Context, groupID int64, status string) error {
	p.events = append(p.events, fmt.Sprintf("group-status:%d:%s", groupID, status))
	if p.inactiveGroupIDs == nil {
		p.inactiveGroupIDs = make(map[int64]bool)
	}
	if status == "active" {
		delete(p.inactiveGroupIDs, groupID)
	} else {
		p.inactiveGroupIDs[groupID] = true
	}
	return nil
}

func (p *relayPlanningSearchProvider) AssignSubscriptionForUser(_ context.Context, userID, groupID int64, validityDays int) error {
	p.assigned = append(p.assigned, fmt.Sprintf("%d:%d:%d", userID, groupID, validityDays))
	p.events = append(p.events, fmt.Sprintf("subscription-add:%d:%d", userID, groupID))
	if p.writeAcksOnly || !p.mutateWrites {
		return nil
	}
	if p.subscriptions == nil {
		p.subscriptions = make(map[int64][]relay.UserSubscription)
	}
	for index := range p.subscriptions[userID] {
		if p.subscriptions[userID][index].GroupID == groupID {
			p.subscriptions[userID][index].Status = "active"
			return nil
		}
	}
	p.subscriptions[userID] = append(p.subscriptions[userID], relay.UserSubscription{ID: userID*1000 + groupID, UserID: userID, GroupID: groupID, Status: "active"})
	return nil
}

func (p *relayPlanningSearchProvider) AssignSubscriptionForUserWithOperationKey(_ context.Context, userID, groupID int64, days int, operationKey string) error {
	return p.applyRenewalWrite("create", userID, groupID, days, operationKey)
}

func (p *relayPlanningSearchProvider) ExtendSubscriptionForUserWithOperationKey(_ context.Context, userID, groupID int64, days int, operationKey string) error {
	p.renewalLegacyExtends.Add(1)
	return p.applyRenewalWrite("extend", userID, groupID, days, operationKey)
}

func (p *relayPlanningSearchProvider) ExtendSubscriptionByIDWithOperationKey(_ context.Context, subscriptionID int64, days int, operationKey string) error {
	p.renewalDirectExtends.Add(1)
	p.renewalMu.Lock()
	var userID, groupID int64
	for currentUserID, subscriptions := range p.subscriptions {
		for _, subscription := range subscriptions {
			if subscription.ID == subscriptionID {
				userID, groupID = currentUserID, subscription.GroupID
				break
			}
		}
	}
	p.renewalMu.Unlock()
	if userID <= 0 || groupID <= 0 {
		return fmt.Errorf("reviewed subscription %d not found", subscriptionID)
	}
	return p.applyRenewalWrite("extend", userID, groupID, days, operationKey)
}

func (p *relayPlanningSearchProvider) applyRenewalWrite(action string, userID, groupID int64, days int, operationKey string) error {
	p.renewalMu.Lock()
	defer p.renewalMu.Unlock()
	p.renewalWrites = append(p.renewalWrites, relayPlanningRenewalWrite{Action: action, UserID: userID, GroupID: groupID, Days: days, OperationKey: operationKey})
	if p.renewalAppliedKeys == nil {
		p.renewalAppliedKeys = make(map[string]bool)
	}
	if p.renewalAppliedKeys[operationKey] {
		return nil
	}
	if err := p.renewalFailures[userID]; err != nil {
		return err
	}
	now := time.Now().UTC()
	if action == "create" {
		p.subscriptions[userID] = append(p.subscriptions[userID], relay.UserSubscription{ID: int64(1000 + len(p.renewalAppliedKeys)), UserID: userID, GroupID: groupID, Status: "active", ExpiresAt: now.AddDate(0, 0, days)})
	} else {
		for index := range p.subscriptions[userID] {
			subscription := &p.subscriptions[userID][index]
			if subscription.GroupID != groupID {
				continue
			}
			base := subscription.ExpiresAt
			if !base.After(now) {
				base = now
			}
			subscription.ExpiresAt = base.AddDate(0, 0, days)
			subscription.Status = "active"
			break
		}
	}
	p.renewalAppliedKeys[operationKey] = true
	if err := p.renewalAmbiguous[userID]; err != nil {
		return err
	}
	return nil
}

func (p *relayPlanningSearchProvider) RemoveSubscriptionForUser(_ context.Context, userID, groupID int64) error {
	p.removed = append(p.removed, fmt.Sprintf("%d:%d", userID, groupID))
	p.events = append(p.events, fmt.Sprintf("subscription-remove:%d:%d", userID, groupID))
	if err := p.removeFailures[groupID]; err != nil {
		return err
	}
	if p.writeAcksOnly || !p.mutateWrites {
		return nil
	}
	remaining := p.subscriptions[userID][:0]
	for _, subscription := range p.subscriptions[userID] {
		if subscription.GroupID != groupID {
			remaining = append(remaining, subscription)
		}
	}
	p.subscriptions[userID] = remaining
	return nil
}

func (p *relayPlanningSearchProvider) BindAPIKeyToGroup(_ context.Context, keyID, groupID int64) error {
	p.bound = append(p.bound, fmt.Sprintf("%d:%d", keyID, groupID))
	p.events = append(p.events, fmt.Sprintf("api-key:%d:%d", keyID, groupID))
	if p.writeAcksOnly || !p.mutateWrites {
		return nil
	}
	for userID := range p.keys {
		for index := range p.keys[userID] {
			if p.keys[userID][index].ID == keyID {
				p.keys[userID][index].GroupID = groupID
				return nil
			}
		}
	}
	return nil
}

func (p *relayPlanningSearchProvider) ListAccountsForPlatform(context.Context, string) ([]relay.Account, error) {
	p.accountReads++
	p.waitForDependency("accounts")
	if p.accountError != nil {
		return nil, p.accountError
	}
	accounts := make([]relay.Account, len(p.accounts))
	for index := range p.accounts {
		accounts[index] = p.accounts[index]
		accounts[index].GroupRelationships = append([]relay.AccountGroupRelationship(nil), p.accounts[index].GroupRelationships...)
	}
	return accounts, nil
}

func (p *relayPlanningSearchProvider) waitForDependency(name string) {
	if p.dependencyStarted == nil || p.dependencyRelease == nil {
		return
	}
	p.dependencyStarted <- name
	<-p.dependencyRelease
}

func (p *relayPlanningSearchProvider) SetAccountGroupRelationship(_ context.Context, accountID, groupID int64, expected []relay.AccountGroupRelationship, desiredPriority *int) error {
	p.accountUpdates++
	priority := 0
	if desiredPriority != nil {
		priority = *desiredPriority
	}
	p.events = append(p.events, fmt.Sprintf("account:%d:%d:%d", accountID, groupID, priority))
	if err := p.accountFailures[groupID]; err != nil {
		return err
	}
	for index := range p.accounts {
		if p.accounts[index].ID != accountID {
			continue
		}
		if p.enforceAccountSnapshot && fmt.Sprint(p.accounts[index].GroupRelationships) != fmt.Sprint(expected) {
			return fmt.Errorf("synthetic stale Account snapshot")
		}
		relationships := p.accounts[index].GroupRelationships[:0]
		for _, relationship := range p.accounts[index].GroupRelationships {
			if relationship.GroupID != groupID {
				relationships = append(relationships, relationship)
			}
		}
		if desiredPriority != nil {
			relationships = append(relationships, relay.AccountGroupRelationship{GroupID: groupID, Priority: *desiredPriority})
		}
		p.accounts[index].GroupRelationships = relationships
	}
	return nil
}

func TestRelayPlanningSearchUsersPaginatesAndDisablesInvalidRelayMappings(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-test").
		SetDisplayName("Relay Planning Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("example-model").
		SetEnabled(true).
		SaveX(ctx)
	validRelayID := 42
	staleRelayID := 99
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(validRelayID).SaveX(ctx)
	bob := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").SaveX(ctx)
	carol := client.User.Create().SetUsername("carol").SetEmail("carol@example.net").SetAuthSource("ldap").SetRelayUserID(staleRelayID).SaveX(ctx)
	client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).
		SaveX(ctx)

	provider := &relayPlanningSearchProvider{users: map[int64]*relay.User{
		int64(validRelayID): {ID: int64(validRelayID), Username: "alice", Email: "alice@example.com"},
	}}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != providerConfig.ID {
			return nil, fmt.Errorf("unexpected provider %d", providerID)
		}
		return provider, nil
	}), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.GET("/admin/relay-planning/users", handler.SearchUsers)

	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/relay-planning/users?provider_id=%d&platform=openai&page=1&page_size=2", providerConfig.ID), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			Items []relayplanning.UserSearchItem `json:"items"`
			Total int                            `json:"total"`
			Page  int                            `json:"page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.Total != 3 || body.Data.Page != 1 || len(body.Data.Items) != 2 {
		t.Fatalf("page = %+v, want first two of three users", body.Data)
	}
	if got := body.Data.Items[0]; got.UserID != alice.ID || !got.Selectable || got.DisabledReason != "" || len(got.ManagedAssignments) != 1 || got.ManagedAssignments[0].TargetGroupID != 101 {
		t.Fatalf("alice = %+v, want selectable current-Provider mapping", got)
	}
	if got := body.Data.Items[1]; got.UserID != bob.ID || got.Selectable || got.DisabledReason != "no relay mapping for the selected provider" {
		t.Fatalf("bob = %+v, want visible disabled missing mapping", got)
	}

	request = httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/relay-planning/users?provider_id=%d&platform=openai&q=%d", providerConfig.ID, staleRelayID), nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("stale search status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode stale response: %v", err)
	}
	if len(body.Data.Items) != 1 || body.Data.Items[0].UserID != carol.ID || body.Data.Items[0].Selectable || body.Data.Items[0].DisabledReason != "relay mapping is not valid for the selected provider" {
		t.Fatalf("carol = %+v, want visible disabled stale mapping", body.Data.Items)
	}
}

func TestRelayPlanningPreviewAllowsTargetOnlyExternalUserAndKeepsMissingUsageUnknown(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-preview-test").
		SetDisplayName("Relay Planning Preview Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("example-model").
		SetEnabled(true).
		SaveX(ctx)

	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	carol := client.User.Create().SetUsername("carol").SetEmail("carol@example.net").SetAuthSource("ldap").SetRelayUserID(43).SaveX(ctx)
	member := client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alice").
		SetEmailNormalized(alice.Email).
		SetDisplayName(alice.Username).
		SetDepartmentExternalID("dept-alpha").
		SetMatchedUserID(alice.ID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	client.DirectoryMemberDepartment.Create().
		SetSourceID(source.ID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID("dept-alpha").
		SetLastSeenRunID(run.ID).
		SaveX(ctx)

	cost := 1000.0
	tokens := int64(100)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{
			42: {ID: 42, Username: "alice", Email: alice.Email},
			43: {ID: 43, Username: "carol", Email: carol.Email},
		},
		groups:        []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}},
		subscriptions: map[int64][]relay.UserSubscription{42: {}, 43: {}},
		usage: map[int64]relay.TeamUserUsageStats{
			42: {UserID: 42, RangeActualCost: &cost, RangeTotalTokens: &tokens},
		},
		relationshipError: errors.New("initial Preview must not load a provider-wide relationship snapshot"),
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != providerConfig.ID {
			return nil, fmt.Errorf("unexpected provider %d", providerID)
		}
		return provider, nil
	}), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/preview", handler.Preview)

	payload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":0,"weekly_cost_target":2500,"selected_user_ids":[%d,%d]}`, providerConfig.ID, alice.ID, carol.ID)
	request := httptest.NewRequest(http.MethodPost, "/admin/relay-planning/preview", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if provider.relationshipReads.Load() != 0 {
		t.Fatalf("initial Preview relationship snapshot reads = %d, want 0", provider.relationshipReads.Load())
	}
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Data.SourceGroupID != 0 || len(body.Data.Candidates) != 2 || len(body.Data.Assignments) != 1 {
		t.Fatalf("plan = %+v, want source-free plan with two candidates and one target", body.Data)
	}
	unknown := body.Data.Candidates[1]
	if unknown.UserID != carol.ID || unknown.UsageKnown || !unknown.Eligible || !unknown.CanAdd {
		t.Fatalf("external candidate = %+v, want selectable target-only user with unknown usage", unknown)
	}
	if body.Data.Assignments[0].TotalCost != 1000 {
		t.Fatalf("assignment total = %v, want only known usage 1000", body.Data.Assignments[0].TotalCost)
	}
	if body.Data.Assignments[0].TargetGroupName != "Department Alpha-openai-01" {
		t.Fatalf("target name = %q, want department-based suggestion", body.Data.Assignments[0].TargetGroupName)
	}
	if !containsRelayPlanningWarning(body.Data.Warnings, "30-day usage is unknown; capacity may be underestimated") {
		t.Fatalf("warnings = %v, want unknown-usage capacity warning", body.Data.Warnings)
	}
	if containsRelayPlanningWarning(body.Data.Warnings, "user is not a member of the selected source group") {
		t.Fatalf("warnings = %v, source membership must not be required without a source", body.Data.Warnings)
	}

	editedPayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":0,"weekly_cost_target":2500,"selected_user_ids":[%d,%d],"assignments":[{"index":0,"target_group_name":"Custom Target","user_ids":[%d,%d]},{"index":1,"user_ids":[]},{"index":2,"user_ids":[]},{"index":3,"user_ids":[]}]}`, providerConfig.ID, alice.ID, carol.ID, alice.ID, carol.ID)
	request = httptest.NewRequest(http.MethodPost, "/admin/relay-planning/preview", strings.NewReader(editedPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("edited target count status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode edited target count response: %v", err)
	}
	if body.Data.GroupCount != 4 || len(body.Data.Assignments) != 4 {
		t.Fatalf("edited target count = %d/%d, want 4/4", body.Data.GroupCount, len(body.Data.Assignments))
	}
	if body.Data.Assignments[0].TargetGroupName != "Custom Target" {
		t.Fatalf("reviewed target name = %q, want administrator edit preserved", body.Data.Assignments[0].TargetGroupName)
	}
}

func TestRelayPlanningPreviewRejectsOccupiedTargetName(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-name-validation-test").
		SetDisplayName("Relay Planning Name Validation Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	cost := 10.0
	tokens := int64(100)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}},
		subscriptions: map[int64][]relay.UserSubscription{42: {}},
		usage:         map[int64]relay.TeamUserUsageStats{42: {UserID: 42, RangeActualCost: &cost, RangeTotalTokens: &tokens}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/preview", handler.Preview)

	payload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"weekly_cost_target":2500,"selected_user_ids":[%d],"assignments":[{"index":0,"target_group_name":"Group Alpha","user_ids":[%d]}]}`, providerConfig.ID, alice.ID, alice.ID)
	request := httptest.NewRequest(http.MethodPost, "/admin/relay-planning/preview", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "already in use") {
		t.Fatalf("status/body = %d/%s, want occupied target name rejected", response.Code, response.Body.String())
	}
}

func TestRelayPlanningExecuteUsesOnlyEachUsersExplicitSource(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-execute-test").
		SetDisplayName("Relay Planning Execute Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("example-model").
		SetEnabled(true).
		SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	carol := client.User.Create().SetUsername("carol").SetEmail("carol@example.net").SetAuthSource("ldap").SetRelayUserID(43).SaveX(ctx)
	for index, local := range []*ent.User{alice, carol} {
		member := client.DirectoryMember.Create().
			SetSourceID(source.ID).
			SetExternalID(fmt.Sprintf("member-%d", index+1)).
			SetEmailNormalized(local.Email).
			SetDisplayName(local.Username).
			SetDepartmentExternalID("dept-alpha").
			SetMatchedUserID(local.ID).
			SetLastSeenRunID(run.ID).
			SaveX(ctx)
		client.DirectoryMemberDepartment.Create().
			SetSourceID(source.ID).
			SetDirectoryMemberID(member.ID).
			SetMemberExternalID(member.ExternalID).
			SetMemberEmailNormalized(member.EmailNormalized).
			SetDepartmentExternalID("dept-alpha").
			SetLastSeenRunID(run.ID).
			SaveX(ctx)
	}
	cost := 10.0
	tokens := int64(100)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{
			42: {ID: 42, Username: "alice", Email: alice.Email},
			43: {ID: 43, Username: "carol", Email: carol.Email},
		},
		groups: []relay.Group{
			{ID: 10, Name: "Group Alpha", Platform: "openai"},
			{ID: 20, Name: "Group Beta", Platform: "openai"},
			{ID: 30, Name: "Group Gamma", Platform: "openai"},
		},
		accounts: []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 10, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{
			42: {{UserID: 42, GroupID: 20, Status: "active"}},
			43: {{UserID: 43, GroupID: 30, Status: "active"}},
		},
		keys: map[int64][]relay.APIKey{
			42: {{ID: 501, UserID: 42, GroupID: 20, Status: "active"}, {ID: 502, UserID: 42, GroupID: 30, Status: "active"}},
			43: {{ID: 503, UserID: 43, GroupID: 30, Status: "active"}},
		},
		usage: map[int64]relay.TeamUserUsageStats{
			42: {UserID: 42, RangeActualCost: &cost, RangeTotalTokens: &tokens},
			43: {UserID: 43, RangeActualCost: &cost, RangeTotalTokens: &tokens},
		},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/preview", handler.Preview)
	router.POST("/admin/relay-planning/execute", handler.Execute)
	previewPayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":0,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d,%d],"assignments":[{"index":0,"user_ids":[%d,%d]}],"member_sources":{"%d":20,"%d":0}}`, providerConfig.ID, alice.ID, carol.ID, alice.ID, carol.ID, alice.ID, carol.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, "/admin/relay-planning/preview", previewPayload)
	payload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":0,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d,%d],"assignments":[{"index":0,"user_ids":[%d,%d]}],"member_sources":{"%d":20,"%d":0},"expected_relationship_fingerprint":%q,"operation_key":"operation-1"}`, providerConfig.ID, alice.ID, carol.ID, alice.ID, carol.ID, alice.ID, carol.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, "/admin/relay-planning/execute", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if fmt.Sprint(provider.assigned) != "[42:100:365 43:100:365]" {
		t.Fatalf("assigned = %v, want both users added to target for 365 days", provider.assigned)
	}
	if fmt.Sprint(provider.bound) != "[501:100]" || fmt.Sprint(provider.removed) != "[42:20]" {
		t.Fatalf("bound/removed = %v / %v, want only Alice's selected Source relationship changed", provider.bound, provider.removed)
	}
	mapping := client.RelayGroupMapping.Query().OnlyX(ctx)
	if mapping.SourceGroupID != 0 || mapping.MemberSources[fmt.Sprint(alice.ID)] != 20 {
		t.Fatalf("mapping source state = source:%d members:%v, want optional plan source and Alice source 20", mapping.SourceGroupID, mapping.MemberSources)
	}
	if _, exists := mapping.MemberSources[fmt.Sprint(carol.ID)]; exists {
		t.Fatalf("target-only Carol unexpectedly has persisted source: %v", mapping.MemberSources)
	}
}

func TestRelayPlanningCreateInheritsTemplateAccountsAndAppliesReviewedOverride(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-account-create-test").
		SetDisplayName("Relay Planning Account Create Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	member := client.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID("member-alice").SetEmailNormalized(alice.Email).SetDisplayName(alice.Username).SetDepartmentExternalID("dept-alpha").SetMatchedUserID(alice.ID).SetLastSeenRunID(run.ID).SaveX(ctx)
	client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(member.ID).SetMemberExternalID(member.ExternalID).SetMemberEmailNormalized(member.EmailNormalized).SetDepartmentExternalID("dept-alpha").SetLastSeenRunID(run.ID).SaveX(ctx)
	cost := 10.0
	tokens := int64(100)
	provider := &relayPlanningSearchProvider{
		users:  map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups: []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 20, Name: "Group Source", Platform: "openai"}},
		accounts: []relay.Account{
			{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 10, Priority: 1}}},
			{ID: 12, Name: "Account Beta", Platform: "openai", Type: "apikey", Status: "error", Schedulable: false, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 999, Priority: 1}, {GroupID: 10, Priority: 1}}},
		},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 20, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 20, Status: "active"}}},
		usage:         map[int64]relay.TeamUserUsageStats{42: {UserID: 42, RangeActualCost: &cost, RangeTotalTokens: &tokens}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/preview", handler.Preview)
	router.POST("/admin/relay-planning/execute", handler.Execute)
	previewPayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":20,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}]}`, providerConfig.ID, alice.ID, alice.ID)
	request := httptest.NewRequest(http.MethodPost, "/admin/relay-planning/preview", strings.NewReader(previewPayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("default preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var previewBody struct {
		Data struct {
			Assignments []struct {
				DesiredAccounts []relayplanning.AccountIntent `json:"desired_accounts"`
				Accounts        []relayplanning.TargetAccount `json:"accounts"`
			} `json:"assignments"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &previewBody); err != nil {
		t.Fatalf("decode default preview response: %v", err)
	}
	if len(previewBody.Data.Assignments) != 1 || len(previewBody.Data.Assignments[0].DesiredAccounts) != 2 || len(previewBody.Data.Assignments[0].Accounts) != 2 {
		t.Fatalf("default preview Accounts = %+v, want both inherited Template Accounts", previewBody.Data.Assignments)
	}
	if previewBody.Data.Assignments[0].DesiredAccounts[0].Priority != 1 || previewBody.Data.Assignments[0].DesiredAccounts[1].Priority != 1 {
		t.Fatalf("default preview priorities = %+v, want duplicate Relay priority 1 preserved", previewBody.Data.Assignments[0].DesiredAccounts)
	}

	reviewedPreviewPayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":20,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"target_group_name":"Reviewed Target","user_ids":[%d],"desired_accounts":[{"account_id":12,"priority":1}]}]}`, providerConfig.ID, alice.ID, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, "/admin/relay-planning/preview", reviewedPreviewPayload)
	provider.groups = append(provider.groups, relay.Group{ID: 99, Name: "Reviewed Target", Platform: "openai"})
	claimedNamePayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":20,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"target_group_name":"Reviewed Target","user_ids":[%d],"desired_accounts":[{"account_id":12,"priority":1}]}],"expected_relationship_fingerprint":%q,"operation_key":"create-accounts-claimed-name"}`, providerConfig.ID, alice.ID, alice.ID, fingerprint)
	request = httptest.NewRequest(http.MethodPost, "/admin/relay-planning/execute", strings.NewReader(claimedNamePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "stale_relay_plan") || len(provider.events) != 0 {
		t.Fatalf("claimed name status/events = %d/%v, want stale 409 and no Relay writes, body=%s", response.Code, provider.events, response.Body.String())
	}
	provider.groups = provider.groups[:len(provider.groups)-1]
	stalePayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":20,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d],"desired_accounts":[{"account_id":11,"priority":1}]}],"expected_relationship_fingerprint":%q,"operation_key":"create-accounts-stale"}`, providerConfig.ID, alice.ID, alice.ID, fingerprint)
	request = httptest.NewRequest(http.MethodPost, "/admin/relay-planning/execute", strings.NewReader(stalePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || len(provider.events) != 0 {
		t.Fatalf("stale reviewed Accounts status/events = %d/%v, want 409 and no Relay writes, body=%s", response.Code, provider.events, response.Body.String())
	}

	payload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":20,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"target_group_name":"Reviewed Target","user_ids":[%d],"desired_accounts":[{"account_id":12,"priority":1}]}],"expected_relationship_fingerprint":%q,"operation_key":"create-accounts-1"}`, providerConfig.ID, alice.ID, alice.ID, fingerprint)
	request = httptest.NewRequest(http.MethodPost, "/admin/relay-planning/execute", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantPrefix := []string{"duplicate:100", "rename:100:Reviewed Target", "account:12:100:1", "group-status:100:active", "subscription-add:42:100"}
	if len(provider.events) < len(wantPrefix) || fmt.Sprint(provider.events[:len(wantPrefix)]) != fmt.Sprint(wantPrefix) {
		t.Fatalf("creation events = %v, want prefix %v", provider.events, wantPrefix)
	}
	var body struct {
		Data struct {
			Groups  []relayplanning.GroupResult `json:"groups"`
			Mapping *relayplanning.Mapping      `json:"mapping"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode creation response: %v", err)
	}
	if body.Data.Mapping == nil || len(body.Data.Mapping.AccountPools) != 1 || len(body.Data.Mapping.AccountPools[0].Current) != 1 || body.Data.Mapping.AccountPools[0].Current[0].ID != 12 {
		t.Fatalf("creation Account readback = %+v, want only reviewed Account 12 bound to the new Target", body.Data.Mapping)
	}
	if len(body.Data.Groups) != 1 || body.Data.Groups[0].Name != "Reviewed Target" || body.Data.Groups[0].Rename != "succeeded" {
		t.Fatalf("creation Group result = %+v, want reviewed name and successful rename", body.Data.Groups)
	}
	persisted := client.RelayGroupMapping.Query().OnlyX(ctx)
	if !persisted.AccountManagementInitialized {
		t.Fatal("new mapping did not initialize Account management")
	}
	gotDesired := persisted.DesiredAccounts["100"]
	if len(gotDesired) != 1 || gotDesired[0]["account_id"] != 12 || gotDesired[0]["priority"] != 1 {
		t.Fatalf("new mapping desired accounts = %+v, want reviewed Account 12/1", gotDesired)
	}
}

func TestRelayPlanningAdoptCurrentAccountsInitializesDesiredStateWithoutRelayWrite(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-account-test").
		SetDisplayName("Relay Planning Account Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetGroupIds([]int64{101, 102}).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		groups: []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 101, Name: "Group Beta", Platform: "openai"}, {ID: 102, Name: "Group Gamma", Platform: "openai"}},
		accounts: []relay.Account{
			{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 999, Priority: 1}, {GroupID: 101, Priority: 2}, {GroupID: 102, Priority: 1}}},
			{ID: 12, Name: "Account Beta", Platform: "openai", Type: "apikey", Status: "error", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}},
			{ID: 13, Name: "Other Platform", Platform: "anthropic", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}},
		},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.GET("/admin/relay-planning/mappings", handler.ListMappings)
	router.POST("/admin/relay-planning/mappings/:id/accounts/adopt", handler.AdoptCurrentAccounts)

	request := httptest.NewRequest(http.MethodGet, "/admin/relay-planning/mappings", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var listBody struct {
		Data struct {
			Items []relayplanning.Mapping `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode mapping list: %v", err)
	}
	if len(listBody.Data.Items) != 1 {
		t.Fatalf("mapping count = %d, want 1", len(listBody.Data.Items))
	}
	listed := listBody.Data.Items[0]
	if listed.AccountManagementInitialized || len(listed.AccountPools) != 2 || listed.AccountPools[0].Drift {
		t.Fatalf("uninitialized account state = %+v, want current reality without drift", listed)
	}
	if got := listed.AccountPools[0].Current; len(got) != 2 || got[0].ID != 12 || got[0].Priority != 1 || got[1].ID != 11 || got[1].Priority != 2 {
		t.Fatalf("current target accounts = %+v, want safe same-platform priority order", got)
	}
	if !containsRelayPlanningWarning(listed.Warnings, "target group 101 has multiple Accounts") || !containsRelayPlanningWarning(listed.Warnings, "account 11 is reused across target groups 101, 102") {
		t.Fatalf("Account warnings = %v, want multi-Account and reused-Account warnings", listed.Warnings)
	}

	request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/accounts/adopt", mapping.ID), nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("adopt status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var adoptBody struct {
		Data relayplanning.Mapping `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &adoptBody); err != nil {
		t.Fatalf("decode adopted mapping: %v", err)
	}
	if !adoptBody.Data.AccountManagementInitialized || adoptBody.Data.AccountPools[0].Drift {
		t.Fatalf("adopted mapping = %+v, want initialized matching state", adoptBody.Data)
	}
	wantDesired := []relayplanning.AccountIntent{{AccountID: 12, Priority: 1}, {AccountID: 11, Priority: 2}}
	if got := adoptBody.Data.DesiredAccounts["101"]; fmt.Sprint(got) != fmt.Sprint(wantDesired) {
		t.Fatalf("desired accounts = %+v, want %+v", got, wantDesired)
	}
	if got := adoptBody.Data.DesiredAccounts["102"]; fmt.Sprint(got) != fmt.Sprint([]relayplanning.AccountIntent{{AccountID: 11, Priority: 1}}) {
		t.Fatalf("Target 102 desired accounts = %+v, want reused Account 11", got)
	}
	if provider.accountUpdates != 0 {
		t.Fatalf("Relay account updates = %d, want zero for adoption", provider.accountUpdates)
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if !persisted.AccountManagementInitialized {
		t.Fatal("adopted account state was not persisted")
	}
}

func TestRelayPlanningListMappingsUsesRelationshipSnapshot(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-mapping-list-test").
		SetDisplayName("Relay Planning Mapping List Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetSourceGroupID(20).
		SetGroupIds([]int64{101}).
		SaveX(ctx)
	client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-beta").
		SetDepartmentName("Department Beta").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetSourceGroupID(20).
		SetGroupIds([]int64{102}).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		directoryUsers:        []relay.User{{ID: 900, Username: "external-user", Email: "external@example.com"}},
		activeSubscriptionIDs: map[int64][]int64{900: {101}},
		groups: []relay.Group{
			{ID: 10, Name: "Group Alpha", Platform: "openai"},
			{ID: 20, Name: "Group Beta", Platform: "openai"},
			{ID: 101, Name: "Group Gamma", Platform: "openai"},
			{ID: 102, Name: "Group Delta", Platform: "openai"},
		},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	router.GET("/admin/relay-planning/mappings", NewRelayPlanningHandler(service).ListMappings)

	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/relay-planning/mappings?provider_id=%d", providerConfig.ID), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data struct {
			Items []relayplanning.Mapping `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode mapping list: %v", err)
	}
	if len(body.Data.Items) != 2 || !containsRelayPlanningWarning(body.Data.Items[0].Warnings, "unmanaged relay member 900 in target group 101") {
		t.Fatalf("mapping warnings = %+v, want batch-derived unmanaged member warning", body.Data.Items)
	}
	if got := provider.relationshipReads.Load(); got != 1 {
		t.Fatalf("relationship snapshot reads = %d, want 1", got)
	}
	if got := provider.directoryReads.Load(); got != 0 {
		t.Fatalf("legacy directory reads = %d, want 0", got)
	}
	if got := provider.subscriptionReads.Load(); got != 0 {
		t.Fatalf("per-user subscription reads = %d, want 0", got)
	}
	if got := provider.accountReads; got != 1 {
		t.Fatalf("same-platform Account reads = %d, want 1", got)
	}
}

func TestRelayPlanningListMappingsStartsIndependentRelayReadsTogether(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-concurrent-list-test").
		SetDisplayName("Relay Planning Concurrent List Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetGroupIds([]int64{101}).
		SaveX(ctx)
	started := make(chan string, 3)
	release := make(chan struct{})
	provider := &relayPlanningSearchProvider{
		groups:            []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		accounts:          []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai"}},
		dependencyStarted: started,
		dependencyRelease: release,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	router.GET("/admin/relay-planning/mappings", NewRelayPlanningHandler(service).ListMappings)

	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/relay-planning/mappings?provider_id=%d", providerConfig.ID), nil)
		router.ServeHTTP(response, request)
		close(done)
	}()
	seen := make(map[string]bool, 3)
	for len(seen) < 3 {
		select {
		case name := <-started:
			seen[name] = true
		case <-time.After(time.Second):
			close(release)
			t.Fatalf("started dependencies = %v, want groups, relationships, and accounts before release", seen)
		}
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("mapping list did not complete after releasing dependencies")
	}
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if provider.groupReads.Load() != 1 || provider.relationshipReads.Load() != 1 || provider.accountReads != 1 {
		t.Fatalf("Relay reads = groups:%d relationships:%d accounts:%d, want 1/1/1", provider.groupReads.Load(), provider.relationshipReads.Load(), provider.accountReads)
	}
}

func TestRelayPlanningListMappingsSQLReadCountDoesNotGrowWithMappings(t *testing.T) {
	ctx := context.Background()
	client, dsn := testdb.OpenWithDSN(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-query-budget-test").SetDisplayName("Relay Planning Query Budget Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	createMapping := func(departmentID string, groupID int64) {
		client.RelayGroupMapping.Create().SetProviderID(providerConfig.ID).SetDepartmentExternalID(departmentID).SetDepartmentName(departmentID).SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{groupID}).SaveX(ctx)
	}
	createMapping("dept-alpha", 101)
	createMapping("dept-missing-1", 102)

	var queryMu sync.Mutex
	queries := make([]string, 0)
	loggedClient, err := ent.Open("postgres", dsn, ent.Debug(), ent.Log(func(values ...any) {
		queryMu.Lock()
		queries = append(queries, fmt.Sprint(values...))
		queryMu.Unlock()
	}))
	if err != nil {
		t.Fatalf("open query-count client: %v", err)
	}
	t.Cleanup(func() { _ = loggedClient.Close() })
	provider := &relayPlanningSearchProvider{groups: []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}}, accounts: []relay.Account{}}
	service := relayplanning.NewService(loggedClient, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	readCount := func() int {
		queryMu.Lock()
		queries = queries[:0]
		queryMu.Unlock()
		if _, listErr := service.ListMappings(ctx, providerConfig.ID); listErr != nil {
			t.Fatalf("list mappings: %v", listErr)
		}
		queryMu.Lock()
		defer queryMu.Unlock()
		return len(queries)
	}
	smallCount := readCount()
	for index := 2; index <= 9; index++ {
		createMapping(fmt.Sprintf("dept-missing-%d", index), int64(101+index))
	}
	largeCount := readCount()
	if largeCount != smallCount {
		t.Fatalf("SQL reads = %d for two mappings and %d for ten mappings, want constant", smallCount, largeCount)
	}
}

func TestRelayPlanningMappingRenewalPreviewShowsManagedSubscriptionOutcomesWithoutMutation(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-renewal-preview-test").
		SetDisplayName("Relay Planning Renewal Preview Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(41).SaveX(ctx)
	bob := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	carol := client.User.Create().SetUsername("carol").SetEmail("carol@example.net").SetAuthSource("ldap").SetRelayUserID(43).SaveX(ctx)
	dana := client.User.Create().SetUsername("dana").SetEmail("dana@example.edu").SetAuthSource("ldap").SetRelayUserID(44).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetGroupIds([]int64{101, 102, 103, 104}).
		SetMemberAssignments(map[string]int64{
			fmt.Sprint(alice.ID): 101,
			fmt.Sprint(bob.ID):   102,
			fmt.Sprint(carol.ID): 103,
			fmt.Sprint(dana.ID):  104,
		}).
		SaveX(ctx)
	activeExpiry := time.Date(2099, time.January, 2, 3, 4, 5, 0, time.UTC)
	expiredAt := time.Date(2000, time.January, 2, 3, 4, 5, 0, time.UTC)
	suspendedExpiry := time.Date(2099, time.February, 3, 4, 5, 6, 0, time.UTC)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{
			41: {ID: 41, Username: "alice", Email: "alice@example.com"},
			42: {ID: 42, Username: "bob", Email: "bob@example.org"},
			43: {ID: 43, Username: "carol", Email: "carol@example.net"},
			44: {ID: 44, Username: "dana", Email: "dana@example.edu"},
			99: {ID: 99, Username: "relay-only", Email: "relay-only@example.invalid"},
		},
		groups: []relay.Group{
			{ID: 101, Name: "Group Active", Platform: "openai"},
			{ID: 102, Name: "Group Expired", Platform: "openai"},
			{ID: 103, Name: "Group Missing", Platform: "openai"},
			{ID: 104, Name: "Group Suspended", Platform: "openai"},
			{ID: 999, Name: "Group Drift", Platform: "openai"},
		},
		subscriptions: map[int64][]relay.UserSubscription{
			41: {
				{ID: 1, UserID: 41, GroupID: 101, Status: "active", ExpiresAt: activeExpiry},
				{ID: 2, UserID: 41, GroupID: 999, Status: "active", ExpiresAt: activeExpiry},
			},
			42: {{ID: 3, UserID: 42, GroupID: 102, Status: "expired", ExpiresAt: expiredAt}},
			43: {},
			44: {{ID: 4, UserID: 44, GroupID: 104, Status: "suspended", ExpiresAt: suspendedExpiry}},
			99: {{ID: 5, UserID: 99, GroupID: 103, Status: "active", ExpiresAt: activeExpiry}},
		},
		relationshipPages: [][]int64{{41, 42}, {43, 44, 99}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/renewal/preview", handler.PreviewMappingRenewal)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/renewal/preview", mapping.ID)

	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.MappingRenewalPreview `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	preview := body.Data
	if preview.MappingID != mapping.ID || preview.RenewalDays != 365 || len(preview.Members) != 4 {
		t.Fatalf("preview = %+v, want mapping %d with four managed members and 365 days", preview, mapping.ID)
	}
	if !strings.HasPrefix(preview.RelationshipFingerprint, "v2:") || strings.Contains(response.Body.String(), "test-admin-key") {
		t.Fatalf("fingerprint/response = %q/%s, want opaque v2 facts without credentials", preview.RelationshipFingerprint, response.Body.String())
	}
	if got := provider.relationshipReads.Load(); got != 1 {
		t.Fatalf("relationship snapshot reads = %d, want 1", got)
	}
	if got := provider.relationshipPageReads.Load(); got != 2 {
		t.Fatalf("relationship snapshot page reads = %d, want 2", got)
	}
	if got := provider.userReads.Load(); got != 0 {
		t.Fatalf("per-member user reads = %d, want 0", got)
	}
	if got := provider.subscriptionReads.Load(); got != 0 {
		t.Fatalf("per-member subscription reads = %d, want 0", got)
	}
	active := preview.Members[0]
	maximumExpiry := time.Date(2099, time.December, 31, 23, 59, 59, 0, time.UTC)
	if active.UserID != alice.ID || active.ExpectedTargetGroupID != 101 || active.ExpectedTargetGroupName != "Group Active" || active.Status != "active" || active.PlannedAction != "extend" || active.CurrentExpiry == nil || !active.CurrentExpiry.Equal(activeExpiry) || active.ResultingExpiry == nil || !active.ResultingExpiry.Equal(maximumExpiry) {
		t.Fatalf("active member = %+v, want expiry capped at Relay maximum %s", active, maximumExpiry)
	}
	if len(active.Drift) != 1 || active.Drift[0].GroupID != 999 || active.Drift[0].GroupName != "Group Drift" || active.Drift[0].Status != "active" {
		t.Fatalf("active drift = %+v, want unexpected Group 999", active.Drift)
	}
	expired := preview.Members[1]
	if expired.Status != "expired" || expired.PlannedAction != "renew" || expired.ResultingExpiry == nil || !expired.ResultingExpiry.Equal(preview.GeneratedAt.AddDate(0, 0, 365)) {
		t.Fatalf("expired member = %+v, want renewal from Preview time", expired)
	}
	missing := preview.Members[2]
	if missing.Status != "missing" || missing.PlannedAction != "create" || missing.CurrentExpiry != nil || missing.ResultingExpiry == nil || !missing.ResultingExpiry.Equal(preview.GeneratedAt.AddDate(0, 0, 365)) {
		t.Fatalf("missing member = %+v, want new subscription from Preview time", missing)
	}
	suspended := preview.Members[3]
	if suspended.Status != "suspended" || suspended.PlannedAction != "skip" || suspended.ResultingExpiry == nil || !suspended.ResultingExpiry.Equal(suspendedExpiry) {
		t.Fatalf("suspended member = %+v, want unchanged skipped subscription", suspended)
	}
	if len(provider.assigned) != 0 || len(provider.removed) != 0 || len(provider.bound) != 0 || provider.accountUpdates != 0 || len(provider.events) != 0 {
		t.Fatalf("provider mutations = assigned:%v removed:%v bound:%v accounts:%d events:%v, want none", provider.assigned, provider.removed, provider.bound, provider.accountUpdates, provider.events)
	}
	fingerprintForCurrentFacts := func() string {
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("fingerprint preview status = %d, want 200, body=%s", response.Code, response.Body.String())
		}
		var current struct {
			Data relayplanning.MappingRenewalPreview `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &current); err != nil {
			t.Fatalf("decode fingerprint preview: %v", err)
		}
		return current.Data.RelationshipFingerprint
	}
	assertFingerprintChanged := func(label string) {
		if current := fingerprintForCurrentFacts(); current == preview.RelationshipFingerprint {
			t.Fatalf("%s did not change renewal relationship fingerprint %q", label, current)
		}
	}
	originalAliceSubscriptions := append([]relay.UserSubscription(nil), provider.subscriptions[41]...)
	provider.subscriptions[41][0].ExpiresAt = activeExpiry.Add(-time.Hour)
	assertFingerprintChanged("expected subscription expiry")
	provider.subscriptions[41] = append([]relay.UserSubscription(nil), originalAliceSubscriptions...)
	provider.subscriptions[41][1].ExpiresAt = activeExpiry.Add(-time.Hour)
	assertFingerprintChanged("unexpected subscription drift expiry")
	provider.subscriptions[41] = append([]relay.UserSubscription(nil), originalAliceSubscriptions...)
	provider.subscriptions[41][0].Status = "suspended"
	assertFingerprintChanged("expected subscription status")
	provider.subscriptions[41] = append([]relay.UserSubscription(nil), originalAliceSubscriptions...)
	provider.subscriptions[41] = provider.subscriptions[41][:1]
	assertFingerprintChanged("unexpected subscription drift")
	provider.subscriptions[41] = append([]relay.UserSubscription(nil), originalAliceSubscriptions...)
	provider.groups[0].Name = "Group Active Renamed"
	assertFingerprintChanged("expected target Group")
	provider.groups[0].Name = "Group Active"
	changedAssignments := map[string]int64{
		fmt.Sprint(alice.ID): 102,
		fmt.Sprint(bob.ID):   102,
		fmt.Sprint(carol.ID): 103,
		fmt.Sprint(dana.ID):  104,
	}
	client.RelayGroupMapping.UpdateOneID(mapping.ID).SetMemberAssignments(changedAssignments).SaveX(ctx)
	assertFingerprintChanged("managed member assignment")
	client.RelayGroupMapping.UpdateOneID(mapping.ID).SetMemberAssignments(map[string]int64{
		fmt.Sprint(alice.ID): 101,
		fmt.Sprint(bob.ID):   102,
		fmt.Sprint(carol.ID): 103,
		fmt.Sprint(dana.ID):  104,
	}).SaveX(ctx)

	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"renewal_days":30}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("adjusted term status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode adjusted response: %v", err)
	}
	if body.Data.RenewalDays != 30 || body.Data.Members[0].ResultingExpiry == nil || !body.Data.Members[0].ResultingExpiry.Equal(activeExpiry.AddDate(0, 0, 30)) {
		t.Fatalf("adjusted preview = %+v, want active expiry extended by 30 days", body.Data)
	}
}

func TestRelayPlanningMappingRenewalPreviewRejectsUnsupportedTerm(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	mapping := client.RelayGroupMapping.Create().SetProviderID(7).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{101}).SaveX(ctx)
	provider := &relayPlanningSearchProvider{}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/renewal/preview", handler.PreviewMappingRenewal)
	for _, test := range []struct {
		payload string
		want    string
	}{
		{payload: `{"renewal_days":0}`, want: "renewal_days must be positive"},
		{payload: `{"renewal_days":36501}`, want: "renewal_days must not exceed 36500"},
	} {
		request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/renewal/preview", mapping.ID), strings.NewReader(test.payload))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), test.want) {
			t.Fatalf("status/body = %d/%s, want %q", response.Code, response.Body.String(), test.want)
		}
	}
	if provider.subscriptionReads.Load() != 0 {
		t.Fatalf("subscription reads = %d, want validation before provider reads", provider.subscriptionReads.Load())
	}
}

func TestRelayPlanningMappingRenewalExecuteRejectsStaleFactsBeforeWrite(t *testing.T) {
	fixture := newRelayPlanningRenewalExecutionFixture(t)
	preview := fixture.preview(t)
	payload := mappingRenewalExecutePayload(t, preview, []int{fixture.alice.ID}, "renewal-dialog-stale", false)
	var reviewedRequest map[string]any
	if err := json.Unmarshal(payload, &reviewedRequest); err != nil {
		t.Fatalf("decode reviewed execution: %v", err)
	}
	reviewedRequest["renewal_days"] = 30
	changedTermPayload, err := json.Marshal(reviewedRequest)
	if err != nil {
		t.Fatalf("encode changed-term execution: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, fixture.path+"/execute", bytes.NewReader(changedTermPayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("changed-term status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Details struct {
			ErrorCode          string                               `json:"error_code"`
			CurrentFingerprint string                               `json:"current_relationship_fingerprint"`
			RefreshedPreview   *relayplanning.MappingRenewalPreview `json:"refreshed_preview"`
			Differences        []string                             `json:"differences"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode changed-term response: %v", err)
	}
	if body.Details.ErrorCode != "stale_relay_plan" || body.Details.RefreshedPreview == nil || body.Details.RefreshedPreview.RenewalDays != 30 || len(fixture.provider.renewalWrites) != 0 {
		t.Fatalf("changed-term details/writes = %+v/%+v, want refreshed 30-day preview before writes", body.Details, fixture.provider.renewalWrites)
	}
	fixture.provider.subscriptions[41][0].ExpiresAt = fixture.provider.subscriptions[41][0].ExpiresAt.Add(time.Hour)
	request = httptest.NewRequest(http.MethodPost, fixture.path+"/execute", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("stale-facts status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	body = struct {
		Details struct {
			ErrorCode          string                               `json:"error_code"`
			CurrentFingerprint string                               `json:"current_relationship_fingerprint"`
			RefreshedPreview   *relayplanning.MappingRenewalPreview `json:"refreshed_preview"`
			Differences        []string                             `json:"differences"`
		} `json:"details"`
	}{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode stale-facts response: %v", err)
	}
	if body.Details.ErrorCode != "stale_relay_plan" || body.Details.CurrentFingerprint == preview.RelationshipFingerprint || body.Details.RefreshedPreview == nil || len(body.Details.Differences) == 0 {
		t.Fatalf("stale details = %+v, want refreshed renewal preview and safe differences", body.Details)
	}
	if len(fixture.provider.renewalWrites) != 0 {
		t.Fatalf("renewal writes = %+v, want none before stale rejection", fixture.provider.renewalWrites)
	}
}

func TestRelayPlanningMappingRenewalExecuteReportsStatesAndRetriesOnlyFailures(t *testing.T) {
	fixture := newRelayPlanningRenewalExecutionFixture(t)
	preview := fixture.preview(t)
	readsBeforeExecute := fixture.provider.relationshipReads.Load()
	aliceExpiryBefore := fixture.provider.subscriptions[41][0].ExpiresAt
	danaExpiryBefore := fixture.provider.subscriptions[44][0].ExpiresAt
	fixture.provider.renewalAmbiguous[42] = errors.New("synthetic timeout after apply")
	fixture.provider.renewalFailures[43] = errors.New("synthetic assignment failure")
	payload := mappingRenewalExecutePayload(t, preview, []int{fixture.alice.ID, fixture.bob.ID, fixture.carol.ID, fixture.dana.ID}, "renewal-dialog-1", false)
	request := httptest.NewRequest(http.MethodPost, fixture.path+"/execute", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.MappingRenewalExecution `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode execute response: %v", err)
	}
	result := body.Data
	if result.OperationKey != "renewal-dialog-1" || result.Preview == nil || len(result.Members) != 4 {
		t.Fatalf("execution = %+v, want operation key, refreshed preview, and four results", result)
	}
	wantStatuses := []string{"succeeded", "failed", "failed", "skipped"}
	for index, want := range wantStatuses {
		if result.Members[index].Status != want {
			t.Fatalf("member results = %+v, want status %q at index %d", result.Members, want, index)
		}
	}
	if got := len(fixture.provider.renewalWrites); got != 3 {
		t.Fatalf("renewal writes = %+v, want active, expired, and missing only", fixture.provider.renewalWrites)
	}
	if got := fixture.provider.relationshipReads.Load() - readsBeforeExecute; got != 2 {
		t.Fatalf("execution relationship snapshot reads = %d, want preflight and readback only", got)
	}
	if got := fixture.provider.renewalLegacyExtends.Load(); got != 0 {
		t.Fatalf("user/group extension calls = %d, want 0", got)
	}
	if got := fixture.provider.renewalDirectExtends.Load(); got != 2 {
		t.Fatalf("reviewed subscription extension calls = %d, want active and expired only", got)
	}
	if fixture.provider.userReads.Load() != 0 || fixture.provider.subscriptionReads.Load() != 0 {
		t.Fatalf("per-member discovery reads = users:%d subscriptions:%d, want 0/0", fixture.provider.userReads.Load(), fixture.provider.subscriptionReads.Load())
	}
	if got := fixture.provider.subscriptions[41][0].ExpiresAt; !got.Equal(aliceExpiryBefore.AddDate(0, 0, 365)) {
		t.Fatalf("active expiry = %s, want current expiry %s + 365 days", got, aliceExpiryBefore)
	}
	if got := fixture.provider.subscriptions[42][0]; got.Status != "active" || !got.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("expired subscription after ambiguous write = %+v, want active from execution time", got)
	}
	if len(fixture.provider.subscriptions[43]) != 0 {
		t.Fatalf("failed missing subscription write created %+v", fixture.provider.subscriptions[43])
	}
	if got := fixture.provider.subscriptions[44][0]; got.Status != "suspended" || !got.ExpiresAt.Equal(danaExpiryBefore) {
		t.Fatalf("suspended subscription = %+v, want unchanged expiry %s", got, danaExpiryBefore)
	}
	firstKeys := make(map[int64]string)
	for _, call := range fixture.provider.renewalWrites {
		if call.OperationKey == "" || call.Days != 365 {
			t.Fatalf("renewal write = %+v, want keyed 365-day write", call)
		}
		firstKeys[call.UserID] = call.OperationKey
	}
	if firstKeys[41] == firstKeys[42] || firstKeys[42] == firstKeys[43] || firstKeys[41] == firstKeys[43] {
		t.Fatalf("per-member operation keys = %+v, want deterministic unique keys", firstKeys)
	}
	bobExpiryAfterAmbiguousWrite := fixture.provider.subscriptions[42][0].ExpiresAt
	delete(fixture.provider.renewalFailures, int64(43))
	retryPayload := mappingRenewalRetryPayload(t, result, "renewal-dialog-1")
	request = httptest.NewRequest(http.MethodPost, fixture.path+"/execute", bytes.NewReader(retryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	if len(body.Data.Members) != 2 || body.Data.Members[0].Status != "succeeded" || body.Data.Members[1].Status != "succeeded" {
		t.Fatalf("retry results = %+v, want only two failed members succeeded", body.Data.Members)
	}
	retryWrites := fixture.provider.renewalWrites[3:]
	retryKeys := make(map[int64]string, len(retryWrites))
	for _, write := range retryWrites {
		retryKeys[write.UserID] = write.OperationKey
	}
	if len(retryWrites) != 2 || retryKeys[42] != firstKeys[42] || retryKeys[43] != firstKeys[43] {
		t.Fatalf("retry writes = %+v, want failed members with original keys", retryWrites)
	}
	if !fixture.provider.subscriptions[42][0].ExpiresAt.Equal(bobExpiryAfterAmbiguousWrite) {
		t.Fatalf("ambiguous retry expiry = %s, want no second extension after %s", fixture.provider.subscriptions[42][0].ExpiresAt, bobExpiryAfterAmbiguousWrite)
	}
	if got := fixture.provider.subscriptions[43]; len(got) != 1 || got[0].Status != "active" || !got[0].ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("retried missing subscription = %+v, want one active subscription from execution time", got)
	}
	if len(fixture.provider.subscriptions[41]) != 2 || fixture.provider.subscriptions[41][1].GroupID != 999 || len(fixture.provider.assigned) != 0 || len(fixture.provider.removed) != 0 || len(fixture.provider.bound) != 0 || len(fixture.provider.events) != 0 {
		t.Fatalf("unrelated relationships changed: subscriptions=%+v assigned=%v removed=%v bound=%v events=%v", fixture.provider.subscriptions[41], fixture.provider.assigned, fixture.provider.removed, fixture.provider.bound, fixture.provider.events)
	}
}

type relayPlanningRenewalExecutionFixture struct {
	ctx      context.Context
	client   *ent.Client
	provider *relayPlanningSearchProvider
	router   *gin.Engine
	mapping  *ent.RelayGroupMapping
	path     string
	alice    *ent.User
	bob      *ent.User
	carol    *ent.User
	dana     *ent.User
}

func newRelayPlanningRenewalExecutionFixture(t *testing.T) *relayPlanningRenewalExecutionFixture {
	t.Helper()
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("renewal-execute-test").SetDisplayName("Renewal Execute Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	alice := client.User.Create().SetUsername("renewal-alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(41).SaveX(ctx)
	bob := client.User.Create().SetUsername("renewal-bob").SetEmail("bob@example.org").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	carol := client.User.Create().SetUsername("renewal-carol").SetEmail("carol@example.net").SetAuthSource("ldap").SetRelayUserID(43).SaveX(ctx)
	dana := client.User.Create().SetUsername("renewal-dana").SetEmail("dana@example.edu").SetAuthSource("ldap").SetRelayUserID(44).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-renewal").SetDepartmentName("Department Renewal").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{101, 102, 103, 104}).SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101, fmt.Sprint(bob.ID): 102, fmt.Sprint(carol.ID): 103, fmt.Sprint(dana.ID): 104}).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{
			41: {ID: 41, Username: alice.Username, Email: alice.Email},
			42: {ID: 42, Username: bob.Username, Email: bob.Email},
			43: {ID: 43, Username: carol.Username, Email: carol.Email},
			44: {ID: 44, Username: dana.Username, Email: dana.Email},
		},
		groups: []relay.Group{{ID: 101, Name: "Group Active", Platform: "openai"}, {ID: 102, Name: "Group Expired", Platform: "openai"}, {ID: 103, Name: "Group Missing", Platform: "openai"}, {ID: 104, Name: "Group Suspended", Platform: "openai"}, {ID: 999, Name: "Group Drift", Platform: "openai"}},
		subscriptions: map[int64][]relay.UserSubscription{
			41: {{ID: 1, UserID: 41, GroupID: 101, Status: "active", ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)}, {ID: 2, UserID: 41, GroupID: 999, Status: "active", ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)}},
			42: {{ID: 3, UserID: 42, GroupID: 102, Status: "expired", ExpiresAt: time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC)}},
			43: {},
			44: {{ID: 4, UserID: 44, GroupID: 104, Status: "suspended", ExpiresAt: time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)}},
		},
		renewalFailures:    make(map[int64]error),
		renewalAmbiguous:   make(map[int64]error),
		renewalAppliedKeys: make(map[string]bool),
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/renewal/preview", handler.PreviewMappingRenewal)
	router.POST("/admin/relay-planning/mappings/:id/renewal/execute", handler.ExecuteMappingRenewal)
	return &relayPlanningRenewalExecutionFixture{ctx: ctx, client: client, provider: provider, router: router, mapping: mapping, path: fmt.Sprintf("/admin/relay-planning/mappings/%d/renewal", mapping.ID), alice: alice, bob: bob, carol: carol, dana: dana}
}

func (f *relayPlanningRenewalExecutionFixture) preview(t *testing.T) relayplanning.MappingRenewalPreview {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, f.path+"/preview", strings.NewReader(`{"renewal_days":365}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.MappingRenewalPreview `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode preview: %v", err)
	}
	return body.Data
}

func mappingRenewalExecutePayload(t *testing.T, preview relayplanning.MappingRenewalPreview, selectedUserIDs []int, operationKey string, retry bool) []byte {
	t.Helper()
	selected := make(map[int]bool, len(selectedUserIDs))
	for _, userID := range selectedUserIDs {
		selected[userID] = true
	}
	members := make([]map[string]any, 0, len(selectedUserIDs))
	for _, member := range preview.Members {
		if selected[member.UserID] {
			members = append(members, map[string]any{"user_id": member.UserID, "target_group_id": member.ExpectedTargetGroupID, "planned_action": member.PlannedAction})
		}
	}
	payload, err := json.Marshal(map[string]any{"renewal_days": preview.RenewalDays, "members": members, "expected_relationship_fingerprint": preview.RelationshipFingerprint, "operation_key": operationKey, "retry": retry})
	if err != nil {
		t.Fatalf("marshal renewal execution: %v", err)
	}
	return payload
}

func mappingRenewalRetryPayload(t *testing.T, result relayplanning.MappingRenewalExecution, operationKey string) []byte {
	t.Helper()
	if result.Preview == nil {
		t.Fatal("renewal execution did not return a refreshed preview")
	}
	members := make([]map[string]any, 0)
	for _, member := range result.Members {
		if member.Status == "failed" {
			members = append(members, map[string]any{"user_id": member.UserID, "target_group_id": member.TargetGroupID, "planned_action": member.Action})
		}
	}
	payload, err := json.Marshal(map[string]any{"renewal_days": result.RenewalDays, "members": members, "expected_relationship_fingerprint": result.Preview.RelationshipFingerprint, "operation_key": operationKey, "retry": true})
	if err != nil {
		t.Fatalf("marshal renewal retry: %v", err)
	}
	return payload
}

func TestRelayPlanningReplanUsesSharedRelationshipSnapshot(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-replan-directory-test").
		SetDisplayName("Relay Planning Replan Directory Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetTemplateGroupName("Group Alpha").
		SetSourceGroupID(20).
		SetSourceGroupName("Group Beta").
		SetGroupIds([]int64{101}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		directoryUsers:        []relay.User{{ID: 900, Username: "external-user", Email: "external@example.com"}},
		activeSubscriptionIDs: map[int64][]int64{900: {101}},
		groups: []relay.Group{
			{ID: 10, Name: "Group Alpha", Platform: "openai"},
			{ID: 20, Name: "Group Beta", Platform: "openai"},
			{ID: 101, Name: "Group Gamma", Platform: "openai"},
		},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	handler := NewRelayPlanningHandler(service)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)

	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("replan status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode replan response: %v", err)
	}
	if len(body.Data.UnmanagedMembers) != 1 || body.Data.UnmanagedMembers[0].RelayUserID != 900 || fmt.Sprint(body.Data.UnmanagedMembers[0].TargetGroupIDs) != "[101]" {
		t.Fatalf("unmanaged members = %+v, want Relay user 900 in target Group 101", body.Data.UnmanagedMembers)
	}
	if got := provider.relationshipReads.Load(); got != 1 {
		t.Fatalf("relationship snapshot reads = %d, want 1", got)
	}
	if got := provider.directoryReads.Load(); got != 0 {
		t.Fatalf("legacy directory reads = %d, want 0", got)
	}
	if got := provider.subscriptionReads.Load(); got != 0 {
		t.Fatalf("per-user subscription reads = %d, want 0", got)
	}
	adoptionFingerprint := previewRelayPlanningFingerprint(t, router, path, `{"adopt_relay_user_ids":[900]}`)
	executePayload := fmt.Sprintf(`{"adopt_relay_user_ids":[900],"expected_relationship_fingerprint":%q,"operation_key":"adopt-relay-user-1"}`, adoptionFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("adoption status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if fmt.Sprint(provider.assigned) != "[900:101:365]" {
		t.Fatalf("adoption assignment = %v, want Relay-only member ensured for 365 days", provider.assigned)
	}
}

func TestRelayPlanningSearchAndSaveDesiredAccountsKeepsRelayReadOnly(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-account-edit-test").
		SetDisplayName("Relay Planning Account Edit Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetGroupIds([]int64{101}).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		groups: []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 101, Name: "Group Beta", Platform: "openai"}},
		accounts: []relay.Account{
			{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}},
			{ID: 12, Name: "Account Beta", Platform: "openai", Type: "apikey", Status: "error", Schedulable: false},
			{ID: 13, Name: "Account Beta Other", Platform: "anthropic", Type: "oauth", Status: "active", Schedulable: true},
			{ID: 14, Name: "Account Gamma", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 3}}},
		},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.GET("/admin/relay-planning/accounts", handler.SearchAccounts)
	router.PUT("/admin/relay-planning/mappings/:id/accounts", handler.SaveDesiredAccounts)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)

	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/relay-planning/accounts?provider_id=%d&platform=openai&q=Beta", providerConfig.ID), nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("search status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var searchBody struct {
		Data relayplanning.AccountSearchPage `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &searchBody); err != nil {
		t.Fatalf("decode account search: %v", err)
	}
	if len(searchBody.Data.Items) != 1 || searchBody.Data.Items[0].ID != 12 || searchBody.Data.Items[0].Type != "apikey" || searchBody.Data.Items[0].Status != "error" || searchBody.Data.Items[0].Schedulable {
		t.Fatalf("account search = %+v, want same-platform error API-key account", searchBody.Data)
	}

	payload := `{"desired_accounts":{"101":[{"account_id":12,"priority":1},{"account_id":11,"priority":2}]}}`
	request = httptest.NewRequest(http.MethodPut, fmt.Sprintf("/admin/relay-planning/mappings/%d/accounts", mapping.ID), strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("save status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var saveBody struct {
		Data relayplanning.Mapping `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &saveBody); err != nil {
		t.Fatalf("decode saved mapping: %v", err)
	}
	want := []relayplanning.AccountIntent{{AccountID: 12, Priority: 1}, {AccountID: 11, Priority: 2}}
	if !saveBody.Data.AccountManagementInitialized || fmt.Sprint(saveBody.Data.DesiredAccounts["101"]) != fmt.Sprint(want) {
		t.Fatalf("saved desired accounts = %+v, want %+v", saveBody.Data, want)
	}
	if provider.accountUpdates != 0 {
		t.Fatalf("Relay account updates = %d, want zero before Confirm", provider.accountUpdates)
	}
	request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID), strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("replan status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var previewBody struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &previewBody); err != nil {
		t.Fatalf("decode replan preview: %v", err)
	}
	if len(previewBody.Data.TargetSummaries) != 1 || len(previewBody.Data.TargetSummaries[0].Accounts) != 3 {
		t.Fatalf("Account summary = %+v, want add/remove/reorder", previewBody.Data.TargetSummaries)
	}
	actions := map[int64]string{}
	for _, change := range previewBody.Data.TargetSummaries[0].Accounts {
		actions[change.AccountID] = change.Action
	}
	if fmt.Sprint(actions) != "map[11:reorder 12:add 14:remove]" {
		t.Fatalf("Account summary actions = %v, want Account 11 reorder, 12 add, 14 remove", actions)
	}
}

func TestRelayPlanningConfirmAppliesDesiredAccountsBeforeMigratingMembers(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-account-apply-test").
		SetDisplayName("Relay Planning Account Apply Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	member := client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alice").
		SetEmailNormalized(alice.Email).
		SetDisplayName(alice.Username).
		SetDepartmentExternalID("dept-alpha").
		SetMatchedUserID(alice.ID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	client.DirectoryMemberDepartment.Create().
		SetSourceID(source.ID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID("dept-alpha").
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetSourceGroupID(20).
		SetGroupIds([]int64{101}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 12, "priority": 1}}}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	cost := 10.0
	tokens := int64(100)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups: []relay.Group{
			{ID: 10, Name: "Group Alpha", Platform: "openai"},
			{ID: 20, Name: "Group Source", Platform: "openai"},
			{ID: 101, Name: "Group Target", Platform: "openai"},
		},
		accounts:      []relay.Account{{ID: 12, Name: "Account Beta", Platform: "openai", Type: "apikey", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 999, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 20, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 20, Status: "active"}}},
		usage:         map[int64]relay.TeamUserUsageStats{42: {UserID: 42, RangeActualCost: &cost, RangeTotalTokens: &tokens}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	previewPath := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}]}`, alice.ID, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, previewPath, previewPayload)
	payload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"expected_relationship_fingerprint":%q,"operation_key":"apply-accounts-1"}`, alice.ID, alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mapping.ID), strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if len(provider.events) < 2 || provider.events[0] != "account:12:101:1" || !strings.HasPrefix(provider.events[1], "subscription-add:") {
		t.Fatalf("execution events = %v, want Account apply before member migration", provider.events)
	}
	if fmt.Sprint(provider.assigned) != "[42:101:365]" {
		t.Fatalf("Replan assignment = %v, want new member subscription for 365 days", provider.assigned)
	}
	var body struct {
		Data struct {
			Accounts []struct {
				TargetGroupID int64  `json:"target_group_id"`
				AccountID     int64  `json:"account_id"`
				Status        string `json:"status"`
			} `json:"accounts"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode confirm response: %v", err)
	}
	if len(body.Data.Accounts) != 1 || body.Data.Accounts[0].TargetGroupID != 101 || body.Data.Accounts[0].AccountID != 12 || body.Data.Accounts[0].Status != "succeeded" {
		t.Fatalf("account results = %+v, want successful target relationship", body.Data.Accounts)
	}
}

func TestRelayPlanningAccountFailureBlocksOnlyItsTarget(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-account-isolation-test").SetDisplayName("Relay Planning Account Isolation Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	bob := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").SetRelayUserID(43).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).SetSourceGroupID(20).
		SetGroupIds([]int64{101, 102}).SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}, "102": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:    map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}, 43: {ID: 43, Username: "bob", Email: bob.Email}},
		groups:   []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target A", Platform: "openai"}, {ID: 102, Name: "Target B", Platform: "openai"}},
		accounts: []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai"}},
		subscriptions: map[int64][]relay.UserSubscription{
			42: {{UserID: 42, GroupID: 20, Status: "active"}},
			43: {{UserID: 43, GroupID: 20, Status: "active"}},
		},
		keys:            map[int64][]relay.APIKey{42: {}, 43: {}},
		accountFailures: map[int64]error{101: errors.New("synthetic Account write failure")},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"selected_user_ids":[%d,%d],"assignments":[{"index":0,"user_ids":[%d]},{"index":1,"user_ids":[%d]}]}`, alice.ID, bob.ID, alice.ID, bob.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, previewPayload)
	executePayload := fmt.Sprintf(`{"selected_user_ids":[%d,%d],"assignments":[{"index":0,"user_ids":[%d]},{"index":1,"user_ids":[%d]}],"expected_relationship_fingerprint":%q,"operation_key":"account-isolation-1"}`, alice.ID, bob.ID, alice.ID, bob.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if containsRelayPlanningEvent(provider.events, "subscription-add:42:101") || !containsRelayPlanningEvent(provider.events, "subscription-add:43:102") {
		t.Fatalf("execution events = %v, want only Target B member migration", provider.events)
	}
	var body struct {
		Data relayplanning.ExecutionResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode execution response: %v", err)
	}
	if len(body.Data.Members) != 2 || body.Data.Members[0].Error == "" || body.Data.Members[1].Error != "" {
		t.Fatalf("member results = %+v, want Target A blocked and Target B successful", body.Data.Members)
	}
}

func TestRelayPlanningExplicitRemovalRestoresSavedSourcesAndMovesKeysBack(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-remove-test").SetDisplayName("Relay Planning Remove Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	bob := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").SetRelayUserID(43).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetSourceGroupID(20).
		SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101, fmt.Sprint(bob.ID): 101}).
		SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20, fmt.Sprint(bob.ID): 20}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:    map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}, 43: {ID: 43, Username: "bob", Email: bob.Email}},
		groups:   []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts: []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{
			42: {{UserID: 42, GroupID: 101, Status: "active"}},
			43: {{UserID: 43, GroupID: 20, Status: "active"}, {UserID: 43, GroupID: 101, Status: "active"}},
		},
		keys: map[int64][]relay.APIKey{
			42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}, {ID: 503, UserID: 42, GroupID: 101, Status: "inactive"}},
			43: {{ID: 502, UserID: 43, GroupID: 101, Status: "active"}},
		},
		mutateWrites: true,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	previewPath := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d,%d]}`, alice.ID, bob.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, previewPath, previewPayload)
	payload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d,%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-1"}`, alice.ID, bob.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mapping.ID), strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantEvents := []string{"subscription-add:42:20", "api-key:501:20", "subscription-remove:42:101", "api-key:502:20", "subscription-remove:43:101"}
	if fmt.Sprint(provider.events) != fmt.Sprint(wantEvents) {
		t.Fatalf("removal events = %v, want %v", provider.events, wantEvents)
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if _, exists := persisted.MemberAssignments[fmt.Sprint(alice.ID)]; exists {
		t.Fatalf("removed member remains in mapping: %v", persisted.MemberAssignments)
	}
	if _, exists := persisted.MemberAssignments[fmt.Sprint(bob.ID)]; exists {
		t.Fatalf("removed member remains in mapping: %v", persisted.MemberAssignments)
	}
	for _, relayUserID := range []int64{42, 43} {
		if subscriptions := provider.subscriptions[relayUserID]; len(subscriptions) != 1 || subscriptions[0].GroupID != 20 || subscriptions[0].Status != "active" {
			t.Fatalf("relay user %d subscriptions = %+v, want only active Source subscription", relayUserID, subscriptions)
		}
	}
	if keys := provider.keys[42]; len(keys) != 2 || keys[0].ID != 501 || keys[0].GroupID != 20 || keys[1].ID != 503 || keys[1].GroupID != 101 || keys[1].Status != "inactive" {
		t.Fatalf("relay user 42 API Keys = %+v, want active key moved and inactive key untouched", keys)
	}
	if keys := provider.keys[43]; len(keys) != 1 || keys[0].GroupID != 20 {
		t.Fatalf("relay user 43 API Keys = %+v, want Source binding", keys)
	}
}

func TestRelayPlanningLegacyRemovalRequiresReviewedSource(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-legacy-remove-test").SetDisplayName("Relay Planning Legacy Remove Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).
		SetOperationState(map[string]map[string]string{
			"operation":                        {"key": "completed-forward", "status": "succeeded"},
			fmt.Sprintf("member:%d", alice.ID): {"subscription": "succeeded", "source_removal": "succeeded", "api_keys": "501:succeeded", "target_group_id": "101"},
		}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}},
		mutateWrites:  true,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	unreviewedPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d]}`, alice.ID)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(unreviewedPayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "removal source") || len(provider.events) != 0 {
		t.Fatalf("unreviewed removal = status:%d events:%v body:%s, want safe 422 before Relay writes", response.Code, provider.events, response.Body.String())
	}
	targetAsSourcePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":101}}`, alice.ID, alice.ID)
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(targetAsSourcePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "managed target") || len(provider.events) != 0 {
		t.Fatalf("Target as Source = status:%d events:%v body:%s, want safe 422 before Relay writes", response.Code, provider.events, response.Body.String())
	}
	negativeSourcePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":-1}}`, alice.ID, alice.ID)
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(negativeSourcePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "non-negative") || len(provider.events) != 0 {
		t.Fatalf("negative Source = status:%d events:%v body:%s, want safe 422 before Relay writes", response.Code, provider.events, response.Body.String())
	}

	reviewedPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":20}}`, alice.ID, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, reviewedPayload)
	targetOnlyPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":0}}`, alice.ID, alice.ID)
	targetOnlyFingerprint := previewRelayPlanningFingerprint(t, router, path, targetOnlyPayload)
	if targetOnlyFingerprint == fingerprint {
		t.Fatalf("removal fingerprints = Source:%q Target-only:%q, want reviewed destination bound", fingerprint, targetOnlyFingerprint)
	}
	staleExecutePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":0},"expected_relationship_fingerprint":%q,"operation_key":"legacy-remove-stale"}`, alice.ID, alice.ID, fingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(staleExecutePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || len(provider.events) != 0 {
		t.Fatalf("changed removal destination = status:%d events:%v body:%s, want stale 409 before Relay writes", response.Code, provider.events, response.Body.String())
	}
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":20},"expected_relationship_fingerprint":%q,"operation_key":"legacy-remove-1"}`, alice.ID, alice.ID, fingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reviewed removal status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantEvents := []string{"subscription-add:42:20", "api-key:501:20", "subscription-remove:42:101"}
	if !reflect.DeepEqual(provider.events, wantEvents) {
		t.Fatalf("reviewed removal events = %v, want %v", provider.events, wantEvents)
	}
}

func TestRelayPlanningLegacyRemovalRetryCanReviewSource(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-legacy-retry-review-test").SetDisplayName("Relay Planning Legacy Retry Review Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).SetMemberAssignments(map[string]int64{}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).
		SetStatus("needs_retry").SetOperationState(map[string]map[string]string{
		"operation": {"status": "needs_retry"},
		fmt.Sprintf("member:%d", alice.ID): {
			"action":          "remove",
			"target_group_id": "101",
			"subscription":    "skipped",
			"source_removal":  "failed",
			"error":           "synthetic legacy removal failure",
		},
	}).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}},
		mutateWrites:  true,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	payload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d]}`, alice.ID)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("legacy retry preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode legacy retry preview: %v", err)
	}
	if len(body.Data.TargetSummaries) != 1 || len(body.Data.TargetSummaries[0].Members) != 1 || len(body.Data.TargetSummaries[0].Subscriptions) != 0 || len(body.Data.TargetSummaries[0].APIKeys) != 0 {
		t.Fatalf("legacy retry summary = %+v, want removal intent without invented relationship effects", body.Data.TargetSummaries)
	}
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"legacy-retry-unreviewed"}`, alice.ID, body.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "stale_relay_plan") || len(provider.events) != 0 {
		t.Fatalf("legacy retry execute = status:%d events:%v body:%s, want safe stale-plan rejection before Relay writes", response.Code, provider.events, response.Body.String())
	}
}

func TestRelayPlanningExplicitRemovalRoundTripsThroughSub2APIAdapter(t *testing.T) {
	ctx := context.Background()
	subscriptions := map[int64]map[string]any{
		101: {"id": int64(77), "user_id": int64(42), "group_id": int64(101), "status": "active"},
	}
	keyGroupID := int64(101)
	var events []string
	subscriptionItems := func() []map[string]any {
		items := make([]map[string]any, 0, len(subscriptions))
		for _, groupID := range []int64{20, 101} {
			if subscription := subscriptions[groupID]; subscription != nil {
				items = append(items, subscription)
			}
		}
		return items
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/users", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 0,
			"data": map[string]any{
				"items": []any{map[string]any{"id": 42, "username": "alice", "email": "alice@example.com", "role": "user", "subscriptions": subscriptionItems()}},
				"page":  1, "page_size": 200, "pages": 1, "total": 1,
			},
		})
	})
	mux.HandleFunc("/api/v1/admin/users/42/api-keys", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "data": []any{map[string]any{"id": 501, "user_id": 42, "group_id": keyGroupID, "status": "active"}}})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/assign", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			UserID  int64 `json:"user_id"`
			GroupID int64 `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode assignment: %v", err)
		}
		events = append(events, fmt.Sprintf("assign:%d:%d", request.UserID, request.GroupID))
		subscriptions[request.GroupID] = map[string]any{"id": int64(78), "user_id": request.UserID, "group_id": request.GroupID, "status": "active"}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":78,"status":"active"}}`))
	})
	mux.HandleFunc("/api/v1/admin/api-keys/501", func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			GroupID int64 `json:"group_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode API Key binding: %v", err)
		}
		events = append(events, fmt.Sprintf("bind:501:%d", request.GroupID))
		keyGroupID = request.GroupID
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"api_key":{"id":501}}}`))
	})
	mux.HandleFunc("/api/v1/admin/users/42/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": subscriptionItems()})
	})
	mux.HandleFunc("/api/v1/admin/subscriptions/77", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("subscription method = %s, want DELETE", r.Method)
		}
		events = append(events, "revoke:42:101")
		delete(subscriptions, 101)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"message":"Subscription revoked successfully"}}`))
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-sub2api-remove-test").SetDisplayName("Relay Planning Sub2API Remove Test").SetBaseURL(server.URL).SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	cost, tokens := 10.0, int64(100)
	actualProvider := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", "test-user-key", "test-model", zap.NewNop())
	provider := &relayPlanningSub2APIProvider{
		relayPlanningSub2API: actualProvider.(relayPlanningSub2API),
		facts: &relayPlanningSearchProvider{
			groups:   []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
			accounts: []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
			usage:    map[int64]relay.TeamUserUsageStats{42: {UserID: 42, RangeActualCost: &cost, RangeTotalTokens: &tokens}},
		},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	payload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d]}`, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, payload)
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"sub2api-remove-1"}`, alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200, body=%s", response.Code, response.Body.String())
	}

	relationships, err := actualProvider.(relay.UserRelationshipSnapshotReader).ListUserRelationships(ctx)
	if err != nil {
		t.Fatalf("read final subscriptions: %v", err)
	}
	keys, err := actualProvider.ListUserAPIKeys(ctx, 42)
	if err != nil {
		t.Fatalf("read final API Keys: %v", err)
	}
	if got, want := events, []string{"assign:42:20", "bind:501:20", "revoke:42:101"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("write events = %v, want %v", got, want)
	}
	if len(relationships) != 1 || len(relationships[0].Subscriptions) != 1 || relationships[0].Subscriptions[0].GroupID != 20 || relationships[0].Subscriptions[0].Status != "active" {
		t.Fatalf("final subscriptions = %+v, want only active Source", relationships)
	}
	if len(keys) != 1 || keys[0].GroupID != 20 {
		t.Fatalf("final API Keys = %+v, want Source binding", keys)
	}
	if got := client.RelayGroupMapping.GetX(ctx, mapping.ID); got.Status != "active" || len(got.MemberAssignments) != 0 {
		t.Fatalf("mapping = status:%s assignments:%v, want active without removed member", got.Status, got.MemberAssignments)
	}
}

func TestRelayPlanningExplicitRemovalRetainsRetryWhenWriteReadbackDoesNotMatch(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-remove-readback-test").SetDisplayName("Relay Planning Remove Readback Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:          map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:         []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts:       []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions:  map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:           map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}},
		writeAcksOnly:  true,
		removeFailures: map[int64]error{},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d]}`, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, previewPayload)
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-readback-1"}`, alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.ExecutionResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode execution response: %v", err)
	}
	if len(body.Data.Members) != 1 || !strings.Contains(body.Data.Members[0].Error, "readback") {
		t.Fatalf("member results = %+v, want relationship readback failure", body.Data.Members)
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if persisted.Status != "needs_retry" {
		t.Fatalf("mapping status = %s, want needs_retry", persisted.Status)
	}
	if _, exists := persisted.MemberAssignments[fmt.Sprint(alice.ID)]; exists {
		t.Fatalf("removed member remains in desired mapping: %v", persisted.MemberAssignments)
	}
}

func TestRelayPlanningExplicitRemovalRetryDoesNotRepeatCompletedWrites(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-remove-readback-retry-test").SetDisplayName("Relay Planning Remove Readback Retry Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:                     map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:                    []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts:                  []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions:             map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:                      map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}, {ID: 502, UserID: 42, GroupID: 101, Status: "inactive"}}},
		mutateWrites:              true,
		removeFailures:            map[int64]error{},
		relationshipReadbackError: errors.New("synthetic readback outage"),
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	payload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d]}`, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, payload)
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-readback-retry-1"}`, alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || client.RelayGroupMapping.GetX(ctx, mapping.ID).Status != "needs_retry" {
		t.Fatalf("first execute = status:%d mapping:%s body:%s, want readback retry", response.Code, client.RelayGroupMapping.GetX(ctx, mapping.ID).Status, response.Body.String())
	}
	writesAfterFirstExecute := append([]string(nil), provider.events...)
	provider.relationshipReadbackError = nil
	changedRetryPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":0}}`, alice.ID, alice.ID)
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(changedRetryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "cannot change while retry is pending") {
		t.Fatalf("changed retry destination = status:%d body:%s, want safe 422", response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(provider.events, writesAfterFirstExecute) {
		t.Fatalf("changed retry writes = %v, want no writes after %v", provider.events, writesAfterFirstExecute)
	}
	provider.keys[42][0].GroupID = 999
	provider.keys[42][0].Status = "inactive"
	provider.keys[42][1].Status = "active"
	retryFingerprint := previewRelayPlanningFingerprint(t, router, path, payload)
	provider.keys[42][0].GroupID = 998
	retryPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-readback-retry-2"}`, alice.ID, retryFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(retryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("drifted completed Key execute status = %d, want stale 409, body=%s", response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(provider.events, writesAfterFirstExecute) {
		t.Fatalf("stale completed Key writes = %v, want no repeats after %v", provider.events, writesAfterFirstExecute)
	}

	provider.keys[42][0].GroupID = 999
	driftedFingerprint := previewRelayPlanningFingerprint(t, router, path, payload)
	driftedPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-readback-retry-3"}`, alice.ID, driftedFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(driftedPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || client.RelayGroupMapping.GetX(ctx, mapping.ID).Status != "needs_retry" {
		t.Fatalf("drifted completed Key retry = status:%d mapping:%s body:%s, want readback retry", response.Code, client.RelayGroupMapping.GetX(ctx, mapping.ID).Status, response.Body.String())
	}
	if !reflect.DeepEqual(provider.events, writesAfterFirstExecute) {
		t.Fatalf("drifted completed Key retry writes = %v, want no repeats after %v", provider.events, writesAfterFirstExecute)
	}
	stillDriftedFingerprint := previewRelayPlanningFingerprint(t, router, path, payload)
	stillDriftedPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-readback-retry-4"}`, alice.ID, stillDriftedFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(stillDriftedPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || client.RelayGroupMapping.GetX(ctx, mapping.ID).Status != "needs_retry" {
		t.Fatalf("repeated drifted Key retry = status:%d mapping:%s body:%s, want readback retry", response.Code, client.RelayGroupMapping.GetX(ctx, mapping.ID).Status, response.Body.String())
	}
	if !reflect.DeepEqual(provider.events, writesAfterFirstExecute) {
		t.Fatalf("repeated drifted Key retry writes = %v, want no repeats after %v", provider.events, writesAfterFirstExecute)
	}

	provider.keys[42][0].GroupID = 20
	provider.keys[42][0].Status = "active"
	finalFingerprint := previewRelayPlanningFingerprint(t, router, path, payload)
	finalPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-readback-retry-5"}`, alice.ID, finalFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(finalPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("restored completed Key retry status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if got := client.RelayGroupMapping.GetX(ctx, mapping.ID).Status; got != "active" {
		t.Fatalf("restored completed Key retry mapping status = %s, want active, body=%s", got, response.Body.String())
	}
	if !reflect.DeepEqual(provider.events, writesAfterFirstExecute) || provider.keys[42][1].GroupID != 101 {
		t.Fatalf("frozen reviewed Key retry = events:%v key502:%+v, want no repeat writes and newly active Key untouched", provider.events, provider.keys[42][1])
	}
}

func TestRelayPlanningExplicitRemovalWithoutSavedSourceOnlyRemovesTargetSubscription(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-remove-without-source-test").SetDisplayName("Relay Planning Remove Without Source Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).
		SetGroupIds([]int64{101}).SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}},
		mutateWrites:  true,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":0}}`, alice.ID, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, previewPayload)
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"member_sources":{"%d":0},"expected_relationship_fingerprint":%q,"operation_key":"remove-without-source-1"}`, alice.ID, alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if fmt.Sprint(provider.events) != "[subscription-remove:42:101]" || len(provider.bound) != 0 {
		t.Fatalf("removal events = %v, bound=%v, want only Target subscription removal", provider.events, provider.bound)
	}
}

func TestRelayPlanningMoveHereTransfersOneManagedTargetAndUpdatesBothMappings(t *testing.T) {
	ctx := context.Background()
	client, dsn := testdb.OpenWithDSN(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-transfer-test").SetDisplayName("Relay Planning Transfer Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-beta")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	member := client.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID("member-alice").SetEmailNormalized(alice.Email).SetDisplayName(alice.Username).SetDepartmentExternalID("dept-beta").SetMatchedUserID(alice.ID).SetLastSeenRunID(run.ID).SaveX(ctx)
	client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(member.ID).SetMemberExternalID(member.ExternalID).SetMemberEmailNormalized(member.EmailNormalized).SetDepartmentExternalID("dept-beta").SetLastSeenRunID(run.ID).SaveX(ctx)
	mappingA := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	mappingB := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-beta").SetDepartmentName("Department Beta").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{202}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"202": {{"account_id": 11, "priority": 2}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	cost := 10.0
	tokens := int64(100)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target A", Platform: "openai"}, {ID: 202, Name: "Target B", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}, {GroupID: 202, Priority: 2}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}},
		usage:         map[int64]relay.TeamUserUsageStats{42: {UserID: 42, RangeActualCost: &cost, RangeTotalTokens: &tokens}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	previewPath := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mappingB.ID)
	previewPayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"member_actions":{"%d":{"mode":"move_here","from_mapping_id":%d}}}`, alice.ID, alice.ID, alice.ID, mappingA.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, previewPath, previewPayload)
	payload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"member_actions":{"%d":{"mode":"move_here","from_mapping_id":%d}},"expected_relationship_fingerprint":%q,"operation_key":"transfer-1"}`, alice.ID, alice.ID, alice.ID, mappingA.ID, fingerprint)
	rawDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open raw test database: %v", err)
	}
	defer rawDB.Close()
	triggerSQL := fmt.Sprintf(`
CREATE FUNCTION reject_source_mapping_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.id = %d THEN
    RAISE EXCEPTION 'synthetic source mapping persistence failure';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER reject_source_mapping_update BEFORE UPDATE ON relay_group_mappings
FOR EACH ROW EXECUTE FUNCTION reject_source_mapping_update();`, mappingA.ID)
	if _, err := rawDB.ExecContext(ctx, triggerSQL); err != nil {
		t.Fatalf("install source mapping failure trigger: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mappingB.ID), strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("persistence failure status = %d, want 422, body=%s", response.Code, response.Body.String())
	}
	failedA := client.RelayGroupMapping.GetX(ctx, mappingA.ID)
	failedB := client.RelayGroupMapping.GetX(ctx, mappingB.ID)
	if failedA.MemberAssignments[fmt.Sprint(alice.ID)] != 101 || failedB.MemberAssignments[fmt.Sprint(alice.ID)] != 0 {
		t.Fatalf("mapping assignments after failed persistence = A:%v B:%v, want atomic rollback", failedA.MemberAssignments, failedB.MemberAssignments)
	}
	var persistenceFailure struct {
		Details struct {
			ErrorCode string `json:"error_code"`
			Retryable bool   `json:"retryable"`
			Mappings  []struct {
				MappingID int    `json:"mapping_id"`
				Role      string `json:"role"`
				Status    string `json:"status"`
			} `json:"mappings"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &persistenceFailure); err != nil {
		t.Fatalf("decode persistence failure: %v", err)
	}
	if persistenceFailure.Details.ErrorCode != "mapping_persistence_failed" || !persistenceFailure.Details.Retryable || len(persistenceFailure.Details.Mappings) != 2 {
		t.Fatalf("persistence failure details = %+v, want retryable destination/source results", persistenceFailure.Details)
	}
	if _, err := rawDB.ExecContext(ctx, `DROP TRIGGER reject_source_mapping_update ON relay_group_mappings; DROP FUNCTION reject_source_mapping_update()`); err != nil {
		t.Fatalf("remove source mapping failure trigger: %v", err)
	}
	provider.events = nil
	request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mappingB.ID), strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("transfer status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var executionBody struct {
		Data struct {
			Mappings []struct {
				MappingID int    `json:"mapping_id"`
				Role      string `json:"role"`
				Status    string `json:"status"`
			} `json:"mappings"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &executionBody); err != nil {
		t.Fatalf("decode transfer execution: %v", err)
	}
	if len(executionBody.Data.Mappings) != 2 || executionBody.Data.Mappings[0].Status != "succeeded" || executionBody.Data.Mappings[1].Status != "succeeded" {
		t.Fatalf("mapping persistence results = %+v, want destination and source success", executionBody.Data.Mappings)
	}
	wantEvents := []string{"subscription-add:42:202", "api-key:501:202", "subscription-remove:42:101"}
	if fmt.Sprint(provider.events) != fmt.Sprint(wantEvents) {
		t.Fatalf("transfer events = %v, want %v", provider.events, wantEvents)
	}
	updatedA := client.RelayGroupMapping.GetX(ctx, mappingA.ID)
	updatedB := client.RelayGroupMapping.GetX(ctx, mappingB.ID)
	if _, exists := updatedA.MemberAssignments[fmt.Sprint(alice.ID)]; exists || updatedB.MemberAssignments[fmt.Sprint(alice.ID)] != 202 {
		t.Fatalf("mapping assignments after transfer = A:%v B:%v", updatedA.MemberAssignments, updatedB.MemberAssignments)
	}
	if updatedA.OperationState["operation"]["key"] != "transfer-1" || updatedB.OperationState["operation"]["key"] != "transfer-1" {
		t.Fatalf("operation keys after transfer = A:%v B:%v", updatedA.OperationState, updatedB.OperationState)
	}

	retryState := updatedB.OperationState
	retryState["operation"]["status"] = "needs_retry"
	retryState[fmt.Sprintf("member:%d", alice.ID)]["error"] = "synthetic persistence failure"
	client.RelayGroupMapping.UpdateOneID(mappingB.ID).SetOperationState(retryState).SetStatus("needs_retry").SaveX(ctx)
	provider.events = nil
	retryPreviewPayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}]}`, alice.ID, alice.ID)
	retryRequest := httptest.NewRequest(http.MethodPost, previewPath, strings.NewReader(retryPreviewPayload))
	retryRequest.Header.Set("Content-Type", "application/json")
	retryResponse := httptest.NewRecorder()
	router.ServeHTTP(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusOK {
		t.Fatalf("retry preview status = %d, want 200, body=%s", retryResponse.Code, retryResponse.Body.String())
	}
	var retryPreview struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(retryResponse.Body.Bytes(), &retryPreview); err != nil {
		t.Fatalf("decode retry preview: %v", err)
	}
	if len(retryPreview.Data.TargetSummaries) != 1 || len(retryPreview.Data.TargetSummaries[0].Members) != 1 || retryPreview.Data.TargetSummaries[0].Members[0].Action != "move" || retryPreview.Data.TargetSummaries[0].Members[0].FromGroupID != 101 {
		t.Fatalf("retry summary = %+v, want preserved Move Here from Target 101", retryPreview.Data.TargetSummaries)
	}
	retryPayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"expected_relationship_fingerprint":%q,"operation_key":"transfer-2"}`, alice.ID, alice.ID, retryPreview.Data.RelationshipFingerprint)
	retryRequest = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mappingB.ID), strings.NewReader(retryPayload))
	retryRequest.Header.Set("Content-Type", "application/json")
	retryResponse = httptest.NewRecorder()
	router.ServeHTTP(retryResponse, retryRequest)
	if retryResponse.Code != http.StatusOK || len(provider.events) != 0 {
		t.Fatalf("retry status = %d, events=%v, want preserved Move Here intent without repeating completed writes, body=%s", retryResponse.Code, provider.events, retryResponse.Body.String())
	}
}

func TestRelayPlanningAddAdditionallyPreservesExistingManagedRelationship(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-add-additionally-test").SetDisplayName("Relay Planning Add Additionally Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-beta")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mappingA := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	mappingB := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-beta").SetDepartmentName("Department Beta").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{202}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"202": {{"account_id": 11, "priority": 2}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target A", Platform: "openai"}, {ID: 202, Name: "Target B", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}, {GroupID: 202, Priority: 2}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mappingB.ID)
	previewPayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"member_actions":{"%d":{"mode":"add_additionally","from_mapping_id":%d}}}`, alice.ID, alice.ID, alice.ID, mappingA.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, previewPayload)
	executePayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"member_actions":{"%d":{"mode":"add_additionally","from_mapping_id":%d}},"expected_relationship_fingerprint":%q,"operation_key":"add-additionally-1"}`, alice.ID, alice.ID, alice.ID, mappingA.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("add additionally status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if fmt.Sprint(provider.events) != "[subscription-add:42:202]" || len(provider.bound) != 0 || len(provider.removed) != 0 {
		t.Fatalf("add additionally events = %v, bound=%v removed=%v, want only new Target subscription", provider.events, provider.bound, provider.removed)
	}
	if fmt.Sprint(provider.assigned) != "[42:202:365]" {
		t.Fatalf("add additionally assignment = %v, want new Target subscription for 365 days", provider.assigned)
	}
	updatedA := client.RelayGroupMapping.GetX(ctx, mappingA.ID)
	updatedB := client.RelayGroupMapping.GetX(ctx, mappingB.ID)
	if updatedA.MemberAssignments[fmt.Sprint(alice.ID)] != 101 || updatedB.MemberAssignments[fmt.Sprint(alice.ID)] != 202 {
		t.Fatalf("mapping assignments after add additionally = A:%v B:%v", updatedA.MemberAssignments, updatedB.MemberAssignments)
	}
}

func TestRelayPlanningMoveHereRejectsCrossProviderAndCrossPlatformSources(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-transfer-boundary-test").SetDisplayName("Relay Planning Transfer Boundary Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	otherProvider := client.RelayProvider.Create().SetName("relay-planning-transfer-other-test").SetDisplayName("Relay Planning Transfer Other Test").SetBaseURL("https://other-relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-beta")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	destination := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-beta").SetDepartmentName("Department Beta").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{202}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"202": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	crossProvider := client.RelayGroupMapping.Create().
		SetProviderID(otherProvider.ID).SetDepartmentExternalID("dept-provider").SetDepartmentName("Department Provider").SetPlatform("openai").SetTemplateGroupID(10).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetWeeklyCostTarget(2500).SaveX(ctx)
	crossPlatform := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-platform").SetDepartmentName("Department Platform").SetPlatform("anthropic").SetTemplateGroupID(30).SetGroupIds([]int64{301}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 301}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 202, Name: "Target", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 202, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}, {UserID: 42, GroupID: 301, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != providerConfig.ID {
			return nil, fmt.Errorf("unexpected provider %d", providerID)
		}
		return provider, nil
	}), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", destination.ID)
	for name, sourceMappingID := range map[string]int{"cross provider": crossProvider.ID, "cross platform": crossPlatform.ID} {
		t.Run(name, func(t *testing.T) {
			payload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"member_actions":{"%d":{"mode":"move_here","from_mapping_id":%d}}}`, alice.ID, alice.ID, alice.ID, sourceMappingID)
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "selected provider and platform") {
				t.Fatalf("preview status = %d, want boundary rejection, body=%s", response.Code, response.Body.String())
			}
			if len(provider.events) != 0 || provider.accountUpdates != 0 {
				t.Fatalf("Relay writes = events:%v account_updates:%d, want none", provider.events, provider.accountUpdates)
			}
		})
	}
}

func TestRelayPlanningConfirmRejectsChangedRelationshipsBeforeRelayWrites(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-stale-test").
		SetDisplayName("Relay Planning Stale Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetSourceGroupID(20).
		SetGroupIds([]int64{101}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 20, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 20, Key: "test-secret-must-not-leak", Status: "active"}}},
		usage:         map[int64]relay.TeamUserUsageStats{},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)

	previewPayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}]}`, alice.ID, alice.ID)
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID), strings.NewReader(previewPayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var previewBody struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &previewBody); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if previewBody.Data.RelationshipFingerprint == "" {
		t.Fatal("preview relationship fingerprint is empty")
	}
	if provider.relationshipReads.Load() != 1 || provider.subscriptionReads.Load() != 0 || provider.keyReads.Load() != 1 {
		t.Fatalf("Preview relationship reads = snapshot:%d subscriptions:%d keys:%d, want 1/0/1", provider.relationshipReads.Load(), provider.subscriptionReads.Load(), provider.keyReads.Load())
	}
	if len(previewBody.Data.TargetSummaries) != 1 {
		t.Fatalf("target summaries = %+v, want one Target Group summary", previewBody.Data.TargetSummaries)
	}
	summary := previewBody.Data.TargetSummaries[0]
	if summary.TargetGroupID != 101 || len(summary.Accounts) != 0 || len(summary.Members) != 1 || summary.Members[0].Action != "move" || summary.Members[0].FromGroupID != 20 || summary.Members[0].ToGroupID != 101 {
		t.Fatalf("target summary = %+v, want Source 20 to Target 101 member move and no Account changes", summary)
	}
	if len(summary.Subscriptions) != 2 || len(summary.APIKeys) != 1 || summary.APIKeys[0].Count != 1 {
		t.Fatalf("target relationship effects = %+v, want add/remove subscriptions and one API Key move", summary)
	}
	if strings.Contains(response.Body.String(), "test-secret-must-not-leak") {
		t.Fatal("preview response leaked API key secret")
	}
	updatedCost := 1234.0
	updatedTokens := int64(987654)
	provider.usage[42] = relay.TeamUserUsageStats{UserID: 42, RangeActualCost: &updatedCost, RangeTotalTokens: &updatedTokens}
	usageOnlyFingerprint := previewRelayPlanningFingerprint(t, router, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID), previewPayload)
	if usageOnlyFingerprint != previewBody.Data.RelationshipFingerprint {
		t.Fatalf("usage-only fingerprint = %q, want unchanged %q", usageOnlyFingerprint, previewBody.Data.RelationshipFingerprint)
	}

	provider.subscriptions[42] = []relay.UserSubscription{{UserID: 42, GroupID: 101, Status: "active"}}
	executePayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"expected_relationship_fingerprint":%q,"operation_key":"stale-1"}`, alice.ID, alice.ID, previewBody.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mapping.ID), strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("confirm status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	var staleBody struct {
		Details struct {
			ErrorCode     string             `json:"error_code"`
			RefreshedPlan relayplanning.Plan `json:"refreshed_plan"`
			Differences   []string           `json:"differences"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &staleBody); err != nil {
		t.Fatalf("decode stale response: %v", err)
	}
	if staleBody.Details.ErrorCode != "stale_relay_plan" || staleBody.Details.RefreshedPlan.RelationshipFingerprint == previewBody.Data.RelationshipFingerprint || len(staleBody.Details.Differences) == 0 {
		t.Fatalf("stale details = %+v, want refreshed fingerprint and differences", staleBody.Details)
	}
	if !containsRelayPlanningWarning(staleBody.Details.Differences, "subscription relationships changed") {
		t.Fatalf("stale differences = %v, want safe subscription category", staleBody.Details.Differences)
	}
	if len(provider.events) != 0 || provider.accountUpdates != 0 {
		t.Fatalf("Relay writes = events:%v account_updates:%d, want none", provider.events, provider.accountUpdates)
	}

	provider.subscriptions[42] = []relay.UserSubscription{{UserID: 42, GroupID: 20, Status: "active"}}
	provider.users[42] = &relay.User{ID: 42, Username: "other", Email: "other@example.org"}
	request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mapping.ID), strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "Relay user mappings changed") {
		t.Fatalf("identity-invalid confirm status = %d, want categorized 409, body=%s", response.Code, response.Body.String())
	}
	if len(provider.events) != 0 || provider.accountUpdates != 0 {
		t.Fatalf("Relay writes after identity change = events:%v account_updates:%d, want none", provider.events, provider.accountUpdates)
	}

	provider.users[42] = &relay.User{ID: 42, Username: "alice", Email: alice.Email}
	provider.subscriptionError = errors.New("synthetic subscription read failure")
	provider.allowedGroupsError = errors.New("synthetic allowed-group read failure")
	request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mapping.ID), strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "subscription relationships changed") {
		t.Fatalf("subscription-unavailable confirm status = %d, want categorized 409, body=%s", response.Code, response.Body.String())
	}
	if len(provider.events) != 0 || provider.accountUpdates != 0 {
		t.Fatalf("Relay writes after subscription read failure = events:%v account_updates:%d, want none", provider.events, provider.accountUpdates)
	}

	provider.subscriptionError = nil
	provider.allowedGroupsError = nil
	provider.groups = []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}}
	request = httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mapping.ID), strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "migration source") {
		t.Fatalf("missing Group confirm status = %d, want structured 409, body=%s", response.Code, response.Body.String())
	}
	if len(provider.events) != 0 || provider.accountUpdates != 0 {
		t.Fatalf("Relay writes after missing Group = events:%v account_updates:%d, want none", provider.events, provider.accountUpdates)
	}
}

func TestRelayPlanningFingerprintTracksReviewedRelationshipFacts(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-fingerprint-test").
		SetDisplayName("Relay Planning Fingerprint Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetSourceGroupID(20).
		SetGroupIds([]int64{101}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}, 43: {ID: 43, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 20, Status: "active"}}, 43: {{UserID: 43, GroupID: 20, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 20, Status: "active"}}, 43: {{ID: 501, UserID: 43, GroupID: 20, Status: "active"}}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	payload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}]}`, alice.ID, alice.ID)
	baseline := previewRelayPlanningFingerprint(t, router, path, payload)
	provider.subscriptions[42][0].ExpiresAt = time.Date(2030, time.January, 1, 0, 0, 0, 0, time.UTC)
	if got := previewRelayPlanningFingerprint(t, router, path, payload); got != baseline {
		t.Fatalf("generic planning expiry-only fingerprint = %q, want unchanged %q", got, baseline)
	}
	provider.subscriptions[42][0].ExpiresAt = time.Time{}

	provider.groups[2].Platform = "anthropic"
	if got := previewRelayPlanningFingerprint(t, router, path, payload); got == baseline {
		t.Fatal("Target Group Platform change did not change fingerprint")
	}
	provider.groups[2].Platform = "openai"

	provider.accounts[0].GroupRelationships[0].Priority = 2
	if got := previewRelayPlanningFingerprint(t, router, path, payload); got == baseline {
		t.Fatal("Account priority change did not change fingerprint")
	}
	provider.accounts[0].GroupRelationships[0].Priority = 1

	provider.keys[42][0].GroupID = 101
	if got := previewRelayPlanningFingerprint(t, router, path, payload); got == baseline {
		t.Fatal("API Key Group change did not change fingerprint")
	}
	provider.keys[42][0].GroupID = 20

	client.User.UpdateOneID(alice.ID).SetRelayUserID(43).SaveX(ctx)
	if got := previewRelayPlanningFingerprint(t, router, path, payload); got == baseline {
		t.Fatal("local User to Relay ID change did not change fingerprint")
	}
}

func TestRelayPlanningReplanSuggestsStableOptInTargetRenames(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-rename-preview-test").
		SetDisplayName("Relay Planning Rename Preview Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetGroupIds([]int64{102, 101}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		groups: []relay.Group{
			{ID: 10, Name: "Group Alpha", Platform: "openai"},
			{ID: 99, Name: "Department Alpha-openai-01", Platform: "openai"},
			{ID: 101, Name: "Legacy A", Platform: "openai"},
			{ID: 102, Name: "Department Alpha-openai-02", Platform: "openai"},
		},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	handler := NewRelayPlanningHandler(service)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID), strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.Data.Assignments) != 2 {
		t.Fatalf("assignments = %+v, want two managed Targets", body.Data.Assignments)
	}
	first, second := body.Data.Assignments[0], body.Data.Assignments[1]
	if first.TargetGroupID != 102 || first.CurrentTargetGroupName != "Department Alpha-openai-02" || first.SuggestedTargetGroupName != "Department Alpha-openai-04" || first.TargetGroupName != "Department Alpha-openai-02" || first.RenameSelected {
		t.Fatalf("first assignment = %+v, want ID 102 stable suggestion 04 and rename unselected", first)
	}
	if second.TargetGroupID != 101 || second.CurrentTargetGroupName != "Legacy A" || second.SuggestedTargetGroupName != "Department Alpha-openai-03" || second.TargetGroupName != "Legacy A" || second.RenameSelected {
		t.Fatalf("second assignment = %+v, want ID 101 stable suggestion 03 and rename unselected", second)
	}

	reviewedAssignments := `[{"index":0,"target_group_name":"Custom B","rename_selected":true,"user_ids":[]},{"index":1,"target_group_name":"Ignored A","rename_selected":false,"user_ids":[]}]`
	previewPayload := `{"assignments":` + reviewedAssignments + `}`
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(previewPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reviewed preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var reviewedBody struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &reviewedBody); err != nil {
		t.Fatalf("decode reviewed preview: %v", err)
	}
	if len(reviewedBody.Data.TargetSummaries) != 2 || reviewedBody.Data.TargetSummaries[0].Rename == nil || reviewedBody.Data.TargetSummaries[0].Rename.FromName != "Department Alpha-openai-02" || reviewedBody.Data.TargetSummaries[0].Rename.ToName != "Custom B" || reviewedBody.Data.TargetSummaries[1].Rename != nil {
		t.Fatalf("rename summaries = %+v, want only Department Alpha-openai-02 -> Custom B", reviewedBody.Data.TargetSummaries)
	}
	fingerprint := reviewedBody.Data.RelationshipFingerprint
	executePayload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"rename-only-1"}`, reviewedAssignments, fingerprint)
	provider.groups[3].Name = "Changed Elsewhere"
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || len(provider.events) != 0 {
		t.Fatalf("stale current name status/events = %d/%v, want 409 and no Relay writes, body=%s", response.Code, provider.events, response.Body.String())
	}
	provider.groups[3].Name = "Department Alpha-openai-02"
	changedReviewedAssignments := `[{"index":0,"target_group_name":"Other B","rename_selected":true,"user_ids":[]},{"index":1,"rename_selected":false,"user_ids":[]}]`
	changedReviewedPayload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"rename-only-changed-review"}`, changedReviewedAssignments, fingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(changedReviewedPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || len(provider.events) != 0 {
		t.Fatalf("changed reviewed name status/events = %d/%v, want 409 and no Relay writes, body=%s", response.Code, provider.events, response.Body.String())
	}
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("rename-only status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if fmt.Sprint(provider.events) != "[rename:102:Custom B]" {
		t.Fatalf("rename-only events = %v, want only selected Target renamed", provider.events)
	}
	var executeBody struct {
		Data relayplanning.ExecutionResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &executeBody); err != nil {
		t.Fatalf("decode rename-only response: %v", err)
	}
	if len(executeBody.Data.Groups) != 2 || executeBody.Data.Groups[0].Name != "Custom B" || executeBody.Data.Groups[0].Rename != "succeeded" || executeBody.Data.Groups[1].Name != "Legacy A" || executeBody.Data.Groups[1].Rename != "skipped" {
		t.Fatalf("rename-only Group results = %+v", executeBody.Data.Groups)
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if fmt.Sprint(persisted.GroupIds) != "[102 101]" {
		t.Fatalf("mapping Group IDs = %v, want stable IDs", persisted.GroupIds)
	}
}

type relayPlanningAddTargetFixture struct {
	ctx      context.Context
	client   *ent.Client
	dsn      string
	mapping  *ent.RelayGroupMapping
	alice    *ent.User
	provider *relayPlanningSearchProvider
	router   *gin.Engine
	path     string
}

func newRelayPlanningAddTargetFixture(t *testing.T) *relayPlanningAddTargetFixture {
	t.Helper()
	ctx := context.Background()
	client, dsn := testdb.OpenWithDSN(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-replan-add-target-test").
		SetDisplayName("Relay Planning Replan Add Target Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetSourceGroupID(20).
		SetGroupIds([]int64{101}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 12, "priority": 1}}}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups: []relay.Group{
			{ID: 10, Name: "Group Alpha", Platform: "openai"},
			{ID: 20, Name: "Group Source", Platform: "openai"},
			{ID: 101, Name: "Department Alpha-openai-01", Platform: "openai"},
		},
		accounts: []relay.Account{
			{ID: 11, Name: "Template Account", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 10, Priority: 1}}},
			{ID: 12, Name: "Existing Account", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}},
		},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 20, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 20, Status: "active"}}},
		mutateWrites:  true,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	return &relayPlanningAddTargetFixture{ctx: ctx, client: client, dsn: dsn, mapping: mapping, alice: alice, provider: provider, router: router, path: path}
}

func TestRelayPlanningReplanPreviewsProposedTargetWithoutWrites(t *testing.T) {
	fixture := newRelayPlanningAddTargetFixture(t)
	payload := `{"assignments":[{"index":0,"target_group_id":101,"user_ids":[]},{"index":1,"user_ids":[]}]}`
	request := httptest.NewRequest(http.MethodPost, fixture.path, strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode Replan Preview: %v", err)
	}
	if len(body.Data.Assignments) != 2 {
		t.Fatalf("assignments = %+v, want existing plus proposed Target", body.Data.Assignments)
	}
	proposed := body.Data.Assignments[1]
	if proposed.TargetGroupID != 0 || proposed.TargetGroupName != "Department Alpha-openai-02" || len(proposed.Accounts) != 1 || proposed.Accounts[0].ID != 11 {
		t.Fatalf("proposed Target = %+v, want no ID, deterministic name, and Template Account 11", proposed)
	}
	if len(body.Data.TemplateAccounts) != 1 || body.Data.TemplateAccounts[0].ID != 11 {
		t.Fatalf("Template Accounts = %+v, want Account 11 for local Add Group editing", body.Data.TemplateAccounts)
	}
	invalidReviews := []struct {
		name    string
		payload string
	}{
		{name: "omit existing Target", payload: `{"assignments":[]}`},
		{name: "replace stable Target ID", payload: `{"assignments":[{"index":0,"target_group_id":999,"user_ids":[]},{"index":1,"user_ids":[]}]}`},
		{name: "supply proposed Target ID", payload: `{"assignments":[{"index":0,"target_group_id":101,"user_ids":[]},{"index":1,"target_group_id":999,"user_ids":[]}]}`},
		{name: "reuse existing Target name", payload: `{"assignments":[{"index":0,"target_group_id":101,"user_ids":[]},{"index":1,"target_group_name":"Department Alpha-openai-01","user_ids":[]}]}`},
		{name: "control character Target name", payload: "{\"assignments\":[{\"index\":0,\"target_group_id\":101,\"user_ids\":[]},{\"index\":1,\"target_group_name\":\"Invalid\\nTarget\",\"user_ids\":[]}]}"},
	}
	for _, invalid := range invalidReviews {
		t.Run(invalid.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, fixture.path, strings.NewReader(invalid.payload))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			fixture.router.ServeHTTP(response, request)
			if response.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422, body=%s", response.Code, response.Body.String())
			}
		})
	}
	if len(fixture.provider.events) != 0 {
		t.Fatalf("Replan Preview wrote Relay state: %v", fixture.provider.events)
	}
	persisted := fixture.client.RelayGroupMapping.GetX(fixture.ctx, fixture.mapping.ID)
	if !reflect.DeepEqual(persisted.GroupIds, []int64{101}) {
		t.Fatalf("persisted Target IDs = %v, want unchanged [101]", persisted.GroupIds)
	}
}

func TestRelayPlanningReplanCreatesProposedTargetOnConfirm(t *testing.T) {
	fixture := newRelayPlanningAddTargetFixture(t)
	assignments := fmt.Sprintf(`[{"index":0,"target_group_id":101,"user_ids":[]},{"index":1,"target_group_name":"Department Alpha-openai-02","user_ids":[%d]}]`, fixture.alice.ID)
	review := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":%s,"member_sources":{"%d":20}}`, fixture.alice.ID, assignments, fixture.alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, fixture.router, fixture.path, review)
	changedAssignments := strings.Replace(assignments, "Department Alpha-openai-02", "Changed Proposed Target", 1)
	stalePayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":%s,"member_sources":{"%d":20},"expected_relationship_fingerprint":%q,"operation_key":"replan-add-target-stale"}`, fixture.alice.ID, changedAssignments, fixture.alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, fixture.path+"/execute", strings.NewReader(stalePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || len(fixture.provider.events) != 0 {
		t.Fatalf("changed proposed Target status/events = %d/%v, want 409 and no Relay writes, body=%s", response.Code, fixture.provider.events, response.Body.String())
	}

	payload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":%s,"member_sources":{"%d":20},"expected_relationship_fingerprint":%q,"operation_key":"replan-add-target-1"}`, fixture.alice.ID, assignments, fixture.alice.ID, fingerprint)
	request = httptest.NewRequest(http.MethodPost, fixture.path+"/execute", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantEvents := "[duplicate:100 rename:100:Department Alpha-openai-02 account:11:100:1 group-status:100:active subscription-add:42:100 api-key:501:100 subscription-remove:42:20]"
	if fmt.Sprint(fixture.provider.events) != wantEvents {
		t.Fatalf("events = %v, want %s", fixture.provider.events, wantEvents)
	}
	var body struct {
		Data relayplanning.ExecutionResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode Replan Execute: %v", err)
	}
	if len(body.Data.Groups) != 2 || body.Data.Groups[0].ID != 101 || body.Data.Groups[0].Status != "unchanged" || body.Data.Groups[1].ID != 100 || body.Data.Groups[1].Creation != "completed" || body.Data.Groups[1].Status != "succeeded" {
		t.Fatalf("Group results = %+v, want stable existing Target and completed proposed Target", body.Data.Groups)
	}
	persisted := fixture.client.RelayGroupMapping.GetX(fixture.ctx, fixture.mapping.ID)
	if !reflect.DeepEqual(persisted.GroupIds, []int64{101, 100}) {
		t.Fatalf("persisted Target IDs = %v, want [101 100]", persisted.GroupIds)
	}
	aliceKey := fmt.Sprint(fixture.alice.ID)
	if persisted.MemberAssignments[aliceKey] != 100 || persisted.MemberSources[aliceKey] != 20 {
		t.Fatalf("persisted member state = assignments:%v sources:%v, want Alice on Target 100 from Source 20", persisted.MemberAssignments, persisted.MemberSources)
	}
	desired := persisted.DesiredAccounts["100"]
	if len(desired) != 1 || desired[0]["account_id"] != 11 || desired[0]["priority"] != 1 {
		t.Fatalf("proposed Target desired Accounts = %+v, want Template Account 11/1", desired)
	}
}

func TestRelayPlanningReplanRetriesProposedTargetWithoutAnotherDuplicate(t *testing.T) {
	fixture := newRelayPlanningAddTargetFixture(t)
	fixture.provider.renameFailures = map[int64]error{100: errors.New("synthetic proposed Target rename failure")}
	assignments := `[{"index":0,"target_group_id":101,"user_ids":[]},{"index":1,"target_group_name":"Department Alpha-openai-02","user_ids":[]}]`
	fingerprint := previewRelayPlanningFingerprint(t, fixture.router, fixture.path, `{"assignments":`+assignments+`}`)
	payload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"replan-add-target-retry-1"}`, assignments, fingerprint)
	request := httptest.NewRequest(http.MethodPost, fixture.path+"/execute", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || fmt.Sprint(fixture.provider.events) != "[duplicate:100 rename:100:Department Alpha-openai-02]" {
		t.Fatalf("first execute status/events = %d/%v, want one duplicate and failed rename, body=%s", response.Code, fixture.provider.events, response.Body.String())
	}
	persisted := fixture.client.RelayGroupMapping.GetX(fixture.ctx, fixture.mapping.ID)
	if !reflect.DeepEqual(persisted.GroupIds, []int64{101, 100}) || persisted.Status != "needs_retry" {
		t.Fatalf("failed creation mapping = groups:%v status:%s, want [101 100]/needs_retry", persisted.GroupIds, persisted.Status)
	}

	request = httptest.NewRequest(http.MethodPost, fixture.path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry Preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var retryPreview struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &retryPreview); err != nil {
		t.Fatalf("decode retry Preview: %v", err)
	}
	if len(retryPreview.Data.Assignments) != 2 || retryPreview.Data.Assignments[1].TargetGroupID != 100 || !retryPreview.Data.Assignments[1].RenameSelected || retryPreview.Data.Assignments[1].TargetGroupName != "Department Alpha-openai-02" {
		t.Fatalf("retry proposed Target = %+v, want pending Target 100 with reviewed rename", retryPreview.Data.Assignments)
	}
	retryAssignments, err := json.Marshal(retryPreview.Data.Assignments)
	if err != nil {
		t.Fatalf("marshal retry assignments: %v", err)
	}
	delete(fixture.provider.renameFailures, int64(100))
	fixture.provider.events = nil
	retryPayload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"replan-add-target-retry-2"}`, retryAssignments, retryPreview.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, fixture.path+"/execute", strings.NewReader(retryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry Execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantEvents := "[rename:100:Department Alpha-openai-02 account:11:100:1 group-status:100:active]"
	if fmt.Sprint(fixture.provider.events) != wantEvents {
		t.Fatalf("retry events = %v, want %s without another duplicate", fixture.provider.events, wantEvents)
	}
	persisted = fixture.client.RelayGroupMapping.GetX(fixture.ctx, fixture.mapping.ID)
	if persisted.Status != "active" {
		t.Fatalf("mapping status after retry = %q, want active", persisted.Status)
	}
}

func TestRelayPlanningReplanPersistsCreatedTargetBeforeFinalMappingFailure(t *testing.T) {
	fixture := newRelayPlanningAddTargetFixture(t)
	assignments := `[{"index":0,"target_group_id":101,"user_ids":[]},{"index":1,"target_group_name":"Department Alpha-openai-02","user_ids":[]}]`
	fingerprint := previewRelayPlanningFingerprint(t, fixture.router, fixture.path, `{"assignments":`+assignments+`}`)
	payload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"replan-add-target-persistence-1"}`, assignments, fingerprint)
	rawDB, err := sql.Open("postgres", fixture.dsn)
	if err != nil {
		t.Fatalf("open raw test database: %v", err)
	}
	defer rawDB.Close()
	triggerSQL := `
CREATE FUNCTION reject_final_replan_target_update() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.operation_state ? 'account:100:11' THEN
    RAISE EXCEPTION 'synthetic final mapping persistence failure';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER reject_final_replan_target_update BEFORE UPDATE ON relay_group_mappings
FOR EACH ROW EXECUTE FUNCTION reject_final_replan_target_update();`
	if _, err := rawDB.ExecContext(fixture.ctx, triggerSQL); err != nil {
		t.Fatalf("install final Mapping failure trigger: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, fixture.path+"/execute", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("persistence failure status = %d, want 422, body=%s", response.Code, response.Body.String())
	}
	persisted := fixture.client.RelayGroupMapping.GetX(fixture.ctx, fixture.mapping.ID)
	groupState := persisted.OperationState["group:1"]
	if !reflect.DeepEqual(persisted.GroupIds, []int64{101, 100}) || persisted.Status != "needs_retry" || groupState["target_group_id"] != "100" || groupState["creation"] != "pending" {
		t.Fatalf("creation checkpoint = groups:%v status:%s state:%v, want durable Target 100 pending retry", persisted.GroupIds, persisted.Status, groupState)
	}
	desired := persisted.DesiredAccounts["100"]
	if len(desired) != 1 || desired[0]["account_id"] != 11 {
		t.Fatalf("checkpoint desired Accounts = %+v, want Template Account 11", desired)
	}
	if _, err := rawDB.ExecContext(fixture.ctx, `DROP TRIGGER reject_final_replan_target_update ON relay_group_mappings; DROP FUNCTION reject_final_replan_target_update()`); err != nil {
		t.Fatalf("remove final Mapping failure trigger: %v", err)
	}

	request = httptest.NewRequest(http.MethodPost, fixture.path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry Preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var retryPreview struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &retryPreview); err != nil {
		t.Fatalf("decode retry Preview: %v", err)
	}
	if len(retryPreview.Data.Assignments) != 2 || retryPreview.Data.Assignments[1].TargetGroupID != 100 {
		t.Fatalf("retry assignments = %+v, want durable Target 100", retryPreview.Data.Assignments)
	}
	retryAssignments, err := json.Marshal(retryPreview.Data.Assignments)
	if err != nil {
		t.Fatalf("marshal retry assignments: %v", err)
	}
	fixture.provider.events = nil
	retryPayload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"replan-add-target-persistence-2"}`, retryAssignments, retryPreview.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, fixture.path+"/execute", strings.NewReader(retryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	fixture.router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry Execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if containsRelayPlanningEvent(fixture.provider.events, "duplicate:100") {
		t.Fatalf("retry events = %v, must not duplicate the checkpointed Target", fixture.provider.events)
	}
}

func TestRelayPlanningRenameFailureDoesNotBlockAccountsAndReplanRestoresRetry(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().
		SetName("relay-planning-rename-retry-test").
		SetDisplayName("Relay Planning Rename Retry Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetEnabled(true).
		SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).
		SetDepartmentExternalID("dept-alpha").
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetGroupIds([]int64{101}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		groups:         []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 101, Name: "Legacy Target", Platform: "openai"}},
		accounts:       []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai"}},
		renameFailures: map[int64]error{101: errors.New("synthetic rename failure")},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	reviewedAssignments := `[{"index":0,"target_group_name":"Reviewed Target","rename_selected":true,"user_ids":[]}]`
	fingerprint := previewRelayPlanningFingerprint(t, router, path, `{"assignments":`+reviewedAssignments+`}`)
	executePayload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"rename-retry-1"}`, reviewedAssignments, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("first execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if fmt.Sprint(provider.events) != "[rename:101:Reviewed Target account:11:101:1]" {
		t.Fatalf("first execute events = %v, want Account reconcile after failed rename", provider.events)
	}
	var firstBody struct {
		Data relayplanning.ExecutionResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &firstBody); err != nil {
		t.Fatalf("decode first execute: %v", err)
	}
	if len(firstBody.Data.Groups) != 1 || firstBody.Data.Groups[0].Rename != "failed" || len(firstBody.Data.Accounts) != 1 || firstBody.Data.Accounts[0].Status != "succeeded" || firstBody.Data.Mapping == nil || firstBody.Data.Mapping.Status != "needs_retry" {
		t.Fatalf("first execute result = %+v, want independent rename failure and Account success", firstBody.Data)
	}

	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var retryPreview struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &retryPreview); err != nil {
		t.Fatalf("decode retry preview: %v", err)
	}
	if len(retryPreview.Data.Assignments) != 1 || !retryPreview.Data.Assignments[0].RenameSelected || retryPreview.Data.Assignments[0].TargetGroupName != "Reviewed Target" {
		t.Fatalf("retry assignment = %+v, want unresolved rename restored", retryPreview.Data.Assignments)
	}

	delete(provider.renameFailures, int64(101))
	provider.events = nil
	retryAssignments := `[{"index":0,"target_group_name":"Reviewed Target","rename_selected":true,"user_ids":[]}]`
	retryPayload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"rename-retry-2"}`, retryAssignments, retryPreview.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(retryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || fmt.Sprint(provider.events) != "[rename:101:Reviewed Target]" {
		t.Fatalf("retry status/events = %d/%v, want rename only, body=%s", response.Code, provider.events, response.Body.String())
	}
	updated := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if updated.Status != "active" {
		t.Fatalf("mapping status = %q, want active after successful retry", updated.Status)
	}
}

func TestRelayPlanningRetriesNewTargetAfterRenameWithoutDuplicatingAgain(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-create-rename-retry-test").SetDisplayName("Relay Planning Create Rename Retry Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	provider := &relayPlanningSearchProvider{
		groups:         []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}},
		accounts:       []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 10, Priority: 1}}}},
		renameFailures: map[int64]error{100: errors.New("synthetic create rename failure")},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/preview", handler.Preview)
	router.POST("/admin/relay-planning/execute", handler.Execute)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	assignments := `[{"index":0,"target_group_name":"Reviewed Target","user_ids":[]}]`
	previewPayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"weekly_cost_target":2500,"assignments":%s}`, providerConfig.ID, assignments)
	fingerprint := previewRelayPlanningFingerprint(t, router, "/admin/relay-planning/preview", previewPayload)
	executePayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"weekly_cost_target":2500,"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"create-rename-retry-1"}`, providerConfig.ID, assignments, fingerprint)
	request := httptest.NewRequest(http.MethodPost, "/admin/relay-planning/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK || fmt.Sprint(provider.events) != "[duplicate:100 rename:100:Reviewed Target]" {
		t.Fatalf("first execute status/events = %d/%v, body=%s", response.Code, provider.events, response.Body.String())
	}
	mapping := client.RelayGroupMapping.Query().OnlyX(ctx)
	if fmt.Sprint(mapping.GroupIds) != "[100]" || mapping.Status != "needs_retry" {
		t.Fatalf("failed creation mapping = groups:%v status:%s", mapping.GroupIds, mapping.Status)
	}

	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var retryPreview struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &retryPreview); err != nil {
		t.Fatalf("decode retry preview: %v", err)
	}
	delete(provider.renameFailures, int64(100))
	provider.accountFailures = map[int64]error{100: errors.New("synthetic create account failure")}
	provider.events = nil
	retryAssignments := `[{"index":0,"target_group_name":"Reviewed Target","rename_selected":true,"user_ids":[]}]`
	retryPayload := fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"create-rename-retry-2"}`, retryAssignments, retryPreview.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(retryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantEvents := "[rename:100:Reviewed Target account:11:100:1]"
	if fmt.Sprint(provider.events) != wantEvents {
		t.Fatalf("first retry events = %v, want %s without activation or another duplicate", provider.events, wantEvents)
	}
	mapping = client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if mapping.Status != "needs_retry" {
		t.Fatalf("mapping status after Account failure = %q, want needs_retry", mapping.Status)
	}

	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Account retry preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &retryPreview); err != nil {
		t.Fatalf("decode Account retry preview: %v", err)
	}
	delete(provider.accountFailures, int64(100))
	provider.events = nil
	retryPayload = fmt.Sprintf(`{"assignments":%s,"expected_relationship_fingerprint":%q,"operation_key":"create-account-retry-3"}`, retryAssignments, retryPreview.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(retryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("Account retry execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantEvents = "[account:11:100:1 group-status:100:active]"
	if fmt.Sprint(provider.events) != wantEvents {
		t.Fatalf("second retry events = %v, want %s without rename or another duplicate", provider.events, wantEvents)
	}
}

func TestRelayPlanningReplanPreservesPerUserSourceOverride(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-member-source-test").SetDisplayName("Relay Planning Member Source Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 20, Name: "Group Beta", Platform: "openai"}, {ID: 30, Name: "Group Gamma", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 30, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", NewRelayPlanningHandler(service).Replan)
	payload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"member_sources":{"%d":30}}`, alice.ID, alice.ID, alice.ID)
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID), strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode replan response: %v", err)
	}
	if len(body.Data.Candidates) != 1 || body.Data.Candidates[0].SourceGroupID != 30 {
		t.Fatalf("candidate source = %+v, want explicit Group 30", body.Data.Candidates)
	}
}

func TestRelayPlanningReplanIncludesSavedExternalMember(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-external-member-test").SetDisplayName("Relay Planning External Member Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	bob := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").SetRelayUserID(43).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(bob.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(bob.ID): 30}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{
			42: {ID: 42, Username: "alice", Email: alice.Email},
			43: {ID: 43, Username: "bob", Email: bob.Email},
		},
		groups:            []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 20, Name: "Group Beta", Platform: "openai"}, {ID: 30, Name: "Group Gamma", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		subscriptions:     map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 20, Status: "active"}}, 43: {{UserID: 43, GroupID: 30, Status: "active"}, {UserID: 43, GroupID: 101, Status: "active"}}},
		keys:              map[int64][]relay.APIKey{42: {}, 43: {}},
		relationshipPages: [][]int64{{42}, {43}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	handler := NewRelayPlanningHandler(service)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)

	replan := func(payload string) relayplanning.Plan {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID), strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", response.Code, response.Body.String())
		}
		var body struct {
			Data relayplanning.Plan `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode replan response: %v", err)
		}
		return body.Data
	}
	plan := replan(`{}`)
	candidates := make(map[int]relayplanning.Candidate, len(plan.Candidates))
	for _, candidate := range plan.Candidates {
		candidates[candidate.UserID] = candidate
	}
	if candidate, ok := candidates[alice.ID]; !ok || candidate.Selected {
		t.Fatalf("additional department candidate = %+v, want present and unselected among %+v", candidate, plan.Candidates)
	}
	if candidate, ok := candidates[bob.ID]; !ok || !candidate.Selected || candidate.SourceGroupID != 30 {
		t.Fatalf("saved external member = %+v, want selected with Source Group 30 among candidates %+v", candidate, plan.Candidates)
	}
	if len(plan.Assignments) != 1 || plan.Assignments[0].TargetGroupID != 101 || len(plan.Assignments[0].UserIDs) != 1 || plan.Assignments[0].UserIDs[0] != bob.ID {
		t.Fatalf("assignments = %+v, want only saved external user %d in Target Group 101", plan.Assignments, bob.ID)
	}
	if len(plan.TargetSummaries) != 1 || len(plan.TargetSummaries[0].Members) > 0 || len(plan.TargetSummaries[0].Subscriptions) > 0 || len(plan.TargetSummaries[0].APIKeys) > 0 {
		t.Fatalf("unchanged external member produced effects: %+v", plan.TargetSummaries)
	}
	if len(provider.events) > 0 {
		t.Fatalf("Replan wrote Relay state before confirmation: %v", provider.events)
	}
	if provider.relationshipReads.Load() != 1 || provider.groupReads.Load() != 1 || provider.accountReads != 1 {
		t.Fatalf("shared Replan reads = relationships:%d groups:%d accounts:%d, want 1/1/1", provider.relationshipReads.Load(), provider.groupReads.Load(), provider.accountReads)
	}
	if provider.relationshipPageReads.Load() != 2 {
		t.Fatalf("relationship snapshot page reads = %d, want 2", provider.relationshipPageReads.Load())
	}
	if provider.userReads.Load() != 0 || provider.subscriptionReads.Load() != 0 || provider.directoryReads.Load() != 0 {
		t.Fatalf("legacy Replan reads = users:%d subscriptions:%d directory:%d, want 0/0/0", provider.userReads.Load(), provider.subscriptionReads.Load(), provider.directoryReads.Load())
	}
	if provider.keyReads.Load() != 2 {
		t.Fatalf("API Key reads = %d, want once for each relevant user", provider.keyReads.Load())
	}
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[%d]}],"expected_relationship_fingerprint":%q,"operation_key":"unchanged-external-member-1"}`, bob.ID, plan.RelationshipFingerprint)
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mapping.ID), strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unchanged replan status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if len(provider.assigned) != 0 {
		t.Fatalf("unchanged replan assignments = %v, want existing subscription untouched", provider.assigned)
	}
}

func TestRelayPlanningReplanKeepsUnavailableSavedMemberAndBlocksMixedConfirm(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-unavailable-member-test").SetDisplayName("Relay Planning Unavailable Member Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	bob := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(bob.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(bob.ID): 20}).
		SetAccountManagementInitialized(true).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:             map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:            []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 20, Name: "Group Source", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		subscriptions:     map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 20, Status: "active"}}},
		keys:              map[int64][]relay.APIKey{42: {}},
		relationshipPages: [][]int64{{42}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	handler := NewRelayPlanningHandler(service)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)

	preview := func(payload string) (relayplanning.Plan, int, string) {
		t.Helper()
		return previewRelayPlanningResponse(t, router, path, payload)
	}

	initial, status, body := preview(`{}`)
	if status != http.StatusOK {
		t.Fatalf("initial Replan status = %d, want 200, body=%s", status, body)
	}
	if len(initial.Assignments) != 1 || !reflect.DeepEqual(initial.Assignments[0].UserIDs, []int{bob.ID}) {
		t.Fatalf("initial assignments = %+v, want unavailable saved member %d", initial.Assignments, bob.ID)
	}
	if len(initial.Candidates) != 2 || !initial.Candidates[1].Selected || !containsRelayPlanningWarning(initial.Candidates[1].Warnings, "has no relay mapping") {
		t.Fatalf("unavailable candidate = %+v, want selected saved member with warning", initial.Candidates)
	}

	reviewPayload := fmt.Sprintf(`{"selected_user_ids":[%d,%d],"assignments":[{"index":0,"user_ids":[%d,%d]}]}`, alice.ID, bob.ID, alice.ID, bob.ID)
	reviewed, status, body := preview(reviewPayload)
	if status != http.StatusOK {
		t.Fatalf("reviewed Replan status = %d, want 200, body=%s", status, body)
	}
	if len(reviewed.Assignments) != 1 || !reflect.DeepEqual(reviewed.Assignments[0].UserIDs, []int{alice.ID, bob.ID}) {
		t.Fatalf("reviewed assignments = %+v, want valid edit plus unavailable saved member", reviewed.Assignments)
	}

	executePayload := fmt.Sprintf(`{"selected_user_ids":[%d,%d],"assignments":[{"index":0,"user_ids":[%d,%d]}],"expected_relationship_fingerprint":%q,"operation_key":"blocked-unavailable-member-1"}`, alice.ID, bob.ID, alice.ID, bob.ID, reviewed.RelationshipFingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("blocked confirm status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	var staleBody struct {
		Details struct {
			ErrorCode     string             `json:"error_code"`
			RefreshedPlan relayplanning.Plan `json:"refreshed_plan"`
			Differences   []string           `json:"differences"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &staleBody); err != nil {
		t.Fatalf("decode blocked confirm response: %v", err)
	}
	if staleBody.Details.ErrorCode != "stale_relay_plan" || !containsRelayPlanningWarning(staleBody.Details.Differences, "Relay user mappings changed") {
		t.Fatalf("blocked confirm details = %+v, want safe Relay identity difference", staleBody.Details)
	}
	if len(staleBody.Details.RefreshedPlan.Assignments) != 1 || !reflect.DeepEqual(staleBody.Details.RefreshedPlan.Assignments[0].UserIDs, []int{alice.ID, bob.ID}) {
		t.Fatalf("refreshed assignments = %+v, want complete reviewed roster", staleBody.Details.RefreshedPlan.Assignments)
	}
	if len(provider.events) != 0 || len(provider.assigned) != 0 || len(provider.removed) != 0 || len(provider.bound) != 0 || provider.accountUpdates != 0 {
		t.Fatalf("Relay writes = events:%v assigned:%v removed:%v bound:%v account_updates:%d, want none", provider.events, provider.assigned, provider.removed, provider.bound, provider.accountUpdates)
	}

	client.User.DeleteOneID(bob.ID).ExecX(ctx)
	missingLocal, status, body := preview(reviewPayload)
	if status != http.StatusOK {
		t.Fatalf("missing-local Replan status = %d, want 200, body=%s", status, body)
	}
	if len(missingLocal.Assignments) != 1 || !reflect.DeepEqual(missingLocal.Assignments[0].UserIDs, []int{alice.ID, bob.ID}) {
		t.Fatalf("missing-local assignments = %+v, want saved member retained", missingLocal.Assignments)
	}
	if !containsRelayPlanningWarning(missingLocal.Warnings, fmt.Sprintf("user %d has no relay mapping", bob.ID)) {
		t.Fatalf("missing-local warnings = %v, want safe unavailable identity warning", missingLocal.Warnings)
	}
	executePayload = fmt.Sprintf(`{"selected_user_ids":[%d,%d],"assignments":[{"index":0,"user_ids":[%d,%d]}],"expected_relationship_fingerprint":%q,"operation_key":"blocked-missing-local-member-1"}`, alice.ID, bob.ID, alice.ID, bob.ID, missingLocal.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("missing-local confirm status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	if err := json.Unmarshal(response.Body.Bytes(), &staleBody); err != nil {
		t.Fatalf("decode missing-local confirm response: %v", err)
	}
	if len(staleBody.Details.RefreshedPlan.Assignments) != 1 || !reflect.DeepEqual(staleBody.Details.RefreshedPlan.Assignments[0].UserIDs, []int{alice.ID, bob.ID}) {
		t.Fatalf("missing-local refreshed assignments = %+v, want complete reviewed roster", staleBody.Details.RefreshedPlan.Assignments)
	}
}

func TestRelayPlanningReplanKeepsUnavailableSavedTargetAndBlocksMixedConfirm(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-unavailable-target-test").SetDisplayName("Relay Planning Unavailable Target Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	bob := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "bob", "bob@example.org", 43)
	carol := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "carol", "carol@example.net", 44)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101, 102}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101, fmt.Sprint(bob.ID): 102}).
		SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20, fmt.Sprint(bob.ID): 20}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}, "102": {{"account_id": 12, "priority": 1}}}).
		SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{
			42: {ID: 42, Username: "alice", Email: alice.Email},
			43: {ID: 43, Username: "bob", Email: bob.Email},
			44: {ID: 44, Username: "carol", Email: carol.Email},
		},
		groups: []relay.Group{
			{ID: 10, Name: "Group Alpha", Platform: "openai"},
			{ID: 20, Name: "Group Source", Platform: "openai"},
			{ID: 102, Name: "Group Target B", Platform: "openai"},
		},
		accounts: []relay.Account{
			{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}},
			{ID: 12, Name: "Account Beta", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 102, Priority: 1}}},
		},
		subscriptions: map[int64][]relay.UserSubscription{
			42: {{UserID: 42, GroupID: 101, Status: "active"}},
			43: {{UserID: 43, GroupID: 102, Status: "active"}},
			44: {{UserID: 44, GroupID: 20, Status: "active"}},
		},
		keys:              map[int64][]relay.APIKey{42: {}, 43: {}, 44: {}},
		relationshipPages: [][]int64{{42, 43, 44}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	handler := NewRelayPlanningHandler(service)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)

	preview := func(payload string) (relayplanning.Plan, int, string) {
		t.Helper()
		return previewRelayPlanningResponse(t, router, path, payload)
	}

	initial, status, body := preview(`{}`)
	if status != http.StatusOK {
		t.Fatalf("initial Replan status = %d, want 200, body=%s", status, body)
	}
	if len(initial.Assignments) != 2 || initial.Assignments[0].TargetGroupID != 101 || !reflect.DeepEqual(initial.Assignments[0].UserIDs, []int{alice.ID}) || initial.Assignments[1].TargetGroupID != 102 || !reflect.DeepEqual(initial.Assignments[1].UserIDs, []int{bob.ID}) {
		t.Fatalf("initial assignments = %+v, want saved Targets 101/102 and their rosters", initial.Assignments)
	}
	if !containsRelayPlanningWarning(initial.Warnings, "target group 101 is unavailable") {
		t.Fatalf("initial warnings = %v, want safe unavailable Target warning", initial.Warnings)
	}
	if len(initial.TargetSummaries) != 2 || initial.TargetSummaries[0].Rename != nil || len(initial.TargetSummaries[0].Accounts) != 0 || len(initial.TargetSummaries[0].Members) != 0 || len(initial.TargetSummaries[0].Subscriptions) != 0 || len(initial.TargetSummaries[0].APIKeys) != 0 {
		t.Fatalf("unavailable Target summary = %+v, want no synthesized changes", initial.TargetSummaries)
	}

	reviewPayload := fmt.Sprintf(`{"selected_user_ids":[%d,%d,%d],"assignments":[{"index":0,"user_ids":[%d]},{"index":1,"user_ids":[%d,%d]}]}`, alice.ID, bob.ID, carol.ID, alice.ID, bob.ID, carol.ID)
	reviewed, status, body := preview(reviewPayload)
	if status != http.StatusOK {
		t.Fatalf("reviewed Replan status = %d, want 200, body=%s", status, body)
	}
	if len(reviewed.Assignments) != 2 || !reflect.DeepEqual(reviewed.Assignments[0].UserIDs, []int{alice.ID}) || !reflect.DeepEqual(reviewed.Assignments[1].UserIDs, []int{bob.ID, carol.ID}) {
		t.Fatalf("reviewed assignments = %+v, want unavailable Target roster plus valid edit", reviewed.Assignments)
	}

	executePayload := fmt.Sprintf(`{"selected_user_ids":[%d,%d,%d],"assignments":[{"index":0,"user_ids":[%d]},{"index":1,"user_ids":[%d,%d]}],"expected_relationship_fingerprint":%q,"operation_key":"blocked-unavailable-target-1"}`, alice.ID, bob.ID, carol.ID, alice.ID, bob.ID, carol.ID, reviewed.RelationshipFingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("blocked confirm status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	var staleBody struct {
		Details struct {
			ErrorCode     string             `json:"error_code"`
			RefreshedPlan relayplanning.Plan `json:"refreshed_plan"`
			Differences   []string           `json:"differences"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &staleBody); err != nil {
		t.Fatalf("decode blocked confirm response: %v", err)
	}
	if staleBody.Details.ErrorCode != "stale_relay_plan" || !containsRelayPlanningWarning(staleBody.Details.Differences, "Target Group changed") {
		t.Fatalf("blocked confirm details = %+v, want safe Target Group difference", staleBody.Details)
	}
	if len(staleBody.Details.RefreshedPlan.Assignments) != 2 || !reflect.DeepEqual(staleBody.Details.RefreshedPlan.Assignments[0].UserIDs, []int{alice.ID}) || !reflect.DeepEqual(staleBody.Details.RefreshedPlan.Assignments[1].UserIDs, []int{bob.ID, carol.ID}) {
		t.Fatalf("refreshed assignments = %+v, want complete reviewed roster", staleBody.Details.RefreshedPlan.Assignments)
	}
	if len(provider.events) != 0 || len(provider.assigned) != 0 || len(provider.removed) != 0 || len(provider.bound) != 0 || provider.accountUpdates != 0 {
		t.Fatalf("Relay writes = events:%v assigned:%v removed:%v bound:%v account_updates:%d, want none", provider.events, provider.assigned, provider.removed, provider.bound, provider.accountUpdates)
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if !reflect.DeepEqual(persisted.MemberAssignments, map[string]int64{fmt.Sprint(alice.ID): 101, fmt.Sprint(bob.ID): 102}) {
		t.Fatalf("persisted assignments = %v, want unchanged saved roster", persisted.MemberAssignments)
	}
	if len(persisted.OperationState) != 0 {
		t.Fatalf("persisted operation state = %v, want no retry-state write", persisted.OperationState)
	}

	provider.groups = append(provider.groups, relay.Group{ID: 101, Name: "Group Target A", Platform: "openai"})
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("repaired Target with old fingerprint status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	if len(provider.events) != 0 {
		t.Fatalf("Relay writes after stale repaired-Target Confirm = %v, want none", provider.events)
	}

	repaired, status, body := preview(reviewPayload)
	if status != http.StatusOK {
		t.Fatalf("repaired Target Preview status = %d, want 200, body=%s", status, body)
	}
	if containsRelayPlanningWarning(repaired.Warnings, "target group 101 is unavailable") || repaired.Assignments[0].CurrentTargetGroupName != "Group Target A" {
		t.Fatalf("repaired Target plan = warnings:%v assignment:%+v, want normal Target 101", repaired.Warnings, repaired.Assignments[0])
	}
	executePayload = fmt.Sprintf(`{"selected_user_ids":[%d,%d,%d],"assignments":[{"index":0,"user_ids":[%d]},{"index":1,"user_ids":[%d,%d]}],"expected_relationship_fingerprint":%q,"operation_key":"repaired-target-1"}`, alice.ID, bob.ID, carol.ID, alice.ID, bob.ID, carol.ID, repaired.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("repaired Target Confirm status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if !containsRelayPlanningEvent(provider.events, "subscription-add:44:102") || !containsRelayPlanningEvent(provider.events, "subscription-remove:44:20") {
		t.Fatalf("repaired Target writes = %v, want only reviewed Carol move", provider.events)
	}
	persisted = client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if !reflect.DeepEqual(persisted.MemberAssignments, map[string]int64{fmt.Sprint(alice.ID): 101, fmt.Sprint(bob.ID): 102, fmt.Sprint(carol.ID): 102}) {
		t.Fatalf("repaired Target persisted assignments = %v, want reviewed roster", persisted.MemberAssignments)
	}
}

func TestRelayPlanningReplanRemovesOneOfTwoSavedMembers(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-multi-remove-test").SetDisplayName("Relay Planning Multi Remove Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	bob := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "bob", "bob@example.org", 43)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101, fmt.Sprint(bob.ID): 101}).
		SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20, fmt.Sprint(bob.ID): 20}).
		SetAccountManagementInitialized(true).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users: map[int64]*relay.User{
			42: {ID: 42, Username: "alice", Email: alice.Email},
			43: {ID: 43, Username: "bob", Email: bob.Email},
		},
		groups: []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 20, Name: "Group Source", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		subscriptions: map[int64][]relay.UserSubscription{
			42: {{UserID: 42, GroupID: 101, Status: "active"}},
			43: {{UserID: 43, GroupID: 101, Status: "active"}},
		},
		keys:              map[int64][]relay.APIKey{42: {}, 43: {}},
		relationshipPages: [][]int64{{42, 43}},
		mutateWrites:      true,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	handler := NewRelayPlanningHandler(service)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	payload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"removed_user_ids":[%d]}`, alice.ID, alice.ID, bob.ID)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reviewed removal status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode reviewed removal response: %v", err)
	}
	if len(body.Data.Assignments) != 1 || !reflect.DeepEqual(body.Data.Assignments[0].UserIDs, []int{alice.ID}) {
		t.Fatalf("reviewed removal assignments = %+v, want only Alice", body.Data.Assignments)
	}
	if containsRelayPlanningWarning(body.Data.Warnings, fmt.Sprintf("user %d has no relay mapping", bob.ID)) {
		t.Fatalf("reviewed removal warnings = %v, Bob must not be treated as unavailable", body.Data.Warnings)
	}
	executePayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"multi-remove-1"}`, alice.ID, alice.ID, bob.ID, body.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reviewed removal confirm status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if !reflect.DeepEqual(provider.assigned, []string{"43:20:365"}) || !reflect.DeepEqual(provider.removed, []string{"43:101"}) {
		t.Fatalf("reviewed removal writes = assigned:%v removed:%v, want Bob source restore and target removal", provider.assigned, provider.removed)
	}
}

func TestRelayPlanningPreviewRejectsStaleProviderIdentity(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-identity-test").SetDisplayName("Relay Planning Identity Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 99)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{99: {ID: 99, Username: "mallory", Email: "mallory@example.org"}},
		groups:        []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}},
		subscriptions: map[int64][]relay.UserSubscription{99: {}},
		keys:          map[int64][]relay.APIKey{99: {}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	router.POST("/admin/relay-planning/preview", NewRelayPlanningHandler(service).Preview)
	payload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}]}`, providerConfig.ID, alice.ID, alice.ID)
	request := httptest.NewRequest(http.MethodPost, "/admin/relay-planning/preview", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 for stale Provider identity, body=%s", response.Code, response.Body.String())
	}
}

func TestRelayPlanningReplanPreservesSubscriptionStaleCategory(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-subscription-stale-test").SetDisplayName("Relay Planning Subscription Stale Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).
		SetAccountManagementInitialized(true).SetWeeklyCostTarget(2500).SaveX(ctx)
	backing := &relayPlanningSearchProvider{
		users:                 map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		directoryUsers:        []relay.User{{ID: 42, Username: "alice", Email: alice.Email}},
		activeSubscriptionIDs: map[int64][]int64{42: {101}},
		groups:                []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 20, Name: "Group Source", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		subscriptions:         map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:                  map[int64][]relay.APIKey{42: {}},
	}
	provider := &relayPlanningFallbackProvider{Provider: backing, backing: backing}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	handler := NewRelayPlanningHandler(service)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("initial Replan status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var initial struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &initial); err != nil {
		t.Fatalf("decode initial Replan response: %v", err)
	}
	backing.subscriptionError = errors.New("synthetic subscription read failure")
	backing.allowedGroupsError = errors.New("synthetic allowed-group read failure")
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "synthetic subscription read failure") || strings.Contains(response.Body.String(), "synthetic allowed-group read failure") {
		t.Fatalf("subscription Preview status/body = %d/%s, want safe 422 without raw provider errors", response.Code, response.Body.String())
	}
	backing.subscriptionError = nil
	backing.allowedGroupsError = nil
	backing.keyError = errors.New("synthetic API Key read failure")
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "synthetic API Key read failure") {
		t.Fatalf("API Key Preview status/body = %d/%s, want safe 422 without raw provider error", response.Code, response.Body.String())
	}
	backing.keyError = nil
	backing.groupError = errors.New("synthetic group read failure")
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "synthetic group read failure") {
		t.Fatalf("Group Preview status/body = %d/%s, want safe 422 without raw provider error", response.Code, response.Body.String())
	}
	backing.groupError = nil
	backing.accountError = errors.New("synthetic account read failure")
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "synthetic account read failure") {
		t.Fatalf("Account Preview status/body = %d/%s, want safe 422 without raw provider error", response.Code, response.Body.String())
	}
	backing.accountError = nil
	backing.directoryError = errors.New("synthetic directory relationship read failure")
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "synthetic directory relationship read failure") {
		t.Fatalf("Directory relationship Preview status/body = %d/%s, want safe 422 without raw provider error", response.Code, response.Body.String())
	}
	backing.directoryError = nil
	snapshotRouter := gin.New()
	snapshotService := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return backing, nil }), nil)
	snapshotRouter.POST("/admin/relay-planning/mappings/:id/replan", NewRelayPlanningHandler(snapshotService).Replan)
	client.RelayGroupMapping.UpdateOneID(mapping.ID).SetOperationState(map[string]map[string]string{"group:1": {"creation": "pending", "target_group_id": "102"}}).SaveX(ctx)
	backing.pendingGroupError = errors.New("synthetic pending Group read failure")
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	snapshotRouter.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "synthetic pending Group read failure") {
		t.Fatalf("pending Group Preview status/body = %d/%s, want safe 422 without raw provider error", response.Code, response.Body.String())
	}
	backing.pendingGroupError = nil
	client.RelayGroupMapping.UpdateOneID(mapping.ID).SetOperationState(map[string]map[string]string{}).SaveX(ctx)
	backing.relationshipError = errors.New("synthetic relationship snapshot failure")
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	snapshotRouter.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || strings.Contains(response.Body.String(), "synthetic relationship snapshot failure") {
		t.Fatalf("relationship snapshot Preview status/body = %d/%s, want safe 422 without raw provider error", response.Code, response.Body.String())
	}
	backing.relationshipError = nil
	backing.subscriptionError = errors.New("synthetic subscription read failure")
	backing.allowedGroupsError = errors.New("synthetic allowed-group read failure")
	executePayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"expected_relationship_fingerprint":%q,"operation_key":"subscription-stale-1"}`, alice.ID, alice.ID, initial.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict {
		t.Fatalf("subscription stale status = %d, want 409, body=%s", response.Code, response.Body.String())
	}
	var staleBody struct {
		Details struct {
			ErrorCode   string   `json:"error_code"`
			Differences []string `json:"differences"`
		} `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &staleBody); err != nil {
		t.Fatalf("decode subscription stale response: %v", err)
	}
	if staleBody.Details.ErrorCode != "stale_relay_plan" || !containsRelayPlanningWarning(staleBody.Details.Differences, "subscription relationships changed") {
		t.Fatalf("subscription stale details = %+v, want safe subscription category", staleBody.Details)
	}
	if len(backing.events) != 0 || backing.accountUpdates != 0 {
		t.Fatalf("Relay writes = events:%v account_updates:%d, want none", backing.events, backing.accountUpdates)
	}
}

func TestRelayPlanningFailedRemovalRemainsRetryable(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-removal-retry-test").SetDisplayName("Relay Planning Removal Retry Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	bob := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "bob", "bob@example.org", 43)
	alice := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101, fmt.Sprint(bob.ID): 101}).
		SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20, fmt.Sprint(bob.ID): 20}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:          map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}, 43: {ID: 43, Username: "bob", Email: bob.Email}},
		groups:         []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 20, Name: "Group Source", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		accounts:       []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions:  map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}, 43: {{UserID: 43, GroupID: 101, Status: "active"}}},
		keys:           map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}, 43: {}},
		removeFailures: map[int64]error{101: errors.New("synthetic removal failure")},
		mutateWrites:   true,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"removed_user_ids":[%d]}`, bob.ID, bob.ID, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, previewPayload)
	executePayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-retry-1"}`, bob.ID, bob.ID, alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if _, stillDesired := persisted.MemberAssignments[fmt.Sprint(alice.ID)]; stillDesired || persisted.MemberAssignments[fmt.Sprint(bob.ID)] != 101 || persisted.Status != "needs_retry" {
		t.Fatalf("failed removal persistence = assignments:%v status:%s, want updated desired state and retry metadata", persisted.MemberAssignments, persisted.Status)
	}
	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(previewPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var retryPreview struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &retryPreview); err != nil {
		t.Fatalf("decode retry preview: %v", err)
	}
	if len(retryPreview.Data.TargetSummaries) != 1 {
		t.Fatalf("retry summaries = %+v, want one Target summary", retryPreview.Data.TargetSummaries)
	}
	retrySummary := retryPreview.Data.TargetSummaries[0]
	if retrySummary.TargetGroupID != 101 || len(retrySummary.Members) != 1 || retrySummary.Members[0].Action != "remove" || retrySummary.Members[0].FromGroupID != 101 || retrySummary.Members[0].ToGroupID != 20 {
		t.Fatalf("retry member summary = %+v, want removal from Target 101 to Source 20", retrySummary)
	}
	if len(retrySummary.Subscriptions) != 1 || retrySummary.Subscriptions[0].Action != "remove" || retrySummary.Subscriptions[0].GroupID != 101 || len(retrySummary.APIKeys) != 0 {
		t.Fatalf("retry relationship summary = %+v, want only unfinished Target removal", retrySummary)
	}
	delete(provider.removeFailures, int64(101))
	retryPayload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-retry-2"}`, bob.ID, bob.ID, alice.ID, retryPreview.Data.RelationshipFingerprint)
	request = httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(retryPayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("retry execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if retried := client.RelayGroupMapping.GetX(ctx, mapping.ID); retried.Status != "active" {
		t.Fatalf("retry mapping status = %s, want active", retried.Status)
	}
}

func TestRelayPlanningReusesAccountAcrossTargetsWithFreshSnapshots(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-account-reuse-test").SetDisplayName("Relay Planning Account Reuse Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).
		SetGroupIds([]int64{101, 102}).SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}, "102": {{"account_id": 11, "priority": 2}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		groups:                 []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 101, Name: "Group Target A", Platform: "openai"}, {ID: 102, Name: "Group Target B", Platform: "openai"}},
		accounts:               []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai"}},
		enforceAccountSnapshot: true,
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	payload := `{"assignments":[{"index":0,"user_ids":[]},{"index":1,"user_ids":[]}]}`
	fingerprint := previewRelayPlanningFingerprint(t, router, path, payload)
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]},{"index":1,"user_ids":[]}],"expected_relationship_fingerprint":%q,"operation_key":"account-reuse-1"}`, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.ExecutionResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode execute response: %v", err)
	}
	for _, result := range body.Data.Accounts {
		if result.AccountID == 11 && result.Status == "failed" {
			t.Fatalf("Account reuse results = %+v, want both target updates to use fresh snapshots", body.Data.Accounts)
		}
	}
	if !containsRelayPlanningEvent(provider.events, "account:11:101:1") || !containsRelayPlanningEvent(provider.events, "account:11:102:2") {
		t.Fatalf("Account events = %v, want successful updates for both targets", provider.events)
	}
}

func TestRelayPlanningEmptyAccountPoolDeactivatesTarget(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-empty-account-test").SetDisplayName("Relay Planning Empty Account Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).
		SetGroupIds([]int64{101}).SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		groups:   []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		accounts: []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	payload := `{"assignments":[{"index":0,"user_ids":[]}]}`
	fingerprint := previewRelayPlanningFingerprint(t, router, path, payload)
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"expected_relationship_fingerprint":%q,"operation_key":"empty-account-1"}`, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	if !containsRelayPlanningEvent(provider.events, "group-status:101:inactive") {
		t.Fatalf("events = %v, want Target Group deactivation", provider.events)
	}
}

func TestRelayPlanningUninitializedAccountsHaveNoDestructiveSummary(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-uninitialized-summary-test").SetDisplayName("Relay Planning Uninitialized Summary Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").SetTemplateGroupID(10).
		SetGroupIds([]int64{101}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		groups:   []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		accounts: []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", NewRelayPlanningHandler(service).Replan)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"assignments":[{"index":0,"user_ids":[]}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("preview status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode preview response: %v", err)
	}
	if len(body.Data.TargetSummaries) != 1 || len(body.Data.TargetSummaries[0].Accounts) != 0 {
		t.Fatalf("Account summary = %+v, want no changes before Adopt Current", body.Data.TargetSummaries)
	}
}

func createRelayPlanningHandlerDirectory(t *testing.T, ctx context.Context, client *ent.Client, departmentID string) (*ent.DirectorySource, *ent.DirectorySyncRun) {
	t.Helper()
	now := time.Now().UTC()
	source := client.DirectorySource.Create().
		SetName("relay-planning-source").
		SetDescription("Synthetic directory source").
		SetScope(directorysource.ScopeFullCompany).
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode(directorysyncrun.ModeApply).
		SetStatus(directorysyncrun.StatusCompleted).
		SetPhase(directorysyncrun.PhaseCompleted).
		SetCompletedAt(now).
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(source.ID).SetLastRunID(run.ID).SetLastSuccessfulRunID(run.ID).SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID(departmentID).
		SetName("Department Alpha").
		SetPath("synthetic/" + departmentID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	return source, run
}

func createRelayPlanningDirectoryUser(t *testing.T, ctx context.Context, client *ent.Client, source *ent.DirectorySource, run *ent.DirectorySyncRun, departmentID, username, email string, relayUserID int) *ent.User {
	t.Helper()
	local := client.User.Create().SetUsername(username).SetEmail(email).SetAuthSource("ldap").SetRelayUserID(relayUserID).SaveX(ctx)
	member := client.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID("member-" + username).SetEmailNormalized(email).SetDisplayName(username).SetDepartmentExternalID(departmentID).SetMatchedUserID(local.ID).SetLastSeenRunID(run.ID).SaveX(ctx)
	client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(member.ID).SetMemberExternalID(member.ExternalID).SetMemberEmailNormalized(member.EmailNormalized).SetDepartmentExternalID(departmentID).SetLastSeenRunID(run.ID).SaveX(ctx)
	return local
}

func previewRelayPlanningFingerprint(t *testing.T, router http.Handler, path, payload string) string {
	t.Helper()
	plan, status, body := previewRelayPlanningResponse(t, router, path, payload)
	if status != http.StatusOK {
		t.Fatalf("preview status = %d, want 200, body=%s", status, body)
	}
	if plan.RelationshipFingerprint == "" {
		t.Fatal("preview relationship fingerprint is empty")
	}
	return plan.RelationshipFingerprint
}

func previewRelayPlanningResponse(t *testing.T, router http.Handler, path, payload string) (relayplanning.Plan, int, string) {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var body struct {
		Data relayplanning.Plan `json:"data"`
	}
	if response.Code == http.StatusOK {
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode preview response: %v", err)
		}
	}
	return body.Data, response.Code, response.Body.String()
}

func containsRelayPlanningWarning(warnings []string, fragment string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, fragment) {
			return true
		}
	}
	return false
}

func containsRelayPlanningEvent(events []string, target string) bool {
	for _, event := range events {
		if event == target {
			return true
		}
	}
	return false
}
