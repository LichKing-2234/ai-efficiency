package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

const adminUsersTestEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

type adminUsersFixture struct {
	aliceID    int
	bobID      int
	ciphertext string
}

func seedAdminUsersFixture(t *testing.T, env *fullTestEnv) adminUsersFixture {
	t.Helper()
	ctx := context.Background()

	ciphertext, err := pkg.Encrypt("test-password", adminUsersTestEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt relay password: %v", err)
	}

	alice, err := env.client.User.Create().
		SetUsername("alice").
		SetEmail("alice@example.com").
		SetAuthSource("ldap").
		SetRole("user").
		SetRelayUserID(42).
		SetRelayAuthPassword(ciphertext).
		Save(ctx)
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}

	bob, err := env.client.User.Create().
		SetUsername("bob").
		SetEmail("bob@example.org").
		SetAuthSource("relay_sso").
		SetRole("user").
		Save(ctx)
	if err != nil {
		t.Fatalf("create bob: %v", err)
	}

	if _, err := env.client.User.Create().
		SetUsername("carol").
		SetEmail("carol@example.net").
		SetAuthSource("ldap").
		SetRole("admin").
		SetRelayUserID(99).
		Save(ctx); err != nil {
		t.Fatalf("create carol: %v", err)
	}

	return adminUsersFixture{aliceID: alice.ID, bobID: bob.ID, ciphertext: ciphertext}
}

func TestAdminUsersListSearchPaginationAndCiphertext(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=alice&page=1&page_size=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "test-password") {
		t.Fatalf("list response leaked plaintext: %s", w.Body.String())
	}

	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("total = %d, want 1", got)
	}
	if got := int(data["page"].(float64)); got != 1 {
		t.Fatalf("page = %d, want 1", got)
	}
	if got := int(data["page_size"].(float64)); got != 2 {
		t.Fatalf("page_size = %d, want 2", got)
	}

	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	row := items[0].(map[string]interface{})
	if int(row["id"].(float64)) != fixture.aliceID {
		t.Fatalf("id = %v, want %d", row["id"], fixture.aliceID)
	}
	if row["username"] != "alice" || row["email"] != "alice@example.com" {
		t.Fatalf("unexpected identity row: %+v", row)
	}
	if row["relay_auth_password"] != fixture.ciphertext {
		t.Fatalf("relay_auth_password = %v, want ciphertext", row["relay_auth_password"])
	}
	if int(row["relay_user_id"].(float64)) != 42 {
		t.Fatalf("relay_user_id = %v, want 42", row["relay_user_id"])
	}
}

func TestAdminUsersListNumericSearchMatchesIDs(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)

	w := doFullRequest(env, http.MethodGet, "/api/v1/admin/users?q=42&page=1&page_size=20", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("relay id search status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("relay id search total = %d, want 1", got)
	}

	w = doFullRequest(env, http.MethodGet, fmt.Sprintf("/api/v1/admin/users?q=%d&page=1&page_size=20", fixture.aliceID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("local id search status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data = parseFullResponse(t, w)["data"].(map[string]interface{})
	if got := int(data["total"].(float64)); got != 1 {
		t.Fatalf("local id search total = %d, want 1", got)
	}
}

func TestAdminUsersListRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodGet, "/api/v1/admin/users", nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUsersRevealRelayPassword(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)

	w := doFullRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/relay-password/reveal", fixture.aliceID), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
	}
	data := parseFullResponse(t, w)["data"].(map[string]interface{})
	if data["password"] != "test-password" {
		t.Fatalf("password = %v, want test-password", data["password"])
	}
}

func TestAdminUsersRevealErrors(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)
	nonAdminToken := createFullNonAdminToken(t, env)

	w := doFullRequestWithToken(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/relay-password/reveal", fixture.aliceID), nil, nonAdminToken)
	if w.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want 403, body=%s", w.Code, w.Body.String())
	}

	w = doFullRequest(env, http.MethodPost, "/api/v1/admin/users/999999/relay-password/reveal", nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("missing user status = %d, want 404, body=%s", w.Code, w.Body.String())
	}

	w = doFullRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/relay-password/reveal", fixture.bobID), nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing password status = %d, want 422, body=%s", w.Code, w.Body.String())
	}

	if _, err := env.client.User.UpdateOneID(fixture.aliceID).SetRelayAuthPassword("not-hex-ciphertext").Save(context.Background()); err != nil {
		t.Fatalf("corrupt relay password: %v", err)
	}
	w = doFullRequest(env, http.MethodPost, fmt.Sprintf("/api/v1/admin/users/%d/relay-password/reveal", fixture.aliceID), nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("decrypt failure status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
}

func TestAdminUsersRevealMissingEncryptionKey(t *testing.T) {
	t.Parallel()

	env := setupFullTestEnv(t)
	fixture := seedAdminUsersFixture(t, env)

	router := gin.New()
	handler := NewAdminUsersHandler(env.client, "")
	router.POST("/admin/users/:id/relay-password/reveal", handler.RevealRelayPassword)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/admin/users/%d/relay-password/reveal", fixture.aliceID), nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("missing key status = %d, want 500, body=%s", w.Code, w.Body.String())
	}
}
