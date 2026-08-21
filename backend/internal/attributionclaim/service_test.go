package attributionclaim

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/attributionpool"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/google/uuid"
)

type fixture struct {
	client     *ent.Client
	service    *Service
	principal  attributionledger.InstallationPrincipal
	providerID int
	repoID     int
	checkpoint string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	ctx := context.Background()
	client := testdb.Open(t)
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	installation := client.ReportingInstallation.Create().SetInstallationID(uuid.NewString()).SetUserID(user.ID).
		SetReporterTokenHash(uuid.NewString()).SetReportingEnabled(true).SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("relay-alpha").SetDisplayName("Relay Alpha").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SaveX(ctx)
	repo := client.RepoConfig.Create().SetName("repo").SetFullName("org/repo").SetCloneURL("https://example.com/org/repo.git").SaveX(ctx)
	checkpoint := client.CommitCheckpoint.Create().SetEventID("checkpoint-1").SetUserID(user.ID).SetWorkspaceID("workspace-1").
		SetRepoConfigID(repo.ID).SetCommitSha("commit-1").SetParentShas([]string{"parent-1"}).SetBindingSource(commitcheckpoint.BindingSourceManual).SaveX(ctx)
	return fixture{
		client: client, service: NewService(client, attributionledger.DefaultProtocolContract()),
		principal:  attributionledger.InstallationPrincipal{DatabaseID: installation.ID, InstallationID: installation.InstallationID, UserID: user.ID},
		providerID: provider.ID, repoID: repo.ID, checkpoint: checkpoint.EventID,
	}
}

func (f fixture) claim(group string, requests ...string) Request {
	return Request{SchemaVersion: SchemaVersion, GroupID: group, RelayProviderID: f.providerID,
		ThreadID: "thread-1", TurnID: "turn-1", EvidenceDigest: "evidence-1",
		Calibration:       &Calibration{Digest: "calibration-1", InputTokens: 10, OutputTokens: 2, TotalTokens: 12},
		CommitAllocations: []CommitAllocation{{Sequence: 1, RepoConfigID: f.repoID, WorkspaceID: "workspace-1", CheckpointEventID: f.checkpoint, CommitSHA: "commit-1", EvidenceDigest: "evidence-1"}},
		RequestIDs:        requests}
}

func (f fixture) localClaim(group string, usage LocalUsageBucket) Request {
	claim := f.claim(group)
	claim.TokenSource = TokenSourceCodexLocal
	claim.Calibration = nil
	claim.LocalUsage = []LocalUsageBucket{usage}
	return claim
}

func TestIngestMaterializesCodexLocalUsageWithoutRequestRows(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	bucket := time.Date(2026, 8, 13, 12, 15, 0, 0, time.UTC)
	usage := LocalUsageBucket{RequestedModel: "gpt-test", BucketStartUTC: bucket, InputTokens: 50, CacheReadTokens: 40, CacheCreationTokens: 10, OutputTokens: 20, TotalTokens: 120, RequestCount: 2}
	claim := f.localClaim("group-local", usage)

	first, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || first.Results[0].Group.Status != "persisted" {
		t.Fatalf("first local ingest = %+v, %v", first, err)
	}
	if count := f.client.AttributionRequestClaim.Query().CountX(ctx); count != 0 {
		t.Fatalf("local Request rows = %d, want 0", count)
	}
	pool := f.client.AttributionUsagePool.Query().OnlyX(ctx)
	if pool.InputTokens != 50 || pool.CacheReadTokens != 40 || pool.CacheCreationTokens != 10 || pool.OutputTokens != 20 || pool.TotalTokens != 120 || pool.RequestCount != 2 || pool.RequestedModel != "gpt-test" || !pool.BucketStartUtc.Equal(bucket) {
		t.Fatalf("local pool = %+v", pool)
	}

	replay, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || replay.Results[0].Group.Status != "duplicate_identical" || f.client.AttributionUsagePool.Query().OnlyX(ctx).TotalTokens != 120 {
		t.Fatalf("local replay = %+v, %v", replay, err)
	}

	claim.LocalUsage[0] = LocalUsageBucket{RequestedModel: "gpt-test", BucketStartUTC: bucket, InputTokens: 70, CacheReadTokens: 50, CacheCreationTokens: 10, OutputTokens: 25, TotalTokens: 155, RequestCount: 3}
	updated, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || updated.Results[0].Group.Status != "persisted" {
		t.Fatalf("local update = %+v, %v", updated, err)
	}
	pool = f.client.AttributionUsagePool.Query().OnlyX(ctx)
	if pool.TotalTokens != 155 || pool.RequestCount != 3 {
		t.Fatalf("updated local pool = %+v", pool)
	}
	repo2 := f.client.RepoConfig.Create().SetName("repo-two").SetFullName("org/repo-two").SetCloneURL("https://example.com/org/repo-two.git").SaveX(ctx)
	checkpoint2 := f.client.CommitCheckpoint.Create().SetEventID("checkpoint-2").SetUserID(f.principal.UserID).SetWorkspaceID("workspace-1").
		SetRepoConfigID(repo2.ID).SetCommitSha("commit-2").SetParentShas([]string{"parent-2"}).SetBindingSource(commitcheckpoint.BindingSourceManual).SaveX(ctx)
	claim.EvidenceDigest = "evidence-2"
	claim.CommitAllocations = append(claim.CommitAllocations, CommitAllocation{
		Sequence: 2, RepoConfigID: repo2.ID, WorkspaceID: "workspace-1", CheckpointEventID: checkpoint2.EventID, CommitSHA: "commit-2", EvidenceDigest: "evidence-2",
	})
	shared, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || shared.Results[0].Group.Status != "persisted" {
		t.Fatalf("local allocation append = %+v, %v", shared, err)
	}
	pool = f.client.AttributionUsagePool.Query().OnlyX(ctx)
	relations := f.client.AttributionUsagePoolCommit.Query().AllX(ctx)
	if pool.TotalTokens != 155 || pool.RequestCount != 3 || len(relations) != 2 || relations[0].RelationKind != attributionusagepoolcommit.RelationKindShared || relations[1].RelationKind != attributionusagepoolcommit.RelationKindShared {
		t.Fatalf("shared local pool = %+v, relations = %+v", pool, relations)
	}

	claim.LocalUsage[0] = usage
	conflict, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || conflict.Results[0].Group.Status != "rejected" || f.client.AttributionUsagePool.Query().OnlyX(ctx).TotalTokens != 155 {
		t.Fatalf("regressed local usage = %+v, %v", conflict, err)
	}
}

func TestIngestRejectsInvalidCodexLocalContracts(t *testing.T) {
	f := newFixture(t)
	bucket := time.Date(2026, 8, 13, 12, 15, 0, 0, time.UTC)
	valid := LocalUsageBucket{RequestedModel: "gpt-test", BucketStartUTC: bucket, InputTokens: 10, OutputTokens: 2, TotalTokens: 12, RequestCount: 1}
	tests := []struct {
		name   string
		mutate func(*Request)
	}{
		{name: "request id", mutate: func(claim *Request) { claim.RequestIDs = []string{"client:req"} }},
		{name: "calibration", mutate: func(claim *Request) { claim.Calibration = &Calibration{Digest: "calibration", TotalTokens: 1} }},
		{name: "missing usage", mutate: func(claim *Request) { claim.LocalUsage = nil }},
		{name: "missing model", mutate: func(claim *Request) { claim.LocalUsage[0].RequestedModel = "" }},
		{name: "unaligned bucket", mutate: func(claim *Request) { claim.LocalUsage[0].BucketStartUTC = bucket.Add(time.Minute) }},
		{name: "inconsistent total", mutate: func(claim *Request) { claim.LocalUsage[0].TotalTokens++ }},
		{name: "overflow", mutate: func(claim *Request) {
			claim.LocalUsage[0].InputTokens = math.MaxInt64
			claim.LocalUsage[0].OutputTokens = 1
			claim.LocalUsage[0].TotalTokens = math.MaxInt64
		}},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			claim := f.localClaim("invalid-local-"+test.name, valid)
			test.mutate(&claim)
			result, err := f.service.Ingest(context.Background(), f.principal, BatchRequest{Groups: []Request{claim}})
			if err != nil || result.Results[0].Group.Status != "rejected" {
				t.Fatalf("invalid local contract %d = %+v, %v", index, result, err)
			}
		})
	}
	if groups := f.client.AttributionClaimGroup.Query().CountX(context.Background()); groups != 0 {
		t.Fatalf("invalid local groups persisted = %d", groups)
	}
}

func TestIngestRejectsLocalUsageOnRelayOfficialClaim(t *testing.T) {
	f := newFixture(t)
	claim := f.claim("invalid-relay-local-usage", "client:req")
	claim.LocalUsage = []LocalUsageBucket{{RequestedModel: "gpt-test", BucketStartUTC: time.Date(2026, 8, 13, 12, 15, 0, 0, time.UTC), InputTokens: 1, TotalTokens: 1, RequestCount: 1}}
	result, err := f.service.Ingest(context.Background(), f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || result.Results[0].Group.Status != "rejected" || f.client.AttributionClaimGroup.Query().ExistX(context.Background()) {
		t.Fatalf("relay claim with local usage = %+v, %v", result, err)
	}
}

func TestIngestReplayAndLateRequest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.claim("group-1", "req-1", "req-2")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := first.Results[0]; got.Group.Status != "persisted" || got.Calibration.Status != "persisted" || got.Requests[0].ID != "req-1" || got.Requests[0].Status != "persisted" {
		t.Fatalf("first result = %+v", got)
	}
	replay, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.claim("group-1", "req-1", "req-2")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := replay.Results[0]; got.Group.Status != "duplicate_identical" || got.Calibration.Status != "duplicate_identical" || got.Requests[0].Status != "duplicate_identical" {
		t.Fatalf("replay result = %+v", got)
	}
	late, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.claim("group-1", "req-1", "req-2", "req-3")}})
	if err != nil {
		t.Fatal(err)
	}
	if got := late.Results[0]; got.Group.Status != "persisted" || got.Requests[2].Status != "persisted" {
		t.Fatalf("late result = %+v", got)
	}
	group := f.client.AttributionClaimGroup.Query().OnlyX(ctx)
	if group.RequestCount != 3 || group.LedgerEpoch != attributionledger.LedgerEpochShadowV2 || group.ExpiresAt.Before(time.Now().Add(89*24*time.Hour)) {
		t.Fatalf("group = %+v", group)
	}
	if count := f.client.AttributionRequestClaim.Query().CountX(ctx); count != 3 {
		t.Fatalf("request claim count = %d, want 3", count)
	}
}

func TestIngestRejectsFinalizedGroup(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.claim("group-final", "req-1")}}); err != nil {
		t.Fatal(err)
	}
	group := f.client.AttributionClaimGroup.Query().OnlyX(ctx)
	f.client.AttributionClaimGroup.UpdateOneID(group.ID).SetFinalizedAt(time.Now().UTC()).ExecX(ctx)
	replay := f.claim("group-final", "req-1", "req-late")
	result, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{replay}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Results[0].Group.Status != "rejected" {
		t.Fatalf("finalized replay result = %+v", result.Results[0])
	}
	if f.client.AttributionRequestClaim.Query().Where(attributionrequestclaim.RequestIDEQ("req-late")).ExistX(ctx) {
		t.Fatal("late Request was added to a finalized group")
	}
}

func TestIngestConflictRollsBackIndependentGroup(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.claim("group-1", "req-1")}}); err != nil {
		t.Fatal(err)
	}
	conflict := f.claim("group-2", "req-1", "req-never-persisted")
	conflict.TurnID = "turn-2"
	valid := f.claim("group-3", "req-3")
	result, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{conflict, valid}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Results[0].Group.Status != "rejected" || result.Results[1].Group.Status != "persisted" {
		t.Fatalf("partial batch = %+v", result)
	}
	if len(result.Results[0].Requests) != 1 || result.Results[0].Requests[0].Status != "conflict" {
		t.Fatalf("conflict item acknowledgement = %+v", result.Results[0].Requests)
	}
	if result.Results[0].Calibration.Status != "rolled_back" {
		t.Fatalf("rolled back calibration acknowledgement = %+v", result.Results[0].Calibration)
	}
	if count := f.client.AttributionClaimGroup.Query().CountX(ctx); count != 2 {
		t.Fatalf("group count = %d, want original plus valid", count)
	}
	if count := f.client.AttributionRequestClaim.Query().Where(attributionrequestclaim.RequestIDEQ("req-never-persisted")).CountX(ctx); count != 0 {
		t.Fatalf("rolled back request count = %d", count)
	}
}

func TestIngestFailsClosedWithoutPersistingClaims(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.client.RelayProvider.UpdateOneID(f.providerID).SetEnabled(false).ExecX(ctx)
	result, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.claim("group-1", "req-1")}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Epoch != attributionledger.LedgerEpochShadowV2 || result.Results[0].Group.Status != "rejected" {
		t.Fatalf("disabled provider result = %+v", result)
	}
	if f.client.AttributionClaimGroup.Query().CountX(ctx) != 0 {
		t.Fatal("rejected shadow ingest persisted a claim")
	}
}

func TestIngestAppendsAllocationAcceptsOldReplayAndRejectsDivergence(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := f.claim("group-allocations", "req-1")
	if _, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{base}}); err != nil {
		t.Fatal(err)
	}
	second := f.client.CommitCheckpoint.Create().SetEventID("checkpoint-2").SetUserID(f.principal.UserID).SetWorkspaceID("workspace-1").
		SetRepoConfigID(f.repoID).SetCommitSha("commit-2").SetParentShas([]string{"commit-1"}).SetBindingSource(commitcheckpoint.BindingSourceManual).SaveX(ctx)
	appended := base
	appended.EvidenceDigest = "allocation-sequence-evidence"
	appended.CommitAllocations = append(append([]CommitAllocation(nil), base.CommitAllocations...), CommitAllocation{
		Sequence: 2, RepoConfigID: f.repoID, WorkspaceID: "workspace-1", CheckpointEventID: second.EventID, CommitSHA: "commit-2", EvidenceDigest: "evidence-2",
	})
	result, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{appended}})
	if err != nil || result.Results[0].Group.Status != "persisted" {
		t.Fatalf("append result = %+v, err = %v", result, err)
	}
	oldReplay, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{base}})
	if err != nil || oldReplay.Results[0].Group.Status != "rejected" {
		t.Fatalf("old replay = %+v, err = %v", oldReplay, err)
	}
	group := f.client.AttributionClaimGroup.Query().OnlyX(ctx)
	if len(group.CommitAllocations) != 2 || group.EvidenceDigest != appended.EvidenceDigest {
		t.Fatalf("stored allocation sequence = %+v", group)
	}
	divergent := appended
	divergent.CommitAllocations = append([]CommitAllocation(nil), appended.CommitAllocations...)
	divergent.CommitAllocations[0].EvidenceDigest = "different"
	conflict, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{divergent}})
	if err != nil || conflict.Results[0].Group.Status != "rejected" {
		t.Fatalf("divergent result = %+v, err = %v", conflict, err)
	}
}

func TestIngestAllowsAcknowledgedClientToAppendAllocationWithoutRequestIDs(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := f.claim("group-allocation-only", "req-1")
	if _, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{base}}); err != nil {
		t.Fatal(err)
	}
	hotClaim := f.client.AttributionRequestClaim.Query().OnlyX(ctx)
	usageAt := time.Date(2026, 8, 11, 12, 17, 0, 0, time.UTC)
	f.client.AttributionRequestClaim.UpdateOneID(hotClaim.ID).SetStatus(attributionrequestclaim.StatusReconciled).SetRequestedModel("gpt-test").SetUsageAt(usageAt).
		SetInputTokens(10).SetOutputTokens(2).SetTotalTokens(12).ExecX(ctx)
	tx, err := f.client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := attributionpool.MaterializeRequestClaim(ctx, tx.Client(), hotClaim.ID, usageAt); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	second := f.client.CommitCheckpoint.Create().SetEventID("checkpoint-2").SetUserID(f.principal.UserID).SetWorkspaceID("workspace-1").
		SetRepoConfigID(f.repoID).SetCommitSha("commit-2").SetParentShas([]string{"commit-1"}).SetBindingSource(commitcheckpoint.BindingSourceManual).SaveX(ctx)
	appended := base
	appended.RequestIDs = nil
	appended.Calibration = nil
	appended.EvidenceDigest = "allocation-sequence-evidence"
	appended.CommitAllocations = append(append([]CommitAllocation(nil), base.CommitAllocations...), CommitAllocation{
		Sequence: 2, RepoConfigID: f.repoID, WorkspaceID: "workspace-1", CheckpointEventID: second.EventID, CommitSHA: "commit-2", EvidenceDigest: "evidence-2",
	})
	result, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{appended}})
	if err != nil || result.Results[0].Group.Status != "persisted" || len(result.Results[0].Requests) != 0 {
		t.Fatalf("allocation-only append = %+v, %v", result, err)
	}
	group := f.client.AttributionClaimGroup.Query().OnlyX(ctx)
	if len(group.CommitAllocations) != 2 || group.RequestCount != 1 {
		t.Fatalf("group = %+v", group)
	}
	pools := f.client.AttributionUsagePool.Query().AllX(ctx)
	if len(pools) != 1 || pools[0].RequestCount != 1 || pools[0].TotalTokens != 12 {
		t.Fatalf("rematerialized pools = %+v", pools)
	}
	relations := f.client.AttributionUsagePoolCommit.Query().AllX(ctx)
	if len(relations) != 2 || relations[0].RelationKind != attributionusagepoolcommit.RelationKindShared || relations[1].RelationKind != attributionusagepoolcommit.RelationKindShared {
		t.Fatalf("rematerialized relations = %+v", relations)
	}
}

func TestIngestRejectsNewGroupWithoutRequestIDs(t *testing.T) {
	f := newFixture(t)
	claim := f.claim("new-empty")
	result, err := f.service.Ingest(context.Background(), f.principal, BatchRequest{Groups: []Request{claim}})
	if err != nil || result.Results[0].Group.Status != "rejected" || f.client.AttributionClaimGroup.Query().CountX(context.Background()) != 0 {
		t.Fatalf("new empty group = %+v, %v", result, err)
	}
}

func TestInvalidatePersistedACKsPreservesNonPersistedItems(t *testing.T) {
	result := Result{
		Calibration: ItemStatus{Status: "persisted"},
		Requests:    []ItemStatus{{Status: "persisted"}, {Status: "duplicate_identical"}, {Status: "conflict"}},
	}
	invalidatePersistedACKs(&result, "unknown")
	if result.Calibration.Status != "unknown" || result.Requests[0].Status != "unknown" || result.Requests[1].Status != "duplicate_identical" || result.Requests[2].Status != "conflict" {
		t.Fatalf("invalidated result = %+v", result)
	}
}

func TestIngestCalibrationConflictDoesNotBlockNewRequest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	base := f.claim("group-calibration", "req-1")
	if _, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{base}}); err != nil {
		t.Fatal(err)
	}
	changed := f.claim("group-calibration", "req-1", "req-2")
	changed.Calibration = &Calibration{Digest: "different", InputTokens: 99, TotalTokens: 99}
	result, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{changed}})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Results[0]; got.Calibration.Status != "conflict" || got.Requests[1].Status != "persisted" {
		t.Fatalf("independent calibration result = %+v", got)
	}
	group := f.client.AttributionClaimGroup.Query().OnlyX(ctx)
	if group.CalibrationDigest != "calibration-1" || group.RequestCount != 2 {
		t.Fatalf("stored group = %+v", group)
	}
}

func TestIngestRejectsCheckpointWorkspaceAndCommitMismatch(t *testing.T) {
	f := newFixture(t)
	for _, mutate := range []func(*Request){
		func(claim *Request) { claim.CommitAllocations[0].WorkspaceID = "other-workspace" },
		func(claim *Request) { claim.CommitAllocations[0].CommitSHA = "other-commit" },
	} {
		claim := f.claim(uuid.NewString(), uuid.NewString())
		mutate(&claim)
		result, err := f.service.Ingest(context.Background(), f.principal, BatchRequest{Groups: []Request{claim}})
		if err != nil || result.Results[0].Group.Status != "rejected" {
			t.Fatalf("mismatch result = %+v, err = %v", result, err)
		}
	}
	if f.client.AttributionClaimGroup.Query().CountX(context.Background()) != 0 {
		t.Fatal("mismatched allocation was persisted")
	}
}
