package attribution

import (
	"context"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/enttest"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	_ "github.com/mattn/go-sqlite3"
)

func TestAttributionSchemasCreateAndQuery(t *testing.T) {
	client := enttest.Open(t, "sqlite3", "file:ent?mode=memory&_fk=1")
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

	pr := client.PrRecord.Create().
		SetRepoConfigID(repo.ID).
		SetScmPrID(55).
		SetStatus(prrecord.StatusOpen).
		SetAttributionStatus(prrecord.AttributionStatusNotRun).
		SaveX(ctx)

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
}
