package quotareset

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetapprovalchainnode"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestListApproverCandidatesIncludesMatchedNonRepresentative(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	peer := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", nil, "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", peer.Email, department.ExternalID, &peer.ID)
	member = client.DirectoryMember.UpdateOneID(member.ID).
		SetDisplayName("Alice Example").
		SetMetadata(map[string]any{"wecom_userid": "alice-wecom"}).
		SaveX(ctx)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, department.ExternalID)
	svc := NewService(client, nil, nil, nil)

	resp, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{
		SourceID: source.ID,
		Query:    "Alice",
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListApproverCandidates() error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].UserID != peer.ID || !resp.Items[0].WeComMentionAvailable {
		t.Fatalf("candidates = %#v, want non-representative peer with mention coverage", resp.Items)
	}
	if len(resp.Items[0].DepartmentPaths) != 1 || resp.Items[0].DepartmentPaths[0] != department.Path {
		t.Fatalf("department paths = %#v, want %#v", resp.Items[0].DepartmentPaths, []string{department.Path})
	}
}

func TestListApproverCandidatesExcludesInactiveMembers(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	activeUser := createQuotaResetUser(t, ctx, client, "active-alice", "active-alice@example.com", nil, "user")
	inactiveUser := createQuotaResetUser(t, ctx, client, "inactive-alice", "inactive-alice@example.com", nil, "user")
	disabledUser := createQuotaResetUser(t, ctx, client, "disabled-alice", "disabled-alice@example.com", nil, "user")
	activeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-active-alice", activeUser.Email, department.ExternalID, &activeUser.ID)
	inactiveMember := createQuotaResetMember(t, ctx, client, source.ID, "member-inactive-alice", inactiveUser.Email, department.ExternalID, &inactiveUser.ID)
	disabledMember := createQuotaResetMember(t, ctx, client, source.ID, "member-disabled-alice", disabledUser.Email, department.ExternalID, &disabledUser.ID)
	client.DirectoryMember.UpdateOneID(inactiveMember.ID).SetStatus("inactive").SaveX(ctx)
	client.User.UpdateOneID(disabledUser.ID).SetRelayDisabledAt(time.Now()).SaveX(ctx)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, activeMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, inactiveMember, department.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, disabledMember, department.ExternalID)
	svc := NewService(client, nil, nil, nil)

	resp, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{SourceID: source.ID, Query: "alice", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("ListApproverCandidates() error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].UserID != activeUser.ID {
		t.Fatalf("candidates = %#v, want active non-disabled user only", resp.Items)
	}
}

func TestMatchApproverCandidatesMatchesNormalizedEmailAndCollectsPaths(t *testing.T) {
	user := &ent.User{ID: 7, Username: "alice", Email: "alice@example.com"}
	member := &ent.DirectoryMember{
		ID:              11,
		ExternalID:      "wecom-alice",
		EmailNormalized: "alice@example.com",
		DisplayName:     "Alice Example",
		Status:          "active",
	}
	memberships := []*ent.DirectoryMemberDepartment{
		{DirectoryMemberID: member.ID, DepartmentExternalID: "department-beta"},
		{DirectoryMemberID: member.ID, DepartmentExternalID: "department-alpha"},
	}
	rootID := "department-engineering"
	departments := map[string]*ent.DirectoryDepartment{
		rootID:             {ExternalID: rootID, Name: "Engineering", Path: "1"},
		"department-alpha": {ExternalID: "department-alpha", ParentExternalID: &rootID, Name: "Alpha", Path: "1.2"},
		"department-beta":  {ExternalID: "department-beta", ParentExternalID: &rootID, Name: "Beta", Path: "1.3"},
	}

	items := matchApproverCandidates("ALICE@EXAMPLE.COM", []*ent.DirectoryMember{member}, memberships, departments, []*ent.User{user})

	if len(items) != 1 || items[0].UserID != user.ID {
		t.Fatalf("candidates = %#v, want normalized-email match", items)
	}
	if got := items[0].DepartmentPaths; len(got) != 2 || got[0] != "Engineering / Alpha" || got[1] != "Engineering / Beta" {
		t.Fatalf("department paths = %#v, want sorted current paths", got)
	}
	if !items[0].WeComMentionAvailable {
		t.Fatal("WeComMentionAvailable = false, want external_id fallback coverage")
	}
}

func TestListApprovalChainOptionsFiltersGroupsAndDepartments(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	root := createQuotaResetDepartment(t, ctx, client, source.ID, "department-engineering", "Engineering", nil)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", &root.ExternalID)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "department-beta", "Department Beta", &root.ExternalID)
	alpha = client.DirectoryDepartment.UpdateOneID(alpha.ID).SetPath("1.2").SaveX(ctx)
	beta = client.DirectoryDepartment.UpdateOneID(beta.ID).SetPath("1.3").SaveX(ctx)
	approver := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", nil, "user")
	createQuotaResetApproverConfig(t, ctx, client, source.ID, alpha.ExternalID, alpha.Path, approver.ID)
	client.QuotaResetApproverConfig.Create().
		SetDirectorySourceID(source.ID).
		SetDepartmentExternalID(beta.ExternalID).
		SetDepartmentDisplayPath(beta.Path).
		SetApproverUserID(approver.ID).
		SetEnabled(false).
		SaveX(ctx)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{groups: []relay.Group{
		{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "Subscription"},
		{ID: 43, Name: "Group Beta", Platform: "anthropic", SubscriptionType: "standard"},
	}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	resp, err := svc.ListApprovalChainOptions(ctx)
	if err != nil {
		t.Fatalf("ListApprovalChainOptions() error = %v", err)
	}
	if len(resp.Groups) != 1 || resp.Groups[0].GroupID != "42" || resp.Groups[0].ProviderID != provider.ID {
		t.Fatalf("groups = %#v, want subscription group only", resp.Groups)
	}
	if len(resp.Departments) != 1 || resp.Departments[0].DepartmentExternalID != alpha.ExternalID || resp.Departments[0].ApproverCount != 1 {
		t.Fatalf("departments = %#v, want enabled configured department only", resp.Departments)
	}
	if resp.Departments[0].DepartmentDisplayPath != "Engineering / Department Alpha" {
		t.Fatalf("department display path = %q, want name-based hierarchy", resp.Departments[0].DepartmentDisplayPath)
	}
}

func TestSaveApprovalChainsPreservesOrder(t *testing.T) {
	ctx := context.Background()
	client, svc, source, provider, alpha, beta := setupApprovalChainTest(t, ctx)

	_, err := svc.SaveApprovalChains(ctx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
		ProviderID: provider.ID,
		GroupID:    "42",
		GroupName:  "Group Alpha",
		Enabled:    true,
		Nodes: []ApprovalChainNodeInput{
			{DirectorySourceID: source.ID, DepartmentExternalID: beta.ExternalID, DepartmentDisplayPath: beta.Path},
			{DirectorySourceID: source.ID, DepartmentExternalID: alpha.ExternalID, DepartmentDisplayPath: alpha.Path},
		},
	}}})
	if err != nil {
		t.Fatalf("SaveApprovalChains() error = %v", err)
	}

	resp, err := svc.ListApprovalChains(ctx)
	if err != nil {
		t.Fatalf("ListApprovalChains() error = %v", err)
	}
	if len(resp.Items) != 1 || len(resp.Items[0].Nodes) != 2 {
		t.Fatalf("chains = %#v, want one two-node chain", resp.Items)
	}
	if got := resp.Items[0].Nodes; got[0].DepartmentExternalID != beta.ExternalID || got[1].DepartmentExternalID != alpha.ExternalID {
		t.Fatalf("nodes = %#v, want Beta then Alpha", got)
	}
	positions := client.QuotaResetApprovalChainNode.Query().Order(ent.Asc(quotaresetapprovalchainnode.FieldPosition)).AllX(ctx)
	if len(positions) != 2 || positions[0].Position != 0 || positions[1].Position != 1 {
		t.Fatalf("stored nodes = %#v, want zero-based positions", positions)
	}
}

func TestSaveApprovalChainsRejectsDuplicateDepartment(t *testing.T) {
	ctx := context.Background()
	_, svc, source, provider, alpha, _ := setupApprovalChainTest(t, ctx)

	_, err := svc.SaveApprovalChains(ctx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
		ProviderID: provider.ID,
		GroupID:    "42",
		GroupName:  "Group Alpha",
		Enabled:    true,
		Nodes: []ApprovalChainNodeInput{
			{DirectorySourceID: source.ID, DepartmentExternalID: alpha.ExternalID, DepartmentDisplayPath: alpha.Path},
			{DirectorySourceID: source.ID, DepartmentExternalID: alpha.ExternalID, DepartmentDisplayPath: alpha.Path},
		},
	}}})
	if !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("SaveApprovalChains(duplicate department) error = %v, want ErrInvalidApproverConfig", err)
	}
}

func TestSaveApprovalChainsRejectsDuplicateGroupBeforeReplacingExistingChains(t *testing.T) {
	ctx := context.Background()
	client, svc, source, provider, alpha, beta := setupApprovalChainTest(t, ctx)
	existing := client.QuotaResetApprovalChain.Create().
		SetProviderID(provider.ID).
		SetGroupID("existing").
		SetGroupName("Existing Group").
		SetEnabled(true).
		SaveX(ctx)

	_, err := svc.SaveApprovalChains(ctx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{
		{
			ProviderID: provider.ID,
			GroupID:    "42",
			GroupName:  "Group Alpha",
			Enabled:    true,
			Nodes:      []ApprovalChainNodeInput{{DirectorySourceID: source.ID, DepartmentExternalID: alpha.ExternalID, DepartmentDisplayPath: alpha.Path}},
		},
		{
			ProviderID: provider.ID,
			GroupID:    "42",
			GroupName:  "Group Alpha Duplicate",
			Enabled:    true,
			Nodes:      []ApprovalChainNodeInput{{DirectorySourceID: source.ID, DepartmentExternalID: beta.ExternalID, DepartmentDisplayPath: beta.Path}},
		},
	}})
	if !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("SaveApprovalChains(duplicate group) error = %v, want ErrInvalidApproverConfig", err)
	}
	if row, getErr := client.QuotaResetApprovalChain.Get(ctx, existing.ID); getErr != nil || row.GroupID != "existing" {
		t.Fatalf("existing chain after rejected save = %#v, %v; want unchanged", row, getErr)
	}
}

func TestSaveApprovalChainsRejectsUnknownSubscriptionGroup(t *testing.T) {
	ctx := context.Background()
	_, svc, _, provider, _, _ := setupApprovalChainTest(t, ctx)

	_, err := svc.SaveApprovalChains(ctx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
		ProviderID: provider.ID,
		GroupID:    "404",
		GroupName:  "Unknown Group",
		Enabled:    true,
	}}})
	if !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("SaveApprovalChains(unknown group) error = %v, want ErrInvalidApproverConfig", err)
	}
}

func TestSaveApprovalChainsRejectsDepartmentWithoutConfig(t *testing.T) {
	ctx := context.Background()
	_, svc, source, provider, _, _ := setupApprovalChainTest(t, ctx)
	unconfigured := createQuotaResetDepartment(t, ctx, svc.client, source.ID, "department-gamma", "Department Gamma", nil)

	_, err := svc.SaveApprovalChains(ctx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
		ProviderID: provider.ID,
		GroupID:    "42",
		GroupName:  "Group Alpha",
		Enabled:    true,
		Nodes: []ApprovalChainNodeInput{{
			DirectorySourceID:     source.ID,
			DepartmentExternalID:  unconfigured.ExternalID,
			DepartmentDisplayPath: unconfigured.Path,
		}},
	}}})
	if !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("SaveApprovalChains(unconfigured department) error = %v, want ErrInvalidApproverConfig", err)
	}
}

func TestSaveApproverConfigsRejectsRemovingChainReferencedDepartment(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	approver := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", nil, "user")
	createQuotaResetApproverConfig(t, ctx, client, source.ID, department.ExternalID, department.Path, approver.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	chain := client.QuotaResetApprovalChain.Create().
		SetProviderID(provider.ID).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetEnabled(true).
		SaveX(ctx)
	client.QuotaResetApprovalChainNode.Create().
		SetChainID(chain.ID).
		SetPosition(0).
		SetDirectorySourceID(source.ID).
		SetDepartmentExternalID(department.ExternalID).
		SetDepartmentDisplayPath(department.Path).
		SaveX(ctx)
	svc := NewService(client, nil, nil, nil)

	_, err := svc.SaveApproverConfigs(ctx, SaveApproverConfigsInput{
		ActorUserID: 1,
		Mode:        ApproverConfigSaveModeReplaceAll,
		Items:       []ApproverConfigInput{},
	})
	if !errors.Is(err, ErrApproverConfigReferenced) {
		t.Fatalf("SaveApproverConfigs(remove referenced department) error = %v, want ErrApproverConfigReferenced", err)
	}
	if got := client.QuotaResetApproverConfig.Query().CountX(ctx); got != 1 {
		t.Fatalf("approver config count = %d, want unchanged row", got)
	}
}

func setupApprovalChainTest(t *testing.T, ctx context.Context) (*ent.Client, *Service, *ent.DirectorySource, *ent.RelayProvider, *ent.DirectoryDepartment, *ent.DirectoryDepartment) {
	t.Helper()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "department-beta", "Department Beta", nil)
	approver := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", nil, "user")
	createQuotaResetApproverConfig(t, ctx, client, source.ID, alpha.ExternalID, alpha.Path, approver.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, beta.ExternalID, beta.Path, approver.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	fake := &fakeQuotaResetProvider{groups: []relay.Group{{
		ID:               42,
		Name:             "Group Alpha",
		Platform:         "openai",
		SubscriptionType: "subscription",
	}}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)
	return client, svc, source, provider, alpha, beta
}
