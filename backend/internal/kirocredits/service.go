package kirocredits

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
	"go.uber.org/zap"
)

const (
	serviceTypeKiro2CC = "kiro2cc"
	dateLayout         = "2006-01-02"
)

// Options configures a Service. Zero values fall back to the defaults.
type Options struct {
	Logger      *zap.Logger
	CacheTTL    time.Duration
	HTTPTimeout time.Duration
	Now         func() time.Time
}

type cachedSnapshot struct {
	value     Snapshot
	expiresAt time.Time
}

type cachedKeys struct {
	value     []adminKey
	expiresAt time.Time
}

type cachedTeam struct {
	value     TeamCredits
	expiresAt time.Time
}

// Service resolves the kiro2cc service entry, maps an AI Efficiency user to the
// gateway-provisioned kiro2cc API key and aggregates that key's credits.
type Service struct {
	client        *ent.Client
	encryptionKey string
	logger        *zap.Logger
	cacheTTL      time.Duration
	httpTimeout   time.Duration
	now           func() time.Time

	mu        sync.Mutex
	snapshots map[string]cachedSnapshot
	keys      map[string]cachedKeys
	teams     map[string]cachedTeam
}

// NewService builds a credits service. A nil logger is replaced with a no-op
// logger so callers do not have to guard optional wiring.
func NewService(client *ent.Client, encryptionKey string, options Options) *Service {
	logger := options.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	cacheTTL := options.CacheTTL
	if cacheTTL <= 0 {
		cacheTTL = defaultCacheTTL
	}
	httpTimeout := options.HTTPTimeout
	if httpTimeout <= 0 {
		httpTimeout = defaultTimeout
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Service{
		client:        client,
		encryptionKey: encryptionKey,
		logger:        logger,
		cacheTTL:      cacheTTL,
		httpTimeout:   httpTimeout,
		now:           now,
		snapshots:     map[string]cachedSnapshot{},
		keys:          map[string]cachedKeys{},
		teams:         map[string]cachedTeam{},
	}
}

// Credits returns the credits snapshot for one user and dashboard range. It
// never returns an error: every failure is expressed as an unavailable
// snapshot so the usage dashboard can fail soft.
func (s *Service) Credits(ctx context.Context, userID int, params Params) Snapshot {
	now := s.now().UTC()
	snapshot := Snapshot{AsOf: now, Status: StatusError}
	if s == nil || s.client == nil {
		snapshot.Message = "Kiro 用量服务未配置"
		return snapshot
	}
	user, err := s.client.User.Get(ctx, userID)
	if err != nil {
		s.logger.Warn("kiro credits: load user", zap.Int("user_id", userID), zap.Error(err))
		snapshot.Message = "无法读取当前用户"
		return snapshot
	}
	if user.RelayUserID == nil || *user.RelayUserID <= 0 {
		snapshot.Status = StatusUnlinked
		snapshot.Message = "当前账号未关联 AI 服务用户"
		return snapshot
	}
	relayUserID := int64(*user.RelayUserID)

	row, err := s.source(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			snapshot.Status = StatusNoSource
			snapshot.Message = "尚未配置 kiro2cc 服务入口"
			return snapshot
		}
		s.logger.Warn("kiro credits: resolve service entry", zap.Error(err))
		snapshot.Message = "Kiro 用量暂时不可用"
		return snapshot
	}
	snapshot.Configured = true

	window, err := parseWindow(params, now)
	if err != nil {
		s.logger.Warn("kiro credits: invalid range", zap.Error(err), zap.Int("user_id", userID))
		snapshot.Message = "用量时间范围无效"
		return snapshot
	}

	cacheKey := cacheKeyFor(row, relayUserID, window)
	if cached, ok := s.loadSnapshot(cacheKey, now); ok {
		return cached
	}

	snapshot = s.collect(ctx, row, relayUserID, window, now)
	s.storeSnapshot(cacheKey, snapshot, now)
	return snapshot
}

func (s *Service) source(ctx context.Context) (*ent.RelayProvider, error) {
	return s.client.RelayProvider.Query().
		Where(
			relayprovider.ServiceTypeEQ(serviceTypeKiro2CC),
			relayprovider.EnabledEQ(true),
		).
		Order(ent.Asc(relayprovider.FieldID)).
		First(ctx)
}

func (s *Service) collect(ctx context.Context, row *ent.RelayProvider, relayUserID int64, window window, now time.Time) Snapshot {
	snapshot := Snapshot{
		Configured: true,
		AsOf:       now,
		ByModel:    []ModelCredits{},
		ByDay:      []DayCredits{},
	}

	token, err := pkg.Decrypt(row.AdminAPIKey, s.encryptionKey)
	if err != nil || strings.TrimSpace(token) == "" {
		s.logger.Warn("kiro credits: decrypt admin token", zap.Int("provider_id", row.ID), zap.Error(err))
		snapshot.Status = StatusError
		snapshot.Message = "kiro2cc 凭据无法解密"
		return snapshot
	}
	client := newAdminClient(row.BaseURL, token, s.httpTimeout)

	keys, err := s.listKeys(ctx, client, row)
	if err != nil {
		s.logger.Warn("kiro credits: list kiro2cc keys", zap.Int("provider_id", row.ID), zap.Error(err))
		snapshot.Status = StatusError
		snapshot.Message = "Kiro 用量暂时不可用"
		return snapshot
	}
	key, found, ambiguous := matchKey(keys, relayUserID)
	if !found {
		snapshot.Status = StatusNoKey
		snapshot.Message = "尚未在该用户下产生 Kiro CLI 调用"
		return snapshot
	}
	snapshot.KeyFound = true
	snapshot.AmbiguousKey = ambiguous

	summary, err := client.usageSummary(ctx, key.ID)
	if err != nil {
		s.logger.Warn("kiro credits: read key usage", zap.Int("provider_id", row.ID), zap.Error(err))
		snapshot.Status = StatusError
		snapshot.Message = "Kiro 用量暂时不可用"
		return snapshot
	}
	snapshot.LifetimeCredits = summary.TotalCredits
	snapshot.LifetimeRequests = summary.TotalRequests

	aggregated, rangePartial, err := s.collectRange(ctx, client, key.ID, window)
	if err != nil {
		s.logger.Warn("kiro credits: read key records", zap.Int("provider_id", row.ID), zap.Error(err))
		snapshot.Status = StatusError
		snapshot.Message = "Kiro 用量暂时不可用"
		return snapshot
	}
	snapshot.Credits = aggregated.credits
	snapshot.CreditsSaved = aggregated.creditsSaved
	snapshot.Requests = aggregated.requests
	snapshot.InputTokens = aggregated.inputTokens
	snapshot.OutputTokens = aggregated.outputTokens
	snapshot.ByModel = aggregated.byModel
	snapshot.ByDay = aggregated.byDay
	if rangePartial || aggregated.missingCredits {
		// Records without a real metering value can only be a lower bound.
		snapshot.Partial = true
	}

	snapshot.Available = true
	snapshot.Status = StatusOK
	return snapshot
}

// collectRange reads one key's records inside the window and aggregates them.
// It returns partial=true when the record page cap is hit before the window's
// oldest record, so callers can render the totals as a lower bound.
func (s *Service) collectRange(ctx context.Context, client *adminClient, keyID int64, window window) (aggregateResult, bool, error) {
	records := make([]usageRecord, 0, 128)
	partial := false
	page := 1
	for page <= maxRecordPages {
		pageData, err := client.usageRecords(ctx, keyID, page, recordPageSize)
		if err != nil {
			return aggregateResult{}, false, err
		}
		reachedOlder := false
		for _, record := range pageData.Records {
			if record.CreatedAt.Before(window.start) {
				reachedOlder = true
				continue
			}
			if record.CreatedAt.Before(window.endExclusive) {
				records = append(records, record)
			}
		}
		if reachedOlder || page >= pageData.TotalPages || len(pageData.Records) == 0 {
			break
		}
		if page == maxRecordPages {
			partial = true
		}
		page++
	}
	return aggregateRecords(window, records), partial, nil
}

type aggregateResult struct {
	credits        float64
	creditsSaved   float64
	requests       int64
	inputTokens    int64
	outputTokens   int64
	byModel        []ModelCredits
	byDay          []DayCredits
	missingCredits bool
}

// aggregateRecords buckets the in-range kiro2cc records by model and by the
// caller's timezone day. Nil credits values are treated as unknown (zero) and
// flagged so callers can render the totals as a lower bound.
func aggregateRecords(window window, records []usageRecord) aggregateResult {
	result := aggregateResult{byModel: []ModelCredits{}, byDay: []DayCredits{}}
	modelBuckets := map[string]*ModelCredits{}
	dayBuckets := map[string]*DayCredits{}
	for _, record := range records {
		credits := 0.0
		if record.CreditsUsed != nil {
			credits = *record.CreditsUsed
		} else {
			result.missingCredits = true
		}
		saved := 0.0
		if record.CreditsSaved != nil {
			saved = *record.CreditsSaved
		}
		result.credits += credits
		result.creditsSaved += saved
		result.requests++
		result.inputTokens += record.InputTokens
		result.outputTokens += record.OutputTokens

		modelBucket := modelBuckets[record.Model]
		if modelBucket == nil {
			modelBucket = &ModelCredits{Model: record.Model}
			modelBuckets[record.Model] = modelBucket
		}
		modelBucket.Requests++
		modelBucket.Credits += credits
		modelBucket.InputTokens += record.InputTokens
		modelBucket.OutputTokens += record.OutputTokens

		day := record.CreatedAt.In(window.location).Format(dateLayout)
		dayBucket := dayBuckets[day]
		if dayBucket == nil {
			dayBucket = &DayCredits{Date: day}
			dayBuckets[day] = dayBucket
		}
		dayBucket.Requests++
		dayBucket.Credits += credits
	}
	for _, bucket := range modelBuckets {
		result.byModel = append(result.byModel, *bucket)
	}
	sort.Slice(result.byModel, func(i, j int) bool {
		if result.byModel[i].Credits != result.byModel[j].Credits {
			return result.byModel[i].Credits > result.byModel[j].Credits
		}
		return result.byModel[i].Model < result.byModel[j].Model
	})
	for _, bucket := range dayBuckets {
		result.byDay = append(result.byDay, *bucket)
	}
	sort.Slice(result.byDay, func(i, j int) bool { return result.byDay[i].Date < result.byDay[j].Date })
	return result
}

func (s *Service) listKeys(ctx context.Context, client *adminClient, row *ent.RelayProvider) ([]adminKey, error) {
	cacheKey := strconv.Itoa(row.ID) + ":" + strconv.FormatInt(row.ConfigurationVersion, 10)
	now := s.now()
	s.mu.Lock()
	cached, ok := s.keys[cacheKey]
	s.mu.Unlock()
	if ok && now.Before(cached.expiresAt) {
		return cached.value, nil
	}
	keys, err := client.listKeys(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.keys[cacheKey] = cachedKeys{value: keys, expiresAt: now.Add(defaultKeyTTL)}
	s.mu.Unlock()
	return keys, nil
}

// matchKey returns the kiro2cc key provisioned by the native gateway for one
// sub2api user. Modern keys are named sub2api-<user id>-<identity>; the legacy
// spelling sub2api-user-<user id> is also accepted.
func matchKey(keys []adminKey, relayUserID int64) (adminKey, bool, bool) {
	modernPrefix := modernKeyNameBase + strconv.FormatInt(relayUserID, 10) + "-"
	legacyName := legacyKeyNameBase + strconv.FormatInt(relayUserID, 10)
	var matches []adminKey
	for _, key := range keys {
		name := strings.TrimSpace(key.Name)
		if strings.HasPrefix(name, modernPrefix) || name == legacyName {
			matches = append(matches, key)
		}
	}
	if len(matches) == 0 {
		return adminKey{}, false, false
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID > matches[j].ID })
	return matches[0], true, len(matches) > 1
}

type window struct {
	start        time.Time
	endExclusive time.Time
	location     *time.Location
}

func parseWindow(params Params, now time.Time) (window, error) {
	location := time.UTC
	if tz := strings.TrimSpace(params.Timezone); tz != "" {
		if loaded, err := time.LoadLocation(tz); err == nil {
			location = loaded
		}
	}
	startDate := strings.TrimSpace(params.StartDate)
	endDate := strings.TrimSpace(params.EndDate)
	if startDate == "" || endDate == "" {
		today := now.In(location)
		startDate = today.AddDate(0, 0, -6).Format(dateLayout)
		endDate = today.Format(dateLayout)
	}
	start, err := time.ParseInLocation(dateLayout, startDate, location)
	if err != nil {
		return window{}, fmt.Errorf("invalid start date %q: %w", startDate, err)
	}
	end, err := time.ParseInLocation(dateLayout, endDate, location)
	if err != nil {
		return window{}, fmt.Errorf("invalid end date %q: %w", endDate, err)
	}
	if end.Before(start) {
		return window{}, fmt.Errorf("end date %q is before start date %q", endDate, startDate)
	}
	return window{start: start, endExclusive: end.AddDate(0, 0, 1), location: location}, nil
}

func cacheKeyFor(row *ent.RelayProvider, relayUserID int64, window window) string {
	return fmt.Sprintf("%d:%d:%d:%s:%s:%s",
		row.ID,
		row.ConfigurationVersion,
		relayUserID,
		window.start.Format(time.RFC3339),
		window.endExclusive.Format(time.RFC3339),
		window.location.String(),
	)
}

func (s *Service) loadSnapshot(key string, now time.Time) (Snapshot, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, ok := s.snapshots[key]
	if !ok || !now.Before(cached.expiresAt) {
		if ok {
			delete(s.snapshots, key)
		}
		return Snapshot{}, false
	}
	return cached.value, true
}

func (s *Service) storeSnapshot(key string, snapshot Snapshot, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Keep the map bounded: dashboards are per user and range, and a stale
	// entry only costs a re-read.
	if len(s.snapshots) > 4096 {
		for existing := range s.snapshots {
			delete(s.snapshots, existing)
		}
	}
	s.snapshots[key] = cachedSnapshot{value: snapshot, expiresAt: now.Add(s.cacheTTL)}
}

// ResetCache drops cached snapshots and key lists. Tests use it; production
// relies on the short TTL instead.
func (s *Service) ResetCache() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots = map[string]cachedSnapshot{}
	s.keys = map[string]cachedKeys{}
	s.teams = map[string]cachedTeam{}
}

// Batch aggregates credits for a set of users over one dashboard range. It is
// read-only and fails soft: any upstream failure yields an unavailable
// TeamCredits instead of an error.
func (s *Service) Batch(ctx context.Context, users []UserRef, params Params) TeamCredits {
	now := s.now().UTC()
	result := TeamCredits{
		AsOf:    now,
		Status:  StatusError,
		ByModel: []ModelCredits{},
		ByDay:   []DayCredits{},
		Users:   []UserCredits{},
	}
	if s == nil || s.client == nil {
		result.Message = "Kiro 用量服务未配置"
		return result
	}
	refs := normalizeRefs(users)
	if len(refs) == 0 {
		result.Status = StatusOK
		result.Available = true
		return result
	}

	row, err := s.source(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			result.Status = StatusNoSource
			result.Message = "尚未配置 kiro2cc 服务入口"
			return result
		}
		s.logger.Warn("kiro credits: resolve service entry for batch", zap.Error(err))
		result.Message = "Kiro 用量暂时不可用"
		return result
	}
	result.Configured = true

	window, err := parseWindow(params, now)
	if err != nil {
		s.logger.Warn("kiro credits: invalid team range", zap.Error(err))
		result.Message = "用量时间范围无效"
		return result
	}

	cacheKey := teamCacheKey(row, refs, window)
	if cached, ok := s.loadTeam(cacheKey, now); ok {
		return cached
	}

	token, err := pkg.Decrypt(row.AdminAPIKey, s.encryptionKey)
	if err != nil || strings.TrimSpace(token) == "" {
		s.logger.Warn("kiro credits: decrypt admin token for batch", zap.Int("provider_id", row.ID), zap.Error(err))
		result.Message = "kiro2cc 凭据无法解密"
		return result
	}
	client := newAdminClient(row.BaseURL, token, s.httpTimeout)
	keys, err := s.listKeys(ctx, client, row)
	if err != nil {
		s.logger.Warn("kiro credits: list kiro2cc keys for batch", zap.Int("provider_id", row.ID), zap.Error(err))
		result.Message = "Kiro 用量暂时不可用"
		return result
	}

	perUser := make([]UserCredits, len(refs))
	errs := make([]error, len(refs))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for index := range refs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ref := refs[i]
			entry := UserCredits{
				UserID:      ref.UserID,
				RelayUserID: ref.RelayUserID,
				ByModel:     []ModelCredits{},
				ByDay:       []DayCredits{},
			}
			key, found, _ := matchKey(keys, ref.RelayUserID)
			if !found {
				perUser[i] = entry
				return
			}
			entry.KeyFound = true
			aggregated, partial, err := s.collectRange(ctx, client, key.ID, window)
			if err != nil {
				errs[i] = err
				perUser[i] = entry
				return
			}
			entry.Credits = aggregated.credits
			entry.CreditsSaved = aggregated.creditsSaved
			entry.Requests = aggregated.requests
			entry.InputTokens = aggregated.inputTokens
			entry.OutputTokens = aggregated.outputTokens
			entry.ByModel = aggregated.byModel
			entry.ByDay = aggregated.byDay
			entry.Partial = partial || aggregated.missingCredits
			perUser[i] = entry
		}(index)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			s.logger.Warn("kiro credits: batch user range failed", zap.Error(err))
			result.Message = "Kiro 用量暂时不可用"
			return result
		}
	}

	modelBuckets := map[string]*ModelCredits{}
	dayBuckets := map[string]*DayCredits{}
	for _, entry := range perUser {
		if entry.KeyFound {
			result.KeyCount++
		}
		result.Credits += entry.Credits
		result.CreditsSaved += entry.CreditsSaved
		result.Requests += entry.Requests
		result.InputTokens += entry.InputTokens
		result.OutputTokens += entry.OutputTokens
		result.Partial = result.Partial || entry.Partial
		for _, model := range entry.ByModel {
			bucket := modelBuckets[model.Model]
			if bucket == nil {
				bucket = &ModelCredits{Model: model.Model}
				modelBuckets[model.Model] = bucket
			}
			bucket.Requests += model.Requests
			bucket.Credits += model.Credits
			bucket.InputTokens += model.InputTokens
			bucket.OutputTokens += model.OutputTokens
		}
		for _, day := range entry.ByDay {
			bucket := dayBuckets[day.Date]
			if bucket == nil {
				bucket = &DayCredits{Date: day.Date}
				dayBuckets[day.Date] = bucket
			}
			bucket.Requests += day.Requests
			bucket.Credits += day.Credits
		}
	}
	for _, bucket := range modelBuckets {
		result.ByModel = append(result.ByModel, *bucket)
	}
	sort.Slice(result.ByModel, func(i, j int) bool {
		if result.ByModel[i].Credits != result.ByModel[j].Credits {
			return result.ByModel[i].Credits > result.ByModel[j].Credits
		}
		return result.ByModel[i].Model < result.ByModel[j].Model
	})
	for _, bucket := range dayBuckets {
		result.ByDay = append(result.ByDay, *bucket)
	}
	sort.Slice(result.ByDay, func(i, j int) bool { return result.ByDay[i].Date < result.ByDay[j].Date })
	result.Users = perUser

	result.Available = true
	result.Status = StatusOK
	s.storeTeam(cacheKey, result, now)
	return result
}

func normalizeRefs(users []UserRef) []UserRef {
	seen := map[int64]struct{}{}
	refs := make([]UserRef, 0, len(users))
	for _, user := range users {
		if user.RelayUserID <= 0 {
			continue
		}
		if _, ok := seen[user.RelayUserID]; ok {
			continue
		}
		seen[user.RelayUserID] = struct{}{}
		refs = append(refs, user)
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].RelayUserID < refs[j].RelayUserID })
	return refs
}

func teamCacheKey(row *ent.RelayProvider, refs []UserRef, window window) string {
	ids := make([]string, 0, len(refs))
	for _, ref := range refs {
		ids = append(ids, strconv.FormatInt(ref.RelayUserID, 10))
	}
	return fmt.Sprintf("%d:%d:team:%s:%s:%s:%s",
		row.ID,
		row.ConfigurationVersion,
		strings.Join(ids, ","),
		window.start.Format(time.RFC3339),
		window.endExclusive.Format(time.RFC3339),
		window.location.String(),
	)
}

func (s *Service) loadTeam(key string, now time.Time) (TeamCredits, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cached, ok := s.teams[key]
	if !ok || !now.Before(cached.expiresAt) {
		if ok {
			delete(s.teams, key)
		}
		return TeamCredits{}, false
	}
	return cached.value, true
}

func (s *Service) storeTeam(key string, value TeamCredits, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.teams) > 1024 {
		for existing := range s.teams {
			delete(s.teams, existing)
		}
	}
	s.teams[key] = cachedTeam{value: value, expiresAt: now.Add(s.cacheTTL)}
}
