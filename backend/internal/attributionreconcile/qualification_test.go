package attributionreconcile

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/internal/activity"
	"github.com/ai-efficiency/backend/internal/attributionclaim"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"go.uber.org/zap"
)

type qualificationDenominator struct{}

func (qualificationDenominator) ResolveDenominator(_ context.Context, request activity.V2DenominatorRequest) (activity.V2Denominator, error) {
	return activity.V2Denominator{
		TotalTokens: 100,
		AsOf:        time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
		Fresh:       true,
		Complete:    true,
		ProviderSet: request.ProviderSet,
	}, nil
}

func TestSyntheticRequestToActivityKeepsShadowEpochIsolated(t *testing.T) {
	ctx := context.Background()
	client, dsn := testdb.OpenWithDSN(t)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SetRelayUserID(42).SaveX(ctx)
	installation := client.ReportingInstallation.Create().SetInstallationID(uuid.NewString()).SetUserID(user.ID).
		SetReporterTokenHash(uuid.NewString()).SetOtlpTokenHash(uuid.NewString()).SetReportingEnabled(true).SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("relay-alpha").SetDisplayName("Relay Alpha").SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-key").SetEnabled(true).SaveX(ctx)
	repo := client.RepoConfig.Create().SetName("repo").SetFullName("example/repo").SetCloneURL("https://example.com/example/repo.git").SaveX(ctx)
	checkpoint := client.CommitCheckpoint.Create().SetEventID("checkpoint-qualification").SetUserID(user.ID).SetWorkspaceID("workspace-qualification").
		SetRepoConfigID(repo.ID).SetCommitSha("commit-qualification").SetParentShas([]string{"parent"}).SetBindingSource(commitcheckpoint.BindingSourceManual).SaveX(ctx)

	claim := attributionclaim.Request{
		SchemaVersion: attributionclaim.SchemaVersion, GroupID: "group-qualification", RelayProviderID: provider.ID,
		ThreadID: "thread-qualification", TurnID: "turn-qualification", EvidenceDigest: "evidence-qualification",
		CommitAllocations: []attributionclaim.CommitAllocation{{
			Sequence: 1, RepoConfigID: repo.ID, WorkspaceID: checkpoint.WorkspaceID, CheckpointEventID: checkpoint.EventID,
			CommitSHA: checkpoint.CommitSha, EvidenceDigest: "evidence-qualification",
		}},
		RequestIDs: []string{"request-qualification"},
	}
	principal := attributionledger.InstallationPrincipal{DatabaseID: installation.ID, InstallationID: installation.InstallationID, UserID: user.ID}
	result, err := attributionclaim.NewService(client, attributionledger.DefaultProtocolContract()).Ingest(ctx, principal, attributionclaim.BatchRequest{Groups: []attributionclaim.Request{claim}})
	if err != nil || result.Epoch != attributionledger.LedgerEpochShadowV2 || result.Results[0].Requests[0].Status != "persisted" {
		t.Fatalf("claim ingest = %+v, %v", result, err)
	}
	if client.AttributionUsageBucket.Query().CountX(ctx) != 0 {
		t.Fatal("v2 claim ingest wrote the v1 usage bucket table")
	}
	requestClaim := client.AttributionRequestClaim.Query().OnlyX(ctx)
	client.AttributionRequestClaim.UpdateOne(requestClaim).SetNextAttemptAt(now).ExecX(ctx)

	reader := &requestReaderProvider{read: func(_ context.Context, requestID string, limit int) ([]relay.RequestUsage, error) {
		if requestID != "request-qualification" || limit != 2 {
			t.Fatalf("usage lookup = %q limit=%d", requestID, limit)
		}
		return []relay.RequestUsage{{
			RequestID: requestID, UserID: 42, RequestedModel: "gpt-test", UsageAt: now,
			InputTokens: 10, OutputTokens: 2, CacheCreationTokens: 3, CacheReadTokens: 4,
		}}, nil
	}}
	reconciler, err := NewService(client, resolverFunc(func(context.Context, int) (relay.Provider, error) { return reader, nil }), zap.NewNop(), Options{
		Now: func() time.Time { return now }, RandFloat64: func() float64 { return 0 },
	})
	if err != nil {
		t.Fatal(err)
	}
	if processed, err := reconciler.RunOnce(ctx); err != nil || processed != 1 {
		t.Fatalf("reconcile = %d, %v", processed, err)
	}
	pool := client.AttributionUsagePool.Query().OnlyX(ctx)
	if pool.LedgerEpoch != attributionledger.LedgerEpochShadowV2 || pool.TotalTokens != 19 || pool.RequestCount != 1 {
		t.Fatalf("shadow pool = %+v", pool)
	}
	if client.AttributionUsageBucket.Query().CountX(ctx) != 0 {
		t.Fatal("v2 reconciliation wrote the v1 usage bucket table")
	}

	query := activity.V2Query{Scope: activity.V2ScopePersonal, FromDate: "2026-08-11", ToDate: "2026-08-11", Timezone: "UTC"}
	formal := activity.NewService(client, activity.ServiceOptions{V2LedgerEpoch: "formal_v2", V2DB: db, V2Denominator: qualificationDenominator{}})
	formalOverview, err := formal.V2Overview(ctx, user.ID, query)
	if err != nil || formalOverview.CommittedTokens != 0 || formalOverview.Readiness.State != "waiting_for_data" {
		t.Fatalf("formal Activity consumed shadow data: overview=%+v err=%v", formalOverview, err)
	}
	shadow := activity.NewService(client, activity.ServiceOptions{V2LedgerEpoch: attributionledger.LedgerEpochShadowV2, V2DB: db, V2Denominator: qualificationDenominator{}})
	shadowOverview, err := shadow.V2Overview(ctx, user.ID, query)
	if err != nil || shadowOverview.CommittedTokens != 19 || shadowOverview.Readiness.State != "active" || shadowOverview.Ratio.Percent == nil || *shadowOverview.Ratio.Percent != 19 {
		t.Fatalf("shadow Activity readback = %+v, %v", shadowOverview, err)
	}
}
