package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/ai-efficiency/backend/internal/webhook"
	"github.com/ai-efficiency/backend/internal/workitems"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func TestSetupRouterInjectsWorkItemsCacheAndSharedDirectoryService(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	logger := zap.NewNop()
	authService := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoService := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)

	revisions := workitems.NewRevisionStore(client)
	if err := revisions.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() revision error = %v", err)
	}
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	cache, err := workitems.NewCountsCache(workitems.NewRedisCountsStore(redisClient), revisions, workitems.CountsCacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCountsCache() error = %v", err)
	}
	directoryService := &fakeDirectoryService{}
	router := SetupRouter(
		client,
		nil,
		authService,
		repoService,
		webhookHandler,
		nil,
		nil,
		"0000000000000000000000000000000000000000000000000000000000000000",
		"",
		middleware.CORS(nil),
		nil,
		nil,
		nil,
		nil,
		nil,
		RouterRuntimeOptions{DirectoryService: directoryService, WorkItemsCache: cache},
	)

	admin := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource("ldap").
		SetRole("admin").
		SaveX(ctx)
	pair, err := authService.GenerateTokenPairForUser(&auth.UserInfo{ID: admin.ID, Username: admin.Username, Role: string(admin.Role)})
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/work-items/counts", nil)
		req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, body = %s", i+1, recorder.Code, recorder.Body.String())
		}
	}
	if directoryService.countCall != 1 {
		t.Fatalf("directory CountOffboardingCandidates calls = %d, want 1", directoryService.countCall)
	}
}
