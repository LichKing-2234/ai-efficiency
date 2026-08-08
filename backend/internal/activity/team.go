package activity

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionallocationrevision"
	"github.com/ai-efficiency/backend/ent/attributionusagebucket"
	"github.com/ai-efficiency/backend/ent/directorymember"
	"github.com/ai-efficiency/backend/ent/directorymemberdepartment"
	"github.com/ai-efficiency/backend/ent/prcommitusagesnapshot"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/ent/repoconfig"
)

type projectedMember struct {
	activity *MemberActivity
	commits  map[commitKey]*Commit
	repoIDs  map[int]struct{}
}

func (s *Service) Members(ctx context.Context, actorUserID int, window Window, page PageOptions) (*MembersActivity, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("activity service is not configured")
	}
	authorization, err := s.resolveAuthorization(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	if !authorization.Admin && !authorization.Representative {
		return nil, ErrForbidden
	}
	window, err = normalizeWindow(window, s.currentTime())
	if err != nil {
		return nil, err
	}
	cacheDimensions := activityCacheDimensions("members", authorization, actorUserID, "members", window, page)
	var cached MembersActivity
	if s.cache.read(ctx, cacheDimensions, &cached) {
		return &cached, nil
	}
	identities, err := s.loadCurrentTeamMembers(ctx, authorization, "")
	if err != nil {
		return nil, err
	}
	projections, _, err := s.projectTeamMembers(ctx, identities, window)
	if err != nil {
		return nil, err
	}
	result := &MembersActivity{
		ContractVersion: MetricContractVersion,
		ScopeVersion:    authorization.Version,
		Window:          window,
		Members:         Page[MemberRow]{Items: memberRows(identities, projections)},
	}
	if err := paginateActivityPage(s, &result.Members, "members", authorization, actorUserID, "members", window, page.Limit, page.Cursor); err != nil {
		return nil, err
	}
	s.cache.write(ctx, cacheDimensions, result)
	return result, nil
}

func (s *Service) Team(ctx context.Context, actorUserID int, teamExternalID string, window Window, page PageOptions) (*TeamActivity, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("activity service is not configured")
	}
	authorization, err := s.resolveAuthorization(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	teamExternalID = strings.TrimSpace(teamExternalID)
	team, allowed := authorization.Teams[teamExternalID]
	if !allowed || (!authorization.Admin && !authorization.Representative) {
		return nil, ErrForbidden
	}
	window, err = normalizeWindow(window, s.currentTime())
	if err != nil {
		return nil, err
	}
	cacheDimensions := activityCacheDimensions("team", authorization, actorUserID, "team:"+teamExternalID, window, page)
	var cached TeamActivity
	if s.cache.read(ctx, cacheDimensions, &cached) {
		return &cached, nil
	}
	identities, err := s.loadCurrentTeamMembers(ctx, authorization, teamExternalID)
	if err != nil {
		return nil, err
	}
	team.MemberCount = len(identities)
	result := &TeamActivity{
		ContractVersion: MetricContractVersion,
		ScopeVersion:    authorization.Version,
		Window:          window,
		Team:            team,
		ActiveMembers:   len(identities),
		Members:         Page[MemberRow]{Items: make([]MemberRow, 0, len(identities))},
	}
	projections, unionCoverage, err := s.projectTeamMembers(ctx, identities, window)
	if err != nil {
		return nil, err
	}
	teamPRs := map[prKey]struct{}{}
	teamMergedPRs := map[prKey]struct{}{}
	teamCommits := map[commitKey]struct{}{}
	teamRepos := map[int]struct{}{}
	rows := memberRows(identities, projections)
	for _, row := range rows {
		identity := row.Member
		if projection := projections[identity.UserID]; identity.UserID > 0 && projection != nil {
			for _, pr := range projection.activity.PRs.Items {
				key := prKey{RepoConfigID: pr.RepoConfigID, PRRecordID: pr.PRRecordID}
				teamPRs[key] = struct{}{}
				if pr.Status == string(prrecord.StatusMerged) {
					teamMergedPRs[key] = struct{}{}
				}
			}
			for key := range projection.commits {
				teamCommits[key] = struct{}{}
				teamRepos[key.RepoConfigID] = struct{}{}
			}
			if latest := row.Metrics.LatestActivity; latest != nil && (result.Metrics.LatestActivity == nil || latest.After(*result.Metrics.LatestActivity)) {
				copy := latest.UTC()
				result.Metrics.LatestActivity = &copy
			}
		}
		result.Members.Items = append(result.Members.Items, row)
	}
	result.Metrics.ParticipatingPRs = CountMetric{Value: len(teamPRs), LowerBound: !unionCoverage.Complete}
	result.Metrics.MergedPRs = CountMetric{Value: len(teamMergedPRs), LowerBound: !unionCoverage.Complete}
	result.Metrics.CommitCount = len(teamCommits)
	result.Metrics.ActiveRepositories = len(teamRepos)
	result.SyncCoverage = unionCoverage
	if err := paginateActivityPage(s, &result.Members, "members", authorization, actorUserID, "team:"+teamExternalID, window, page.Limit, page.Cursor); err != nil {
		return nil, err
	}
	s.cache.write(ctx, cacheDimensions, result)
	return result, nil
}

func (s *Service) loadCurrentTeamMembers(ctx context.Context, authorization *authorizationScope, teamExternalID string) ([]MemberIdentity, error) {
	memberships, err := s.client.DirectoryMemberDepartment.Query().Where(
		directorymemberdepartment.SourceIDEQ(authorization.SourceID),
		directorymemberdepartment.LastSeenRunIDEQ(authorization.RunID),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load current activity team memberships: %w", err)
	}
	memberIDs := map[int]struct{}{}
	for _, membership := range memberships {
		if teamExternalID != "" {
			if membership.DepartmentExternalID != teamExternalID {
				continue
			}
		} else if _, allowed := authorization.Teams[membership.DepartmentExternalID]; !allowed {
			continue
		}
		memberIDs[membership.DirectoryMemberID] = struct{}{}
	}
	if len(memberIDs) == 0 {
		return []MemberIdentity{}, nil
	}
	members, err := s.client.DirectoryMember.Query().Where(
		directorymember.IDIn(sortedIntKeys(memberIDs)...),
		directorymember.SourceIDEQ(authorization.SourceID),
		directorymember.LastSeenRunIDEQ(authorization.RunID),
		directorymember.StatusEQ("active"),
	).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load current activity team members: %w", err)
	}
	departmentsByMember := map[int][]string{}
	for _, membership := range memberships {
		if _, selected := memberIDs[membership.DirectoryMemberID]; !selected {
			continue
		}
		departmentsByMember[membership.DirectoryMemberID] = appendUniqueString(departmentsByMember[membership.DirectoryMemberID], membership.DepartmentExternalID)
	}
	usersByID, usersByEmail, err := s.usersByIdentity(ctx)
	if err != nil {
		return nil, err
	}
	identities := make([]MemberIdentity, 0, len(members))
	for _, member := range members {
		identity := MemberIdentity{
			DirectoryMemberExternalID: member.ExternalID,
			DisplayName:               member.DisplayName,
			Email:                     member.EmailNormalized,
			DepartmentExternalIDs:     departmentsByMember[member.ID],
		}
		var matched *ent.User
		if member.MatchedUserID != nil {
			matched = usersByID[*member.MatchedUserID]
		}
		if matched == nil {
			matched = usersByEmail[strings.ToLower(strings.TrimSpace(member.EmailNormalized))]
		}
		if matched != nil {
			if _, allowed := authorization.AllowedUserIDs[matched.ID]; !allowed {
				continue
			}
			identity.UserID = matched.ID
			identity.DisplayName = matched.Username
			identity.Email = matched.Email
		}
		identities = append(identities, identity)
	}
	sort.Slice(identities, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(identities[i].DisplayName))
		right := strings.ToLower(strings.TrimSpace(identities[j].DisplayName))
		if left != right {
			return left < right
		}
		return identities[i].DirectoryMemberExternalID < identities[j].DirectoryMemberExternalID
	})
	return identities, nil
}

func memberRows(identities []MemberIdentity, projections map[int]*projectedMember) []MemberRow {
	rows := make([]MemberRow, 0, len(identities))
	for _, identity := range identities {
		row := MemberRow{Member: identity}
		if projection := projections[identity.UserID]; identity.UserID > 0 && projection != nil {
			row.Available = true
			row.Metrics = projection.activity.Metrics
			row.Quality = projection.activity.Quality
		}
		rows = append(rows, row)
	}
	return rows
}

func (s *Service) usersByIdentity(ctx context.Context) (map[int]*ent.User, map[string]*ent.User, error) {
	users, err := s.client.User.Query().All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load activity users: %w", err)
	}
	byID := make(map[int]*ent.User, len(users))
	byEmail := make(map[string]*ent.User, len(users))
	for _, item := range users {
		byID[item.ID] = item
		byEmail[strings.ToLower(strings.TrimSpace(item.Email))] = item
	}
	return byID, byEmail, nil
}

func (s *Service) projectTeamMembers(ctx context.Context, identities []MemberIdentity, window Window) (map[int]*projectedMember, SyncCoverage, error) {
	projections := map[int]*projectedMember{}
	userIDs := make([]int, 0, len(identities))
	for _, identity := range identities {
		if identity.UserID <= 0 {
			continue
		}
		userIDs = append(userIDs, identity.UserID)
		projections[identity.UserID] = &projectedMember{
			activity: &MemberActivity{Member: identity, Available: true, Window: window, PRs: Page[PullRequest]{Items: []PullRequest{}}, Commits: Page[Commit]{Items: []Commit{}}},
			commits:  map[commitKey]*Commit{},
			repoIDs:  map[int]struct{}{},
		}
	}
	if len(userIDs) == 0 {
		return projections, SyncCoverage{Complete: true}, nil
	}
	buckets, err := s.client.AttributionUsageBucket.Query().Where(
		attributionusagebucket.UserIDIn(userIDs...),
		attributionusagebucket.ObservedEndAtGTE(window.From),
		attributionusagebucket.ObservedEndAtLT(window.To),
	).All(ctx)
	if err != nil {
		return nil, SyncCoverage{}, fmt.Errorf("load team activity buckets: %w", err)
	}
	if len(buckets) == 0 {
		return projections, SyncCoverage{Complete: true}, nil
	}
	bucketIDs := make([]int, 0, len(buckets))
	for _, bucket := range buckets {
		bucketIDs = append(bucketIDs, bucket.ID)
	}
	revisions, err := s.client.AttributionAllocationRevision.Query().Where(
		attributionallocationrevision.UsageBucketIDIn(bucketIDs...), latestAllocationRevisionPredicate(),
	).All(ctx)
	if err != nil {
		return nil, SyncCoverage{}, fmt.Errorf("load latest team activity allocations: %w", err)
	}
	revisionByBucket := make(map[int]*ent.AttributionAllocationRevision, len(revisions))
	for _, revision := range revisions {
		revisionByBucket[revision.UsageBucketID] = revision
	}
	allRepoIDs := map[int]struct{}{}
	allCommitSHAs := map[string]struct{}{}
	for _, bucket := range buckets {
		projection := projections[bucket.UserID]
		if projection == nil {
			continue
		}
		result := projection.activity
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
			return nil, SyncCoverage{}, fmt.Errorf("decode team activity allocation for bucket %s: %w", bucket.BucketID, err)
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
				sha := strings.TrimSpace(allocation.Target.CommitSHA)
				if bucket.TokenQuality != attributionusagebucket.TokenQualityMeasured || allocation.Target.RepoConfigID <= 0 || sha == "" {
					continue
				}
				key := commitKey{RepoConfigID: allocation.Target.RepoConfigID, CommitSHA: sha}
				commit := projection.commits[key]
				if commit == nil {
					commit = &Commit{RepoConfigID: key.RepoConfigID, CommitSHA: sha, Branch: strings.TrimSpace(allocation.Target.Branch), LatestActivity: bucket.ObservedEndAt.UTC(), PRs: []PRReference{}}
					projection.commits[key] = commit
				}
				commit.ProcessedTokens += allocation.Tokens.Processed
				if bucket.ObservedEndAt.After(commit.LatestActivity) {
					commit.LatestActivity = bucket.ObservedEndAt.UTC()
				}
				projection.repoIDs[key.RepoConfigID] = struct{}{}
				allRepoIDs[key.RepoConfigID] = struct{}{}
				allCommitSHAs[sha] = struct{}{}
			}
		}
	}
	if len(allRepoIDs) == 0 {
		return projections, SyncCoverage{Complete: true}, nil
	}
	repos, err := s.client.RepoConfig.Query().Where(repoconfig.IDIn(sortedIntKeys(allRepoIDs)...)).All(ctx)
	if err != nil {
		return nil, SyncCoverage{}, fmt.Errorf("load team activity repositories: %w", err)
	}
	repoNameByID := map[int]string{}
	for _, repo := range repos {
		repoNameByID[repo.ID] = repo.FullName
	}
	commitSHAs := make([]string, 0, len(allCommitSHAs))
	for sha := range allCommitSHAs {
		commitSHAs = append(commitSHAs, sha)
	}
	sort.Strings(commitSHAs)
	snapshots, err := s.client.PRCommitUsageSnapshot.Query().Where(prcommitusagesnapshot.CommitShaIn(commitSHAs...)).All(ctx)
	if err != nil {
		return nil, SyncCoverage{}, fmt.Errorf("load team activity PR commit snapshots: %w", err)
	}
	prIDs := map[int]struct{}{}
	for _, snapshot := range snapshots {
		prIDs[snapshot.PrRecordID] = struct{}{}
	}
	prByID := map[int]*ent.PrRecord{}
	if len(prIDs) > 0 {
		prs, err := s.client.PrRecord.Query().Where(prrecord.IDIn(sortedIntKeys(prIDs)...)).WithRepoConfig().All(ctx)
		if err != nil {
			return nil, SyncCoverage{}, fmt.Errorf("load team activity PR records: %w", err)
		}
		for _, pr := range prs {
			prByID[pr.ID] = pr
		}
	}
	jobs, err := s.client.PRSyncJob.Query().Where(prsyncjob.RepoConfigIDIn(sortedIntKeys(allRepoIDs)...), latestPRSyncJobPredicate()).All(ctx)
	if err != nil {
		return nil, SyncCoverage{}, fmt.Errorf("load team activity PR sync coverage: %w", err)
	}
	jobByRepo := map[int]*ent.PRSyncJob{}
	for _, job := range jobs {
		jobByRepo[job.RepoConfigID] = job
	}
	for _, projection := range projections {
		prItems := map[prKey]*PullRequest{}
		for key, commit := range projection.commits {
			commit.RepoName = repoNameByID[key.RepoConfigID]
		}
		for _, snapshot := range snapshots {
			pr := prByID[snapshot.PrRecordID]
			if pr == nil || pr.Edges.RepoConfig == nil {
				continue
			}
			key := commitKey{RepoConfigID: pr.Edges.RepoConfig.ID, CommitSHA: snapshot.CommitSha}
			commit := projection.commits[key]
			if commit == nil {
				continue
			}
			pk := prKey{RepoConfigID: key.RepoConfigID, PRRecordID: pr.ID}
			item := prItems[pk]
			if item == nil {
				item = &PullRequest{RepoConfigID: key.RepoConfigID, RepoName: pr.Edges.RepoConfig.FullName, PRRecordID: pr.ID, SCMPRID: pr.ScmPrID, Title: pr.Title, URL: pr.ScmPrURL, Status: string(pr.Status), MergedAt: pr.MergedAt, Commits: []CommitReference{}}
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
			projection.activity.PRs.Items = append(projection.activity.PRs.Items, *item)
			if item.Status == string(prrecord.StatusMerged) {
				projection.activity.Metrics.MergedPRs.Value++
			}
		}
		for _, commit := range projection.commits {
			projection.activity.Commits.Items = append(projection.activity.Commits.Items, *commit)
		}
		projection.activity.Metrics.ParticipatingPRs.Value = len(prItems)
		projection.activity.Metrics.CommitCount = len(projection.commits)
		projection.activity.Metrics.ActiveRepositories = len(projection.repoIDs)
		projection.activity.SyncCoverage = coverageForRepositories(projection.repoIDs, jobByRepo, s.currentTime())
		projection.activity.Metrics.ParticipatingPRs.LowerBound = !projection.activity.SyncCoverage.Complete
		projection.activity.Metrics.MergedPRs.LowerBound = !projection.activity.SyncCoverage.Complete
		sort.Slice(projection.activity.PRs.Items, func(i, j int) bool {
			left, right := projection.activity.PRs.Items[i], projection.activity.PRs.Items[j]
			if left.RepoConfigID != right.RepoConfigID {
				return left.RepoConfigID < right.RepoConfigID
			}
			return left.PRRecordID < right.PRRecordID
		})
		sort.Slice(projection.activity.Commits.Items, func(i, j int) bool {
			left, right := projection.activity.Commits.Items[i], projection.activity.Commits.Items[j]
			if !left.LatestActivity.Equal(right.LatestActivity) {
				return left.LatestActivity.After(right.LatestActivity)
			}
			if left.RepoConfigID != right.RepoConfigID {
				return left.RepoConfigID < right.RepoConfigID
			}
			return left.CommitSHA < right.CommitSHA
		})
	}
	return projections, coverageForRepositories(allRepoIDs, jobByRepo, s.currentTime()), nil
}

func coverageForRepositories(repoIDs map[int]struct{}, jobs map[int]*ent.PRSyncJob, now time.Time) SyncCoverage {
	coverage := SyncCoverage{Complete: true}
	for repoID := range repoIDs {
		job := jobs[repoID]
		switch {
		case job == nil:
			coverage.UnsyncedRepositories++
		case job.Status == prsyncjob.StatusFailed || job.Status == prsyncjob.StatusAbandoned || job.Status == prsyncjob.StatusCancelled:
			coverage.FailedRepositories++
		case job.Status != prsyncjob.StatusCompleted || job.CompletedAt == nil:
			coverage.PartiallySyncedRepositories++
		case job.UpsertFailedPrs > 0 || job.UsageFailedPrs > 0:
			coverage.PartiallySyncedRepositories++
		case now.Sub(job.CompletedAt.UTC()) > defaultPRSyncStaleAfter:
			coverage.StaleRepositories++
		}
	}
	coverage.AffectedRepositories = coverage.UnsyncedRepositories + coverage.StaleRepositories + coverage.PartiallySyncedRepositories + coverage.FailedRepositories
	coverage.Complete = coverage.AffectedRepositories == 0
	return coverage
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
