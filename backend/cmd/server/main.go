package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ai-efficiency/backend/ent"
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
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/prusage"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/repo"
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
	const timeout = 100 * time.Millisecond
	return &redis.Options{
		Addr:                  cfg.Addr,
		Password:              cfg.Password,
		DB:                    cfg.DB,
		MaxRetries:            -1,
		DialTimeout:           timeout,
		DialerRetries:         1,
		ReadTimeout:           timeout,
		WriteTimeout:          timeout,
		PoolTimeout:           timeout,
		ContextTimeoutEnabled: true,
	}
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
	defer redisClient.Close()
	if err := metrics.RegisterRedisPool(redisClient); err != nil {
		logger.Fatal("register Redis pool metrics", zap.Error(err))
	}
	workItemsCache, err := workitems.NewCountsCache(
		workitems.NewRedisCountsStore(redisClient),
		workItemsRevisionStore,
		workitems.CountsCacheOptions{
			Namespace: cfg.Redis.Namespace,
			Metrics:   metrics.CacheRecorder("work_items_counts"),
		},
	)
	if err != nil {
		logger.Fatal("initialize work item counts cache", zap.Error(err))
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
		WebhookPublicURL: cfg.Server.PublicURL,
		FrontendURL:      cfg.Server.FrontendURL,
		ServerMode:       cfg.Server.Mode,
		HTTPClient:       httpClients.scm,
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
	providerHandler := handler.NewProviderHandler(entClient, cfg.Encryption.Key, logger, httpClients.providerRelay)
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

	checkpointService := checkpoint.NewService(entClient)
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

	r := handler.SetupRouterWithOptions(
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
			DirectoryService:       directoryService,
			WorkItemsCache:         workItemsCache,
			WorkItemsRevisionStore: workItemsRevisionStore,
			WebhookHTTPClient:      httpClients.webhook,
			RequestLogger:          logger,
			RequestObserver:        metrics.RequestObserver(),
			Release:                versionInfo.Version,
			RequestTimeout:         time.Duration(cfg.Server.RequestTimeoutSeconds) * time.Second,
		},
	)

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

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server...")
	stopDirectoryScheduler()

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
