package accountquota

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

// quotaBehavior is the canned upstream answer for one account.
type quotaBehavior struct {
	status int
	quota  map[string]any
	delay  time.Duration
}

// quotaUpstream is a fake Sub2API admin API that records quota refresh calls.
type quotaUpstream struct {
	server    *httptest.Server
	mu        sync.Mutex
	calls     []int64
	behaviors map[int64]quotaBehavior
}

func newQuotaUpstream(t *testing.T) *quotaUpstream {
	t.Helper()
	upstream := &quotaUpstream{
		behaviors: make(map[int64]quotaBehavior),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/admin/openai/accounts/", func(writer http.ResponseWriter, request *http.Request) {
		rest := strings.TrimPrefix(request.URL.Path, "/api/v1/admin/openai/accounts/")
		rawID := strings.SplitN(rest, "/", 2)[0]
		accountID, err := strconv.ParseInt(rawID, 10, 64)
		if err != nil {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		upstream.mu.Lock()
		upstream.calls = append(upstream.calls, accountID)
		behavior := upstream.behaviors[accountID]
		upstream.mu.Unlock()
		if behavior.delay > 0 {
			select {
			case <-time.After(behavior.delay):
			case <-request.Context().Done():
				return
			}
		}
		if behavior.status != 0 && behavior.status != http.StatusOK {
			writer.WriteHeader(behavior.status)
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"code":    behavior.status,
				"message": "upstream rejected the refresh",
			})
			return
		}
		quota := behavior.quota
		if quota == nil {
			quota = defaultQuotaPayload()
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"code": 0, "data": quota})
	})
	upstream.server = httptest.NewServer(mux)
	t.Cleanup(upstream.server.Close)
	return upstream
}

func (u *quotaUpstream) setBehavior(accountID int64, behavior quotaBehavior) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.behaviors[accountID] = behavior
}

func (u *quotaUpstream) callIDs() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.calls...)
}

func defaultQuotaPayload() map[string]any {
	return map[string]any{
		"plan_type": "plus",
		"rate_limit": map[string]any{
			"primary_window": map[string]any{
				"used_percent":         42.5,
				"limit_window_seconds": 18000,
				"reset_at":             1762000000,
			},
			"secondary_window": map[string]any{
				"used_percent":         18.0,
				"limit_window_seconds": 604800,
				"reset_at":             1762500000,
			},
		},
		"rate_limit_reset_credits": map[string]any{
			"available_count": 2,
			"credits": []map[string]any{
				{"expires_at": "2026-11-20T00:00:00Z"},
			},
		},
		"credits": map[string]any{
			"has_credits": true,
			"unlimited":   false,
			"balance":     "12.34",
		},
		"fetched_at": 1762000000,
	}
}

// memoryStore is the in-process repository used by the tests.
type memoryStore struct {
	mu           sync.Mutex
	nextID       int
	sweeps       map[int]*Sweep
	observations map[int]map[int64]Observation
	createErr    error
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		sweeps:       make(map[int]*Sweep),
		observations: make(map[int]map[int64]Observation),
	}
}

func (m *memoryStore) CreateSweep(_ context.Context, sweep *Sweep) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.createErr != nil {
		return m.createErr
	}
	m.nextID++
	sweep.ID = m.nextID
	stored := *sweep
	m.sweeps[sweep.ID] = &stored
	return nil
}

func (m *memoryStore) StartSweep(_ context.Context, id int, startedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sweep, ok := m.sweeps[id]
	if !ok {
		return errors.New("sweep not found")
	}
	sweep.Status = SweepRunning
	sweep.StartedAt = &startedAt
	return nil
}

func (m *memoryStore) UpdateProgress(_ context.Context, id int, processed, success, skipped, failed int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sweep, ok := m.sweeps[id]
	if !ok {
		return errors.New("sweep not found")
	}
	sweep.ProcessedCount = processed
	sweep.SuccessCount = success
	sweep.SkippedCount = skipped
	sweep.FailedCount = failed
	return nil
}

func (m *memoryStore) FinishSweep(_ context.Context, id int, status string, results []SweepResult, lastError string, finishedAt time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	sweep, ok := m.sweeps[id]
	if !ok {
		return errors.New("sweep not found")
	}
	sweep.Status = status
	sweep.Results = append([]SweepResult(nil), results...)
	sweep.LastError = lastError
	sweep.FinishedAt = &finishedAt
	return nil
}

func (m *memoryStore) AbandonStaleSweeps(_ context.Context, providerID int, olderThan, finishedAt time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	abandoned := 0
	for _, sweep := range m.sweeps {
		if sweep.ProviderID != providerID {
			continue
		}
		if sweep.Status != SweepQueued && sweep.Status != SweepRunning {
			continue
		}
		if !sweep.CreatedAt.Before(olderThan) {
			continue
		}
		sweep.Status = SweepAbandoned
		sweep.FinishedAt = &finishedAt
		abandoned++
	}
	return abandoned, nil
}

func (m *memoryStore) SweepByID(_ context.Context, id int) (*Sweep, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sweep, ok := m.sweeps[id]
	if !ok {
		return nil, errSweepNotFound()
	}
	copied := *sweep
	return &copied, nil
}

func (m *memoryStore) LatestSweep(_ context.Context, providerID int) (*Sweep, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var latest *Sweep
	for _, sweep := range m.sweeps {
		if sweep.ProviderID != providerID {
			continue
		}
		if latest == nil || sweep.ID > latest.ID {
			copied := *sweep
			latest = &copied
		}
	}
	return latest, nil
}

func (m *memoryStore) UpsertObservation(_ context.Context, providerID int, accountID int64, observation Observation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.observations[providerID] == nil {
		m.observations[providerID] = make(map[int64]Observation)
	}
	m.observations[providerID][accountID] = observation
	return nil
}

func (m *memoryStore) Observations(_ context.Context, providerID int) (map[int64]Observation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	observations := make(map[int64]Observation, len(m.observations[providerID]))
	for accountID, observation := range m.observations[providerID] {
		observations[accountID] = observation
	}
	return observations, nil
}

func (m *memoryStore) sweep(t *testing.T, id int) *Sweep {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	sweep, ok := m.sweeps[id]
	if !ok {
		t.Fatalf("sweep %d not found", id)
	}
	copied := *sweep
	return &copied
}

// fakeRuntime serves the owner account directory and one relay provider.
type fakeRuntime struct {
	accounts    []relay.OwnerAccount
	provider    relay.Provider
	ownerErr    error
	invalidated []string
}

func (r *fakeRuntime) OwnerAccounts(_ context.Context, providerID int, configurationVersion int64, platform string) ([]relay.OwnerAccount, error) {
	if r.ownerErr != nil {
		return nil, r.ownerErr
	}
	return append([]relay.OwnerAccount(nil), r.accounts...), nil
}

func (r *fakeRuntime) InvalidateOwnerAccounts(providerID int, platform string) {
	r.invalidated = append(r.invalidated, platform)
}

func (r *fakeRuntime) Resolve(_ context.Context, providerID int) (relay.Provider, error) {
	return r.provider, nil
}

func (r *fakeRuntime) ResolveLongRunningEntity(row *ent.RelayProvider) (relay.Provider, error) {
	return r.provider, nil
}

// fakeProviderReader returns one fixed provider row.
type fakeProviderReader struct {
	row *ent.RelayProvider
	err error
}

func (r fakeProviderReader) Get(_ context.Context, id int) (*ent.RelayProvider, error) {
	if r.err != nil {
		return nil, r.err
	}
	return r.row, nil
}

// testEnv assembles a service with the fakes and the real Sub2API relay adapter,
// so the worker path (including upstream error classification) is exercised.
type testEnv struct {
	service  *Service
	store    *memoryStore
	runtime  *fakeRuntime
	upstream *quotaUpstream
}

func newTestEnv(t *testing.T, accounts []relay.OwnerAccount, options Options) *testEnv {
	t.Helper()
	upstream := newQuotaUpstream(t)
	provider := relay.NewSub2apiProvider(upstream.server.Client(), upstream.server.URL, "test-admin-key", "gpt-5.4", zap.NewNop())
	runtime := &fakeRuntime{accounts: accounts, provider: provider}
	store := newMemoryStore()
	if options.JitterMax == 0 {
		options.JitterMax = time.Nanosecond
	}
	service := newService(runtime, fakeProviderReader{row: &ent.RelayProvider{ID: 1, ConfigurationVersion: 1}}, store, zap.NewNop(), withDefaults(options))
	return &testEnv{service: service, store: store, runtime: runtime, upstream: upstream}
}

func (e *testEnv) waitForTerminal(t *testing.T, sweepID int) *Sweep {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		sweep := e.store.sweep(t, sweepID)
		switch sweep.Status {
		case SweepCompleted, SweepFailed, SweepAbandoned:
			return sweep
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("sweep %d did not reach a terminal status", sweepID)
	return nil
}

func oauthAccount(id int64, name string) relay.OwnerAccount {
	return relay.OwnerAccount{ID: id, Name: name, Email: name + "@example.com", Platform: PlatformOpenAI, Type: "oauth", Status: "active", Schedulable: true}
}

func TestStartSweepRefreshesEligibleAccountsAndStoresObservations(t *testing.T) {
	autoReset := oauthAccount(3, "carol")
	autoReset.AutoResetCreditEnabled = true
	shadow := oauthAccount(5, "eve")
	shadow.Shadow = true
	apiKey := oauthAccount(4, "dave")
	apiKey.Type = "apikey"
	env := newTestEnv(t, []relay.OwnerAccount{
		oauthAccount(1, "alice"),
		oauthAccount(2, "bob"),
		autoReset,
		apiKey,
		shadow,
	}, Options{})

	sweep, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI})
	if err != nil {
		t.Fatalf("start sweep: %v", err)
	}
	finished := env.waitForTerminal(t, sweep.ID)

	if got, want := len(env.upstream.callIDs()), 2; got != want {
		t.Fatalf("upstream refresh calls = %d, want %d (api %v)", got, want, env.upstream.callIDs())
	}
	if finished.Status != SweepCompleted {
		t.Fatalf("status = %s, want completed (last error %q)", finished.Status, finished.LastError)
	}
	if finished.TotalCount != 3 || finished.SkippedCount != 1 || finished.SuccessCount != 2 || finished.ProcessedCount != 3 {
		t.Fatalf("unexpected counters: total=%d skipped=%d success=%d processed=%d", finished.TotalCount, finished.SkippedCount, finished.SuccessCount, finished.ProcessedCount)
	}
	if len(finished.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(finished.Results))
	}
	skipped := 0
	for _, result := range finished.Results {
		if result.Status == ResultSkipped {
			skipped++
			if result.AccountID != 3 {
				t.Fatalf("skipped account = %d, want 3", result.AccountID)
			}
		}
	}
	if skipped != 1 {
		t.Fatalf("skipped results = %d, want 1", skipped)
	}

	observations, err := env.store.Observations(context.Background(), 1)
	if err != nil {
		t.Fatalf("load observations: %v", err)
	}
	if len(observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(observations))
	}
	observation := observations[1]
	if observation.SweepID != sweep.ID {
		t.Fatalf("observation sweep = %d, want %d", observation.SweepID, sweep.ID)
	}
	if observation.PlanType != "plus" {
		t.Fatalf("plan type = %q, want plus", observation.PlanType)
	}
	if len(observation.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(observation.Windows))
	}
	if observation.Windows[0].WindowSeconds != 18000 || observation.Windows[1].WindowSeconds != 604800 {
		t.Fatalf("window seconds = %d/%d, want 18000/604800", observation.Windows[0].WindowSeconds, observation.Windows[1].WindowSeconds)
	}
	if observation.Credits == nil || observation.Credits.Balance == nil || *observation.Credits.Balance != "12.34" {
		t.Fatalf("credits = %#v", observation.Credits)
	}
	if observation.ResetCredits == nil || observation.ResetCredits.Available != 1 || len(observation.ResetCredits.ExpiresAt) != 1 {
		t.Fatalf("reset credits = %#v", observation.ResetCredits)
	}
	if len(env.runtime.invalidated) != 1 {
		t.Fatalf("directory invalidations = %d, want 1", len(env.runtime.invalidated))
	}
}

func TestStartSweepIncludeAutoResetOptIn(t *testing.T) {
	autoReset := oauthAccount(2, "bob")
	autoReset.AutoResetCreditEnabled = true
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice"), autoReset}, Options{})

	sweep, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI, IncludeAutoReset: true})
	if err != nil {
		t.Fatalf("start sweep: %v", err)
	}
	finished := env.waitForTerminal(t, sweep.ID)
	if finished.SkippedCount != 0 || finished.SuccessCount != 2 {
		t.Fatalf("unexpected counters: skipped=%d success=%d", finished.SkippedCount, finished.SuccessCount)
	}
	if got, want := len(env.upstream.callIDs()), 2; got != want {
		t.Fatalf("upstream refresh calls = %d, want %d", got, want)
	}
}

func TestStartSweepSelectedScopeOnlyRefreshesSelection(t *testing.T) {
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice"), oauthAccount(2, "bob")}, Options{})

	sweep, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI, AccountIDs: []int64{2}})
	if err != nil {
		t.Fatalf("start sweep: %v", err)
	}
	finished := env.waitForTerminal(t, sweep.ID)
	if finished.Scope != ScopeSelected {
		t.Fatalf("scope = %q, want selected", finished.Scope)
	}
	if got, want := env.upstream.callIDs(), []int64{2}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("upstream refresh calls = %v, want %v", got, want)
	}
}

func TestStartSweepRejectsIneligibleSelection(t *testing.T) {
	apiKey := oauthAccount(2, "bob")
	apiKey.Type = "apikey"
	autoReset := oauthAccount(3, "carol")
	autoReset.AutoResetCreditEnabled = true
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice"), apiKey, autoReset}, Options{})

	for _, accountID := range []int64{2, 3} {
		_, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI, AccountIDs: []int64{accountID}})
		var classified *Error
		if !errors.As(err, &classified) || classified.Code != CodeTargetNotEligible {
			t.Fatalf("account %d: error = %v, want target-not-eligible", accountID, err)
		}
	}
	if len(env.upstream.callIDs()) != 0 {
		t.Fatalf("no upstream calls expected before eligibility passes")
	}
}

func TestStartSweepRejectsConcurrentSweep(t *testing.T) {
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice")}, Options{})
	row := &Sweep{ID: 7, Status: SweepRunning, ProviderID: 1, Platform: PlatformOpenAI, Scope: ScopeAll, CreatedAt: time.Now().UTC()}
	env.store.sweeps[7] = row

	_, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI})
	var classified *Error
	if !errors.As(err, &classified) || classified.Code != CodeSweepInFlight {
		t.Fatalf("error = %v, want sweep-in-flight", err)
	}
}

func TestStartSweepAllScopeHonorsMinimumInterval(t *testing.T) {
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice")}, Options{MinSweepInterval: 2 * time.Minute})
	env.store.sweeps[7] = &Sweep{
		ID: 7, Status: SweepCompleted, ProviderID: 1, Platform: PlatformOpenAI, Scope: ScopeAll,
		CreatedAt: time.Now().UTC().Add(-30 * time.Second),
	}

	_, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI})
	var classified *Error
	if !errors.As(err, &classified) || classified.Code != CodeSweepTooSoon {
		t.Fatalf("error = %v, want sweep-too-soon", err)
	}
	// A selected refresh is explicit and small, so the interval does not apply.
	if _, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI, AccountIDs: []int64{1}}); err != nil {
		t.Fatalf("selected sweep after interval: %v", err)
	}
}

func TestSweepRecordsPerAccountFailureWithoutAborting(t *testing.T) {
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice"), oauthAccount(2, "bob")}, Options{})
	env.upstream.setBehavior(2, quotaBehavior{status: http.StatusUnauthorized})

	sweep, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI})
	if err != nil {
		t.Fatalf("start sweep: %v", err)
	}
	finished := env.waitForTerminal(t, sweep.ID)
	if finished.Status != SweepCompleted {
		t.Fatalf("status = %q, want completed", finished.Status)
	}
	if finished.SuccessCount != 1 || finished.FailedCount != 1 {
		t.Fatalf("counters: success=%d failed=%d, want 1/1", finished.SuccessCount, finished.FailedCount)
	}
	failed := false
	for _, result := range finished.Results {
		if result.Status == ResultFailed {
			failed = true
			if result.AccountID != 2 || result.ErrorCode != ReasonReauthRequired {
				t.Fatalf("failed result = %#v, want account 2 reauth-required", result)
			}
			if strings.Contains(strings.ToLower(result.Message), "status") {
				t.Fatalf("result message leaks upstream text: %q", result.Message)
			}
		}
	}
	if !failed {
		t.Fatalf("no failed result recorded")
	}
	observations, err := env.store.Observations(context.Background(), 1)
	if err != nil {
		t.Fatalf("load observations: %v", err)
	}
	if len(observations) != 1 {
		t.Fatalf("observations = %d, want only the successful account", len(observations))
	}
}

func TestSweepRecordsRateLimitedAccount(t *testing.T) {
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice")}, Options{})
	env.upstream.setBehavior(1, quotaBehavior{status: http.StatusTooManyRequests})
	sweep, err := env.service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI})
	if err != nil {
		t.Fatalf("start sweep: %v", err)
	}
	finished := env.waitForTerminal(t, sweep.ID)
	if len(finished.Results) != 1 || finished.Results[0].ErrorCode != ReasonRateLimited {
		t.Fatalf("results = %#v, want one rate-limited result", finished.Results)
	}
}

func TestSweepFailsWhenProviderCannotRefreshQuota(t *testing.T) {
	runtime := &fakeRuntime{
		accounts: []relay.OwnerAccount{oauthAccount(1, "alice")},
		provider: unsupportedProvider{},
	}
	store := newMemoryStore()
	service := newService(runtime, fakeProviderReader{row: &ent.RelayProvider{ID: 1, ConfigurationVersion: 1}}, store, zap.NewNop(), withDefaults(Options{}))
	sweep, err := service.StartSweep(context.Background(), StartSweepRequest{ProviderID: 1, Platform: PlatformOpenAI})
	if err != nil {
		t.Fatalf("start sweep: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		current := store.sweep(t, sweep.ID)
		if current.Status == SweepFailed {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("sweep did not fail")
}

// unsupportedProvider satisfies relay.Provider without the quota capability.
type unsupportedProvider struct {
	relay.Provider
}

func TestListReturnsObservationsAndSnapshotFallback(t *testing.T) {
	balance := "7.50"
	observedAt := time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)
	expiry := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)
	accounts := []relay.OwnerAccount{
		{
			ID: 1, Name: "alice", Email: "alice@example.com", Platform: PlatformOpenAI, Type: "oauth", Status: "active", Schedulable: true,
			Credits:      &relay.OwnerAccountCredits{HasCredits: true, Balance: &balance, ObservedAt: &observedAt},
			ResetCredits: &relay.OwnerAccountResetCredits{Available: 1, ExpiresAt: []time.Time{expiry}},
		},
		{
			ID: 2, Name: "bob", Email: "bob@example.com", Platform: PlatformOpenAI, Type: "oauth", Status: "error", Schedulable: false,
		},
	}
	env := newTestEnv(t, accounts, Options{})
	env.store.observations[1] = map[int64]Observation{
		1: {
			SweepID:    9,
			ObservedAt: observedAt,
			PlanType:   "plus",
			Windows:    []QuotaWindow{{UsedPercent: 12, WindowSeconds: 18000}},
			Credits:    &Credits{HasCredits: true, Balance: &balance},
			ResetCredits: &ResetCredits{
				Available: 1,
				ExpiresAt: []string{"2026-11-10T00:00:00Z"},
			},
		},
	}

	page, err := env.service.List(context.Background(), 1, PlatformOpenAI)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("total = %d items = %d, want 2", page.Total, len(page.Items))
	}
	if page.Items[0].Account.Name != "alice" {
		t.Fatalf("first account = %q, want alice", page.Items[0].Account.Name)
	}
	if page.Items[0].Observation == nil || page.Items[0].Observation.SweepID != 9 {
		t.Fatalf("observation = %#v", page.Items[0].Observation)
	}
	if page.Items[0].Snapshot == nil || page.Items[0].Snapshot.Credits == nil {
		t.Fatalf("snapshot = %#v", page.Items[0].Snapshot)
	}
	if page.Items[1].Observation != nil {
		t.Fatalf("bob should not have an observation yet")
	}
	if page.Items[1].Snapshot != nil {
		t.Fatalf("bob should have no snapshot facts: %#v", page.Items[1].Snapshot)
	}
}

func TestListRejectsUnsupportedPlatform(t *testing.T) {
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice")}, Options{})
	if _, err := env.service.List(context.Background(), 1, "anthropic"); err == nil {
		t.Fatalf("expected an error for a non-openai platform")
	}
}

func TestListMapsUpstreamCapabilityError(t *testing.T) {
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice")}, Options{})
	env.runtime.ownerErr = relay.ErrCapabilityUnsupported
	_, err := env.service.List(context.Background(), 1, PlatformOpenAI)
	var classified *Error
	if !errors.As(err, &classified) || classified.Code != CodeCapabilityUnsupported {
		t.Fatalf("error = %v, want capability-unsupported", err)
	}
}

func TestClassifyAccountFailure(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"capability", relay.ErrCapabilityUnsupported, ReasonCapabilityUnsupported},
		{"compliance", relay.ErrComplianceAcknowledgementRequired, ReasonComplianceAckRequired},
		{"outcome-unknown", relay.ErrUpstreamOutcomeUnknown, ReasonUpstreamOutcomeUnknown},
		{"deadline", context.DeadlineExceeded, ReasonTimeout},
		{"canceled", context.Canceled, ReasonTimeout},
		{"other", errors.New("boom"), ReasonUpstreamUnavailable},
	}
	for _, testCase := range cases {
		code, message := classifyAccountFailure(testCase.err)
		if code != testCase.want {
			t.Fatalf("%s: code = %q, want %q", testCase.name, code, testCase.want)
		}
		if message == "" || strings.Contains(message, "boom") {
			t.Fatalf("%s: message = %q", testCase.name, message)
		}
	}
}

func TestSettleStaleSweepsMarksAbandoned(t *testing.T) {
	env := newTestEnv(t, []relay.OwnerAccount{oauthAccount(1, "alice")}, Options{})
	stale := &Sweep{
		ID: 5, Status: SweepRunning, ProviderID: 1, Platform: PlatformOpenAI, Scope: ScopeAll,
		CreatedAt: time.Now().UTC().Add(-time.Hour),
	}
	env.store.sweeps[5] = stale
	latest, err := env.service.LatestSweep(context.Background(), 1)
	if err != nil {
		t.Fatalf("latest sweep: %v", err)
	}
	if latest == nil || latest.Status != SweepAbandoned {
		t.Fatalf("latest = %#v, want abandoned", latest)
	}
}
