package owneraccount

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"
	"go.uber.org/zap"
)

// Statements in this file deliberately use database time (`now()`), because
// freshness, cooldowns, daily caps and stale-heartbeat sweeps must not depend on
// replica clocks.

// databaseNow returns the database clock. It is the reference time for every
// freshness, cooldown, capacity and heartbeat decision.
func (s *Service) databaseNow(ctx context.Context) (time.Time, error) {
	var now time.Time
	if err := s.db.QueryRowContext(ctx, `SELECT now()`).Scan(&now); err != nil {
		return time.Time{}, fmt.Errorf("read database time: %w", err)
	}
	return now.UTC(), nil
}

// accountOperationFacts is the per-account local history a read or a gate needs:
// when each mutation was last attempted, when a successful observation of each
// upstream fact was recorded, the state of the most recent operation of each
// action, and the refresh cooldown deadline.
type accountOperationFacts struct {
	LastResetAt        *time.Time
	LastInviteAt       *time.Time
	QuotaObservedAt    *time.Time
	ReferralObservedAt *time.Time
	Latest             map[string]latestOperation
}

type latestOperation struct {
	ID        int
	Status    string
	CreatedAt time.Time
}

// operationFacts loads the local history of the given accounts in two bounded
// queries: one aggregate of attempt and successful-observation times, and one
// latest-row-per-action projection used for unknown blocking and cooldowns.
func (s *Service) operationFacts(ctx context.Context, providerID int, accountIDs []int64) (map[int64]*accountOperationFacts, error) {
	facts := make(map[int64]*accountOperationFacts, len(accountIDs))
	if len(accountIDs) == 0 {
		return facts, nil
	}
	for _, accountID := range accountIDs {
		facts[accountID] = &accountOperationFacts{Latest: map[string]latestOperation{}}
	}

	aggregateRows, err := s.db.QueryContext(ctx, `
		SELECT account_id,
			max(created_at) FILTER (WHERE action = 'reset_credit'),
			max(created_at) FILTER (WHERE action = 'send_invitation'),
			max(created_at) FILTER (WHERE action = 'refresh_quota' AND status = 'succeeded'),
			max(created_at) FILTER (WHERE action = 'refresh_referral' AND status = 'succeeded'),
			max(created_at) FILTER (WHERE action = 'reset_credit' AND status = 'succeeded'),
			max(created_at) FILTER (WHERE action = 'send_invitation' AND status = 'succeeded')
		FROM owner_account_operations
		WHERE provider_id = $1 AND account_id = ANY($2)
		GROUP BY account_id`, providerID, pq.Array(accountIDs))
	if err != nil {
		return nil, fmt.Errorf("load owner operation facts: %w", err)
	}
	defer aggregateRows.Close()
	for aggregateRows.Next() {
		var accountID int64
		var lastReset, lastInvite, quotaRefresh, referralRefresh, resetSuccess, inviteSuccess sql.NullTime
		if err := aggregateRows.Scan(&accountID, &lastReset, &lastInvite, &quotaRefresh, &referralRefresh, &resetSuccess, &inviteSuccess); err != nil {
			return nil, fmt.Errorf("scan owner operation facts: %w", err)
		}
		entry := facts[accountID]
		if entry == nil {
			entry = &accountOperationFacts{Latest: map[string]latestOperation{}}
			facts[accountID] = entry
		}
		entry.LastResetAt = nullTimePtr(lastReset)
		entry.LastInviteAt = nullTimePtr(lastInvite)
		entry.QuotaObservedAt = laterTime(nullTimePtr(quotaRefresh), nullTimePtr(resetSuccess))
		entry.ReferralObservedAt = laterTime(nullTimePtr(referralRefresh), nullTimePtr(inviteSuccess))
	}
	if err := aggregateRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate owner operation facts: %w", err)
	}

	latestRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (account_id, action) account_id, action, status, id, created_at
		FROM owner_account_operations
		WHERE provider_id = $1 AND account_id = ANY($2)
		ORDER BY account_id, action, created_at DESC, id DESC`, providerID, pq.Array(accountIDs))
	if err != nil {
		return nil, fmt.Errorf("load latest owner operations: %w", err)
	}
	defer latestRows.Close()
	for latestRows.Next() {
		var accountID int64
		var action, status string
		var id int
		var createdAt time.Time
		if err := latestRows.Scan(&accountID, &action, &status, &id, &createdAt); err != nil {
			return nil, fmt.Errorf("scan latest owner operation: %w", err)
		}
		entry := facts[accountID]
		if entry == nil {
			entry = &accountOperationFacts{Latest: map[string]latestOperation{}}
			facts[accountID] = entry
		}
		entry.Latest[action] = latestOperation{ID: id, Status: status, CreatedAt: createdAt.UTC()}
	}
	if err := latestRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate latest owner operations: %w", err)
	}
	return facts, nil
}

// sweepStaleAccounts marks running rows of the given accounts unknown when their
// heartbeat is older than the stale window. It runs before reads and before a
// mutation so a dead worker cannot block an account forever.
func (s *Service) sweepStaleAccounts(ctx context.Context, providerID int, accountIDs []int64) (int64, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE owner_account_operations
		SET status = 'unknown', upstream_code = 'worker-stale',
			finished_at = now(), updated_at = now()
		WHERE provider_id = $1 AND account_id = ANY($2) AND status = 'running'
			AND (heartbeat_at IS NULL OR heartbeat_at < now() - $3::interval)`,
		providerID, pq.Array(accountIDs), intervalLiteral(s.options.StaleHeartbeat))
	if err != nil {
		return 0, fmt.Errorf("sweep stale owner operations: %w", err)
	}
	return result.RowsAffected()
}

// SweepStaleOperations performs the startup sweep: every running row whose
// heartbeat is older than the stale window is marked unknown. A running row on
// another replica keeps a fresh heartbeat and is untouched.
func (s *Service) SweepStaleOperations(ctx context.Context) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE owner_account_operations
		SET status = 'unknown', upstream_code = 'worker-stale',
			finished_at = now(), updated_at = now()
		WHERE status = 'running'
			AND (heartbeat_at IS NULL OR heartbeat_at < now() - $1::interval)`,
		intervalLiteral(s.options.StaleHeartbeat))
	if err != nil {
		return 0, fmt.Errorf("sweep stale owner operations: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count swept owner operations: %w", err)
	}
	if affected > 0 {
		s.logger.Warn("owner account operations marked unknown at startup", zap.Int64("operations", affected))
	}
	return affected, nil
}

// mutationAttempt is one mutation about to start.
type mutationAttempt struct {
	ProviderID  int
	AccountID   int64
	ActorUserID int
	Action      string
	Reason      string
	InviteEmail string
}

// insertRunningOperation commits the running row that claims the per-account
// single-flight slot, using the database as the authority. A stale running row is
// swept in the same transaction, and a live one returns its id as a conflict.
func (s *Service) insertRunningOperation(ctx context.Context, attempt mutationAttempt) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin owner operation: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		UPDATE owner_account_operations
		SET status = 'unknown', upstream_code = 'worker-stale',
			finished_at = now(), updated_at = now()
		WHERE provider_id = $1 AND account_id = $2 AND status = 'running'
			AND (heartbeat_at IS NULL OR heartbeat_at < now() - $3::interval)`,
		attempt.ProviderID, attempt.AccountID, intervalLiteral(s.options.StaleHeartbeat)); err != nil {
		return 0, fmt.Errorf("sweep stale owner operation: %w", err)
	}

	var existingID int
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM owner_account_operations
		WHERE provider_id = $1 AND account_id = $2 AND status = 'running'
		ORDER BY created_at DESC, id DESC LIMIT 1`,
		attempt.ProviderID, attempt.AccountID).Scan(&existingID)
	if err == nil {
		return 0, errMutationInFlight(existingID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, fmt.Errorf("load running owner operation: %w", err)
	}

	var operationID int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO owner_account_operations
			(provider_id, account_id, actor_user_id, action, status, reason, invite_email,
			 heartbeat_at, started_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'running', NULLIF($5, ''), NULLIF($6, ''), now(), now(), now(), now())
		RETURNING id`,
		attempt.ProviderID, attempt.AccountID, attempt.ActorUserID, attempt.Action,
		attempt.Reason, attempt.InviteEmail).Scan(&operationID)
	if err != nil {
		if isUniqueViolation(err) {
			// Another replica won the race between our check and insert. The
			// transaction is aborted, so read the winner on a fresh statement.
			_ = tx.Rollback()
			var winnerID int
			if scanErr := s.db.QueryRowContext(ctx, `
				SELECT id FROM owner_account_operations
				WHERE provider_id = $1 AND account_id = $2 AND status = 'running'
				ORDER BY created_at DESC, id DESC LIMIT 1`,
				attempt.ProviderID, attempt.AccountID).Scan(&winnerID); scanErr == nil {
				return 0, errMutationInFlight(winnerID)
			}
			return 0, errBusy()
		}
		return 0, fmt.Errorf("insert owner operation: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit owner operation: %w", err)
	}
	return operationID, nil
}

// insertTerminalOperation records a terminal-only refresh operation. Refreshes
// never own a running slot.
func (s *Service) insertTerminalOperation(ctx context.Context, attempt mutationAttempt, status, upstreamCode string) (int, error) {
	if attempt.Action != "refresh_quota" && attempt.Action != "refresh_referral" {
		return 0, fmt.Errorf("insert terminal owner operation: unsupported action %q", attempt.Action)
	}
	var operationID int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO owner_account_operations
			(provider_id, account_id, actor_user_id, action, status, reason,
			 upstream_code, started_at, finished_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), now(), now(), now(), now())
		RETURNING id`,
		attempt.ProviderID, attempt.AccountID, attempt.ActorUserID, attempt.Action,
		status, attempt.Reason, upstreamCode).Scan(&operationID)
	if err != nil {
		return 0, fmt.Errorf("insert terminal owner operation: %w", err)
	}
	return operationID, nil
}

// touchHeartbeat renews the running row's heartbeat using database time.
func (s *Service) touchHeartbeat(ctx context.Context, operationID int) error {
	if _, err := s.db.ExecContext(ctx, `
		UPDATE owner_account_operations
		SET heartbeat_at = now(), updated_at = now()
		WHERE id = $1 AND status = 'running'`, operationID); err != nil {
		return fmt.Errorf("renew owner operation heartbeat: %w", err)
	}
	return nil
}

// finishOperation writes a terminal state under an independent short context. It
// never repeats the upstream call; only this write is retried a bounded number of
// times.
func (s *Service) finishOperation(operationID int, status, upstreamCode, warningCode string, result map[string]any) error {
	var payload any
	if result != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode owner operation result: %w", err)
		}
		payload = string(encoded)
	}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), s.options.TerminalWriteTimeout)
		_, err := s.db.ExecContext(ctx, `
			UPDATE owner_account_operations
			SET status = $2, finished_at = now(), updated_at = now(),
				upstream_code = NULLIF($3, ''), warning_code = NULLIF($4, ''),
				result = $5::jsonb
			WHERE id = $1`,
			operationID, status, upstreamCode, warningCode, payload)
		cancel()
		if err == nil {
			return nil
		}
		lastErr = err
		time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
	}
	return fmt.Errorf("write owner operation terminal state: %w", lastErr)
}

// countRecentMutations returns the rolling 24-hour attempt count and the oldest
// counted attempt, which determines the retry hint.
func (s *Service) countRecentMutations(ctx context.Context, providerID int, accountID int64, action string) (int, *time.Time, error) {
	var count int
	var oldest sql.NullTime
	if err := s.db.QueryRowContext(ctx, `
		SELECT count(*), min(created_at) FROM owner_account_operations
		WHERE provider_id = $1 AND account_id = $2 AND action = $3
			AND created_at > now() - interval '24 hours'`,
		providerID, accountID, action).Scan(&count, &oldest); err != nil {
		return 0, nil, fmt.Errorf("count owner mutations: %w", err)
	}
	return count, nullTimePtr(oldest), nil
}

func intervalLiteral(duration time.Duration) string {
	return fmt.Sprintf("%d milliseconds", duration.Milliseconds())
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	return false
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	utc := value.Time.UTC()
	return &utc
}

func laterTime(left, right *time.Time) *time.Time {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	if right.After(*left) {
		return right
	}
	return left
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
