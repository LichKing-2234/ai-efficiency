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
// when each mutation was last attempted, the persisted upstream observation a
// mutation gate may use, the state of the most recent operation of each action,
// and the refresh cooldown deadline.
//
// Mutation gates deliberately read only these local rows. The directory cache is
// display state; a stale cached snapshot must never admit or reject a spend.
type accountOperationFacts struct {
	LastResetAt         *time.Time
	LastInviteAt        *time.Time
	QuotaObservation    *quotaObservationFacts
	ReferralObservation *referralObservationFacts
	Latest              map[string]latestOperation
}

// quotaObservationFacts is the sanitized quota observation persisted on a
// succeeded refresh_quota or reset_credit row. ObservedAt is database time.
type quotaObservationFacts struct {
	ObservedAt            time.Time
	ResetCreditsRemaining int
	ResetCreditExpiries   []time.Time
}

// referralObservationFacts is the sanitized eligibility observation persisted on
// a succeeded refresh_referral or send_invitation row.
type referralObservationFacts struct {
	ObservedAt       time.Time
	Eligible         bool
	ProgramID        string
	RemainingInvites int
}

func (f *accountOperationFacts) quotaObservedAt() *time.Time {
	if f == nil || f.QuotaObservation == nil {
		return nil
	}
	observed := f.QuotaObservation.ObservedAt
	return &observed
}

func (f *accountOperationFacts) referralObservedAt() *time.Time {
	if f == nil || f.ReferralObservation == nil {
		return nil
	}
	observed := f.ReferralObservation.ObservedAt
	return &observed
}

type latestOperation struct {
	ID        int
	Status    string
	CreatedAt time.Time
}

// persistedQuotaObservation and persistedReferralObservation are the sanitized
// result payloads written by the refresh and mutation operations. Pointer fields
// distinguish "observation carried this fact" from a decoded zero value.
type persistedQuotaObservation struct {
	ResetCreditsRemaining *int     `json:"reset_credits_remaining"`
	ResetCreditExpiries   []string `json:"reset_credit_expiries"`
}

type persistedReferralObservation struct {
	Eligible         *bool   `json:"eligible"`
	ProgramID        *string `json:"program_id"`
	RemainingInvites *int    `json:"remaining_invites"`
}

// operationFacts loads the local history of the given accounts in three bounded
// queries: one aggregate of attempt times, one latest-succeeded-observation
// projection per fact family, and one latest-row-per-action projection used for
// unknown blocking and cooldowns.
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
			max(created_at) FILTER (WHERE action = 'send_invitation')
		FROM owner_account_operations
		WHERE provider_id = $1 AND account_id = ANY($2)
		GROUP BY account_id`, providerID, pq.Array(accountIDs))
	if err != nil {
		return nil, fmt.Errorf("load owner operation facts: %w", err)
	}
	defer aggregateRows.Close()
	for aggregateRows.Next() {
		var accountID int64
		var lastReset, lastInvite sql.NullTime
		if err := aggregateRows.Scan(&accountID, &lastReset, &lastInvite); err != nil {
			return nil, fmt.Errorf("scan owner operation facts: %w", err)
		}
		entry := facts[accountID]
		if entry == nil {
			entry = &accountOperationFacts{Latest: map[string]latestOperation{}}
			facts[accountID] = entry
		}
		entry.LastResetAt = nullTimePtr(lastReset)
		entry.LastInviteAt = nullTimePtr(lastInvite)
	}
	if err := aggregateRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate owner operation facts: %w", err)
	}

	// The latest succeeded row per fact family carries the observation a gate may
	// use. finished_at is the write time of the observation; a reset's post-reset
	// quota therefore postdates its own attempt.
	observationRows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT ON (account_id, family) account_id, family, result,
			coalesce(finished_at, created_at) AS observed_at
		FROM (
			SELECT *, CASE
				WHEN action IN ('refresh_quota', 'reset_credit') THEN 'quota'
				ELSE 'referral'
			END AS family
			FROM owner_account_operations
			WHERE provider_id = $1 AND account_id = ANY($2) AND status = 'succeeded'
				AND action IN ('refresh_quota', 'reset_credit', 'refresh_referral', 'send_invitation')
		) observations
		ORDER BY account_id, family, created_at DESC, id DESC`, providerID, pq.Array(accountIDs))
	if err != nil {
		return nil, fmt.Errorf("load owner operation observations: %w", err)
	}
	defer observationRows.Close()
	for observationRows.Next() {
		var accountID int64
		var family string
		var raw []byte
		var observedAt time.Time
		if err := observationRows.Scan(&accountID, &family, &raw, &observedAt); err != nil {
			return nil, fmt.Errorf("scan owner operation observation: %w", err)
		}
		entry := facts[accountID]
		if entry == nil {
			entry = &accountOperationFacts{Latest: map[string]latestOperation{}}
			facts[accountID] = entry
		}
		observedAt = observedAt.UTC()
		switch family {
		case "quota":
			entry.QuotaObservation = decodeQuotaObservation(raw, observedAt)
		case "referral":
			entry.ReferralObservation = decodeReferralObservation(raw, observedAt)
		}
	}
	if err := observationRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate owner operation observations: %w", err)
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

// sweepScope narrows the stale-heartbeat sweep. An empty ProviderID sweeps every
// running row; otherwise the sweep is limited to the given accounts of that
// provider.
type sweepScope struct {
	ProviderID *int
	AccountIDs []int64
}

// sqlExecer is satisfied by both *sql.DB and *sql.Tx, so the single sweep
// statement can also run inside the single-flight transaction.
type sqlExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// sweepStaleRunning marks running rows unknown when their heartbeat is older than
// the stale window. It is the one implementation behind the account-scoped read
// sweep, the startup sweep and the mutation single-flight recovery.
func (s *Service) sweepStaleRunning(ctx context.Context, exec sqlExecer, scope sweepScope) (int64, error) {
	query := `
		UPDATE owner_account_operations
		SET status = 'unknown', upstream_code = 'worker-stale',
			finished_at = now(), updated_at = now()
		WHERE status = 'running'
			AND (heartbeat_at IS NULL OR heartbeat_at < now() - $1::interval)`
	args := []any{intervalLiteral(s.options.StaleHeartbeat)}
	if scope.ProviderID != nil {
		query += ` AND provider_id = $2 AND account_id = ANY($3)`
		args = append(args, *scope.ProviderID, pq.Array(scope.AccountIDs))
	}
	result, err := exec.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("sweep stale owner operations: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count swept owner operations: %w", err)
	}
	return affected, nil
}

// sweepStaleAccounts runs the account-scoped sweep before reads and mutations so a
// dead worker cannot block an account forever.
func (s *Service) sweepStaleAccounts(ctx context.Context, providerID int, accountIDs []int64) (int64, error) {
	if len(accountIDs) == 0 {
		return 0, nil
	}
	return s.sweepStaleRunning(ctx, s.db, sweepScope{ProviderID: &providerID, AccountIDs: accountIDs})
}

// SweepStaleOperations performs the startup sweep: every running row whose
// heartbeat is older than the stale window is marked unknown. A running row on
// another replica keeps a fresh heartbeat and is untouched.
func (s *Service) SweepStaleOperations(ctx context.Context) (int64, error) {
	affected, err := s.sweepStaleRunning(ctx, s.db, sweepScope{})
	if err != nil {
		return 0, err
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

	if _, err := s.sweepStaleRunning(ctx, tx, sweepScope{
		ProviderID: &attempt.ProviderID, AccountIDs: []int64{attempt.AccountID},
	}); err != nil {
		return 0, err
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

// insertTerminalOperation records a terminal-only refresh operation, including the
// sanitized upstream observation the matching mutation gate later reads. Refreshes
// never own a running slot.
func (s *Service) insertTerminalOperation(ctx context.Context, attempt mutationAttempt, status, upstreamCode string, result map[string]any) (int, error) {
	if attempt.Action != "refresh_quota" && attempt.Action != "refresh_referral" {
		return 0, fmt.Errorf("insert terminal owner operation: unsupported action %q", attempt.Action)
	}
	var payload any
	if result != nil {
		encoded, err := json.Marshal(result)
		if err != nil {
			return 0, fmt.Errorf("encode terminal owner operation result: %w", err)
		}
		payload = string(encoded)
	}
	var operationID int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO owner_account_operations
			(provider_id, account_id, actor_user_id, action, status, reason,
			 upstream_code, result, started_at, finished_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8::jsonb, now(), now(), now(), now())
		RETURNING id`,
		attempt.ProviderID, attempt.AccountID, attempt.ActorUserID, attempt.Action,
		status, attempt.Reason, upstreamCode, payload).Scan(&operationID)
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

// decodeQuotaObservation reads the sanitized quota result persisted on the latest
// succeeded refresh or reset row. A row without the persisted credit fields is not
// an observation, so a gate can never fall back to a cached snapshot.
func decodeQuotaObservation(raw []byte, observedAt time.Time) *quotaObservationFacts {
	if len(raw) == 0 {
		return nil
	}
	var payload persistedQuotaObservation
	if err := json.Unmarshal(raw, &payload); err != nil || payload.ResetCreditsRemaining == nil {
		return nil
	}
	expiries := make([]time.Time, 0, len(payload.ResetCreditExpiries))
	for _, value := range payload.ResetCreditExpiries {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			continue
		}
		expiries = append(expiries, parsed.UTC())
	}
	return &quotaObservationFacts{
		ObservedAt:            observedAt,
		ResetCreditsRemaining: *payload.ResetCreditsRemaining,
		ResetCreditExpiries:   expiries,
	}
}

// decodeReferralObservation reads the sanitized eligibility result persisted on
// the latest succeeded referral refresh or invitation row.
func decodeReferralObservation(raw []byte, observedAt time.Time) *referralObservationFacts {
	if len(raw) == 0 {
		return nil
	}
	var payload persistedReferralObservation
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil
	}
	if payload.Eligible == nil || payload.ProgramID == nil || payload.RemainingInvites == nil {
		return nil
	}
	return &referralObservationFacts{
		ObservedAt:       observedAt,
		Eligible:         *payload.Eligible,
		ProgramID:        *payload.ProgramID,
		RemainingInvites: *payload.RemainingInvites,
	}
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
