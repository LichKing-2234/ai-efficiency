package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionallocationrevision"
	"github.com/ai-efficiency/backend/ent/attributionusagebucket"
	"github.com/ai-efficiency/backend/ent/directorydepartment"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/prcommitusagesnapshot"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/attributionledger"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/representativescope"
)

var (
	ErrForbidden       = errors.New("activity subject is outside the current authorized scope")
	ErrInvalidCursor   = errors.New("invalid activity cursor")
	ErrSnapshotExpired = errors.New("activity snapshot expired")
)

const defaultPRSyncStaleAfter = 24 * time.Hour

type ScopeResolver interface {
	Resolve(context.Context, int) (*representativescope.Scope, error)
}

type ServiceOptions struct {
	ScopeResolver ScopeResolver
	CursorSecret  string
	Cache         *Cache
}

type Service struct {
	client       *ent.Client
	correlation  *attributionledger.CorrelationStore
	scope        ScopeResolver
	cursorSecret []byte
	cache        *Cache
	now          func() time.Time
}

func NewService(client *ent.Client, correlation *attributionledger.CorrelationStore, options ServiceOptions) *Service {
	scopeResolver := options.ScopeResolver
	if scopeResolver == nil && client != nil {
		scopeResolver = representativescope.New(client)
	}
	return &Service{client: client, correlation: correlation, scope: scopeResolver, cursorSecret: []byte(options.CursorSecret), cache: options.Cache, now: time.Now}
}

func (s *Service) Scope(ctx context.Context, actorUserID int) (*ScopeResponse, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("activity service is not configured")
	}
	authorization, err := s.resolveAuthorization(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	result := &ScopeResponse{
		ContractVersion: MetricContractVersion,
		ScopeVersion:    authorization.Version,
		CanViewTeams:    authorization.Admin || authorization.Representative,
		Admin:           authorization.Admin,
		Representative:  authorization.Representative,
		Teams:           []Team{},
	}
	if result.CanViewTeams {
		for _, team := range authorization.Teams {
			result.Teams = append(result.Teams, team)
		}
		sort.Slice(result.Teams, func(i, j int) bool {
			if result.Teams[i].DisplayPath != result.Teams[j].DisplayPath {
				return result.Teams[i].DisplayPath < result.Teams[j].DisplayPath
			}
			return result.Teams[i].ExternalID < result.Teams[j].ExternalID
		})
	}
	return result, nil
}

func (s *Service) Member(ctx context.Context, actorUserID, targetUserID int, window Window, pages DetailPageOptions) (*MemberActivity, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("activity service is not configured")
	}
	authorization, err := s.resolveAuthorization(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	if _, allowed := authorization.AllowedUserIDs[targetUserID]; !allowed {
		return nil, ErrForbidden
	}
	target, err := s.client.User.Query().Where(user.IDEQ(targetUserID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load activity target: %w", err)
	}
	window, err = normalizeWindow(window, s.currentTime())
	if err != nil {
		return nil, err
	}
	cacheDimensions := activityCacheDimensions("member", authorization, actorUserID, fmt.Sprintf("member:%d", targetUserID), window, pages)
	var cached MemberActivity
	if s.cache.read(ctx, cacheDimensions, &cached) {
		return &cached, nil
	}
	result := &MemberActivity{
		ContractVersion: MetricContractVersion,
		Window:          window,
		Member:          MemberIdentity{UserID: target.ID, DisplayName: target.Username, Email: target.Email},
		Available:       true,
		PRs:             Page[PullRequest]{Items: []PullRequest{}},
		Commits:         Page[Commit]{Items: []Commit{}},
		Buckets:         Page[BucketSummary]{Items: []BucketSummary{}},
		BucketAccess:    authorization.Admin || actorUserID == targetUserID,
	}
	if err := s.loadMemberActivity(ctx, result, target.ID, pages); err != nil {
		return nil, err
	}
	if !result.BucketAccess {
		result.Buckets.Items = []BucketSummary{}
		result.Buckets.NextCursor = ""
	}
	if err := s.paginateMemberActivity(result, authorization, actorUserID, targetUserID, pages); err != nil {
		return nil, err
	}
	s.cache.write(ctx, cacheDimensions, result)
	return result, nil
}

type authorizationScope struct {
	Actor          *ent.User
	Admin          bool
	Representative bool
	Version        string
	SourceID       int
	RunID          int
	AllowedUserIDs map[int]struct{}
	Teams          map[string]Team
}

func (s *Service) resolveAuthorization(ctx context.Context, actorUserID int) (*authorizationScope, error) {
	actor, err := s.client.User.Query().Where(user.IDEQ(actorUserID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load activity actor: %w", err)
	}
	authorization := &authorizationScope{
		Actor: actor, AllowedUserIDs: map[int]struct{}{actor.ID: {}}, Teams: map[string]Team{},
	}
	snapshot, hasSnapshot, err := directorysync.CurrentSnapshot(ctx, s.client)
	if err != nil {
		return nil, fmt.Errorf("resolve activity directory snapshot: %w", err)
	}
	if hasSnapshot {
		authorization.Version = fmt.Sprintf("directory:%d:%d", snapshot.SourceID, snapshot.RunID)
		authorization.SourceID = snapshot.SourceID
		authorization.RunID = snapshot.RunID
	}
	if actor.Role == user.RoleAdmin {
		authorization.Admin = true
		if !hasSnapshot {
			return authorization, nil
		}
		departments, err := s.client.DirectoryDepartment.Query().Where(
			directorydepartment.SourceIDEQ(snapshot.SourceID),
			directorydepartment.LastSeenRunIDEQ(snapshot.RunID),
		).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load current Admin activity teams: %w", err)
		}
		for _, department := range departments {
			authorization.Teams[department.ExternalID] = Team{
				ExternalID: department.ExternalID, ParentExternalID: department.EffectiveParentExternalID,
				Name: department.Name, DisplayPath: department.Path,
			}
		}
		members, err := s.client.DirectoryMember.Query().Where(
			directorymember.SourceIDEQ(snapshot.SourceID),
			directorymember.LastSeenRunIDEQ(snapshot.RunID),
			directorymember.StatusEQ("active"),
		).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load current Admin activity members: %w", err)
		}
		memberships := []*ent.DirectoryMemberDepartment{}
		if len(members) > 0 {
			memberIDs := make([]int, 0, len(members))
			for _, member := range members {
				memberIDs = append(memberIDs, member.ID)
			}
			memberships, err = s.client.DirectoryMemberDepartment.Query().Where(
				directorymemberdepartment.SourceIDEQ(snapshot.SourceID),
				directorymemberdepartment.LastSeenRunIDEQ(snapshot.RunID),
				directorymemberdepartment.DirectoryMemberIDIn(memberIDs...),
			).All(ctx)
			if err != nil {
				return nil, fmt.Errorf("load current Admin activity member departments: %w", err)
			}
		}
		memberCounts := adminActivityTeamMemberCounts(departments, members, memberships)
		for externalID, count := range memberCounts {
			team := authorization.Teams[externalID]
			team.MemberCount = count
			authorization.Teams[externalID] = team
		}
		userByEmail, err := s.userIDsByEmail(ctx)
		if err != nil {
			return nil, err
		}
		for _, member := range members {
			if member.MatchedUserID != nil && *member.MatchedUserID > 0 {
				authorization.AllowedUserIDs[*member.MatchedUserID] = struct{}{}
				continue
			}
			if userID := userByEmail[strings.ToLower(strings.TrimSpace(member.EmailNormalized))]; userID > 0 {
				authorization.AllowedUserIDs[userID] = struct{}{}
			}
		}
		return authorization, nil
	}
	if s.scope == nil || !hasSnapshot {
		return authorization, nil
	}
	representativeScope, err := s.scope.Resolve(ctx, actor.ID)
	if err != nil {
		return nil, fmt.Errorf("resolve representative activity scope: %w", err)
	}
	if representativeScope == nil || !representativeScope.IsRepresentative {
		return authorization, nil
	}
	authorization.Representative = true
	authorization.Version = representativeScope.Version
	for _, department := range representativeScope.MemberTreeDepartments {
		authorization.Teams[department.ExternalID] = Team{
			ExternalID: department.ExternalID, ParentExternalID: department.ParentExternalID,
			Name: department.Name, DisplayPath: department.DisplayPath, MemberCount: department.SubtreeMemberCount,
		}
	}
	for _, subject := range representativeScope.OverviewSubjects {
		if subject.SubjectType == "member" && subject.UserID > 0 {
			authorization.AllowedUserIDs[subject.UserID] = struct{}{}
		}
	}
	return authorization, nil
}

func (s *Service) userIDsByEmail(ctx context.Context) (map[string]int, error) {
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load activity users: %w", err)
	}
	result := make(map[string]int, len(users))
	for _, item := range users {
		result[strings.ToLower(strings.TrimSpace(item.Email))] = item.ID
	}
	return result, nil
}

func adminActivityTeamMemberCounts(departments []*ent.DirectoryDepartment, members []*ent.DirectoryMember, memberships []*ent.DirectoryMemberDepartment) map[string]int {
	knownDepartments := map[string]struct{}{}
	parentByDepartment := map[string]string{}
	for _, department := range departments {
		if department == nil {
			continue
		}
		knownDepartments[department.ExternalID] = struct{}{}
		if department.EffectiveParentExternalID != nil {
			parentID := strings.TrimSpace(*department.EffectiveParentExternalID)
			if parentID != "" {
				parentByDepartment[department.ExternalID] = parentID
			}
		}
	}
	activeMembers := map[int]struct{}{}
	departmentsByMember := map[int]map[string]struct{}{}
	addMember := func(departmentID string, memberID int) {
		departmentID = strings.TrimSpace(departmentID)
		if _, ok := knownDepartments[departmentID]; !ok || memberID <= 0 {
			return
		}
		if departmentsByMember[memberID] == nil {
			departmentsByMember[memberID] = map[string]struct{}{}
		}
		departmentsByMember[memberID][departmentID] = struct{}{}
	}
	for _, member := range members {
		if member == nil {
			continue
		}
		activeMembers[member.ID] = struct{}{}
		addMember(member.DepartmentExternalID, member.ID)
	}
	for _, membership := range memberships {
		if membership == nil {
			continue
		}
		if _, ok := activeMembers[membership.DirectoryMemberID]; !ok {
			continue
		}
		addMember(membership.DepartmentExternalID, membership.DirectoryMemberID)
	}

	membersByDepartment := make(map[string]map[int]struct{}, len(knownDepartments))
	for memberID, directDepartments := range departmentsByMember {
		seenDepartments := map[string]struct{}{}
		for departmentID := range directDepartments {
			for current := departmentID; current != ""; current = parentByDepartment[current] {
				if _, known := knownDepartments[current]; !known {
					break
				}
				if _, seen := seenDepartments[current]; seen {
					break
				}
				seenDepartments[current] = struct{}{}
				if membersByDepartment[current] == nil {
					membersByDepartment[current] = map[int]struct{}{}
				}
				membersByDepartment[current][memberID] = struct{}{}
			}
		}
	}

	counts := make(map[string]int, len(knownDepartments))
	for departmentID := range knownDepartments {
		counts[departmentID] = len(membersByDepartment[departmentID])
	}
	return counts
}

func normalizeWindow(window Window, now time.Time) (Window, error) {
	to := window.To.UTC()
	if to.IsZero() {
		to = now.UTC()
	}
	from := window.From.UTC()
	if from.IsZero() {
		from = to.Add(-30 * 24 * time.Hour)
	}
	if !to.After(from) || to.Sub(from) > 366*24*time.Hour {
		return Window{}, fmt.Errorf("invalid activity window")
	}
	return Window{From: from, To: to}, nil
}

func (s *Service) currentTime() time.Time {
	if s.now == nil {
		return time.Now().UTC()
	}
	return s.now().UTC()
}

type allocation struct {
	Target struct {
		Status       string `json:"status"`
		RepoConfigID int    `json:"repo_config_id"`
		CommitSHA    string `json:"commit_sha"`
		Branch       string `json:"branch"`
	} `json:"target"`
	Tokens struct {
		Processed int64 `json:"processed_total_tokens"`
	} `json:"tokens"`
}

type commitKey struct {
	RepoConfigID int
	CommitSHA    string
}

type prKey struct {
	RepoConfigID int
	PRRecordID   int
}

func (s *Service) loadMemberActivity(ctx context.Context, result *MemberActivity, userID int, pages DetailPageOptions) error {
	buckets, err := s.client.AttributionUsageBucket.Query().Where(
		attributionusagebucket.UserIDEQ(userID),
		attributionusagebucket.ObservedEndAtGTE(result.Window.From),
		attributionusagebucket.ObservedEndAtLT(result.Window.To),
	).Order(ent.Desc(attributionusagebucket.FieldObservedEndAt), ent.Desc(attributionusagebucket.FieldID)).All(ctx)
	if err != nil {
		return fmt.Errorf("load activity buckets: %w", err)
	}
	if len(buckets) == 0 {
		result.SyncCoverage = SyncCoverage{Complete: true}
		return nil
	}
	bucketIDs := make([]int, 0, len(buckets))
	for _, bucket := range buckets {
		bucketIDs = append(bucketIDs, bucket.ID)
	}
	revisions, err := s.client.AttributionAllocationRevision.Query().Where(
		attributionallocationrevision.UsageBucketIDIn(bucketIDs...),
		latestAllocationRevisionPredicate(),
	).All(ctx)
	if err != nil {
		return fmt.Errorf("load latest activity allocations: %w", err)
	}
	revisionByBucket := make(map[int]*ent.AttributionAllocationRevision, len(revisions))
	for _, revision := range revisions {
		revisionByBucket[revision.UsageBucketID] = revision
	}

	commits := map[commitKey]*Commit{}
	repoIDs := map[int]struct{}{}
	for _, bucket := range buckets {
		switch bucket.TokenQuality {
		case attributionusagebucket.TokenQualityMeasured:
			result.Quality.MeasuredBuckets++
			if result.Metrics.LatestActivity == nil || bucket.ObservedEndAt.After(*result.Metrics.LatestActivity) {
				latest := bucket.ObservedEndAt.UTC()
				result.Metrics.LatestActivity = &latest
			}
		case attributionusagebucket.TokenQualityInvalid:
			result.Quality.InvalidTokenFacts++
		case attributionusagebucket.TokenQualityHistoricalAdvisory:
			result.Quality.HistoricalAdvisoryFacts++
		}
		result.Quality.CoverageGapCount += bucket.CoverageGapCount
		revision := revisionByBucket[bucket.ID]
		if revision == nil {
			continue
		}
		allocations, err := decodeAllocations(revision.Allocations)
		if err != nil {
			return fmt.Errorf("decode activity allocation for bucket %s: %w", bucket.BucketID, err)
		}
		allocationStatus := "unbound"
		if len(allocations) > 0 {
			allocationStatus = allocations[0].Target.Status
		}
		if bucket.TokenQuality == attributionusagebucket.TokenQualityMeasured {
			result.Buckets.Items = append(result.Buckets.Items, BucketSummary{
				BucketID: bucket.BucketID, ObservedEnd: bucket.ObservedEndAt.UTC(), ProcessedTokens: bucket.ProcessedTotalTokens, AllocationStatus: allocationStatus,
			})
		}
		for _, allocation := range allocations {
			switch allocation.Target.Status {
			case "unbound":
				if bucket.TokenQuality == attributionusagebucket.TokenQualityMeasured {
					result.Quality.UnboundBuckets++
				}
			case "multi_repo_shared":
				if bucket.TokenQuality == attributionusagebucket.TokenQualityMeasured {
					result.Quality.MultiRepoSharedBuckets++
				}
			case "bound_auto", "bound_manual":
				if bucket.TokenQuality != attributionusagebucket.TokenQualityMeasured || allocation.Target.RepoConfigID <= 0 || strings.TrimSpace(allocation.Target.CommitSHA) == "" {
					continue
				}
				key := commitKey{RepoConfigID: allocation.Target.RepoConfigID, CommitSHA: strings.TrimSpace(allocation.Target.CommitSHA)}
				item := commits[key]
				if item == nil {
					item = &Commit{RepoConfigID: key.RepoConfigID, CommitSHA: key.CommitSHA, Branch: strings.TrimSpace(allocation.Target.Branch), LatestActivity: bucket.ObservedEndAt.UTC(), PRs: []PRReference{}}
					commits[key] = item
				}
				item.ProcessedTokens += allocation.Tokens.Processed
				if bucket.ObservedEndAt.After(item.LatestActivity) {
					item.LatestActivity = bucket.ObservedEndAt.UTC()
				}
				repoIDs[key.RepoConfigID] = struct{}{}
			}
		}
	}
	result.Metrics.ActiveRepositories = len(repoIDs)
	result.Metrics.CommitCount = len(commits)
	if len(commits) == 0 {
		return nil
	}
	if err := s.attachRepositoriesAndPRs(ctx, result, commits, repoIDs); err != nil {
		return err
	}
	sort.Slice(result.Commits.Items, func(i, j int) bool {
		if !result.Commits.Items[i].LatestActivity.Equal(result.Commits.Items[j].LatestActivity) {
			return result.Commits.Items[i].LatestActivity.After(result.Commits.Items[j].LatestActivity)
		}
		if result.Commits.Items[i].RepoConfigID != result.Commits.Items[j].RepoConfigID {
			return result.Commits.Items[i].RepoConfigID < result.Commits.Items[j].RepoConfigID
		}
		return result.Commits.Items[i].CommitSHA < result.Commits.Items[j].CommitSHA
	})
	return nil
}

func latestAllocationRevisionPredicate() func(*entsql.Selector) {
	return func(revisions *entsql.Selector) {
		newer := entsql.Table(attributionallocationrevision.Table).As("newer_activity_revisions")
		revisions.Where(entsql.NotExists(
			entsql.SelectExpr(entsql.Expr("1")).From(newer).Where(entsql.And(
				entsql.ColumnsEQ(newer.C(attributionallocationrevision.FieldUsageBucketID), revisions.C(attributionallocationrevision.FieldUsageBucketID)),
				entsql.ColumnsGT(newer.C(attributionallocationrevision.FieldSequence), revisions.C(attributionallocationrevision.FieldSequence)),
			)),
		))
	}
}

func decodeAllocations(values []map[string]any) ([]allocation, error) {
	payload, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	var allocations []allocation
	if err := json.Unmarshal(payload, &allocations); err != nil {
		return nil, err
	}
	return allocations, nil
}

func (s *Service) attachRepositoriesAndPRs(ctx context.Context, result *MemberActivity, commits map[commitKey]*Commit, repoIDs map[int]struct{}) error {
	repoIDList := sortedIntKeys(repoIDs)
	repos, err := s.client.RepoConfig.Query().Where(func(selector *entsql.Selector) {
		selector.Where(entsql.InInts(selector.C("id"), repoIDList...))
	}).All(ctx)
	if err != nil {
		return fmt.Errorf("load activity repositories: %w", err)
	}
	repoNameByID := map[int]string{}
	for _, repo := range repos {
		repoNameByID[repo.ID] = repo.FullName
	}
	commitSHAs := make([]string, 0, len(commits))
	seenSHA := map[string]struct{}{}
	for key, item := range commits {
		item.RepoName = repoNameByID[key.RepoConfigID]
		if _, ok := seenSHA[key.CommitSHA]; !ok {
			seenSHA[key.CommitSHA] = struct{}{}
			commitSHAs = append(commitSHAs, key.CommitSHA)
		}
	}
	snapshots, err := s.client.PRCommitUsageSnapshot.Query().Where(prcommitusagesnapshot.CommitShaIn(commitSHAs...)).All(ctx)
	if err != nil {
		return fmt.Errorf("load activity PR commit snapshots: %w", err)
	}
	prIDs := map[int]struct{}{}
	for _, snapshot := range snapshots {
		prIDs[snapshot.PrRecordID] = struct{}{}
	}
	if len(prIDs) == 0 {
		for _, commit := range commits {
			result.Commits.Items = append(result.Commits.Items, *commit)
		}
		return s.loadSyncCoverage(ctx, result, repoIDList)
	}
	prs, err := s.client.PrRecord.Query().Where(prrecord.IDIn(sortedIntKeys(prIDs)...)).WithRepoConfig().All(ctx)
	if err != nil {
		return fmt.Errorf("load activity PR records: %w", err)
	}
	prByID := map[int]*ent.PrRecord{}
	for _, pr := range prs {
		prByID[pr.ID] = pr
	}
	prItems := map[prKey]*PullRequest{}
	for _, snapshot := range snapshots {
		pr := prByID[snapshot.PrRecordID]
		if pr == nil || pr.Edges.RepoConfig == nil {
			continue
		}
		key := commitKey{RepoConfigID: pr.Edges.RepoConfig.ID, CommitSHA: snapshot.CommitSha}
		commit := commits[key]
		if commit == nil {
			continue
		}
		pk := prKey{RepoConfigID: key.RepoConfigID, PRRecordID: pr.ID}
		item := prItems[pk]
		if item == nil {
			item = &PullRequest{
				RepoConfigID: key.RepoConfigID, RepoName: pr.Edges.RepoConfig.FullName, PRRecordID: pr.ID, SCMPRID: pr.ScmPrID,
				Title: pr.Title, URL: pr.ScmPrURL, Status: string(pr.Status), MergedAt: pr.MergedAt, Commits: []CommitReference{},
			}
			if pr.MergedAt != nil {
				hours := pr.MergedAt.Sub(pr.CreatedAt).Hours()
				item.CycleTimeHours = &hours
			}
			prItems[pk] = item
		}
		if !pullRequestHasCommit(*item, key) {
			item.Commits = append(item.Commits, CommitReference{RepoConfigID: key.RepoConfigID, CommitSHA: key.CommitSHA})
		}
		if !commitHasPR(*commit, pk) {
			commit.PRs = append(commit.PRs, PRReference{RepoConfigID: key.RepoConfigID, PRRecordID: pr.ID, SCMPRID: pr.ScmPrID})
		}
	}
	for _, item := range prItems {
		result.PRs.Items = append(result.PRs.Items, *item)
		if item.Status == string(prrecord.StatusMerged) {
			result.Metrics.MergedPRs.Value++
		}
	}
	result.Metrics.ParticipatingPRs.Value = len(prItems)
	for _, commit := range commits {
		result.Commits.Items = append(result.Commits.Items, *commit)
	}
	sort.Slice(result.PRs.Items, func(i, j int) bool {
		if result.PRs.Items[i].RepoConfigID != result.PRs.Items[j].RepoConfigID {
			return result.PRs.Items[i].RepoConfigID < result.PRs.Items[j].RepoConfigID
		}
		return result.PRs.Items[i].PRRecordID < result.PRs.Items[j].PRRecordID
	})
	return s.loadSyncCoverage(ctx, result, repoIDList)
}

func (s *Service) loadSyncCoverage(ctx context.Context, result *MemberActivity, repoIDs []int) error {
	coverage := SyncCoverage{Complete: true}
	if len(repoIDs) == 0 {
		result.SyncCoverage = coverage
		return nil
	}
	jobs, err := s.client.PRSyncJob.Query().Where(prsyncjob.RepoConfigIDIn(repoIDs...), latestPRSyncJobPredicate()).All(ctx)
	if err != nil {
		return fmt.Errorf("load activity PR sync coverage: %w", err)
	}
	jobByRepo := map[int]*ent.PRSyncJob{}
	for _, job := range jobs {
		jobByRepo[job.RepoConfigID] = job
	}
	for _, repoID := range repoIDs {
		job := jobByRepo[repoID]
		switch {
		case job == nil:
			coverage.UnsyncedRepositories++
		case job.Status == prsyncjob.StatusFailed || job.Status == prsyncjob.StatusAbandoned || job.Status == prsyncjob.StatusCancelled:
			coverage.FailedRepositories++
		case job.Status != prsyncjob.StatusCompleted || job.CompletedAt == nil:
			coverage.PartiallySyncedRepositories++
		case job.UpsertFailedPrs > 0 || job.UsageFailedPrs > 0:
			coverage.PartiallySyncedRepositories++
		case s.currentTime().Sub(job.CompletedAt.UTC()) > defaultPRSyncStaleAfter:
			coverage.StaleRepositories++
		}
	}
	coverage.AffectedRepositories = coverage.UnsyncedRepositories + coverage.StaleRepositories + coverage.PartiallySyncedRepositories + coverage.FailedRepositories
	coverage.Complete = coverage.AffectedRepositories == 0
	result.SyncCoverage = coverage
	result.Metrics.ParticipatingPRs.LowerBound = !coverage.Complete
	result.Metrics.MergedPRs.LowerBound = !coverage.Complete
	return nil
}

func latestPRSyncJobPredicate() func(*entsql.Selector) {
	return func(jobs *entsql.Selector) {
		newer := entsql.Table(prsyncjob.Table).As("newer_activity_pr_sync_jobs")
		jobs.Where(entsql.NotExists(
			entsql.SelectExpr(entsql.Expr("1")).From(newer).Where(entsql.And(
				entsql.ColumnsEQ(newer.C(prsyncjob.FieldRepoConfigID), jobs.C(prsyncjob.FieldRepoConfigID)),
				entsql.ColumnsGT(newer.C(prsyncjob.FieldID), jobs.C(prsyncjob.FieldID)),
			)),
		))
	}
}

func sortedIntKeys[T any](values map[int]T) []int {
	keys := make([]int, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Ints(keys)
	return keys
}

func pullRequestHasCommit(pr PullRequest, key commitKey) bool {
	for _, existing := range pr.Commits {
		if existing.RepoConfigID == key.RepoConfigID && existing.CommitSHA == key.CommitSHA {
			return true
		}
	}
	return false
}

func commitHasPR(commit Commit, key prKey) bool {
	for _, existing := range commit.PRs {
		if existing.RepoConfigID == key.RepoConfigID && existing.PRRecordID == key.PRRecordID {
			return true
		}
	}
	return false
}
