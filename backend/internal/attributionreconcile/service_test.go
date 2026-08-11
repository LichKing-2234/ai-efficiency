package attributionreconcile

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/internal/attributionpool"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

type resolverFunc func(context.Context, int) (relay.Provider, error)

func (f resolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type requestReaderProvider struct {
	relay.Provider
	read func(context.Context, string, int) ([]relay.RequestUsage, error)
}

func (p *requestReaderProvider) ReadRequestUsage(ctx context.Context, requestID string, limit int) ([]relay.RequestUsage, error) {
	return p.read(ctx, requestID, limit)
}

type reconcileFixture struct {
	client     *ent.Client
	providerID int
	claimID    int
	now        time.Time
}

func newReconcileFixture(t *testing.T) reconcileFixture {
	t.Helper()
	ctx := context.Background()
	client := testdb.Open(t)
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("relay-alpha").SetDisplayName("Relay Alpha").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SaveX(ctx)
	repo := client.RepoConfig.Create().SetName("repo").SetFullName("acme/repo").SetCloneURL("https://github.com/acme/repo.git").SaveX(ctx)
	group := client.AttributionClaimGroup.Create().SetGroupID("group-1").SetInstallationID(1).SetUserID(user.ID).SetRelayProviderID(provider.ID).
		SetSchemaVersion(2).SetThreadID("thread-1").SetTurnID("turn-1").SetEvidenceDigest("evidence").SetCommitAllocations([]map[string]any{{"repo_config_id": repo.ID, "commit_sha": "commit-1"}}).
		SetRequestCount(1).SetExpiresAt(now.Add(90 * 24 * time.Hour)).SaveX(ctx)
	claim := client.AttributionRequestClaim.Create().SetClaimGroupID(group.ID).SetRelayProviderID(provider.ID).SetRequestID("req-1").
		SetCanonicalDigest("digest").SetNextAttemptAt(now).SetExpiresAt(now.Add(90 * 24 * time.Hour)).SaveX(ctx)
	return reconcileFixture{client: client, providerID: provider.ID, claimID: claim.ID, now: now}
}

func newTestService(t *testing.T, fixture reconcileFixture, reader *requestReaderProvider) *Service {
	t.Helper()
	service, err := NewService(fixture.client, resolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != fixture.providerID {
			t.Fatalf("provider ID = %d", providerID)
		}
		return reader, nil
	}), zap.NewNop(), Options{Now: func() time.Time { return fixture.now }, RandFloat64: func() float64 { return 0 }})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestRunOnceReconcilesExactOwnedUsage(t *testing.T) {
	fixture := newReconcileFixture(t)
	reader := &requestReaderProvider{read: func(_ context.Context, requestID string, limit int) ([]relay.RequestUsage, error) {
		if requestID != "req-1" || limit != 2 {
			t.Fatalf("lookup = %q limit %d", requestID, limit)
		}
		return []relay.RequestUsage{{RequestID: "req-1", UserID: 42, RequestedModel: "gpt-test", UsageAt: fixture.now.Add(-time.Minute), InputTokens: 10, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 4}}, nil
	}}
	processed, err := newTestService(t, fixture, reader).RunOnce(context.Background())
	if err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v", processed, err)
	}
	claim := fixture.client.AttributionRequestClaim.GetX(context.Background(), fixture.claimID)
	if claim.Status != attributionrequestclaim.StatusReconciled || claim.TotalTokens != 19 || claim.RequestedModel != "gpt-test" || claim.ReconciledAt == nil || claim.MaterializedPoolID == nil || claim.LeaseExpiresAt != nil || claim.LeaseToken != "" {
		t.Fatalf("claim = %+v", claim)
	}
}

func TestReconciliationSerializesWithAllocationRematerialization(t *testing.T) {
	fixture := newReconcileFixture(t)
	ctx := context.Background()
	claim := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	group := fixture.client.AttributionClaimGroup.GetX(ctx, claim.ClaimGroupID)
	repo2 := fixture.client.RepoConfig.Create().SetName("repo-two").SetFullName("acme/repo-two").SetCloneURL("https://github.com/acme/repo-two.git").SaveX(ctx)
	reconcileTx, err := fixture.client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reconcileTx.Rollback() }()
	if err := lockClaimGroup(ctx, reconcileTx.Client(), group.ID); err != nil {
		t.Fatal(err)
	}
	usage := validUsage(fixture)
	if err := reconcileTx.Client().AttributionRequestClaim.UpdateOneID(claim.ID).SetStatus(attributionrequestclaim.StatusReconciled).
		SetRequestedModel(usage.RequestedModel).SetUsageAt(usage.UsageAt).SetInputTokens(usage.InputTokens).SetOutputTokens(usage.OutputTokens).
		SetCacheCreationTokens(usage.CacheCreationTokens).SetCacheReadTokens(usage.CacheReadTokens).SetTotalTokens(12).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := attributionpool.MaterializeRequestClaim(ctx, reconcileTx.Client(), claim.ID, fixture.now); err != nil {
		t.Fatal(err)
	}
	blockedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	err = fixture.client.AttributionClaimGroup.UpdateOneID(group.ID).SetEvidenceDigest("must-block").Exec(blockedCtx)
	blockedErr := blockedCtx.Err()
	cancel()
	if err == nil || !errors.Is(blockedErr, context.DeadlineExceeded) {
		t.Fatalf("allocation update while reconciliation lock held = %v, context = %v", err, blockedErr)
	}
	if err := reconcileTx.Commit(); err != nil {
		t.Fatal(err)
	}

	allocationTx, err := fixture.client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = allocationTx.Rollback() }()
	if err := allocationTx.Client().AttributionClaimGroup.UpdateOneID(group.ID).SetEvidenceDigest("evidence-shared").SetCommitAllocations([]map[string]any{
		{"repo_config_id": group.CommitAllocations[0]["repo_config_id"], "commit_sha": "commit-1"},
		{"repo_config_id": repo2.ID, "commit_sha": "commit-2"},
	}).Exec(ctx); err != nil {
		t.Fatal(err)
	}
	if err := attributionpool.MaterializeGroup(ctx, allocationTx.Client(), group.ID, fixture.now); err != nil {
		t.Fatal(err)
	}
	if err := allocationTx.Commit(); err != nil {
		t.Fatal(err)
	}

	pools := fixture.client.AttributionUsagePool.Query().AllX(ctx)
	if len(pools) != 1 || pools[0].RequestCount != 1 || pools[0].TotalTokens != 12 {
		t.Fatalf("pool conservation after allocation race = %+v", pools)
	}
	relations := fixture.client.AttributionUsagePoolCommit.Query().AllX(ctx)
	if len(relations) != 2 || relations[0].RelationKind != attributionusagepoolcommit.RelationKindShared || relations[1].RelationKind != attributionusagepoolcommit.RelationKindShared {
		t.Fatalf("shared relations after allocation race = %+v", relations)
	}
}

func TestRunOnceFailClosedOutcomes(t *testing.T) {
	tests := []struct {
		name string
		rows func(reconcileFixture) []relay.RequestUsage
		want attributionrequestclaim.Status
	}{
		{name: "pending", rows: func(reconcileFixture) []relay.RequestUsage { return nil }, want: attributionrequestclaim.StatusPending},
		{name: "ambiguous", rows: func(f reconcileFixture) []relay.RequestUsage {
			row := validUsage(f)
			return []relay.RequestUsage{row, row}
		}, want: attributionrequestclaim.StatusAmbiguous},
		{name: "owner mismatch", rows: func(f reconcileFixture) []relay.RequestUsage {
			row := validUsage(f)
			row.UserID = 99
			return []relay.RequestUsage{row}
		}, want: attributionrequestclaim.StatusOwnerMismatch},
		{name: "request mismatch", rows: func(f reconcileFixture) []relay.RequestUsage {
			row := validUsage(f)
			row.RequestID = "other"
			return []relay.RequestUsage{row}
		}, want: attributionrequestclaim.StatusInvalidUsage},
		{name: "empty model", rows: func(f reconcileFixture) []relay.RequestUsage {
			row := validUsage(f)
			row.RequestedModel = " "
			return []relay.RequestUsage{row}
		}, want: attributionrequestclaim.StatusInvalidUsage},
		{name: "zero usage time", rows: func(f reconcileFixture) []relay.RequestUsage {
			row := validUsage(f)
			row.UsageAt = time.Time{}
			return []relay.RequestUsage{row}
		}, want: attributionrequestclaim.StatusInvalidUsage},
		{name: "negative", rows: func(f reconcileFixture) []relay.RequestUsage {
			row := validUsage(f)
			row.InputTokens = -1
			return []relay.RequestUsage{row}
		}, want: attributionrequestclaim.StatusInvalidUsage},
		{name: "overflow", rows: func(f reconcileFixture) []relay.RequestUsage {
			row := validUsage(f)
			row.InputTokens = math.MaxInt64
			row.OutputTokens = 1
			return []relay.RequestUsage{row}
		}, want: attributionrequestclaim.StatusInvalidUsage},
		{name: "inconsistent total", rows: func(f reconcileFixture) []relay.RequestUsage {
			row := validUsage(f)
			total := int64(999)
			row.TotalTokens = &total
			return []relay.RequestUsage{row}
		}, want: attributionrequestclaim.StatusInvalidUsage},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReconcileFixture(t)
			reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) { return test.rows(fixture), nil }}
			if _, err := newTestService(t, fixture, reader).RunOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			claim := fixture.client.AttributionRequestClaim.GetX(context.Background(), fixture.claimID)
			if claim.Status != test.want || claim.LeaseToken != "" || claim.LeaseExpiresAt != nil {
				t.Fatalf("claim status = %s, want %s: %+v", claim.Status, test.want, claim)
			}
			if test.want == attributionrequestclaim.StatusPending && !claim.NextAttemptAt.After(fixture.now) {
				t.Fatalf("next attempt = %v", claim.NextAttemptAt)
			}
		})
	}
}

func TestRunOnceRejectsClaimGroupProviderMismatch(t *testing.T) {
	fixture := newReconcileFixture(t)
	ctx := context.Background()
	other := fixture.client.RelayProvider.Create().SetName("relay-beta").SetDisplayName("Relay Beta").SetBaseURL("https://relay-beta.example.com").SetAdminAPIKey("test-key").SaveX(ctx)
	claim := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	fixture.client.AttributionClaimGroup.UpdateOneID(claim.ClaimGroupID).SetRelayProviderID(other.ID).ExecX(ctx)
	var calls atomic.Int32
	reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
		calls.Add(1)
		return []relay.RequestUsage{validUsage(fixture)}, nil
	}}
	if _, err := newTestService(t, fixture, reader).RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	claim = fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	if claim.Status != attributionrequestclaim.StatusInvalidUsage || claim.LastErrorCode != "provider_mismatch" {
		t.Fatalf("claim = %+v", claim)
	}
	if calls.Load() != 0 {
		t.Fatalf("upstream calls = %d, want provider mismatch rejected before lookup", calls.Load())
	}
}

func TestRunOnceRevalidatesOwnerAfterLookup(t *testing.T) {
	fixture := newReconcileFixture(t)
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
		close(started)
		<-release
		return []relay.RequestUsage{validUsage(fixture)}, nil
	}}
	done := make(chan error, 1)
	go func() { _, err := newTestService(t, fixture, reader).RunOnce(ctx); done <- err }()
	<-started
	claim := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	group := fixture.client.AttributionClaimGroup.GetX(ctx, claim.ClaimGroupID)
	fixture.client.User.UpdateOneID(group.UserID).SetRelayUserID(99).ExecX(ctx)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	claim = fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	if claim.Status != attributionrequestclaim.StatusOwnerMismatch || claim.LastErrorCode != "owner_mismatch" {
		t.Fatalf("claim = %+v", claim)
	}
}

func TestRunOnceRevalidatesProviderAfterLookup(t *testing.T) {
	fixture := newReconcileFixture(t)
	ctx := context.Background()
	started := make(chan struct{})
	release := make(chan struct{})
	reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
		close(started)
		<-release
		return []relay.RequestUsage{validUsage(fixture)}, nil
	}}
	done := make(chan error, 1)
	go func() { _, err := newTestService(t, fixture, reader).RunOnce(ctx); done <- err }()
	<-started
	fixture.client.RelayProvider.UpdateOneID(fixture.providerID).SetEnabled(false).ExecX(ctx)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	claim := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	if claim.Status != attributionrequestclaim.StatusProviderUnavailable || claim.LastErrorCode != "provider_unavailable" || !claim.NextAttemptAt.After(fixture.now) {
		t.Fatalf("claim = %+v", claim)
	}
}

func TestRunOnceRejectsMissingGroupAndProviderBeforeLookup(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(context.Context, reconcileFixture)
		status attributionrequestclaim.Status
		code   string
	}{
		{name: "missing group", setup: func(ctx context.Context, f reconcileFixture) {
			f.client.AttributionRequestClaim.UpdateOneID(f.claimID).SetClaimGroupID(999999).ExecX(ctx)
		}, status: attributionrequestclaim.StatusInvalidUsage, code: "missing_claim_group"},
		{name: "missing provider", setup: func(ctx context.Context, f reconcileFixture) {
			claim := f.client.AttributionRequestClaim.GetX(ctx, f.claimID)
			f.client.AttributionClaimGroup.UpdateOneID(claim.ClaimGroupID).SetRelayProviderID(999999).ExecX(ctx)
			f.client.AttributionRequestClaim.UpdateOneID(f.claimID).SetRelayProviderID(999999).ExecX(ctx)
		}, status: attributionrequestclaim.StatusProviderUnavailable, code: "provider_unavailable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newReconcileFixture(t)
			ctx := context.Background()
			test.setup(ctx, fixture)
			reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
				t.Fatal("unexpected upstream lookup")
				return nil, nil
			}}
			service, err := NewService(fixture.client, resolverFunc(func(context.Context, int) (relay.Provider, error) { return reader, nil }), zap.NewNop(), Options{Now: func() time.Time { return fixture.now }, RandFloat64: func() float64 { return 0 }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			claim := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
			if claim.Status != test.status || claim.LastErrorCode != test.code {
				t.Fatalf("claim = %+v", claim)
			}
		})
	}
}

func TestRunOnceRequiresEnabledCapableProviderAndRelayOwner(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(context.Context, reconcileFixture)
		resolve  resolverFunc
		wantCode string
	}{
		{
			name: "disabled provider",
			setup: func(ctx context.Context, f reconcileFixture) {
				f.client.RelayProvider.UpdateOneID(f.providerID).SetEnabled(false).ExecX(ctx)
			},
			resolve:  func(context.Context, int) (relay.Provider, error) { return nil, errors.New("must not resolve") },
			wantCode: "provider_unavailable",
		},
		{
			name:     "unsupported provider",
			setup:    func(context.Context, reconcileFixture) {},
			resolve:  func(context.Context, int) (relay.Provider, error) { return struct{ relay.Provider }{}, nil },
			wantCode: "provider_unsupported",
		},
		{
			name: "missing relay owner",
			setup: func(ctx context.Context, f reconcileFixture) {
				claim := f.client.AttributionRequestClaim.GetX(ctx, f.claimID)
				group := f.client.AttributionClaimGroup.GetX(ctx, claim.ClaimGroupID)
				f.client.User.UpdateOneID(group.UserID).ClearRelayUserID().ExecX(ctx)
			},
			resolve: func(context.Context, int) (relay.Provider, error) {
				return &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
					return []relay.RequestUsage{{RequestID: "req-1", UserID: 42, RequestedModel: "gpt-test", UsageAt: time.Now()}}, nil
				}}, nil
			},
			wantCode: "owner_mismatch",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			fixture := newReconcileFixture(t)
			test.setup(ctx, fixture)
			service, err := NewService(fixture.client, test.resolve, zap.NewNop(), Options{Now: func() time.Time { return fixture.now }, RandFloat64: func() float64 { return 0 }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := service.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			claim := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
			if claim.LastErrorCode != test.wantCode || claim.LeaseToken != "" || claim.LeaseExpiresAt != nil {
				t.Fatalf("claim = %+v, want code %q", claim, test.wantCode)
			}
		})
	}
}

func TestRunOnceRecoversExpiredLease(t *testing.T) {
	fixture := newReconcileFixture(t)
	ctx := context.Background()
	fixture.client.AttributionRequestClaim.UpdateOneID(fixture.claimID).SetLeaseToken("dead-worker").SetLeaseExpiresAt(fixture.now.Add(-time.Second)).ExecX(ctx)
	reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
		return []relay.RequestUsage{validUsage(fixture)}, nil
	}}
	processed, err := newTestService(t, fixture, reader).RunOnce(ctx)
	if err != nil || processed != 1 {
		t.Fatalf("RunOnce = %d, %v", processed, err)
	}
	if got := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID).Status; got != attributionrequestclaim.StatusReconciled {
		t.Fatalf("status = %s", got)
	}
}

func TestQueuedCandidateGetsFreshLeaseTime(t *testing.T) {
	fixture := newReconcileFixture(t)
	ctx := context.Background()
	first := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	second := fixture.client.AttributionRequestClaim.Create().SetClaimGroupID(first.ClaimGroupID).SetRelayProviderID(fixture.providerID).SetRequestID("req-2").
		SetCanonicalDigest("digest-2").SetNextAttemptAt(fixture.now).SetExpiresAt(fixture.now.Add(90 * 24 * time.Hour)).SaveX(ctx)
	var clock atomic.Int64
	clock.Store(fixture.now.UnixNano())
	var calls atomic.Int32
	secondStarted := make(chan string)
	releaseSecond := make(chan struct{})
	reader := &requestReaderProvider{read: func(_ context.Context, requestID string, _ int) ([]relay.RequestUsage, error) {
		if calls.Add(1) == 1 {
			clock.Add(int64(time.Second))
		} else {
			secondStarted <- requestID
			<-releaseSecond
		}
		usage := validUsage(fixture)
		usage.RequestID = requestID
		return []relay.RequestUsage{usage}, nil
	}}
	service, err := NewService(fixture.client, resolverFunc(func(context.Context, int) (relay.Provider, error) { return reader, nil }), zap.NewNop(), Options{
		BatchSize: 2, Concurrency: 1, LeaseTTL: 100 * time.Millisecond,
		Now: func() time.Time { return time.Unix(0, clock.Load()).UTC() }, RandFloat64: func() float64 { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := service.RunOnce(ctx); done <- err }()
	blockedRequestID := <-secondStarted
	blockedID := second.ID
	if blockedRequestID == first.RequestID {
		blockedID = first.ID
	}
	leased := fixture.client.AttributionRequestClaim.GetX(ctx, blockedID)
	wantExpiry := fixture.now.Add(time.Second + 100*time.Millisecond)
	if leased.LeaseExpiresAt == nil || !leased.LeaseExpiresAt.Equal(wantExpiry) {
		t.Fatalf("queued lease expiry = %v, want %v", leased.LeaseExpiresAt, wantExpiry)
	}
	close(releaseSecond)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestLateWorkerCannotOverwriteRenewedLease(t *testing.T) {
	fixture := newReconcileFixture(t)
	ctx := context.Background()
	fixture.client.AttributionRequestClaim.UpdateOneID(fixture.claimID).SetLeaseToken("new-worker").SetLeaseExpiresAt(fixture.now.Add(time.Minute)).ExecX(ctx)
	err := newTestService(t, fixture, &requestReaderProvider{}).finish(ctx, fixture.claimID, "old-worker", attributionrequestclaim.StatusAmbiguous, "ambiguous_request")
	if err == nil {
		t.Fatal("late worker unexpectedly updated claim")
	}
	claim := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	if claim.LeaseToken != "new-worker" || claim.Status != attributionrequestclaim.StatusPending {
		t.Fatalf("claim = %+v", claim)
	}
}

func TestRetryDelayJitterAndCap(t *testing.T) {
	fixture := newReconcileFixture(t)
	service, err := NewService(fixture.client, resolverFunc(func(context.Context, int) (relay.Provider, error) { return nil, nil }), zap.NewNop(), Options{RandFloat64: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := service.retryDelay(1), 67500*time.Millisecond; got != want {
		t.Fatalf("first delay = %s, want %s", got, want)
	}
	if got, want := service.retryDelay(99), 4050*time.Second; got != want {
		t.Fatalf("capped delay = %s, want %s", got, want)
	}
}

func TestRunOnceLeaseCollapsesConcurrentReplicas(t *testing.T) {
	fixture := newReconcileFixture(t)
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
		calls.Add(1)
		close(started)
		<-release
		return []relay.RequestUsage{validUsage(fixture)}, nil
	}}
	first := newTestService(t, fixture, reader)
	second := newTestService(t, fixture, reader)
	done := make(chan error, 1)
	go func() { _, err := first.RunOnce(context.Background()); done <- err }()
	<-started
	processed, err := second.RunOnce(context.Background())
	if err != nil || processed != 0 {
		t.Fatalf("second RunOnce = %d, %v", processed, err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("reader calls = %d", calls.Load())
	}
}

func TestRunOnceRetriesProviderFailureWithBackoff(t *testing.T) {
	fixture := newReconcileFixture(t)
	service, err := NewService(fixture.client, resolverFunc(func(context.Context, int) (relay.Provider, error) {
		return nil, errors.New("offline")
	}), zap.NewNop(), Options{Now: func() time.Time { return fixture.now }, RandFloat64: func() float64 { return 0.5 }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim := fixture.client.AttributionRequestClaim.GetX(context.Background(), fixture.claimID)
	if claim.Status != attributionrequestclaim.StatusProviderUnavailable || claim.LastErrorCode != "provider_unavailable" || !claim.NextAttemptAt.After(fixture.now.Add(time.Minute)) {
		t.Fatalf("retry claim = %+v", claim)
	}
}

func TestRunOnceRetriesReaderFailureWithBackoff(t *testing.T) {
	fixture := newReconcileFixture(t)
	reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
		return nil, errors.New("upstream unavailable")
	}}
	if _, err := newTestService(t, fixture, reader).RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	claim := fixture.client.AttributionRequestClaim.GetX(context.Background(), fixture.claimID)
	if claim.Status != attributionrequestclaim.StatusProviderUnavailable || claim.LastErrorCode != "read_error" || !claim.NextAttemptAt.After(fixture.now) || claim.LeaseToken != "" || claim.LeaseExpiresAt != nil {
		t.Fatalf("claim = %+v", claim)
	}
}

func TestPostLookupTransitionsUseCompletionTime(t *testing.T) {
	t.Run("retry", func(t *testing.T) {
		fixture := newReconcileFixture(t)
		var clock atomic.Int64
		clock.Store(fixture.now.UnixNano())
		reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
			clock.Add(int64(2 * time.Minute))
			return nil, errors.New("slow failure")
		}}
		service, err := NewService(fixture.client, resolverFunc(func(context.Context, int) (relay.Provider, error) { return reader, nil }), zap.NewNop(), Options{Now: func() time.Time { return time.Unix(0, clock.Load()).UTC() }, RandFloat64: func() float64 { return 0 }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		claim := fixture.client.AttributionRequestClaim.GetX(context.Background(), fixture.claimID)
		completion := fixture.now.Add(2 * time.Minute)
		if !claim.NextAttemptAt.Equal(completion.Add(time.Minute)) {
			t.Fatalf("next attempt = %v, want completion-based %v", claim.NextAttemptAt, completion.Add(time.Minute))
		}
	})

	t.Run("reconciled", func(t *testing.T) {
		fixture := newReconcileFixture(t)
		var clock atomic.Int64
		clock.Store(fixture.now.UnixNano())
		reader := &requestReaderProvider{read: func(context.Context, string, int) ([]relay.RequestUsage, error) {
			clock.Add(int64(2 * time.Minute))
			return []relay.RequestUsage{validUsage(fixture)}, nil
		}}
		service, err := NewService(fixture.client, resolverFunc(func(context.Context, int) (relay.Provider, error) { return reader, nil }), zap.NewNop(), Options{Now: func() time.Time { return time.Unix(0, clock.Load()).UTC() }, RandFloat64: func() float64 { return 0 }})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.RunOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
		claim := fixture.client.AttributionRequestClaim.GetX(context.Background(), fixture.claimID)
		want := fixture.now.Add(2 * time.Minute)
		if claim.ReconciledAt == nil || !claim.ReconciledAt.Equal(want) {
			t.Fatalf("reconciled at = %v, want %v", claim.ReconciledAt, want)
		}
	})
}

func validUsage(fixture reconcileFixture) relay.RequestUsage {
	return relay.RequestUsage{RequestID: "req-1", UserID: 42, RequestedModel: "gpt-test", UsageAt: fixture.now.Add(-time.Minute), InputTokens: 10, OutputTokens: 2}
}
