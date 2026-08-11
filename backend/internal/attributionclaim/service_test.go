package attributionclaim

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/internal/attributionledger"
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
		SetReporterTokenHash(uuid.NewString()).SetOtlpTokenHash(uuid.NewString()).SetReportingEnabled(true).SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("relay-alpha").SetDisplayName("Relay Alpha").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SaveX(ctx)
	repo := client.RepoConfig.Create().SetName("repo").SetFullName("org/repo").SetCloneURL("https://example.com/org/repo.git").SaveX(ctx)
	checkpoint := client.CommitCheckpoint.Create().SetEventID("checkpoint-1").SetUserID(user.ID).SetWorkspaceID("workspace-1").
		SetRepoConfigID(repo.ID).SetCommitSha("commit-1").SetParentShas([]string{"parent-1"}).SetBindingSource(commitcheckpoint.BindingSourceManual).SaveX(ctx)
	return fixture{
		client: client, service: NewService(client),
		principal:  attributionledger.InstallationPrincipal{DatabaseID: installation.ID, InstallationID: installation.InstallationID, UserID: user.ID},
		providerID: provider.ID, repoID: repo.ID, checkpoint: checkpoint.EventID,
	}
}

func (f fixture) claim(group string, requests ...string) Request {
	return Request{SchemaVersion: SchemaVersion, GroupID: group, RelayProviderID: f.providerID, RepoConfigID: f.repoID,
		CheckpointEventID: f.checkpoint, ThreadID: "thread-1", TurnID: "turn-1", EvidenceDigest: "evidence-1",
		CalibrationDigest: "calibration-1", RequestIDs: requests}
}

func TestIngestReplayAndLateRequest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	first, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.claim("group-1", "client:req-1", "req-2")}})
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
	if group.RequestCount != 3 || group.LedgerEpoch != LedgerEpoch || group.ExpiresAt.Before(time.Now().Add(89*24*time.Hour)) {
		t.Fatalf("group = %+v", group)
	}
	if count := f.client.AttributionRequestClaim.Query().CountX(ctx); count != 3 {
		t.Fatalf("request claim count = %d, want 3", count)
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
	if count := f.client.AttributionClaimGroup.Query().CountX(ctx); count != 2 {
		t.Fatalf("group count = %d, want original plus valid", count)
	}
	if count := f.client.AttributionRequestClaim.Query().Where(attributionrequestclaim.RequestIDEQ("req-never-persisted")).CountX(ctx); count != 0 {
		t.Fatalf("rolled back request count = %d", count)
	}
}

func TestIngestFailsClosedAndDoesNotTouchV1Ledger(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.client.RelayProvider.UpdateOneID(f.providerID).SetEnabled(false).ExecX(ctx)
	result, err := f.service.Ingest(ctx, f.principal, BatchRequest{Groups: []Request{f.claim("group-1", "req-1")}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Epoch != LedgerEpoch || result.Results[0].Group.Status != "rejected" {
		t.Fatalf("disabled provider result = %+v", result)
	}
	if f.client.AttributionClaimGroup.Query().CountX(ctx) != 0 || f.client.AttributionUsageBucket.Query().CountX(ctx) != 0 {
		t.Fatal("rejected shadow ingest changed claim or v1 formal ledger")
	}
}
