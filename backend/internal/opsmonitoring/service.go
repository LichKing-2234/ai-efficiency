// Package opsmonitoring builds the read-only Ops Monitoring view: every group's
// upstream ops health, fetched together. It is deliberately a display layer —
// upstream metrics are carried through verbatim and never recomputed here.
package opsmonitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	// DefaultWindow is the window the upstream ops panel shows by default.
	DefaultWindow = "1h"

	// GroupStatusOK and GroupStatusFailed describe one group's fetch outcome,
	// not upstream health. An unfetched group is never silently rendered as
	// healthy.
	GroupStatusOK     = "ok"
	GroupStatusFailed = "failed"

	defaultGroupFetchConcurrency = 8
	defaultGroupFetchTimeout     = 5 * time.Second
	// defaultGroupsBudget caps a whole fan-out. Every group needs a wave of its
	// own once the concurrency is saturated: 57 groups measured at ~0.7s each is
	// already ~6s of work, so an 8s budget would report healthy groups as failed
	// after a single slow upstream call.
	defaultGroupsBudget   = 20 * time.Second
	defaultGroupsFreshFor = 60 * time.Second
)

var supportedWindows = map[string]time.Duration{
	"1h":  time.Hour,
	"6h":  6 * time.Hour,
	"24h": 24 * time.Hour,
}

// ParseWindow resolves a requested window to its canonical key and duration.
// An empty window selects the default; anything else must be one of the
// supported windows, so a caller can never silently receive a different range.
func ParseWindow(raw string) (string, time.Duration, error) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" {
		key = DefaultWindow
	}
	window, ok := supportedWindows[key]
	if !ok {
		return "", 0, fmt.Errorf("unsupported ops monitoring window %q", raw)
	}
	return key, window, nil
}

// ProviderResolver resolves one configured relay provider by ID.
type ProviderResolver interface {
	Resolve(context.Context, int) (relay.Provider, error)
}

type GroupsRequest struct {
	ProviderID int
	Window     string
	// Force skips the cache read for an explicit operator refresh. Automatic
	// reads leave it false and keep using the cache.
	Force bool
}

// GroupRow is one group's Ops Monitoring row. Overview is the upstream ops
// overview object exactly as returned; it is absent only when the fetch failed.
type GroupRow struct {
	GroupID            int64           `json:"group_id"`
	GroupName          string          `json:"group_name"`
	Platform           string          `json:"platform"`
	AccountCount       int             `json:"account_count"`
	ActiveAccountCount int             `json:"active_account_count"`
	Status             string          `json:"status"`
	Error              string          `json:"error"`
	Overview           json.RawMessage `json:"overview"`
}

type GroupsSnapshot struct {
	ProviderID       int        `json:"provider_id"`
	Window           string     `json:"window"`
	StartTime        time.Time  `json:"start_time"`
	EndTime          time.Time  `json:"end_time"`
	AsOf             time.Time  `json:"as_of"`
	Refreshing       bool       `json:"refreshing"`
	FailedGroupCount int        `json:"failed_group_count"`
	Groups           []GroupRow `json:"groups"`
}

type GroupAccountsRequest struct {
	ProviderID int
	GroupID    int64
}

type GroupAccountsSnapshot struct {
	GroupID  int64                `json:"group_id"`
	AsOf     time.Time            `json:"as_of"`
	Accounts []relay.GroupAccount `json:"accounts"`
}

type ProxyEgressRequest struct {
	ProviderID int
	ProxyID    int64
	// Force re-probes instead of replaying the cached measurement. The probe is
	// the one action an operator takes while fixing a proxy, so an explicit
	// request must not be answered from a six-hour-old cache entry.
	Force bool
}

// ProxyEgressRecord is one measured egress probe result plus when it was taken.
// The probe outcome itself is upstream's.
type ProxyEgressRecord struct {
	ProxyID int64 `json:"proxy_id"`
	relay.ProxyEgressCheck
	CheckedAt time.Time `json:"checked_at"`
	Cached    bool      `json:"cached"`
}

type Service struct {
	resolver ProviderResolver
	cache    *Cache
	now      func() time.Time

	concurrency     int
	perGroupTimeout time.Duration
	budget          time.Duration
	freshFor        time.Duration

	groupsFlight readcache.FlightGroup[*GroupsSnapshot]
}

func NewService(resolver ProviderResolver, cache *Cache) *Service {
	return &Service{
		resolver:        resolver,
		cache:           cache,
		now:             time.Now,
		concurrency:     defaultGroupFetchConcurrency,
		perGroupTimeout: defaultGroupFetchTimeout,
		budget:          defaultGroupsBudget,
		freshFor:        defaultGroupsFreshFor,
	}
}

// Groups returns every selected group's upstream ops overview for one window.
// A group that cannot be fetched is reported as failed in its own row; only a
// provider-level failure fails the whole read.
func (s *Service) Groups(ctx context.Context, request GroupsRequest) (*GroupsSnapshot, error) {
	if s == nil || s.resolver == nil {
		return nil, fmt.Errorf("ops monitoring service is unavailable")
	}
	windowKey, window, err := ParseWindow(request.Window)
	if err != nil {
		return nil, err
	}

	lister, reader, err := s.groupReaders(ctx, request.ProviderID)
	if err != nil {
		return nil, err
	}

	dimensions := groupsCacheDimensions{
		SchemaVersion: opsMonitoringCacheSchemaVersion,
		Kind:          "groups",
		ProviderID:    request.ProviderID,
		Window:        windowKey,
	}

	// Force means an operator asked for a fresh read. Answering that from the
	// cache would hide exactly the change they are looking for.
	if s.cache != nil && !request.Force {
		if cached, ok := s.cache.readGroups(ctx, dimensions); ok && cached != nil {
			age := s.now().Sub(cached.AsOf)
			if age < s.freshFor {
				cached.Refreshing = false
				return cached, nil
			}
			if age < groupsCacheTTL {
				// Stale but readable: answer now, refresh behind the response.
				cached.Refreshing = true
				go s.refreshGroups(dimensions, request.ProviderID, windowKey, window, lister, reader)
				return cached, nil
			}
		}
	}

	snapshot, err := s.loadGroupsOnce(ctx, dimensions, request.ProviderID, windowKey, window, lister, reader)
	if err != nil {
		return nil, err
	}
	s.cacheGroups(ctx, dimensions, snapshot)
	return snapshot, nil
}

// cacheGroups stores a snapshot only when every group was read. A failure is
// not a measurement: caching one would keep showing a transient upstream error
// as if it were data for the whole freshness window, and the next read would
// never call upstream again.
func (s *Service) cacheGroups(ctx context.Context, dimensions groupsCacheDimensions, snapshot *GroupsSnapshot) {
	if s.cache == nil || snapshot == nil || snapshot.FailedGroupCount > 0 {
		return
	}
	s.cache.writeGroups(ctx, dimensions, snapshot)
}

// GroupAccounts lists one group's accounts with their proxy bindings. It is
// loaded on demand and never cache-warmed with the group table.
func (s *Service) GroupAccounts(ctx context.Context, request GroupAccountsRequest) (*GroupAccountsSnapshot, error) {
	if s == nil || s.resolver == nil {
		return nil, fmt.Errorf("ops monitoring service is unavailable")
	}
	if request.GroupID <= 0 {
		return nil, fmt.Errorf("group id is required")
	}
	_, reader, err := s.groupReaders(ctx, request.ProviderID)
	if err != nil {
		return nil, err
	}
	accounts, err := reader.ListGroupAccounts(ctx, request.GroupID)
	if err != nil {
		return nil, err
	}
	if accounts == nil {
		accounts = []relay.GroupAccount{}
	}
	return &GroupAccountsSnapshot{GroupID: request.GroupID, AsOf: s.now().UTC(), Accounts: accounts}, nil
}

// ProxyEgress measures one proxy's real egress identity. The probe is an
// upstream action, so it is issued on explicit demand and its result is cached.
func (s *Service) ProxyEgress(ctx context.Context, request ProxyEgressRequest) (*ProxyEgressRecord, error) {
	if s == nil || s.resolver == nil {
		return nil, fmt.Errorf("ops monitoring service is unavailable")
	}
	if request.ProxyID <= 0 {
		return nil, fmt.Errorf("proxy id is required")
	}
	_, reader, err := s.groupReaders(ctx, request.ProviderID)
	if err != nil {
		return nil, err
	}

	if s.cache != nil && !request.Force {
		if record, ok := s.cache.readProxyEgress(ctx, request.ProviderID, request.ProxyID); ok && record != nil {
			record.Cached = true
			return record, nil
		}
	}

	check, err := reader.TestProxy(ctx, request.ProxyID)
	if err != nil {
		return nil, err
	}
	if check == nil {
		return nil, fmt.Errorf("proxy %d test returned no result", request.ProxyID)
	}
	record := &ProxyEgressRecord{ProxyID: request.ProxyID, ProxyEgressCheck: *check, CheckedAt: s.now().UTC()}
	if s.cache != nil {
		s.cache.writeProxyEgress(ctx, request.ProviderID, record)
	}
	return record, nil
}

func (s *Service) groupReaders(ctx context.Context, providerID int) (relay.PlatformGroupLister, relay.GroupOpsHealthReader, error) {
	provider, err := s.resolver.Resolve(ctx, providerID)
	if err != nil {
		return nil, nil, err
	}
	if provider == nil {
		return nil, nil, fmt.Errorf("relay provider %d is not configured", providerID)
	}
	lister, ok := provider.(relay.PlatformGroupLister)
	if !ok {
		return nil, nil, fmt.Errorf("relay provider %d does not expose group listings", providerID)
	}
	reader, ok := provider.(relay.GroupOpsHealthReader)
	if !ok {
		return nil, nil, fmt.Errorf("relay provider %d does not expose ops monitoring", providerID)
	}
	return lister, reader, nil
}

func (s *Service) loadGroupsOnce(
	ctx context.Context,
	dimensions groupsCacheDimensions,
	providerID int,
	windowKey string,
	window time.Duration,
	lister relay.PlatformGroupLister,
	reader relay.GroupOpsHealthReader,
) (*GroupsSnapshot, error) {
	key := fmt.Sprintf("%d|%s", providerID, windowKey)
	return s.groupsFlight.Do(ctx, key, s.budget, func(flightContext context.Context) (*GroupsSnapshot, error) {
		return s.loadGroups(flightContext, providerID, windowKey, window, lister, reader)
	})
}

func (s *Service) refreshGroups(
	dimensions groupsCacheDimensions,
	providerID int,
	windowKey string,
	window time.Duration,
	lister relay.PlatformGroupLister,
	reader relay.GroupOpsHealthReader,
) {
	ctx, cancel := context.WithTimeout(context.Background(), s.budget)
	defer cancel()
	snapshot, err := s.loadGroupsOnce(ctx, dimensions, providerID, windowKey, window, lister, reader)
	if err != nil || snapshot == nil {
		return
	}
	s.cacheGroups(ctx, dimensions, snapshot)
}

func (s *Service) loadGroups(
	ctx context.Context,
	providerID int,
	windowKey string,
	window time.Duration,
	lister relay.PlatformGroupLister,
	reader relay.GroupOpsHealthReader,
) (*GroupsSnapshot, error) {
	loadContext, cancel := context.WithTimeout(ctx, s.budget)
	defer cancel()

	groups, err := lister.ListPlatformGroups(loadContext)
	if err != nil {
		return nil, fmt.Errorf("list platform groups: %w", err)
	}

	end := s.now().UTC()
	start := end.Add(-window)
	rows := make([]GroupRow, len(groups))

	semaphore := make(chan struct{}, s.concurrency)
	var waitGroup sync.WaitGroup
	for index, group := range groups {
		rows[index] = GroupRow{
			GroupID:            group.ID,
			GroupName:          group.Name,
			Platform:           group.Platform,
			AccountCount:       group.AccountCount,
			ActiveAccountCount: group.ActiveAccountCount,
			Status:             GroupStatusOK,
		}
		waitGroup.Add(1)
		go func(index int, group relay.Group) {
			defer waitGroup.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-loadContext.Done():
				rows[index].Status = GroupStatusFailed
				rows[index].Error = "ops monitoring budget exhausted before fetch"
				return
			}

			groupContext, cancelGroup := context.WithTimeout(loadContext, s.perGroupTimeout)
			defer cancelGroup()
			health, fetchErr := reader.ReadGroupOpsHealth(groupContext, group.ID, start, end)
			if fetchErr != nil {
				rows[index].Status = GroupStatusFailed
				rows[index].Error = fetchErr.Error()
				return
			}
			if health == nil {
				rows[index].Status = GroupStatusFailed
				rows[index].Error = "ops overview returned no result"
				return
			}
			rows[index].Overview = health.Overview
		}(index, group)
	}
	waitGroup.Wait()

	failedCount := 0
	for _, row := range rows {
		if row.Status == GroupStatusFailed {
			failedCount++
		}
	}

	return &GroupsSnapshot{
		ProviderID:       providerID,
		Window:           windowKey,
		StartTime:        start,
		EndTime:          end,
		AsOf:             end,
		FailedGroupCount: failedCount,
		Groups:           rows,
	}, nil
}
