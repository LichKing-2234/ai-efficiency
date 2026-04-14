package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ai-efficiency/backend/ent"
	_ "github.com/ai-efficiency/backend/ent/runtime"
	"github.com/ai-efficiency/backend/internal/analysis"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/attribution"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/checkpoint"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/deployment"
	"github.com/ai-efficiency/backend/internal/efficiency"
	"github.com/ai-efficiency/backend/internal/handler"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/prsync"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/sessionbootstrap"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	redis "github.com/redis/go-redis/v9"

	entsql "entgo.io/ent/dialect/sql"
	"go.uber.org/zap"
)

const (
	sessionStaleSweepInterval = 1 * time.Minute
	sessionStaleAbandonAfter  = 5 * time.Minute
)

// authTokenAdapter adapts auth.Service to the oauth.TokenGenerator interface.
type authTokenAdapter struct {
	authService *auth.Service
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
	settingsConfigPath := config.ResolveWritableConfigPath(explicitConfigPath, os.Getenv("AE_DEPLOYMENT_STATE_DIR"))
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
	versionInfo := deployment.CurrentVersion()
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
	if deployment.RequireExplicitDBDSN(cfg.DB.DSN) {
		logger.Fatal("DB.DSN is required and must point to PostgreSQL")
	}

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
	logger.Info("database schema migrated")
	backfillResult, err := credential.BackfillLegacySCMCredentials(context.Background(), entClient, cfg.Encryption.Key)
	if err != nil {
		logger.Fatal("backfill legacy scm credentials", zap.Error(err))
	}
	if backfillResult != nil && len(backfillResult.Skipped) > 0 {
		logger.Warn("skipped legacy scm credential backfill rows", zap.Strings("providers", backfillResult.Skipped))
	}

	// Init relay provider
	var relayProvider relay.Provider
	if cfg.Relay.URL != "" {
		relayProvider = relay.NewSub2apiProvider(
			http.DefaultClient,
			cfg.Relay.URL,
			cfg.Relay.URL,
			cfg.Relay.APIKey,
			cfg.Relay.Model,
			logger,
		)
		if updater, ok := relayProvider.(interface{ SetAdminAPIKey(string) }); ok {
			updater.SetAdminAPIKey(cfg.Relay.AdminAPIKey)
		}
		logger.Info("relay provider initialized", zap.String("provider", cfg.Relay.Provider), zap.String("url", cfg.Relay.URL))
	}

	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.Redis.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	})
	defer redisClient.Close()

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
	repoService := repo.NewService(entClient, cfg.Encryption.Key, logger)

	// Init analysis service
	dataDir := os.Getenv("AE_DATA_DIR")
	if dataDir == "" {
		dataDir = "data"
	}
	analysisCloner := analysis.NewCloner(dataDir, logger)
	llmAnalyzer := llm.NewAnalyzer(cfg.Analysis.LLM, relayProvider, logger)
	analysisService := analysis.NewService(entClient, analysisCloner, llmAnalyzer, logger, cfg.Encryption.Key)

	// Init PR labeler (with optional relay usage stats lookup)
	labeler := efficiency.NewLabeler(entClient, relayProvider, logger)
	aggregator := efficiency.NewAggregator(entClient, logger)

	// Init webhook handler (with labeler for auto-labeling on PR events)
	webhookHandler := webhook.NewHandler(entClient, labeler, logger)
	syncService := prsync.NewService(entClient, labeler, aggregator, logger)

	// Init optimizer
	optimizer := analysis.NewOptimizer(llmAnalyzer, logger)

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
	settingsHandler := handler.NewSettingsHandler(settingsConfigPath, cfg.Relay, llmAnalyzer, logger, relayRuntimeUpdater)
	chatHandler := handler.NewChatHandler(entClient, llmAnalyzer, dataDir, logger)

	// Init OAuth handler
	oauthServer := oauth.NewServer()
	oauthHandler := oauth.NewHandler(oauthServer, cfg.Server.FrontendURL, &authTokenAdapter{authService: authService})

	// Init provider handler
	providerHandler := handler.NewProviderHandler(entClient, cfg.Encryption.Key, logger)

	// Init admin settings handler
	adminSettingsHandler := handler.NewAdminSettingsHandler(settingsConfigPath, &ldapConfig)

	// Init session bootstrap lifecycle service (ae-cli start/heartbeat/stop).
	var sessionBootstrapSvc *sessionbootstrap.Service
	if relayProvider != nil {
		sessionBootstrapSvc = sessionbootstrap.NewService(
			entClient,
			relayProvider,
			relayIdentityResolver,
			cfg.Relay.Provider,
			cfg.Relay.URL,
			cfg.Relay.DefaultGroupID,
			24*time.Hour,
			cfg.Encryption.Key,
		)
	}
	checkpointService := checkpoint.NewService(entClient)
	checkpointHandler := handler.NewCheckpointHandler(checkpointService)
	attributionService := attribution.NewService(entClient, relayProvider)
	handler.SetPRAttributionService(attributionService)
	var relayPinger deployment.Pinger
	if relayProvider != nil {
		relayPinger = deployment.FuncPinger(func(ctx context.Context) error {
			return relayProvider.Ping(ctx)
		})
	}
	healthService := deployment.NewHealthService(
		deployment.FuncPinger(func(ctx context.Context) error {
			if sqlDB == nil {
				return nil
			}
			return sqlDB.PingContext(ctx)
		}),
		deployment.FuncPinger(func(ctx context.Context) error {
			if redisClient == nil {
				return nil
			}
			return redisClient.Ping(ctx).Err()
		}),
		relayPinger,
		deployment.CurrentVersion(),
	)
	deploymentHTTPClient := &http.Client{
		Timeout: 10 * time.Second,
	}
	var releaseSource deployment.ReleaseSource
	if cfg.Deployment.Update.Enabled && cfg.Deployment.Update.ReleaseAPIURL != "" {
		releaseSource = deployment.NewGitHubReleaseSource(deploymentHTTPClient, cfg.Deployment.Update.ReleaseAPIURL)
	}
	var updaterClient deployment.Updater
	if cfg.Deployment.Update.Enabled && cfg.Deployment.Update.UpdaterURL != "" {
		updaterClient = deployment.NewUpdaterClient(deploymentHTTPClient, cfg.Deployment.Update.UpdaterURL)
	}
	var systemdUpdater deployment.SystemdUpdater
	var systemdManager deployment.RestartManager
	if cfg.Deployment.Mode == "systemd" {
		systemdManager = deployment.NewSystemdServiceManager(
			deployment.SystemdServiceConfig{ServiceName: "ai-efficiency"},
			nil,
		)
		if cfg.Deployment.Update.Enabled {
			systemdUpdater = deployment.NewSystemdBinaryUpdater(deployment.SystemdBinaryConfig{
				InstallDir:  "/opt/ai-efficiency",
				BinaryName:  "ai-efficiency-server",
				BackupName:  "ai-efficiency-server.backup",
				DownloadDir: filepath.Join(os.TempDir(), "ai-efficiency-update"),
				HTTPClient:  deploymentHTTPClient,
			})
		}
	} else {
		systemdManager = deployment.NewSystemdServiceManager(
			deployment.SystemdServiceConfig{},
			nil,
		)
		if cfg.Deployment.Update.Enabled {
			runtimePaths := deployment.RuntimeBinaryPaths(cfg.Deployment.StateDir)
			systemdUpdater = deployment.NewSystemdBinaryUpdater(deployment.SystemdBinaryConfig{
				InstallDir:  runtimePaths.RuntimeDir,
				BinaryName:  filepath.Base(runtimePaths.RuntimeBinary),
				BackupName:  filepath.Base(runtimePaths.BackupBinary),
				DownloadDir: filepath.Join(cfg.Deployment.StateDir, ".downloads"),
				HTTPClient:  deploymentHTTPClient,
			})
		}
	}
	deploymentService := deployment.NewService(cfg.Deployment, versionInfo, releaseSource, updaterClient, systemdUpdater, systemdManager)
	deploymentHandler := handler.NewDeploymentHandler(
		healthService,
		deploymentService,
	)

	r := handler.SetupRouter(
		entClient,
		authService,
		repoService,
		analysisService,
		webhookHandler,
		syncService,
		settingsHandler,
		chatHandler,
		aggregator,
		optimizer,
		cfg.Encryption.Key,
		middleware.CORS(nil),
		oauthHandler,
		providerHandler,
		adminSettingsHandler,
		sessionBootstrapSvc,
		checkpointHandler,
		deploymentHandler,
	)

	// Start server
	addr := fmt.Sprintf(":%d", cfg.Server.Port)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		logger.Info("starting server", zap.String("addr", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server error", zap.Error(err))
		}
	}()

	var sweepCancel context.CancelFunc
	if sessionBootstrapSvc != nil {
		var sweepCtx context.Context
		sweepCtx, sweepCancel = context.WithCancel(context.Background())
		go func() {
			ticker := time.NewTicker(sessionStaleSweepInterval)
			defer ticker.Stop()
			for {
				select {
				case <-sweepCtx.Done():
					return
				case <-ticker.C:
					cutoff := time.Now().Add(-sessionStaleAbandonAfter)
					count, err := sessionBootstrapSvc.ExpireStaleSessions(sweepCtx, cutoff)
					if err != nil {
						logger.Warn("expire stale sessions failed", zap.Error(err))
						continue
					}
					if count > 0 {
						logger.Info("expired stale sessions", zap.Int("count", count), zap.Duration("older_than", sessionStaleAbandonAfter))
					}
				}
			}
		}()
	}

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server...")
	if sweepCancel != nil {
		sweepCancel()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("server shutdown", zap.Error(err))
	}
	logger.Info("server stopped")
}
