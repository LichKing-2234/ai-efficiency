package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	_ "github.com/ai-efficiency/backend/ent/runtime"
	"github.com/ai-efficiency/backend/internal/attribution"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/buildinfo"
	"github.com/ai-efficiency/backend/internal/checkpoint"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/handler"
	"github.com/ai-efficiency/backend/internal/health"
	"github.com/ai-efficiency/backend/internal/httpclient"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/personalusage"
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/prusage"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/relayruntime"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/representativescope"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
	"github.com/ai-efficiency/backend/internal/versioncheck"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/ai-efficiency/backend/internal/workitems"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	redis "github.com/redis/go-redis/v9"

	entsql "entgo.io/ent/dialect/sql"
	"go.uber.org/zap"
)

// authTokenAdapter adapts auth.Service to the oauth.TokenGenerator interface.
type authTokenAdapter struct {
	authService *auth.Service
}

func redisClientOptions(cfg config.RedisConfig) *redis.Options {
	return &redis.Options{
		Addr:                  cfg.Addr,
		Password:              cfg.Password,
		DB:                    cfg.DB,
		MaxRetries:            -1,
		DialTimeout:           time.Second,
		DialerRetries:         1,
		ReadTimeout:           2 * time.Second,
		WriteTimeout:          2 * time.Second,
		PoolTimeout:           time.Second,
		MinIdleConns:          4,
		ContextTimeoutEnabled: true,
	}
}

type relayProviderResolver interface {
	Resolve(context.Context, int) (relay.Provider, error)
}

type primaryTeamUsagePrewarmResolver struct {
	client   *ent.Client
	provider relayProviderResolver
}

func (r primaryTeamUsagePrewarmResolver) ResolvePrimaryProviderBinding(ctx context.Context) (teamusage.ProviderBinding, error) {
	if r.client == nil || r.provider == nil {
		return teamusage.ProviderBinding{}, fmt.Errorf("primary Team Usage prewarm resolver is unavailable")
	}
	row, err := r.client.RelayProvider.Query().
		Where(relayprovider.IsPrimary(true), relayprovider.Enabled(true)).
		Order(ent.Asc(relayprovider.FieldID)).
		First(ctx)
	if err != nil {
		return teamusage.ProviderBinding{}, fmt.Errorf("resolve primary Relay provider row: %w", err)
	}
	provider, err := r.provider.Resolve(ctx, row.ID)
	if err != nil {
		return teamusage.ProviderBinding{}, fmt.Errorf("resolve primary Relay provider runtime: %w", err)
	}
	return teamusage.ProviderBinding{
		ProviderID: row.ID, ProviderVersion: row.ConfigurationVersion, Provider: provider,
	}, nil
}

type teamUsagePrewarmRuntime struct {
	options      teamUsagePrewarmRuntimeOptions
	timezones    []string
	readerSource *teamusage.PrewarmReaderSlot
	initDone     chan struct{}
	stopDone     chan struct{}

	mu        sync.Mutex
	started   bool
	stopped   bool
	cancel    context.CancelFunc
	reader    *teamusage.PrewarmReader
	prewarmer *teamusage.Prewarmer
	wg        sync.WaitGroup
}

type teamUsagePrewarmMetricsFactory func([]string) (teamusage.PrewarmMetrics, error)

type teamUsagePrewarmRuntimeOptions struct {
	Config         config.TeamUsagePrewarmConfig
	RedisNamespace string
	Store          readcache.BatchStore
	PingRedis      func(context.Context) error
	Resolver       teamusage.PrimaryProviderBindingResolver
	MetricsFactory teamUsagePrewarmMetricsFactory
	Reporter       teamusage.PrewarmReporter
	Logger         *zap.Logger
	InitTimeout    time.Duration
}

const teamUsagePrewarmInitTimeout = 5 * time.Second

func prepareTeamUsagePrewarm(options teamUsagePrewarmRuntimeOptions) *teamUsagePrewarmRuntime {
	if !options.Config.Enabled {
		return nil
	}
	timezones, err := teamusage.NormalizePrewarmTimezones(options.Config.Timezones)
	if err != nil {
		logTeamUsagePrewarmDisabled(options.Logger, "invalid_configuration")
		return nil
	}
	if len(timezones) == 0 {
		return nil
	}
	if options.InitTimeout <= 0 {
		options.InitTimeout = teamUsagePrewarmInitTimeout
	}
	return &teamUsagePrewarmRuntime{
		options: options, timezones: timezones,
		readerSource: teamusage.NewPrewarmReaderSlot(), initDone: make(chan struct{}), stopDone: make(chan struct{}),
	}
}

func (r *teamUsagePrewarmRuntime) ReaderSource() teamusage.PrewarmReaderSource {
	if r == nil {
		return nil
	}
	return r.readerSource
}

func (r *teamUsagePrewarmRuntime) Start(parent context.Context) {
	if r == nil || parent == nil {
		return
	}
	r.mu.Lock()
	if r.started || r.stopped {
		r.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(parent)
	r.started = true
	r.cancel = cancel
	r.wg.Add(1)
	r.mu.Unlock()

	go r.initialize(runCtx)
}

func (r *teamUsagePrewarmRuntime) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.stopped {
		done := r.stopDone
		r.mu.Unlock()
		<-done
		return
	}
	r.stopped = true
	cancel := r.cancel
	r.cancel = nil
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	r.wg.Wait()
	r.readerSource.Clear()
	r.mu.Lock()
	prewarmer := r.prewarmer
	r.mu.Unlock()
	if prewarmer != nil {
		prewarmer.Stop()
	}
	close(r.stopDone)
}

func (r *teamUsagePrewarmRuntime) initialize(runCtx context.Context) {
	defer r.wg.Done()
	defer close(r.initDone)
	initCtx, cancel := context.WithTimeout(runCtx, r.options.InitTimeout)
	defer cancel()

	if r.options.Store == nil || r.options.PingRedis == nil || r.options.PingRedis(initCtx) != nil {
		logTeamUsagePrewarmDisabled(r.options.Logger, "redis_unavailable")
		return
	}
	if r.options.Resolver == nil {
		logTeamUsagePrewarmDisabled(r.options.Logger, "provider_unavailable")
		return
	}
	binding, err := r.options.Resolver.ResolvePrimaryProviderBinding(initCtx)
	if err != nil || binding.ProviderID <= 0 || binding.ProviderVersion <= 0 || binding.Provider == nil {
		logTeamUsagePrewarmDisabled(r.options.Logger, "provider_unavailable")
		return
	}
	if _, ok := binding.Provider.(relay.ProviderWideTeamUsageProvider); !ok {
		logTeamUsagePrewarmDisabled(r.options.Logger, "provider_unsupported")
		return
	}
	if _, ok := binding.Provider.(relay.ProviderWideTeamTrendProvider); !ok {
		logTeamUsagePrewarmDisabled(r.options.Logger, "provider_unsupported")
		return
	}
	if r.options.MetricsFactory == nil {
		logTeamUsagePrewarmDisabled(r.options.Logger, "telemetry_unavailable")
		return
	}
	prewarmMetrics, err := r.options.MetricsFactory(r.timezones)
	if err != nil || prewarmMetrics == nil {
		logTeamUsagePrewarmDisabled(r.options.Logger, "telemetry_unavailable")
		return
	}
	cache, err := teamusage.NewPrewarmCache(r.options.Store, teamusage.PrewarmCacheOptions{
		Namespace: r.options.RedisNamespace, Metrics: prewarmMetrics,
	})
	if err != nil {
		logTeamUsagePrewarmDisabled(r.options.Logger, "cache_unavailable")
		return
	}
	prewarmer, err := teamusage.NewPrewarmer(r.options.Resolver, cache, teamusage.PrewarmerOptions{
		Timezones: r.timezones, Metrics: prewarmMetrics, Reporter: r.options.Reporter,
	})
	if err != nil {
		logTeamUsagePrewarmDisabled(r.options.Logger, "lifecycle_unavailable")
		return
	}
	reader, err := teamusage.NewPrewarmReader(cache, prewarmer.SourceCallLimiter(), teamusage.PrewarmReaderOptions{
		Timezones: r.timezones, Metrics: prewarmMetrics,
	})
	if err != nil {
		logTeamUsagePrewarmDisabled(r.options.Logger, "reader_unavailable")
		return
	}
	if initCtx.Err() != nil || runCtx.Err() != nil {
		return
	}
	r.mu.Lock()
	if r.stopped {
		r.mu.Unlock()
		return
	}
	r.reader = reader
	r.prewarmer = prewarmer
	r.readerSource.Store(reader)
	r.mu.Unlock()
	prewarmer.Start(runCtx)
}

func logTeamUsagePrewarmDisabled(logger *zap.Logger, reason string) {
	if logger == nil {
		return
	}
	logger.Warn("Team Usage prewarm disabled", zap.String("reason", reason))
}

type teamUsagePrewarmReporter struct {
	logger *zap.Logger
}

func newTeamUsagePrewarmReporter(logger *zap.Logger) teamusage.PrewarmReporter {
	return teamUsagePrewarmReporter{logger: logger}
}

func (r teamUsagePrewarmReporter) ReportPrewarmBackground(event teamusage.PrewarmBackgroundEvent) {
	if r.logger == nil || !validTeamUsagePrewarmClass(event.Class) || !validTeamUsagePrewarmOutcome(event.Outcome) {
		return
	}
	r.logger.Info(
		"Team Usage prewarm background outcome",
		zap.String("operation_id", boundedTeamUsagePrewarmField(event.OperationID, 64)),
		zap.Int("provider_id", nonnegativeBoundedInt(event.ProviderID, 1<<31-1)),
		zap.Int64("provider_version", nonnegativeBoundedInt64(event.ProviderVersion, 1<<63-1)),
		zap.String("timezone", boundedTeamUsagePrewarmField(event.Timezone, 64)),
		zap.String("class", string(event.Class)),
		zap.String("outcome", string(event.Outcome)),
		zap.Int64("duration_ms", nonnegativeBoundedInt64(event.Duration.Milliseconds(), int64((6*time.Minute).Milliseconds()))),
		zap.Int("bytes", nonnegativeBoundedInt(event.Bytes, 64<<20)),
		zap.Int("points", nonnegativeBoundedInt(event.Points, 1_000_000)),
		zap.Int("users", nonnegativeBoundedInt(event.Users, 5_000)),
	)
}

func validTeamUsagePrewarmClass(class teamusage.PrewarmCycleClass) bool {
	switch class {
	case teamusage.PrewarmCycleMoving, teamusage.PrewarmCycleRecovery, teamusage.PrewarmCycleStartup,
		teamusage.PrewarmCycleHistory29d, teamusage.PrewarmCycleHistory6d:
		return true
	default:
		return false
	}
}

func validTeamUsagePrewarmOutcome(outcome teamusage.PrewarmCycleOutcome) bool {
	switch outcome {
	case teamusage.PrewarmCycleSuccess, teamusage.PrewarmCycleError, teamusage.PrewarmCycleCanceled, teamusage.PrewarmCycleRejected:
		return true
	default:
		return false
	}
}

func boundedTeamUsagePrewarmField(value string, limit int) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	if len(value) > limit {
		value = value[:limit]
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') &&
			(char < '0' || char > '9') && char != '-' && char != '_' && char != '/' {
			return "invalid"
		}
	}
	return value
}

func nonnegativeBoundedInt(value, limit int) int {
	if value < 0 {
		return 0
	}
	if value > limit {
		return limit
	}
	return value
}

func nonnegativeBoundedInt64(value, limit int64) int64 {
	if value < 0 {
		return 0
	}
	if value > limit {
		return limit
	}
	return value
}

type prewarmStopper interface {
	Stop()
}

func closeTeamUsagePrewarmResources(prewarm prewarmStopper, closeRedis func() error) error {
	if prewarm != nil {
		prewarm.Stop()
	}
	if closeRedis == nil {
		return nil
	}
	return closeRedis()
}

func newHTTPServer(addr string, handler http.Handler, cfg config.ServerConfig) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: time.Duration(cfg.ReadHeaderTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.IdleTimeoutSeconds) * time.Second,
	}
}

type runtimeHTTPClients struct {
	runtimeRelay  *http.Client
	providerRelay *http.Client
	directory     *http.Client
	settings      *http.Client
	scm           *http.Client
	version       *http.Client
	webhook       *http.Client
}

func httpClientOptions(cfg config.HTTPClientConfig) httpclient.Options {
	return httpclient.Options{
		ConnectTimeout:        time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
		TLSHandshakeTimeout:   time.Duration(cfg.TLSHandshakeTimeoutSeconds) * time.Second,
		ResponseHeaderTimeout: time.Duration(cfg.ResponseHeaderTimeoutSeconds) * time.Second,
		OverallTimeout:        time.Duration(cfg.OverallTimeoutSeconds) * time.Second,
		IdleConnTimeout:       time.Duration(cfg.IdleConnTimeoutSeconds) * time.Second,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		MaxConnsPerHost:       cfg.MaxConnsPerHost,
	}
}

func newRuntimeHTTPClients(cfg config.HTTPClientConfig, relayWrappers ...httpclient.TransportWrapper) runtimeHTTPClients {
	downstreamOptions := httpClientOptions(cfg)
	relayClient := httpclient.New(downstreamOptions, relayWrappers...)
	generalClient := httpclient.New(downstreamOptions)

	versionOptions := downstreamOptions
	versionOptions.OverallTimeout = config.VersionCheckTimeoutSeconds * time.Second
	version := httpclient.New(versionOptions)

	webhookOptions := downstreamOptions
	webhookOptions.OverallTimeout = config.QuotaNotificationWebhookTimeoutSeconds * time.Second
	webhook := httpclient.New(webhookOptions)

	return runtimeHTTPClients{
		runtimeRelay:  relayClient,
		providerRelay: relayClient,
		directory:     generalClient,
		settings:      relayClient,
		scm:           generalClient,
		version:       version,
		webhook:       webhook,
	}
}

func initializeRepoInventory(ctx context.Context, entClient *ent.Client, redisClient redis.UniversalClient, namespace string, metrics readcache.Metrics) (*repo.InventoryCache, *repo.InventoryRevisionStore, error) {
	revisions := repo.NewInventoryRevisionStore(entClient)
	if err := revisions.Ensure(ctx); err != nil {
		return nil, nil, fmt.Errorf("initialize repository inventory revision: %w", err)
	}
	cache, err := repo.NewInventoryCache(
		readcache.NewRedisStore(redisClient),
		revisions,
		repo.InventoryCacheOptions{Namespace: namespace, Metrics: metrics},
	)
	if err != nil {
		return nil, nil, fmt.Errorf("initialize repository inventory cache: %w", err)
	}
	return cache, revisions, nil
}

func (a *authTokenAdapter) GenerateAccessToken(userID int, username, role string) (string, string, int, error) {
	info := &auth.UserInfo{
		ID:       userID,
		Username: username,
		Role:     role,
	}
	pair, err := a.authService.GenerateTokenPairForUser(info)
	if err != nil {
		return "", "", 0, err
	}
	return pair.AccessToken, pair.RefreshToken, pair.ExpiresIn, nil
}

func main() {
	// Init logger
	logger, _ := zap.NewProduction()
	if os.Getenv("AE_SERVER_MODE") == "debug" {
		logger, _ = zap.NewDevelopment()
	}
	defer logger.Sync()

	// Load config
	explicitConfigPath := strings.TrimSpace(os.Getenv("AE_CONFIG_PATH"))
	settingsConfigPath := config.ResolveWritableConfigPath(explicitConfigPath, config.ResolveRuntimeStateDir(os.Getenv))
	loadConfigPath := settingsConfigPath
	if explicitConfigPath == "" {
		if _, statErr := os.Stat(loadConfigPath); statErr != nil {
			if os.IsNotExist(statErr) {
				loadConfigPath = ""
			} else {
				logger.Fatal("stat writable config", zap.String("path", loadConfigPath), zap.Error(statErr))
			}
		}
	}

	cfg, err := config.Load(loadConfigPath)
	if err != nil {
		logger.Fatal("load config", zap.Error(err))
	}
	if err := config.EnsureWritableConfigFile(settingsConfigPath, cfg); err != nil {
		logger.Fatal("ensure writable config", zap.String("path", settingsConfigPath), zap.Error(err))
	}
	versionInfo := buildinfo.CurrentVersion()
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "version") {
		fmt.Println(versionInfo.Version)
		return
	}
	logger.Info(
		"build metadata",
		zap.String("version", versionInfo.Version),
		zap.String("commit", versionInfo.Commit),
		zap.String("build_time", versionInfo.BuildTime),
	)
	if config.RequireExplicitDBDSN(cfg.DB.DSN) {
		logger.Fatal("DB.DSN is required and must point to PostgreSQL")
	}
	metrics := telemetry.NewMetrics(versionInfo.Version)
	cacheMetrics := newProductionCacheMetrics(metrics)
	httpClients := newRuntimeHTTPClients(
		cfg.HTTPClient,
		telemetry.WrapDependency(logger, versionInfo.Version, "relay", "http_request", metrics.DependencyObserver()),
	)
	defer httpClients.runtimeRelay.CloseIdleConnections()
	defer httpClients.directory.CloseIdleConnections()
	defer httpClients.version.CloseIdleConnections()
	defer httpClients.webhook.CloseIdleConnections()

	// Set gin mode
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Connect to ai_efficiency database
	db, err := sql.Open("postgres", cfg.DB.DSN)
	if err != nil {
		logger.Fatal("connect ai_efficiency db", zap.Error(err))
	}
	sqlDB := db
	defer db.Close()
	db.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	db.SetMaxIdleConns(cfg.DB.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.DB.ConnMaxLifetime) * time.Second)
	if err := metrics.RegisterDBPool(db); err != nil {
		logger.Fatal("register database pool metrics", zap.Error(err))
	}

	if err := db.Ping(); err != nil {
		logger.Fatal("ping ai_efficiency db", zap.Error(err))
	}
	drv := entsql.OpenDB("postgres", db)
	entClient := ent.NewClient(ent.Driver(drv))
	logger.Info("connected to ai_efficiency database (PostgreSQL)")
	defer entClient.Close()

	// Auto-migrate
	if err := entClient.Schema.Create(context.Background()); err != nil {
		logger.Fatal("ent auto-migrate", zap.Error(err))
	}
	if err := dropLegacyRelayProviderAdminURL(context.Background(), db); err != nil {
		logger.Fatal("drop legacy relay provider admin_url", zap.Error(err))
	}
	logger.Info("database schema migrated")
	workItemsRevisionStore := workitems.NewRevisionStore(entClient)
	if err := workItemsRevisionStore.Ensure(context.Background()); err != nil {
		logger.Fatal("initialize work item counts revision", zap.Error(err))
	}
	if err := ensurePrimaryRelayProviderFromConfig(context.Background(), entClient, cfg.Relay, cfg.Encryption.Key); err != nil {
		logger.Fatal("bootstrap primary relay provider from config", zap.Error(err))
	}
	backfillResult, err := credential.BackfillLegacySCMCredentials(context.Background(), entClient, cfg.Encryption.Key)
	if err != nil {
		logger.Fatal("backfill legacy scm credentials", zap.Error(err))
	}
	if backfillResult != nil && len(backfillResult.Skipped) > 0 {
		logger.Warn("skipped legacy scm credential backfill rows", zap.Strings("providers", backfillResult.Skipped))
	}

	runtimeRelayCfg, relayConfigSource, err := resolveRuntimeRelayConfig(context.Background(), cfg.Relay, func(ctx context.Context) (*config.RelayConfig, error) {
		return loadPrimaryRelayConfig(ctx, entClient, cfg.Encryption.Key)
	})
	if err != nil {
		logger.Fatal("resolve runtime relay config", zap.Error(err))
	}
	cfg.Relay = runtimeRelayCfg

	// Init relay provider
	var relayProvider relay.Provider
	if cfg.Relay.URL != "" {
		relayProvider = relay.NewSub2apiProvider(
			httpClients.runtimeRelay,
			cfg.Relay.URL,
			cfg.Relay.AdminAPIKey,
			cfg.Relay.Model,
			logger,
		)
		logger.Info("relay provider initialized",
			zap.String("provider", cfg.Relay.Provider),
			zap.String("url", cfg.Relay.URL),
			zap.String("source", relayConfigSource),
		)
	}

	redisClient := redis.NewClient(redisClientOptions(cfg.Redis))
	var prewarmRuntime *teamUsagePrewarmRuntime
	defer func() {
		if err := closeTeamUsagePrewarmResources(prewarmRuntime, redisClient.Close); err != nil {
			logger.Warn("close Redis client", zap.Error(err))
		}
	}()
	if err := metrics.RegisterRedisPool(redisClient); err != nil {
		logger.Fatal("register Redis pool metrics", zap.Error(err))
	}
	redisStore := readcache.NewRedisStore(redisClient)
	providerInvalidationBus, err := relayruntime.NewRedisInvalidationBus(redisClient, cfg.Redis.Namespace)
	if err != nil {
		logger.Fatal("initialize relay provider invalidation bus", zap.Error(err))
	}
	providerRuntime, err := relayruntime.NewManager(entClient, cfg.Encryption.Key, logger, relayruntime.Options{
		Namespace:       cfg.Redis.Namespace,
		Store:           redisStore,
		Bus:             providerInvalidationBus,
		MetadataMetrics: cacheMetrics.providerMetadata,
		Factory: func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error) {
			return relay.NewSub2apiProvider(
				httpClients.providerRelay,
				row.BaseURL,
				adminAPIKey,
				row.DefaultModel,
				logger,
			), nil
		},
	})
	if err != nil {
		logger.Fatal("initialize relay provider runtime", zap.Error(err))
	}
	providerRuntimeCtx, stopProviderRuntime := context.WithCancel(context.Background())
	defer stopProviderRuntime()
	providerRuntime.Start(providerRuntimeCtx)
	workItemsCache, err := workitems.NewCountsCache(
		redisStore,
		workItemsRevisionStore,
		workitems.CountsCacheOptions{
			Namespace: cfg.Redis.Namespace,
			Metrics:   cacheMetrics.workItemsCounts,
		},
	)
	if err != nil {
		logger.Fatal("initialize work item counts cache", zap.Error(err))
	}
	personalUsageCache, err := personalusage.NewCache(
		redisStore,
		personalusage.CacheOptions{Namespace: cfg.Redis.Namespace, Metrics: cacheMetrics.personalUsage},
	)
	if err != nil {
		logger.Fatal("initialize personal usage cache", zap.Error(err))
	}
	representativeScopeCache, err := representativescope.NewCache(
		redisStore,
		representativescope.CacheOptions{Namespace: cfg.Redis.Namespace, Metrics: cacheMetrics.representativeScope},
	)
	if err != nil {
		logger.Fatal("initialize representative scope cache", zap.Error(err))
	}
	teamUsageSnapshotCache, err := teamusage.NewSnapshotCache(
		redisStore,
		teamusage.SnapshotCacheOptions{
			Namespace:           cfg.Redis.Namespace,
			SummaryMetrics:      cacheMetrics.teamUsageSummary,
			TrendMetrics:        cacheMetrics.teamUsageTrend,
			MembersMetrics:      cacheMetrics.teamUsageMembers,
			OrganizationMetrics: cacheMetrics.teamUsageOrg,
		},
	)
	if err != nil {
		logger.Fatal("initialize team usage snapshot cache", zap.Error(err))
	}
	teamUsageOriginCache, err := teamusage.NewOriginCache(
		redisStore,
		teamusage.OriginCacheOptions{
			Namespace: cfg.Redis.Namespace,
			Metrics:   cacheMetrics.teamUsageOrigin,
		},
	)
	if err != nil {
		logger.Fatal("initialize team usage origin cache", zap.Error(err))
	}
	prewarmRuntime = prepareTeamUsagePrewarm(teamUsagePrewarmRuntimeOptions{
		Config: cfg.TeamUsagePrewarm, RedisNamespace: cfg.Redis.Namespace, Store: redisStore,
		PingRedis: func(ctx context.Context) error {
			return redisClient.Ping(ctx).Err()
		},
		Resolver:       primaryTeamUsagePrewarmResolver{client: entClient, provider: providerRuntime},
		MetricsFactory: cacheMetrics.teamUsagePrewarm,
		Reporter:       newTeamUsagePrewarmReporter(logger),
		Logger:         logger,
	})
	repoInventoryCache, repoInventoryRevisions, err := initializeRepoInventory(
		context.Background(),
		entClient,
		redisClient,
		cfg.Redis.Namespace,
		cacheMetrics.repositoryInventory,
	)
	if err != nil {
		logger.Fatal("initialize repository inventory", zap.Error(err))
	}

	// Init LDAP config (shared between auth service and admin settings handler)
	var ldapConfig atomic.Pointer[config.LDAPConfig]
	ldapConfig.Store(&cfg.Auth.LDAP)

	// Init auth service
	authService := auth.NewService(
		entClient,
		cfg.Auth.JWTSecret,
		cfg.Auth.AccessTokenTTL,
		cfg.Auth.RefreshTokenTTL,
		logger,
		cfg.Encryption.Key,
	)
	// When relay is configured, allow LDAP logins to provision/resolve a relay-side identity
	// (by stable username) for session/PR attribution.
	var relayIdentityResolver *auth.RelayIdentityResolver
	if relayProvider != nil {
		relayIdentityResolver = auth.NewRelayIdentityResolver(relayProvider, "")
		authService.SetRelayIdentityResolver(relayIdentityResolver)
	}

	// Register auth providers
	if relayProvider != nil {
		authService.RegisterProvider(auth.NewSSOProvider(relayProvider, logger))
	}
	authService.RegisterProvider(auth.NewLDAPProvider(&ldapConfig, logger))

	// Init repo service
	repoService := repo.NewService(entClient, cfg.Encryption.Key, logger, repo.ServiceOptions{
		WebhookPublicURL:       cfg.Server.PublicURL,
		FrontendURL:            cfg.Server.FrontendURL,
		ServerMode:             cfg.Server.Mode,
		HTTPClient:             httpClients.scm,
		InventoryCache:         repoInventoryCache,
		InventoryRevisionStore: repoInventoryRevisions,
	})

	// Init PR labeler (with optional relay usage stats lookup)
	labeler := efficiency.NewLabeler(entClient, relayProvider, logger)

	// Init webhook handler (with labeler for auto-labeling on PR events)
	webhookHandler := webhook.NewHandler(entClient, labeler, logger)
	prUsageService := prusage.NewService(entClient)
	syncService := prsync.NewService(entClient, labeler, logger, prUsageService)

	// Setup router
	var relayRuntimeUpdater interface {
		SetAdminAPIKey(string)
		SetModel(string)
	}
	if u, ok := relayProvider.(interface {
		SetAdminAPIKey(string)
		SetModel(string)
	}); ok {
		relayRuntimeUpdater = u
	}
	settingsHandler := handler.NewSettingsHandlerWithHTTPClient(settingsConfigPath, cfg.Relay, logger, httpClients.settings, relayRuntimeUpdater)

	// Init OAuth handler
	oauthServer := oauth.NewServer()
	oauthHandler := oauth.NewHandler(oauthServer, cfg.Server.FrontendURL, &authTokenAdapter{authService: authService})

	// Init provider handler
	providerHandler, err := handler.NewProviderHandler(entClient, cfg.Encryption.Key, logger, providerRuntime)
	if err != nil {
		logger.Fatal("initialize relay provider handler", zap.Error(err))
	}
	directoryService := directorysync.NewService(entClient, directorysync.ServiceOptions{
		Executor:                  directorysync.NewExecutor(directorysync.ExecutorOptions{HTTPClient: httpClients.directory}),
		Credentials:               directorysync.NewEntCredentialResolver(entClient, cfg.Encryption.Key),
		RelayDisablers:            directorysync.NewProviderRelayDisablerResolver(providerHandler),
		TokenRevoker:              authService,
		WorkItemCountsInvalidator: workItemsRevisionStore,
	})
	directorySchedulerCtx, stopDirectoryScheduler := context.WithCancel(context.Background())
	defer stopDirectoryScheduler()
	directoryService.StartScheduler(directorySchedulerCtx, time.Minute)

	// Init admin settings handler
	adminSettingsHandler := handler.NewAdminSettingsHandler(settingsConfigPath, &ldapConfig)

	checkpointService := checkpoint.NewService(entClient, checkpoint.ServiceOptions{
		InventoryRevisionStore: repoInventoryRevisions,
		RepoService:            repoService,
	})
	checkpointHandler := handler.NewCheckpointHandler(checkpointService)
	attributionService := attribution.NewService(entClient, relayProvider)
	handler.SetPRAttributionService(attributionService)
	handler.SetPRUsageService(prUsageService)
	var relayPinger health.Pinger
	if relayProvider != nil {
		relayPinger = health.FuncPinger(func(ctx context.Context) error {
			return relayProvider.Ping(ctx)
		})
	}
	healthService := health.NewService(
		health.FuncPinger(func(ctx context.Context) error {
			if sqlDB == nil {
				return nil
			}
			return sqlDB.PingContext(ctx)
		}),
		health.FuncPinger(func(ctx context.Context) error {
			if redisClient == nil {
				return nil
			}
			return redisClient.Ping(ctx).Err()
		}),
		relayPinger,
		versionInfo,
		health.WithReadyTimeout(time.Duration(cfg.Server.ReadinessTimeoutSeconds)*time.Second),
	)
	var releaseSource versioncheck.ReleaseSource
	if cfg.VersionCheck.Enabled && strings.TrimSpace(cfg.VersionCheck.ReleaseAPIURL) != "" {
		releaseSource = versioncheck.NewGitHubReleaseSource(httpClients.version, cfg.VersionCheck.ReleaseAPIURL)
	}
	versionCheckService := versioncheck.NewService(versionInfo, releaseSource)
	healthHandler := handler.NewHealthHandler(healthService, versionCheckService)
	webVitalsHandler := handler.NewWebVitalsHandler(metrics, handler.WebVitalsOptions{})

	r, err := handler.SetupRouter(
		entClient,
		sqlDB,
		authService,
		repoService,
		webhookHandler,
		syncService,
		settingsHandler,
		cfg.Encryption.Key,
		cfg.Server.PublicURL,
		middleware.CORS([]string{cfg.Server.FrontendURL}),
		oauthHandler,
		providerHandler,
		adminSettingsHandler,
		checkpointHandler,
		healthHandler,
		handler.RouterOptions{
			DirectoryService:             directoryService,
			PersonalUsageCache:           personalUsageCache,
			WorkItemsCache:               workItemsCache,
			WorkItemsRevisionStore:       workItemsRevisionStore,
			RepresentativeScopeCache:     representativeScopeCache,
			TeamUsageSnapshotCache:       teamUsageSnapshotCache,
			TeamUsageOriginCache:         teamUsageOriginCache,
			TeamUsagePrewarmReaderSource: prewarmRuntime.ReaderSource(),
			TeamUsageCursorSecret:        cfg.Encryption.Key,
			WebhookHTTPClient:            httpClients.webhook,
			RequestLogger:                logger,
			RequestObserver:              metrics.RequestObserver(),
			WebVitalsHandler:             webVitalsHandler,
			Release:                      versionInfo.Version,
			RequestTimeout:               time.Duration(cfg.Server.RequestTimeoutSeconds) * time.Second,
		},
	)
	if err != nil {
		logger.Fatal("initialize HTTP router", zap.Error(err))
	}

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := newHTTPServer(addr, r, cfg.Server)
	metricsSrv := newMetricsServer(cfg.Metrics.ListenAddress, metrics.Handler(), cfg.Server)

	go func() {
		logger.Info("starting server", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()
	go func() {
		logger.Info("starting metrics server", zap.String("addr", cfg.Metrics.ListenAddress))
		if err := metricsSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("metrics server error", zap.Error(err))
		}
	}()
	if prewarmRuntime != nil {
		prewarmRuntime.Start(context.Background())
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server...")
	stopDirectoryScheduler()
	if prewarmRuntime != nil {
		prewarmRuntime.Stop()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("server shutdown", zap.Error(err))
	}
	if err := metricsSrv.Shutdown(ctx); err != nil {
		logger.Fatal("metrics server shutdown", zap.Error(err))
	}
	logger.Info("server stopped")
}
