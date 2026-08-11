package activity

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionusagebucket"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/internal/testdb"
	_ "github.com/lib/pq"
)

func TestCommitSHAProjectionUsesDedicatedLookupIndex(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	repo := client.RepoConfig.Create().SetName("repo-plan").SetFullName("acme/repo-plan").SetCloneURL("https://example.com/acme/repo-plan.git").SaveX(ctx)
	pr := client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(9001).SetTitle("Query plan PR").SaveX(ctx)
	builders := make([]*ent.PRCommitUsageSnapshotCreate, 0, 2000)
	for index := 0; index < 2000; index++ {
		builders = append(builders, client.PRCommitUsageSnapshot.Create().SetPrRecordID(pr.ID).SetCommitSha(fmt.Sprintf("sha-%04d", index)))
	}
	if _, err := client.PRCommitUsageSnapshot.CreateBulk(builders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "ANALYZE pr_commit_usage_snapshots"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, "EXPLAIN (COSTS OFF) SELECT pr_record_id, commit_sha FROM pr_commit_usage_snapshots WHERE commit_sha = $1", "sha-1999")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	plan := strings.Join(lines, "\n")
	if strings.Contains(plan, "Seq Scan on pr_commit_usage_snapshots") || !strings.Contains(plan, "Index") {
		t.Fatalf("commit SHA lookup plan did not use an index:\n%s", plan)
	}
}

func TestV2RepositoryPageUsesPoolRangeAndCommitLookupIndexes(t *testing.T) {
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("plan-user").SetEmail("plan-user@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("plan-provider").SetDisplayName("Plan Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("encrypted-test-key").SetDefaultModel("example-model").SetIsPrimary(true).SetEnabled(true).SaveX(ctx)
	repo := client.RepoConfig.Create().SetName("plan").SetFullName("example/plan").SetCloneURL("https://example.com/example/plan.git").SaveX(ctx)
	poolBuilders := make([]*ent.AttributionUsagePoolCreate, 0, 2000)
	for index := 0; index < 2000; index++ {
		epoch := "other"
		if index == 1999 {
			epoch = "formal_v2"
		}
		poolBuilders = append(poolBuilders, client.AttributionUsagePool.Create().SetCanonicalPoolKey(fmt.Sprintf("plan-%04d", index)).SetLedgerEpoch(epoch).SetRelayProviderID(provider.ID).SetUserID(user.ID).SetRequestedModel("model-test").SetBucketStartUtc(time.Date(2026, 8, 1, 0, 0, index, 0, time.UTC)).SetTotalTokens(10))
	}
	pools, err := client.AttributionUsagePool.CreateBulk(poolBuilders...).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	commitBuilders := make([]*ent.AttributionUsagePoolCommitCreate, 0, len(pools))
	for index, pool := range pools {
		commitBuilders = append(commitBuilders, client.AttributionUsagePoolCommit.Create().SetPoolID(pool.ID).SetRepoConfigID(repo.ID).SetCommitSha(fmt.Sprintf("plan-sha-%04d", index)).SetRelationKind(attributionusagepoolcommit.RelationKindDirect))
	}
	if _, err := client.AttributionUsagePoolCommit.CreateBulk(commitBuilders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "ANALYZE attribution_usage_pools; ANALYZE attribution_usage_pool_commits"); err != nil {
		t.Fatal(err)
	}
	rows, err := db.QueryContext(ctx, `EXPLAIN (COSTS OFF) SELECT c.repo_config_id,p.id FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id WHERE p.ledger_epoch='formal_v2' AND p.relay_provider_id>0 AND p.user_id=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3`, user.ID, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, line)
	}
	plan := strings.Join(lines, "\n")
	if strings.Contains(plan, "Seq Scan on attribution_usage_pools") || (!strings.Contains(plan, "attributionusagepool_ledger_epoch_user_id_bucket_start_utc") && !strings.Contains(plan, "attributionusagepool_ledger_epoch_bucket_start_utc")) {
		t.Fatalf("v2 pool plan did not use range index:\n%s", plan)
	}
	if strings.Contains(plan, "Seq Scan on attribution_usage_pool_commits") || !strings.Contains(plan, "attributionusagepoolcommit_pool_id_repo_config_id_commit_sha") {
		t.Fatalf("v2 commit plan did not use pool lookup index:\n%s", plan)
	}
}

func TestV2ReadPathsStayWithinScaleLatencyBudget(t *testing.T) {
	const (
		poolCount       = 2500
		repositoryCount = 25
		maxReadLatency  = 2 * time.Second
	)
	client, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("scale-v2-user").SetEmail("scale-v2-user@example.com").SetAuthSource("ldap").SaveX(ctx)
	provider := client.RelayProvider.Create().SetName("scale-v2-provider").SetDisplayName("Scale V2 Provider").SetBaseURL("https://relay.example.com").SetAdminAPIKey("test-key").SetEnabled(true).SaveX(ctx)
	repositories := make([]*ent.RepoConfig, 0, repositoryCount)
	pullRequests := make([]*ent.PrRecord, 0, repositoryCount)
	for index := 0; index < repositoryCount; index++ {
		repo := client.RepoConfig.Create().SetName(fmt.Sprintf("repo-%02d", index)).SetFullName(fmt.Sprintf("example/repo-%02d", index)).SetCloneURL(fmt.Sprintf("https://example.com/repo-%02d.git", index)).SaveX(ctx)
		repositories = append(repositories, repo)
		pullRequests = append(pullRequests, client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(index+1).SetTitle(fmt.Sprintf("Scale PR %02d", index)).SetScmPrURL(fmt.Sprintf("https://example.com/pr/%d", index+1)).SaveX(ctx))
	}
	poolBuilders := make([]*ent.AttributionUsagePoolCreate, 0, poolCount)
	for index := 0; index < poolCount; index++ {
		poolBuilders = append(poolBuilders, client.AttributionUsagePool.Create().SetCanonicalPoolKey(fmt.Sprintf("scale-v2-%04d", index)).SetLedgerEpoch("formal_v2").
			SetRelayProviderID(provider.ID).SetUserID(user.ID).SetRequestedModel("model-test").SetBucketStartUtc(time.Date(2026, 8, 1, 0, index%60, 0, 0, time.UTC)).SetTotalTokens(1))
	}
	pools, err := client.AttributionUsagePool.CreateBulk(poolBuilders...).Save(ctx)
	if err != nil {
		t.Fatal(err)
	}
	commitBuilders := make([]*ent.AttributionUsagePoolCommitCreate, 0, poolCount)
	prCommitBuilders := make([]*ent.PRCommitUsageSnapshotCreate, 0, poolCount)
	for index, pool := range pools {
		repositoryIndex := index % repositoryCount
		commitSHA := fmt.Sprintf("scale-v2-sha-%04d", index)
		commitBuilders = append(commitBuilders, client.AttributionUsagePoolCommit.Create().SetPoolID(pool.ID).SetRepoConfigID(repositories[repositoryIndex].ID).SetCommitSha(commitSHA).SetRelationKind(attributionusagepoolcommit.RelationKindDirect))
		prCommitBuilders = append(prCommitBuilders, client.PRCommitUsageSnapshot.Create().SetPrRecordID(pullRequests[repositoryIndex].ID).SetCommitSha(commitSHA))
	}
	if _, err := client.AttributionUsagePoolCommit.CreateBulk(commitBuilders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.PRCommitUsageSnapshot.CreateBulk(prCommitBuilders...).Save(ctx); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, "ANALYZE attribution_usage_pools; ANALYZE attribution_usage_pool_commits; ANALYZE pr_commit_usage_snapshots; ANALYZE pr_records"); err != nil {
		t.Fatal(err)
	}
	service := NewService(client, nil, ServiceOptions{
		CursorSecret: "scale-secret", V2LedgerEpoch: "formal_v2", V2DB: db,
		V2Denominator: fixedV2Denominator{V2Denominator{TotalTokens: poolCount * 2, AsOf: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), Fresh: true, Complete: true}},
	})
	query := V2Query{Scope: V2ScopePersonal, FromDate: "2026-08-01", ToDate: "2026-08-01", Timezone: "UTC"}
	withinBudget := func(name string, call func() error) {
		t.Helper()
		started := time.Now()
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if elapsed := time.Since(started); elapsed > maxReadLatency {
			t.Fatalf("%s latency %s exceeds %s budget at %d pools", name, elapsed, maxReadLatency, poolCount)
		}
	}
	withinBudget("summary and daily trend", func() error {
		result, err := service.V2Overview(ctx, user.ID, query)
		if err == nil && result.CommittedTokens != poolCount {
			return fmt.Errorf("committed tokens = %d, want %d", result.CommittedTokens, poolCount)
		}
		return err
	})
	var repositoryPage *V2Page[V2RepositoryRow]
	withinBudget("repository ranking", func() error {
		var err error
		repositoryPage, err = service.V2Repositories(ctx, user.ID, V2PageQuery{V2Query: query})
		return err
	})
	withinBudget("repository search and name sort", func() error {
		_, err := service.V2Repositories(ctx, user.ID, V2PageQuery{V2Query: query, Search: "repo-1", Sort: "name"})
		return err
	})
	withinBudget("repository cursor page", func() error {
		_, err := service.V2Repositories(ctx, user.ID, V2PageQuery{V2Query: query, Cursor: repositoryPage.NextCursor})
		return err
	})
	var pullRequestPage *V2Page[V2PullRequestRow]
	withinBudget("pull-request ranking", func() error {
		var err error
		pullRequestPage, err = service.V2PullRequests(ctx, user.ID, V2PageQuery{V2Query: query})
		return err
	})
	withinBudget("pull-request search and name sort", func() error {
		_, err := service.V2PullRequests(ctx, user.ID, V2PageQuery{V2Query: query, Search: "Scale PR 1", Sort: "name"})
		return err
	})
	withinBudget("pull-request cursor page", func() error {
		_, err := service.V2PullRequests(ctx, user.ID, V2PageQuery{V2Query: query, Cursor: pullRequestPage.NextCursor})
		return err
	})
}

func TestTeamProjectionQueryCountDoesNotGrowWithMemberCount(t *testing.T) {
	seed, dsn := testdb.OpenWithDSN(t)
	ctx := context.Background()
	representative := seed.User.Create().SetUsername("representative").SetEmail("representative@example.com").SetAuthSource("ldap").SaveX(ctx)
	source := seed.DirectorySource.Create().SetName("company").SetEnabled(true).SetDsl("version: 1").SaveX(ctx)
	run := seed.DirectorySyncRun.Create().SetSourceID(source.ID).SetMode("apply").SetStatus("completed").SetPhase("completed").SetCompletedAt(time.Now().UTC()).SaveX(ctx)
	seed.DirectorySource.UpdateOne(source).SetLastSuccessfulRunID(run.ID).SetLastRunID(run.ID).ExecX(ctx)
	seed.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("team-scale").SetName("Scale Team").SetPath("Scale Team").SetLastSeenRunID(run.ID).
		SetMetadata(map[string]any{"representative_external_ids": []string{"member-representative"}}).SaveX(ctx)
	representativeMember := seed.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID("member-representative").SetEmailNormalized(representative.Email).
		SetDisplayName(representative.Username).SetDepartmentExternalID("team-scale").SetStatus("active").SetMatchedUserID(representative.ID).SetLastSeenRunID(run.ID).SaveX(ctx)
	seed.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(representativeMember.ID).SetMemberExternalID(representativeMember.ExternalID).
		SetMemberEmailNormalized(representative.Email).SetDepartmentExternalID("team-scale").SetLastSeenRunID(run.ID).SaveX(ctx)
	repo := seed.RepoConfig.Create().SetName("repo-scale").SetFullName("acme/repo-scale").SetCloneURL("https://example.com/acme/repo-scale.git").SaveX(ctx)
	observedAt := time.Now().UTC().Add(-time.Hour)
	for index := 0; index < 40; index++ {
		member := seed.User.Create().SetUsername(fmt.Sprintf("member-%02d", index)).SetEmail(fmt.Sprintf("member-%02d@example.com", index)).SetAuthSource("ldap").SaveX(ctx)
		directoryMember := seed.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID(fmt.Sprintf("member-%02d", index)).SetEmailNormalized(member.Email).
			SetDisplayName(member.Username).SetDepartmentExternalID("team-scale").SetStatus("active").SetMatchedUserID(member.ID).SetLastSeenRunID(run.ID).SaveX(ctx)
		seed.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(directoryMember.ID).SetMemberExternalID(directoryMember.ExternalID).
			SetMemberEmailNormalized(member.Email).SetDepartmentExternalID("team-scale").SetLastSeenRunID(run.ID).SaveX(ctx)
		installation := seed.ReportingInstallation.Create().SetInstallationID(fmt.Sprintf("scale-installation-%02d", index)).SetUserID(member.ID).
			SetReporterTokenHash(fmt.Sprintf("scale-reporter-%02d", index)).SetOtlpTokenHash(fmt.Sprintf("scale-otlp-%02d", index)).SaveX(ctx)
		createActivityBucket(t, seed, installation.ID, member.ID, fmt.Sprintf("scale-bucket-%02d", index), observedAt.Add(time.Duration(index)*time.Second), attributionusagebucket.TokenQualityMeasured, 0, testAllocations(repo.ID, "scale-shared-commit", "bound_auto"))
	}
	pr := seed.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(9100).SetTitle("Scale shared PR").SetStatus(prrecord.StatusMerged).SetMergedAt(observedAt.Add(time.Hour)).SaveX(ctx)
	seed.PRCommitUsageSnapshot.Create().SetPrRecordID(pr.ID).SetCommitSha("scale-shared-commit").SaveX(ctx)
	seed.PRSyncJob.Create().SetRepoConfigID(repo.ID).SetStatus("completed").SetPhase("completed").SetCompletedAt(time.Now().UTC()).SaveX(ctx)

	queryCount := 0
	var debugLines []string
	client, err := ent.Open("postgres", dsn, ent.Debug(), ent.Log(func(args ...any) {
		queryCount++
		debugLines = append(debugLines, fmt.Sprint(args...))
	}))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	service := NewService(client, nil, ServiceOptions{})
	result, err := service.Team(ctx, representative.ID, "team-scale", Window{From: observedAt.Add(-time.Hour), To: time.Now().UTC().Add(time.Hour)}, PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ActiveMembers != 41 || result.Metrics.ParticipatingPRs.Value != 1 {
		t.Fatalf("scale result members=%d metrics=%+v", result.ActiveMembers, result.Metrics)
	}
	if queryCount > 24 {
		t.Fatalf("team projection issued %d SQL operations for 41 members, want bounded set queries:\n%s", queryCount, strings.Join(debugLines, "\n"))
	}
}
