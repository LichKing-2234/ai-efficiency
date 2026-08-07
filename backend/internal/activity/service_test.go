package activity

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionusagebucket"
	"github.com/ai-efficiency/backend/ent/prrecord"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestMemberActivityUsesLatestAllocationAndDeduplicatesPRs(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("alice").SetEmail("alice@example.com").SetAuthSource("ldap").SaveX(ctx)
	installation := client.ReportingInstallation.Create().
		SetInstallationID("11111111-1111-4111-8111-111111111111").
		SetUserID(user.ID).
		SetReporterTokenHash("reporter-hash-a").
		SetOtlpTokenHash("otlp-hash-a").
		SaveX(ctx)
	repo := client.RepoConfig.Create().
		SetName("repo-a").SetFullName("acme/repo-a").SetCloneURL("https://example.com/acme/repo-a.git").
		SaveX(ctx)
	observedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	bucket := client.AttributionUsageBucket.Create().
		SetBucketID("bucket-rewritten").SetReportingInstallationID(installation.ID).SetUserID(user.ID).
		SetTool("codex").SetSessionSlices([]map[string]any{}).SetObservedStartAt(observedAt.Add(-time.Minute)).SetObservedEndAt(observedAt).
		SetFreshInputTokens(70).SetCacheReadTokens(20).SetOutputTokens(10).SetProcessedTotalTokens(100).
		SetSourceDigest("source-a").SetImmutableDigest("immutable-a").SetExtractorVersion("test").
		SetTokenQuality(attributionusagebucket.TokenQualityMeasured).
		SaveX(ctx)
	client.AttributionAllocationRevision.Create().
		SetRevisionID("revision-old").SetUsageBucketID(bucket.ID).SetSequence(1).SetReason("initial").SetEvidenceVersion("test").
		SetAllocations(testAllocations(repo.ID, "commit-old", "bound_auto")).SetRestatedAt(observedAt).
		SaveX(ctx)
	client.AttributionAllocationRevision.Create().
		SetRevisionID("revision-current").SetUsageBucketID(bucket.ID).SetSequence(2).SetReason("rewrite").SetEvidenceVersion("test").
		SetAllocations(testAllocations(repo.ID, "commit-current", "bound_auto")).SetRestatedAt(observedAt.Add(time.Minute)).
		SaveX(ctx)

	mergedAt := observedAt.Add(2 * time.Hour)
	mergedPR := client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(101).SetTitle("Merged PR").SetStatus(prrecord.StatusMerged).SetMergedAt(mergedAt).SetCreatedAt(observedAt.Add(-time.Hour)).SaveX(ctx)
	openPR := client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(102).SetTitle("Open PR").SetStatus(prrecord.StatusOpen).SetCreatedAt(observedAt.Add(-30 * time.Minute)).SaveX(ctx)
	oldPR := client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(103).SetTitle("Old revision PR").SetStatus(prrecord.StatusMerged).SetMergedAt(mergedAt).SaveX(ctx)
	for index, item := range []struct {
		pr  *ent.PrRecord
		sha string
	}{{mergedPR, "commit-current"}, {openPR, "commit-current"}, {oldPR, "commit-old"}} {
		client.PRCommitUsageSnapshot.Create().SetPrRecordID(item.pr.ID).SetCommitSha(item.sha).SetSortOrder(index).SaveX(ctx)
	}
	client.PRSyncJob.Create().SetRepoConfigID(repo.ID).SetStatus("completed").SetPhase("completed").SetCompletedAt(observedAt.Add(3 * time.Hour)).SaveX(ctx)

	service := NewService(client, nil, ServiceOptions{})
	service.now = func() time.Time { return observedAt.Add(4 * time.Hour) }
	result, err := service.Member(ctx, user.ID, user.ID, Window{From: observedAt.Add(-24 * time.Hour), To: observedAt.Add(24 * time.Hour)}, DetailPageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics.ParticipatingPRs.Value != 2 || result.Metrics.ParticipatingPRs.LowerBound {
		t.Fatalf("participating PRs = %+v, want exact 2", result.Metrics.ParticipatingPRs)
	}
	if result.Metrics.MergedPRs.Value != 1 || result.Metrics.CommitCount != 1 || result.Metrics.ActiveRepositories != 1 {
		t.Fatalf("metrics = %+v", result.Metrics)
	}
	if len(result.PRs.Items) != 2 || len(result.Commits.Items) != 1 || len(result.Commits.Items[0].PRs) != 2 {
		t.Fatalf("PRs=%+v commits=%+v", result.PRs.Items, result.Commits.Items)
	}
	if result.Commits.Items[0].CommitSHA != "commit-current" || result.Commits.Items[0].ProcessedTokens != 100 {
		t.Fatalf("commit = %+v, want latest-revision commit with non-duplicated Token", result.Commits.Items[0])
	}
}

func TestMemberActivityKeepsRepoSHAIdentityAndQualityPoolsSeparate(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	user := client.User.Create().SetUsername("bob").SetEmail("bob@example.org").SetAuthSource("ldap").SaveX(ctx)
	installation := client.ReportingInstallation.Create().
		SetInstallationID("22222222-2222-4222-8222-222222222222").SetUserID(user.ID).
		SetReporterTokenHash("reporter-hash-b").SetOtlpTokenHash("otlp-hash-b").SaveX(ctx)
	repoA := client.RepoConfig.Create().SetName("repo-a").SetFullName("acme/repo-a").SetCloneURL("https://example.com/acme/repo-a.git").SaveX(ctx)
	repoB := client.RepoConfig.Create().SetName("repo-b").SetFullName("acme/repo-b").SetCloneURL("https://example.com/acme/repo-b.git").SaveX(ctx)
	observedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	createActivityBucket(t, client, installation.ID, user.ID, "direct-a", observedAt, attributionusagebucket.TokenQualityMeasured, 0, testAllocations(repoA.ID, "same-sha", "bound_auto"))
	createActivityBucket(t, client, installation.ID, user.ID, "direct-b", observedAt.Add(time.Minute), attributionusagebucket.TokenQualityMeasured, 0, testAllocations(repoB.ID, "same-sha", "bound_auto"))
	createActivityBucket(t, client, installation.ID, user.ID, "unbound", observedAt.Add(2*time.Minute), attributionusagebucket.TokenQualityMeasured, 0, testAllocations(0, "", "unbound"))
	createActivityBucket(t, client, installation.ID, user.ID, "shared", observedAt.Add(3*time.Minute), attributionusagebucket.TokenQualityMeasured, 0, testAllocations(0, "", "multi_repo_shared"))
	createActivityBucket(t, client, installation.ID, user.ID, "invalid", observedAt.Add(4*time.Minute), attributionusagebucket.TokenQualityInvalid, 2, testAllocations(0, "", "unbound"))

	prA := client.PrRecord.Create().SetRepoConfigID(repoA.ID).SetScmPrID(201).SetTitle("Repo A PR").SaveX(ctx)
	prB := client.PrRecord.Create().SetRepoConfigID(repoB.ID).SetScmPrID(201).SetTitle("Repo B PR").SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().SetPrRecordID(prA.ID).SetCommitSha("same-sha").SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().SetPrRecordID(prB.ID).SetCommitSha("same-sha").SaveX(ctx)

	service := NewService(client, nil, ServiceOptions{})
	service.now = func() time.Time { return observedAt.Add(5 * time.Minute) }
	result, err := service.Member(ctx, user.ID, user.ID, Window{From: observedAt.Add(-time.Hour), To: observedAt.Add(time.Hour)}, DetailPageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Metrics.ActiveRepositories != 2 || result.Metrics.CommitCount != 2 || result.Metrics.ParticipatingPRs.Value != 2 {
		t.Fatalf("metrics = %+v", result.Metrics)
	}
	if !result.Metrics.ParticipatingPRs.LowerBound || result.SyncCoverage.UnsyncedRepositories != 2 || result.SyncCoverage.AffectedRepositories != 2 {
		t.Fatalf("sync coverage = %+v metrics=%+v", result.SyncCoverage, result.Metrics)
	}
	if result.Quality.UnboundBuckets != 1 || result.Quality.MultiRepoSharedBuckets != 1 || result.Quality.InvalidTokenFacts != 1 || result.Quality.CoverageGapCount != 2 {
		t.Fatalf("quality = %+v", result.Quality)
	}
	if len(result.Commits.Items) != 2 || result.Commits.Items[0].RepoConfigID == result.Commits.Items[1].RepoConfigID {
		t.Fatalf("commits = %+v, want identical SHA kept once per repository", result.Commits.Items)
	}
}

func TestMemberActivityRevalidatesRepresentativeAndAdminScope(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	representative := client.User.Create().SetUsername("representative").SetEmail("representative@example.com").SetAuthSource("ldap").SaveX(ctx)
	member := client.User.Create().SetUsername("member").SetEmail("member@example.com").SetAuthSource("ldap").SaveX(ctx)
	ordinary := client.User.Create().SetUsername("ordinary").SetEmail("ordinary@example.com").SetAuthSource("ldap").SaveX(ctx)
	outside := client.User.Create().SetUsername("outside").SetEmail("outside@example.com").SetAuthSource("ldap").SaveX(ctx)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	createActivityDirectoryScope(t, client, representative, member, ordinary, outside, admin)

	service := NewService(client, nil, ServiceOptions{})
	window := Window{From: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 8, 6, 0, 0, 0, 0, time.UTC)}
	representativeView, err := service.Member(ctx, representative.ID, member.ID, window, DetailPageOptions{})
	if err != nil {
		t.Fatalf("representative member view: %v", err)
	}
	if representativeView.BucketAccess || len(representativeView.Buckets.Items) != 0 {
		t.Fatalf("representative received restricted buckets: %+v", representativeView)
	}
	adminView, err := service.Member(ctx, admin.ID, member.ID, window, DetailPageOptions{})
	if err != nil {
		t.Fatalf("admin member view: %v", err)
	}
	if !adminView.BucketAccess {
		t.Fatalf("admin bucket access = false")
	}
	if _, err := service.Member(ctx, representative.ID, outside.ID, window, DetailPageOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("representative outside-scope err = %v, want ErrForbidden", err)
	}
	if _, err := service.Member(ctx, ordinary.ID, member.ID, window, DetailPageOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ordinary cross-member err = %v, want ErrForbidden", err)
	}
}

func TestTeamActivityKeepsDirectoryOnlyMembersAndDeduplicatesSharedPR(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	representative := client.User.Create().SetUsername("representative").SetEmail("representative@example.com").SetAuthSource("ldap").SaveX(ctx)
	member := client.User.Create().SetUsername("member").SetEmail("member@example.com").SetAuthSource("ldap").SaveX(ctx)
	secondMember := client.User.Create().SetUsername("second").SetEmail("second@example.com").SetAuthSource("ldap").SaveX(ctx)
	outside := client.User.Create().SetUsername("outside").SetEmail("outside@example.com").SetAuthSource("ldap").SaveX(ctx)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	createActivityDirectoryScope(t, client, representative, member, secondMember, outside, admin)
	repo := client.RepoConfig.Create().SetName("repo-team").SetFullName("acme/repo-team").SetCloneURL("https://example.com/acme/repo-team.git").SaveX(ctx)
	observedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for index, user := range []*ent.User{member, secondMember} {
		installation := client.ReportingInstallation.Create().
			SetInstallationID(fmt.Sprintf("33333333-3333-4333-8333-%012d", index+1)).SetUserID(user.ID).
			SetReporterTokenHash(fmt.Sprintf("reporter-team-%d", index)).SetOtlpTokenHash(fmt.Sprintf("otlp-team-%d", index)).SaveX(ctx)
		createActivityBucket(t, client, installation.ID, user.ID, fmt.Sprintf("team-bucket-%d", index), observedAt.Add(time.Duration(index)*time.Minute), attributionusagebucket.TokenQualityMeasured, 0, testAllocations(repo.ID, "shared-commit", "bound_auto"))
	}
	pr := client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(301).SetTitle("Shared team PR").SetStatus(prrecord.StatusMerged).SetMergedAt(observedAt.Add(time.Hour)).SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().SetPrRecordID(pr.ID).SetCommitSha("shared-commit").SaveX(ctx)
	client.PRSyncJob.Create().SetRepoConfigID(repo.ID).SetStatus("completed").SetPhase("completed").SetCompletedAt(observedAt.Add(2 * time.Hour)).SaveX(ctx)

	service := NewService(client, nil, ServiceOptions{})
	service.now = func() time.Time { return observedAt.Add(3 * time.Hour) }
	result, err := service.Team(ctx, representative.ID, "team-child", Window{From: observedAt.Add(-time.Hour), To: observedAt.Add(4 * time.Hour)}, PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.ActiveMembers != 3 || len(result.Members.Items) != 3 {
		t.Fatalf("members = %+v active=%d, want two local plus one directory-only", result.Members.Items, result.ActiveMembers)
	}
	if result.Metrics.ParticipatingPRs.Value != 1 || result.Metrics.MergedPRs.Value != 1 || result.Metrics.ActiveRepositories != 1 {
		t.Fatalf("team metrics = %+v, want shared PR counted once", result.Metrics)
	}
	var directoryOnly *MemberRow
	for index := range result.Members.Items {
		row := &result.Members.Items[index]
		if row.Member.DirectoryMemberExternalID == "member-directory-only" {
			directoryOnly = row
		}
	}
	if directoryOnly == nil || directoryOnly.Available || directoryOnly.Member.UserID != 0 {
		t.Fatalf("directory-only member = %+v", directoryOnly)
	}
}

func TestScopeUsesCurrentRoleAndAuthorizedDirectoryTeams(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	representative := client.User.Create().SetUsername("representative").SetEmail("representative@example.com").SetAuthSource("ldap").SaveX(ctx)
	member := client.User.Create().SetUsername("member").SetEmail("member@example.com").SetAuthSource("ldap").SaveX(ctx)
	ordinary := client.User.Create().SetUsername("ordinary").SetEmail("ordinary@example.com").SetAuthSource("ldap").SaveX(ctx)
	outside := client.User.Create().SetUsername("outside").SetEmail("outside@example.com").SetAuthSource("ldap").SaveX(ctx)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	createActivityDirectoryScope(t, client, representative, member, ordinary, outside, admin)

	service := NewService(client, nil, ServiceOptions{})
	representativeScope, err := service.Scope(ctx, representative.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !representativeScope.CanViewTeams || !representativeScope.Representative || representativeScope.Admin || len(representativeScope.Teams) != 2 {
		t.Fatalf("representative scope = %+v, want two-team subtree", representativeScope)
	}
	ordinaryScope, err := service.Scope(ctx, ordinary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if ordinaryScope.CanViewTeams || ordinaryScope.Representative || ordinaryScope.Admin || len(ordinaryScope.Teams) != 0 {
		t.Fatalf("ordinary scope = %+v, want self-only", ordinaryScope)
	}
	adminScope, err := service.Scope(ctx, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !adminScope.CanViewTeams || !adminScope.Admin || len(adminScope.Teams) != 3 {
		t.Fatalf("admin scope = %+v, want all current teams", adminScope)
	}
}

func TestMemberActivityPaginatesPRsCommitsAndBucketsWithIndependentSignedCursors(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	member := client.User.Create().SetUsername("member").SetEmail("member@example.com").SetAuthSource("ldap").SaveX(ctx)
	installation := client.ReportingInstallation.Create().
		SetInstallationID("44444444-4444-4444-8444-444444444444").SetUserID(member.ID).
		SetReporterTokenHash("reporter-pagination").SetOtlpTokenHash("otlp-pagination").SaveX(ctx)
	repo := client.RepoConfig.Create().SetName("repo-page").SetFullName("acme/repo-page").SetCloneURL("https://example.com/acme/repo-page.git").SaveX(ctx)
	observedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for index := 0; index < 2; index++ {
		sha := fmt.Sprintf("page-commit-%d", index)
		createActivityBucket(t, client, installation.ID, member.ID, fmt.Sprintf("page-bucket-%d", index), observedAt.Add(time.Duration(index)*time.Minute), attributionusagebucket.TokenQualityMeasured, 0, testAllocations(repo.ID, sha, "bound_auto"))
		pr := client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(400 + index).SetTitle(fmt.Sprintf("Page PR %d", index)).SaveX(ctx)
		client.PRCommitUsageSnapshot.Create().SetPrRecordID(pr.ID).SetCommitSha(sha).SaveX(ctx)
	}
	client.PRSyncJob.Create().SetRepoConfigID(repo.ID).SetStatus("completed").SetPhase("completed").SetCompletedAt(observedAt.Add(time.Hour)).SaveX(ctx)

	service := NewService(client, nil, ServiceOptions{CursorSecret: "activity-test-cursor-secret"})
	service.now = func() time.Time { return observedAt.Add(2 * time.Hour) }
	window := Window{From: observedAt.Add(-time.Hour), To: observedAt.Add(3 * time.Hour)}
	first, err := service.Member(ctx, member.ID, member.ID, window, DetailPageOptions{PRLimit: 1, CommitLimit: 1, BucketLimit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.PRs.Items) != 1 || len(first.Commits.Items) != 1 || len(first.Buckets.Items) != 1 || first.PRs.NextCursor == "" || first.Commits.NextCursor == "" || first.Buckets.NextCursor == "" {
		t.Fatalf("first pages = PR=%+v commit=%+v bucket=%+v", first.PRs, first.Commits, first.Buckets)
	}
	second, err := service.Member(ctx, member.ID, member.ID, window, DetailPageOptions{
		PRLimit: 1, PRCursor: first.PRs.NextCursor,
		CommitLimit: 1, CommitCursor: first.Commits.NextCursor,
		BucketLimit: 1, BucketCursor: first.Buckets.NextCursor,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.PRs.Items) != 1 || second.PRs.Items[0].PRRecordID == first.PRs.Items[0].PRRecordID || second.PRs.NextCursor != "" {
		t.Fatalf("second PR page = %+v", second.PRs)
	}
	if len(second.Commits.Items) != 1 || second.Commits.Items[0].CommitSHA == first.Commits.Items[0].CommitSHA || second.Commits.NextCursor != "" {
		t.Fatalf("second commit page = %+v", second.Commits)
	}
	if len(second.Buckets.Items) != 1 || second.Buckets.Items[0].BucketID == first.Buckets.Items[0].BucketID || second.Buckets.NextCursor != "" {
		t.Fatalf("second bucket page = %+v", second.Buckets)
	}
	_, err = service.Member(ctx, member.ID, member.ID, window, DetailPageOptions{PRLimit: 1, PRCursor: first.PRs.NextCursor + "tampered"})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("tampered cursor err = %v, want ErrInvalidCursor", err)
	}
}

func TestTeamMemberCursorExpiresWhenDirectorySnapshotChanges(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	representative := client.User.Create().SetUsername("representative").SetEmail("representative@example.com").SetAuthSource("ldap").SaveX(ctx)
	member := client.User.Create().SetUsername("member").SetEmail("member@example.com").SetAuthSource("ldap").SaveX(ctx)
	secondMember := client.User.Create().SetUsername("second").SetEmail("second@example.com").SetAuthSource("ldap").SaveX(ctx)
	outside := client.User.Create().SetUsername("outside").SetEmail("outside@example.com").SetAuthSource("ldap").SaveX(ctx)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	createActivityDirectoryScope(t, client, representative, member, secondMember, outside, admin)

	service := NewService(client, nil, ServiceOptions{CursorSecret: "activity-test-cursor-secret"})
	first, err := service.Team(ctx, representative.ID, "team-child", Window{}, PageOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Members.Items) != 1 || first.Members.NextCursor == "" {
		t.Fatalf("first team member page = %+v", first.Members)
	}
	source := client.DirectorySource.Query().OnlyX(ctx)
	newRun := client.DirectorySyncRun.Create().SetSourceID(source.ID).SetMode("apply").SetStatus("completed").SetPhase("completed").SetCompletedAt(time.Now().UTC()).SaveX(ctx)
	client.DirectorySource.UpdateOne(source).SetLastSuccessfulRunID(newRun.ID).SetLastRunID(newRun.ID).ExecX(ctx)

	_, err = service.Team(ctx, representative.ID, "team-child", first.Window, PageOptions{Limit: 1, Cursor: first.Members.NextCursor})
	if !errors.Is(err, ErrSnapshotExpired) {
		t.Fatalf("stale team cursor err = %v, want ErrSnapshotExpired", err)
	}
}

func TestMembersListsAuthorizedCurrentMembersAndRejectsOrdinaryEnumeration(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	representative := client.User.Create().SetUsername("representative").SetEmail("representative@example.com").SetAuthSource("ldap").SaveX(ctx)
	member := client.User.Create().SetUsername("member").SetEmail("member@example.com").SetAuthSource("ldap").SaveX(ctx)
	ordinary := client.User.Create().SetUsername("ordinary").SetEmail("ordinary@example.com").SetAuthSource("ldap").SaveX(ctx)
	outside := client.User.Create().SetUsername("outside").SetEmail("outside@example.com").SetAuthSource("ldap").SaveX(ctx)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	createActivityDirectoryScope(t, client, representative, member, ordinary, outside, admin)

	service := NewService(client, nil, ServiceOptions{CursorSecret: "activity-test-cursor-secret"})
	result, err := service.Members(ctx, representative.ID, Window{}, PageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Members.Items) != 5 {
		t.Fatalf("representative members = %+v, want root/child current members plus directory-only", result.Members.Items)
	}
	var unavailable int
	for _, row := range result.Members.Items {
		if !row.Available {
			unavailable++
		}
		if row.Member.UserID == outside.ID {
			t.Fatalf("outside member leaked into representative scope: %+v", row)
		}
	}
	if unavailable != 1 {
		t.Fatalf("unavailable rows = %d, want directory-only row", unavailable)
	}
	if _, err := service.Members(ctx, ordinary.ID, Window{}, PageOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("ordinary enumeration err = %v, want ErrForbidden", err)
	}
}

func TestRepositoryActivityUsesActorScopeAndDeduplicatesSharedMemberWork(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	representative := client.User.Create().SetUsername("representative").SetEmail("representative@example.com").SetAuthSource("ldap").SaveX(ctx)
	member := client.User.Create().SetUsername("member").SetEmail("member@example.com").SetAuthSource("ldap").SaveX(ctx)
	secondMember := client.User.Create().SetUsername("second").SetEmail("second@example.com").SetAuthSource("ldap").SaveX(ctx)
	outside := client.User.Create().SetUsername("outside").SetEmail("outside@example.com").SetAuthSource("ldap").SaveX(ctx)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	createActivityDirectoryScope(t, client, representative, member, secondMember, outside, admin)
	repo := client.RepoConfig.Create().SetName("repo-scope").SetFullName("acme/repo-scope").SetCloneURL("https://example.com/acme/repo-scope.git").SaveX(ctx)
	observedAt := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	for index, subject := range []*ent.User{member, secondMember, outside} {
		installation := client.ReportingInstallation.Create().
			SetInstallationID(fmt.Sprintf("55555555-5555-4555-8555-%012d", index+1)).SetUserID(subject.ID).
			SetReporterTokenHash(fmt.Sprintf("reporter-repo-%d", index)).SetOtlpTokenHash(fmt.Sprintf("otlp-repo-%d", index)).SaveX(ctx)
		createActivityBucket(t, client, installation.ID, subject.ID, fmt.Sprintf("repo-bucket-%d", index), observedAt.Add(time.Duration(index)*time.Minute), attributionusagebucket.TokenQualityMeasured, 0, testAllocations(repo.ID, "shared-repo-commit", "bound_auto"))
	}
	pr := client.PrRecord.Create().SetRepoConfigID(repo.ID).SetScmPrID(501).SetTitle("Shared repository PR").SetStatus(prrecord.StatusMerged).SetMergedAt(observedAt.Add(time.Hour)).SaveX(ctx)
	client.PRCommitUsageSnapshot.Create().SetPrRecordID(pr.ID).SetCommitSha("shared-repo-commit").SaveX(ctx)
	client.PRSyncJob.Create().SetRepoConfigID(repo.ID).SetStatus("completed").SetPhase("completed").SetCompletedAt(observedAt.Add(2 * time.Hour)).SaveX(ctx)

	service := NewService(client, nil, ServiceOptions{CursorSecret: "activity-test-cursor-secret"})
	service.now = func() time.Time { return observedAt.Add(3 * time.Hour) }
	window := Window{From: observedAt.Add(-time.Hour), To: observedAt.Add(4 * time.Hour)}
	representativeView, err := service.Repository(ctx, representative.ID, repo.ID, window, RepositoryPageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if representativeView.ParticipatingMembers != 2 || representativeView.Metrics.ParticipatingPRs.Value != 1 || representativeView.Metrics.CommitCount != 1 {
		t.Fatalf("representative repository metrics = %+v members=%d", representativeView.Metrics, representativeView.ParticipatingMembers)
	}
	if len(representativeView.Commits.Items) != 1 || representativeView.Commits.Items[0].ProcessedTokens != 200 {
		t.Fatalf("representative commits = %+v, want one union commit with two members' Token", representativeView.Commits.Items)
	}
	memberView, err := service.Repository(ctx, member.ID, repo.ID, window, RepositoryPageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if memberView.ParticipatingMembers != 1 || len(memberView.Commits.Items) != 1 || memberView.Commits.Items[0].ProcessedTokens != 100 {
		t.Fatalf("member repository view = %+v", memberView)
	}
}

func TestBucketDetailRestrictsRawRequestIDsAndReportsRetentionStates(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	owner := client.User.Create().SetUsername("owner").SetEmail("owner@example.com").SetAuthSource("ldap").SaveX(ctx)
	other := client.User.Create().SetUsername("other").SetEmail("other@example.com").SetAuthSource("ldap").SaveX(ctx)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	installation := client.ReportingInstallation.Create().SetInstallationID("66666666-6666-4666-8666-666666666666").SetUserID(owner.ID).
		SetReporterTokenHash("reporter-bucket-detail").SetOtlpTokenHash("otlp-bucket-detail").SaveX(ctx)
	observedAt := time.Now().UTC().Add(-time.Minute)
	bucket := client.AttributionUsageBucket.Create().
		SetBucketID("bucket-detail").SetReportingInstallationID(installation.ID).SetUserID(owner.ID).SetTool("codex").SetModel("gpt-test").
		SetSessionSlices([]map[string]any{{"conversation_id": "conversation-detail", "observed_start_at": observedAt.Add(-time.Minute), "observed_end_at": observedAt, "token_atom_count": 1, "atom_set_digest": "atom-detail"}}).
		SetObservedStartAt(observedAt.Add(-time.Minute)).SetObservedEndAt(observedAt).
		SetFreshInputTokens(70).SetCacheReadTokens(20).SetOutputTokens(10).SetProcessedTotalTokens(100).
		SetSourceDigest("source-detail").SetImmutableDigest("immutable-detail").SetExtractorVersion("extractor-test").SetNormalizationVersion(2).
		SetTokenQuality(attributionusagebucket.TokenQualityMeasured).SetRequestCorrelationQuality(attributionusagebucket.RequestCorrelationQualityAdvisory).
		SetRequestIDCoverageCount(1).SaveX(ctx)
	client.AttributionAllocationRevision.Create().SetRevisionID("revision-detail").SetUsageBucketID(bucket.ID).SetSequence(1).
		SetReason("initial").SetEvidenceVersion("evidence-test").SetAllocations(testAllocations(0, "", "unbound")).SetRestatedAt(observedAt).SaveX(ctx)
	store := &activityMemoryStore{values: map[string][]byte{}}
	correlation := attributionledger.NewCorrelationStore(store, "activity-test")
	if err := correlation.Put(ctx, installation.InstallationID, []attributionledger.RequestEvidence{{
		ConversationID: "conversation-detail", RequestID: "request-detail", ObservedAt: observedAt,
		EventName: "codex.api_request", Transport: "http", StatusCode: 200,
	}}); err != nil {
		t.Fatal(err)
	}
	service := NewService(client, correlation, ServiceOptions{})
	retained, err := service.Bucket(ctx, owner.ID, bucket.BucketID)
	if err != nil {
		t.Fatal(err)
	}
	if retained.RequestIDs.State != "retained" || len(retained.RequestIDs.Evidence) != 1 || retained.RequestIDs.Evidence[0].RequestID != "request-detail" {
		t.Fatalf("retained request IDs = %+v", retained.RequestIDs)
	}
	if _, err := service.Bucket(ctx, other.ID, bucket.BucketID); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other-user bucket err = %v, want ErrForbidden", err)
	}
	if _, err := service.Bucket(ctx, admin.ID, bucket.BucketID); err != nil {
		t.Fatalf("admin bucket detail: %v", err)
	}
	unavailable, err := NewService(client, nil, ServiceOptions{}).Bucket(ctx, owner.ID, bucket.BucketID)
	if err != nil || unavailable.RequestIDs.State != "unavailable" {
		t.Fatalf("unavailable request IDs = %+v err=%v", unavailable, err)
	}
	emptyStore := attributionledger.NewCorrelationStore(&activityMemoryStore{values: map[string][]byte{}}, "activity-test")
	expired, err := NewService(client, emptyStore, ServiceOptions{}).Bucket(ctx, owner.ID, bucket.BucketID)
	if err != nil || expired.RequestIDs.State != "expired" {
		t.Fatalf("expired request IDs = %+v err=%v", expired, err)
	}
	client.AttributionUsageBucket.UpdateOne(bucket).SetRequestIDCoverageCount(0).SetRequestCorrelationQuality(attributionusagebucket.RequestCorrelationQualityUnlinked).ExecX(ctx)
	unlinked, err := NewService(client, nil, ServiceOptions{}).Bucket(ctx, owner.ID, bucket.BucketID)
	if err != nil || unlinked.RequestIDs.State != "unlinked" {
		t.Fatalf("unlinked request IDs = %+v err=%v", unlinked, err)
	}
}

func TestActivityCacheNeverAuthorizesAfterRepresentativeScopeIsRevoked(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	representative := client.User.Create().SetUsername("representative").SetEmail("representative@example.com").SetAuthSource("ldap").SaveX(ctx)
	member := client.User.Create().SetUsername("member").SetEmail("member@example.com").SetAuthSource("ldap").SaveX(ctx)
	ordinary := client.User.Create().SetUsername("ordinary").SetEmail("ordinary@example.com").SetAuthSource("ldap").SaveX(ctx)
	outside := client.User.Create().SetUsername("outside").SetEmail("outside@example.com").SetAuthSource("ldap").SaveX(ctx)
	admin := client.User.Create().SetUsername("admin").SetEmail("admin@example.com").SetAuthSource("ldap").SetRole(entuser.RoleAdmin).SaveX(ctx)
	createActivityDirectoryScope(t, client, representative, member, ordinary, outside, admin)
	store := &activityMemoryStore{values: map[string][]byte{}}
	cache, err := NewCache(store, CacheOptions{Namespace: "activity-test"})
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(client, nil, ServiceOptions{Cache: cache})
	window := Window{From: time.Now().UTC().Add(-time.Hour), To: time.Now().UTC()}
	if _, err := service.Member(ctx, representative.ID, member.ID, window, DetailPageOptions{}); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	cachedValues := len(store.values)
	store.mu.Unlock()
	if cachedValues == 0 {
		t.Fatal("first member read did not populate Activity cache")
	}
	root := client.DirectoryDepartment.Query().Where(func(selector *entsql.Selector) {
		selector.Where(entsql.EQ(selector.C("external_id"), "team-root"))
	}).OnlyX(ctx)
	client.DirectoryDepartment.UpdateOne(root).SetMetadata(map[string]any{}).ExecX(ctx)
	if _, err := service.Member(ctx, representative.ID, member.ID, window, DetailPageOptions{}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("revoked representative received cached member data: %v", err)
	}
}

type activityMemoryStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (s *activityMemoryStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, readcache.ErrMiss
	}
	return append([]byte(nil), value...), nil
}

func (s *activityMemoryStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *activityMemoryStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return true, nil
}

func (s *activityMemoryStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return time.Minute, nil
}

func (s *activityMemoryStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return true, nil
}

func testAllocations(repoConfigID int, commitSHA, status string) []map[string]any {
	return []map[string]any{{
		"target": map[string]any{
			"status": status, "repo_config_id": repoConfigID, "repo_key": fmt.Sprintf("repo:%d", repoConfigID),
			"workspace_id": "workspace-a", "commit_sha": commitSHA, "branch": "main",
		},
		"tokens": map[string]any{"processed_total_tokens": 100},
	}}
}

func createActivityBucket(
	t *testing.T,
	client *ent.Client,
	installationID, userID int,
	bucketID string,
	observedAt time.Time,
	quality attributionusagebucket.TokenQuality,
	coverageGaps int,
	allocations []map[string]any,
) *ent.AttributionUsageBucket {
	t.Helper()
	bucket := client.AttributionUsageBucket.Create().
		SetBucketID(bucketID).SetReportingInstallationID(installationID).SetUserID(userID).
		SetTool("codex").SetSessionSlices([]map[string]any{}).SetObservedStartAt(observedAt.Add(-time.Second)).SetObservedEndAt(observedAt).
		SetFreshInputTokens(70).SetCacheReadTokens(20).SetOutputTokens(10).SetProcessedTotalTokens(100).
		SetSourceDigest("source-" + bucketID).SetImmutableDigest("immutable-" + bucketID).SetExtractorVersion("test").
		SetTokenQuality(quality).SetCoverageGapCount(coverageGaps).SaveX(context.Background())
	client.AttributionAllocationRevision.Create().
		SetRevisionID("revision-" + bucketID).SetUsageBucketID(bucket.ID).SetSequence(1).SetReason("initial").SetEvidenceVersion("test").
		SetAllocations(allocations).SetRestatedAt(observedAt).SaveX(context.Background())
	return bucket
}

func createActivityDirectoryScope(t *testing.T, client *ent.Client, representative, member, ordinary, outside, admin *ent.User) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)
	source := client.DirectorySource.Create().SetName("company").SetEnabled(true).SetDsl("version: 1").SaveX(ctx)
	run := client.DirectorySyncRun.Create().SetSourceID(source.ID).SetMode("apply").SetStatus("completed").SetPhase("completed").SetCompletedAt(now).SaveX(ctx)
	client.DirectorySource.UpdateOne(source).SetLastSuccessfulRunID(run.ID).SetLastRunID(run.ID).ExecX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("team-root").SetName("Team Root").SetPath("Team Root").SetLastSeenRunID(run.ID).
		SetMetadata(map[string]any{"representative_external_ids": []string{"member-representative"}}).SaveX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("team-child").SetParentExternalID("team-root").SetEffectiveParentExternalID("team-root").SetName("Team Child").SetPath("Team Root / Team Child").SetLastSeenRunID(run.ID).SaveX(ctx)
	client.DirectoryDepartment.Create().SetSourceID(source.ID).SetExternalID("team-outside").SetName("Outside").SetPath("Outside").SetLastSeenRunID(run.ID).SaveX(ctx)
	for _, item := range []struct {
		externalID string
		department string
		user       *ent.User
	}{
		{"member-representative", "team-root", representative},
		{"member-target", "team-child", member},
		{"member-ordinary", "team-child", ordinary},
		{"member-outside", "team-outside", outside},
		{"member-admin", "team-root", admin},
	} {
		directoryMember := client.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID(item.externalID).
			SetEmailNormalized(item.user.Email).SetDisplayName(item.user.Username).SetDepartmentExternalID(item.department).
			SetStatus("active").SetMatchedUserID(item.user.ID).SetLastSeenRunID(run.ID).SaveX(ctx)
		client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(directoryMember.ID).
			SetMemberExternalID(item.externalID).SetMemberEmailNormalized(item.user.Email).SetDepartmentExternalID(item.department).SetLastSeenRunID(run.ID).SaveX(ctx)
	}
	directoryOnly := client.DirectoryMember.Create().SetSourceID(source.ID).SetExternalID("member-directory-only").
		SetEmailNormalized("directory-only@example.net").SetDisplayName("Directory Only").SetDepartmentExternalID("team-child").
		SetStatus("active").SetLastSeenRunID(run.ID).SaveX(ctx)
	client.DirectoryMemberDepartment.Create().SetSourceID(source.ID).SetDirectoryMemberID(directoryOnly.ID).
		SetMemberExternalID(directoryOnly.ExternalID).SetMemberEmailNormalized(directoryOnly.EmailNormalized).SetDepartmentExternalID("team-child").SetLastSeenRunID(run.ID).SaveX(ctx)
}
