package accountquota

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/accountquotaobservation"
	"github.com/ai-efficiency/backend/ent/accountquotasweep"
)

// Repository is the durable side of a sweep: the job row the page polls and the
// last successful observation per account. It is an interface so the service can
// be exercised without a database.
type Repository interface {
	CreateSweep(ctx context.Context, sweep *Sweep) error
	StartSweep(ctx context.Context, id int, startedAt time.Time) error
	UpdateProgress(ctx context.Context, id int, processed, success, skipped, failed int) error
	FinishSweep(ctx context.Context, id int, status string, results []SweepResult, lastError string, finishedAt time.Time) error
	// AbandonStaleSweeps marks queued/running sweeps created before olderThan as
	// abandoned, because no worker finished them.
	AbandonStaleSweeps(ctx context.Context, providerID int, olderThan, finishedAt time.Time) (int, error)
	SweepByID(ctx context.Context, id int) (*Sweep, error)
	LatestSweep(ctx context.Context, providerID int) (*Sweep, error)
	UpsertObservation(ctx context.Context, providerID int, accountID int64, observation Observation) error
	Observations(ctx context.Context, providerID int) (map[int64]Observation, error)
}

// entRepository stores sweeps and observations in the platform database.
type entRepository struct {
	client *ent.Client
}

func newEntRepository(client *ent.Client) *entRepository {
	return &entRepository{client: client}
}

func (r *entRepository) CreateSweep(ctx context.Context, sweep *Sweep) error {
	row, err := r.client.AccountQuotaSweep.Create().
		SetStatus(accountquotasweep.Status(sweep.Status)).
		SetProviderID(sweep.ProviderID).
		SetPlatform(sweep.Platform).
		SetScope(accountquotasweep.Scope(sweep.Scope)).
		SetIncludeAutoReset(sweep.IncludeAutoReset).
		SetTotalCount(sweep.TotalCount).
		SetSkippedCount(sweep.SkippedCount).
		SetResults(resultsToMaps(sweep.Results)).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return errSweepInFlight()
		}
		return fmt.Errorf("create account quota sweep: %w", err)
	}
	sweep.ID = row.ID
	sweep.CreatedAt = row.CreatedAt
	return nil
}

func (r *entRepository) StartSweep(ctx context.Context, id int, startedAt time.Time) error {
	return r.client.AccountQuotaSweep.UpdateOneID(id).
		SetStatus(accountquotasweep.StatusRunning).
		SetStartedAt(startedAt).
		Exec(ctx)
}

func (r *entRepository) UpdateProgress(ctx context.Context, id int, processed, success, skipped, failed int) error {
	return r.client.AccountQuotaSweep.UpdateOneID(id).
		SetProcessedCount(processed).
		SetSuccessCount(success).
		SetSkippedCount(skipped).
		SetFailedCount(failed).
		Exec(ctx)
}

func (r *entRepository) FinishSweep(ctx context.Context, id int, status string, results []SweepResult, lastError string, finishedAt time.Time) error {
	update := r.client.AccountQuotaSweep.UpdateOneID(id).
		SetStatus(accountquotasweep.Status(status)).
		SetFinishedAt(finishedAt).
		SetResults(resultsToMaps(results))
	if lastError == "" {
		update.ClearLastError()
	} else {
		update.SetLastError(lastError)
	}
	return update.Exec(ctx)
}

func (r *entRepository) AbandonStaleSweeps(ctx context.Context, providerID int, olderThan, finishedAt time.Time) (int, error) {
	count, err := r.client.AccountQuotaSweep.Update().
		Where(
			accountquotasweep.ProviderID(providerID),
			accountquotasweep.StatusIn(accountquotasweep.StatusQueued, accountquotasweep.StatusRunning),
			accountquotasweep.CreatedAtLT(olderThan),
		).
		SetStatus(accountquotasweep.StatusAbandoned).
		SetFinishedAt(finishedAt).
		SetLastError("the sweep did not finish before the stale threshold").
		Save(ctx)
	if err != nil {
		return 0, fmt.Errorf("abandon stale account quota sweeps: %w", err)
	}
	return count, nil
}

func (r *entRepository) SweepByID(ctx context.Context, id int) (*Sweep, error) {
	row, err := r.client.AccountQuotaSweep.Get(ctx, id)
	if ent.IsNotFound(err) {
		return nil, errSweepNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("load account quota sweep %d: %w", id, err)
	}
	return sweepFromRow(row)
}

func (r *entRepository) LatestSweep(ctx context.Context, providerID int) (*Sweep, error) {
	row, err := r.client.AccountQuotaSweep.Query().
		Where(accountquotasweep.ProviderID(providerID)).
		Order(ent.Desc(accountquotasweep.FieldID)).
		First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load latest account quota sweep: %w", err)
	}
	return sweepFromRow(row)
}

func (r *entRepository) UpsertObservation(ctx context.Context, providerID int, accountID int64, observation Observation) error {
	windows := make([]map[string]any, 0, len(observation.Windows))
	for _, window := range observation.Windows {
		windows = append(windows, map[string]any{
			"used_percent":   window.UsedPercent,
			"window_seconds": window.WindowSeconds,
			"resets_at":      window.ResetsAt,
		})
	}
	update := r.client.AccountQuotaObservation.Update().
		Where(
			accountquotaobservation.ProviderID(providerID),
			accountquotaobservation.AccountID(accountID),
		).
		SetSweepID(observation.SweepID).
		SetPlanType(observation.PlanType).
		SetWindows(windows).
		SetCredits(creditsToMap(observation.Credits)).
		SetResetCreditSnapshot(resetCreditsToMap(observation.ResetCredits)).
		SetObservedAt(observation.ObservedAt)
	updated, err := update.Save(ctx)
	if err != nil {
		return fmt.Errorf("update account quota observation: %w", err)
	}
	if updated > 0 {
		return nil
	}
	create := r.client.AccountQuotaObservation.Create().
		SetProviderID(providerID).
		SetAccountID(accountID).
		SetSweepID(observation.SweepID).
		SetPlanType(observation.PlanType).
		SetWindows(windows).
		SetObservedAt(observation.ObservedAt)
	if observation.Credits != nil {
		create.SetCredits(creditsToMap(observation.Credits))
	}
	if observation.ResetCredits != nil {
		create.SetResetCreditSnapshot(resetCreditsToMap(observation.ResetCredits))
	}
	if _, err := create.Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			// A concurrent sweep wrote the same account first; the newer row is
			// equivalent for display, so a retry is unnecessary.
			return nil
		}
		return fmt.Errorf("create account quota observation: %w", err)
	}
	return nil
}

func (r *entRepository) Observations(ctx context.Context, providerID int) (map[int64]Observation, error) {
	rows, err := r.client.AccountQuotaObservation.Query().
		Where(accountquotaobservation.ProviderID(providerID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load account quota observations: %w", err)
	}
	observations := make(map[int64]Observation, len(rows))
	for _, row := range rows {
		observations[row.AccountID] = observationFromRow(row)
	}
	return observations, nil
}

func sweepFromRow(row *ent.AccountQuotaSweep) (*Sweep, error) {
	sweep := &Sweep{
		ID:               row.ID,
		Status:           row.Status.String(),
		ProviderID:       row.ProviderID,
		Platform:         row.Platform,
		Scope:            row.Scope.String(),
		IncludeAutoReset: row.IncludeAutoReset,
		TotalCount:       row.TotalCount,
		ProcessedCount:   row.ProcessedCount,
		SuccessCount:     row.SuccessCount,
		SkippedCount:     row.SkippedCount,
		FailedCount:      row.FailedCount,
		CreatedAt:        row.CreatedAt,
		StartedAt:        row.StartedAt,
		FinishedAt:       row.FinishedAt,
		Results:          make([]SweepResult, 0, len(row.Results)),
	}
	if row.LastError != nil {
		sweep.LastError = *row.LastError
	}
	for _, result := range row.Results {
		sweep.Results = append(sweep.Results, sweepResultFromMap(result))
	}
	return sweep, nil
}

func observationFromRow(row *ent.AccountQuotaObservation) Observation {
	observation := Observation{
		SweepID:      row.SweepID,
		ObservedAt:   row.ObservedAt.UTC(),
		Windows:      make([]QuotaWindow, 0, len(row.Windows)),
		Credits:      creditsFromMap(row.Credits),
		ResetCredits: resetCreditsFromMap(row.ResetCreditSnapshot),
	}
	if row.PlanType != nil {
		observation.PlanType = *row.PlanType
	}
	for _, window := range row.Windows {
		observation.Windows = append(observation.Windows, QuotaWindow{
			UsedPercent:   floatFromMap(window["used_percent"]),
			WindowSeconds: int64FromMap(window["window_seconds"]),
			ResetsAt:      stringFromMap(window["resets_at"]),
		})
	}
	return observation
}

func resultsToMaps(results []SweepResult) []map[string]any {
	maps := make([]map[string]any, 0, len(results))
	for _, result := range results {
		entry := map[string]any{
			"account_id": result.AccountID,
			"status":     result.Status,
			"error_code": result.ErrorCode,
			"message":    result.Message,
		}
		if result.Observation != nil {
			entry["observation"] = observationToMap(result.Observation)
		}
		maps = append(maps, entry)
	}
	return maps
}

func sweepResultFromMap(entry map[string]any) SweepResult {
	result := SweepResult{
		AccountID: int64FromMap(entry["account_id"]),
		Status:    stringFromAny(entry["status"]),
		ErrorCode: stringFromAny(entry["error_code"]),
		Message:   stringFromAny(entry["message"]),
	}
	if raw, ok := entry["observation"].(map[string]any); ok {
		if observation := observationFromMap(raw); observation != nil {
			result.Observation = observation
		}
	}
	return result
}

func observationToMap(observation *Observation) map[string]any {
	windows := make([]map[string]any, 0, len(observation.Windows))
	for _, window := range observation.Windows {
		windows = append(windows, map[string]any{
			"used_percent":   window.UsedPercent,
			"window_seconds": window.WindowSeconds,
			"resets_at":      window.ResetsAt,
		})
	}
	return map[string]any{
		"sweep_id":      observation.SweepID,
		"observed_at":   observation.ObservedAt.UTC().Format(time.RFC3339),
		"plan_type":     observation.PlanType,
		"windows":       windows,
		"credits":       creditsToMap(observation.Credits),
		"reset_credits": resetCreditsToMap(observation.ResetCredits),
	}
}

func observationFromMap(raw map[string]any) *Observation {
	observedAt, err := time.Parse(time.RFC3339, stringFromAny(raw["observed_at"]))
	if err != nil {
		return nil
	}
	observation := &Observation{
		SweepID:      int(int64FromMap(raw["sweep_id"])),
		ObservedAt:   observedAt.UTC(),
		PlanType:     stringFromAny(raw["plan_type"]),
		Windows:      make([]QuotaWindow, 0),
		Credits:      creditsFromMap(mapFromAny(raw["credits"])),
		ResetCredits: resetCreditsFromMap(mapFromAny(raw["reset_credits"])),
	}
	for _, window := range listOfMaps(raw["windows"]) {
		observation.Windows = append(observation.Windows, QuotaWindow{
			UsedPercent:   floatFromMap(window["used_percent"]),
			WindowSeconds: int64FromMap(window["window_seconds"]),
			ResetsAt:      stringFromMap(window["resets_at"]),
		})
	}
	return observation
}

func creditsToMap(credits *Credits) map[string]any {
	if credits == nil {
		return nil
	}
	entry := map[string]any{
		"has_credits": credits.HasCredits,
		"unlimited":   credits.Unlimited,
	}
	if credits.Balance != nil {
		entry["balance"] = *credits.Balance
	}
	return entry
}

func creditsFromMap(entry map[string]any) *Credits {
	if entry == nil {
		return nil
	}
	return &Credits{
		HasCredits: boolFromMap(entry["has_credits"]),
		Unlimited:  boolFromMap(entry["unlimited"]),
		Balance:    stringFromMap(entry["balance"]),
	}
}

func resetCreditsToMap(credits *ResetCredits) map[string]any {
	if credits == nil {
		return nil
	}
	return map[string]any{
		"available":  credits.Available,
		"expires_at": credits.ExpiresAt,
	}
}

func resetCreditsFromMap(entry map[string]any) *ResetCredits {
	if entry == nil {
		return nil
	}
	credits := &ResetCredits{Available: int(int64FromMap(entry["available"])), ExpiresAt: make([]string, 0)}
	for _, value := range listOfStrings(entry["expires_at"]) {
		credits.ExpiresAt = append(credits.ExpiresAt, value)
	}
	return credits
}

func mapFromAny(value any) map[string]any {
	entry, _ := value.(map[string]any)
	return entry
}

func listOfMaps(value any) []map[string]any {
	switch entries := value.(type) {
	case []map[string]any:
		return entries
	case []any:
		maps := make([]map[string]any, 0, len(entries))
		for _, entry := range entries {
			if mapped, ok := entry.(map[string]any); ok {
				maps = append(maps, mapped)
			}
		}
		return maps
	}
	return nil
}

func listOfStrings(value any) []string {
	switch entries := value.(type) {
	case []string:
		return entries
	case []any:
		strings := make([]string, 0, len(entries))
		for _, entry := range entries {
			if text, ok := entry.(string); ok {
				strings = append(strings, text)
			}
		}
		return strings
	}
	return nil
}

func stringFromAny(value any) string {
	text, _ := value.(string)
	return text
}

func stringFromMap(value any) *string {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	return &text
}

func boolFromMap(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func floatFromMap(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case float32:
		return float64(number)
	case int:
		return float64(number)
	case int64:
		return float64(number)
	case json.Number:
		parsed, _ := number.Float64()
		return parsed
	}
	return 0
}

func int64FromMap(value any) int64 {
	switch number := value.(type) {
	case int64:
		return number
	case int:
		return int64(number)
	case float64:
		return int64(number)
	case json.Number:
		parsed, _ := number.Int64()
		return parsed
	}
	return 0
}
