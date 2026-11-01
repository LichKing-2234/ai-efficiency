package accountquota

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// accountOutcome is the terminal result of one target inside a sweep.
type accountOutcome struct {
	accountID   int64
	status      string
	errorCode   string
	message     string
	observation *Observation
}

// runSweep is the detached worker of one accepted sweep. It performs exactly one
// quota refresh per target through the long-running relay client, records every
// outcome and never aborts because one account failed.
func (s *Service) runSweep(ctx context.Context, sweep *Sweep, targets []relay.OwnerAccount, skippedCount int) {
	startedAt := s.now()
	if err := s.store.StartSweep(ctx, sweep.ID, startedAt); err != nil {
		s.logError("start account quota sweep", err)
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			s.logger.Error("account quota sweep worker panicked", zap.Int("sweep_id", sweep.ID))
			s.failSweep(sweep, "the sweep worker failed unexpectedly")
		}
	}()

	provider, err := s.providers.Get(ctx, sweep.ProviderID)
	if err != nil {
		s.logError("load relay provider for sweep", err)
		s.failSweep(sweep, "the relay provider could not be loaded")
		return
	}
	resolved, err := s.runtime.ResolveLongRunningEntity(provider)
	if err != nil {
		s.logError("resolve relay provider for sweep", err)
		s.failSweep(sweep, "the relay provider could not be resolved")
		return
	}
	manager, ok := resolved.(relay.OpenAIQuotaManager)
	if !ok {
		s.failSweep(sweep, "the relay provider does not support OpenAI quota refresh")
		return
	}

	sweepCtx, cancel := context.WithTimeout(ctx, s.options.SweepTimeout)
	defer cancel()

	// Skipped accounts were decided before the worker started; they count as
	// processed from the first progress write so the progress bar can reach total.
	processed, success, failed := skippedCount, 0, 0
	if err := s.store.UpdateProgress(ctx, sweep.ID, processed, success, skippedCount, failed); err != nil {
		s.logError("record account quota sweep progress", err)
	}

	outcomes := make(chan accountOutcome, len(targets))
	slots := make(chan struct{}, s.options.Concurrency)
	var workers sync.WaitGroup
	for _, target := range targets {
		workers.Add(1)
		go func(accountID int64) {
			defer workers.Done()
			select {
			case slots <- struct{}{}:
			case <-sweepCtx.Done():
				outcomes <- accountOutcome{
					accountID: accountID,
					status:    ResultFailed,
					errorCode: ReasonTimeout,
					message:   "the sweep timed out before this account was refreshed",
				}
				return
			}
			defer func() { <-slots }()
			s.jitterDelay(sweepCtx)
			outcomes <- s.refreshTarget(sweepCtx, manager, sweep.ID, accountID)
		}(target.ID)
	}
	go func() {
		workers.Wait()
		close(outcomes)
	}()

	results := append([]SweepResult(nil), sweep.Results...)
	for outcome := range outcomes {
		results = append(results, resultFromOutcome(outcome))
		switch outcome.status {
		case ResultSucceeded:
			success++
			if outcome.observation != nil {
				observation := *outcome.observation
				if err := s.store.UpsertObservation(ctx, sweep.ProviderID, outcome.accountID, observation); err != nil {
					s.logError("store account quota observation", err)
				}
			}
		default:
			failed++
		}
		processed++
		if err := s.store.UpdateProgress(ctx, sweep.ID, processed, success, skippedCount, failed); err != nil {
			s.logError("record account quota sweep progress", err)
		}
	}

	finishedAt := s.now()
	if err := s.store.FinishSweep(ctx, sweep.ID, SweepCompleted, results, "", finishedAt); err != nil {
		s.logError("finish account quota sweep", err)
	}
	// The directory snapshot the tab reads is now behind the sweep observations.
	s.runtime.InvalidateOwnerAccounts(sweep.ProviderID, sweep.Platform)
	s.logger.Info("account quota sweep finished",
		zap.Int("sweep_id", sweep.ID),
		zap.Int("provider_id", sweep.ProviderID),
		zap.Int("total", sweep.TotalCount),
		zap.Int("success", success),
		zap.Int("failed", failed),
		zap.Int("skipped", skippedCount),
		zap.Duration("duration", finishedAt.Sub(startedAt)),
	)
}

// refreshTarget performs one upstream quota refresh. The per-account deadline is
// shorter than the sweep deadline so one slow account cannot consume the sweep.
func (s *Service) refreshTarget(ctx context.Context, manager relay.OpenAIQuotaManager, sweepID int, accountID int64) accountOutcome {
	callCtx, cancel := context.WithTimeout(ctx, s.options.AccountTimeout)
	defer cancel()
	quota, err := manager.RefreshOpenAIQuota(callCtx, accountID)
	if err != nil {
		errorCode, message := classifyAccountFailure(err)
		return accountOutcome{accountID: accountID, status: ResultFailed, errorCode: errorCode, message: message}
	}
	return accountOutcome{
		accountID:   accountID,
		status:      ResultSucceeded,
		observation: observationFromQuota(sweepID, quota, s.now()),
	}
}

func (s *Service) failSweep(sweep *Sweep, message string) {
	ctx, cancel := s.terminalContext()
	defer cancel()
	finishedAt := s.now()
	if err := s.store.FinishSweep(ctx, sweep.ID, SweepFailed, sweep.Results, message, finishedAt); err != nil {
		s.logError("mark account quota sweep failed", err)
	}
}

func (s *Service) jitterDelay(ctx context.Context) {
	if s.options.JitterMax <= 0 {
		return
	}
	delay := time.Duration(rand.Int64N(int64(s.options.JitterMax)))
	if s.options.Sleep != nil {
		_ = s.options.Sleep(ctx, delay)
		return
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func observationFromQuota(sweepID int, quota *relay.OpenAIQuota, now time.Time) *Observation {
	observedAt := now
	if quota.ObservedAt != nil {
		observedAt = quota.ObservedAt.UTC()
	}
	observation := &Observation{
		SweepID:    sweepID,
		ObservedAt: observedAt,
		PlanType:   quota.PlanType,
		Windows:    make([]QuotaWindow, 0, 2),
	}
	for _, window := range []*relay.OpenAIQuotaWindow{quota.PrimaryWindow, quota.SecondaryWindow} {
		if window == nil {
			continue
		}
		var resetsAt *string
		if window.ResetsAt != nil {
			formatted := window.ResetsAt.UTC().Format(time.RFC3339)
			resetsAt = &formatted
		}
		observation.Windows = append(observation.Windows, QuotaWindow{
			UsedPercent:   window.Utilization,
			WindowSeconds: window.WindowSeconds,
			ResetsAt:      resetsAt,
		})
	}
	if quota.Credits != nil {
		observation.Credits = &Credits{
			HasCredits: quota.Credits.HasCredits,
			Unlimited:  quota.Credits.Unlimited,
			Balance:    quota.Credits.Balance,
		}
	}
	if quota.ResetCredits != nil {
		observation.ResetCredits = &ResetCredits{
			Available: quota.ResetCredits.Available,
			ExpiresAt: formatExpiries(quota.ResetCredits.ExpiresAt),
		}
	}
	return observation
}

func formatExpiries(expiries []time.Time) []string {
	formatted := make([]string, 0, len(expiries))
	for _, expiry := range expiries {
		formatted = append(formatted, expiry.UTC().Format(time.RFC3339))
	}
	return formatted
}

func resultFromOutcome(outcome accountOutcome) SweepResult {
	return SweepResult{
		AccountID:   outcome.accountID,
		Status:      outcome.status,
		ErrorCode:   outcome.errorCode,
		Message:     outcome.message,
		Observation: outcome.observation,
	}
}
