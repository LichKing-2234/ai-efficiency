package workitems

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/internal/directorysync"
)

type CountsResponse struct {
	QuotaResetApprovalCount int `json:"quota_reset_approval_count"`
	QuotaResetAdminCount    int `json:"quota_reset_admin_count"`
	OffboardingCount        int `json:"offboarding_count"`
	TotalCount              int `json:"total_count"`
}

type Service struct {
	client *ent.Client
}

func NewService(client *ent.Client) *Service {
	return &Service{client: client}
}

func (s *Service) Counts(ctx context.Context, userID int, admin bool) (*CountsResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("work items service is not configured")
	}
	approvalCount, err := s.countAssignedQuotaApprovals(ctx, userID)
	if err != nil {
		return nil, err
	}
	counts := &CountsResponse{QuotaResetApprovalCount: approvalCount}
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
		counts.TotalCount = adminQuotaCount + offboardingCount
		return counts, nil
	}
	counts.TotalCount = approvalCount
	return counts, nil
}

func (s *Service) countAssignedQuotaApprovals(ctx context.Context, userID int) (int, error) {
	count, err := s.client.QuotaResetRequest.Query().
		Where(
			quotaresetrequest.StatusIn(actionableQuotaResetStatuses()...),
			quotaresetrequest.RequesterUserIDNEQ(userID),
			func(selector *sql.Selector) {
				selector.Where(jsonbContainsInt(selector, quotaresetrequest.FieldResolvedApproverUserIds, userID))
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
