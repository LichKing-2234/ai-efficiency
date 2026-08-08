package activity

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/user"
)

func (s *Service) Repository(ctx context.Context, actorUserID, repoConfigID int, window Window, pages RepositoryPageOptions) (*RepositoryActivity, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("activity service is not configured")
	}
	authorization, err := s.resolveAuthorization(ctx, actorUserID)
	if err != nil {
		return nil, err
	}
	repo, err := s.client.RepoConfig.Query().Where(repoconfig.IDEQ(repoConfigID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load activity repository: %w", err)
	}
	window, err = normalizeWindow(window, s.currentTime())
	if err != nil {
		return nil, err
	}
	cacheDimensions := activityCacheDimensions("repository", authorization, actorUserID, "repository:"+strconv.Itoa(repoConfigID), window, pages)
	var cached RepositoryActivity
	if s.cache.read(ctx, cacheDimensions, &cached) {
		return &cached, nil
	}
	identities, err := s.authorizedMemberIdentities(ctx, authorization)
	if err != nil {
		return nil, err
	}
	projections, _, err := s.projectTeamMembers(ctx, identities, window)
	if err != nil {
		return nil, err
	}
	result := &RepositoryActivity{
		ContractVersion: MetricContractVersion,
		ScopeVersion:    authorization.Version,
		Window:          window,
		Repository:      RepositoryIdentity{RepoConfigID: repo.ID, Name: repo.FullName},
		Members:         Page[MemberRow]{Items: []MemberRow{}},
		PRs:             Page[PullRequest]{Items: []PullRequest{}},
		Commits:         Page[Commit]{Items: []Commit{}},
	}
	unionPRs := map[prKey]*PullRequest{}
	unionCommits := map[commitKey]*Commit{}
	for _, identity := range identities {
		projection := projections[identity.UserID]
		if identity.UserID <= 0 || projection == nil {
			continue
		}
		memberMetrics := Metrics{}
		memberPRs := map[prKey]struct{}{}
		memberMergedPRs := map[prKey]struct{}{}
		for _, pr := range projection.activity.PRs.Items {
			if pr.RepoConfigID != repoConfigID {
				continue
			}
			key := prKey{RepoConfigID: repoConfigID, PRRecordID: pr.PRRecordID}
			memberPRs[key] = struct{}{}
			if pr.Status == string(prrecord.StatusMerged) {
				memberMergedPRs[key] = struct{}{}
			}
			mergePullRequest(unionPRs, key, pr)
		}
		memberCommitCount := 0
		for key, commit := range projection.commits {
			if key.RepoConfigID != repoConfigID {
				continue
			}
			memberCommitCount++
			mergeCommit(unionCommits, key, *commit)
			if memberMetrics.LatestActivity == nil || commit.LatestActivity.After(*memberMetrics.LatestActivity) {
				latest := commit.LatestActivity.UTC()
				memberMetrics.LatestActivity = &latest
			}
		}
		if memberCommitCount == 0 {
			continue
		}
		memberMetrics.ParticipatingPRs = CountMetric{Value: len(memberPRs), LowerBound: !projection.activity.SyncCoverage.Complete}
		memberMetrics.MergedPRs = CountMetric{Value: len(memberMergedPRs), LowerBound: !projection.activity.SyncCoverage.Complete}
		memberMetrics.ActiveRepositories = 1
		memberMetrics.CommitCount = memberCommitCount
		result.Members.Items = append(result.Members.Items, MemberRow{Member: identity, Available: true, Metrics: memberMetrics})
	}
	for _, pr := range unionPRs {
		result.PRs.Items = append(result.PRs.Items, *pr)
		if pr.Status == string(prrecord.StatusMerged) {
			result.Metrics.MergedPRs.Value++
		}
	}
	for _, commit := range unionCommits {
		result.Commits.Items = append(result.Commits.Items, *commit)
		if result.Metrics.LatestActivity == nil || commit.LatestActivity.After(*result.Metrics.LatestActivity) {
			latest := commit.LatestActivity.UTC()
			result.Metrics.LatestActivity = &latest
		}
	}
	result.ParticipatingMembers = len(result.Members.Items)
	result.Metrics.ParticipatingPRs.Value = len(unionPRs)
	result.Metrics.CommitCount = len(unionCommits)
	if len(unionCommits) > 0 {
		result.Metrics.ActiveRepositories = 1
		jobs, err := s.client.PRSyncJob.Query().Where(prsyncjob.RepoConfigIDEQ(repoConfigID), latestPRSyncJobPredicate()).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load repository activity PR sync coverage: %w", err)
		}
		jobByRepo := map[int]*ent.PRSyncJob{}
		for _, job := range jobs {
			jobByRepo[repoConfigID] = job
		}
		result.SyncCoverage = coverageForRepositories(map[int]struct{}{repoConfigID: {}}, jobByRepo, s.currentTime())
	} else {
		result.SyncCoverage = SyncCoverage{Complete: true}
	}
	result.Metrics.ParticipatingPRs.LowerBound = !result.SyncCoverage.Complete
	result.Metrics.MergedPRs.LowerBound = !result.SyncCoverage.Complete
	sortRepositoryActivity(result)
	subject := "repository:" + strconv.Itoa(repoConfigID)
	if err := paginateActivityPage(s, &result.Members, "repository_members", authorization, actorUserID, subject, window, pages.MemberLimit, pages.MemberCursor); err != nil {
		return nil, err
	}
	if err := paginateActivityPage(s, &result.PRs, "repository_prs", authorization, actorUserID, subject, window, pages.PRLimit, pages.PRCursor); err != nil {
		return nil, err
	}
	if err := paginateActivityPage(s, &result.Commits, "repository_commits", authorization, actorUserID, subject, window, pages.CommitLimit, pages.CommitCursor); err != nil {
		return nil, err
	}
	s.cache.write(ctx, cacheDimensions, result)
	return result, nil
}

func (s *Service) authorizedMemberIdentities(ctx context.Context, authorization *authorizationScope) ([]MemberIdentity, error) {
	if authorization.Admin || authorization.Representative {
		return s.loadCurrentTeamMembers(ctx, authorization, "")
	}
	actor, err := s.client.User.Query().Where(user.IDEQ(authorization.Actor.ID)).Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("load self activity identity: %w", err)
	}
	return []MemberIdentity{{UserID: actor.ID, DisplayName: actor.Username, Email: actor.Email}}, nil
}

func mergePullRequest(items map[prKey]*PullRequest, key prKey, incoming PullRequest) {
	current := items[key]
	if current == nil {
		copy := incoming
		copy.Commits = append([]CommitReference(nil), incoming.Commits...)
		items[key] = &copy
		return
	}
	for _, commit := range incoming.Commits {
		candidate := commitKey{RepoConfigID: commit.RepoConfigID, CommitSHA: commit.CommitSHA}
		if !pullRequestHasCommit(*current, candidate) {
			current.Commits = append(current.Commits, commit)
		}
	}
}

func mergeCommit(items map[commitKey]*Commit, key commitKey, incoming Commit) {
	current := items[key]
	if current == nil {
		copy := incoming
		copy.PRs = append([]PRReference(nil), incoming.PRs...)
		items[key] = &copy
		return
	}
	current.ProcessedTokens += incoming.ProcessedTokens
	if incoming.LatestActivity.After(current.LatestActivity) {
		current.LatestActivity = incoming.LatestActivity.UTC()
	}
	if current.Branch == "" {
		current.Branch = incoming.Branch
	}
	for _, pr := range incoming.PRs {
		key := prKey{RepoConfigID: pr.RepoConfigID, PRRecordID: pr.PRRecordID}
		if !commitHasPR(*current, key) {
			current.PRs = append(current.PRs, pr)
		}
	}
}

func sortRepositoryActivity(result *RepositoryActivity) {
	sort.Slice(result.Members.Items, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(result.Members.Items[i].Member.DisplayName))
		right := strings.ToLower(strings.TrimSpace(result.Members.Items[j].Member.DisplayName))
		if left != right {
			return left < right
		}
		return result.Members.Items[i].Member.UserID < result.Members.Items[j].Member.UserID
	})
	sort.Slice(result.PRs.Items, func(i, j int) bool { return result.PRs.Items[i].PRRecordID < result.PRs.Items[j].PRRecordID })
	sort.Slice(result.Commits.Items, func(i, j int) bool {
		if !result.Commits.Items[i].LatestActivity.Equal(result.Commits.Items[j].LatestActivity) {
			return result.Commits.Items[i].LatestActivity.After(result.Commits.Items[j].LatestActivity)
		}
		return result.Commits.Items[i].CommitSHA < result.Commits.Items[j].CommitSHA
	})
}
