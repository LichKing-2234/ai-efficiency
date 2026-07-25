package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	_ "github.com/ai-efficiency/backend/ent/runtime"
	"github.com/ai-efficiency/backend/internal/buildinfo"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/httpclient"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/redisruntime"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/relayruntime"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/telemetry"
	_ "github.com/lib/pq"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const refreshInterval = time.Minute

type providerManager interface {
	Resolve(context.Context, int) (relay.Provider, error)
}

type workerDependencies struct {
	openDatabase       func(string, string) (*sql.DB, error)
	newRedisClient     func(config.RedisConfig) redis.UniversalClient
	newProviderManager func(*ent.Client, string, *zap.Logger, relayruntime.Options) (providerManager, error)
	newRefresher       func(teamusage.PrimaryProviderBindingResolver, *teamusage.PrewarmCache, teamusage.RefresherOptions) (teamusage.Refresher, error)
}

func defaultWorkerDependencies() workerDependencies {
	return workerDependencies{
		openDatabase:   sql.Open,
		newRedisClient: func(cfg config.RedisConfig) redis.UniversalClient { return redisruntime.NewClient(cfg) },
		newProviderManager: func(client *ent.Client, encryptionKey string, logger *zap.Logger, options relayruntime.Options) (providerManager, error) {
			return relayruntime.NewManager(client, encryptionKey, logger, options)
		},
		newRefresher: teamusage.NewRefresher,
	}
}

type primaryProviderBindingResolver struct {
	client  *ent.Client
	manager providerManager
}

func (r primaryProviderBindingResolver) ResolvePrimaryProviderBinding(ctx context.Context) (teamusage.ProviderBinding, error) {
	if r.client == nil || r.manager == nil {
		return teamusage.ProviderBinding{}, fmt.Errorf("primary Relay provider resolver is not configured")
	}
	row, err := r.client.RelayProvider.Query().
		Where(relayprovider.IsPrimaryEQ(true), relayprovider.EnabledEQ(true)).
		Only(ctx)
	if err != nil {
		return teamusage.ProviderBinding{}, fmt.Errorf("query enabled primary Relay provider: %w", err)
	}
	provider, err := r.manager.Resolve(ctx, row.ID)
	if err != nil {
		return teamusage.ProviderBinding{}, fmt.Errorf("resolve primary Relay provider %d: %w", row.ID, err)
	}
	return teamusage.ProviderBinding{
		ProviderID: row.ID, ProviderVersion: row.ConfigurationVersion, Provider: provider,
	}, nil
}

type workerRuntime struct {
	refresher   teamusage.Refresher
	sqlDB       *sql.DB
	entClient   *ent.Client
	redisClient redis.UniversalClient
	relayHTTP   *http.Client

	closeOnce sync.Once
	closeErr  error
}

func (r *workerRuntime) Close() error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		var errs []error
		if r.redisClient != nil {
			errs = appendCloseError(errs, "close Redis client", r.redisClient.Close())
		}
		if r.entClient != nil {
			errs = appendCloseError(errs, "close Ent client", r.entClient.Close())
		}
		if r.sqlDB != nil {
			errs = appendCloseError(errs, "close PostgreSQL connection", r.sqlDB.Close())
		}
		if r.relayHTTP != nil {
			r.relayHTTP.CloseIdleConnections()
		}
		r.closeErr = errors.Join(errs...)
	})
	return r.closeErr
}

func appendCloseError(errs []error, operation string, err error) []error {
	if err == nil || errors.Is(err, sql.ErrConnDone) {
		return errs
	}
	return append(errs, fmt.Errorf("%s: %w", operation, err))
}

func bootstrapWorker(
	ctx context.Context,
	cfg *config.Config,
	logger *zap.Logger,
	metrics *telemetry.Metrics,
	deps workerDependencies,
) (_ *workerRuntime, err error) {
	if cfg == nil {
		return nil, fmt.Errorf("worker config is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	if metrics == nil {
		return nil, fmt.Errorf("worker metrics registry is required")
	}
	if config.RequireExplicitDBDSN(cfg.DB.DSN) {
		return nil, fmt.Errorf("DB.DSN is required and must point to PostgreSQL")
	}
	if err := config.ValidateRedisNamespace(cfg.Redis.Namespace); err != nil {
		return nil, fmt.Errorf("invalid Redis namespace: %w", err)
	}
	if strings.TrimSpace(cfg.Encryption.Key) == "" {
		return nil, fmt.Errorf("encryption key is required")
	}
	timezones, err := teamusage.NormalizePrewarmTimezones(cfg.Prewarmer.Timezones)
	if err != nil {
		return nil, fmt.Errorf("normalize prewarmer timezones: %w", err)
	}
	if len(timezones) == 0 {
		return nil, fmt.Errorf("at least one prewarmer timezone is required")
	}
	if deps.openDatabase == nil || deps.newRedisClient == nil || deps.newProviderManager == nil || deps.newRefresher == nil {
		return nil, fmt.Errorf("worker dependencies are not configured")
	}

	runtime := &workerRuntime{}
	defer func() {
		if err != nil {
			_ = runtime.Close()
		}
	}()

	db, err := deps.openDatabase("postgres", cfg.DB.DSN)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL connection: %w", err)
	}
	runtime.sqlDB = db
	db.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	db.SetMaxIdleConns(cfg.DB.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.DB.ConnMaxLifetime) * time.Second)
	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping PostgreSQL: %w", err)
	}
	if err := metrics.RegisterDBPool(db); err != nil {
		return nil, fmt.Errorf("register PostgreSQL pool metrics: %w", err)
	}
	driver := entsql.OpenDB("postgres", db)
	runtime.entClient = ent.NewClient(ent.Driver(driver))

	runtime.redisClient = deps.newRedisClient(cfg.Redis)
	if runtime.redisClient == nil {
		return nil, fmt.Errorf("Redis client factory returned nil")
	}
	if err := runtime.redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	if err := metrics.RegisterRedisPool(runtime.redisClient); err != nil {
		return nil, fmt.Errorf("register Redis pool metrics: %w", err)
	}
	store := readcache.NewRedisStore(runtime.redisClient)
	cache, err := teamusage.NewPrewarmCache(store, teamusage.PrewarmCacheOptions{Namespace: cfg.Redis.Namespace})
	if err != nil {
		return nil, fmt.Errorf("initialize Team Usage prewarm cache: %w", err)
	}
	prewarmMetrics, err := metrics.TeamUsagePrewarmRecorder(timezones)
	if err != nil {
		return nil, fmt.Errorf("register Team Usage prewarm metrics: %w", err)
	}
	runtime.relayHTTP = newWorkerRelayHTTPClient(cfg.HTTPClient, logger, metrics)
	manager, err := deps.newProviderManager(runtime.entClient, cfg.Encryption.Key, logger, relayruntime.Options{
		Namespace: cfg.Redis.Namespace,
		Store:     store,
		Factory: func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error) {
			return relay.NewSub2apiProvider(
				runtime.relayHTTP, row.BaseURL, adminAPIKey, row.DefaultModel, logger,
			), nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Relay provider manager: %w", err)
	}
	resolver := primaryProviderBindingResolver{client: runtime.entClient, manager: manager}
	runtime.refresher, err = deps.newRefresher(resolver, cache, teamusage.RefresherOptions{
		Timezones: timezones,
		Metrics:   prewarmMetrics,
		Reporter:  refreshLogReporter{logger: logger},
	})
	if err != nil {
		return nil, fmt.Errorf("initialize Team Usage refresher: %w", err)
	}
	return runtime, nil
}

func newWorkerRelayHTTPClient(cfg config.HTTPClientConfig, logger *zap.Logger, metrics *telemetry.Metrics) *http.Client {
	return httpclient.New(httpclient.Options{
		ConnectTimeout:        time.Duration(cfg.ConnectTimeoutSeconds) * time.Second,
		TLSHandshakeTimeout:   time.Duration(cfg.TLSHandshakeTimeoutSeconds) * time.Second,
		ResponseHeaderTimeout: time.Duration(cfg.ResponseHeaderTimeoutSeconds) * time.Second,
		OverallTimeout:        time.Duration(cfg.OverallTimeoutSeconds) * time.Second,
		IdleConnTimeout:       time.Duration(cfg.IdleConnTimeoutSeconds) * time.Second,
		MaxIdleConns:          cfg.MaxIdleConns,
		MaxIdleConnsPerHost:   cfg.MaxIdleConnsPerHost,
		MaxConnsPerHost:       cfg.MaxConnsPerHost,
	}, telemetry.WrapDependency(
		logger, buildinfo.CurrentVersion().Version, "relay", "http_request", metrics.DependencyObserver(),
	))
}

type refreshLogReporter struct {
	logger *zap.Logger
}

func (r refreshLogReporter) ReportRefresh(report teamusage.RefreshReport) {
	if r.logger == nil {
		return
	}
	r.logger.Info("Team Usage prewarm refresh completed",
		zap.String("outcome", string(report.Outcome)),
		zap.Duration("duration", report.Duration),
		zap.Int("planned_lanes", report.PlannedLanes),
		zap.Int("published_lanes", report.PublishedLanes),
		zap.Int("directory_sources", report.SourceCounts[teamusage.PrewarmSourceDirectory]),
		zap.Int("current_stats_sources", report.SourceCounts[teamusage.PrewarmSourceCurrentStats]),
		zap.Int("today_hour_sources", report.SourceCounts[teamusage.PrewarmSourceTodayHour]),
		zap.Int("history_6d_sources", report.SourceCounts[teamusage.PrewarmSourceHistory6d]),
		zap.Int("history_29d_sources", report.SourceCounts[teamusage.PrewarmSourceHistory29d]),
	)
}

func runLoop(
	ctx context.Context,
	refresher teamusage.Refresher,
	ticks <-chan time.Time,
	report func(error),
) error {
	if refresher == nil {
		return fmt.Errorf("Team Usage refresher is required")
	}
	if report == nil {
		report = func(error) {}
	}
	for {
		report(refresher.Refresh(ctx))
		select {
		case <-ctx.Done():
			return nil
		case <-ticks:
		}
	}
}

func newWorkerMetricsServer(cfg *config.Config, handler http.Handler) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", handler)
	return &http.Server{
		Addr:              cfg.Metrics.ListenAddress,
		Handler:           mux,
		ReadHeaderTimeout: time.Duration(cfg.Server.ReadHeaderTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.Server.IdleTimeoutSeconds) * time.Second,
	}
}

func runWorker(ctx context.Context, cfg *config.Config, logger *zap.Logger, metrics *telemetry.Metrics) (err error) {
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	runtime, err := bootstrapWorker(workerCtx, cfg, logger, metrics, defaultWorkerDependencies())
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, runtime.Close()) }()

	metricsServer := newWorkerMetricsServer(cfg, metrics.Handler())
	metricsErrors := make(chan error, 1)
	go func() {
		if serveErr := metricsServer.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			metricsErrors <- serveErr
		}
	}()
	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()
	loopDone := make(chan error, 1)
	// The Refresher reporter emits the bounded outcome; never log raw refresh errors here.
	go func() { loopDone <- runLoop(workerCtx, runtime.refresher, ticker.C, func(error) {}) }()

	select {
	case err = <-loopDone:
	case serveErr := <-metricsErrors:
		err = fmt.Errorf("serve worker metrics: %w", serveErr)
		cancel()
		<-loopDone
	}
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if shutdownErr := metricsServer.Shutdown(shutdownCtx); shutdownErr != nil {
		err = errors.Join(err, fmt.Errorf("shutdown worker metrics: %w", shutdownErr))
	}
	return err
}

func runMain() int {
	logger, _ := zap.NewProduction()
	if os.Getenv("AE_SERVER_MODE") == "debug" {
		logger, _ = zap.NewDevelopment()
	}
	defer func() { _ = logger.Sync() }()
	cfg, err := config.Load(strings.TrimSpace(os.Getenv("AE_CONFIG_PATH")))
	if err != nil {
		logger.Error("load worker config", zap.Error(err))
		return 1
	}
	metrics := telemetry.NewMetrics(buildinfo.CurrentVersion().Version)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := runWorker(ctx, cfg, logger, metrics); err != nil {
		logger.Error("prewarmer stopped", zap.Error(err))
		return 1
	}
	return 0
}

func main() {
	os.Exit(runMain())
}
