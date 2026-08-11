package activity

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/attributionusagepool"
	"github.com/ai-efficiency/backend/ent/attributionusagepoolcommit"
	"github.com/ai-efficiency/backend/ent/prcommitusagesnapshot"
	"github.com/ai-efficiency/backend/ent/prrecord"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/ent/repoconfig"
)

const v2PageSize = 20

type v2Scope struct {
	authorization *authorizationScope
	userIDs       map[int]struct{}
	subject       string
}

type v2PoolProjection struct {
	pool    *ent.AttributionUsagePool
	commits []*ent.AttributionUsagePoolCommit
}

func (s *Service) V2Overview(ctx context.Context, actorUserID int, query V2Query) (*V2Overview, error) {
	scope, from, to, location, err := s.resolveV2Query(ctx, actorUserID, query)
	if err != nil {
		return nil, err
	}
	result := &V2Overview{ContractVersion: V2MetricContractVersion, ScopeVersion: scope.authorization.Version, FromDate: query.FromDate, ToDate: query.ToDate, Timezone: query.Timezone, Trend: emptyV2Trend(query.FromDate, query.ToDate), Readiness: V2Readiness{State: "waiting_for_data"}}
	if s.v2LedgerEpoch == "" {
		result.Ratio = V2Ratio{State: "denominator_unavailable"}
		return result, nil
	}
	denominator := V2Denominator{}
	if s.v2Denominator != nil {
		denominator, _ = s.v2Denominator.ResolveDenominator(ctx, V2DenominatorRequest{ActorUserID: actorUserID, Scope: query.Scope, SubjectUserID: query.SubjectID, TeamID: query.TeamID, FromDate: query.FromDate, ToDate: query.ToDate, Timezone: query.Timezone, ScopeVersion: scope.authorization.Version})
	}
	if s.v2DB != nil {
		return s.queryV2OverviewSQL(ctx, actorUserID, scope, query, from, to, denominator, result)
	}
	projections, err := s.loadV2Pools(ctx, scope.userIDs, from, to, query.RepoID, query.PRRecordID)
	if err != nil {
		return nil, err
	}
	trend := make(map[string]*V2TrendPoint, len(result.Trend))
	for index := range result.Trend {
		trend[result.Trend[index].Date] = &result.Trend[index]
	}
	var ratioCommitted int64
	ratioCoverage := V2Coverage{Complete: true}
	for _, projection := range projections {
		result.CommittedTokens += projection.pool.TotalTokens
		if denominator.Complete && denominator.Fresh && !projection.pool.BucketStartUtc.Add(15*time.Minute).After(denominator.AsOf) {
			ratioCommitted += projection.pool.TotalTokens
			if projection.pool.CoverageGapCount > 0 {
				ratioCoverage.Complete = false
				ratioCoverage.LowerBound = true
			}
		}
		if projection.pool.CoverageGapCount > 0 {
			result.Coverage.LowerBound = true
		}
		date := projection.pool.BucketStartUtc.In(location).Format("2006-01-02")
		point := trend[date]
		if point == nil {
			continue
		}
		if query.PRRecordID > 0 {
			point.InvolvedTokens += projection.pool.TotalTokens
			continue
		}
		if query.RepoID > 0 {
			direct, shared := v2RelationPresence(projection.commits)
			if direct {
				point.DirectTokens += projection.pool.TotalTokens
			}
			if shared {
				point.SharedTokens += projection.pool.TotalTokens
			}
			continue
		}
		point.DirectTokens += projection.pool.TotalTokens
	}
	repoIDs := map[int]struct{}{}
	for _, projection := range projections {
		for _, commit := range projection.commits {
			repoIDs[commit.RepoConfigID] = struct{}{}
		}
	}
	result.SCMCoverage, err = s.v2SyncCoverage(ctx, repoIDs)
	if err != nil {
		return nil, err
	}
	result.Coverage.Complete = !result.Coverage.LowerBound
	result.Readiness, err = s.v2Readiness(ctx, scope.userIDs)
	if err != nil {
		return nil, err
	}
	result.Ratio = v2Ratio(ratioCommitted, ratioCoverage, denominator)
	return result, nil
}

func (s *Service) V2Repositories(ctx context.Context, actorUserID int, query V2PageQuery) (*V2Page[V2RepositoryRow], error) {
	if err := validateV2PageQuery(query); err != nil {
		return nil, err
	}
	scope, from, to, _, err := s.resolveV2Query(ctx, actorUserID, query.V2Query)
	if err != nil {
		return nil, err
	}
	if s.v2DB != nil {
		return s.queryV2RepositoriesSQL(ctx, actorUserID, scope, from, to, query)
	}
	projections, err := s.loadV2Pools(ctx, scope.userIDs, from, to, 0, 0)
	if err != nil {
		return nil, err
	}
	type aggregate struct{ direct, shared int64 }
	values := map[int]*aggregate{}
	var total int64
	for _, projection := range projections {
		total += projection.pool.TotalTokens
		perRepo := map[int]map[attributionusagepoolcommit.RelationKind]struct{}{}
		for _, commit := range projection.commits {
			if commit.Orphaned {
				continue
			}
			if perRepo[commit.RepoConfigID] == nil {
				perRepo[commit.RepoConfigID] = map[attributionusagepoolcommit.RelationKind]struct{}{}
			}
			perRepo[commit.RepoConfigID][commit.RelationKind] = struct{}{}
		}
		for repoID, relations := range perRepo {
			_, direct := relations[attributionusagepoolcommit.RelationKindDirect]
			_, shared := relations[attributionusagepoolcommit.RelationKindShared]
			if !direct && !shared {
				continue
			}
			value := values[repoID]
			if value == nil {
				value = &aggregate{}
				values[repoID] = value
			}
			if direct {
				value.direct += projection.pool.TotalTokens
			}
			if shared {
				value.shared += projection.pool.TotalTokens
			}
		}
	}
	repoIDs := make([]int, 0, len(values))
	for id := range values {
		repoIDs = append(repoIDs, id)
	}
	repos, err := s.client.RepoConfig.Query().Where(repoconfig.IDIn(repoIDs...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load v2 repositories: %w", err)
	}
	rows := make([]V2RepositoryRow, 0, len(repos))
	search := strings.ToLower(strings.TrimSpace(query.Search))
	for _, repo := range repos {
		if search != "" && !strings.Contains(strings.ToLower(repo.FullName), search) {
			continue
		}
		value := values[repo.ID]
		row := V2RepositoryRow{RepoConfigID: repo.ID, Name: repo.FullName, DirectTokens: value.direct, SharedTokens: value.shared}
		if total > 0 {
			share := float64(value.direct) * 100 / float64(total)
			row.DirectShare = &share
		}
		rows = append(rows, row)
	}
	sortV2Repositories(rows, query.Sort)
	return paginateV2(s, rows, "repositories", scope, actorUserID, query, func(row V2RepositoryRow) int { return row.RepoConfigID })
}

func (s *Service) V2PullRequests(ctx context.Context, actorUserID int, query V2PageQuery) (*V2Page[V2PullRequestRow], error) {
	if err := validateV2PageQuery(query); err != nil {
		return nil, err
	}
	scope, from, to, _, err := s.resolveV2Query(ctx, actorUserID, query.V2Query)
	if err != nil {
		return nil, err
	}
	if s.v2DB != nil {
		return s.queryV2PullRequestsSQL(ctx, actorUserID, scope, from, to, query)
	}
	projections, err := s.loadV2Pools(ctx, scope.userIDs, from, to, query.RepoID, 0)
	if err != nil {
		return nil, err
	}
	poolByCommit := map[string]map[int]*v2PoolProjection{}
	for _, projection := range projections {
		for _, commit := range projection.commits {
			if commit.Orphaned {
				continue
			}
			key := fmt.Sprintf("%d:%s", commit.RepoConfigID, commit.CommitSha)
			if poolByCommit[key] == nil {
				poolByCommit[key] = map[int]*v2PoolProjection{}
			}
			copy := projection
			poolByCommit[key][projection.pool.ID] = &copy
		}
	}
	if len(poolByCommit) == 0 {
		return &V2Page[V2PullRequestRow]{Items: []V2PullRequestRow{}}, nil
	}
	snapshots, err := s.client.PRCommitUsageSnapshot.Query().Where(prcommitusagesnapshot.CommitShaIn(v2CommitSHAs(poolByCommit)...)).WithPrRecord(func(q *ent.PrRecordQuery) { q.WithRepoConfig() }).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load v2 PR projection: %w", err)
	}
	tokensByPR := map[int]map[int]*v2PoolProjection{}
	prs := map[int]*ent.PrRecord{}
	for _, snapshot := range snapshots {
		pr, edgeErr := snapshot.Edges.PrRecordOrErr()
		if edgeErr != nil {
			continue
		}
		repo, edgeErr := pr.Edges.RepoConfigOrErr()
		if edgeErr != nil {
			continue
		}
		pools := poolByCommit[fmt.Sprintf("%d:%s", repo.ID, snapshot.CommitSha)]
		if len(pools) == 0 {
			continue
		}
		if tokensByPR[pr.ID] == nil {
			tokensByPR[pr.ID] = map[int]*v2PoolProjection{}
		}
		for id, pool := range pools {
			tokensByPR[pr.ID][id] = pool
		}
		prs[pr.ID] = pr
	}
	rows := make([]V2PullRequestRow, 0, len(prs))
	search := strings.ToLower(strings.TrimSpace(query.Search))
	for id, pr := range prs {
		repo, _ := pr.Edges.RepoConfigOrErr()
		if query.PRRecordID > 0 && id != query.PRRecordID {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(fmt.Sprintf("%s #%d %s", repo.FullName, pr.ScmPrID, pr.Title)), search) {
			continue
		}
		var tokens int64
		anyShared, anyDirect := false, false
		for _, projection := range tokensByPR[id] {
			tokens += projection.pool.TotalTokens
			direct, shared := v2RelationPresence(projection.commits)
			anyShared = anyShared || shared
			anyDirect = anyDirect || direct
		}
		overlap := "inherited"
		if anyDirect {
			overlap = "direct"
		}
		if anyShared {
			overlap = "shared"
		}
		rows = append(rows, V2PullRequestRow{PRRecordID: id, RepoConfigID: repo.ID, RepositoryName: repo.FullName, SCMPRID: pr.ScmPrID, Title: pr.Title, URL: pr.ScmPrURL, Status: string(pr.Status), InvolvedTokens: tokens, OverlapState: overlap})
	}
	sortV2PRs(rows, query.Sort)
	page, err := paginateV2(s, rows, "pull_requests", scope, actorUserID, query, func(row V2PullRequestRow) int { return row.PRRecordID })
	if err != nil {
		return nil, err
	}
	repoIDs := map[int]struct{}{}
	for _, projection := range projections {
		for _, commit := range projection.commits {
			repoIDs[commit.RepoConfigID] = struct{}{}
		}
	}
	coverage, err := s.v2SyncCoverage(ctx, repoIDs)
	if err != nil {
		return nil, err
	}
	page.SCMCoverage = &coverage
	return page, nil
}

func (s *Service) resolveV2Query(ctx context.Context, actorUserID int, query V2Query) (*v2Scope, time.Time, time.Time, *time.Location, error) {
	if s == nil || s.client == nil {
		return nil, time.Time{}, time.Time{}, nil, errors.New("activity service is not configured")
	}
	location, err := time.LoadLocation(strings.TrimSpace(query.Timezone))
	if err != nil {
		return nil, time.Time{}, time.Time{}, nil, fmt.Errorf("%w: timezone", ErrInvalidQuery)
	}
	fromDay, err := time.ParseInLocation("2006-01-02", query.FromDate, location)
	if err != nil {
		return nil, time.Time{}, time.Time{}, nil, fmt.Errorf("%w: from", ErrInvalidQuery)
	}
	toDay, err := time.ParseInLocation("2006-01-02", query.ToDate, location)
	if err != nil || toDay.Before(fromDay) || fromDay.Before(toDay.AddDate(0, 0, -89)) {
		return nil, time.Time{}, time.Time{}, nil, fmt.Errorf("%w: to", ErrInvalidQuery)
	}
	authorization, err := s.resolveAuthorization(ctx, actorUserID)
	if err != nil {
		return nil, time.Time{}, time.Time{}, nil, err
	}
	scope := &v2Scope{authorization: authorization, userIDs: map[int]struct{}{}, subject: string(query.Scope)}
	switch query.Scope {
	case V2ScopePersonal:
		scope.userIDs[actorUserID] = struct{}{}
	case V2ScopeMember:
		if _, ok := authorization.AllowedUserIDs[query.SubjectID]; !ok {
			return nil, time.Time{}, time.Time{}, nil, ErrForbidden
		}
		scope.userIDs[query.SubjectID] = struct{}{}
		scope.subject += fmt.Sprintf(":%d", query.SubjectID)
	case V2ScopeTeam:
		if _, ok := authorization.Teams[query.TeamID]; !ok || (!authorization.Admin && !authorization.Representative) {
			return nil, time.Time{}, time.Time{}, nil, ErrForbidden
		}
		identities, loadErr := s.loadCurrentTeamMembers(ctx, authorization, query.TeamID)
		if loadErr != nil {
			return nil, time.Time{}, time.Time{}, nil, loadErr
		}
		for _, identity := range identities {
			if identity.UserID > 0 {
				scope.userIDs[identity.UserID] = struct{}{}
			}
		}
		scope.subject += ":" + query.TeamID
	default:
		return nil, time.Time{}, time.Time{}, nil, fmt.Errorf("%w: scope", ErrInvalidQuery)
	}
	return scope, fromDay.UTC(), toDay.AddDate(0, 0, 1).UTC(), location, nil
}

func validateV2PageQuery(query V2PageQuery) error {
	if sortKey := strings.TrimSpace(query.Sort); sortKey != "" && sortKey != "tokens" && sortKey != "name" {
		return fmt.Errorf("%w: sort", ErrInvalidQuery)
	}
	if len([]rune(strings.TrimSpace(query.Search))) > 100 {
		return fmt.Errorf("%w: search", ErrInvalidQuery)
	}
	return nil
}

func (s *Service) loadV2Pools(ctx context.Context, userIDs map[int]struct{}, from, to time.Time, repoID, prID int) ([]v2PoolProjection, error) {
	if s.v2LedgerEpoch == "" || len(userIDs) == 0 {
		return []v2PoolProjection{}, nil
	}
	ids := sortedIntKeys(userIDs)
	pools, err := s.client.AttributionUsagePool.Query().Where(attributionusagepool.LedgerEpochEQ(s.v2LedgerEpoch), attributionusagepool.UserIDIn(ids...), attributionusagepool.BucketStartUtcGTE(from), attributionusagepool.BucketStartUtcLT(to)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load v2 usage pools: %w", err)
	}
	if len(pools) == 0 {
		return []v2PoolProjection{}, nil
	}
	poolIDs := make([]int, 0, len(pools))
	byID := map[int]*v2PoolProjection{}
	for _, pool := range pools {
		poolIDs = append(poolIDs, pool.ID)
		byID[pool.ID] = &v2PoolProjection{pool: pool}
	}
	commitsQuery := s.client.AttributionUsagePoolCommit.Query().Where(attributionusagepoolcommit.PoolIDIn(poolIDs...), attributionusagepoolcommit.OrphanedEQ(false))
	commits, err := commitsQuery.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("load v2 pool commits: %w", err)
	}
	selectedCommits := commits
	if repoID > 0 {
		selectedCommits = selectedCommits[:0]
		for _, commit := range commits {
			if commit.RepoConfigID == repoID {
				selectedCommits = append(selectedCommits, commit)
			}
		}
	}
	if prID > 0 {
		pr, err := s.client.PrRecord.Query().Where(prrecord.IDEQ(prID)).WithRepoConfig().Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("load v2 PR: %w", err)
		}
		repo, _ := pr.Edges.RepoConfigOrErr()
		snapshots, err := s.client.PRCommitUsageSnapshot.Query().Where(prcommitusagesnapshot.PrRecordIDEQ(prID)).All(ctx)
		if err != nil {
			return nil, fmt.Errorf("load v2 PR commits: %w", err)
		}
		allowed := map[string]struct{}{}
		for _, snapshot := range snapshots {
			allowed[snapshot.CommitSha] = struct{}{}
		}
		filtered := selectedCommits[:0]
		for _, commit := range selectedCommits {
			if commit.RepoConfigID == repo.ID {
				if _, ok := allowed[commit.CommitSha]; ok {
					filtered = append(filtered, commit)
				}
			}
		}
		selectedCommits = filtered
	}
	countingPools := map[int]struct{}{}
	for _, commit := range commits {
		if commit.RelationKind == attributionusagepoolcommit.RelationKindDirect || commit.RelationKind == attributionusagepoolcommit.RelationKindShared {
			countingPools[commit.PoolID] = struct{}{}
		}
	}
	for _, commit := range selectedCommits {
		if projection := byID[commit.PoolID]; projection != nil {
			projection.commits = append(projection.commits, commit)
		}
	}
	result := make([]v2PoolProjection, 0, len(byID))
	for _, pool := range pools {
		projection := byID[pool.ID]
		if _, counted := countingPools[pool.ID]; !counted {
			continue
		}
		if (repoID > 0 || prID > 0) && len(projection.commits) == 0 {
			continue
		}
		result = append(result, *projection)
	}
	return result, nil
}

func (s *Service) v2Readiness(ctx context.Context, userIDs map[int]struct{}) (V2Readiness, error) {
	if s.v2LedgerEpoch == "" || len(userIDs) == 0 {
		return V2Readiness{State: "waiting_for_data"}, nil
	}
	if s.v2DB != nil {
		return s.v2ReadinessSQL(ctx, userIDs)
	}
	pools, err := s.client.AttributionUsagePool.Query().Where(attributionusagepool.LedgerEpochEQ(s.v2LedgerEpoch), attributionusagepool.UserIDIn(sortedIntKeys(userIDs)...)).Order(ent.Asc(attributionusagepool.FieldCreatedAt)).All(ctx)
	if err != nil {
		return V2Readiness{}, fmt.Errorf("load v2 readiness: %w", err)
	}
	if len(pools) == 0 {
		return V2Readiness{State: "waiting_for_data"}, nil
	}
	poolIDs := make([]int, 0, len(pools))
	for _, pool := range pools {
		poolIDs = append(poolIDs, pool.ID)
	}
	commits, err := s.client.AttributionUsagePoolCommit.Query().Where(attributionusagepoolcommit.PoolIDIn(poolIDs...), attributionusagepoolcommit.OrphanedEQ(false), attributionusagepoolcommit.RelationKindIn(attributionusagepoolcommit.RelationKindDirect, attributionusagepoolcommit.RelationKindShared)).All(ctx)
	if err != nil {
		return V2Readiness{}, err
	}
	if len(commits) == 0 {
		return V2Readiness{State: "waiting_for_data"}, nil
	}
	committedPoolIDs := map[int]struct{}{}
	for _, commit := range commits {
		committedPoolIDs[commit.PoolID] = struct{}{}
	}
	at := pools[0].CreatedAt.UTC()
	for _, pool := range pools {
		if _, exists := committedPoolIDs[pool.ID]; exists {
			at = pool.CreatedAt.UTC()
			break
		}
	}
	return V2Readiness{State: "active", FirstAcceptedAt: &at}, nil
}

func (s *Service) v2SyncCoverage(ctx context.Context, repoIDs map[int]struct{}) (SyncCoverage, error) {
	if len(repoIDs) == 0 {
		return SyncCoverage{Complete: true}, nil
	}
	jobs, err := s.client.PRSyncJob.Query().Where(prsyncjob.RepoConfigIDIn(sortedIntKeys(repoIDs)...), latestPRSyncJobPredicate()).All(ctx)
	if err != nil {
		return SyncCoverage{}, fmt.Errorf("load v2 PR sync coverage: %w", err)
	}
	byRepo := map[int]*ent.PRSyncJob{}
	for _, job := range jobs {
		byRepo[job.RepoConfigID] = job
	}
	return coverageForRepositories(repoIDs, byRepo, s.currentTime()), nil
}

func v2Ratio(committed int64, coverage V2Coverage, d V2Denominator) V2Ratio {
	r := V2Ratio{State: "denominator_unavailable", CommittedTokens: committed}
	if !d.Fresh || !d.Complete {
		return r
	}
	total := d.TotalTokens
	r.TotalTokens = &total
	asOf := d.AsOf.UTC()
	r.AsOf = &asOf
	if total == 0 {
		r.State = "complete_zero_usage"
		return r
	}
	percent := float64(committed) * 100 / float64(total)
	if percent > 100 {
		percent = 100
	}
	r.Percent = &percent
	if committed == 0 {
		r.State = "true_zero_committed"
	} else if coverage.LowerBound {
		r.State = "lower_bound"
	} else {
		r.State = "exact"
	}
	return r
}

func v2RelationPresence(commits []*ent.AttributionUsagePoolCommit) (bool, bool) {
	direct, shared := false, false
	for _, commit := range commits {
		if commit.RelationKind == attributionusagepoolcommit.RelationKindDirect {
			direct = true
		}
		if commit.RelationKind == attributionusagepoolcommit.RelationKindShared {
			shared = true
		}
	}
	return direct, shared
}
func emptyV2Trend(from, to string) []V2TrendPoint {
	start, _ := time.Parse("2006-01-02", from)
	end, _ := time.Parse("2006-01-02", to)
	result := []V2TrendPoint{}
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		result = append(result, V2TrendPoint{Date: day.Format("2006-01-02")})
	}
	return result
}
func v2CommitSHAs(values map[string]map[int]*v2PoolProjection) []string {
	seen := map[string]struct{}{}
	for key := range values {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) == 2 {
			seen[parts[1]] = struct{}{}
		}
	}
	result := make([]string, 0, len(seen))
	for sha := range seen {
		result = append(result, sha)
	}
	return result
}
func sortV2Repositories(rows []V2RepositoryRow, sortKey string) {
	sort.Slice(rows, func(i, j int) bool {
		if sortKey == "name" {
			if rows[i].Name != rows[j].Name {
				return rows[i].Name < rows[j].Name
			}
		} else if rows[i].DirectTokens != rows[j].DirectTokens {
			return rows[i].DirectTokens > rows[j].DirectTokens
		}
		return rows[i].RepoConfigID < rows[j].RepoConfigID
	})
}
func sortV2PRs(rows []V2PullRequestRow, sortKey string) {
	sort.Slice(rows, func(i, j int) bool {
		if sortKey == "name" {
			left, right := strings.ToLower(rows[i].Title), strings.ToLower(rows[j].Title)
			if left != right {
				return left < right
			}
		} else if rows[i].InvolvedTokens != rows[j].InvolvedTokens {
			return rows[i].InvolvedTokens > rows[j].InvolvedTokens
		}
		return rows[i].PRRecordID < rows[j].PRRecordID
	})
}

func paginateV2[T any](s *Service, rows []T, collection string, scope *v2Scope, actorUserID int, query V2PageQuery, identity func(T) int) (*V2Page[T], error) {
	binding := strings.Join([]string{scope.subject, query.FromDate, query.ToDate, query.Timezone, strings.TrimSpace(query.Search), strings.TrimSpace(query.Sort), fmt.Sprint(query.RepoID), fmt.Sprint(query.PRRecordID)}, "|")
	offset := 0
	if strings.TrimSpace(query.Cursor) != "" {
		cursor, err := s.decodeCursor(query.Cursor)
		if err != nil {
			return nil, err
		}
		if cursor.ScopeVersion != scope.authorization.Version {
			return nil, ErrSnapshotExpired
		}
		if cursor.Version != activityCursorVersion || cursor.Collection != "v2_"+collection || cursor.ActorUserID != actorUserID || cursor.Subject != binding || cursor.LastID <= 0 {
			return nil, ErrInvalidCursor
		}
		found := false
		for index, row := range rows {
			if identity(row) == cursor.LastID {
				offset = index + 1
				found = true
				break
			}
		}
		if !found {
			return nil, ErrSnapshotExpired
		}
	}
	end := offset + v2PageSize
	if end > len(rows) {
		end = len(rows)
	}
	page := &V2Page[T]{Items: append([]T(nil), rows[offset:end]...)}
	if end < len(rows) {
		cursor, err := s.encodeCursor(activityCursor{Version: activityCursorVersion, Collection: "v2_" + collection, ScopeVersion: scope.authorization.Version, ActorUserID: actorUserID, Subject: binding, LastID: identity(rows[end-1])})
		if err != nil {
			return nil, err
		}
		page.NextCursor = cursor
	}
	return page, nil
}
