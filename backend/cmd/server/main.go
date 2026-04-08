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
	"github.com/ai-efficiency/backend/internal/analysis"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/attribution"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/checkpoint"
	"github.com/ai-efficiency/backend/internal/config"
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
	_ "github.com/mattn/go-sqlite3"
	redis "github.com/redis/go-redis/v9"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"go.uber.org/zap"
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
	configPath := os.Getenv("AE_CONFIG_PATH")
	cfg, err := config.Load(configPath)
	if err != nil {
		logger.Fatal("load config", zap.Error(err))
	}
	versionInfo := deployment.CurrentVersion()
	logger.Info(
		"build metadata",
		zap.String("version", versionInfo.Version),
		zap.String("commit", versionInfo.Commit),
		zap.String("build_time", versionInfo.BuildTime),
	)

	// Set gin mode
	if cfg.Server.Mode == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Connect to ai_efficiency database
	var entClient *ent.Client
	var sqlDB *sql.DB
	useSQLite := cfg.DB.DSN == "" || strings.HasPrefix(cfg.DB.DSN, "sqlite3://") || strings.HasPrefix(cfg.DB.DSN, "file:")

	if useSQLite {
		// SQLite dev mode
		sqliteDSN := "file:ai_efficiency.db?_fk=1"
		if strings.HasPrefix(cfg.DB.DSN, "sqlite3://") {
			sqliteDSN = strings.TrimPrefix(cfg.DB.DSN, "sqlite3://")
		} else if strings.HasPrefix(cfg.DB.DSN, "file:") {
			sqliteDSN = cfg.DB.DSN
		}
		db, err := sql.Open("sqlite3", sqliteDSN)
		if err != nil {
			logger.Fatal("open sqlite db", zap.Error(err))
		}
		sqlDB = db
		defer db.Close()
		drv := entsql.OpenDB(dialect.SQLite, db)
		entClient = ent.NewClient(ent.Driver(drv))
		logger.Info("using SQLite dev mode", zap.String("dsn", sqliteDSN))
	} else {
		// PostgreSQL production mode
		db, err := sql.Open("postgres", cfg.DB.DSN)
		if err != nil {
			logger.Fatal("connect ai_efficiency db", zap.Error(err))
		}
		sqlDB = db
		defer db.Close()
		db.SetMaxOpenConns(cfg.DB.MaxOpenConns)
		db.SetMaxIdleConns(cfg.DB.MaxIdleConns)
		db.SetConnMaxLifetime(time.Duration(cfg.DB.ConnMaxLifetime) * time.Second)

		if err := db.Ping(); err != nil {
			logger.Fatal("ping ai_efficiency db", zap.Error(err))
		}
		drv := entsql.OpenDB(dialect.Postgres, db)
		entClient = ent.NewClient(ent.Driver(drv))
		logger.Info("connected to ai_efficiency database (PostgreSQL)")
	}
	defer entClient.Close()

	// Auto-migrate
	if err := entClient.Schema.Create(context.Background()); err != nil {
		logger.Fatal("ent auto-migrate", zap.Error(err))
	}
	logger.Info("database schema migrated")

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
	analysisService := analysis.NewService(entClient, analysisCloner, llmAnalyzer, logger)

	// Init PR labeler (with optional relay usage stats lookup)
	labeler := efficiency.NewLabeler(entClient, relayProvider, logger)
	aggregator := efficiency.NewAggregator(entClient, logger)

	// Init webhook handler (with labeler for auto-labeling on PR events)
	webhookHandler := webhook.NewHandler(entClient, labeler, logger)
	syncService := prsync.NewService(entClient, labeler, aggregator, logger)

	// Init optimizer
	optimizer := analysis.NewOptimizer(llmAnalyzer, logger)

	// Setup router
	settingsConfigPath := "config.yaml"
	if cp := os.Getenv("AE_CONFIG_PATH"); cp != "" {
		settingsConfigPath = cp
	}
	var relayAdminUpdater interface{ SetAdminAPIKey(string) }
	if u, ok := relayProvider.(interface{ SetAdminAPIKey(string) }); ok {
		relayAdminUpdater = u
	}
	settingsHandler := handler.NewSettingsHandler(settingsConfigPath, cfg.Relay, llmAnalyzer, logger, relayAdminUpdater)
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
	var releaseSource deployment.ReleaseSource
	if cfg.Deployment.Update.Enabled && cfg.Deployment.Update.ReleaseAPIURL != "" {
		releaseSource = deployment.NewGitHubReleaseSource(http.DefaultClient, cfg.Deployment.Update.ReleaseAPIURL)
	}
	var updaterClient deployment.Updater
	if cfg.Deployment.Update.Enabled && cfg.Deployment.Update.UpdaterURL != "" {
		updaterClient = deployment.NewUpdaterClient(http.DefaultClient, cfg.Deployment.Update.UpdaterURL)
	}
	deploymentService := deployment.NewService(cfg.Deployment, versionInfo, releaseSource, updaterClient)
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

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal("server shutdown", zap.Error(err))
	}
	logger.Info("server stopped")
}
