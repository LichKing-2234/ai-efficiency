package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/adminsubscriptionjob"
	"github.com/ai-efficiency/backend/internal/adminsubscription"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/gin-gonic/gin"
)

type adminUsersProviderResolverFunc func(ctx context.Context, providerID int) (relay.Provider, error)

func (f adminUsersProviderResolverFunc) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	return f(ctx, providerID)
}

type adminUsersRelayFake struct {
	relay.Provider
	groups           []relay.Group
	assignedUserID   int64
	assignedGroupID  int64
	assignedValidity int
	disabledUserID   int64
	calls            []adminUsersRelaySubscriptionCall
	assignStarted    chan struct{}
	unblockAssign    chan struct{}
	assignStartOnce  sync.Once
}

type adminUsersRelaySubscriptionCall struct {
	Operation string
	UserID    int64
	GroupID   int64
	Days      int
}

func (f *adminUsersRelayFake) ListPlatformGroups(ctx context.Context) ([]relay.Group, error) {
	return f.groups, nil
}

func (f *adminUsersRelayFake) AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error {
	if f.assignStarted != nil {
		f.assignStartOnce.Do(func() {
			close(f.assignStarted)
		})
	}
	if f.unblockAssign != nil {
		<-f.unblockAssign
	}
	f.assignedUserID = userID
	f.assignedGroupID = groupID
	f.assignedValidity = validityDays
	f.calls = append(f.calls, adminUsersRelaySubscriptionCall{
		Operation: "add",
		UserID:    userID,
		GroupID:   groupID,
		Days:      validityDays,
	})
	return nil
}

func (f *adminUsersRelayFake) ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error {
	f.calls = append(f.calls, adminUsersRelaySubscriptionCall{
		Operation: "extend",
		UserID:    userID,
		GroupID:   groupID,
		Days:      days,
	})
	return nil
}

func (f *adminUsersRelayFake) RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error {
	f.calls = append(f.calls, adminUsersRelaySubscriptionCall{
		Operation: "remove",
		UserID:    userID,
		GroupID:   groupID,
	})
	return nil
}

func (f *adminUsersRelayFake) ResetSubscriptionQuotaForUser(ctx context.Context, userID, groupID int64) error {
	f.calls = append(f.calls, adminUsersRelaySubscriptionCall{
		Operation: "reset_quota",
		UserID:    userID,
		GroupID:   groupID,
	})
	return nil
}

func (f *adminUsersRelayFake) DisableUser(ctx context.Context, userID int64) error {
	f.disabledUserID = userID
	return nil
}

func TestAdminUsersStartSubscriptionJobReturnsQueuedWithoutWaitingForRelayMutation(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups:        []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
		assignStarted: make(chan struct{}),
		unblockAssign: make(chan struct{}),
	}
	defer close(fakeRelay.unblockAssign)
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/subscription-jobs", handler.StartSubscriptionJob)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"selected","user_ids":[%d],"operation":"add","provider_id":%d,"group_id":"42","validity_days":60}`, localUser.ID, provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscription-jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	select {
	case <-fakeRelay.assignStarted:
	case <-time.After(time.Second):
		t.Fatal("background subscription mutation did not start")
	}
	if len(fakeRelay.calls) != 0 {
		t.Fatalf("calls = %+v, want no completed relay mutation before unblock", fakeRelay.calls)
	}

	var resp struct {
		Data struct {
			ID             int    `json:"id"`
			Status         string `json:"status"`
			Phase          string `json:"phase"`
			TotalCount     int    `json:"total_count"`
			ProcessedCount int    `json:"processed_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.ID <= 0 || resp.Data.Status != "queued" || resp.Data.Phase != "queued" || resp.Data.TotalCount != 1 || resp.Data.ProcessedCount != 0 {
		t.Fatalf("unexpected job response: %+v", resp.Data)
	}
}

func TestAdminUsersStartSubscriptionJobUsesCurrentAccessStatusFilter(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	defer client.Close()
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	disabledUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SetTokenValidAfter(time.Date(2026, 6, 26, 9, 0, 0, 0, time.UTC)).
		SaveX(ctx)
	client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(99).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/subscription-jobs", handler.StartSubscriptionJob)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"current_filter","filters":{"access_status":"disabled"},"operation":"add","provider_id":%d,"group_id":"42","validity_days":60}`, provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscription-jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			TotalCount    int   `json:"total_count"`
			TargetUserIDs []int `json:"target_user_ids"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.TotalCount != 1 || len(resp.Data.TargetUserIDs) != 1 || resp.Data.TargetUserIDs[0] != disabledUser.ID {
		t.Fatalf("job targets = %+v, want only disabled user %d", resp.Data, disabledUser.ID)
	}
}

func TestAdminUsersStartSubscriptionJobSupportsSelectedQuotaReset(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	defer client.Close()
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/subscription-jobs", handler.StartSubscriptionJob)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"selected","user_ids":[%d],"operation":"reset_quota","provider_id":%d,"group_id":"42"}`, localUser.ID, provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscription-jobs", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Status        string `json:"status"`
			Operation     string `json:"operation"`
			TotalCount    int    `json:"total_count"`
			TargetUserIDs []int  `json:"target_user_ids"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.Status != "queued" || resp.Data.Operation != "reset_quota" || resp.Data.TotalCount != 1 || len(resp.Data.TargetUserIDs) != 1 || resp.Data.TargetUserIDs[0] != localUser.ID {
		t.Fatalf("unexpected job response: %+v", resp.Data)
	}
}

func TestAdminSubscriptionJobErrorStatusClassifiesValidationAndInternalErrors(t *testing.T) {
	if got := adminSubscriptionJobErrorStatus(adminsubscription.NewValidationError("scope is required")); got != http.StatusBadRequest {
		t.Fatalf("validation status = %d, want 400", got)
	}
	if got := adminSubscriptionJobErrorStatus(adminsubscription.NewTooManyTargetsError(500)); got != http.StatusUnprocessableEntity {
		t.Fatalf("too many targets status = %d, want 422", got)
	}
	if got := adminSubscriptionJobErrorStatus(errors.New("list users: database unavailable")); got != http.StatusInternalServerError {
		t.Fatalf("internal status = %d, want 500", got)
	}
}

func TestAdminUsersGetSubscriptionJobReturnsProgressAndResults(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	svc := adminsubscription.NewService(client)
	job, err := svc.StartJob(ctx, adminsubscription.StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{localUser.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 60,
	})
	if err != nil {
		t.Fatalf("StartJob error: %v", err)
	}
	if err := svc.RunJob(ctx, job.ID, &adminUsersRelayFake{}); err != nil {
		t.Fatalf("RunJob error: %v", err)
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey)

	router := gin.New()
	router.GET("/admin/users/subscription-jobs/:id", handler.GetSubscriptionJob)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/users/subscription-jobs/%d", job.ID), nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			ID             int `json:"id"`
			ProcessedCount int `json:"processed_count"`
			SuccessCount   int `json:"success_count"`
			Results        []struct {
				UserID int    `json:"user_id"`
				Status string `json:"status"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.ID != job.ID || resp.Data.ProcessedCount != 1 || resp.Data.SuccessCount != 1 {
		t.Fatalf("unexpected job counters: %+v", resp.Data)
	}
	if len(resp.Data.Results) != 1 || resp.Data.Results[0].UserID != localUser.ID || resp.Data.Results[0].Status != "success" {
		t.Fatalf("unexpected results: %+v", resp.Data.Results)
	}
}

func TestAdminUsersGetLatestSubscriptionJobReturnsNewestJob(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	alice := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(99).
		SaveX(ctx)
	svc := adminsubscription.NewService(client)
	if _, err := svc.StartJob(ctx, adminsubscription.StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{alice.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 60,
	}); err != nil {
		t.Fatalf("StartJob alice error: %v", err)
	}
	time.Sleep(time.Millisecond)
	latest, err := svc.StartJob(ctx, adminsubscription.StartJobRequest{
		Scope:        "selected",
		UserIDs:      []int{bob.ID},
		Operation:    "add",
		ProviderID:   7,
		GroupID:      "42",
		ValidityDays: 60,
	})
	if err != nil {
		t.Fatalf("StartJob bob error: %v", err)
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey)

	router := gin.New()
	router.GET("/admin/users/subscription-jobs/latest", handler.GetLatestSubscriptionJob)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users/subscription-jobs/latest", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			ID        int   `json:"id"`
			TargetIDs []int `json:"target_user_ids"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.ID != latest.ID || len(resp.Data.TargetIDs) != 1 || resp.Data.TargetIDs[0] != bob.ID {
		t.Fatalf("latest job = %+v, want id %d target %d", resp.Data, latest.ID, bob.ID)
	}
}

func TestAdminUsersListSubscriptionOptions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	client.RelayProvider.Create().
		SetName("disabled").
		SetDisplayName("Disabled").
		SetBaseURL("https://disabled.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetEnabled(false).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{
			{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"},
			{ID: 43, Name: "Group Beta", Platform: "anthropic", SubscriptionType: "standard"},
		},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			t.Fatalf("providerID = %d, want %d", providerID, provider.ID)
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.GET("/admin/users/subscription-options", handler.ListSubscriptionOptions)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users/subscription-options", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			Providers []struct {
				ID     int `json:"id"`
				Groups []struct {
					GroupID          string `json:"group_id"`
					GroupName        string `json:"group_name"`
					Platform         string `json:"platform"`
					SubscriptionType string `json:"subscription_type"`
				} `json:"groups"`
			} `json:"providers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, w.Body.String())
	}
	if len(resp.Data.Providers) != 1 {
		t.Fatalf("providers = %d, want 1", len(resp.Data.Providers))
	}
	if resp.Data.Providers[0].ID != provider.ID {
		t.Fatalf("provider id = %d, want %d", resp.Data.Providers[0].ID, provider.ID)
	}
	if len(resp.Data.Providers[0].Groups) != 1 {
		t.Fatalf("groups = %d, want 1 subscription group", len(resp.Data.Providers[0].Groups))
	}
	if got := resp.Data.Providers[0].Groups[0]; got.GroupID != "42" || got.GroupName != "Group Alpha" || got.Platform != "openai" || got.SubscriptionType != "subscription" {
		t.Fatalf("unexpected group: %+v", got)
	}
}

func TestAdminUsersAssignSubscription(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/:id/subscriptions", handler.AssignSubscription)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"provider_id":%d,"group_id":"42","validity_days":60}`, provider.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/subscriptions", localUser.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if fakeRelay.assignedUserID != 42 || fakeRelay.assignedGroupID != 42 || fakeRelay.assignedValidity != 60 {
		t.Fatalf("assignment = user %d group %d days %d, want user 42 group 42 days 60", fakeRelay.assignedUserID, fakeRelay.assignedGroupID, fakeRelay.assignedValidity)
	}
}

func TestAdminUsersBatchManageSubscriptionsSelectedUsersSkipsUnmapped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	alice := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	bob := client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SetRole("user").
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/subscriptions/batch", handler.ManageSubscriptions)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"selected","user_ids":[%d,%d],"operation":"add","provider_id":%d,"group_id":"42","validity_days":60}`, alice.ID, bob.ID, provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscriptions/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if len(fakeRelay.calls) != 1 {
		t.Fatalf("calls = %+v, want one relay add", fakeRelay.calls)
	}
	if got := fakeRelay.calls[0]; got.Operation != "add" || got.UserID != 42 || got.GroupID != 42 || got.Days != 60 {
		t.Fatalf("unexpected call: %+v", got)
	}

	var resp struct {
		Data struct {
			TotalCount   int `json:"total_count"`
			SuccessCount int `json:"success_count"`
			SkippedCount int `json:"skipped_count"`
			FailedCount  int `json:"failed_count"`
			Results      []struct {
				UserID int    `json:"user_id"`
				Status string `json:"status"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.TotalCount != 2 || resp.Data.SuccessCount != 1 || resp.Data.SkippedCount != 1 || resp.Data.FailedCount != 0 {
		t.Fatalf("unexpected counts: %+v", resp.Data)
	}
	if len(resp.Data.Results) != 2 || resp.Data.Results[0].UserID != alice.ID || resp.Data.Results[0].Status != "success" || resp.Data.Results[1].UserID != bob.ID || resp.Data.Results[1].Status != "skipped" {
		t.Fatalf("unexpected results: %+v", resp.Data.Results)
	}
}

func TestAdminUsersBatchManageSubscriptionsSelectedUsersReportsMissingIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	alice := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/subscriptions/batch", handler.ManageSubscriptions)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"selected","user_ids":[%d,999],"operation":"add","provider_id":%d,"group_id":"42","validity_days":60}`, alice.ID, provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscriptions/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Data struct {
			TotalCount   int `json:"total_count"`
			SuccessCount int `json:"success_count"`
			SkippedCount int `json:"skipped_count"`
			FailedCount  int `json:"failed_count"`
			Results      []struct {
				UserID  int    `json:"user_id"`
				Status  string `json:"status"`
				Message string `json:"message"`
			} `json:"results"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v, body=%s", err, w.Body.String())
	}
	if resp.Data.TotalCount != 2 || resp.Data.SuccessCount != 1 || resp.Data.SkippedCount != 0 || resp.Data.FailedCount != 1 {
		t.Fatalf("unexpected counts: %+v", resp.Data)
	}
	if len(resp.Data.Results) != 2 || resp.Data.Results[0].UserID != alice.ID || resp.Data.Results[0].Status != "success" || resp.Data.Results[1].UserID != 999 || resp.Data.Results[1].Status != "failed" {
		t.Fatalf("unexpected results: %+v", resp.Data.Results)
	}
	if !strings.Contains(resp.Data.Results[1].Message, "user not found") {
		t.Fatalf("missing user message = %q, want user not found", resp.Data.Results[1].Message)
	}
}

func TestAdminUsersBatchManageSubscriptionsRejectsOversizedSelectedBatch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	userIDs := make([]string, 0, adminSubscriptionBatchMaxUsers+1)
	for i := 1; i <= adminSubscriptionBatchMaxUsers+1; i++ {
		userIDs = append(userIDs, strconv.Itoa(i))
	}

	router := gin.New()
	router.POST("/admin/users/subscriptions/batch", handler.ManageSubscriptions)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"selected","user_ids":[%s],"operation":"add","provider_id":%d,"group_id":"42","validity_days":60}`, strings.Join(userIDs, ","), provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscriptions/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", w.Code, w.Body.String())
	}
	if len(fakeRelay.calls) != 0 {
		t.Fatalf("calls = %+v, want no relay calls", fakeRelay.calls)
	}
}

func TestAdminUsersBatchManageSubscriptionsCurrentFilterExtendsMatchingMappedUsers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(99).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/subscriptions/batch", handler.ManageSubscriptions)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"current_filter","filters":{"q":"alice"},"operation":"extend","provider_id":%d,"group_id":"42","days":7}`, provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscriptions/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if len(fakeRelay.calls) != 1 {
		t.Fatalf("calls = %+v, want one relay extend", fakeRelay.calls)
	}
	if got := fakeRelay.calls[0]; got.Operation != "extend" || got.UserID != 42 || got.GroupID != 42 || got.Days != 7 {
		t.Fatalf("unexpected call: %+v", got)
	}
}

func TestAdminUsersBatchManageSubscriptionsAllMappedRemovesEveryMappedUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SetRole("user").
		SaveX(ctx)
	client.User.Create().
		SetUsername("carol").
		SetEmail("carol@example.net").
		SetAuthSource("relay_sso").
		SetRole("user").
		SetRelayUserID(99).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/subscriptions/batch", handler.ManageSubscriptions)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"all_mapped","operation":"remove","provider_id":%d,"group_id":"42"}`, provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscriptions/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if len(fakeRelay.calls) != 2 {
		t.Fatalf("calls = %+v, want two relay removes", fakeRelay.calls)
	}
	if fakeRelay.calls[0].Operation != "remove" || fakeRelay.calls[0].UserID != 42 || fakeRelay.calls[1].Operation != "remove" || fakeRelay.calls[1].UserID != 99 {
		t.Fatalf("unexpected calls: %+v", fakeRelay.calls)
	}
}

func TestAdminUsersBatchManageSubscriptionsAllMappedResetsQuotaForEveryMappedUser(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)
	client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("ldap").
		SetRole("user").
		SaveX(ctx)
	client.User.Create().
		SetUsername("carol").
		SetEmail("carol@example.net").
		SetAuthSource("relay_sso").
		SetRole("user").
		SetRelayUserID(99).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/subscriptions/batch", handler.ManageSubscriptions)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"scope":"all_mapped","operation":"reset_quota","provider_id":%d,"group_id":"42"}`, provider.ID)
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscriptions/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if len(fakeRelay.calls) != 2 {
		t.Fatalf("calls = %+v, want two relay quota resets", fakeRelay.calls)
	}
	if fakeRelay.calls[0].Operation != "reset_quota" || fakeRelay.calls[0].UserID != 42 || fakeRelay.calls[1].Operation != "reset_quota" || fakeRelay.calls[1].UserID != 99 {
		t.Fatalf("unexpected calls: %+v", fakeRelay.calls)
	}
}

func TestAdminUsersAssignSubscriptionRejectsDisabledProvider(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(false).
		SaveX(ctx)
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/:id/subscriptions", handler.AssignSubscription)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"provider_id":%d,"group_id":"42","validity_days":60}`, provider.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/subscriptions", localUser.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", w.Code, w.Body.String())
	}
	if fakeRelay.assignedUserID != 0 {
		t.Fatalf("assigned user id = %d, want no assignment", fakeRelay.assignedUserID)
	}
}

func TestAdminUsersAssignSubscriptionRejectsUnknownGroup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/:id/subscriptions", handler.AssignSubscription)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"provider_id":%d,"group_id":"99","validity_days":60}`, provider.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/subscriptions", localUser.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", w.Code, w.Body.String())
	}
	if fakeRelay.assignedUserID != 0 {
		t.Fatalf("assigned user id = %d, want no assignment", fakeRelay.assignedUserID)
	}
}

func TestAdminUsersAssignSubscriptionRejectsNonSubscriptionGroup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := testdb.Open(t)
	provider := client.RelayProvider.Create().
		SetName("sub2api").
		SetDisplayName("Sub2API").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("gpt-5.4").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(ctx)
	localUser := client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SaveX(ctx)

	fakeRelay := &adminUsersRelayFake{
		groups: []relay.Group{{ID: 43, Name: "Group Beta", Platform: "anthropic", SubscriptionType: "standard"}},
	}
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != provider.ID {
			return nil, errors.New("unexpected provider")
		}
		return fakeRelay, nil
	}))

	router := gin.New()
	router.POST("/admin/users/:id/subscriptions", handler.AssignSubscription)
	w := httptest.NewRecorder()
	body := fmt.Sprintf(`{"provider_id":%d,"group_id":"43","validity_days":60}`, provider.ID)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/subscriptions", localUser.ID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body=%s", w.Code, w.Body.String())
	}
	if fakeRelay.assignedUserID != 0 {
		t.Fatalf("assigned user id = %d, want no assignment", fakeRelay.assignedUserID)
	}
}

type adminUsersCurrentFilterMutationFixture struct {
	client               *ent.Client
	providerID           int
	users                map[string]*ent.User
	localUserIDByRelayID map[int64]int
}

func TestAdminUsersCurrentFilterEffectiveCycleParity(t *testing.T) {
	fixture := seedAdminUsersCurrentFilterMutationFixture(t)
	cases := []struct {
		name            string
		filter          adminManageSubscriptionsFilter
		wantUserKeys    []string
		excludedFilter  *adminManageSubscriptionsFilter
		excludedUserKey string
	}{
		{
			name:         "positive matched user id",
			filter:       adminManageSubscriptionsFilter{Q: "matched-id", DepartmentID: "dept-alpha-one"},
			wantUserKeys: []string{"matched"},
		},
		{
			name:         "mixed-case normalized email",
			filter:       adminManageSubscriptionsFilter{Q: "  MIXED.EMAIL@example.ORG  ", DepartmentID: "dept-alpha-one"},
			wantUserKeys: []string{"email"},
		},
		{
			name:         "one member with matched-id and normalized-email mappings",
			filter:       adminManageSubscriptionsFilter{Q: "dual", DepartmentID: "dept-alpha-one"},
			wantUserKeys: []string{"dual-id", "dual-email"},
		},
		{
			name:         "multi-membership through ancestor subtree",
			filter:       adminManageSubscriptionsFilter{Q: "multi-member", DepartmentID: "dept-alpha"},
			wantUserKeys: []string{"multi"},
		},
		{
			name:         "cycle anchor has exact effective component",
			filter:       adminManageSubscriptionsFilter{DepartmentID: "dept-cycle-a"},
			wantUserKeys: []string{"cycle-a", "cycle-b", "cycle-c"},
		},
		{
			name:         "cycle non-anchor excludes anchor-only user",
			filter:       adminManageSubscriptionsFilter{DepartmentID: "dept-cycle-b"},
			wantUserKeys: []string{"cycle-b", "cycle-c"},
		},
		{
			name:         "cycle leaf has itself only",
			filter:       adminManageSubscriptionsFilter{DepartmentID: "dept-cycle-c"},
			wantUserKeys: []string{"cycle-c"},
		},
		{
			name:            "current membership overrides conflicting legacy primary",
			filter:          adminManageSubscriptionsFilter{Q: "override-current", DepartmentID: "dept-beta"},
			wantUserKeys:    []string{"override"},
			excludedFilter:  &adminManageSubscriptionsFilter{Q: "override-current", DepartmentID: "dept-alpha-one"},
			excludedUserKey: "override",
		},
		{
			name:         "legacy primary applies without current memberships",
			filter:       adminManageSubscriptionsFilter{Q: "legacy-only", DepartmentID: "dept-alpha-one"},
			wantUserKeys: []string{"legacy"},
		},
		{
			name:         "search department and access status intersect",
			filter:       adminManageSubscriptionsFilter{Q: "matched", DepartmentID: "dept-alpha", AccessStatus: "configured"},
			wantUserKeys: []string{"matched"},
		},
		{
			name:         "unknown department",
			filter:       adminManageSubscriptionsFilter{DepartmentID: "dept-unknown"},
			wantUserKeys: []string{},
		},
		{
			name:         "unmatched user without department filter",
			filter:       adminManageSubscriptionsFilter{Q: "unmatched-local"},
			wantUserKeys: []string{"unmatched"},
		},
		{
			name:         "unmatched user with department filter",
			filter:       adminManageSubscriptionsFilter{Q: "unmatched-local", DepartmentID: "dept-alpha"},
			wantUserKeys: []string{},
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			want := adminUsersCurrentFilterFixtureIDs(fixture, tt.wantUserKeys...)
			listIDs := listAllAdminUserIDsForCurrentFilter(t, fixture.client, tt.filter)
			if !slices.Equal(listIDs, want) {
				t.Fatalf("complete List ids = %v, want %v", listIDs, want)
			}
			persistedIDs := startAdminUsersCurrentFilterJob(t, fixture, tt.filter, http.StatusOK)
			if !slices.Equal(persistedIDs, listIDs) {
				t.Fatalf("persisted snapshot ids = %v, complete List ids = %v", persistedIDs, listIDs)
			}
			batchIDs := runAdminUsersCurrentFilterBatch(t, fixture, tt.filter, http.StatusOK)
			if !slices.Equal(batchIDs, listIDs) {
				t.Fatalf("compatibility batch ids = %v, complete List ids = %v", batchIDs, listIDs)
			}
			if tt.excludedFilter != nil {
				excluded := listAllAdminUserIDsForCurrentFilter(t, fixture.client, *tt.excludedFilter)
				for _, id := range excluded {
					if id == fixture.users[tt.excludedUserKey].ID {
						t.Fatalf("conflicting legacy primary leaked user %d into ids %v", id, excluded)
					}
				}
			}
			if tt.filter.DepartmentID == "dept-cycle-b" {
				anchorID := fixture.users["cycle-a"].ID
				for _, ids := range [][]int{listIDs, persistedIDs, batchIDs} {
					for _, id := range ids {
						if id == anchorID {
							t.Fatalf("cycle B included anchor-only user %d in %v", anchorID, ids)
						}
					}
				}
			}
		})
	}
}

func TestAdminUsersCurrentFilterTargetLimitParity(t *testing.T) {
	for _, targetCount := range []int{adminsubscription.MaxTargets, adminsubscription.MaxTargets + 1} {
		t.Run(strconv.Itoa(targetCount), func(t *testing.T) {
			fixture := seedAdminUsersBulkCurrentFilterMutationFixture(t, targetCount)
			filter := adminManageSubscriptionsFilter{Q: "  bulk-target-  "}
			listIDs := listAllAdminUserIDsForCurrentFilter(t, fixture.client, filter)
			if len(listIDs) != targetCount {
				t.Fatalf("complete List target count = %d, want %d", len(listIDs), targetCount)
			}
			if targetCount == adminsubscription.MaxTargets {
				persistedIDs := startAdminUsersCurrentFilterJob(t, fixture, filter, http.StatusOK)
				if !slices.Equal(persistedIDs, listIDs) {
					t.Fatalf("500-target persisted ids differ: got %d want %d", len(persistedIDs), len(listIDs))
				}
				batchIDs := runAdminUsersCurrentFilterBatch(t, fixture, filter, http.StatusOK)
				if !slices.Equal(batchIDs, listIDs) {
					t.Fatalf("500-target batch ids differ: got %d want %d", len(batchIDs), len(listIDs))
				}
				return
			}

			startAdminUsersCurrentFilterJob(t, fixture, filter, http.StatusUnprocessableEntity)
			if count := fixture.client.AdminSubscriptionJob.Query().CountX(context.Background()); count != 0 {
				t.Fatalf("job rows = %d, want zero for 501 targets", count)
			}
			runAdminUsersCurrentFilterBatch(t, fixture, filter, http.StatusUnprocessableEntity)
		})
	}
}

func TestAdminUsersCurrentFilterInvalidAccessStatusReturns400WithoutMutation(t *testing.T) {
	fixture := seedAdminUsersBulkCurrentFilterMutationFixture(t, 1)
	filter := adminManageSubscriptionsFilter{AccessStatus: " unknown "}
	startAdminUsersCurrentFilterJob(t, fixture, filter, http.StatusBadRequest)
	if count := fixture.client.AdminSubscriptionJob.Query().CountX(context.Background()); count != 0 {
		t.Fatalf("job rows = %d, want zero for invalid access status", count)
	}
	runAdminUsersCurrentFilterBatch(t, fixture, filter, http.StatusBadRequest)
}

func seedAdminUsersCurrentFilterMutationFixture(t *testing.T) adminUsersCurrentFilterMutationFixture {
	t.Helper()
	client := testdb.Open(t)
	fixture := adminUsersCurrentFilterMutationFixture{
		client:               client,
		users:                map[string]*ent.User{},
		localUserIDByRelayID: map[int64]int{},
	}
	fixture.providerID = createAdminUsersCurrentFilterProvider(t, client)

	createUser := func(key, username, email, relayPassword string, relayUserID int) *ent.User {
		builder := client.User.Create().
			SetUsername(username).
			SetEmail(email).
			SetAuthSource("ldap").
			SetRole("user").
			SetRelayUserID(relayUserID)
		if relayPassword != "" {
			builder.SetRelayAuthPassword(relayPassword)
		}
		user := builder.SaveX(context.Background())
		fixture.users[key] = user
		fixture.localUserIDByRelayID[int64(relayUserID)] = user.ID
		return user
	}
	createUser("matched", "matched-id", "matched-local@example.com", "encrypted-password", 2101)
	createUser("email", "email-match", " Mixed.Email@Example.org ", "", 2102)
	createUser("dual-id", "dual-id", "dual-id@example.com", "", 2103)
	createUser("dual-email", "dual-email", " Dual.Email@Example.org ", "", 2104)
	createUser("multi", "multi-member", "multi@example.com", "", 2105)
	createUser("override", "override-current", "override@example.com", "", 2106)
	createUser("legacy", "legacy-only", "legacy@example.org", "", 2107)
	createUser("cycle-a", "cycle-a", "cycle-a@example.com", "", 2108)
	createUser("cycle-b", "cycle-b", "cycle-b@example.org", "", 2109)
	createUser("cycle-c", "cycle-c", "cycle-c@example.net", "", 2110)
	createUser("unmatched", "unmatched-local", "unmatched@example.com", "", 2111)

	ctx := context.Background()
	source := client.DirectorySource.Create().
		SetName("Current Directory").
		SetDescription("Synthetic current-filter mutation fixture").
		SetEnabled(true).
		SetDsl("version: 1\nscope: full_company\nsteps: []\n").
		SaveX(ctx)
	run := client.DirectorySyncRun.Create().
		SetSourceID(source.ID).
		SetMode("apply").
		SetStatus("completed").
		SetPhase("completed").
		SetDepartmentCount(6).
		SetMemberCount(10).
		SetCompletedAt(time.Date(2026, 7, 15, 10, 0, 0, 0, time.UTC)).
		SaveX(ctx)
	client.DirectorySource.UpdateOneID(source.ID).SetLastRunID(run.ID).SetLastSuccessfulRunID(run.ID).SaveX(ctx)

	for _, department := range []struct {
		id              string
		parent          string
		effectiveParent string
		name            string
	}{
		{id: "dept-alpha", name: "Department Alpha"},
		{id: "dept-alpha-one", parent: "dept-alpha", effectiveParent: "dept-alpha", name: "Team Alpha One"},
		{id: "dept-beta", name: "Department Beta"},
		{id: "dept-cycle-a", parent: "dept-cycle-c", name: "Cycle Alpha"},
		{id: "dept-cycle-b", parent: "dept-cycle-a", effectiveParent: "dept-cycle-a", name: "Cycle Beta"},
		{id: "dept-cycle-c", parent: "dept-cycle-b", effectiveParent: "dept-cycle-b", name: "Cycle Gamma"},
	} {
		builder := client.DirectoryDepartment.Create().
			SetSourceID(source.ID).
			SetExternalID(department.id).
			SetName(department.name).
			SetPath("synthetic/" + department.id).
			SetLastSeenRunID(run.ID)
		if department.parent != "" {
			builder.SetParentExternalID(department.parent)
		}
		if department.effectiveParent != "" {
			builder.SetEffectiveParentExternalID(department.effectiveParent)
		}
		builder.SaveX(ctx)
	}

	createMember := func(externalID, email, legacyDepartmentID string, matchedUser *ent.User, membershipIDs ...string) {
		builder := client.DirectoryMember.Create().
			SetSourceID(source.ID).
			SetExternalID(externalID).
			SetEmailNormalized(strings.ToLower(strings.TrimSpace(email))).
			SetDisplayName(externalID).
			SetDepartmentExternalID(legacyDepartmentID).
			SetLastSeenRunID(run.ID)
		if matchedUser != nil {
			builder.SetMatchedUserID(matchedUser.ID)
		}
		member := builder.SaveX(ctx)
		for _, departmentID := range membershipIDs {
			client.DirectoryMemberDepartment.Create().
				SetSourceID(source.ID).
				SetDirectoryMemberID(member.ID).
				SetMemberExternalID(member.ExternalID).
				SetMemberEmailNormalized(member.EmailNormalized).
				SetDepartmentExternalID(departmentID).
				SetLastSeenRunID(run.ID).
				SaveX(ctx)
		}
	}
	createMember("member-matched", "directory-matched@example.com", "dept-alpha-one", fixture.users["matched"], "dept-alpha-one")
	createMember("member-email", fixture.users["email"].Email, "dept-alpha-one", nil, "dept-alpha-one")
	createMember("member-dual", fixture.users["dual-email"].Email, "dept-alpha-one", fixture.users["dual-id"], "dept-alpha-one")
	createMember("member-multi", "directory-multi@example.com", "dept-beta", fixture.users["multi"], "dept-alpha-one", "dept-beta")
	createMember("member-override", "directory-override@example.com", "dept-alpha-one", fixture.users["override"], "dept-beta")
	createMember("member-legacy", "directory-legacy@example.org", "dept-alpha-one", fixture.users["legacy"])
	createMember("member-cycle-a", "directory-cycle-a@example.com", "dept-cycle-a", fixture.users["cycle-a"], "dept-cycle-a")
	createMember("member-cycle-b", "directory-cycle-b@example.org", "dept-cycle-b", fixture.users["cycle-b"], "dept-cycle-b")
	createMember("member-cycle-c", "directory-cycle-c@example.net", "dept-cycle-c", fixture.users["cycle-c"], "dept-cycle-c")

	return fixture
}

func seedAdminUsersBulkCurrentFilterMutationFixture(t *testing.T, targetCount int) adminUsersCurrentFilterMutationFixture {
	t.Helper()
	client := testdb.Open(t)
	fixture := adminUsersCurrentFilterMutationFixture{
		client:               client,
		users:                map[string]*ent.User{},
		localUserIDByRelayID: make(map[int64]int, targetCount),
	}
	fixture.providerID = createAdminUsersCurrentFilterProvider(t, client)
	builders := make([]*ent.UserCreate, 0, targetCount)
	for i := 0; i < targetCount; i++ {
		builders = append(builders, client.User.Create().
			SetUsername(fmt.Sprintf("bulk-target-%03d", i+1)).
			SetEmail(fmt.Sprintf("bulk-target-%03d@example.com", i+1)).
			SetAuthSource("ldap").
			SetRole("user").
			SetRelayUserID(10000+i))
	}
	users, err := client.User.CreateBulk(builders...).Save(context.Background())
	if err != nil {
		t.Fatalf("create %d bulk current-filter users: %v", targetCount, err)
	}
	for i, user := range users {
		fixture.localUserIDByRelayID[int64(10000+i)] = user.ID
	}
	return fixture
}

func createAdminUsersCurrentFilterProvider(t *testing.T, client *ent.Client) int {
	t.Helper()
	provider := client.RelayProvider.Create().
		SetName("synthetic-relay").
		SetDisplayName("Synthetic Relay").
		SetBaseURL("https://relay.example.com").
		SetAdminAPIKey("test-admin-key").
		SetDefaultModel("test-model").
		SetIsPrimary(true).
		SetEnabled(true).
		SaveX(context.Background())
	return provider.ID
}

func adminUsersCurrentFilterFixtureIDs(fixture adminUsersCurrentFilterMutationFixture, keys ...string) []int {
	ids := make([]int, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, fixture.users[key].ID)
	}
	return ids
}

func listAllAdminUserIDsForCurrentFilter(t *testing.T, client *ent.Client, filter adminManageSubscriptionsFilter) []int {
	t.Helper()
	handler := NewAdminUsersHandler(client, adminUsersTestEncryptionKey)
	router := gin.New()
	router.GET("/admin/users", handler.List)
	page := 1
	ids := []int{}
	for {
		values := url.Values{}
		values.Set("page", strconv.Itoa(page))
		values.Set("page_size", "73")
		if filter.Q != "" {
			values.Set("q", filter.Q)
		}
		if filter.DepartmentID != "" {
			values.Set("department_id", filter.DepartmentID)
		}
		if filter.AccessStatus != "" {
			values.Set("access_status", filter.AccessStatus)
		}
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/admin/users?"+values.Encode(), nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /admin/users status = %d, want 200, body=%s", w.Code, w.Body.String())
		}
		var response struct {
			Data struct {
				Items []struct {
					ID int `json:"id"`
				} `json:"items"`
				Total int `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode GET /admin/users: %v, body=%s", err, w.Body.String())
		}
		for _, item := range response.Data.Items {
			ids = append(ids, item.ID)
		}
		if len(ids) >= response.Data.Total {
			return ids
		}
		if len(response.Data.Items) == 0 {
			t.Fatalf("GET /admin/users page %d returned no items before total %d; collected %d", page, response.Data.Total, len(ids))
		}
		page++
	}
}

func startAdminUsersCurrentFilterJob(t *testing.T, fixture adminUsersCurrentFilterMutationFixture, filter adminManageSubscriptionsFilter, wantStatus int) []int {
	t.Helper()
	fakeRelay := &adminUsersRelayFake{groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}}}
	handler := NewAdminUsersHandler(fixture.client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != fixture.providerID {
			return nil, fmt.Errorf("unexpected provider %d", providerID)
		}
		return fakeRelay, nil
	}))
	router := gin.New()
	router.POST("/admin/users/subscription-jobs", handler.StartSubscriptionJob)
	body, err := json.Marshal(adminManageSubscriptionsRequest{
		Scope:        "current_filter",
		Filters:      filter,
		Operation:    "add",
		ProviderID:   fixture.providerID,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("marshal subscription job request: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscription-jobs", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != wantStatus {
		t.Fatalf("POST /admin/users/subscription-jobs status = %d, want %d, body=%s", w.Code, wantStatus, w.Body.String())
	}
	if wantStatus != http.StatusOK {
		if len(fakeRelay.calls) != 0 {
			t.Fatalf("subscription job relay calls = %+v, want zero for status %d", fakeRelay.calls, wantStatus)
		}
		return nil
	}
	var response struct {
		Data struct {
			ID            int   `json:"id"`
			TargetUserIDs []int `json:"target_user_ids"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode subscription job response: %v, body=%s", err, w.Body.String())
	}
	job, err := fixture.client.AdminSubscriptionJob.Get(context.Background(), response.Data.ID)
	if err != nil {
		t.Fatalf("load persisted subscription job: %v", err)
	}
	snapshots := adminsubscription.TargetSnapshotsFromJob(job)
	ids := make([]int, 0, len(snapshots))
	for _, snapshot := range snapshots {
		ids = append(ids, snapshot.UserID)
	}
	if !slices.Equal(response.Data.TargetUserIDs, ids) {
		t.Fatalf("job response target ids = %v, persisted snapshot ids = %v", response.Data.TargetUserIDs, ids)
	}
	waitForAdminUsersSubscriptionJob(t, fixture.client, response.Data.ID)
	return ids
}

func runAdminUsersCurrentFilterBatch(t *testing.T, fixture adminUsersCurrentFilterMutationFixture, filter adminManageSubscriptionsFilter, wantStatus int) []int {
	t.Helper()
	fakeRelay := &adminUsersRelayFake{groups: []relay.Group{{ID: 42, Name: "Group Alpha", Platform: "openai", SubscriptionType: "subscription"}}}
	handler := NewAdminUsersHandler(fixture.client, adminUsersTestEncryptionKey, adminUsersProviderResolverFunc(func(_ context.Context, providerID int) (relay.Provider, error) {
		if providerID != fixture.providerID {
			return nil, fmt.Errorf("unexpected provider %d", providerID)
		}
		return fakeRelay, nil
	}))
	router := gin.New()
	router.POST("/admin/users/subscriptions/batch", handler.ManageSubscriptions)
	body, err := json.Marshal(adminManageSubscriptionsRequest{
		Scope:        "current_filter",
		Filters:      filter,
		Operation:    "add",
		ProviderID:   fixture.providerID,
		GroupID:      "42",
		ValidityDays: 30,
	})
	if err != nil {
		t.Fatalf("marshal compatibility batch request: %v", err)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/users/subscriptions/batch", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != wantStatus {
		t.Fatalf("POST /admin/users/subscriptions/batch status = %d, want %d, body=%s", w.Code, wantStatus, w.Body.String())
	}
	if wantStatus != http.StatusOK {
		if len(fakeRelay.calls) != 0 {
			t.Fatalf("compatibility batch relay calls = %+v, want zero for status %d", fakeRelay.calls, wantStatus)
		}
		return nil
	}
	ids := make([]int, 0, len(fakeRelay.calls))
	for _, call := range fakeRelay.calls {
		localUserID, ok := fixture.localUserIDByRelayID[call.UserID]
		if !ok {
			t.Fatalf("relay call user %d has no local fixture mapping", call.UserID)
		}
		ids = append(ids, localUserID)
	}
	return ids
}

func waitForAdminUsersSubscriptionJob(t *testing.T, client *ent.Client, jobID int) {
	t.Helper()
	// This watchdog bounds condition polling; job duration is not a performance assertion.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		job, err := client.AdminSubscriptionJob.Get(context.Background(), jobID)
		if err != nil {
			t.Fatalf("poll subscription job %d: %v", jobID, err)
		}
		switch job.Status {
		case adminsubscriptionjob.StatusCompleted:
			return
		case adminsubscriptionjob.StatusFailed, adminsubscriptionjob.StatusAbandoned:
			t.Fatalf("subscription job %d ended %s: %v", jobID, job.Status, job.LastError)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("subscription job %d did not complete", jobID)
}
