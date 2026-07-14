package workitems

import (
	"context"
	"fmt"

	"entgo.io/ent/dialect/sql"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
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

type offboardingCounter interface {
	CountOffboardingCandidates(ctx context.Context, sourceID int) (int, error)
}

type Service struct {
	client             *ent.Client
	providerLister     providerLister
	offboardingCounter offboardingCounter
	countsCache        *CountsCache
}

func NewService(client *ent.Client, offboardingCounter offboardingCounter, providerListers ...providerLister) *Service {
	var lister providerLister
	if len(providerListers) > 0 {
		lister = providerListers[0]
	}
	return &Service{client: client, providerLister: lister, offboardingCounter: offboardingCounter}
}

func (s *Service) WithCountsCache(cache *CountsCache) *Service {
	if s != nil {
		s.countsCache = cache
	}
	return s
}

func (s *Service) Counts(ctx context.Context, userID int, admin bool) (*CountsResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("work items service is not configured")
	}
	if s.countsCache == nil {
		result, err := s.loadCounts(ctx, userID, admin)
		return result.Counts, err
	}
	role := "user"
	if admin {
		role = "admin"
	}
	return s.countsCache.GetOrLoad(ctx, userID, role, func(loadCtx context.Context) (CountsLoadResult, error) {
		return s.loadCounts(loadCtx, userID, admin)
	})
}

func (s *Service) loadCounts(ctx context.Context, userID int, admin bool) (CountsLoadResult, error) {
	approvalCount, err := s.countAssignedQuotaApprovals(ctx, userID)
	if err != nil {
		return CountsLoadResult{}, err
	}
	// AI access is remote-derived; an unavailable relay must not hide local approval queues.
	aiAccessSetupCount := 0
	cacheable := true
	if count, accessErr := s.countAIAccessSetup(ctx, userID); accessErr == nil {
		aiAccessSetupCount = count
	} else {
		cacheable = false
	}
	counts := &CountsResponse{
		QuotaResetApprovalCount: approvalCount,
		AIAccessSetupCount:      aiAccessSetupCount,
	}
	if admin {
		adminQuotaCount, err := s.countAdminQuotaApprovals(ctx)
		if err != nil {
			return CountsLoadResult{}, err
		}
		offboardingCount, err := s.countOffboardingCandidates(ctx)
		if err != nil {
			return CountsLoadResult{}, err
		}
		counts.QuotaResetAdminCount = adminQuotaCount
		counts.OffboardingCount = offboardingCount
		counts.TotalCount = aiAccessSetupCount + adminQuotaCount + offboardingCount
		return CountsLoadResult{Counts: counts, Cacheable: cacheable}, nil
	}
	counts.TotalCount = aiAccessSetupCount + approvalCount
	return CountsLoadResult{Counts: counts, Cacheable: cacheable}, nil
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
	if s.offboardingCounter == nil {
		return 0, nil
	}
	count, err := s.offboardingCounter.CountOffboardingCandidates(ctx, 0)
	if err != nil {
		return 0, fmt.Errorf("count directory offboarding candidates: %w", err)
	}
	return count, nil
}
