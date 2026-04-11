package attribution

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestAttributionSchemasCreateAndQuery(t *testing.T) {
	client := testdb.Open(t)
	defer client.Close()

	ctx := context.Background()

	scm := client.ScmProvider.Create().
		SetName("gh").
		SetType(scmprovider.TypeGithub).
		SetBaseURL("https://api.github.com").
		SetCredentials("enc").
		SaveX(ctx)

	repo := client.RepoConfig.Create().
		SetScmProviderID(scm.ID).
		SetName("ai-efficiency").
		SetFullName("org/ai-efficiency").
		SetCloneURL("https://github.com/org/ai-efficiency.git").
		SetDefaultBranch("main").
		SetRelayProviderName("sub2api").
		SetRelayGroupID("g-default").
		SaveX(ctx)

	sess := client.Session.Create().
		SetRepoConfigID(repo.ID).
		SetBranch("main").
		SetProviderName("codex").
		SetRuntimeRef("rt-1").
		SetInitialWorkspaceRoot("/tmp/repo").
		SetInitialGitDir("/tmp/repo/.git").
		SetInitialGitCommonDir("/tmp/repo/.git").
		SetHeadShaAtStart("abc123").
		SetLastSeenAt(time.Now()).
		SaveX(ctx)

	client.SessionWorkspace.Create().
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetWorkspaceRoot("/tmp/repo").
		SetGitDir("/tmp/repo/.git").
		SetGitCommonDir("/tmp/repo/.git").
		SetBindingSource("marker").
		SaveX(ctx)

	client.CommitCheckpoint.Create().
		SetEventID("cp-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetBindingSource("marker").
		SetCommitSha("abc123").
		SetParentShas([]string{"000000"}).
		SetBranchSnapshot("feat/attribution").
		SetHeadSnapshot("abc123").
		SetAgentSnapshot(map[string]any{"codex": map[string]any{"total_tokens": 500}}).
		SetCapturedAt(time.Now()).
		SaveX(ctx)

	// Uniqueness/FK semantics: settlement lookup relies on (repo_config_id, commit_sha) being unique.
	// Expect duplicate inserts to fail.
	if _, err := client.CommitCheckpoint.Create().
		SetEventID("cp-dup").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetBindingSource("marker").
		SetCommitSha("abc123").
		SetParentShas([]string{"000000"}).
		SetCapturedAt(time.Now()).
		Save(ctx); err == nil {
		t.Fatalf("expected duplicate (repo_config_id, commit_sha) to fail")
	}

	// Integrity: repo_config_id should be a real FK.
	if _, err := client.CommitCheckpoint.Create().
		SetEventID("cp-bad-repo").
		SetWorkspaceID("ws-1").
		SetRepoConfigID(999999).
		SetBindingSource("unbound").
		SetCommitSha("deadbeef").
		SetParentShas([]string{"000000"}).
		SetCapturedAt(time.Now()).
		Save(ctx); err == nil {
		t.Fatalf("expected commit_checkpoint with unknown repo_config_id to fail")
	}

	client.CommitRewrite.Create().
		SetEventID("rw-1").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetRewriteType("amend").
		SetOldCommitSha("abc123").
		SetNewCommitSha("def456").
		SetBindingSource("marker").
		SetCapturedAt(time.Now()).
		SaveX(ctx)

	// Uniqueness semantics for rewrites: (repo_config_id, old_commit_sha, new_commit_sha, rewrite_type) must be unique.
	if _, err := client.CommitRewrite.Create().
		SetEventID("rw-dup").
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetRepoConfigID(repo.ID).
		SetRewriteType("amend").
		SetOldCommitSha("abc123").
		SetNewCommitSha("def456").
		SetBindingSource("marker").
		SetCapturedAt(time.Now()).
		Save(ctx); err == nil {
		t.Fatalf("expected duplicate commit_rewrite composite key to fail")
	}

	client.AgentMetadataEvent.Create().
		SetSessionID(sess.ID).
		SetWorkspaceID("ws-1").
		SetSource("codex").
		SetUsageUnit("token").
		SetInputTokens(10).
		SetOutputTokens(20).
		SetRawPayload(map[string]any{"k": "v"}).
		SetObservedAt(time.Now()).
		SaveX(ctx)

	pr := client.PrRecord.Create().
		SetRepoConfigID(repo.ID).
		SetScmPrID(55).
		SetStatus(prrecord.StatusOpen).
		SetAttributionStatus(prrecord.AttributionStatusNotRun).
		SaveX(ctx)

	// Completed runs must have a result_classification.
	if _, err := client.PrAttributionRun.Create().
		SetPrRecordID(pr.ID).
		SetTriggerMode("manual").
		SetTriggeredBy("alice").
		SetStatus("completed").
		Save(ctx); err == nil {
		t.Fatalf("expected completed attribution run without result_classification to fail")
	}

	// Integrity: last_attribution_run_id should not accept arbitrary integers.
	if _, err := client.PrRecord.UpdateOneID(pr.ID).
		SetLastAttributionRunID(999999).
		Save(ctx); err == nil {
		t.Fatalf("expected setting last_attribution_run_id to unknown run id to fail")
	}

	run := client.PrAttributionRun.Create().
		SetPrRecordID(pr.ID).
		SetTriggerMode("manual").
		SetTriggeredBy("alice").
		SetStatus("completed").
		SetResultClassification("clear").
		SetMatchedCommitShas([]string{"abc123"}).
		SetMatchedSessionIds([]string{sess.ID.String()}).
		SetPrimaryUsageSummary(map[string]any{"total_tokens": 500}).
		SetMetadataSummary(map[string]any{"codex": map[string]any{"total_tokens": 500}}).
		SetValidationSummary(map[string]any{"result": "consistent", "confidence": "high"}).
		SaveX(ctx)

	if run.ID == 0 {
		t.Fatal("expected attribution run ID to be assigned")
	}

	pr2 := client.PrRecord.Create().
		SetRepoConfigID(repo.ID).
		SetScmPrID(56).
		SetStatus(prrecord.StatusOpen).
		SetAttributionStatus(prrecord.AttributionStatusNotRun).
		SaveX(ctx)

	run2 := client.PrAttributionRun.Create().
		SetPrRecordID(pr2.ID).
		SetTriggerMode("manual").
		SetTriggeredBy("bob").
		SetStatus("completed").
		SetResultClassification("clear").
		SaveX(ctx)

	// Integrity: last_attribution_run_id must refer to a run that belongs to the same PR.
	if _, err := client.PrRecord.UpdateOneID(pr.ID).
		SetLastAttributionRunID(run2.ID).
		Save(ctx); err == nil {
		t.Fatalf("expected setting last_attribution_run_id to another PR's run to fail")
	}

	// Failed runs should not require result_classification to be fabricated.
	if _, err := client.PrAttributionRun.Create().
		SetPrRecordID(pr.ID).
		SetTriggerMode("manual").
		SetTriggeredBy("alice").
		SetStatus("failed").
		SetErrorMessage("boom").
		Save(ctx); err != nil {
		t.Fatalf("expected failed attribution run without result_classification to save, got: %v", err)
	}
}
