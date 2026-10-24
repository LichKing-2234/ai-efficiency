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
	committed, committedCredit, ratioCommitted, hasGap, ratioGap, providerMismatch, err := s.queryV2ScopeTotalsSQL(ctx, scope, from, to, denominator)
	if err != nil {
		return nil, fmt.Errorf("load v2 overview totals: %w", err)
	}
	result.CommittedTokens = committed
	result.CommittedCredit = committedCredit
	result.Coverage = V2Coverage{Complete: !hasGap, LowerBound: hasGap}
	ratioCoverage := V2Coverage{Complete: !ratioGap, LowerBound: ratioGap}
	if providerMismatch {
		denominator = V2Denominator{Retryable: denominator.Retryable}
	}
	args := []any{s.v2LedgerEpoch, from, to}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	args = append(args, query.Timezone)
	tzPH := fmt.Sprintf("$%d", len(args))
	joins, filter, group := "", "", ""
	if query.PRRecordID > 0 {
		args = append(args, query.PRRecordID)
		filter = " AND pr.id=" + fmt.Sprintf("$%d", len(args))
		joins = " JOIN attribution_usage_pool_commits c ON c.pool_id=p.id JOIN pr_commit_usage_snapshots pcs ON pcs.commit_sha=c.commit_sha JOIN pr_records pr ON pr.id=pcs.pr_record_id AND pr.repo_config_pr_records=c.repo_config_id"
		group = " GROUP BY p.id,p.total_tokens,p.credit_usage,p.bucket_start_utc,p.coverage_gap_count"
	} else if query.RepoID > 0 {
		args = append(args, query.RepoID)
		filter = " AND c.repo_config_id=" + fmt.Sprintf("$%d", len(args))
		joins = " JOIN attribution_usage_pool_commits c ON c.pool_id=p.id"
		group = " GROUP BY p.id,p.total_tokens,p.credit_usage,p.bucket_start_utc,p.coverage_gap_count"
	}
	relationColumns := "true has_direct,false has_shared"
	if query.PRRecordID > 0 || query.RepoID > 0 {
		relationColumns = "bool_or(c.relation_kind='direct') has_direct,bool_or(c.relation_kind='shared') has_shared"
	}
	statement := fmt.Sprintf(`WITH selected AS (
 SELECT p.id,p.total_tokens,p.credit_usage,p.bucket_start_utc,p.coverage_gap_count,%s
 FROM attribution_usage_pools p %s
	 WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 AND p.user_id IN (%s) AND p.relay_provider_id > 0
	   AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits counted WHERE counted.pool_id=p.id AND counted.relation_kind IN ('direct','shared'))
   %s %s
), daily AS (
 SELECT to_char(bucket_start_utc AT TIME ZONE %s,'YYYY-MM-DD') local_day,
        SUM(total_tokens)::bigint total_tokens,
        SUM(CASE WHEN has_direct THEN total_tokens ELSE 0 END)::bigint direct_tokens,
        SUM(CASE WHEN has_shared THEN total_tokens ELSE 0 END)::bigint shared_tokens,
        SUM(credit_usage)::double precision credit_usage,
        SUM(CASE WHEN has_direct THEN credit_usage ELSE 0 END)::double precision direct_credit,
        SUM(CASE WHEN has_shared THEN credit_usage ELSE 0 END)::double precision shared_credit
 FROM selected GROUP BY local_day
)
SELECT local_day,total_tokens,direct_tokens,shared_tokens,credit_usage,direct_credit,shared_credit FROM daily ORDER BY local_day`, relationColumns, joins, users, filter, group, tzPH)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query v2 overview: %w", err)
	}
	defer rows.Close()
	trend := map[string]*V2TrendPoint{}
	for index := range result.Trend {
		trend[result.Trend[index].Date] = &result.Trend[index]
	}
	for rows.Next() {
		var day string
		var total, direct, shared int64
		var totalCredit, directCredit, sharedCredit float64
		if err := rows.Scan(&day, &total, &direct, &shared, &totalCredit, &directCredit, &sharedCredit); err != nil {
			return nil, fmt.Errorf("scan v2 overview: %w", err)
		}
		if point := trend[day]; point != nil {
			if query.PRRecordID > 0 {
				point.InvolvedTokens = total
				point.InvolvedCredit = totalCredit
			} else if query.RepoID > 0 {
				point.DirectTokens = direct
				point.SharedTokens = shared
				point.DirectCredit = directCredit
				point.SharedCredit = sharedCredit
			} else {
				point.DirectTokens = total
				point.DirectCredit = totalCredit
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate v2 overview: %w", err)
	}
	repoIDs, err := s.queryV2RepoIDsSQL(ctx, scope, from, to, query.RepoID, query.PRRecordID)
	if err != nil {
		return nil, fmt.Errorf("load v2 overview repository coverage: %w", err)
	}
	result.SCMCoverage, err = s.v2SyncCoverage(ctx, repoIDs)
	if err != nil {
		return nil, fmt.Errorf("resolve v2 overview SCM coverage: %w", err)
	}
	result.Readiness, err = s.v2ReadinessSQL(ctx, scope)
	if err != nil {
		return nil, fmt.Errorf("load v2 overview readiness: %w", err)
	}
	result.Ratio = v2Ratio(ratioCommitted, ratioCoverage, denominator)
	totalCredit, err := s.queryV2CreditDenominatorSQL(ctx, scope, from, to)
	if err != nil {
		return nil, fmt.Errorf("load v2 credit denominator: %w", err)
	}
	result.CreditRatio = v2CreditRatio(committedCredit, totalCredit, result.Coverage)
	return result, nil
}

// queryV2ScopeTotalsSQL returns committed tokens, committed credit, and the
// ratio numerator. Credit stays out of the ratio: its denominator is relay Token
// consumption, and credit is a unit the relay never billed.
func (s *Service) queryV2ScopeTotalsSQL(ctx context.Context, scope *v2Scope, from, to time.Time, denominator V2Denominator) (int64, float64, int64, bool, bool, bool, error) {
	args := []any{s.v2LedgerEpoch, from, to}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	providers := appendSQLInts(&args, sortedIntKeys(scope.providerIDs))
	asOf := from
	if denominator.Complete && denominator.Fresh {
		asOf = denominator.AsOf
	}
	args = append(args, asOf)
	mismatch := fmt.Sprintf("relay_provider_id NOT IN (%s)", providers)
	if len(scope.providerIDs) == 0 {
		mismatch = "true"
	}
	statement := fmt.Sprintf(`WITH scoped AS (SELECT p.* FROM attribution_usage_pools p WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 AND p.user_id IN (%s) AND p.relay_provider_id > 0 AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits c WHERE c.pool_id=p.id AND c.relation_kind IN ('direct','shared'))) SELECT COALESCE(SUM(total_tokens),0)::bigint,COALESCE(SUM(credit_usage),0)::double precision,COALESCE(SUM(total_tokens) FILTER (WHERE relay_provider_id IN (%s) AND bucket_start_utc + interval '15 minutes' <= $%d),0)::bigint,COALESCE(bool_or(coverage_gap_count>0),false),COALESCE(bool_or(coverage_gap_count>0 AND bucket_start_utc + interval '15 minutes' <= $%d) FILTER (WHERE relay_provider_id IN (%s)),false),COALESCE(bool_or(%s),false) FROM scoped`, users, providers, len(args), len(args), providers, mismatch)
	var committed, ratio int64
	var committedCredit float64
	var gap, ratioGap, providerMismatch bool
	if err := s.v2DB.QueryRowContext(ctx, statement, args...).Scan(&committed, &committedCredit, &ratio, &gap, &ratioGap, &providerMismatch); err != nil {
		return 0, 0, 0, false, false, false, fmt.Errorf("query v2 scope totals: %w", err)
	}
	return committed, committedCredit, ratio, gap, ratioGap, providerMismatch, nil
}

// queryV2CreditDenominatorSQL sums the credit this scope reported in the window.
//
// Its source is deliberately different from the Token denominator's. Tokens are
// counted from the gateway's billing record, which no client can influence.
// Credit never reaches the gateway — the agents that bill in it do not route
// through the relay — so the only account of it is what the CLI collected and
// uploaded. The ratio built on it therefore measures how much of the credit this
// machine reported reached a commit, and cannot see credit the agent never
// reported. V2CreditRatio says so in its own type rather than borrowing V2Ratio.
func (s *Service) queryV2CreditDenominatorSQL(ctx context.Context, scope *v2Scope, from, to time.Time) (float64, error) {
	if len(scope.userIDs) == 0 {
		return 0, nil
	}
	args := []any{from, to}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	statement := fmt.Sprintf(`SELECT COALESCE(SUM(credit_usage),0)::double precision FROM tool_usage_events WHERE observed_start_at >= $1 AND observed_start_at < $2 AND user_id IN (%s)`, users)
	var total float64
	if err := s.v2DB.QueryRowContext(ctx, statement, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("query v2 credit denominator: %w", err)
	}
	return total, nil
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
		return nil, fmt.Errorf("decode v2 repository cursor: %w", err)
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
 SELECT c.repo_config_id, p.id pool_id, p.total_tokens, p.credit_usage,
        bool_or(c.relation_kind = 'direct') has_direct,
        bool_or(c.relation_kind = 'shared') has_shared
 FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id
 WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3
	   AND p.user_id IN (%s) AND p.relay_provider_id > 0
 GROUP BY c.repo_config_id,p.id,p.total_tokens,p.credit_usage
 HAVING bool_or(c.relation_kind IN ('direct','shared'))
), repo_totals AS (
 SELECT pr.repo_config_id,r.full_name repo_name,
        COALESCE(SUM(CASE WHEN pr.has_direct THEN pr.total_tokens ELSE 0 END),0)::bigint direct_tokens,
        COALESCE(SUM(CASE WHEN pr.has_shared THEN pr.total_tokens ELSE 0 END),0)::bigint shared_tokens,
        COALESCE(SUM(CASE WHEN pr.has_direct THEN pr.credit_usage ELSE 0 END),0)::double precision direct_credit,
        COALESCE(SUM(CASE WHEN pr.has_shared THEN pr.credit_usage ELSE 0 END),0)::double precision shared_credit
 FROM pool_repo pr JOIN repo_configs r ON r.id=pr.repo_config_id GROUP BY pr.repo_config_id,r.full_name
), scope_total AS (
 SELECT COALESCE(SUM(total_tokens),0)::bigint total_tokens,
        COALESCE(SUM(credit_usage),0)::double precision total_credit FROM attribution_usage_pools
	 WHERE ledger_epoch=$1 AND bucket_start_utc >= $2 AND bucket_start_utc < $3 AND user_id IN (%s) AND relay_provider_id > 0
	   AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits counted WHERE counted.pool_id=attribution_usage_pools.id AND counted.relation_kind IN ('direct','shared'))
)
SELECT repo_config_id,repo_name,direct_tokens,shared_tokens,direct_credit,shared_credit,scope_total.total_tokens,scope_total.total_credit
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
		var totalCredit float64
		if err := rows.Scan(&row.RepoConfigID, &row.Name, &row.DirectTokens, &row.SharedTokens, &row.DirectCredit, &row.SharedCredit, &total, &totalCredit); err != nil {
			return nil, fmt.Errorf("scan v2 repositories: %w", err)
		}
		if total > 0 {
			share := float64(row.DirectTokens) * 100 / float64(total)
			row.DirectShare = &share
		}
		if totalCredit > 0 {
			creditShare := row.DirectCredit * 100 / totalCredit
			row.CreditShare = &creditShare
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate v2 repositories: %w", err)
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
		return nil, fmt.Errorf("decode v2 pull-request cursor: %w", err)
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
        p.id pool_id,p.total_tokens,p.credit_usage,bool_or(c.relation_kind='shared') has_shared,bool_or(c.relation_kind='direct') has_direct
 FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id
 JOIN pr_commit_usage_snapshots pcs ON pcs.commit_sha=c.commit_sha
 JOIN pr_records pr ON pr.id=pcs.pr_record_id AND pr.repo_config_pr_records=c.repo_config_id
 JOIN repo_configs r ON r.id=pr.repo_config_pr_records
 WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3
	   AND p.user_id IN (%s) AND p.relay_provider_id > 0
	   AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits counted WHERE counted.pool_id=p.id AND counted.relation_kind IN ('direct','shared')) %s
 GROUP BY pr.id,pr.repo_config_pr_records,r.full_name,pr.scm_pr_id,pr.title,pr.scm_pr_url,pr.status,p.id,p.total_tokens,p.credit_usage
), pr_totals AS (
 SELECT pr_record_id,repo_config_id,repo_name,scm_pr_id,title,scm_pr_url,status,SUM(total_tokens)::bigint involved_tokens,
        SUM(credit_usage)::double precision involved_credit,
        CASE WHEN bool_or(has_shared) THEN 'shared' WHEN bool_or(has_direct) THEN 'direct' ELSE 'inherited' END overlap_state
 FROM pool_pr GROUP BY pr_record_id,repo_config_id,repo_name,scm_pr_id,title,scm_pr_url,status
)
SELECT pr_record_id,repo_config_id,repo_name,scm_pr_id,title,scm_pr_url,status,involved_tokens,involved_credit,overlap_state
FROM pr_totals WHERE (repo_name||' #'||scm_pr_id::text||' '||title) ILIKE %s ESCAPE '\' %s ORDER BY %s LIMIT %d`, users, filters, searchPH, cursorWhere, order, v2PageSize+1)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query v2 pull requests: %w", err)
	}
	defer rows.Close()
	items := make([]V2PullRequestRow, 0, v2PageSize+1)
	for rows.Next() {
		var row V2PullRequestRow
		if err := rows.Scan(&row.PRRecordID, &row.RepoConfigID, &row.RepositoryName, &row.SCMPRID, &row.Title, &row.URL, &row.Status, &row.InvolvedTokens, &row.InvolvedCredit, &row.OverlapState); err != nil {
			return nil, fmt.Errorf("scan v2 pull requests: %w", err)
		}
		items = append(items, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate v2 pull requests: %w", err)
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
		return nil, fmt.Errorf("encode v2 pull-request cursor: %w", err)
	}
	repoIDs, loadErr := s.queryV2RepoIDsSQL(ctx, scope, from, to, query.RepoID, query.PRRecordID)
	if loadErr != nil {
		return nil, fmt.Errorf("load v2 pull-request repository coverage: %w", loadErr)
	}
	coverage, loadErr := s.v2SyncCoverage(ctx, repoIDs)
	if loadErr != nil {
		return nil, fmt.Errorf("resolve v2 pull-request SCM coverage: %w", loadErr)
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
	statement := fmt.Sprintf(`SELECT DISTINCT c.repo_config_id FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id %s WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 AND p.user_id IN (%s) AND p.relay_provider_id > 0 AND c.relation_kind IN ('direct','shared') %s`, joins, users, filter)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("query v2 repository coverage: %w", err)
	}
	defer rows.Close()
	result := map[int]struct{}{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan v2 repository coverage: %w", err)
		}
		result[id] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate v2 repository coverage: %w", err)
	}
	if repoID == 0 && prID == 0 {
		return result, nil
	}
	if repoID > 0 {
		authorized, err := s.v2RepoAuthorizedSQL(ctx, scope, repoID)
		if err != nil {
			return nil, err
		}
		if authorized {
			result[repoID] = struct{}{}
		}
	}
	if prID > 0 {
		var resolvedRepoID int
		if err := s.v2DB.QueryRowContext(ctx, `SELECT repo_config_pr_records FROM pr_records WHERE id=$1`, prID).Scan(&resolvedRepoID); err != nil && err != sql.ErrNoRows {
			return nil, fmt.Errorf("resolve v2 PR repository coverage: %w", err)
		}
		authorized, err := s.v2RepoAuthorizedSQL(ctx, scope, resolvedRepoID)
		if err != nil {
			return nil, err
		}
		if authorized {
			result[resolvedRepoID] = struct{}{}
		}
	}
	return result, nil
}

func (s *Service) v2RepoAuthorizedSQL(ctx context.Context, scope *v2Scope, repoID int) (bool, error) {
	if repoID <= 0 {
		return false, nil
	}
	args := []any{s.v2LedgerEpoch}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	args = append(args, repoID)
	statement := fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id WHERE p.ledger_epoch=$1 AND p.user_id IN (%s) AND p.relay_provider_id > 0 AND c.repo_config_id=$%d AND c.relation_kind IN ('direct','shared'))`, users, len(args))
	var authorized bool
	if err := s.v2DB.QueryRowContext(ctx, statement, args...).Scan(&authorized); err != nil {
		return false, fmt.Errorf("authorize v2 repository coverage: %w", err)
	}
	return authorized, nil
}

func (s *Service) v2ReadinessSQL(ctx context.Context, scope *v2Scope) (V2Readiness, error) {
	args := []any{s.v2LedgerEpoch}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	statement := fmt.Sprintf(`SELECT MIN(p.created_at),MAX(p.created_at) FROM attribution_usage_pools p WHERE p.ledger_epoch=$1 AND p.user_id IN (%s) AND p.relay_provider_id > 0 AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits c WHERE c.pool_id=p.id AND c.relation_kind IN ('direct','shared'))`, users)
	var first, latest sql.NullTime
	if err := s.v2DB.QueryRowContext(ctx, statement, args...).Scan(&first, &latest); err != nil {
		return V2Readiness{}, fmt.Errorf("query v2 readiness: %w", err)
	}
	if !first.Valid || !latest.Valid {
		return V2Readiness{State: "waiting_for_data"}, nil
	}
	firstValue, latestValue := first.Time.UTC(), latest.Time.UTC()
	return V2Readiness{State: "active", FirstAcceptedAt: &firstValue, LatestAcceptedAt: &latestValue}, nil
}

func (s *Service) queryV2AvailableUserIDsSQL(ctx context.Context, userIDs map[int]struct{}, from, to time.Time) ([]int, error) {
	args := []any{s.v2LedgerEpoch, from, to}
	users := appendSQLInts(&args, sortedIntKeys(userIDs))
	statement := fmt.Sprintf(`SELECT DISTINCT p.user_id FROM attribution_usage_pools p WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 AND p.user_id IN (%s) AND p.relay_provider_id > 0 AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits c WHERE c.pool_id=p.id AND c.relation_kind IN ('direct','shared')) ORDER BY p.user_id`, users)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []int{}
	for rows.Next() {
		var userID int
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		result = append(result, userID)
	}
	return result, rows.Err()
}

func (s *Service) v2ComparisonInsideEpoch(previousFrom time.Time) (bool, error) {
	if s.v2LedgerEpoch == "" || s.v2CutoverAt.IsZero() {
		return false, nil
	}
	return !previousFrom.Before(s.v2CutoverAt), nil
}

func (s *Service) attachV2RepositoryChanges(ctx context.Context, scope *v2Scope, from, to, previousFrom, previousTo time.Time, items []V2RepositoryRow) error {
	ids := make([]int, len(items))
	byID := make(map[int]*V2RepositoryRow, len(items))
	for index := range items {
		ids[index], byID[items[index].RepoConfigID] = items[index].RepoConfigID, &items[index]
	}
	if len(ids) == 0 {
		return nil
	}
	args := []any{s.v2LedgerEpoch, from, to, previousFrom, previousTo}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	repositories := appendSQLInts(&args, ids)
	statement := fmt.Sprintf(`WITH values_by_period AS (
 SELECT c.repo_config_id, CASE WHEN p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 THEN 'current' ELSE 'previous' END period,
        p.id,p.total_tokens,p.credit_usage,p.coverage_gap_count,bool_or(c.relation_kind='direct') has_direct
 FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id
 WHERE p.ledger_epoch=$1 AND ((p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3) OR (p.bucket_start_utc >= $4 AND p.bucket_start_utc < $5))
   AND p.user_id IN (%s) AND p.relay_provider_id > 0 AND c.repo_config_id IN (%s)
 GROUP BY c.repo_config_id,period,p.id,p.total_tokens,p.credit_usage,p.coverage_gap_count
), totals AS (
 SELECT repo_config_id,period,COALESCE(SUM(total_tokens) FILTER (WHERE has_direct),0)::bigint tokens,
        COALESCE(SUM(credit_usage) FILTER (WHERE has_direct),0)::double precision credit,
        COALESCE(bool_or(coverage_gap_count>0),false) gap
 FROM values_by_period GROUP BY repo_config_id,period
)
SELECT repo_config_id,COALESCE(MAX(tokens) FILTER (WHERE period='current'),0),COALESCE(MAX(tokens) FILTER (WHERE period='previous'),0),
       COALESCE(MAX(credit) FILTER (WHERE period='current'),0),COALESCE(MAX(credit) FILTER (WHERE period='previous'),0),
       COALESCE(bool_or(gap),false) FROM totals GROUP BY repo_config_id`, users, repositories)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("query v2 repository changes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var current, previous int64
		var currentCredit, previousCredit float64
		var gap bool
		if err := rows.Scan(&id, &current, &previous, &currentCredit, &previousCredit, &gap); err != nil {
			return fmt.Errorf("scan v2 repository changes: %w", err)
		}
		if row := byID[id]; row != nil && !gap {
			change := current - previous
			row.TokenChange = &change
			creditChange := currentCredit - previousCredit
			row.CreditChange = &creditChange
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate v2 repository changes: %w", err)
	}
	return nil
}

func (s *Service) attachV2PRChanges(ctx context.Context, scope *v2Scope, from, to, previousFrom, previousTo time.Time, items []V2PullRequestRow) error {
	ids := make([]int, len(items))
	byID := make(map[int]*V2PullRequestRow, len(items))
	for index := range items {
		ids[index], byID[items[index].PRRecordID] = items[index].PRRecordID, &items[index]
	}
	if len(ids) == 0 {
		return nil
	}
	args := []any{s.v2LedgerEpoch, from, to, previousFrom, previousTo}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	prs := appendSQLInts(&args, ids)
	statement := fmt.Sprintf(`WITH pool_pr AS (
 SELECT pr.id pr_record_id,CASE WHEN p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 THEN 'current' ELSE 'previous' END period,
        p.id,p.total_tokens,p.credit_usage,p.coverage_gap_count
 FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id
 JOIN pr_commit_usage_snapshots pcs ON pcs.commit_sha=c.commit_sha JOIN pr_records pr ON pr.id=pcs.pr_record_id AND pr.repo_config_pr_records=c.repo_config_id
 WHERE p.ledger_epoch=$1 AND ((p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3) OR (p.bucket_start_utc >= $4 AND p.bucket_start_utc < $5))
   AND p.user_id IN (%s) AND p.relay_provider_id > 0 AND pr.id IN (%s)
   AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits counted WHERE counted.pool_id=p.id AND counted.relation_kind IN ('direct','shared'))
 GROUP BY pr.id,period,p.id,p.total_tokens,p.credit_usage,p.coverage_gap_count
), totals AS (SELECT pr_record_id,period,SUM(total_tokens)::bigint tokens,SUM(credit_usage)::double precision credit,COALESCE(bool_or(coverage_gap_count>0),false) gap FROM pool_pr GROUP BY pr_record_id,period)
SELECT pr_record_id,COALESCE(MAX(tokens) FILTER (WHERE period='current'),0),COALESCE(MAX(tokens) FILTER (WHERE period='previous'),0),
       COALESCE(MAX(credit) FILTER (WHERE period='current'),0),COALESCE(MAX(credit) FILTER (WHERE period='previous'),0),
       COALESCE(bool_or(gap),false) FROM totals GROUP BY pr_record_id`, users, prs)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("query v2 pull-request changes: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var current, previous int64
		var currentCredit, previousCredit float64
		var gap bool
		if err := rows.Scan(&id, &current, &previous, &currentCredit, &previousCredit, &gap); err != nil {
			return fmt.Errorf("scan v2 pull-request changes: %w", err)
		}
		if row := byID[id]; row != nil && !gap {
			change := current - previous
			row.TokenChange = &change
			creditChange := currentCredit - previousCredit
			row.CreditChange = &creditChange
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate v2 pull-request changes: %w", err)
	}
	return nil
}

func (s *Service) attachV2PRCommits(ctx context.Context, scope *v2Scope, from, to time.Time, items []V2PullRequestRow) error {
	ids := make([]int, len(items))
	byID := make(map[int]*V2PullRequestRow, len(items))
	for index := range items {
		ids[index] = items[index].PRRecordID
		items[index].Commits = []V2CommitReference{}
		byID[items[index].PRRecordID] = &items[index]
	}
	if len(ids) == 0 {
		return nil
	}
	args := []any{s.v2LedgerEpoch, from, to}
	users := appendSQLInts(&args, sortedIntKeys(scope.userIDs))
	prs := appendSQLInts(&args, ids)
	statement := fmt.Sprintf(`SELECT DISTINCT pr.id,c.repo_config_id,c.commit_sha FROM attribution_usage_pools p JOIN attribution_usage_pool_commits c ON c.pool_id=p.id JOIN pr_commit_usage_snapshots pcs ON pcs.commit_sha=c.commit_sha JOIN pr_records pr ON pr.id=pcs.pr_record_id AND pr.repo_config_pr_records=c.repo_config_id WHERE p.ledger_epoch=$1 AND p.bucket_start_utc >= $2 AND p.bucket_start_utc < $3 AND p.user_id IN (%s) AND p.relay_provider_id > 0 AND pr.id IN (%s) AND EXISTS (SELECT 1 FROM attribution_usage_pool_commits counted WHERE counted.pool_id=p.id AND counted.relation_kind IN ('direct','shared')) ORDER BY pr.id,c.repo_config_id,c.commit_sha`, users, prs)
	rows, err := s.v2DB.QueryContext(ctx, statement, args...)
	if err != nil {
		return fmt.Errorf("query v2 PR commits: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var commit V2CommitReference
		if err := rows.Scan(&id, &commit.RepoConfigID, &commit.CommitSHA); err != nil {
			return fmt.Errorf("scan v2 PR commits: %w", err)
		}
		if row := byID[id]; row != nil {
			row.Commits = append(row.Commits, commit)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate v2 PR commits: %w", err)
	}
	return nil
}

func appendSQLInts(args *[]any, values []int) string {
	if len(values) == 0 {
		return "NULL"
	}
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
	return strings.Join([]string{scope.subject, scope.providerSet, query.FromDate, query.ToDate, query.Timezone, strings.TrimSpace(query.Search), strings.TrimSpace(query.Sort), fmt.Sprint(query.RepoID), fmt.Sprint(query.PRRecordID)}, "|")
}
func (s *Service) decodeV2PageCursor(encoded, collection string, scope *v2Scope, actor int, binding string) (string, int, error) {
	if strings.TrimSpace(encoded) == "" {
		return "", 0, nil
	}
	cursor, err := s.decodeCursor(encoded)
	if err != nil {
		return "", 0, err
	}
	if cursor.ScopeVersion != v2SnapshotVersion(scope) {
		return "", 0, ErrSnapshotExpired
	}
	if cursor.Version != activityCursorVersion || cursor.Collection != "v2_"+collection || cursor.ActorUserID != actor || cursor.Subject != binding || cursor.LastID <= 0 || cursor.LastValue == "" {
		return "", 0, ErrInvalidCursor
	}
	return cursor.LastValue, cursor.LastID, nil
}
func (s *Service) encodeV2PageCursor(collection string, scope *v2Scope, actor int, binding string, lastID int, lastValue string) (string, error) {
	return s.encodeCursor(activityCursor{Version: activityCursorVersion, Collection: "v2_" + collection, ScopeVersion: v2SnapshotVersion(scope), ActorUserID: actor, Subject: binding, LastID: lastID, LastValue: lastValue})
}

func v2SnapshotVersion(scope *v2Scope) string {
	return scope.authorization.Version + "|providers:" + scope.providerSet
}
