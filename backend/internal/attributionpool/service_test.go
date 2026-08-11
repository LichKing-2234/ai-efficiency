package attributionpool

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionrequestclaim"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/internal/testdb"
)

type poolFixture struct {
	client  *ent.Client
	userID  int
	groupID int
	claimID int
	repo1   int
	repo2   int
	now     time.Time
}

func newPoolFixture(t *testing.T) poolFixture {
	t.Helper()
	ctx := context.Background()
	client := testdb.Open(t)
	now := time.Date(2026, 8, 11, 12, 17, 0, 0, time.UTC)
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("relay-alpha").SetDisplayName("Relay Alpha").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SaveX(ctx)
	repo1 := client.RepoConfig.Create().SetName("one").SetFullName("acme/one").SetCloneURL("https://github.com/acme/one.git").SaveX(ctx)
	repo2 := client.RepoConfig.Create().SetName("two").SetFullName("acme/two").SetCloneURL("https://github.com/acme/two.git").SaveX(ctx)
	group := client.AttributionClaimGroup.Create().SetGroupID("group-1").SetInstallationID(1).SetUserID(user.ID).SetRelayProviderID(provider.ID).
		SetSchemaVersion(2).SetThreadID("thread-1").SetTurnID("turn-1").SetEvidenceDigest("evidence-1").
		SetCommitAllocations([]map[string]any{{"repo_config_id": repo1.ID, "commit_sha": "commit-a"}}).
		SetRequestCount(1).SetExpiresAt(now.Add(90 * 24 * time.Hour)).SaveX(ctx)
	usageAt := now
	claim := client.AttributionRequestClaim.Create().SetClaimGroupID(group.ID).SetRelayProviderID(provider.ID).SetRequestID("req-1").
		SetCanonicalDigest("digest-1").SetStatus(attributionrequestclaim.StatusReconciled).SetRequestedModel("gpt-test").SetUsageAt(usageAt).
		SetInputTokens(10).SetOutputTokens(2).SetCacheCreationTokens(3).SetCacheReadTokens(4).SetTotalTokens(19).
		SetNextAttemptAt(now).SetExpiresAt(now.Add(90 * 24 * time.Hour)).SaveX(ctx)
	return poolFixture{client: client, userID: user.ID, groupID: group.ID, claimID: claim.ID, repo1: repo1.ID, repo2: repo2.ID, now: now}
}

func materializeInTransaction(t *testing.T, fixture poolFixture, claimID int) error {
	t.Helper()
	tx, err := fixture.client.Tx(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := MaterializeRequestClaim(context.Background(), tx.Client(), claimID, fixture.now); err != nil {
		return err
	}
	return tx.Commit()
}

func TestMaterializeRequestClaimStoresOfficialTokenOnce(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	pools := fixture.client.AttributionUsagePool.Query().AllX(ctx)
	if len(pools) != 1 {
		t.Fatalf("pools = %d, want 1", len(pools))
	}
	pool := pools[0]
	if pool.RelayProviderID <= 0 || pool.InputTokens != 10 || pool.OutputTokens != 2 || pool.CacheCreationTokens != 3 || pool.CacheReadTokens != 4 || pool.TotalTokens != 19 || pool.RequestCount != 1 || !pool.BucketStartUtc.Equal(time.Date(2026, 8, 11, 12, 15, 0, 0, time.UTC)) {
		t.Fatalf("pool = %+v", pool)
	}
	relations := fixture.client.AttributionUsagePoolCommit.Query().AllX(ctx)
	if len(relations) != 1 || relations[0].RepoConfigID != fixture.repo1 || relations[0].CommitSha != "commit-a" || relations[0].RelationKind != attributionusagepoolcommit.RelationKindDirect {
		t.Fatalf("relations = %+v", relations)
	}
}

func TestCanonicalContributionIgnoresAllocationOrderAndDuplicates(t *testing.T) {
	usageAt := time.Date(2026, 8, 11, 12, 17, 0, 0, time.UTC)
	claim := &ent.AttributionRequestClaim{RequestedModel: "gpt-test", UsageAt: &usageAt, InputTokens: 1, TotalTokens: 1}
	first, err := canonicalContribution(3, 7, []map[string]any{
		{"repo_config_id": float64(2), "commit_sha": "bbb"},
		{"repo_config_id": float64(1), "commit_sha": "aaa"},
	}, claim)
	if err != nil {
		t.Fatal(err)
	}
	second, err := canonicalContribution(3, 7, []map[string]any{
		{"repo_config_id": 1, "commit_sha": "aaa"},
		{"repo_config_id": 2, "commit_sha": "bbb"},
		{"repo_config_id": 1, "commit_sha": "aaa"},
	}, claim)
	if err != nil {
		t.Fatal(err)
	}
	if first.key != second.key || len(first.commits) != 2 || len(second.commits) != 2 {
		t.Fatalf("canonical contributions differ: %+v %+v", first, second)
	}
	otherProvider, err := canonicalContribution(4, 7, []map[string]any{{"repo_config_id": 1, "commit_sha": "aaa"}, {"repo_config_id": 2, "commit_sha": "bbb"}}, claim)
	if err != nil || otherProvider.key == first.key {
		t.Fatalf("provider identity not isolated: first=%+v other=%+v err=%v", first, otherProvider, err)
	}
}

func TestMaterializeGroupMigratesDirectToSharedWithoutRecounting(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fixture.client.AttributionClaimGroup.UpdateOneID(fixture.groupID).SetEvidenceDigest("evidence-2").SetCommitAllocations([]map[string]any{
		{"repo_config_id": fixture.repo2, "commit_sha": "commit-b"},
		{"repo_config_id": fixture.repo1, "commit_sha": "commit-a"},
	}).ExecX(ctx)
	tx, err := fixture.client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := MaterializeGroup(ctx, tx.Client(), fixture.groupID, fixture.now.Add(time.Minute)); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	pools := fixture.client.AttributionUsagePool.Query().AllX(ctx)
	if len(pools) != 1 || pools[0].TotalTokens != 19 || pools[0].RequestCount != 1 {
		t.Fatalf("migrated pools = %+v", pools)
	}
	relations := fixture.client.AttributionUsagePoolCommit.Query().Order(ent.Asc(attributionusagepoolcommit.FieldRepoConfigID)).AllX(ctx)
	if len(relations) != 2 || relations[0].RelationKind != attributionusagepoolcommit.RelationKindShared || relations[1].RelationKind != attributionusagepoolcommit.RelationKindShared {
		t.Fatalf("shared relations = %+v", relations)
	}
}

func TestMaterializeRequestClaimsAggregateAcrossRequestsAndSplitModelBucket(t *testing.T) {
	fixture := newPoolFixture(t)
	ctx := context.Background()
	first := fixture.client.AttributionRequestClaim.GetX(ctx, fixture.claimID)
	secondUsageAt := fixture.now.Add(2 * time.Minute)
	second := fixture.client.AttributionRequestClaim.Create().SetClaimGroupID(fixture.groupID).SetRelayProviderID(first.RelayProviderID).SetRequestID("req-2").
		SetCanonicalDigest("digest-2").SetStatus(attributionrequestclaim.StatusReconciled).SetRequestedModel("gpt-test").SetUsageAt(secondUsageAt).
		SetInputTokens(5).SetOutputTokens(1).SetTotalTokens(6).SetNextAttemptAt(fixture.now).SetExpiresAt(first.ExpiresAt).SaveX(ctx)
	thirdUsageAt := fixture.now.Add(16 * time.Minute)
	third := fixture.client.AttributionRequestClaim.Create().SetClaimGroupID(fixture.groupID).SetRelayProviderID(first.RelayProviderID).SetRequestID("req-3").
		SetCanonicalDigest("digest-3").SetStatus(attributionrequestclaim.StatusReconciled).SetRequestedModel("gpt-other").SetUsageAt(thirdUsageAt).
		SetInputTokens(7).SetTotalTokens(7).SetNextAttemptAt(fixture.now).SetExpiresAt(first.ExpiresAt).SaveX(ctx)
	for _, id := range []int{first.ID, second.ID, third.ID} {
		if err := materializeInTransaction(t, fixture, id); err != nil {
			t.Fatal(err)
		}
	}
	pools := fixture.client.AttributionUsagePool.Query().AllX(ctx)
	if len(pools) != 2 {
		t.Fatalf("pools = %d, want 2", len(pools))
	}
	var requestCount, total int
	var tokenTotal int64
	for _, pool := range pools {
		requestCount += pool.RequestCount
		total++
		tokenTotal += pool.TotalTokens
	}
	if total != 2 || requestCount != 3 || tokenTotal != 32 {
		t.Fatalf("pool conservation: pools=%d requests=%d Token=%d", total, requestCount, tokenTotal)
	}
}

func TestConcurrentMaterializationCountsEachClaimOnce(t *testing.T) {
	fixture := newPoolFixture(t)
	const workers = 6
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs <- materializeInTransaction(t, fixture, fixture.claimID)
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !strings.Contains(err.Error(), "materialization raced") && !ent.IsConstraintError(err) {
			t.Fatal(err)
		}
	}
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	pools := fixture.client.AttributionUsagePool.Query().AllX(context.Background())
	if len(pools) != 1 || pools[0].RequestCount != 1 || pools[0].TotalTokens != 19 {
		t.Fatalf("concurrent pool = %+v", pools)
	}
}

func TestLongLivedPoolSurvivesHotClaimCleanupWithoutRequestIdentity(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fixture.client.AttributionRequestClaim.DeleteOneID(fixture.claimID).ExecX(ctx)
	fixture.client.AttributionClaimGroup.DeleteOneID(fixture.groupID).ExecX(ctx)
	pools := fixture.client.AttributionUsagePool.Query().AllX(ctx)
	if len(pools) != 1 || pools[0].RequestCount != 1 || pools[0].TotalTokens != 19 {
		t.Fatalf("pool after hot cleanup = %+v", pools)
	}
	if relations := fixture.client.AttributionUsagePoolCommit.Query().AllX(ctx); len(relations) != 1 {
		t.Fatalf("relations after hot cleanup = %+v", relations)
	}
}

func TestApplyRewriteMigratesHotAllocationWithoutRecounting(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	fixture.client.AttributionUsagePool.Update().AddCoverageGapCount(2).ExecX(context.Background())
	applyRewriteInTransaction(t, fixture, "commit-a", "commit-b")
	ctx := context.Background()
	group := fixture.client.AttributionClaimGroup.GetX(ctx, fixture.groupID)
	if got := group.CommitAllocations[0]["commit_sha"]; got != "commit-b" {
		t.Fatalf("hot allocation commit = %v, want commit-b", got)
	}
	assertSingleRewrittenPool(t, fixture, "commit-b")
	if gaps := fixture.client.AttributionUsagePool.Query().OnlyX(ctx).CoverageGapCount; gaps != 2 {
		t.Fatalf("rewritten coverage gaps = %d, want 2", gaps)
	}
}

func TestApplyRewriteMigratesPostRetentionPoolWithoutRequestRows(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	fixture.client.AttributionRequestClaim.DeleteOneID(fixture.claimID).ExecX(ctx)
	fixture.client.AttributionClaimGroup.DeleteOneID(fixture.groupID).ExecX(ctx)
	applyRewriteInTransaction(t, fixture, "commit-a", "commit-b")
	assertSingleRewrittenPool(t, fixture, "commit-b")
}

func TestApplyRewriteResolvesOutOfOrderChainToTerminalCommit(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for index, mapping := range [][2]string{{"commit-b", "commit-c"}, {"commit-a", "commit-b"}} {
		fixture.client.CommitRewrite.Create().SetEventID(fmt.Sprintf("rewrite-chain-%d", index)).SetUserID(fixture.userID).
			SetWorkspaceID("workspace-chain").SetRepoConfigID(fixture.repo1).SetRewriteType("rebase").
			SetOldCommitSha(mapping[0]).SetNewCommitSha(mapping[1]).SetBindingSource("unbound").SaveX(ctx)
	}
	applyRewriteInTransaction(t, fixture, "commit-a", "commit-b")
	assertSingleRewrittenPool(t, fixture, "commit-c")
}

func applyRewriteInTransaction(t *testing.T, fixture poolFixture, oldCommitSHA, newCommitSHA string) {
	t.Helper()
	tx, err := fixture.client.Tx(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := ApplyRewrite(context.Background(), tx.Client(), fixture.userID, fixture.repo1, oldCommitSHA, newCommitSHA, fixture.now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

func assertSingleRewrittenPool(t *testing.T, fixture poolFixture, commitSHA string) {
	t.Helper()
	ctx := context.Background()
	pools := fixture.client.AttributionUsagePool.Query().AllX(ctx)
	if len(pools) != 1 || pools[0].TotalTokens != 19 || pools[0].RequestCount != 1 {
		t.Fatalf("rewritten pools = %+v", pools)
	}
	relations := fixture.client.AttributionUsagePoolCommit.Query().AllX(ctx)
	if len(relations) != 1 || relations[0].CommitSha != commitSHA || relations[0].RelationKind != attributionusagepoolcommit.RelationKindDirect {
		t.Fatalf("rewritten relations = %+v", relations)
	}
}

func TestApplyCherryPickAddsInheritedRelationWithoutRecounting(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tx, err := fixture.client.Tx(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := ApplyCherryPick(ctx, tx.Client(), fixture.userID, fixture.repo1, "commit-a", "commit-cherry", "patch-a", "patch-a"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	pools := fixture.client.AttributionUsagePool.Query().AllX(ctx)
	if len(pools) != 1 || pools[0].TotalTokens != 19 || pools[0].RequestCount != 1 {
		t.Fatalf("pools after cherry-pick = %+v", pools)
	}
	relations := fixture.client.AttributionUsagePoolCommit.Query().Order(ent.Asc(attributionusagepoolcommit.FieldCommitSha)).AllX(ctx)
	if len(relations) != 2 || relations[0].RelationKind != attributionusagepoolcommit.RelationKindDirect || relations[1].CommitSha != "commit-cherry" || relations[1].RelationKind != attributionusagepoolcommit.RelationKindInheritedNonCounting {
		t.Fatalf("cherry-pick relations = %+v", relations)
	}
	if err := ApplyCherryPick(ctx, fixture.client, fixture.userID, fixture.repo1, "commit-a", "commit-other", "patch-a", "patch-b"); err == nil {
		t.Fatal("mismatched patch evidence was accepted")
	}
}

func TestApplyCherryPickResolvesEarlierSourceAndTargetRewrites(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for index, mapping := range [][2]string{{"commit-a", "commit-b"}, {"commit-target-old", "commit-target-new"}} {
		fixture.client.CommitRewrite.Create().SetEventID(fmt.Sprintf("rewrite-before-cherry-%d", index)).SetUserID(fixture.userID).
			SetWorkspaceID("workspace-cherry").SetRepoConfigID(fixture.repo1).SetRewriteType("rebase").
			SetOldCommitSha(mapping[0]).SetNewCommitSha(mapping[1]).SetBindingSource("unbound").SaveX(ctx)
	}
	applyRewriteInTransaction(t, fixture, "commit-a", "commit-b")
	if err := ApplyCherryPick(ctx, fixture.client, fixture.userID, fixture.repo1, "commit-a", "commit-target-old", "patch-a", "patch-a"); err != nil {
		t.Fatal(err)
	}
	relations := fixture.client.AttributionUsagePoolCommit.Query().Order(ent.Asc(attributionusagepoolcommit.FieldCommitSha)).AllX(ctx)
	if len(relations) != 2 || relations[0].CommitSha != "commit-b" || relations[1].CommitSha != "commit-target-new" || relations[1].RelationKind != attributionusagepoolcommit.RelationKindInheritedNonCounting {
		t.Fatalf("resolved cherry-pick relations = %+v", relations)
	}
}

func TestMaterializationFindsEarlierCherryPickEvidence(t *testing.T) {
	fixture := newPoolFixture(t)
	ctx := context.Background()
	fixture.client.CommitCheckpoint.Create().SetEventID("checkpoint-cherry").SetUserID(fixture.userID).
		SetWorkspaceID("workspace-cherry").SetRepoConfigID(fixture.repo1).SetCommitSha("commit-cherry").
		SetParentShas([]string{"parent"}).SetLineageKind(commitcheckpoint.LineageKindCherryPick).
		SetSourceCommitSha("commit-a").SetCommitPatchID("patch-a").SetSourcePatchID("patch-a").
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).SaveX(ctx)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	relations := fixture.client.AttributionUsagePoolCommit.Query().AllX(ctx)
	if len(relations) != 2 {
		t.Fatalf("relations after late source materialization = %+v", relations)
	}
}

func TestMaterializationResolvesCherryPickSourceAndTargetRewrites(t *testing.T) {
	fixture := newPoolFixture(t)
	ctx := context.Background()
	fixture.client.CommitCheckpoint.Create().SetEventID("checkpoint-cherry-rewritten").SetUserID(fixture.userID).
		SetWorkspaceID("workspace-cherry").SetRepoConfigID(fixture.repo1).SetCommitSha("commit-target-old").
		SetParentShas([]string{"parent"}).SetLineageKind(commitcheckpoint.LineageKindCherryPick).
		SetSourceCommitSha("commit-a").SetCommitPatchID("patch-a").SetSourcePatchID("patch-a").
		SetBindingSource(commitcheckpoint.BindingSourceUnbound).SaveX(ctx)
	for index, mapping := range [][2]string{{"commit-a", "commit-b"}, {"commit-target-old", "commit-target-new"}} {
		fixture.client.CommitRewrite.Create().SetEventID(fmt.Sprintf("rewrite-cherry-%d", index)).SetUserID(fixture.userID).
			SetWorkspaceID("workspace-cherry").SetRepoConfigID(fixture.repo1).SetRewriteType("rebase").
			SetOldCommitSha(mapping[0]).SetNewCommitSha(mapping[1]).SetBindingSource("unbound").SaveX(ctx)
	}
	fixture.client.AttributionClaimGroup.UpdateOneID(fixture.groupID).
		SetCommitAllocations([]map[string]any{{"repo_config_id": fixture.repo1, "commit_sha": "commit-b"}}).ExecX(ctx)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	relations := fixture.client.AttributionUsagePoolCommit.Query().Order(ent.Asc(attributionusagepoolcommit.FieldCommitSha)).AllX(ctx)
	if len(relations) != 2 || relations[0].CommitSha != "commit-b" || relations[0].RelationKind != attributionusagepoolcommit.RelationKindDirect || relations[1].CommitSha != "commit-target-new" || relations[1].RelationKind != attributionusagepoolcommit.RelationKindInheritedNonCounting {
		t.Fatalf("rewritten cherry-pick relations = %+v", relations)
	}
}

func TestMarkCommitOrphanedPreservesTokenAndRequiresAuthority(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := MarkCommitOrphaned(ctx, fixture.client, fixture.userID, fixture.repo1, "commit-a", "branch_deleted"); err == nil {
		t.Fatal("non-authoritative orphan evidence was accepted")
	}
	if err := MarkCommitOrphaned(ctx, fixture.client, fixture.userID, fixture.repo1, "commit-a", "authoritative_scm"); err != nil {
		t.Fatal(err)
	}
	pool := fixture.client.AttributionUsagePool.Query().OnlyX(ctx)
	relation := fixture.client.AttributionUsagePoolCommit.Query().OnlyX(ctx)
	if !relation.Orphaned || pool.TotalTokens != 19 || pool.RequestCount != 1 {
		t.Fatalf("orphan relation/pool = %+v / %+v", relation, pool)
	}
}

func TestPRProjectionsKeepOneDistinctPoolScopeTotal(t *testing.T) {
	fixture := newPoolFixture(t)
	if err := materializeInTransaction(t, fixture, fixture.claimID); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := ApplyCherryPick(ctx, fixture.client, fixture.userID, fixture.repo1, "commit-a", "commit-backport", "patch-a", "patch-a"); err != nil {
		t.Fatal(err)
	}
	commits := []string{"commit-a", "commit-a", "commit-backport"}
	for index, commitSHA := range commits {
		pr := fixture.client.PrRecord.Create().SetRepoConfigID(fixture.repo1).SetScmPrID(index + 1).
			SetTitle([]string{"stacked base", "stacked child", "backport"}[index]).SaveX(ctx)
		fixture.client.PRCommitUsageSnapshot.Create().SetPrRecordID(pr.ID).SetCommitSha(commitSHA).SetSortOrder(1).SaveX(ctx)
	}
	projected := fixture.client.AttributionUsagePoolCommit.Query().Where(
		attributionusagepoolcommit.RepoConfigIDEQ(fixture.repo1), attributionusagepoolcommit.CommitShaIn(commits...),
	).AllX(ctx)
	distinct := map[int]struct{}{}
	for _, relation := range projected {
		distinct[relation.PoolID] = struct{}{}
	}
	pool := fixture.client.AttributionUsagePool.Query().OnlyX(ctx)
	if len(projected) != 2 || len(distinct) != 1 || pool.TotalTokens != 19 {
		t.Fatalf("PR projections=%+v distinct pools=%d scope Token=%d, want 2/1/19", projected, len(distinct), pool.TotalTokens)
	}
}
