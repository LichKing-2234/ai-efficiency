package repo

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/systemsetting"
	"github.com/google/uuid"
)

const repoInventoryRevisionKey = "repo_inventory_revision_v1"

type InventoryRevisionStore struct {
	client *ent.Client
}

type InventoryRevisionInvalidator interface {
	InvalidateTx(context.Context, *ent.Tx) error
}

func NewInventoryRevisionStore(client *ent.Client) *InventoryRevisionStore {
	return &InventoryRevisionStore{client: client}
}

func (s *InventoryRevisionStore) Ensure(ctx context.Context) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("repository inventory revision store is not configured")
	}
	_, err := s.client.SystemSetting.Create().
		SetKey(repoInventoryRevisionKey).
		SetValue(uuid.NewString()).
		Save(ctx)
	if err == nil {
		return nil
	}
	if !ent.IsConstraintError(err) {
		return fmt.Errorf("initialize repository inventory revision: %w", err)
	}
	if _, currentErr := s.Current(ctx); currentErr != nil {
		return fmt.Errorf("initialize repository inventory revision from concurrent winner: %w", currentErr)
	}
	return nil
}

func (s *InventoryRevisionStore) Current(ctx context.Context) (string, error) {
	if s == nil || s.client == nil {
		return "", fmt.Errorf("repository inventory revision store is not configured")
	}
	row, err := s.client.SystemSetting.Query().
		Where(systemsetting.KeyEQ(repoInventoryRevisionKey)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return "", fmt.Errorf("repository inventory revision is not initialized")
	}
	if err != nil {
		return "", fmt.Errorf("read repository inventory revision: %w", err)
	}
	parsed, err := uuid.Parse(row.Value)
	if err != nil || parsed.String() != row.Value {
		return "", fmt.Errorf("repository inventory revision is malformed")
	}
	return row.Value, nil
}

func (s *InventoryRevisionStore) InvalidateTx(ctx context.Context, tx *ent.Tx) error {
	if tx == nil {
		return fmt.Errorf("invalidate repository inventory revision: transaction is required")
	}
	affected, err := tx.SystemSetting.Update().
		Where(systemsetting.KeyEQ(repoInventoryRevisionKey)).
		SetValue(uuid.NewString()).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("invalidate repository inventory revision: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("invalidate repository inventory revision: affected %d rows", affected)
	}
	return nil
}
