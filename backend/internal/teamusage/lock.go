package teamusage

import (
	"context"
	"database/sql"
	"fmt"
)

type AdvisoryLocker interface {
	WithProviderGroupLock(ctx context.Context, providerID int, groupID int64, fn func(context.Context) error) error
}

type PostgresAdvisoryLocker struct {
	db *sql.DB
}

func NewPostgresAdvisoryLocker(db *sql.DB) *PostgresAdvisoryLocker {
	return &PostgresAdvisoryLocker{db: db}
}

func (l *PostgresAdvisoryLocker) WithProviderGroupLock(ctx context.Context, providerID int, groupID int64, fn func(context.Context) error) error {
	if l == nil || l.db == nil {
		return fn(ctx)
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin advisory lock transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	lockKey := fmt.Sprintf("teamusage:%d:%d", providerID, groupID)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		return fmt.Errorf("acquire provider/group advisory lock: %w", err)
	}
	if err := fn(ctx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit advisory lock transaction: %w", err)
	}
	committed = true
	return nil
}
