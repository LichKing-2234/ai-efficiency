package activity

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (s *Service) queryV2OverviewSQL(ctx context.Context, actorUserID int, scope *v2Scope, query V2Query, from, to time.Time, denominator V2Denominator, result *V2Overview) (*V2Overview, error) {
	args := []any{s.v2LedgerEpoch, from, to}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	asOf := from
	if denominator.Complete && denominator.Fresh {
		asOf = denominator.AsOf
	}
	args = append(args, asOf, query.Timezone)
	asOfPH, tzPH := fmt.Sprintf("$%d", len(args)-1), fmt.Sprintf("$%d", len(args))
	joins, filter, group := "", "", ""
	if query.PRRecordID > 0 {
		args = append(args, query.PRRecordID)
		filter = " AND c.orphaned=false AND pr.id=" + fmt.Sprintf("$%d", len(args))
		joins = " JOIN attribution_usage_pool_commits c ON c.pool_id=p.id JOIN pr_commit_usage_snapshots pcs ON pcs.commit_sha=c.commit_sha JOIN pr_records pr ON pr.id=pcs.pr_record_id AND pr.repo_config_pr_records=c.repo_config_id"
		group = " GROUP BY p.id,p.total_tokens,p.bucket_start_utc,p.coverage_gap_count"
	} else if query.RepoID > 0 {
		args = append(args, query.RepoID)
		filter = " AND c.orphaned=false AND c.repo_config_id=" + fmt.Sprintf("$%d", len(args))
		joins = " JOIN attribution_usage_pool_commits c ON c.pool_id=p.id"
		group = " GROUP BY p.id,p.total_tokens,p.bucket_start_utc,p.coverage_gap_count"
	}
	relationColumns := "true has_direct,false has_shared"
	if query.PRRecordID > 0 || query.RepoID > 0 {
		relationColumns = "bool_or(c.relation_kind='direct') has_direct,bool_or(c.relation_kind='shared') has_shared"
	}
	statement := fmt.Sprintf(`WITH selected AS (
 SELECT p.id,p.total_tokens,p.bucket_start_utc,p.coverage_gap_count,%s
 FROM attribution_usage_pools p %s
 WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 AND p.user_id IN (%s)
   AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits counted WHERE counted.pool_id=p.id AND counted.orphaned=false AND counted.relation_kind IN ('direct','shared'))
   %s %s
), daily AS (
 SELECT to_char(bucket_start_utc AT TIME ZONE %s,'YYYY-MM-DD') local_day,
        SUM(total_tokens)::bigint total_tokens,
        SUM(CASE WHEN has_direct THEN total_tokens ELSE 0 END)::bigint direct_tokens,
        SUM(CASE WHEN has_shared THEN total_tokens ELSE 0 END)::bigint shared_tokens,
        bool_or(coverage_gap_count>0) has_gap,
        SUM(CASE WHEN bucket_start_utc + interval '15 minutes' <= %s THEN total_tokens ELSE 0 END)::bigint ratio_tokens,
        bool_or(coverage_gap_count>0 AND bucket_start_utc + interval '15 minutes' <= %s) ratio_gap
 FROM selected GROUP BY local_day
)
SELECT local_day,total_tokens,direct_tokens,shared_tokens,has_gap,ratio_tokens,ratio_gap FROM daily ORDER BY local_day`, relationColumns, joins, users, filter, group, tzPH, asOfPH, asOfPH)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query v2 overview: %w", err)
	}
	defer rows.Close()
	trend := map[string]*V2TrendPoint{}
	for index := range result.Trend {
		trend[result.Trend[index].Date] = &result.Trend[index]
	}
	var ratioCommitted int64
	ratioCoverage := V2Coverage{Complete: true}
	for rows.Next() {
		var day string
		var total, direct, shared, ratio int64
		var gap, ratioGap bool
		if err := rows.Scan(&day, &total, &direct, &shared, &gap, &ratio, &ratioGap); err != nil {
			return nil, err
		}
		result.CommittedTokens += total
		ratioCommitted += ratio
		if gap {
			result.Coverage.LowerBound = true
		}
		if ratioGap {
			ratioCoverage.LowerBound = true
			ratioCoverage.Complete = false
		}
		if point := trend[day]; point != nil {
			if query.PRRecordID > 0 {
				point.InvolvedTokens = total
			} else if query.RepoID > 0 {
				point.DirectTokens = direct
				point.SharedTokens = shared
			} else {
				point.DirectTokens = total
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	result.Coverage.Complete = !result.Coverage.LowerBound
	repoIDs, err := s.queryV2RepoIDsSQL(ctx, scope, from, to, query.RepoID, query.PRRecordID)
	if err != nil {
		return nil, err
	}
	result.SCMCoverage, err = s.v2SyncCoverage(ctx, repoIDs)
	if err != nil {
		return nil, err
	}
	result.Readiness, err = s.v2Readiness(ctx, scope.userIDs)
	if err != nil {
		return nil, err
	}
	result.Ratio = v2Ratio(ratioCommitted, ratioCoverage, denominator)
	return result, nil
}

func (s *Service) queryV2RepositoriesSQL(ctx context.Context, actorUserID int, scope *v2Scope, from, to time.Time, query V2PageQuery) (*V2Page[V2RepositoryRow], error) {
	if s.v2LedgerEpoch == "" || len(scope.userIDs) == 0 {
		return &V2Page[V2RepositoryRow]{Items: []V2RepositoryRow{}}, nil
	}
	args := []any{s.v2LedgerEpoch, from, to}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	binding := v2CursorBinding(scope, query)
	lastValue, lastID, err := s.decodeV2PageCursor(query.Cursor, "repositories", scope, actorUserID, binding)
	if err != nil {
		return nil, err
	}
	search := strings.TrimSpace(query.Search)
	args = append(args, "%"+escapeV2Like(search)+"%")
	searchPlaceholder := fmt.Sprintf("$%d", len(args))
	sortKey, order, cursorWhere := "direct_tokens", "direct_tokens DESC, repo_config_id ASC", ""
	if query.Sort == "name" {
		sortKey, order = "repo_name", "repo_name ASC, repo_config_id ASC"
	}
	if lastID > 0 {
		args = append(args, lastValue, lastID)
		value, id := fmt.Sprintf("$%d", len(args)-1), fmt.Sprintf("$%d", len(args))
		if sortKey == "direct_tokens" {
			cursorWhere = "AND (direct_tokens < " + value + "::bigint OR (direct_tokens = " + value + "::bigint AND repo_config_id > " + id + "))"
		} else {
			cursorWhere = "AND (repo_name > " + value + " OR (repo_name = " + value + " AND repo_config_id > " + id + "))"
		}
	}
	statement := fmt.Sprintf(`WITH pool_repo AS (
 SELECT c.repo_config_id, p.id pool_id, p.total_tokens,
        bool_or(c.relation_kind = 'direct') has_direct,
        bool_or(c.relation_kind = 'shared') has_shared
 FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id
 WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3
   AND p.user_id IN (%s) AND c.orphaned=false
 GROUP BY c.repo_config_id,p.id,p.total_tokens
 HAVING bool_or(c.relation_kind IN ('direct','shared'))
), repo_totals AS (
 SELECT pr.repo_config_id,r.full_name repo_name,
        COALESCE(SUM(CASE WHEN pr.has_direct THEN pr.total_tokens ELSE 0 END),0)::bigint direct_tokens,
        COALESCE(SUM(CASE WHEN pr.has_shared THEN pr.total_tokens ELSE 0 END),0)::bigint shared_tokens
 FROM pool_repo pr JOIN repo_configs r ON r.id=pr.repo_config_id GROUP BY pr.repo_config_id,r.full_name
), scope_total AS (
 SELECT COALESCE(SUM(total_tokens),0)::bigint total_tokens FROM attribution_usage_pools
 WHERE ledger_epoch=$1 AND bucket_start_utc >= $2 AND bucket_start_utc < $3 AND user_id IN (%s)
   AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits counted WHERE counted.pool_id=attribution_usage_pools.id AND counted.orphaned=false AND counted.relation_kind IN ('direct','shared'))
)
SELECT repo_config_id,repo_name,direct_tokens,shared_tokens,scope_total.total_tokens
FROM repo_totals CROSS JOIN scope_total WHERE repo_name ILIKE %s ESCAPE '\' %s
ORDER BY %s LIMIT %d`, users, users, searchPlaceholder, cursorWhere, order, v2PageSize+1)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query v2 repositories: %w", err)
	}
	defer rows.Close()
	items := make([]V2RepositoryRow, 0, v2PageSize+1)
	for rows.Next() {
		var row V2RepositoryRow
		var total int64
		if err := rows.Scan(&row.RepoConfigID, &row.Name, &row.DirectTokens, &row.SharedTokens, &total); err != nil {
			return nil, err
		}
		if total > 0 {
			share := float64(row.DirectTokens) * 100 / float64(total)
			row.DirectShare = &share
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	page := &V2Page[V2RepositoryRow]{Items: items}
	if len(items) > v2PageSize {
		page.Items = items[:v2PageSize]
		last := page.Items[len(page.Items)-1]
		value := strconv.FormatInt(last.DirectTokens, 10)
		if sortKey == "repo_name" {
			value = last.Name
		}
		page.NextCursor, err = s.encodeV2PageCursor("repositories", scope, actorUserID, binding, last.RepoConfigID, value)
	}
	return page, err
}

func (s *Service) queryV2PullRequestsSQL(ctx context.Context, actorUserID int, scope *v2Scope, from, to time.Time, query V2PageQuery) (*V2Page[V2PullRequestRow], error) {
	if s.v2LedgerEpoch == "" || len(scope.userIDs) == 0 {
		return &V2Page[V2PullRequestRow]{Items: []V2PullRequestRow{}}, nil
	}
	args := []any{s.v2LedgerEpoch, from, to}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	binding := v2CursorBinding(scope, query)
	lastValue, lastID, err := s.decodeV2PageCursor(query.Cursor, "pull_requests", scope, actorUserID, binding)
	if err != nil {
		return nil, err
	}
	search := strings.TrimSpace(query.Search)
	args = append(args, "%"+escapeV2Like(search)+"%")
	searchPH := fmt.Sprintf("$%d", len(args))
	filters := ""
	if query.RepoID > 0 {
		args = append(args, query.RepoID)
		filters += " AND c.repo_config_id=" + fmt.Sprintf("$%d", len(args))
	}
	if query.PRRecordID > 0 {
		args = append(args, query.PRRecordID)
		filters += " AND pr.id=" + fmt.Sprintf("$%d", len(args))
	}
	sortKey, order, cursorWhere := "involved_tokens", "involved_tokens DESC, pr_record_id ASC", ""
	if query.Sort == "name" {
		sortKey, order = "title", "lower(title) ASC, pr_record_id ASC"
	}
	if lastID > 0 {
		args = append(args, lastValue, lastID)
		value, id := fmt.Sprintf("$%d", len(args)-1), fmt.Sprintf("$%d", len(args))
		if sortKey == "involved_tokens" {
			cursorWhere = "AND (involved_tokens < " + value + "::bigint OR (involved_tokens = " + value + "::bigint AND pr_record_id > " + id + "))"
		} else {
			cursorWhere = "AND (lower(title) > lower(" + value + ") OR (lower(title) = lower(" + value + ") AND pr_record_id > " + id + "))"
		}
	}
	statement := fmt.Sprintf(`WITH pool_pr AS (
 SELECT pr.id pr_record_id,pr.repo_config_pr_records repo_config_id,r.full_name repo_name,pr.scm_pr_id,pr.title,pr.scm_pr_url,pr.status,
        p.id pool_id,p.total_tokens,bool_or(c.relation_kind='shared') has_shared,bool_or(c.relation_kind='direct') has_direct
 FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id
 JOIN pr_commit_usage_snapshots pcs ON pcs.commit_sha=c.commit_sha
 JOIN pr_records pr ON pr.id=pcs.pr_record_id AND pr.repo_config_pr_records=c.repo_config_id
 JOIN repo_configs r ON r.id=pr.repo_config_pr_records
 WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3
   AND p.user_id IN (%s) AND c.orphaned=false
   AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits counted WHERE counted.pool_id=p.id AND counted.orphaned=false AND counted.relation_kind IN ('direct','shared')) %s
 GROUP BY pr.id,pr.repo_config_pr_records,r.full_name,pr.scm_pr_id,pr.title,pr.scm_pr_url,pr.status,p.id,p.total_tokens
), pr_totals AS (
 SELECT pr_record_id,repo_config_id,repo_name,scm_pr_id,title,scm_pr_url,status,SUM(total_tokens)::bigint involved_tokens,
        CASE WHEN bool_or(has_shared) THEN 'shared' WHEN bool_or(has_direct) THEN 'direct' ELSE 'inherited' END overlap_state
 FROM pool_pr GROUP BY pr_record_id,repo_config_id,repo_name,scm_pr_id,title,scm_pr_url,status
)
SELECT pr_record_id,repo_config_id,repo_name,scm_pr_id,title,scm_pr_url,status,involved_tokens,overlap_state
FROM pr_totals WHERE (repo_name||' #'||scm_pr_id::text||' '||title) ILIKE %s ESCAPE '\' %s ORDER BY %s LIMIT %d`, users, filters, searchPH, cursorWhere, order, v2PageSize+1)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query v2 pull requests: %w", err)
	}
	defer rows.Close()
	items := make([]V2PullRequestRow, 0, v2PageSize+1)
	for rows.Next() {
		var row V2PullRequestRow
		if err := rows.Scan(&row.PRRecordID, &row.RepoConfigID, &row.RepositoryName, &row.SCMPRID, &row.Title, &row.URL, &row.Status, &row.InvolvedTokens, &row.OverlapState); err != nil {
			return nil, err
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	page := &V2Page[V2PullRequestRow]{Items: items}
	if len(items) > v2PageSize {
		page.Items = items[:v2PageSize]
		last := page.Items[len(page.Items)-1]
		value := strconv.FormatInt(last.InvolvedTokens, 10)
		if sortKey == "title" {
			value = last.Title
		}
		page.NextCursor, err = s.encodeV2PageCursor("pull_requests", scope, actorUserID, binding, last.PRRecordID, value)
	}
	if err != nil {
		return nil, err
	}
	repoIDs, loadErr := s.queryV2RepoIDsSQL(ctx, scope, from, to, query.RepoID, query.PRRecordID)
	if loadErr != nil {
		return nil, loadErr
	}
	coverage, loadErr := s.v2SyncCoverage(ctx, repoIDs)
	if loadErr != nil {
		return nil, loadErr
	}
	page.SCMCoverage = &coverage
	return page, nil
}

func (s *Service) queryV2RepoIDsSQL(ctx context.Context, scope *v2Scope, from, to time.Time, repoID, prID int) (map[int]struct{}, error) {
	args := []any{s.v2LedgerEpoch, from, to}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	filter, joins := "", ""
	if repoID > 0 {
		args = append(args, repoID)
		filter = " AND c.repo_config_id=" + fmt.Sprintf("$%d", len(args))
	}
	if prID > 0 {
		args = append(args, prID)
		joins = " JOIN pr_commit_usage_snapshots pcs ON pcs.commit_sha=c.commit_sha JOIN pr_records pr ON pr.id=pcs.pr_record_id AND pr.repo_config_pr_records=c.repo_config_id"
		filter += " AND pr.id=" + fmt.Sprintf("$%d", len(args))
	}
	statement := fmt.Sprintf(`SELECT DISTINCT c.repo_config_id FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id %s WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 AND p.user_id IN (%s) AND c.orphaned=false %s`, joins, users, filter)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query v2 repository coverage: %w", err)
	}
	defer rows.Close()
	result := map[int]struct{}{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result[id] = struct{}{}
	}
	return result, rows.Err()
}

func (s *Service) v2ReadinessSQL(ctx context.Context, userIDs map[int]struct{}) (V2Readiness, error) {
	args := []any{s.v2LedgerEpoch}
	users := appendSQLInts(&args, sortedIntKeys(userIDs))
	statement := fmt.Sprintf(`SELECT MIN(p.created_at) FROM attribution_usage_pools p WHERE p.ledger_epoch=$1 AND p.user_id IN (%s) AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits c WHERE c.pool_id=p.id AND c.orphaned=false AND c.relation_kind IN ('direct','shared'))`, users)
	var at sql.NullTime
	if err := s.v2DB.QueryRowContext(ctx, statement, args...).Scan(&at); err != nil {
		return V2Readiness{}, fmt.Errorf("query v2 readiness: %w", err)
	}
	if !at.Valid {
		return V2Readiness{State: "waiting_for_data"}, nil
	}
	value := at.Time.UTC()
	return V2Readiness{State: "active", FirstAcceptedAt: &value}, nil
}

func appendSQLInts(args *[]any, values []int) string {
	placeholders := make([]string, len(values))
	for i, value := range values {
		*args = append(*args, value)
		placeholders[i] = fmt.Sprintf("$%d", len(*args))
	}
	return strings.Join(placeholders, ",")
}
func escapeV2Like(value string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(value)
}
func v2CursorBinding(scope *v2Scope, query V2PageQuery) string {
	return strings.Join([]string{scope.subject, query.FromDate, query.ToDate, query.Timezone, strings.TrimSpace(query.Search), strings.TrimSpace(query.Sort), fmt.Sprint(query.RepoID), fmt.Sprint(query.PRRecordID)}, "|")
}
func (s *Service) decodeV2PageCursor(encoded, collection string, scope *v2Scope, actor int, binding string) (string, int, error) {
	if strings.TrimSpace(encoded) == "" {
		return "", 0, nil
	}
	cursor, err := s.decodeCursor(encoded)
	if err != nil {
		return "", 0, err
	}
	if cursor.ScopeVersion != scope.authorization.Version {
		return "", 0, ErrSnapshotExpired
	}
	if cursor.Version != activityCursorVersion || cursor.Collection != "v2_"+collection || cursor.ActorUserID != actor || cursor.Subject != binding || cursor.LastID <= 0 || cursor.LastValue == "" {
		return "", 0, ErrInvalidCursor
	}
	return cursor.LastValue, cursor.LastID, nil
}
func (s *Service) encodeV2PageCursor(collection string, scope *v2Scope, actor int, binding string, lastID int, lastValue string) (string, error) {
	return s.encodeCursor(activityCursor{Version: activityCursorVersion, Collection: "v2_" + collection, ScopeVersion: scope.authorization.Version, ActorUserID: actor, Subject: binding, LastID: lastID, LastValue: lastValue})
}
