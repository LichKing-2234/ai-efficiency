package workitems

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/usersetup"
)

type CountsResponse struct {
	QuotaResetApprovalCount int `json:"quota_reset_approval_count"`
	QuotaResetAdminCount    int `json:"quota_reset_admin_count"`
	AIAccessSetupCount      int `json:"ai_access_setup_count"`
	OffboardingCount        int `json:"offboarding_count"`
	TotalCount              int `json:"total_count"`
}

type providerLister interface {
	ListProviders(ctx context.Context, req usersetup.ListProvidersRequest) (*usersetup.ListProvidersResponse, error)
}

type Service struct {
	client         *ent.Client
	providerLister providerLister
}

func NewService(client *ent.Client, providerListers ...providerLister) *Service {
	var lister providerLister
	if len(providerListers) > 0 {
		lister = providerListers[0]
	}
	return &Service{client: client, providerLister: lister}
}

func (s *Service) Counts(ctx context.Context, userID int, admin bool) (*CountsResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("work items service is not configured")
	}
	approvalCount, err := s.countAssignedQuotaApprovals(ctx, userID)
	if err != nil {
		return nil, err
	}
	// AI access is remote-derived; an unavailable relay must not hide local approval queues.
	aiAccessSetupCount := 0
	if count, accessErr := s.countAIAccessSetup(ctx, userID); accessErr == nil {
		aiAccessSetupCount = count
	}
	counts := &CountsResponse{
		QuotaResetApprovalCount: approvalCount,
		AIAccessSetupCount:      aiAccessSetupCount,
	}
	if admin {
		adminQuotaCount, err := s.countAdminQuotaApprovals(ctx)
		if err != nil {
			return nil, err
		}
		offboardingCount, err := s.countOffboardingCandidates(ctx)
		if err != nil {
			return nil, err
		}
		counts.QuotaResetAdminCount = adminQuotaCount
		counts.OffboardingCount = offboardingCount
		counts.TotalCount = aiAccessSetupCount + adminQuotaCount + offboardingCount
		return counts, nil
	}
	counts.TotalCount = aiAccessSetupCount + approvalCount
	return counts, nil
}

func (s *Service) countAIAccessSetup(ctx context.Context, userID int) (int, error) {
	if s.providerLister == nil {
		return 0, nil
	}
	resp, err := s.providerLister.ListProviders(ctx, usersetup.ListProvidersRequest{UserID: userID})
	if err != nil {
		return 0, fmt.Errorf("count ai access setup work item: %w", err)
	}
	if resp == nil {
		return 1, nil
	}
	for _, provider := range resp.Providers {
		for _, group := range provider.Groups {
			if group.Credential.State == "existing_hidden" {
				return 0, nil
			}
		}
	}
	return 1, nil
}

func (s *Service) countAssignedQuotaApprovals(ctx context.Context, userID int) (int, error) {
	count, err := s.client.QuotaResetRequest.Query().
		Where(
			quotaresetrequest.StatusIn(actionableQuotaResetStatuses()...),
			quotaresetrequest.RequesterUserIDNEQ(userID),
			func(selector *sql.Selector) {
				resolved := jsonbContainsInt(selector, quotaresetrequest.FieldResolvedApproverUserIds, userID)
				selector.Where(sql.Or(
					sql.And(
						sql.In(selector.C(quotaresetrequest.FieldStatus), quotaresetrequest.StatusPending, quotaresetrequest.StatusWorkflowPending),
						resolved,
					),
					sql.And(
						sql.EQ(selector.C(quotaresetrequest.FieldStatus), quotaresetrequest.StatusApprovedResetFailed),
						sql.Or(
							sql.And(sql.EQ(selector.C(quotaresetrequest.FieldWorkflowVersion), 1), resolved),
							sql.And(sql.EQ(selector.C(quotaresetrequest.FieldWorkflowVersion), 2), sql.EQ(selector.C(quotaresetrequest.FieldApprovedByUserID), userID)),
						),
					),
				))
			},
		).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count assigned quota reset approvals: %w", err)
	}
	return count, nil
}

func jsonbContainsInt(selector *sql.Selector, field string, value int) *sql.Predicate {
	return sql.P(func(builder *sql.Builder) {
		builder.WriteString(selector.C(field)).
			WriteString("::jsonb @> ").
			Arg(fmt.Sprintf("[%d]", value))
	})
}

func (s *Service) countAdminQuotaApprovals(ctx context.Context) (int, error) {
	count, err := s.client.QuotaResetRequest.Query().
		Where(quotaresetrequest.StatusIn(actionableQuotaResetStatuses()...)).
		Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("count admin quota reset approvals: %w", err)
	}
	return count, nil
}

func actionableQuotaResetStatuses() []quotaresetrequest.Status {
	return []quotaresetrequest.Status{
		quotaresetrequest.StatusPending,
		quotaresetrequest.StatusWorkflowPending,
		quotaresetrequest.StatusApprovedResetFailed,
	}
}

func (s *Service) countOffboardingCandidates(ctx context.Context) (int, error) {
	candidates, err := directorysync.NewService(s.client, directorysync.ServiceOptions{}).ListOffboardingCandidates(ctx, 0, "")
	if err != nil {
		return 0, fmt.Errorf("count directory offboarding candidates: %w", err)
	}
	return len(candidates), nil
}
