package activity

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/prsyncjob"
	"github.com/ai-efficiency/backend/ent/relayprovider"
)

const v2PageSize = 20

type v2Scope struct {
	authorization *authorizationScope
	userIDs       map[int]struct{}
	providerIDs   map[int]struct{}
	providerSet   string
	subject       string
}

func (s *Service) V2Overview(ctx context.Context, actorUserID int, query V2Query) (*V2Overview, error) {
	scope, from, to, _, err := s.resolveV2Query(ctx, actorUserID, query)
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
		denominator, err = s.v2Denominator.ResolveDenominator(ctx, V2DenominatorRequest{ActorUserID: actorUserID, Scope: query.Scope, SubjectUserID: query.SubjectID, TeamID: query.TeamID, FromDate: query.FromDate, ToDate: query.ToDate, Timezone: query.Timezone, ScopeVersion: scope.authorization.Version, ProviderSet: scope.providerSet})
		if err != nil {
			denominator = V2Denominator{ProviderSet: scope.providerSet, Retryable: true}
		}
		if denominator.ProviderSet != scope.providerSet {
			denominator = V2Denominator{}
		}
	}
	if s.v2DB == nil {
		return nil, fmt.Errorf("Activity v2 database is not configured")
	}
	return s.queryV2OverviewSQL(ctx, actorUserID, scope, query, from, to, denominator, result)
}

func (s *Service) V2Repositories(ctx context.Context, actorUserID int, query V2PageQuery) (*V2Page[V2RepositoryRow], error) {
	if err := validateV2PageQuery(query); err != nil {
		return nil, err
	}
	scope, from, to, _, err := s.resolveV2Query(ctx, actorUserID, query.V2Query)
	if err != nil {
		return nil, err
	}
	if s.v2DB == nil {
		return nil, fmt.Errorf("Activity v2 database is not configured")
	}
	return s.queryV2RepositoriesSQL(ctx, actorUserID, scope, from, to, query)
}

func (s *Service) V2PullRequests(ctx context.Context, actorUserID int, query V2PageQuery) (*V2Page[V2PullRequestRow], error) {
	if err := validateV2PageQuery(query); err != nil {
		return nil, err
	}
	scope, from, to, _, err := s.resolveV2Query(ctx, actorUserID, query.V2Query)
	if err != nil {
		return nil, err
	}
	if s.v2DB == nil {
		return nil, fmt.Errorf("Activity v2 database is not configured")
	}
	return s.queryV2PullRequestsSQL(ctx, actorUserID, scope, from, to, query)
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
	providers, err := s.client.RelayProvider.Query().Where(relayprovider.EnabledEQ(true)).Order(ent.Asc(relayprovider.FieldID)).All(ctx)
	if err != nil {
		return nil, time.Time{}, time.Time{}, nil, fmt.Errorf("load Activity provider set: %w", err)
	}
	scope := &v2Scope{authorization: authorization, userIDs: map[int]struct{}{}, providerIDs: map[int]struct{}{}, subject: string(query.Scope)}
	providerSet := make([]string, 0, len(providers))
	for _, provider := range providers {
		scope.providerIDs[provider.ID] = struct{}{}
		providerSet = append(providerSet, fmt.Sprintf("%d:%d", provider.ID, provider.ConfigurationVersion))
	}
	scope.providerSet = strings.Join(providerSet, ",")
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
	r := V2Ratio{State: "denominator_unavailable", Retryable: d.Retryable, CommittedTokens: committed}
	if !d.Fresh || !d.Complete || committed < 0 || d.TotalTokens < 0 || committed > d.TotalTokens {
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
	r.Percent = &percent
	if coverage.LowerBound {
		r.State = "lower_bound"
	} else if committed == 0 {
		r.State = "true_zero_committed"
	} else {
		r.State = "exact"
	}
	return r
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
