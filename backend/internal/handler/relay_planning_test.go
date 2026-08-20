package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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
)

type relayPlanningResolverFunc func(context.Context, int) (relay.Provider, error)

func (f relayPlanningResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type relayPlanningSearchProvider struct {
	relay.Provider
	users                  map[int64]*relay.User
	groups                 []relay.Group
	subscriptions          map[int64][]relay.UserSubscription
	keys                   map[int64][]relay.APIKey
	usage                  map[int64]relay.TeamUserUsageStats
	subscriptionError      error
	allowedGroupsError     error
	assigned               []string
	removed                []string
	removeFailures         map[int64]error
	bound                  []string
	accounts               []relay.Account
	accountReads           int
	accountUpdates         int
	accountFailures        map[int64]error
	enforceAccountSnapshot bool
	events                 []string
	subscriptionReads      atomic.Int64
	keyReads               atomic.Int64
}

func (p *relayPlanningSearchProvider) GetUser(_ context.Context, userID int64) (*relay.User, error) {
	return p.users[userID], nil
}

func (p *relayPlanningSearchProvider) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	return append([]relay.Group(nil), p.groups...), nil
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
	return &relay.Group{ID: 100, Name: "Group Alpha Copy", Platform: "openai"}, nil
}

func (p *relayPlanningSearchProvider) UpdateGroupStatus(_ context.Context, groupID int64, status string) error {
	p.events = append(p.events, fmt.Sprintf("group-status:%d:%s", groupID, status))
	return nil
}

func (p *relayPlanningSearchProvider) AssignSubscriptionForUser(_ context.Context, userID, groupID int64, _ int) error {
	p.assigned = append(p.assigned, fmt.Sprintf("%d:%d", userID, groupID))
	p.events = append(p.events, fmt.Sprintf("subscription-add:%d:%d", userID, groupID))
	return nil
}

func (p *relayPlanningSearchProvider) RemoveSubscriptionForUser(_ context.Context, userID, groupID int64) error {
	p.removed = append(p.removed, fmt.Sprintf("%d:%d", userID, groupID))
	p.events = append(p.events, fmt.Sprintf("subscription-remove:%d:%d", userID, groupID))
	if err := p.removeFailures[groupID]; err != nil {
		return err
	}
	return nil
}

func (p *relayPlanningSearchProvider) BindAPIKeyToGroup(_ context.Context, keyID, groupID int64) error {
	p.bound = append(p.bound, fmt.Sprintf("%d:%d", keyID, groupID))
	p.events = append(p.events, fmt.Sprintf("api-key:%d:%d", keyID, groupID))
	return nil
}

func (p *relayPlanningSearchProvider) ListAccountsForPlatform(context.Context, string) ([]relay.Account, error) {
	p.accountReads++
	accounts := make([]relay.Account, len(p.accounts))
	for index := range p.accounts {
		accounts[index] = p.accounts[index]
		accounts[index].GroupRelationships = append([]relay.AccountGroupRelationship(nil), p.accounts[index].GroupRelationships...)
	}
	return accounts, nil
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
	if !containsRelayPlanningWarning(body.Data.Warnings, "30-day usage is unknown; capacity may be underestimated") {
		t.Fatalf("warnings = %v, want unknown-usage capacity warning", body.Data.Warnings)
	}
	if containsRelayPlanningWarning(body.Data.Warnings, "user is not a member of the selected source group") {
		t.Fatalf("warnings = %v, source membership must not be required without a source", body.Data.Warnings)
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
			42: {{ID: 501, UserID: 42, GroupID: 20}, {ID: 502, UserID: 42, GroupID: 30}},
			43: {{ID: 503, UserID: 43, GroupID: 30}},
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
	if fmt.Sprint(provider.assigned) != "[42:100 43:100]" {
		t.Fatalf("assigned = %v, want both users added to target", provider.assigned)
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

	reviewedPreviewPayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":20,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d],"desired_accounts":[{"account_id":12,"priority":1}]}]}`, providerConfig.ID, alice.ID, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, "/admin/relay-planning/preview", reviewedPreviewPayload)
	stalePayload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":20,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d],"desired_accounts":[{"account_id":11,"priority":1}]}],"expected_relationship_fingerprint":%q,"operation_key":"create-accounts-stale"}`, providerConfig.ID, alice.ID, alice.ID, fingerprint)
	request = httptest.NewRequest(http.MethodPost, "/admin/relay-planning/execute", strings.NewReader(stalePayload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || len(provider.events) != 0 {
		t.Fatalf("stale reviewed Accounts status/events = %d/%v, want 409 and no Relay writes, body=%s", response.Code, provider.events, response.Body.String())
	}

	payload := fmt.Sprintf(`{"provider_id":%d,"department_id":"dept-alpha","platform":"openai","template_group_id":10,"source_group_id":20,"weekly_cost_target":2500,"group_count":1,"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d],"desired_accounts":[{"account_id":12,"priority":1}]}],"expected_relationship_fingerprint":%q,"operation_key":"create-accounts-1"}`, providerConfig.ID, alice.ID, alice.ID, fingerprint)
	request = httptest.NewRequest(http.MethodPost, "/admin/relay-planning/execute", strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantPrefix := []string{"duplicate:100", "account:12:100:1", "group-status:100:active", "subscription-add:42:100"}
	if len(provider.events) < len(wantPrefix) || fmt.Sprint(provider.events[:len(wantPrefix)]) != fmt.Sprint(wantPrefix) {
		t.Fatalf("creation events = %v, want prefix %v", provider.events, wantPrefix)
	}
	var body struct {
		Data struct {
			Mapping *relayplanning.Mapping `json:"mapping"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode creation response: %v", err)
	}
	if body.Data.Mapping == nil || len(body.Data.Mapping.AccountPools) != 1 || len(body.Data.Mapping.AccountPools[0].Current) != 1 || body.Data.Mapping.AccountPools[0].Current[0].ID != 12 {
		t.Fatalf("creation Account readback = %+v, want only reviewed Account 12 bound to the new Target", body.Data.Mapping)
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

func TestRelayPlanningExplicitRemovalRestoresSavedSourceAndMovesKeysBack(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-remove-test").SetDisplayName("Relay Planning Remove Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
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
		SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).
		SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).
		SetAccountManagementInitialized(true).
		SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).
		SetWeeklyCostTarget(2500).
		SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:         map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:        []relay.Group{{ID: 10, Name: "Template", Platform: "openai"}, {ID: 20, Name: "Source", Platform: "openai"}, {ID: 101, Name: "Target", Platform: "openai"}},
		accounts:      []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", Type: "oauth", Status: "active", Schedulable: true, GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions: map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	previewPath := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d]}`, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, previewPath, previewPayload)
	payload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-1"}`, alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/relay-planning/mappings/%d/replan/execute", mapping.ID), strings.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("remove status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	wantEvents := []string{"subscription-add:42:20", "api-key:501:20", "subscription-remove:42:101"}
	if fmt.Sprint(provider.events) != fmt.Sprint(wantEvents) {
		t.Fatalf("removal events = %v, want %v", provider.events, wantEvents)
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if _, exists := persisted.MemberAssignments[fmt.Sprint(alice.ID)]; exists {
		t.Fatalf("removed member remains in mapping: %v", persisted.MemberAssignments)
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
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d]}`, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, previewPayload)
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-without-source-1"}`, alice.ID, fingerprint)
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
	if retryResponse.Code != http.StatusOK || !containsRelayPlanningEvent(provider.events, "subscription-remove:42:101") {
		t.Fatalf("retry status = %d, events=%v, want preserved Move Here removal, body=%s", retryResponse.Code, provider.events, retryResponse.Body.String())
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
	if provider.subscriptionReads.Load() != 1 || provider.keyReads.Load() != 1 {
		t.Fatalf("Preview relationship reads = subscriptions:%d keys:%d, want one candidate read each", provider.subscriptionReads.Load(), provider.keyReads.Load())
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
		keys:          map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 20}}, 43: {{ID: 501, UserID: 43, GroupID: 20}}},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	payload := fmt.Sprintf(`{"selected_user_ids":[%d],"assignments":[{"index":0,"user_ids":[%d]}]}`, alice.ID, alice.ID)
	baseline := previewRelayPlanningFingerprint(t, router, path, payload)

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

func TestRelayPlanningFailedRemovalRemainsRetryable(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerConfig := client.RelayProvider.Create().SetName("relay-planning-removal-retry-test").SetDisplayName("Relay Planning Removal Retry Test").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-admin-key").SetEnabled(true).SaveX(ctx)
	source, run := createRelayPlanningHandlerDirectory(t, ctx, client, "dept-alpha")
	alice := createRelayPlanningDirectoryUser(t, ctx, client, source, run, "dept-alpha", "alice", "alice@example.com", 42)
	mapping := client.RelayGroupMapping.Create().
		SetProviderID(providerConfig.ID).SetDepartmentExternalID("dept-alpha").SetDepartmentName("Department Alpha").SetPlatform("openai").
		SetTemplateGroupID(10).SetSourceGroupID(20).SetGroupIds([]int64{101}).SetMemberAssignments(map[string]int64{fmt.Sprint(alice.ID): 101}).SetMemberSources(map[string]int64{fmt.Sprint(alice.ID): 20}).
		SetAccountManagementInitialized(true).SetDesiredAccounts(map[string][]map[string]int64{"101": {{"account_id": 11, "priority": 1}}}).SetWeeklyCostTarget(2500).SaveX(ctx)
	provider := &relayPlanningSearchProvider{
		users:          map[int64]*relay.User{42: {ID: 42, Username: "alice", Email: alice.Email}},
		groups:         []relay.Group{{ID: 10, Name: "Group Alpha", Platform: "openai"}, {ID: 20, Name: "Group Source", Platform: "openai"}, {ID: 101, Name: "Group Target", Platform: "openai"}},
		accounts:       []relay.Account{{ID: 11, Name: "Account Alpha", Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}}},
		subscriptions:  map[int64][]relay.UserSubscription{42: {{UserID: 42, GroupID: 101, Status: "active"}}},
		keys:           map[int64][]relay.APIKey{42: {{ID: 501, UserID: 42, GroupID: 101, Status: "active"}}},
		removeFailures: map[int64]error{101: errors.New("synthetic removal failure")},
	}
	service := relayplanning.NewService(client, relayPlanningResolverFunc(func(context.Context, int) (relay.Provider, error) { return provider, nil }), nil)
	handler := NewRelayPlanningHandler(service)
	router := gin.New()
	router.POST("/admin/relay-planning/mappings/:id/replan", handler.Replan)
	router.POST("/admin/relay-planning/mappings/:id/replan/execute", handler.ReplanExecute)
	path := fmt.Sprintf("/admin/relay-planning/mappings/%d/replan", mapping.ID)
	previewPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d]}`, alice.ID)
	fingerprint := previewRelayPlanningFingerprint(t, router, path, previewPayload)
	executePayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-retry-1"}`, alice.ID, fingerprint)
	request := httptest.NewRequest(http.MethodPost, path+"/execute", strings.NewReader(executePayload))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("execute status = %d, want 200, body=%s", response.Code, response.Body.String())
	}
	persisted := client.RelayGroupMapping.GetX(ctx, mapping.ID)
	if _, stillDesired := persisted.MemberAssignments[fmt.Sprint(alice.ID)]; stillDesired || persisted.Status != "needs_retry" {
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
	if len(retrySummary.Subscriptions) != 2 || len(retrySummary.APIKeys) != 1 || retrySummary.APIKeys[0].FromGroupID != 101 || retrySummary.APIKeys[0].ToGroupID != 20 {
		t.Fatalf("retry relationship summary = %+v, want Source restore, Target removal, and API Key move", retrySummary)
	}
	delete(provider.removeFailures, int64(101))
	retryPayload := fmt.Sprintf(`{"assignments":[{"index":0,"user_ids":[]}],"removed_user_ids":[%d],"expected_relationship_fingerprint":%q,"operation_key":"remove-retry-2"}`, alice.ID, retryPreview.Data.RelationshipFingerprint)
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
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(payload))
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
	if body.Data.RelationshipFingerprint == "" {
		t.Fatal("preview relationship fingerprint is empty")
	}
	return body.Data.RelationshipFingerprint
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
