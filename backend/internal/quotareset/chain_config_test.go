package quotareset

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
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

func TestListApproverCandidatesRequiresSourceID(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	createQuotaResetDirectorySource(t, ctx, client)
	svc := NewService(client, nil, nil, nil)

	_, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{})
	if !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("ListApproverCandidates() error = %v, want ErrInvalidApproverConfig", err)
	}
}

func TestListApproverCandidatesRequiresCurrentSourceID(t *testing.T) {
	t.Run("current source", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		current := createQuotaResetDirectorySource(t, ctx, client)
		svc := NewService(client, nil, nil, nil)

		resp, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{SourceID: current.ID, Page: 1, PageSize: 20})
		if err != nil {
			t.Fatalf("ListApproverCandidates(current source) error = %v", err)
		}
		if resp == nil || resp.Page != 1 || resp.PageSize != 20 || resp.Total != 0 || len(resp.Items) != 0 {
			t.Fatalf("ListApproverCandidates(current source) = %#v, want empty paginated response", resp)
		}
	})

	t.Run("stale source", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		stale := createQuotaResetDirectorySource(t, ctx, client)
		createQuotaResetDirectorySource(t, ctx, client)
		svc := NewService(client, nil, nil, nil)

		_, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{SourceID: stale.ID})
		if !errors.Is(err, ErrDirectoryUnavailable) {
			t.Fatalf("ListApproverCandidates(stale source) error = %v, want ErrDirectoryUnavailable", err)
		}
	})

	t.Run("nonexistent source", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		current := createQuotaResetDirectorySource(t, ctx, client)
		svc := NewService(client, nil, nil, nil)

		_, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{SourceID: current.ID + 1000})
		if !errors.Is(err, ErrDirectoryUnavailable) {
			t.Fatalf("ListApproverCandidates(nonexistent source) error = %v, want ErrDirectoryUnavailable", err)
		}
	})

	t.Run("no current source", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		svc := NewService(client, nil, nil, nil)

		_, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{SourceID: 1})
		if !errors.Is(err, ErrDirectoryUnavailable) {
			t.Fatalf("ListApproverCandidates(no current source) error = %v, want ErrDirectoryUnavailable", err)
		}
	})

	t.Run("negative source", func(t *testing.T) {
		ctx := context.Background()
		client := testdb.Open(t)
		svc := NewService(client, nil, nil, nil)

		_, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{SourceID: -1})
		if !errors.Is(err, ErrInvalidApproverConfig) {
			t.Fatalf("ListApproverCandidates(negative source) error = %v, want ErrInvalidApproverConfig", err)
		}
	})
}

func TestListApproverCandidatesRetriesConsistentCurrentSnapshot(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	oldDepartment := createQuotaResetDepartment(t, ctx, client, source.ID, "department-old", "Department Old", nil)
	oldUser := createQuotaResetUser(t, ctx, client, "old-candidate", "old-candidate@example.com", nil, "user")
	oldMember := createQuotaResetMember(t, ctx, client, source.ID, "member-old", oldUser.Email, oldDepartment.ExternalID, &oldUser.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, oldMember, oldDepartment.ExternalID)
	newUser := createQuotaResetUser(t, ctx, client, "new-candidate", "new-candidate@example.com", nil, "user")

	replaced := 0
	client.DirectoryMember.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			value, err := next.Query(ctx, query)
			if err != nil || replaced > 0 {
				return value, err
			}
			replaced++
			if err := replaceCandidateSnapshot(ctx, client, source.ID, newUser, replaced); err != nil {
				return nil, err
			}
			return value, nil
		})
	}))

	resp, err := NewService(client, nil, nil, nil).ListApproverCandidates(ctx, ApproverCandidateParams{
		SourceID: source.ID,
		Page:     1,
		PageSize: 20,
	})
	if err != nil {
		t.Fatalf("ListApproverCandidates(snapshot replacement) error = %v", err)
	}
	if replaced != 1 {
		t.Fatalf("snapshot replacements = %d, want 1", replaced)
	}
	if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].UserID != newUser.ID {
		t.Fatalf("candidates = %#v, want only new snapshot user %d", resp.Items, newUser.ID)
	}
	if !reflect.DeepEqual(resp.Items[0].DepartmentPaths, []string{"Department New 1"}) {
		t.Fatalf("new candidate department paths = %#v", resp.Items[0].DepartmentPaths)
	}
}

func TestListApproverCandidatesReturnsUnavailableWhenSnapshotKeepsChanging(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-initial", "Department Initial", nil)
	user := createQuotaResetUser(t, ctx, client, "candidate", "candidate@example.com", nil, "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-initial", user.Email, department.ExternalID, &user.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, department.ExternalID)

	replacements := 0
	client.DirectoryMember.Intercept(ent.InterceptFunc(func(next ent.Querier) ent.Querier {
		return ent.QuerierFunc(func(ctx context.Context, query ent.Query) (ent.Value, error) {
			value, err := next.Query(ctx, query)
			if err != nil {
				return value, err
			}
			replacements++
			if err := replaceCandidateSnapshot(ctx, client, source.ID, user, replacements); err != nil {
				return nil, err
			}
			return value, nil
		})
	}))

	_, err := NewService(client, nil, nil, nil).ListApproverCandidates(ctx, ApproverCandidateParams{SourceID: source.ID})
	if !errors.Is(err, ErrDirectoryUnavailable) {
		t.Fatalf("ListApproverCandidates(changing snapshot) error = %v, want ErrDirectoryUnavailable", err)
	}
	if replacements != 2 {
		t.Fatalf("snapshot replacements = %d, want bounded 2 attempts", replacements)
	}
}

func replaceCandidateSnapshot(ctx context.Context, client *ent.Client, sourceID int, user *ent.User, generation int) error {
	tx, err := client.Tx(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	run, err := tx.DirectorySyncRun.Create().
		SetSourceID(sourceID).
		SetMode("apply").
		SetStatus("running").
		SetPhase("applying").
		Save(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.DirectoryMemberDepartment.Delete().Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.DirectoryMember.Delete().Exec(ctx); err != nil {
		return err
	}
	if _, err := tx.DirectoryDepartment.Delete().Exec(ctx); err != nil {
		return err
	}
	departmentID := fmt.Sprintf("department-new-%d", generation)
	departmentName := fmt.Sprintf("Department New %d", generation)
	if _, err := tx.DirectoryDepartment.Create().
		SetSourceID(sourceID).
		SetExternalID(departmentID).
		SetName(departmentName).
		SetPath(departmentName).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		return err
	}
	member, err := tx.DirectoryMember.Create().
		SetSourceID(sourceID).
		SetExternalID(fmt.Sprintf("member-new-%d", generation)).
		SetEmailNormalized(user.Email).
		SetDisplayName(user.Username).
		SetDepartmentExternalID(departmentID).
		SetMatchedUserID(user.ID).
		SetLastSeenRunID(run.ID).
		Save(ctx)
	if err != nil {
		return err
	}
	if _, err := tx.DirectoryMemberDepartment.Create().
		SetSourceID(sourceID).
		SetDirectoryMemberID(member.ID).
		SetMemberExternalID(member.ExternalID).
		SetMemberEmailNormalized(member.EmailNormalized).
		SetDepartmentExternalID(departmentID).
		SetLastSeenRunID(run.ID).
		Save(ctx); err != nil {
		return err
	}
	completedAt := time.Date(2026, 7, 14, 12, generation, 0, 0, time.UTC)
	if _, err := tx.DirectorySyncRun.UpdateOneID(run.ID).
		SetStatus("completed").
		SetPhase("completed").
		SetCompletedAt(completedAt).
		Save(ctx); err != nil {
		return err
	}
	if _, err := tx.DirectorySource.UpdateOneID(sourceID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		Save(ctx); err != nil {
		return err
	}
	return tx.Commit()
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

func TestListApproverCandidatesMaxIntPageReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	user := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", nil, "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", user.Email, department.ExternalID, &user.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, department.ExternalID)
	svc := NewService(client, nil, nil, nil)
	maxInt := int(^uint(0) >> 1)

	resp, err := svc.ListApproverCandidates(ctx, ApproverCandidateParams{SourceID: source.ID, Page: maxInt, PageSize: 20})
	if err != nil {
		t.Fatalf("ListApproverCandidates(max page) error = %v", err)
	}
	if len(resp.Items) != 0 || resp.Page != maxInt || resp.PageSize != 20 || resp.Total != 1 {
		t.Fatalf("response = %#v, want empty max-int page with 1 total", resp)
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

func TestSaveApproverConfigsAllowsRemovingDepartmentReferencedOnlyByDisabledChain(t *testing.T) {
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
		SetEnabled(false).
		SaveX(ctx)
	client.QuotaResetApprovalChainNode.Create().
		SetChainID(chain.ID).
		SetPosition(0).
		SetDirectorySourceID(source.ID).
		SetDepartmentExternalID(department.ExternalID).
		SetDepartmentDisplayPath(department.Path).
		SaveX(ctx)
	svc := NewService(client, nil, nil, nil)

	resp, err := svc.SaveApproverConfigs(ctx, SaveApproverConfigsInput{
		ActorUserID: 1,
		Mode:        ApproverConfigSaveModeReplaceAll,
		Items:       []ApproverConfigInput{},
	})
	if err != nil {
		t.Fatalf("SaveApproverConfigs(remove disabled-chain department) error = %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("saved configs = %#v, want empty replacement", resp.Items)
	}
	if got := client.QuotaResetApproverConfig.Query().CountX(ctx); got != 0 {
		t.Fatalf("approver config count = %d, want removed row", got)
	}
}

func TestSaveApproverConfigsReferenceErrorListsEnabledGroupsDeterministically(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	department := createQuotaResetDepartment(t, ctx, client, source.ID, "department-alpha", "Department Alpha", nil)
	approver := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", nil, "user")
	createQuotaResetApproverConfig(t, ctx, client, source.ID, department.ExternalID, department.Path, approver.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	secondProvider := client.RelayProvider.Create().
		SetName("secondary-relay").
		SetDisplayName("Secondary Relay").
		SetBaseURL("https://secondary-relay.example.com").
		SetAdminAPIKey("test-secondary-admin-api-key").
		SetRelayType("sub2api").
		SetEnabled(true).
		SaveX(ctx)
	for _, item := range []struct {
		providerID int
		groupID    string
		groupName  string
		enabled    bool
	}{
		{providerID: secondProvider.ID, groupID: "40", groupName: "Group Gamma", enabled: true},
		{providerID: provider.ID, groupID: "43", groupName: "Group Beta", enabled: true},
		{providerID: provider.ID, groupID: "41", groupName: "Group Disabled", enabled: false},
		{providerID: provider.ID, groupID: "42", groupName: "Group Alpha", enabled: true},
	} {
		chain := client.QuotaResetApprovalChain.Create().
			SetProviderID(item.providerID).
			SetGroupID(item.groupID).
			SetGroupName(item.groupName).
			SetEnabled(item.enabled).
			SaveX(ctx)
		client.QuotaResetApprovalChainNode.Create().
			SetChainID(chain.ID).
			SetPosition(0).
			SetDirectorySourceID(source.ID).
			SetDepartmentExternalID(department.ExternalID).
			SetDepartmentDisplayPath(department.Path).
			SaveX(ctx)
	}
	svc := NewService(client, nil, nil, nil)

	_, err := svc.SaveApproverConfigs(ctx, SaveApproverConfigsInput{
		ActorUserID: 1,
		Mode:        ApproverConfigSaveModeReplaceAll,
		Items:       []ApproverConfigInput{},
	})
	if !errors.Is(err, ErrApproverConfigReferenced) {
		t.Fatalf("SaveApproverConfigs() error = %v, want ErrApproverConfigReferenced", err)
	}
	want := fmt.Sprintf(
		`approver_config_referenced: enabled approval chains reference departments without approver configs: provider_id=%d group_id=42 group_name="Group Alpha", provider_id=%d group_id=43 group_name="Group Beta", provider_id=%d group_id=40 group_name="Group Gamma"`,
		provider.ID,
		provider.ID,
		secondProvider.ID,
	)
	if err.Error() != want {
		t.Fatalf("SaveApproverConfigs() error = %q, want %q", err, want)
	}
}

func TestEnsureApprovalConfigurationLockRowHandlesConcurrentCreation(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, nil)
	const workers = 16
	start := make(chan struct{})
	results := make(chan error, workers)

	for range workers {
		go func() {
			<-start
			results <- svc.ensureApprovalConfigurationLockRow(ctx)
		}()
	}
	close(start)
	for range workers {
		if err := <-results; err != nil {
			t.Fatalf("ensureApprovalConfigurationLockRow() error = %v", err)
		}
	}
	if got := client.SystemSetting.Query().CountX(ctx); got != 1 {
		t.Fatalf("system setting count = %d, want one shared lock row", got)
	}
}

func TestApprovalConfigurationLockSerializesCriticalSections(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	svc := NewService(client, nil, nil, nil)
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	secondAttempted := make(chan struct{})
	secondEntered := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() {
		releaseOnce.Do(func() { close(firstRelease) })
	}
	defer releaseFirst()

	firstCtx := approvalConfigurationLockTestContext(ctx, nil, func() {
		close(firstEntered)
		<-firstRelease
	})
	firstResult := make(chan error, 1)
	go func() {
		tx, err := svc.beginApprovalConfigurationTx(firstCtx)
		if err == nil {
			err = tx.Commit()
		}
		firstResult <- err
	}()
	waitForApprovalConfigurationSignal(t, firstEntered, "first transaction to enter critical section")

	secondCtx := approvalConfigurationLockTestContext(ctx, func() {
		close(secondAttempted)
	}, func() {
		close(secondEntered)
	})
	secondResult := make(chan error, 1)
	go func() {
		tx, err := svc.beginApprovalConfigurationTx(secondCtx)
		if err == nil {
			err = tx.Commit()
		}
		secondResult <- err
	}()
	waitForApprovalConfigurationSignal(t, secondAttempted, "second transaction to attempt lock")
	assertNoApprovalConfigurationSignal(t, secondEntered, "second transaction entered before first released lock")

	releaseFirst()
	if err := waitForApprovalConfigurationResult(t, firstResult, "first transaction"); err != nil {
		t.Fatalf("first transaction error = %v", err)
	}
	waitForApprovalConfigurationSignal(t, secondEntered, "second transaction to enter after first commit")
	if err := waitForApprovalConfigurationResult(t, secondResult, "second transaction"); err != nil {
		t.Fatalf("second transaction error = %v", err)
	}
}

func TestSaveApprovalChainsPausedGroupDiscoveryDoesNotHoldConfigurationLock(t *testing.T) {
	ctx := context.Background()
	client, _, source, provider, alpha, _ := setupApprovalChainTest(t, ctx)
	discoveryEntered := make(chan struct{})
	discoveryRelease := make(chan struct{})
	var releaseOnce sync.Once
	releaseDiscovery := func() {
		releaseOnce.Do(func() { close(discoveryRelease) })
	}
	defer releaseDiscovery()
	fake := &fakeQuotaResetProvider{
		listPlatformGroupsFn: func(ctx context.Context) ([]relay.Group, error) {
			close(discoveryEntered)
			select {
			case <-discoveryRelease:
				return []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)
	saveResult := make(chan approvalChainSaveResult, 1)
	go func() {
		response, err := svc.SaveApprovalChains(ctx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
			ProviderID: provider.ID,
			GroupID:    "42",
			Enabled:    true,
			Nodes: []ApprovalChainNodeInput{{
				DirectorySourceID:    source.ID,
				DepartmentExternalID: alpha.ExternalID,
			}},
		}}})
		saveResult <- approvalChainSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, discoveryEntered, "group discovery to pause")

	readCtx, cancelRead := context.WithTimeout(ctx, 5*time.Second)
	defer cancelRead()
	readResponse, err := svc.ListApprovalChains(readCtx)
	releaseDiscovery()
	saved := waitForApprovalChainSaveResult(t, saveResult, "chain save after group discovery release")
	if err != nil {
		t.Fatalf("ListApprovalChains() while discovery paused error = %v", err)
	}
	if len(readResponse.Items) != 0 {
		t.Fatalf("ListApprovalChains() while discovery paused = %#v, want pre-save empty revision", readResponse.Items)
	}
	if saved.err != nil {
		t.Fatalf("SaveApprovalChains() error = %v", saved.err)
	}
	assertSingleApprovalChain(t, saved.response, "42", alpha.ExternalID)
}

func TestSaveApprovalChainsRevalidatesEnabledProviderAfterDiscovery(t *testing.T) {
	ctx := context.Background()
	client, _, source, provider, alpha, _ := setupApprovalChainTest(t, ctx)
	fake := &fakeQuotaResetProvider{
		listPlatformGroupsFn: func(context.Context) ([]relay.Group, error) {
			client.RelayProvider.UpdateOneID(provider.ID).SetEnabled(false).SaveX(ctx)
			return []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}}, nil
		},
	}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)

	_, err := svc.SaveApprovalChains(ctx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
		ProviderID: provider.ID,
		GroupID:    "42",
		Enabled:    true,
		Nodes: []ApprovalChainNodeInput{{
			DirectorySourceID:    source.ID,
			DepartmentExternalID: alpha.ExternalID,
		}},
	}}})
	if !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("SaveApprovalChains() error = %v, want ErrInvalidApproverConfig", err)
	}
	if got := client.QuotaResetApprovalChain.Query().CountX(ctx); got != 0 {
		t.Fatalf("approval chain count = %d, want no chain for disabled provider", got)
	}
}

func TestSaveApprovalChainsConcurrentFullListSavesAreSerialized(t *testing.T) {
	ctx := context.Background()
	_, svc, source, provider, alpha, beta := setupApprovalChainTest(t, ctx)
	firstEntered := make(chan struct{})
	firstRelease := make(chan struct{})
	secondAttempted := make(chan struct{})
	secondEntered := make(chan struct{})
	var releaseOnce sync.Once
	releaseFirst := func() {
		releaseOnce.Do(func() { close(firstRelease) })
	}
	defer releaseFirst()

	firstInput := SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
		ProviderID: provider.ID,
		GroupID:    "42",
		GroupName:  "Group Alpha",
		Enabled:    true,
		Nodes: []ApprovalChainNodeInput{{
			DirectorySourceID:     source.ID,
			DepartmentExternalID:  alpha.ExternalID,
			DepartmentDisplayPath: alpha.Path,
		}},
	}}}
	secondInput := SaveApprovalChainsInput{ActorUserID: 10, Items: []ApprovalChainInput{{
		ProviderID: provider.ID,
		GroupID:    "43",
		GroupName:  "Group Beta",
		Enabled:    true,
		Nodes: []ApprovalChainNodeInput{{
			DirectorySourceID:     source.ID,
			DepartmentExternalID:  beta.ExternalID,
			DepartmentDisplayPath: beta.Path,
		}},
	}}}

	firstCtx := approvalConfigurationLockTestContext(ctx, nil, func() {
		close(firstEntered)
		<-firstRelease
	})
	firstResult := make(chan approvalChainSaveResult, 1)
	go func() {
		response, err := svc.SaveApprovalChains(firstCtx, firstInput)
		firstResult <- approvalChainSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, firstEntered, "first chain save to enter critical section")

	secondCtx := approvalConfigurationLockTestContext(ctx, func() {
		close(secondAttempted)
	}, func() {
		close(secondEntered)
	})
	secondResult := make(chan approvalChainSaveResult, 1)
	go func() {
		response, err := svc.SaveApprovalChains(secondCtx, secondInput)
		secondResult <- approvalChainSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, secondAttempted, "second chain save to attempt lock")
	assertNoApprovalConfigurationSignal(t, secondEntered, "second chain save entered before first released lock")

	releaseFirst()
	first := waitForApprovalChainSaveResult(t, firstResult, "first chain save")
	if first.err != nil {
		t.Fatalf("first SaveApprovalChains() error = %v", first.err)
	}
	assertSingleApprovalChain(t, first.response, "42", alpha.ExternalID)
	waitForApprovalConfigurationSignal(t, secondEntered, "second chain save to enter after first commit")
	second := waitForApprovalChainSaveResult(t, secondResult, "second chain save")
	if second.err != nil {
		t.Fatalf("second SaveApprovalChains() error = %v", second.err)
	}
	assertSingleApprovalChain(t, second.response, "43", beta.ExternalID)

	final, err := svc.ListApprovalChains(ctx)
	if err != nil {
		t.Fatalf("ListApprovalChains() error = %v", err)
	}
	assertSingleApprovalChain(t, final, "43", beta.ExternalID)
}

func TestListApprovalChainsWaitsForLockedSaveAndReturnsCompleteRevision(t *testing.T) {
	ctx := context.Background()
	_, svc, source, provider, alpha, beta := setupApprovalChainTest(t, ctx)
	writerEntered := make(chan struct{})
	writerRelease := make(chan struct{})
	readerAttempted := make(chan struct{})
	readerEntered := make(chan struct{})
	var releaseOnce sync.Once
	releaseWriter := func() {
		releaseOnce.Do(func() { close(writerRelease) })
	}
	defer releaseWriter()

	writerCtx := approvalConfigurationLockTestContext(ctx, nil, func() {
		close(writerEntered)
		<-writerRelease
	})
	writerResult := make(chan approvalChainSaveResult, 1)
	go func() {
		response, err := svc.SaveApprovalChains(writerCtx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
			ProviderID: provider.ID,
			GroupID:    "42",
			GroupName:  "Group Alpha",
			Enabled:    true,
			Nodes: []ApprovalChainNodeInput{
				{DirectorySourceID: source.ID, DepartmentExternalID: beta.ExternalID, DepartmentDisplayPath: beta.Path},
				{DirectorySourceID: source.ID, DepartmentExternalID: alpha.ExternalID, DepartmentDisplayPath: alpha.Path},
			},
		}}})
		writerResult <- approvalChainSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, writerEntered, "chain writer to enter critical section")

	readerCtx := approvalConfigurationLockTestContext(ctx, func() {
		close(readerAttempted)
	}, func() {
		close(readerEntered)
	})
	readerResult := make(chan approvalChainSaveResult, 1)
	go func() {
		response, err := svc.ListApprovalChains(readerCtx)
		readerResult <- approvalChainSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, readerAttempted, "chain reader to attempt lock")
	assertNoApprovalConfigurationSignal(t, readerEntered, "chain reader entered before writer released lock")

	releaseWriter()
	writer := waitForApprovalChainSaveResult(t, writerResult, "chain writer")
	if writer.err != nil {
		t.Fatalf("SaveApprovalChains() error = %v", writer.err)
	}
	waitForApprovalConfigurationSignal(t, readerEntered, "chain reader to enter after writer commit")
	reader := waitForApprovalChainSaveResult(t, readerResult, "chain reader")
	if reader.err != nil {
		t.Fatalf("ListApprovalChains() error = %v", reader.err)
	}
	if len(reader.response.Items) != 1 || len(reader.response.Items[0].Nodes) != 2 {
		t.Fatalf("reader response = %#v, want one complete two-node chain", reader.response)
	}
	if got := reader.response.Items[0].Nodes; got[0].DepartmentExternalID != beta.ExternalID || got[1].DepartmentExternalID != alpha.ExternalID {
		t.Fatalf("reader nodes = %#v, want Beta then Alpha", got)
	}
}

func TestSaveApprovalChainsRevalidatesAfterSerializedApproverRemoval(t *testing.T) {
	ctx := context.Background()
	client, svc, source, provider, alpha, _ := setupApprovalChainTest(t, ctx)
	removalEntered := make(chan struct{})
	removalRelease := make(chan struct{})
	chainAttempted := make(chan struct{})
	chainEntered := make(chan struct{})
	var releaseOnce sync.Once
	releaseRemoval := func() {
		releaseOnce.Do(func() { close(removalRelease) })
	}
	defer releaseRemoval()

	removalCtx := approvalConfigurationLockTestContext(ctx, nil, func() {
		close(removalEntered)
		<-removalRelease
	})
	removalResult := make(chan approverConfigSaveResult, 1)
	go func() {
		response, err := svc.SaveApproverConfigs(removalCtx, SaveApproverConfigsInput{
			ActorUserID: 1,
			Mode:        ApproverConfigSaveModeReplaceAll,
			Items:       []ApproverConfigInput{},
		})
		removalResult <- approverConfigSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, removalEntered, "approver removal to enter critical section")

	chainCtx := approvalConfigurationLockTestContext(ctx, func() {
		close(chainAttempted)
	}, func() {
		close(chainEntered)
	})
	chainResult := make(chan approvalChainSaveResult, 1)
	go func() {
		response, err := svc.SaveApprovalChains(chainCtx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
			ProviderID: provider.ID,
			GroupID:    "42",
			GroupName:  "Group Alpha",
			Enabled:    true,
			Nodes: []ApprovalChainNodeInput{{
				DirectorySourceID:     source.ID,
				DepartmentExternalID:  alpha.ExternalID,
				DepartmentDisplayPath: alpha.Path,
			}},
		}}})
		chainResult <- approvalChainSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, chainAttempted, "chain save to attempt lock")
	assertNoApprovalConfigurationSignal(t, chainEntered, "chain save entered before approver removal released lock")

	releaseRemoval()
	removal := waitForApproverConfigSaveResult(t, removalResult, "approver removal")
	if removal.err != nil {
		t.Fatalf("SaveApproverConfigs() error = %v", removal.err)
	}
	if len(removal.response.Items) != 0 {
		t.Fatalf("approver removal response = %#v, want empty list", removal.response.Items)
	}
	waitForApprovalConfigurationSignal(t, chainEntered, "chain save to enter after approver removal commit")
	chain := waitForApprovalChainSaveResult(t, chainResult, "chain save")
	if !errors.Is(chain.err, ErrInvalidApproverConfig) {
		t.Fatalf("SaveApprovalChains() error = %v, want ErrInvalidApproverConfig", chain.err)
	}
	if got := client.QuotaResetApprovalChain.Query().CountX(ctx); got != 0 {
		t.Fatalf("approval chain count = %d, want no invalid chain", got)
	}
	if got := client.QuotaResetApproverConfig.Query().CountX(ctx); got != 0 {
		t.Fatalf("approver config count = %d, want completed removal", got)
	}
}

func TestSaveApproverConfigsRevalidatesAfterSerializedChainSave(t *testing.T) {
	ctx := context.Background()
	client, svc, source, provider, alpha, _ := setupApprovalChainTest(t, ctx)
	chainEntered := make(chan struct{})
	chainRelease := make(chan struct{})
	removalAttempted := make(chan struct{})
	removalEntered := make(chan struct{})
	var releaseOnce sync.Once
	releaseChain := func() {
		releaseOnce.Do(func() { close(chainRelease) })
	}
	defer releaseChain()

	chainCtx := approvalConfigurationLockTestContext(ctx, nil, func() {
		close(chainEntered)
		<-chainRelease
	})
	chainResult := make(chan approvalChainSaveResult, 1)
	go func() {
		response, err := svc.SaveApprovalChains(chainCtx, SaveApprovalChainsInput{ActorUserID: 9, Items: []ApprovalChainInput{{
			ProviderID: provider.ID,
			GroupID:    "42",
			GroupName:  "Group Alpha",
			Enabled:    true,
			Nodes: []ApprovalChainNodeInput{{
				DirectorySourceID:     source.ID,
				DepartmentExternalID:  alpha.ExternalID,
				DepartmentDisplayPath: alpha.Path,
			}},
		}}})
		chainResult <- approvalChainSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, chainEntered, "chain save to enter critical section")

	removalCtx := approvalConfigurationLockTestContext(ctx, func() {
		close(removalAttempted)
	}, func() {
		close(removalEntered)
	})
	removalResult := make(chan approverConfigSaveResult, 1)
	go func() {
		response, err := svc.SaveApproverConfigs(removalCtx, SaveApproverConfigsInput{
			ActorUserID: 1,
			Mode:        ApproverConfigSaveModeReplaceAll,
			Items:       []ApproverConfigInput{},
		})
		removalResult <- approverConfigSaveResult{response: response, err: err}
	}()
	waitForApprovalConfigurationSignal(t, removalAttempted, "approver removal to attempt lock")
	assertNoApprovalConfigurationSignal(t, removalEntered, "approver removal entered before chain save released lock")

	releaseChain()
	chain := waitForApprovalChainSaveResult(t, chainResult, "chain save")
	if chain.err != nil {
		t.Fatalf("SaveApprovalChains() error = %v", chain.err)
	}
	waitForApprovalConfigurationSignal(t, removalEntered, "approver removal to enter after chain save commit")
	removal := waitForApproverConfigSaveResult(t, removalResult, "approver removal")
	if !errors.Is(removal.err, ErrApproverConfigReferenced) {
		t.Fatalf("SaveApproverConfigs() error = %v, want ErrApproverConfigReferenced", removal.err)
	}
	if got := client.QuotaResetApproverConfig.Query().CountX(ctx); got != 2 {
		t.Fatalf("approver config count = %d, want unchanged configs", got)
	}
	if got := client.QuotaResetApprovalChain.Query().CountX(ctx); got != 1 {
		t.Fatalf("approval chain count = %d, want committed chain", got)
	}
}

type approvalChainSaveResult struct {
	response *ApprovalChainListResponse
	err      error
}

type approverConfigSaveResult struct {
	response *ApproverConfigListResponse
	err      error
}

func approvalConfigurationLockTestContext(ctx context.Context, beforeLock, afterLock func()) context.Context {
	return context.WithValue(ctx, approvalConfigurationLockHooksContextKey{}, approvalConfigurationLockHooks{
		beforeLock: beforeLock,
		afterLock:  afterLock,
	})
}

func waitForApprovalConfigurationSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func assertNoApprovalConfigurationSignal(t *testing.T, signal <-chan struct{}, failure string) {
	t.Helper()
	select {
	case <-signal:
		t.Fatal(failure)
	case <-time.After(150 * time.Millisecond):
	}
}

func waitForApprovalConfigurationResult(t *testing.T, result <-chan error, description string) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return nil
	}
}

func waitForApprovalChainSaveResult(t *testing.T, result <-chan approvalChainSaveResult, description string) approvalChainSaveResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return approvalChainSaveResult{}
	}
}

func waitForApproverConfigSaveResult(t *testing.T, result <-chan approverConfigSaveResult, description string) approverConfigSaveResult {
	t.Helper()
	select {
	case value := <-result:
		return value
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return approverConfigSaveResult{}
	}
}

func assertSingleApprovalChain(t *testing.T, response *ApprovalChainListResponse, groupID, departmentID string) {
	t.Helper()
	if response == nil || len(response.Items) != 1 || len(response.Items[0].Nodes) != 1 {
		t.Fatalf("approval chains = %#v, want one complete one-node chain", response)
	}
	if response.Items[0].GroupID != groupID || response.Items[0].Nodes[0].DepartmentExternalID != departmentID {
		t.Fatalf("approval chains = %#v, want group %s and department %s", response.Items, groupID, departmentID)
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
	fake := &fakeQuotaResetProvider{groups: []relay.Group{
		{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"},
		{ID: 43, Name: "Group Beta", Platform: "anthropic", SubscriptionType: "subscription"},
	}}
	svc := NewService(client, fakeProviderResolver(provider.ID, fake), nil, nil)
	return client, svc, source, provider, alpha, beta
}
