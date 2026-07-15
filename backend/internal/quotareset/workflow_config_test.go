package quotareset

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestResolveWorkflowUsesExactConfigThenRepresentativeFallbackAndOrderedChain(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	root := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-root", "Company", nil)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-alpha", "Group Alpha", &root.ExternalID)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-beta", "Group Beta", &root.ExternalID)
	gamma := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-gamma", "Group Gamma", &root.ExternalID)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	configured := createQuotaResetUser(t, ctx, client, "configured", "configured@example.org", nil, "user")
	representative := createQuotaResetUser(t, ctx, client, "representative", "representative@example.org", nil, "user")
	betaApprover := createQuotaResetUser(t, ctx, client, "beta-approver", "beta@example.org", nil, "user")
	rootApprover := createQuotaResetUser(t, ctx, client, "root-approver", "root@example.org", nil, "user")

	requesterMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, alpha.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, requesterMember, alpha.ExternalID)
	configuredMember := createQuotaResetMember(t, ctx, client, source.ID, "member-configured", configured.Email, alpha.ExternalID, &configured.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, configuredMember, alpha.ExternalID)
	representativeMember := createQuotaResetMember(t, ctx, client, source.ID, "member-representative", representative.Email, alpha.ExternalID, &representative.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, representativeMember, alpha.ExternalID)
	betaMember := createQuotaResetMember(t, ctx, client, source.ID, "member-beta", betaApprover.Email, beta.ExternalID, &betaApprover.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, betaMember, beta.ExternalID)
	rootMember := createQuotaResetMember(t, ctx, client, source.ID, "member-root", rootApprover.Email, root.ExternalID, &rootApprover.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, rootMember, root.ExternalID)
	client.DirectoryDepartment.UpdateOneID(alpha.ID).
		SetMetadata(map[string]any{"representative_external_ids": []any{representativeMember.ExternalID}}).
		SaveX(ctx)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, alpha.ExternalID, "Company / Group Alpha", configured.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, beta.ExternalID, "Company / Group Beta", betaApprover.ID)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, root.ExternalID, "Company", rootApprover.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)
	client.QuotaResetApprovalChain.Create().
		SetProviderID(provider.ID).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetDepartmentChain([]map[string]any{
			{"directory_source_id": source.ID, "department_external_id": beta.ExternalID, "department_display_path": "Company / Group Beta"},
			{"directory_source_id": source.ID, "department_external_id": gamma.ExternalID, "department_display_path": "Company / Group Gamma"},
		}).
		SaveX(ctx)

	svc := NewService(client, nil, NewApproverResolver(client), nil)
	workflow, paths, err := svc.resolveWorkflow(ctx, requester, provider.ID, "42")
	if err != nil {
		t.Fatalf("resolveWorkflow() error = %v", err)
	}
	if len(paths) != 1 || paths[0].StartDepartmentExternalID != alpha.ExternalID || paths[0].MatchedDepartmentExternalID != alpha.ExternalID {
		t.Fatalf("paths = %#v, want exact alpha evidence", paths)
	}
	if len(workflow.Steps) != 3 {
		t.Fatalf("steps = %#v, want initial plus two configured steps", workflow.Steps)
	}
	if got := workflowApproverIDs(workflow.Steps[0]); !reflect.DeepEqual(got, []int{configured.ID}) {
		t.Fatalf("initial approvers = %v, want exact config only", got)
	}
	if got := workflowApproverIDs(workflow.Steps[1]); !reflect.DeepEqual(got, []int{betaApprover.ID}) {
		t.Fatalf("beta approvers = %v", got)
	}
	if !workflow.Steps[2].AdminFallback || len(workflow.Steps[2].Approvers) != 0 {
		t.Fatalf("gamma step = %+v, want admin fallback", workflow.Steps[2])
	}
	for _, step := range workflow.Steps {
		for _, approver := range step.Approvers {
			if approver.UserID == representative.ID || approver.UserID == rootApprover.ID {
				t.Fatalf("unexpected fallback/parent approver in configured exact path: %+v", approver)
			}
		}
	}
}

func TestResolveWorkflowMergesDirectDepartmentsAndFallsBackPerDepartment(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-alpha", "Group Alpha", nil)
	beta := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-beta", "Group Beta", nil)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	alphaApprover := createQuotaResetUser(t, ctx, client, "alpha-approver", "alpha@example.org", nil, "user")
	betaRepresentative := createQuotaResetUser(t, ctx, client, "beta-representative", "beta@example.org", nil, "user")
	member := createQuotaResetMember(t, ctx, client, source.ID, "member-alice", requester.Email, alpha.ExternalID, &requester.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, alpha.ExternalID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, member, beta.ExternalID)
	alphaMember := createQuotaResetMember(t, ctx, client, source.ID, "member-alpha", alphaApprover.Email, alpha.ExternalID, &alphaApprover.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, alphaMember, alpha.ExternalID)
	betaMember := createQuotaResetMember(t, ctx, client, source.ID, "member-beta", betaRepresentative.Email, beta.ExternalID, &betaRepresentative.ID)
	createQuotaResetMemberDepartment(t, ctx, client, source.ID, betaMember, beta.ExternalID)
	client.DirectoryDepartment.UpdateOneID(beta.ID).
		SetMetadata(map[string]any{"representative_external_ids": []any{betaMember.ExternalID}}).
		SaveX(ctx)
	createQuotaResetApproverConfig(t, ctx, client, source.ID, alpha.ExternalID, alpha.Name, alphaApprover.ID)
	provider := createQuotaResetRelayProvider(t, ctx, client)

	workflow, _, err := NewService(client, nil, NewApproverResolver(client), nil).resolveWorkflow(ctx, requester, provider.ID, "42")
	if err != nil {
		t.Fatalf("resolveWorkflow() error = %v", err)
	}
	if len(workflow.Steps) != 1 || workflow.Steps[0].Kind != WorkflowStepRequesterDepartments {
		t.Fatalf("steps = %#v, want one merged initial step", workflow.Steps)
	}
	if got, want := workflowApproverIDs(workflow.Steps[0]), []int{alphaApprover.ID, betaRepresentative.ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("initial approvers = %v, want %v", got, want)
	}
}

func TestResolveWorkflowCreatesAdminFallbackWhenNoStepCanResolve(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	requester := createQuotaResetUser(t, ctx, client, "alice", "alice@example.com", intPtr(1001), "user")
	provider := createQuotaResetRelayProvider(t, ctx, client)

	workflow, _, err := NewService(client, nil, NewApproverResolver(client), nil).resolveWorkflow(ctx, requester, provider.ID, "42")
	if err != nil {
		t.Fatalf("resolveWorkflow() error = %v", err)
	}
	if len(workflow.Steps) != 1 || !workflow.Steps[0].AdminFallback || workflow.Steps[0].Status != WorkflowStepActive {
		t.Fatalf("steps = %#v, want one active admin fallback", workflow.Steps)
	}
}

func TestApprovalChainsReplaceAndValidateCurrentFacts(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	source := createQuotaResetDirectorySource(t, ctx, client)
	alpha := createQuotaResetDepartment(t, ctx, client, source.ID, "dept-alpha", "Group Alpha", nil)
	providerRow := createQuotaResetRelayProvider(t, ctx, client)
	provider := &fakeApprovalChainProvider{fakeQuotaResetProvider: fakeQuotaResetProvider{}, groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}}}
	svc := NewService(client, fakeProviderResolver(providerRow.ID, provider), nil, nil)
	input := ApprovalChainInput{
		ProviderID: providerRow.ID,
		GroupID:    "42",
		GroupName:  "Group Alpha",
		Enabled:    true,
		Departments: []ChainDepartmentInput{{
			DirectorySourceID:     source.ID,
			DepartmentExternalID:  alpha.ExternalID,
			DepartmentDisplayPath: "Company / Group Alpha",
		}},
	}

	response, err := svc.SaveApprovalChains(ctx, 7, []ApprovalChainInput{input})
	if err != nil {
		t.Fatalf("SaveApprovalChains() error = %v", err)
	}
	if len(response.Items) != 1 || len(response.Items[0].Departments) != 1 || response.Items[0].Departments[0].DepartmentExternalID != alpha.ExternalID {
		t.Fatalf("saved chains = %#v", response.Items)
	}
	if len(response.Groups) != 1 || response.Groups[0].GroupID != "42" {
		t.Fatalf("group options = %#v", response.Groups)
	}

	duplicate := input
	duplicate.Departments = append(duplicate.Departments, duplicate.Departments[0])
	if _, err := svc.SaveApprovalChains(ctx, 7, []ApprovalChainInput{duplicate}); !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("duplicate department error = %v, want ErrInvalidApproverConfig", err)
	}
	unknownGroup := input
	unknownGroup.GroupID = "99"
	if _, err := svc.SaveApprovalChains(ctx, 7, []ApprovalChainInput{unknownGroup}); !errors.Is(err, ErrInvalidApproverConfig) {
		t.Fatalf("unknown group error = %v, want ErrInvalidApproverConfig", err)
	}
}

func TestApprovalChainOptionsRequireExplicitSubscriptionType(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	providerRow := createQuotaResetRelayProvider(t, ctx, client)
	provider := &fakeApprovalChainProvider{groups: []relay.Group{
		{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"},
		{ID: 43, Name: "Unknown Group", Platform: "openai"},
	}}

	options, err := NewService(client, fakeProviderResolver(providerRow.ID, provider), nil, nil).approvalChainGroupOptions(ctx)
	if err != nil {
		t.Fatalf("approvalChainGroupOptions() error = %v", err)
	}
	if len(options) != 1 || options[0].GroupID != "42" {
		t.Fatalf("options = %#v, want explicit subscription group only", options)
	}
}

type fakeApprovalChainProvider struct {
	fakeQuotaResetProvider
	groups []relay.Group
}

func (f *fakeApprovalChainProvider) ListPlatformGroups(context.Context) ([]relay.Group, error) {
	return append([]relay.Group(nil), f.groups...), nil
}

func workflowApproverIDs(step WorkflowStep) []int {
	ids := make([]int, 0, len(step.Approvers))
	for _, approver := range step.Approvers {
		ids = append(ids, approver.UserID)
	}
	sort.Ints(ids)
	return ids
}
