package repo

import (
	"context"
	"fmt"

	"github.com/ai-efficiency/backend/ent"
)

func (s *Service) mutateInventory(ctx context.Context, operation string, mutate func(*ent.Tx) error) error {
	if s == nil || s.entClient == nil {
		return fmt.Errorf("%s: repository service is not configured", operation)
	}
	if mutate == nil {
		return fmt.Errorf("%s: mutation is required", operation)
	}
	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("%s: begin transaction: %w", operation, err)
	}
	defer tx.Rollback()

	if err := mutate(tx); err != nil {
		return err
	}
	if s.inventoryRevision != nil {
		if err := s.inventoryRevision.InvalidateTx(ctx, tx); err != nil {
			return fmt.Errorf("%s: %w", operation, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: commit transaction: %w", operation, err)
	}
	return nil
}
