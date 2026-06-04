package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
	calls            []adminUsersRelaySubscriptionCall
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
