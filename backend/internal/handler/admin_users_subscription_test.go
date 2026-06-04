package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
}

func (f *adminUsersRelayFake) ListPlatformGroups(ctx context.Context) ([]relay.Group, error) {
	return f.groups, nil
}

func (f *adminUsersRelayFake) AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error {
	f.assignedUserID = userID
	f.assignedGroupID = groupID
	f.assignedValidity = validityDays
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
