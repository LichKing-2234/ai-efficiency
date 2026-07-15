package quotareset

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
)

type WorkItemCounts struct {
	Assigned int
	Admin    int
}

func CountWorkItems(ctx context.Context, client *ent.Client, userID int, admin bool) (WorkItemCounts, error) {
	assigned, err := client.QuotaResetRequest.Query().
		Where(
			quotaresetrequest.RequesterUserIDNEQ(userID),
			quotaresetrequest.Or(
				quotaresetrequest.And(
					quotaresetrequest.WorkflowVersionLT(WorkflowVersionV2),
					quotaresetrequest.StatusIn(actionableQuotaResetStatuses()...),
					legacyApproverJSONPredicate(userID),
				),
				quotaresetrequest.And(
					quotaresetrequest.WorkflowVersionGTE(WorkflowVersionV2),
					quotaresetrequest.StatusEQ(quotaresetrequest.StatusPending),
					v2ActiveApproverPredicate(userID),
				),
				quotaresetrequest.And(
					quotaresetrequest.WorkflowVersionGTE(WorkflowVersionV2),
					quotaresetrequest.StatusEQ(quotaresetrequest.StatusApprovedResetFailed),
					v2CompletionActorPredicate(userID),
				),
			),
		).
		Count(ctx)
	if err != nil {
		return WorkItemCounts{}, fmt.Errorf("count assigned quota reset work items: %w", err)
	}
	counts := WorkItemCounts{Assigned: assigned}
	if !admin {
		return counts, nil
	}
	counts.Admin, err = client.QuotaResetRequest.Query().
		Where(quotaresetrequest.Or(
			quotaresetrequest.And(
				quotaresetrequest.WorkflowVersionLT(WorkflowVersionV2),
				quotaresetrequest.StatusIn(actionableQuotaResetStatuses()...),
			),
			quotaresetrequest.And(
				quotaresetrequest.WorkflowVersionGTE(WorkflowVersionV2),
				quotaresetrequest.StatusEQ(quotaresetrequest.StatusPending),
				quotaresetrequest.RequesterUserIDNEQ(userID),
			),
			quotaresetrequest.And(
				quotaresetrequest.WorkflowVersionGTE(WorkflowVersionV2),
				quotaresetrequest.StatusEQ(quotaresetrequest.StatusApprovedResetFailed),
			),
		)).
		Count(ctx)
	if err != nil {
		return WorkItemCounts{}, fmt.Errorf("count admin quota reset work items: %w", err)
	}
	return counts, nil
}

func actionableQuotaResetStatuses() []quotaresetrequest.Status {
	return []quotaresetrequest.Status{
		quotaresetrequest.StatusPending,
		quotaresetrequest.StatusApprovedResetFailed,
	}
}
