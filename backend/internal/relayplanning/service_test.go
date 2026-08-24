package relayplanning

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/directorysource"
	"github.com/ai-efficiency/backend/ent/directorysyncrun"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestPreviewRequestJSONUsesSnakeCase(t *testing.T) {
	var got PreviewRequest
	if err := json.Unmarshal([]byte(`{"provider_id":7,"department_id":"dept-alpha","platform":"openai","template_group_id":84,"source_group_id":42,"weekly_cost_target":12.5,"group_count":2,"selected_user_ids":[1,2],"existing_mapping_id":9}`), &got); err != nil {
		t.Fatalf("unmarshal preview request: %v", err)
	}
	if got.ProviderID != 7 || got.DepartmentID != "dept-alpha" || got.Platform != "openai" || got.TemplateGroupID != 84 || got.SourceGroupID != 42 || got.WeeklyCostTarget != 12.5 || got.GroupCount != 2 || len(got.SelectedUserIDs) != 2 || got.ExistingMappingID != 9 {
		t.Fatalf("decoded preview request = %#v", got)
	}
}

func TestNormalizeRequestKeepsTemplateAndSourceIndependent(t *testing.T) {
	got := normalizeRequest(PreviewRequest{DepartmentID: " dept-alpha ", Platform: " openai ", TemplateGroupID: 84, SourceGroupID: 42})
	if got.TemplateGroupID != 84 || got.SourceGroupID != 42 || got.DepartmentID != "dept-alpha" || got.Platform != "openai" {
		t.Fatalf("normalized request = %#v", got)
	}
	got = normalizeRequest(PreviewRequest{TemplateGroupID: 84})
	if got.TemplateGroupID != 84 || got.SourceGroupID != 0 {
		t.Fatalf("source-free request did not remain target-only: %#v", got)
	}
}

func TestValidateAssignmentsAllowsExplicitNonSourceMemberOnce(t *testing.T) {
	assignments, err := validateAssignments([]Assignment{{Index: 0, UserIDs: []int{2}}}, []Candidate{
		{UserID: 1, RangeCost: 10, SourceMember: true, CanAdd: true},
		{UserID: 2, RangeCost: 3, SourceMember: false, CanAdd: true},
	}, 1)
	if err != nil {
		t.Fatalf("validateAssignments() error = %v", err)
	}
	if assignments[0].TotalCost != 3 || len(assignments[0].UserIDs) != 1 || assignments[0].UserIDs[0] != 2 {
		t.Fatalf("validated assignments = %#v", assignments)
	}
}

func TestValidateAssignmentsRejectsDuplicateMember(t *testing.T) {
	_, err := validateAssignments([]Assignment{{Index: 0, UserIDs: []int{1}}, {Index: 1, UserIDs: []int{1}}}, []Candidate{{UserID: 1, CanAdd: true}}, 2)
	if err == nil || !strings.Contains(err.Error(), "assigned more than once") {
		t.Fatalf("validateAssignments() error = %v, want duplicate-member error", err)
	}
}

func TestMappingAvailabilityWarningsArePlatformScoped(t *testing.T) {
	warnings := mappingAvailabilityWarnings(Mapping{Platform: "openai", TemplateGroupID: 11, SourceGroupID: 12, GroupIDs: []int64{13}}, []relay.Group{
		{ID: 11, Platform: "openai"},
		{ID: 12, Platform: "anthropic"},
		{ID: 13, Platform: "openai"},
	})
	if len(warnings) != 1 || warnings[0] != "migration source group 12 is unavailable" {
		t.Fatalf("mapping availability warnings = %#v", warnings)
	}
}

func TestFindGroupForPlatformRejectsCrossPlatformAndMissingGroups(t *testing.T) {
	groups := []relay.Group{{ID: 11, Name: "Group Alpha", Platform: "openai"}, {ID: 12, Platform: "anthropic"}}
	if _, err := findGroupForPlatform(groups, 12, "openai", "template"); err == nil || !strings.Contains(err.Error(), "does not belong to platform") {
		t.Fatalf("cross-platform group error = %v", err)
	}
	if _, err := findGroupForPlatform(groups, 99, "openai", "target"); err == nil || !strings.Contains(err.Error(), "is unavailable") {
		t.Fatalf("missing group error = %v", err)
	}
}

func TestExecutionStateRetainsRetryableStepErrors(t *testing.T) {
	state := executionState("op-1", []GroupResult{{Index: 0, ID: 41, Status: "succeeded"}, {Index: 1, Status: "failed", Error: "upstream timeout"}}, []MemberResult{{UserID: 7, TargetGroupID: 41, Subscription: "succeeded", SourceRemoval: "failed", Error: "remove failed"}, {UserID: 8, TargetGroupID: 41, Subscription: "succeeded", SourceRemoval: "skipped", APIKeys: []string{"9:failed:bind timeout"}}})
	if state["operation"]["status"] != "needs_retry" || state["group:1"]["error"] != "upstream timeout" || state["member:7"]["source_removal"] != "failed" {
		t.Fatalf("execution state = %#v", state)
	}
	if !operationStateNeedsRetry(state, "member:7") || !operationStateNeedsRetry(state, "member:8") || operationStateNeedsRetry(state, "member:9") {
		t.Fatalf("retry lookup = member7:%v member8:%v member9:%v", operationStateNeedsRetry(state, "member:7"), operationStateNeedsRetry(state, "member:8"), operationStateNeedsRetry(state, "member:9"))
	}
}

func TestOperationStateRetryPrefersOriginalSourceForFailedMigration(t *testing.T) {
	state := map[string]map[string]string{
		"member:7": {"subscription": "succeeded", "source_removal": "failed"},
	}
	if !operationStateNeedsRetry(state, "member:7") {
		t.Fatal("failed source removal must remain retryable")
	}
}

func TestPreviewDoesNotCreateDefaultGroupForNonSourceOnlyCandidates(t *testing.T) {
	recommended, count := resolveGroupCount(PreviewRequest{WeeklyCostTarget: 2500}, nil)
	if recommended != 0 || count != 0 {
		t.Fatalf("empty eligible recommendation = %d/%d, want 0/0", recommended, count)
	}
}

func TestAllocatePlacesNextMemberInLowestCostGroup(t *testing.T) {
	candidates := []Candidate{
		{UserID: 1, RangeCost: 8, Eligible: true},
		{UserID: 2, RangeCost: 7, Eligible: true},
		{UserID: 3, RangeCost: 4, Eligible: true},
		{UserID: 4, RangeCost: 3, Eligible: true},
	}
	got := allocate(candidates, 2)
	if len(got) != 2 {
		t.Fatalf("assignment count = %d, want 2", len(got))
	}
	if got[0].TotalCost != 11 || got[1].TotalCost != 11 {
		t.Fatalf("balanced costs = %.1f/%.1f, want 11/11", got[0].TotalCost, got[1].TotalCost)
	}
	if len(got[0].UserIDs) != 2 || len(got[1].UserIDs) != 2 {
		t.Fatalf("user distribution = %#v, want two users per group", got)
	}
}

func TestAllocateUsesStableGroupTieBreak(t *testing.T) {
	got := allocate([]Candidate{{UserID: 2, RangeCost: 1}, {UserID: 1, RangeCost: 1}}, 2)
	if got[0].UserIDs[0] != 2 || got[1].UserIDs[0] != 1 {
		t.Fatalf("tie break assignments = %#v, want insertion order across empty groups", got)
	}
}

func TestAllocateSerializesEmptyGroupsAsEmptyUserLists(t *testing.T) {
	got, err := json.Marshal(allocate(nil, 2))
	if err != nil {
		t.Fatalf("marshal assignments: %v", err)
	}
	if string(got) != `[{"index":0,"total_cost":0,"user_ids":[]},{"index":1,"total_cost":0,"user_ids":[]}]` {
		t.Fatalf("assignments JSON = %s, want empty user arrays", got)
	}
}

func TestResolveGroupCountCapsRecommendationAtEligibleMembers(t *testing.T) {
	candidates := []Candidate{
		{UserID: 1, RangeCost: 10000},
		{UserID: 2, RangeCost: 10000},
		{UserID: 3, RangeCost: 10000},
		{UserID: 4, RangeCost: 10000},
	}
	recommended, count := resolveGroupCount(PreviewRequest{WeeklyCostTarget: 2500, GroupCount: 2}, candidates)
	if recommended != 4 || count != 4 {
		t.Fatalf("group counts = recommended %d / planned %d, want 4 / 4", recommended, count)
	}
}

func TestResolveGroupCountAllowsExplicitReplanResize(t *testing.T) {
	candidates := []Candidate{{UserID: 1, RangeCost: 10000}, {UserID: 2, RangeCost: 10000}}
	recommended, count := resolveGroupCount(PreviewRequest{WeeklyCostTarget: 2500, GroupCount: 1, ExistingMappingID: 9}, candidates)
	if recommended != 2 || count != 1 {
		t.Fatalf("group counts = recommended %d / planned %d, want 2 / 1", recommended, count)
	}
}

func TestProposedGroupNamesSkipOccupiedDepartmentSequence(t *testing.T) {
	groups := []relay.Group{
		{Name: "Group Alpha"},
		{Name: "Department Alpha-openai-01"},
		{Name: "Department Alpha-openai-03"},
	}
	got := proposedGroupNames("Department Alpha", "openai", groups, 3)
	want := []string{"Department Alpha-openai-02", "Department Alpha-openai-04", "Department Alpha-openai-05"}
	if len(got) != len(want) {
		t.Fatalf("proposed names = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("proposed names = %#v, want %#v", got, want)
		}
	}
}

func TestMergeTrendUsageFillsMissingRangeTokens(t *testing.T) {
	stats := map[int64]relay.TeamUserUsageStats{
		101: {UserID: 101},
		102: {UserID: 102, RangeTotalTokens: int64Pointer(9)},
	}
	firstTokens, secondTokens := int64(120), int64(80)
	mergeTrendUsage(stats, map[int64][]relay.UsageTrendPoint{
		101: {
			{Date: "2026-08-01", TotalTokens: &firstTokens, ActualCost: 1.25},
			{Date: "2026-08-02", TotalTokens: &secondTokens, ActualCost: 2.75},
		},
	})
	if stats[101].RangeTotalTokens == nil || *stats[101].RangeTotalTokens != 200 {
		t.Fatalf("range tokens = %#v, want 200", stats[101].RangeTotalTokens)
	}
	if stats[101].RangeActualCost == nil || *stats[101].RangeActualCost != 4 {
		t.Fatalf("range cost = %#v, want 4", stats[101].RangeActualCost)
	}
	if *stats[102].RangeTotalTokens != 9 {
		t.Fatalf("existing range tokens overwritten: %d", *stats[102].RangeTotalTokens)
	}
}

type usageStatsTestProvider struct {
	relay.Provider
	trend map[int64][]relay.UsageTrendPoint
}

func (p usageStatsTestProvider) GetBatchUserUsageStats(_ context.Context, ids []int64, _ relay.TeamUsageSummaryParams) (map[int64]relay.TeamUserUsageStats, error) {
	stats := make(map[int64]relay.TeamUserUsageStats, len(ids))
	for _, id := range ids {
		cost := 7.5
		stats[id] = relay.TeamUserUsageStats{UserID: id, RangeActualCost: &cost}
	}
	return stats, nil
}

func (p usageStatsTestProvider) GetUsageTrendForUsers(_ context.Context, _ []int64, _ relay.TeamMemberTrendParams) (map[int64][]relay.UsageTrendPoint, error) {
	return p.trend, nil
}

func TestUsageStatsUsesTrendTokensWhenBatchSummaryOmitsThem(t *testing.T) {
	first, second := int64(120), int64(80)
	provider := usageStatsTestProvider{trend: map[int64][]relay.UsageTrendPoint{
		101: {{TotalTokens: &first}, {TotalTokens: &second}},
	}}
	got, err := usageStats(context.Background(), provider, []int64{101})
	if err != nil {
		t.Fatalf("usageStats() error = %v", err)
	}
	if got[101].RangeTotalTokens == nil || *got[101].RangeTotalTokens != 200 {
		t.Fatalf("range tokens = %#v, want 200", got[101].RangeTotalTokens)
	}
}

type prewarmStatsReaderStub struct {
	stats   map[int64]relay.TeamUserUsageStats
	outcome teamusage.PrewarmReadOutcome
	err     error
	calls   int
}

func (r *prewarmStatsReaderStub) ReadAuthorizedStats(context.Context, teamusage.PrewarmReadRequest) (map[int64]relay.TeamUserUsageStats, teamusage.PrewarmReadOutcome, error) {
	r.calls++
	return r.stats, r.outcome, r.err
}

func TestLoadUsageStatsUsesPrewarmFullHitWithoutRelayFallback(t *testing.T) {
	tokens := int64(321)
	cost := 12.5
	reader := &prewarmStatsReaderStub{outcome: teamusage.PrewarmReadFullHit, stats: map[int64]relay.TeamUserUsageStats{
		101: {UserID: 101, RangeActualCost: &cost, RangeTotalTokens: &tokens},
	}}
	service := &Service{prewarmReader: reader}

	got, err := service.loadUsageStats(context.Background(), nil, 7, 11, []int64{101})
	if err != nil {
		t.Fatalf("loadUsageStats() error = %v", err)
	}
	if reader.calls != 1 || got[101].RangeTotalTokens == nil || *got[101].RangeTotalTokens != 321 {
		t.Fatalf("loadUsageStats() = %#v after %d cache calls, want prewarm values", got, reader.calls)
	}
}

func TestLoadUsageStatsFallsBackWhenPrewarmMisses(t *testing.T) {
	first, second := int64(120), int64(80)
	provider := usageStatsTestProvider{trend: map[int64][]relay.UsageTrendPoint{
		101: {{TotalTokens: &first}, {TotalTokens: &second}},
	}}
	reader := &prewarmStatsReaderStub{outcome: teamusage.PrewarmReadMiss}
	service := &Service{prewarmReader: reader}

	got, err := service.loadUsageStats(context.Background(), provider, 7, 11, []int64{101})
	if err != nil {
		t.Fatalf("loadUsageStats() fallback error = %v", err)
	}
	if reader.calls != 1 || got[101].RangeTotalTokens == nil || *got[101].RangeTotalTokens != 200 {
		t.Fatalf("loadUsageStats() fallback = %#v after %d cache calls, want exact trend values", got, reader.calls)
	}
}

type concurrentCandidateFactsProvider struct {
	relay.Provider
	started chan string
	release chan struct{}
}

func (p concurrentCandidateFactsProvider) ListUserSubscriptions(context.Context, int64) ([]relay.UserSubscription, error) {
	p.started <- "subscriptions"
	<-p.release
	return []relay.UserSubscription{
		{GroupID: 42, Status: "active"},
		{GroupID: 84, Status: "active"},
	}, nil
}

func (p concurrentCandidateFactsProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) {
	p.started <- "api_keys"
	<-p.release
	return []relay.APIKey{{ID: 7, GroupID: 42}}, nil
}

func (p concurrentCandidateFactsProvider) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	return nil, errors.New("slow allowed-group fallback should not run")
}

func TestLoadCandidateRelayFactsUsesSubscriptionsAndRunsIndependentReadsConcurrently(t *testing.T) {
	provider := concurrentCandidateFactsProvider{
		started: make(chan string, 2),
		release: make(chan struct{}),
	}
	done := make(chan candidateRelayFacts, 1)
	go func() {
		done <- loadCandidateRelayFacts(context.Background(), provider, 101, relay.Group{ID: 42, Platform: "openai"}, "openai")
	}()

	started := map[string]bool{}
	for len(started) < 2 {
		select {
		case operation := <-provider.started:
			started[operation] = true
		case <-time.After(250 * time.Millisecond):
			close(provider.release)
			t.Fatalf("candidate Relay reads started sequentially: %#v", started)
		}
	}
	close(provider.release)
	facts := <-done
	if facts.groupErr != nil || facts.keyErr != nil {
		t.Fatalf("candidate facts errors = %v / %v", facts.groupErr, facts.keyErr)
	}
	if !facts.eligible || facts.migratableKeyCount != 1 {
		t.Fatalf("candidate facts = %#v, want eligible with one migratable key", facts)
	}
	if len(facts.currentGroupIDs) != 1 || facts.currentGroupIDs[0] != 84 {
		t.Fatalf("current group IDs = %#v, want [84]", facts.currentGroupIDs)
	}
}

type candidateFactsFallbackProvider struct{ relay.Provider }

func (candidateFactsFallbackProvider) ListUserSubscriptions(context.Context, int64) ([]relay.UserSubscription, error) {
	return nil, errors.New("synthetic subscription failure")
}

func (candidateFactsFallbackProvider) ListAllowedGroupsForUser(context.Context, int64) ([]relay.Group, error) {
	return []relay.Group{{ID: 42, Platform: "openai"}}, nil
}

func (candidateFactsFallbackProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) {
	return nil, nil
}

func TestLoadCandidateRelayFactsFallsBackWhenSubscriptionReadFails(t *testing.T) {
	facts := loadCandidateRelayFacts(context.Background(), candidateFactsFallbackProvider{}, 101, relay.Group{ID: 42, Platform: "openai"}, "openai")
	if facts.groupErr != nil || !facts.eligible {
		t.Fatalf("candidate fallback facts = %#v, want eligible allowed-group result", facts)
	}
}

func int64Pointer(value int64) *int64 {
	return &value
}

func TestExecuteReplanRetriesFailedAPIKeyMoveFromPreviousTarget(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerRow, err := client.RelayProvider.Create().
		SetName("relay-planning-test").
		SetDisplayName("Relay Planning Test").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetRelayType("sub2api").
		Save(ctx)
	if err != nil {
		t.Fatalf("create relay provider: %v", err)
	}

	departmentID := "dept-alpha"
	source, run := createRelayPlanningDirectorySnapshot(t, ctx, client, departmentID)
	user := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRelayUserID(1001).
		SaveX(ctx)
	member := client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-alice").
		SetEmailNormalized(user.Email).
		SetDisplayName(user.Username).
		SetDepartmentExternalID(departmentID).
		SetMatchedUserID(user.ID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	client.DirectoryMemberDepartment.Create().
		SetSourceID(source.ID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)

	fake := &replanRetryProvider{
		groups: []relay.Group{
			{ID: 10, Name: "Template", Platform: "openai"},
			{ID: 20, Name: "Source", Platform: "openai"},
			{ID: 101, Name: "Target A", Platform: "openai"},
			{ID: 102, Name: "Target B", Platform: "openai"},
		},
		subscriptions: []relay.UserSubscription{
			{UserID: 1001, GroupID: 20, Status: "active"},
			{UserID: 1001, GroupID: 101, Status: "active"},
		},
		keys:         []relay.APIKey{{ID: 501, UserID: 1001, GroupID: 101, Status: "active"}},
		bindFailures: 1,
	}
	resolver := relayPlanningProviderResolver(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != providerRow.ID {
			return nil, fmt.Errorf("provider id = %d, want %d", providerID, providerRow.ID)
		}
		return fake, nil
	})
	service := NewService(client, resolver, nil)
	mappingRow := client.RelayGroupMapping.Create().
		SetProviderID(providerRow.ID).
		SetDepartmentExternalID(departmentID).
		SetDepartmentName("Department Alpha").
		SetPlatform("openai").
		SetTemplateGroupID(10).
		SetTemplateGroupName("Template").
		SetSourceGroupID(20).
		SetSourceGroupName("Source").
		SetGroupIds([]int64{101, 102}).
		SetMemberAssignments(map[string]int64{fmt.Sprint(user.ID): 101}).
		SetMemberSources(map[string]int64{fmt.Sprint(user.ID): 20}).
		SetStatus("active").
		SetWeeklyCostTarget(2500).
		SaveX(ctx)

	replanRequest := ExecuteRequest{
		PreviewRequest: PreviewRequest{Assignments: []Assignment{
			{Index: 0, UserIDs: []int{}},
			{Index: 1, UserIDs: []int{user.ID}},
		}},
		OperationKey: "replan-op-1",
	}
	preview, err := service.Replan(ctx, mappingRow.ID, nil, replanRequest.Assignments, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first Replan() error = %v", err)
	}
	replanRequest.ExpectedRelationshipFingerprint = preview.RelationshipFingerprint
	first, err := service.ExecuteReplan(ctx, mappingRow.ID, replanRequest)
	if err != nil {
		t.Fatalf("first ExecuteReplan() error = %v", err)
	}
	if len(first.Members) != 1 || first.Members[0].Error == "" {
		t.Fatalf("first replan members = %#v, want API key failure", first.Members)
	}
	if len(first.Members[0].APIKeys) != 1 || !strings.Contains(first.Members[0].APIKeys[0], "failed") {
		t.Fatalf("first replan API keys = %#v, want failed move", first.Members[0].APIKeys)
	}
	if fake.assignmentCalls != 1 {
		t.Fatalf("subscription assignments after first attempt = %d, want 1", fake.assignmentCalls)
	}
	if fmt.Sprint(fake.assignmentValidityDays) != "[365]" {
		t.Fatalf("subscription validity after first attempt = %v, want [365]", fake.assignmentValidityDays)
	}

	fake.mu.Lock()
	fake.bindFailures = 0
	fake.mu.Unlock()
	retryRequest := ExecuteRequest{
		PreviewRequest: PreviewRequest{Assignments: []Assignment{
			{Index: 0, UserIDs: []int{}},
			{Index: 1, UserIDs: []int{user.ID}},
		}},
		OperationKey: "replan-op-2",
	}
	preview, err = service.Replan(ctx, mappingRow.ID, nil, retryRequest.Assignments, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("retry Replan() error = %v", err)
	}
	retryRequest.ExpectedRelationshipFingerprint = preview.RelationshipFingerprint
	second, err := service.ExecuteReplan(ctx, mappingRow.ID, retryRequest)
	if err != nil {
		t.Fatalf("retry ExecuteReplan() error = %v", err)
	}
	if len(second.Members) != 1 || second.Members[0].Error != "" {
		t.Fatalf("retry replan members = %#v, want success", second.Members)
	}
	if len(second.Members[0].APIKeys) != 1 || !strings.Contains(second.Members[0].APIKeys[0], "succeeded") {
		t.Fatalf("retry replan API keys = %#v, want successful move from previous target", second.Members[0].APIKeys)
	}
	if len(fake.bound) != 1 || fake.bound[0] != "501:102" {
		t.Fatalf("bound API keys = %#v, want [501:102]", fake.bound)
	}
	if fake.assignmentCalls != 1 {
		t.Fatalf("subscription assignments after retry = %d, want successful step not repeated", fake.assignmentCalls)
	}
	if fmt.Sprint(fake.assignmentValidityDays) != "[365]" {
		t.Fatalf("subscription validity after retry = %v, want successful 365-day step not repeated", fake.assignmentValidityDays)
	}

	updated := client.RelayGroupMapping.GetX(ctx, mappingRow.ID)
	retryState := updated.OperationState
	retryState["operation"]["status"] = "needs_retry"
	retryEntry := retryState[fmt.Sprintf("member:%d", user.ID)]
	retryEntry["subscription"] = "succeeded"
	retryEntry["source_removal"] = "failed"
	retryEntry["error"] = "synthetic source removal failure"
	retryEntry["from_group_id"] = "102"
	retryEntry["target_group_id"] = "102"
	client.RelayGroupMapping.UpdateOneID(mappingRow.ID).SetOperationState(retryState).SetStatus("needs_retry").SaveX(ctx)
	fake.mu.Lock()
	fake.subscriptions = []relay.UserSubscription{{UserID: 1001, GroupID: 20, Status: "active"}, {UserID: 1001, GroupID: 102, Status: "active"}}
	fake.mu.Unlock()
	changedTargetRequest := ExecuteRequest{
		PreviewRequest: PreviewRequest{Assignments: []Assignment{
			{Index: 0, UserIDs: []int{user.ID}},
			{Index: 1, UserIDs: []int{}},
		}},
		OperationKey: "replan-op-3",
	}
	preview, err = service.Replan(ctx, mappingRow.ID, nil, changedTargetRequest.Assignments, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("changed-target Replan() error = %v", err)
	}
	changedTargetRequest.ExpectedRelationshipFingerprint = preview.RelationshipFingerprint
	if _, err := service.ExecuteReplan(ctx, mappingRow.ID, changedTargetRequest); err != nil {
		t.Fatalf("changed-target ExecuteReplan() error = %v", err)
	}
	if fake.assignmentCalls != 2 {
		t.Fatalf("subscription assignments after changed-target retry = %d, want new Target assigned", fake.assignmentCalls)
	}
	if fmt.Sprint(fake.assignmentValidityDays) != "[365 365]" {
		t.Fatalf("subscription validity after changed-target retry = %v, want each new Target assigned for 365 days", fake.assignmentValidityDays)
	}
}

func createRelayPlanningDirectorySnapshot(t *testing.T, ctx context.Context, client *ent.Client, departmentID string) (*ent.DirectorySource, *ent.DirectorySyncRun) {
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
	client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		SaveX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID(departmentID).
		SetName("Department Alpha").
		SetPath("synthetic/" + departmentID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)
	return source, run
}

type replanRetryProvider struct {
	relay.Provider
	mu                     sync.Mutex
	groups                 []relay.Group
	subscriptions          []relay.UserSubscription
	keys                   []relay.APIKey
	bindFailures           int
	bound                  []string
	assignmentCalls        int
	assignmentValidityDays []int
}

type relayPlanningProviderResolver func(context.Context, int) (relay.Provider, error)

func (f relayPlanningProviderResolver) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

func (p *replanRetryProvider) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	return append([]relay.Group(nil), p.groups...), nil
}

func (p *replanRetryProvider) GetUser(_ context.Context, userID int64) (*relay.User, error) {
	return &relay.User{ID: userID, Username: "alice", Email: "alice@example.com"}, nil
}

func (p *replanRetryProvider) ListUserSubscriptions(_ context.Context, userID int64) ([]relay.UserSubscription, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	items := make([]relay.UserSubscription, 0, len(p.subscriptions))
	for _, item := range p.subscriptions {
		if item.UserID == 0 || item.UserID == userID {
			items = append(items, item)
		}
	}
	return items, nil
}

func (p *replanRetryProvider) ListUserAPIKeys(context.Context, int64) ([]relay.APIKey, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]relay.APIKey(nil), p.keys...), nil
}

func (p *replanRetryProvider) ListAccountsForPlatform(context.Context, string) ([]relay.Account, error) {
	return nil, nil
}

func (p *replanRetryProvider) GetUsageStats(context.Context, int64, time.Time, time.Time) (*relay.UsageStats, error) {
	return &relay.UsageStats{TotalTokens: 100, TotalCost: 10}, nil
}

func (p *replanRetryProvider) AssignSubscriptionForUser(_ context.Context, _, _ int64, validityDays int) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.assignmentCalls++
	p.assignmentValidityDays = append(p.assignmentValidityDays, validityDays)
	return nil
}

func (p *replanRetryProvider) RemoveSubscriptionForUser(_ context.Context, userID, groupID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	filtered := p.subscriptions[:0]
	for _, item := range p.subscriptions {
		if item.UserID == userID && item.GroupID == groupID {
			continue
		}
		filtered = append(filtered, item)
	}
	p.subscriptions = filtered
	return nil
}

func (p *replanRetryProvider) BindAPIKeyToGroup(_ context.Context, keyID, groupID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.bindFailures > 0 {
		p.bindFailures--
		return errors.New("synthetic key move failure")
	}
	for index := range p.keys {
		if p.keys[index].ID == keyID {
			p.keys[index].GroupID = groupID
		}
	}
	p.bound = append(p.bound, fmt.Sprintf("%d:%d", keyID, groupID))
	return nil
}
