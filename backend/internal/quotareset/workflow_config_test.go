package quotareset

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

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
