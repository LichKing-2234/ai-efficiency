package personalusage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
)

type Service struct {
	client        *ent.Client
	resolver      ProviderResolver
	encryptionKey string
	cache         *Cache
}

type resolvedSubject struct {
	configured      bool
	actorID         int
	relayUserID     int64
	bindingVersion  int64
	login           string
	password        string
	providerID      int
	providerVersion int64
	provider        relay.Provider
	origin          relay.UserUsageOriginReader
}

func NewService(client *ent.Client, resolver ProviderResolver, encryptionKey string, cache *Cache) *Service {
	return &Service{client: client, resolver: resolver, encryptionKey: encryptionKey, cache: cache}
}

func (s *Service) Dashboard(ctx context.Context, request Request) (*Snapshot, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	subject, err := s.resolveSubject(ctx, request.UserID)
	if err != nil {
		return nil, err
	}
	if !subject.configured {
		return unconfiguredSnapshot(request), nil
	}
	if s.cache == nil {
		return nil, fmt.Errorf("%w: cache is unavailable", ErrConfiguration)
	}

	key := CacheKey{
		ProviderID: subject.providerID, ProviderVersion: subject.providerVersion,
		ActorID: subject.actorID, RelayUserID: subject.relayUserID, BindingVersion: subject.bindingVersion,
		Params: request.Params,
	}
	loader := func(loadCtx context.Context, includeQuota bool) (OriginLoadResult, error) {
		originResult, originErr := subject.origin.ReadUserUsageOrigin(loadCtx, relay.UserUsageOriginRequest{
			Login: subject.login, Password: subject.password, RelayUserID: subject.relayUserID,
			Params:   request.Params,
			Branches: relay.UserUsageOriginBranches{Usage: true, Quota: includeQuota},
		})
		if originErr != nil {
			return OriginLoadResult{}, originErr
		}
		if originResult == nil {
			return OriginLoadResult{UsageErr: errors.New("personal usage origin returned an empty result")}, nil
		}
		if errors.Is(originResult.UsageErr, relay.ErrInvalidCredentials) {
			return OriginLoadResult{}, originResult.UsageErr
		}
		if loadCtx.Err() != nil {
			return OriginLoadResult{}, loadCtx.Err()
		}
		loaded := OriginLoadResult{Usage: originResult.Usage, UsageErr: originResult.UsageErr}
		if includeQuota {
			loaded.Quota, loaded.QuotaFreshness = quotaResult(originResult, s.cache.now(), request.Params)
			loaded.QuotaLoaded = true
		}
		return loaded, nil
	}

	result, err := s.cache.GetOrLoad(ctx, key, request.IncludeGroupQuotas, loader)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Usage == nil {
		return nil, errors.New("personal usage cache returned no usage")
	}
	if request.IncludeGroupQuotas && !result.QuotaLoaded {
		result.Quota, result.QuotaFreshness, err = s.loadQuota(ctx, subject, request.Params)
		if err != nil {
			return nil, err
		}
		result.QuotaLoaded = true
	}
	return snapshotFromResult(result, request.IncludeGroupQuotas), nil
}

func (s *Service) GroupQuotas(ctx context.Context, request Request) (*GroupQuotaResponse, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	subject, err := s.resolveSubject(ctx, request.UserID)
	if err != nil {
		return nil, err
	}
	if !subject.configured {
		return &GroupQuotaResponse{
			GroupQuotas:    emptyGroupQuotas(),
			QuotaFreshness: QuotaFreshness{CacheStatus: "uncached", SourceStatus: "error"},
		}, nil
	}
	quota, freshness, err := s.loadQuota(ctx, subject, request.Params)
	if err != nil {
		return nil, err
	}
	return &GroupQuotaResponse{GroupQuotas: quota, QuotaFreshness: freshness}, nil
}

func (s *Service) GroupPoolUsage(ctx context.Context, request Request) (*GroupPoolUsageResponse, error) {
	if err := validateRequest(request); err != nil {
		return nil, err
	}
	subject, err := s.resolveSubject(ctx, request.UserID)
	if err != nil {
		return nil, err
	}
	if !subject.configured {
		return &GroupPoolUsageResponse{
			PoolUsage:          emptyGroupPoolUsage(),
			PoolUsageFreshness: PoolUsageFreshness{CacheStatus: "uncached", SourceStatus: "error"},
		}, nil
	}
	reader, ok := subject.provider.(relay.GroupOAuthPoolUsageReader)
	if !ok {
		return unavailablePoolUsageResponse(), nil
	}
	groups, err := subject.provider.ListAllowedGroupsForUser(ctx, subject.relayUserID)
	if err != nil {
		return unavailablePoolUsageResponse(), nil
	}
	groupIDs := make([]int64, 0, len(groups))
	for _, group := range groups {
		if group.ID > 0 {
			groupIDs = append(groupIDs, group.ID)
		}
	}
	pool, err := reader.ReadGroupOAuthPoolUsage(ctx, groupIDs)
	if err != nil {
		return unavailablePoolUsageResponse(), nil
	}
	return &GroupPoolUsageResponse{
		PoolUsage: pool,
		PoolUsageFreshness: PoolUsageFreshness{
			AsOf:         latestPoolAsOf(pool),
			CacheStatus:  "uncached",
			SourceStatus: "ok",
		},
	}, nil
}

func unavailablePoolUsageResponse() *GroupPoolUsageResponse {
	return &GroupPoolUsageResponse{
		PoolUsage:          unavailableGroupPoolUsage(),
		PoolUsageFreshness: PoolUsageFreshness{CacheStatus: "uncached", SourceStatus: "error"},
	}
}

func (s *Service) loadQuota(ctx context.Context, subject *resolvedSubject, params relay.UserUsageDashboardParams) (relay.UserUsageGroupQuotaState, QuotaFreshness, error) {
	result, err := subject.origin.ReadUserUsageOrigin(ctx, relay.UserUsageOriginRequest{
		Login:       subject.login,
		Password:    subject.password,
		RelayUserID: subject.relayUserID,
		Params:      params,
		Branches:    relay.UserUsageOriginBranches{Quota: true},
	})
	if err != nil {
		if ctx.Err() != nil {
			return relay.UserUsageGroupQuotaState{}, QuotaFreshness{}, ctx.Err()
		}
		return unavailableGroupQuotas(), QuotaFreshness{CacheStatus: "uncached", SourceStatus: "error"}, nil
	}
	quota, freshness := quotaResult(result, s.cache.now(), params)
	return quota, freshness, nil
}

func quotaResult(result *relay.UserUsageOriginResult, now time.Time, params relay.UserUsageDashboardParams) (relay.UserUsageGroupQuotaState, QuotaFreshness) {
	if result == nil || result.QuotaErr != nil {
		return unavailableGroupQuotas(), QuotaFreshness{CacheStatus: "uncached", SourceStatus: "error"}
	}
	asOf := now.UTC()
	return mergeGroupQuotas(result.APIKeys, result.Subscriptions, quotaWindow(params)), QuotaFreshness{
		AsOf: &asOf, CacheStatus: "uncached", SourceStatus: "ok",
	}
}

func (s *Service) resolveSubject(ctx context.Context, userID int) (*resolvedSubject, error) {
	if s == nil || s.client == nil || s.resolver == nil {
		return nil, fmt.Errorf("%w: service dependencies are unavailable", ErrConfiguration)
	}
	user, err := s.client.User.Get(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("%w: fetch user: %w", ErrConfiguration, err)
	}
	if user.RelayAuthPassword == nil || strings.TrimSpace(*user.RelayAuthPassword) == "" {
		return &resolvedSubject{configured: false, actorID: user.ID}, nil
	}
	if user.RelayUserID == nil || *user.RelayUserID <= 0 {
		return nil, fmt.Errorf("%w: current user has no Relay binding", ErrConfiguration)
	}
	password, err := pkg.Decrypt(strings.TrimSpace(*user.RelayAuthPassword), s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("%w: decrypt Relay password: %w", ErrConfiguration, err)
	}
	login := firstNonEmpty(user.Email, user.Username)
	if login == "" {
		return nil, fmt.Errorf("%w: current user has no Relay login", ErrConfiguration)
	}
	providerRow, err := s.client.RelayProvider.Query().
		Where(relayprovider.IsPrimary(true), relayprovider.Enabled(true)).
		Order(ent.Asc(relayprovider.FieldID)).
		First(ctx)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve primary provider: %w", ErrConfiguration, err)
	}
	provider, err := s.resolver.Resolve(ctx, providerRow.ID)
	if err != nil {
		return nil, fmt.Errorf("%w: resolve Relay provider: %w", ErrConfiguration, err)
	}
	origin, ok := provider.(relay.UserUsageOriginReader)
	if !ok {
		return nil, fmt.Errorf("%w: Relay provider does not support personal usage origin reads", ErrConfiguration)
	}
	bindingVersion := user.UpdatedAt.UTC().UnixNano()
	if bindingVersion <= 0 {
		bindingVersion = 1
	}
	return &resolvedSubject{
		configured: true, actorID: user.ID, relayUserID: int64(*user.RelayUserID),
		bindingVersion: bindingVersion, login: login, password: password,
		providerID: providerRow.ID, providerVersion: providerRow.ConfigurationVersion, provider: provider, origin: origin,
	}, nil
}

func validateRequest(request Request) error {
	if request.UserID <= 0 {
		return fmt.Errorf("%w: user ID must be positive", ErrConfiguration)
	}
	if request.Params.StartDate == "" || request.Params.EndDate == "" || request.Params.Granularity == "" {
		return fmt.Errorf("%w: usage range is incomplete", ErrConfiguration)
	}
	return nil
}

func unconfiguredSnapshot(request Request) *Snapshot {
	snapshot := &Snapshot{
		Configured: false,
		Range: relay.UserUsageDashboardRange{
			StartDate: request.Params.StartDate, EndDate: request.Params.EndDate,
			Granularity: request.Params.Granularity, Timezone: request.Params.Timezone,
		},
		Trend:  []relay.UserUsageTrendPoint{},
		Models: []relay.UserUsageModelStat{},
	}
	if request.IncludeGroupQuotas {
		quota := emptyGroupQuotas()
		snapshot.GroupQuotas = &quota
	}
	return snapshot
}

func snapshotFromResult(result *CacheResult, includeQuota bool) *Snapshot {
	snapshot := &Snapshot{
		Configured: true,
		Range:      result.Usage.Range, Stats: result.Usage.Stats,
		Trend:          append([]relay.UserUsageTrendPoint{}, result.Usage.Trend...),
		Models:         append([]relay.UserUsageModelStat{}, result.Usage.Models...),
		UsageFreshness: &result.UsageFreshness,
	}
	if includeQuota {
		quota := result.Quota
		quotaFreshness := result.QuotaFreshness
		snapshot.GroupQuotas = &quota
		snapshot.QuotaFreshness = &quotaFreshness
	}
	return snapshot
}
