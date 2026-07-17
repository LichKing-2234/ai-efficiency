package handler

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/oauth"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relayruntime"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/teamusage"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func setupRouterForTest(
	t *testing.T,
	entClient *ent.Client,
	sqlDB *sql.DB,
	authService *auth.Service,
	repoService *repo.Service,
	webhookHandler *webhook.Handler,
	syncService prSyncer,
	settingsHandler *SettingsHandler,
	encryptionKey string,
	publicURL string,
	corsMiddleware gin.HandlerFunc,
	oauthHandler *oauth.Handler,
	providerHandler *ProviderHandler,
	adminSettingsHandler *AdminSettingsHandler,
	checkpointHandler *CheckpointHandler,
	healthHandler *HealthHandler,
	runtimeOptions ...RouterOptions,
) *gin.Engine {
	t.Helper()
	var options RouterOptions
	if len(runtimeOptions) > 0 {
		options = runtimeOptions[0]
	}
	if options.TeamUsageSnapshotCache == nil {
		redisServer := miniredis.RunT(t)
		redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
		t.Cleanup(func() {
			if err := redisClient.Close(); err != nil {
				t.Errorf("close test Redis client: %v", err)
			}
		})
		var err error
		options.TeamUsageSnapshotCache, err = teamusage.NewSnapshotCache(
			readcache.NewRedisStore(redisClient),
			teamusage.SnapshotCacheOptions{Namespace: "handler-tests"},
		)
		if err != nil {
			t.Fatalf("initialize test Team Usage snapshot cache: %v", err)
		}
	}
	if strings.TrimSpace(options.TeamUsageCursorSecret) == "" {
		options.TeamUsageCursorSecret = "test-team-usage-cursor-secret"
	}
	router, err := setupRouter(
		entClient,
		sqlDB,
		authService,
		repoService,
		webhookHandler,
		syncService,
		settingsHandler,
		encryptionKey,
		publicURL,
		corsMiddleware,
		oauthHandler,
		providerHandler,
		adminSettingsHandler,
		checkpointHandler,
		healthHandler,
		options,
	)
	if err != nil {
		t.Fatalf("setup test router: %v", err)
	}
	return router
}

func newProviderHandlerForTest(t *testing.T, entClient *ent.Client, encryptionKey string, logger *zap.Logger, runtimes ...*relayruntime.Manager) *ProviderHandler {
	t.Helper()
	var runtime *relayruntime.Manager
	if len(runtimes) > 0 {
		runtime = runtimes[0]
	}
	if runtime == nil {
		var err error
		runtime, err = relayruntime.NewManager(entClient, encryptionKey, logger, relayruntime.Options{})
		if err != nil {
			t.Fatalf("initialize test relay runtime: %v", err)
		}
	}
	handler, err := NewProviderHandler(entClient, encryptionKey, logger, runtime)
	if err != nil {
		t.Fatalf("initialize test provider handler: %v", err)
	}
	return handler
}
