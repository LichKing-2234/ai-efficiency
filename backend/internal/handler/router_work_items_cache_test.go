package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent/quotaresetrequest"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/directorysync"
	"github.com/ai-efficiency/backend/internal/middleware"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/representativescope"
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
	router := setupRouterForTest(t,
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
		RouterOptions{DirectoryService: directoryService, WorkItemsCache: cache},
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

func TestSetupRouterInjectsRepresentativeScopeCache(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	logger := zap.NewNop()
	authService := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoService := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	scopeCache, err := representativescope.NewCache(
		readcache.NewRedisStore(redisClient),
		representativescope.CacheOptions{Namespace: "test"},
	)
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}

	actor := client.User.Create().
		SetUsername("representative").
		SetEmail("representative@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SaveX(ctx)
	completedAt := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	source := client.DirectorySource.Create().
		SetName("Example Directory").
		SetDescription("Synthetic organization directory").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetCompletedAt(completedAt).
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(source.ID).
		SetLastRunID(run.ID).
		SetLastSuccessfulRunID(run.ID).
		ExecX(ctx)
	client.DirectoryDepartment.Create().
		SetSourceID(source.ID).
		SetExternalID("department-alpha").
		SetName("Department Alpha").
		SetLastSeenRunID(run.ID).
		SetMetadata(map[string]any{"representative_external_ids": []string{"member-representative"}}).
		SaveX(ctx)
	client.DirectoryMember.Create().
		SetSourceID(source.ID).
		SetExternalID("member-representative").
		SetEmailNormalized(actor.Email).
		SetDisplayName("Representative").
		SetDepartmentExternalID("department-alpha").
		SetMatchedUserID(actor.ID).
		SetLastSeenRunID(run.ID).
		SaveX(ctx)

	router := setupRouterForTest(t,
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
		RouterOptions{RepresentativeScopeCache: scopeCache},
	)
	token := workItemsTestAccessToken(t, authService, actor.ID, actor.Username, "user")
	for requestIndex := 0; requestIndex < 2; requestIndex++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/user/team-usage/scope", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), `"is_representative":true`) {
			t.Fatalf("request %d response = %d %s", requestIndex+1, recorder.Code, recorder.Body.String())
		}
	}
	valueKeys := 0
	for _, key := range redisServer.Keys() {
		if strings.Contains(key, ":representative-scope:") && !strings.HasSuffix(key, ":lease") {
			valueKeys++
		}
	}
	if valueKeys != 1 {
		t.Fatalf("representative scope value keys = %d, want 1", valueKeys)
	}
}

func TestSetupRouterQuotaMutationInvalidatesWarmWorkItemCounts(t *testing.T) {
	client := testdb.Open(t)
	ctx := context.Background()
	logger := zap.NewNop()
	authService := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
	repoService := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
	webhookHandler := webhook.NewHandler(client, nil, logger)
	providerHandler := newProviderHandlerForTest(t, client, "0000000000000000000000000000000000000000000000000000000000000000", logger)

	revisions := workitems.NewRevisionStore(client)
	if err := revisions.Ensure(ctx); err != nil {
		t.Fatalf("Ensure() revision error = %v", err)
	}
	oldRevision, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() old revision error = %v", err)
	}
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	cache, err := workitems.NewCountsCache(workitems.NewRedisCountsStore(redisClient), revisions, workitems.CountsCacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCountsCache() error = %v", err)
	}
	directoryService := &fakeDirectoryService{}
	router := setupRouterForTest(t,
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
		providerHandler,
		nil,
		nil,
		nil,
		RouterOptions{
			DirectoryService:       directoryService,
			WorkItemsCache:         cache,
			WorkItemsRevisionStore: revisions,
		},
	)

	requester := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SaveX(ctx)
	approver := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SetRole("user").
		SaveX(ctx)
	admin := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource("ldap").
		SetRole("admin").
		SaveX(ctx)
	provider := client.RelayProvider.Create().
		SetName("disabled-primary-relay").
		SetDisplayName("Disabled Primary Relay").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("synthetic-encrypted-key").
		SetRelayType("sub2api").
		SetIsPrimary(true).
		SetEnabled(false).
		SaveX(ctx)
	request := client.QuotaResetRequest.Create().
		SetRequesterUserID(requester.ID).
		SetRequesterRelayUserID(1001).
		SetProviderID(provider.ID).
		SetGroupID("42").
		SetGroupName("Group Alpha").
		SetGroupPlatform("openai").
		SetReason("Synthetic quota reset request").
		SetResolvedApproverUserIds([]int{approver.ID}).
		SetMatchedDepartmentPaths([]map[string]any{}).
		SaveX(ctx)

	requesterToken := workItemsTestAccessToken(t, authService, requester.ID, requester.Username, "user")
	approverToken := workItemsTestAccessToken(t, authService, approver.ID, approver.Username, "user")
	adminToken := workItemsTestAccessToken(t, authService, admin.ID, admin.Username, "admin")
	if got := workItemsCountsThroughRouter(t, router, approverToken); got.QuotaResetApprovalCount != 1 {
		t.Fatalf("warm approver quota count = %d, want 1", got.QuotaResetApprovalCount)
	}
	if got := workItemsCountsThroughRouter(t, router, adminToken); got.QuotaResetAdminCount != 1 {
		t.Fatalf("warm admin quota count = %d, want 1", got.QuotaResetAdminCount)
	}
	oldKeys := append([]string(nil), server.Keys()...)
	if len(oldKeys) != 2 {
		t.Fatalf("warm Redis keys = %#v, want approver and admin entries", oldKeys)
	}
	for _, key := range oldKeys {
		if !strings.Contains(key, ":rev:"+oldRevision+":") {
			t.Fatalf("warm Redis key = %q, want old revision %s", key, oldRevision)
		}
	}

	cancelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/user/quota-reset/requests/"+strconv.Itoa(request.ID)+"/cancel", nil)
	cancelRequest.Header.Set("Authorization", "Bearer "+requesterToken)
	cancelRecorder := httptest.NewRecorder()
	router.ServeHTTP(cancelRecorder, cancelRequest)
	if cancelRecorder.Code != http.StatusOK {
		t.Fatalf("cancel status = %d, body = %s", cancelRecorder.Code, cancelRecorder.Body.String())
	}
	if status := client.QuotaResetRequest.GetX(ctx, request.ID).Status; status != quotaresetrequest.StatusCancelled {
		t.Fatalf("request status = %s, want cancelled", status)
	}
	newRevision, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() new revision error = %v", err)
	}
	if newRevision == oldRevision {
		t.Fatalf("revision after cancel = %q, want different from %q", newRevision, oldRevision)
	}
	if got := workItemsCountsThroughRouter(t, router, approverToken); got.QuotaResetApprovalCount != 0 {
		t.Fatalf("post-cancel approver quota count = %d, want 0", got.QuotaResetApprovalCount)
	}
	if got := workItemsCountsThroughRouter(t, router, adminToken); got.QuotaResetAdminCount != 0 {
		t.Fatalf("post-cancel admin quota count = %d, want 0", got.QuotaResetAdminCount)
	}
	for _, key := range oldKeys {
		if !server.Exists(key) {
			t.Fatalf("old Redis key %q was deleted; want revision isolation to make it unreachable", key)
		}
	}
	newKeyCount := 0
	for _, key := range server.Keys() {
		if strings.Contains(key, ":rev:"+newRevision+":") {
			newKeyCount++
		}
	}
	if newKeyCount != 2 {
		t.Fatalf("new revision Redis key count = %d, keys = %#v, want 2", newKeyCount, server.Keys())
	}
}

func TestSetupRouterDirectoryMutationsInvalidateWarmCountsAcrossRedisOutage(t *testing.T) {
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
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	cache, err := workitems.NewCountsCache(workitems.NewRedisCountsStore(redisClient), revisions, workitems.CountsCacheOptions{Namespace: "test"})
	if err != nil {
		t.Fatalf("NewCountsCache() error = %v", err)
	}

	directoryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Header.Get("X-Directory-API-Key") != "test-directory-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch request.URL.Path {
		case "/departments":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"departments": []map[string]any{
				{"id": "dept-alpha", "name": "Department Alpha", "path": "Department Alpha"},
			}}})
		case "/users":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"users": []map[string]any{
				{"id": "member-alice", "email": "alice@example.com", "name": "Alice", "status": "active"},
			}}})
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(directoryServer.Close)
	disabler := &workItemsRelayDisabler{}
	directoryService := directorysync.NewService(client, directorysync.ServiceOptions{
		Executor:                  directorysync.NewExecutor(directorysync.ExecutorOptions{AllowHTTP: true}),
		Credentials:               workItemsDirectoryCredentials{"directory_api_key": "test-directory-secret"},
		RelayDisablers:            workItemsRelayDisablerResolver{disabler: disabler},
		TokenRevoker:              authService,
		WorkItemCountsInvalidator: revisions,
		Now:                       func() time.Time { return time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC) },
	})
	router := setupRouterForTest(t,
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
		RouterOptions{
			DirectoryService:       directoryService,
			WorkItemsCache:         cache,
			WorkItemsRevisionStore: revisions,
		},
	)

	admin := client.User.Create().
		SetUsername("admin").
		SetEmail("admin@example.com").
		SetAuthSource("ldap").
		SetRole("admin").
		SaveX(ctx)
	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("relay_sso").
		SetRole("user").
		SetRelayUserID(2002).
		SaveX(ctx)
	client.RelayProvider.Create().
		SetName("primary-relay").
		SetDisplayName("Primary Relay").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("synthetic-encrypted-key").
		SetRelayType("sub2api").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	dsl := workItemsDirectoryDSL(directoryServer.URL)
	source, err := directoryService.CreateSource(ctx, directorysync.SourceInput{
		Name:             "Synthetic Directory",
		Description:      "Synthetic full company directory",
		Scope:            "full_company",
		Enabled:          true,
		DSL:              dsl,
		ScheduleEnabled:  false,
		ScheduleInterval: "daily",
		ScheduleTimezone: "UTC",
	})
	if err != nil {
		t.Fatalf("CreateSource() error = %v", err)
	}
	adminToken := workItemsTestAccessToken(t, authService, admin.ID, admin.Username, "admin")
	initialRevision, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() initial revision error = %v", err)
	}
	if got := workItemsCountsThroughRouter(t, router, adminToken); got.OffboardingCount != 0 {
		t.Fatalf("warm offboarding count = %d, want 0 before first apply", got.OffboardingCount)
	}
	initialKey := workItemsRedisKeyForRevision(t, redisServer.Keys(), initialRevision)

	run, err := directoryService.RunSource(ctx, source.ID, "apply", "manual")
	if err != nil {
		t.Fatalf("RunSource(apply) error = %v", err)
	}
	run, err = directoryService.ExecuteRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ExecuteRun(apply) error = %v", err)
	}
	if run.Status.String() != "completed" {
		t.Fatalf("apply status = %s, want completed", run.Status)
	}
	applyRevision, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() apply revision error = %v", err)
	}
	if applyRevision == initialRevision {
		t.Fatalf("revision after apply = %q, want different from %q", applyRevision, initialRevision)
	}
	if got := workItemsCountsThroughRouter(t, router, adminToken); got.OffboardingCount != 1 {
		t.Fatalf("post-apply offboarding count = %d, want bob as candidate", got.OffboardingCount)
	}
	if !redisServer.Exists(initialKey) {
		t.Fatalf("initial cache key %q missing; want old revision retained but unreachable", initialKey)
	}
	applyKey := workItemsRedisKeyForRevision(t, redisServer.Keys(), applyRevision)

	redisServer.Close()
	updatedSource, err := directoryService.UpdateSource(ctx, source.ID, directorysync.SourceInput{
		Name:             "Synthetic Directory Updated",
		Description:      source.Description,
		Scope:            source.Scope.String(),
		Enabled:          source.Enabled,
		DSL:              source.Dsl,
		ScheduleEnabled:  source.ScheduleEnabled,
		ScheduleInterval: source.ScheduleInterval.String(),
		ScheduleTimezone: source.ScheduleTimezone,
	})
	if err != nil {
		t.Fatalf("UpdateSource() error = %v", err)
	}
	if updatedSource.Name != "Synthetic Directory Updated" {
		t.Fatalf("updated source name = %q", updatedSource.Name)
	}
	preOffboardingRevision, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() source-update revision error = %v", err)
	}
	if preOffboardingRevision == applyRevision {
		t.Fatalf("revision after source update = %q, want different from apply revision", preOffboardingRevision)
	}
	if got := workItemsCountsThroughRouter(t, router, adminToken); got.OffboardingCount != 1 {
		t.Fatalf("post-source-update offboarding count with Redis down = %d, want 1", got.OffboardingCount)
	}
	if err := redisServer.Restart(); err != nil {
		t.Fatalf("restart miniredis after source update: %v", err)
	}
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping restarted miniredis after source update: %v", err)
	}
	if got := workItemsCountsThroughRouter(t, router, adminToken); got.OffboardingCount != 1 {
		t.Fatalf("post-source-update offboarding count after Redis restart = %d, want 1", got.OffboardingCount)
	}
	if !redisServer.Exists(applyKey) {
		t.Fatalf("old apply cache key %q missing; test requires preserved stale value", applyKey)
	}
	preOffboardingKey := workItemsRedisKeyForRevision(t, redisServer.Keys(), preOffboardingRevision)

	redisServer.Close()
	action, err := directoryService.DisableRelayUserForCandidate(ctx, directorysync.DisableCandidateRequest{
		SourceID:          source.ID,
		UserID:            bob.ID,
		ConfirmEmail:      "bob@example.org",
		Reason:            "missing_from_latest_full_company_directory",
		PerformedByUserID: admin.ID,
	})
	if err != nil {
		t.Fatalf("DisableRelayUserForCandidate() error = %v", err)
	}
	if action.Status.String() != "succeeded" {
		t.Fatalf("offboarding action status = %s, want succeeded", action.Status)
	}
	if len(disabler.userIDs) != 1 || disabler.userIDs[0] != 2002 {
		t.Fatalf("disabled Relay user IDs = %v, want [2002]", disabler.userIDs)
	}
	if tokenFloor := client.User.GetX(ctx, bob.ID).TokenValidAfter; tokenFloor == nil {
		t.Fatal("token_valid_after is nil after successful offboarding")
	}
	postOffboardingRevision, err := revisions.Current(ctx)
	if err != nil {
		t.Fatalf("Current() post-offboarding revision error = %v", err)
	}
	if postOffboardingRevision == preOffboardingRevision {
		t.Fatalf("revision after offboarding = %q, want different from %q", postOffboardingRevision, preOffboardingRevision)
	}
	if got := workItemsCountsThroughRouter(t, router, adminToken); got.OffboardingCount != 0 {
		t.Fatalf("offboarding count with Redis down = %d, want authoritative 0", got.OffboardingCount)
	}
	if err := redisServer.Restart(); err != nil {
		t.Fatalf("restart miniredis: %v", err)
	}
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping restarted miniredis: %v", err)
	}
	if got := workItemsCountsThroughRouter(t, router, adminToken); got.OffboardingCount != 0 {
		t.Fatalf("offboarding count after Redis restart = %d, want 0", got.OffboardingCount)
	}
	if !redisServer.Exists(preOffboardingKey) {
		t.Fatalf("old cached value key %q missing; test requires preserved stale value", preOffboardingKey)
	}
	_ = workItemsRedisKeyForRevision(t, redisServer.Keys(), postOffboardingRevision)
}

func workItemsTestAccessToken(t *testing.T, service *auth.Service, userID int, username, role string) string {
	t.Helper()
	pair, err := service.GenerateTokenPairForUser(&auth.UserInfo{ID: userID, Username: username, Role: role})
	if err != nil {
		t.Fatalf("generate %s token: %v", username, err)
	}
	return pair.AccessToken
}

func workItemsCountsThroughRouter(t *testing.T, router http.Handler, token string) workitems.CountsResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/work-items/counts", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusOK {
		t.Fatalf("counts status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Data workitems.CountsResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode counts response: %v", err)
	}
	return response.Data
}

type workItemsDirectoryCredentials map[string]string

func (credentials workItemsDirectoryCredentials) ResolveCredential(_ context.Context, ref string) (string, bool, error) {
	value, ok := credentials[ref]
	return value, ok, nil
}

type workItemsRelayDisablerResolver struct {
	disabler relay.UserDisabler
}

func (resolver workItemsRelayDisablerResolver) ResolveRelayDisabler(context.Context, int) (relay.UserDisabler, error) {
	return resolver.disabler, nil
}

type workItemsRelayDisabler struct {
	userIDs []int64
}

func (disabler *workItemsRelayDisabler) DisableUser(_ context.Context, userID int64) error {
	disabler.userIDs = append(disabler.userIDs, userID)
	return nil
}

func workItemsRedisKeyForRevision(t *testing.T, keys []string, revision string) string {
	t.Helper()
	for _, key := range keys {
		if strings.Contains(key, ":rev:"+revision+":") && !strings.HasSuffix(key, ":lease") {
			return key
		}
	}
	t.Fatalf("no counts cache key for revision %s in %#v", revision, keys)
	return ""
}

func workItemsDirectoryDSL(baseURL string) string {
	return fmt.Sprintf(`
version: 1
scope: full_company
auth:
  type: header
  header: X-Directory-API-Key
  credential_ref: directory_api_key
limits:
  timeout_seconds: 5
  max_response_bytes: 1048576
  max_items: 100
steps:
  - id: departments
    request:
      method: GET
      url: %s/departments
    extract:
      items: $.data.departments
    map:
      department:
        external_id: $.id
        parent_external_id: $.parent_id
        name: $.name
        path: $.path
  - id: members
    foreach: departments.items
    request:
      method: GET
      url: %s/users
      query:
        department_id: "{{ item.external_id }}"
    extract:
      items: $.data.users
    map:
      member:
        external_id: $.id
        email: $.email
        display_name: $.name
        department_external_id: "{{ source.external_id }}"
        status: $.status
`, baseURL, baseURL)
}
