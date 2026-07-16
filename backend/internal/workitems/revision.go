package workitems

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/systemsetting"
	"github.com/google/uuid"
)

const workItemCountsRevisionKey = "work_items_counts_revision_v1"

type RevisionStore struct {
	client *ent.Client
}

func NewRevisionStore(client *ent.Client) *RevisionStore {
	return &RevisionStore{client: client}
}

func (s *RevisionStore) Ensure(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("work item counts revision store is not configured")
	}
	_, err := s.client.SystemSetting.Create().
		SetKey(workItemCountsRevisionKey).
		SetValue(uuid.NewString()).
		Save(ctx)
	if err == nil {
		return nil
	}
	if !ent.IsConstraintError(err) {
		return fmt.Errorf("initialize work item counts revision: %w", err)
	}
	if _, currentErr := s.Current(ctx); currentErr != nil {
		return fmt.Errorf("initialize work item counts revision from concurrent winner: %w", currentErr)
	}
	return nil
}

func (s *RevisionStore) Current(ctx context.Context) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("work item counts revision store is not configured")
	}
	row, err := s.client.SystemSetting.Query().
		Where(systemsetting.KeyEQ(workItemCountsRevisionKey)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return "", fmt.Errorf("work item counts revision is not initialized")
	}
	if err != nil {
		return "", fmt.Errorf("read work item counts revision: %w", err)
	}
	parsed, err := uuid.Parse(row.Value)
	if err != nil || parsed.String() != row.Value {
		return "", fmt.Errorf("work item counts revision is malformed")
	}
	return row.Value, nil
}

func (s *RevisionStore) InvalidateWorkItemCountsTx(ctx context.Context, tx *ent.Tx) error {
	if tx == nil {
		return fmt.Errorf("invalidate work item counts revision: transaction is required")
	}
	affected, err := tx.SystemSetting.Update().
		Where(systemsetting.KeyEQ(workItemCountsRevisionKey)).
		SetValue(uuid.NewString()).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("invalidate work item counts revision: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("invalidate work item counts revision: affected %d rows", affected)
	}
	return nil
}
