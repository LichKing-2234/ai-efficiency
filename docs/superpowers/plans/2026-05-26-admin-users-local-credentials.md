# Admin Users Local Credentials Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an admin-only local users page with search, pagination, encrypted relay password display, and explicit plaintext copy through a reveal endpoint.

**Architecture:** The backend adds a focused `AdminUsersHandler` wired under `/api/v1/admin/users`, using only the local Ent `users` table and the existing AES-GCM helper for reveal. The frontend adds a dedicated `/admin/users` route, API wrapper, and dense admin table; plaintext relay passwords are never rendered and are only written to the clipboard after a per-row reveal call. Project-level docs record the new surface after the code lands.

**Tech Stack:** Go, Gin, Ent, existing `backend/internal/pkg` response and crypto helpers, Vue 3 `<script setup lang="ts">`, Pinia auth store, Vue Router, Axios API wrappers, Vitest, Vue Test Utils, TailwindCSS.

---

## File Structure

- Create `backend/internal/handler/admin_users.go`: owns admin local user list and relay password reveal HTTP behavior.
- Create `backend/internal/handler/admin_users_test.go`: handler-level tests for search, pagination, admin-only access, encrypted list output, and plaintext reveal errors.
- Modify `backend/internal/handler/router.go`: instantiate `AdminUsersHandler` and wire `/api/v1/admin/users` routes behind `RequireAdmin`.
- Modify `frontend/src/types/index.ts`: add `AdminUser`, `AdminUsersListResponse`, and `AdminRelayPasswordRevealResponse`.
- Create `frontend/src/api/adminUsers.ts`: API wrapper for list and reveal calls.
- Create `frontend/src/views/admin/AdminUsersView.vue`: admin-only table UI with search, page size, pagination, copy encrypted, and copy plaintext.
- Create `frontend/src/__tests__/admin-users-view.test.ts`: UI tests for fetching, searching, pagination, and copy behavior.
- Modify `frontend/src/router/index.ts`: add `/admin/users` route with `requireAdmin`.
- Modify `frontend/src/components/AppSidebar.vue`: add admin-only `Users` navigation link.
- Modify `frontend/src/__tests__/app-sidebar.test.ts`: cover the new admin link and non-admin hiding behavior.
- Modify `frontend/src/__tests__/router.test.ts`: cover route registration and non-admin redirect for `/admin/users`.
- Modify `docs/architecture.md`: document the current admin users surface.

---

### Task 1: Backend Admin Users API

**Files:**
- Create: `backend/internal/handler/admin_users_test.go`
- Create: `backend/internal/handler/admin_users.go`
- Modify: `backend/internal/handler/router.go`

- [x] **Step 1: Write failing backend tests**

Create `backend/internal/handler/admin_users_test.go` with these tests. They use the existing full router helpers, sanitized fixture users, and the same all-zero 32-byte hex encryption key passed to `SetupRouter` by `setupFullTestEnv`.

```go
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
```

- [x] **Step 2: Run backend tests and confirm they fail**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestAdminUsers' -count=1
```

Expected result: the tests fail with `404` responses because `/api/v1/admin/users` has not been wired yet.

- [x] **Step 3: Implement `AdminUsersHandler`**

Create `backend/internal/handler/admin_users.go`:

```go
package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

type AdminUsersHandler struct {
	entClient     *ent.Client
	encryptionKey string
}

type adminUserRow struct {
	ID                int       `json:"id"`
	Username          string    `json:"username"`
	Email             string    `json:"email"`
	Role              string    `json:"role"`
	AuthSource        string    `json:"auth_source"`
	RelayUserID       *int      `json:"relay_user_id,omitempty"`
	RelayAuthPassword string    `json:"relay_auth_password"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type adminUsersListRequest struct {
	Q        string
	Page     int
	PageSize int
}

func NewAdminUsersHandler(entClient *ent.Client, encryptionKey string) *AdminUsersHandler {
	return &AdminUsersHandler{
		entClient:     entClient,
		encryptionKey: strings.TrimSpace(encryptionKey),
	}
}

func (h *AdminUsersHandler) List(c *gin.Context) {
	req := parseAdminUsersListRequest(c)
	query := h.entClient.User.Query()
	if req.Q != "" {
		predicates := []predicate.User{
			entuser.UsernameContainsFold(req.Q),
			entuser.EmailContainsFold(req.Q),
		}
		if n, err := strconv.Atoi(req.Q); err == nil {
			predicates = append(predicates, entuser.IDEQ(n), entuser.RelayUserIDEQ(n))
		}
		query = query.Where(entuser.Or(predicates...))
	}

	total, err := query.Clone().Count(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list users: "+err.Error())
		return
	}

	users, err := query.
		Order(ent.Asc(entuser.FieldID)).
		Limit(req.PageSize).
		Offset((req.Page - 1) * req.PageSize).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list users: "+err.Error())
		return
	}

	items := make([]adminUserRow, 0, len(users))
	for _, u := range users {
		relayPassword := ""
		if u.RelayAuthPassword != nil {
			relayPassword = strings.TrimSpace(*u.RelayAuthPassword)
		}
		items = append(items, adminUserRow{
			ID:                u.ID,
			Username:          u.Username,
			Email:             u.Email,
			Role:              string(u.Role),
			AuthSource:        string(u.AuthSource),
			RelayUserID:       u.RelayUserID,
			RelayAuthPassword: relayPassword,
			CreatedAt:         u.CreatedAt,
			UpdatedAt:         u.UpdatedAt,
		})
	}

	pkg.Success(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      req.Page,
		"page_size": req.PageSize,
	})
}

func (h *AdminUsersHandler) RevealRelayPassword(c *gin.Context) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "user not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay auth password is not stored")
		return
	}
	if h.encryptionKey == "" {
		pkg.Error(c, http.StatusInternalServerError, "relay auth password cannot be decrypted")
		return
	}

	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
	if err != nil || strings.TrimSpace(password) == "" {
		pkg.Error(c, http.StatusInternalServerError, "relay auth password cannot be decrypted")
		return
	}

	pkg.Success(c, gin.H{"password": password})
}

func parseAdminUsersListRequest(c *gin.Context) adminUsersListRequest {
	page := parseOptionalInt(c.DefaultQuery("page", "1"))
	if page <= 0 {
		page = 1
	}
	pageSize := parseOptionalInt(c.DefaultQuery("page_size", "20"))
	switch {
	case pageSize <= 0:
		pageSize = 20
	case pageSize > 100:
		pageSize = 100
	}
	return adminUsersListRequest{
		Q:        strings.TrimSpace(c.Query("q")),
		Page:     page,
		PageSize: pageSize,
	}
}
```

- [x] **Step 4: Wire admin users routes**

In `backend/internal/handler/router.go`, instantiate the handler near the other handler constructors:

```go
adminUsersHandler := NewAdminUsersHandler(entClient, encryptionKey)
```

Then add this route block after the admin provider block and before admin credentials:

```go
adminUsersGroup := protected.Group("/admin/users")
adminUsersGroup.Use(auth.RequireAdmin())
{
	adminUsersGroup.GET("", adminUsersHandler.List)
	adminUsersGroup.POST("/:id/relay-password/reveal", adminUsersHandler.RevealRelayPassword)
}
```

- [x] **Step 5: Run backend tests and confirm they pass**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestAdminUsers' -count=1
```

Expected result: all `TestAdminUsers...` tests pass.

- [x] **Step 6: Commit backend API**

Run:

```bash
git add backend/internal/handler/admin_users.go backend/internal/handler/admin_users_test.go backend/internal/handler/router.go
git commit -m "feat(backend): add admin local users credentials API"
```

---

### Task 2: Frontend API, Types, And Admin Users Page

**Files:**
- Modify: `frontend/src/types/index.ts`
- Create: `frontend/src/api/adminUsers.ts`
- Create: `frontend/src/views/admin/AdminUsersView.vue`
- Create: `frontend/src/__tests__/admin-users-view.test.ts`

- [x] **Step 1: Write failing AdminUsersView tests**

Create `frontend/src/__tests__/admin-users-view.test.ts`:

```ts
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createRouter, createMemoryHistory } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AdminUsersView from '@/views/admin/AdminUsersView.vue'

vi.mock('@/api/adminUsers', () => ({
  listAdminUsers: vi.fn(),
  revealAdminUserRelayPassword: vi.fn(),
}))

Object.assign(navigator, {
  clipboard: {
    writeText: vi.fn(),
  },
})

function createTestRouter() {
  return createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div>Dashboard</div>' } },
      { path: '/admin/users', component: AdminUsersView },
      { path: '/login', component: { template: '<div>Login</div>' } },
    ],
  })
}

async function mountAdminUsersView() {
  const { listAdminUsers } = await import('@/api/adminUsers')
  ;(listAdminUsers as any).mockResolvedValue({
    data: {
      data: {
        items: [
          {
            id: 7,
            username: 'alice',
            email: 'alice@example.com',
            role: 'user',
            auth_source: 'ldap',
            relay_user_id: 42,
            relay_auth_password: 'encrypted-relay-password-ciphertext',
            created_at: '2026-05-26T00:00:00Z',
            updated_at: '2026-05-26T01:00:00Z',
          },
        ],
        total: 120,
        page: 1,
        page_size: 20,
      },
    },
  })

  const pinia = createPinia()
  setActivePinia(pinia)
  const auth = useAuthStore(pinia)
  auth.token = 'token'
  auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'relay_sso' }

  const router = createTestRouter()
  await router.push('/admin/users')
  await router.isReady()

  const wrapper = mount(AdminUsersView, {
    global: {
      plugins: [pinia, router],
      stubs: {
        AppLayout: {
          template: '<div><slot /></div>',
        },
      },
    },
  })
  await flushPromises()
  return { wrapper, listAdminUsers }
}

describe('AdminUsersView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('loads and renders local users with pagination controls', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    expect(listAdminUsers).toHaveBeenCalledWith({ q: '', page: 1, page_size: 20 })
    expect(wrapper.text()).toContain('Admin Users')
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).toContain('alice@example.com')
    expect(wrapper.text()).toContain('ldap')
    expect(wrapper.text()).toContain('42')
    expect(wrapper.text()).toContain('encrypted-relay-password-ciphertext')
    expect(wrapper.text()).toContain('120 total')
    expect(wrapper.text()).toContain('Page 1 / 6')
  })

  it('searches from page one when the search button is clicked', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    await wrapper.get('[data-testid="admin-users-search"]').setValue('alice@example.com')
    await wrapper.get('[data-testid="admin-users-search-button"]').trigger('click')
    await flushPromises()

    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: 'alice@example.com', page: 1, page_size: 20 })
  })

  it('updates page size and next page params', async () => {
    const { wrapper, listAdminUsers } = await mountAdminUsersView()

    await wrapper.get('[data-testid="admin-users-page-size"]').setValue('50')
    await flushPromises()
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: '', page: 1, page_size: 50 })

    await wrapper.get('[data-testid="admin-users-next-page"]').trigger('click')
    await flushPromises()
    expect((listAdminUsers as any).mock.calls.at(-1)[0]).toEqual({ q: '', page: 2, page_size: 50 })
  })

  it('copies encrypted ciphertext without calling reveal', async () => {
    const { wrapper } = await mountAdminUsersView()
    const { revealAdminUserRelayPassword } = await import('@/api/adminUsers')

    await wrapper.get('[data-testid="copy-encrypted-7"]').trigger('click')

    expect(revealAdminUserRelayPassword).not.toHaveBeenCalled()
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('encrypted-relay-password-ciphertext')
  })

  it('copies plaintext from reveal without rendering plaintext', async () => {
    const { revealAdminUserRelayPassword } = await import('@/api/adminUsers')
    ;(revealAdminUserRelayPassword as any).mockResolvedValue({
      data: { data: { password: 'test-password' } },
    })

    const { wrapper } = await mountAdminUsersView()
    await wrapper.get('[data-testid="copy-plaintext-7"]').trigger('click')
    await flushPromises()

    expect(revealAdminUserRelayPassword).toHaveBeenCalledWith(7)
    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('test-password')
    expect(wrapper.text()).toContain('Copied plaintext')
    expect(wrapper.text()).not.toContain('test-password')
  })
})
```

- [x] **Step 2: Run AdminUsersView tests and confirm they fail**

Run:

```bash
cd frontend && pnpm test src/__tests__/admin-users-view.test.ts
```

Expected result: the test fails because `@/views/admin/AdminUsersView.vue` and `@/api/adminUsers` do not exist yet.

- [x] **Step 3: Add frontend types**

Append these interfaces to `frontend/src/types/index.ts` near the other admin/API response types:

```ts
export interface AdminUser {
  id: number
  username: string
  email: string
  role: string
  auth_source: string
  relay_user_id?: number | null
  relay_auth_password: string
  created_at: string
  updated_at: string
}

export interface AdminUsersListResponse {
  items: AdminUser[]
  total: number
  page: number
  page_size: number
}

export interface AdminRelayPasswordRevealResponse {
  password: string
}
```

- [x] **Step 4: Add admin users API wrapper**

Create `frontend/src/api/adminUsers.ts`:

```ts
import client from './client'
import type {
  AdminRelayPasswordRevealResponse,
  AdminUsersListResponse,
  ApiResponse,
} from '@/types'

export interface AdminUsersListParams {
  q?: string
  page?: number
  page_size?: number
}

export function listAdminUsers(params: AdminUsersListParams) {
  return client.get<ApiResponse<AdminUsersListResponse>>('/admin/users', { params })
}

export function revealAdminUserRelayPassword(id: number) {
  return client.post<ApiResponse<AdminRelayPasswordRevealResponse>>(`/admin/users/${id}/relay-password/reveal`)
}
```

- [x] **Step 5: Implement AdminUsersView**

Create `frontend/src/views/admin/AdminUsersView.vue`:

```vue
<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { listAdminUsers, revealAdminUserRelayPassword } from '@/api/adminUsers'
import type { AdminUser } from '@/types'

const loading = ref(false)
const error = ref('')
const rows = ref<AdminUser[]>([])
const total = ref(0)
const copiedState = reactive<Record<number, string>>({})
let searchTimer: ReturnType<typeof window.setTimeout> | undefined

const filters = reactive({
  q: '',
  page: 1,
  page_size: 20,
})

const totalPages = computed(() => Math.max(1, Math.ceil(total.value / filters.page_size)))
const canGoPrev = computed(() => filters.page > 1)
const canGoNext = computed(() => filters.page < totalPages.value)

async function loadUsers() {
  loading.value = true
  error.value = ''
  try {
    const res = await listAdminUsers({
      q: filters.q,
      page: filters.page,
      page_size: filters.page_size,
    })
    const data = res.data.data
    rows.value = data?.items ?? []
    total.value = data?.total ?? 0
    filters.page = data?.page ?? filters.page
    filters.page_size = data?.page_size ?? filters.page_size
  } catch (err: any) {
    error.value = err.response?.data?.message || err.message || 'Failed to load users.'
    rows.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

async function applySearch() {
  filters.page = 1
  await loadUsers()
}

async function changePageSize() {
  filters.page = 1
  await loadUsers()
}

async function previousPage() {
  if (!canGoPrev.value) return
  filters.page -= 1
  await loadUsers()
}

async function nextPage() {
  if (!canGoNext.value) return
  filters.page += 1
  await loadUsers()
}

function formatDate(value?: string | null) {
  if (!value) return '-'
  return new Date(value).toLocaleString()
}

function displayRelayUserID(user: AdminUser) {
  return user.relay_user_id == null ? '-' : String(user.relay_user_id)
}

async function copyEncrypted(user: AdminUser) {
  if (!user.relay_auth_password) {
    copiedState[user.id] = 'No encrypted password'
    return
  }
  try {
    await navigator.clipboard.writeText(user.relay_auth_password)
    copiedState[user.id] = 'Copied encrypted'
  } catch (err: any) {
    copiedState[user.id] = err.message || 'Copy failed'
  }
}

async function copyPlaintext(user: AdminUser) {
  copiedState[user.id] = ''
  try {
    const res = await revealAdminUserRelayPassword(user.id)
    const password = res.data.data?.password || ''
    if (!password) {
      copiedState[user.id] = 'No plaintext returned'
      return
    }
    await navigator.clipboard.writeText(password)
    copiedState[user.id] = 'Copied plaintext'
  } catch (err: any) {
    copiedState[user.id] = err.response?.data?.message || err.message || 'Copy failed'
  }
}

watch(
  () => filters.q,
  () => {
    if (searchTimer) {
      window.clearTimeout(searchTimer)
    }
    searchTimer = window.setTimeout(() => {
      void applySearch()
    }, 300)
  }
)

onMounted(loadUsers)
</script>

<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex items-start justify-between gap-4">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">Admin Users</h1>
          <p class="mt-1 text-sm text-gray-500">Inspect local users and copy stored relay credentials when needed.</p>
        </div>
        <button
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          :disabled="loading"
          @click="loadUsers"
        >
          {{ loading ? 'Loading...' : 'Refresh' }}
        </button>
      </div>

      <div class="rounded-lg bg-white p-4 shadow">
        <div class="grid gap-3 md:grid-cols-[minmax(0,1fr)_120px_auto]">
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            Search
            <input
              v-model="filters.q"
              data-testid="admin-users-search"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              placeholder="username, email, local id, relay user id"
              @keyup.enter="applySearch"
            />
          </label>
          <label class="text-xs font-medium uppercase tracking-wide text-gray-500">
            Page Size
            <select
              v-model.number="filters.page_size"
              data-testid="admin-users-page-size"
              class="mt-1 w-full rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700"
              @change="changePageSize"
            >
              <option :value="10">10</option>
              <option :value="20">20</option>
              <option :value="50">50</option>
              <option :value="100">100</option>
            </select>
          </label>
          <div class="flex items-end">
            <button
              data-testid="admin-users-search-button"
              class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white disabled:opacity-50"
              :disabled="loading"
              @click="applySearch"
            >
              Search
            </button>
          </div>
        </div>
        <p v-if="error" class="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <div class="flex flex-wrap items-center justify-between gap-3">
          <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Local Users</h2>
          <div class="flex items-center gap-2 text-xs text-gray-500">
            <span>{{ total }} total</span>
            <button
              data-testid="admin-users-prev-page"
              class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
              :disabled="!canGoPrev || loading"
              @click="previousPage"
            >
              Prev
            </button>
            <span>Page {{ filters.page }} / {{ totalPages }}</span>
            <button
              data-testid="admin-users-next-page"
              class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
              :disabled="!canGoNext || loading"
              @click="nextPage"
            >
              Next
            </button>
          </div>
        </div>

        <div v-if="rows.length > 0" class="mt-3 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">ID</th>
                <th class="px-3 py-2 text-left font-medium">Username</th>
                <th class="px-3 py-2 text-left font-medium">Email</th>
                <th class="px-3 py-2 text-left font-medium">Role</th>
                <th class="px-3 py-2 text-left font-medium">Auth Source</th>
                <th class="px-3 py-2 text-left font-medium">Relay User ID</th>
                <th class="px-3 py-2 text-left font-medium">Relay Auth Password</th>
                <th class="px-3 py-2 text-left font-medium">Created</th>
                <th class="px-3 py-2 text-left font-medium">Updated</th>
                <th class="px-3 py-2 text-left font-medium">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr v-for="row in rows" :key="row.id">
                <td class="whitespace-nowrap px-3 py-2 text-gray-600">{{ row.id }}</td>
                <td class="px-3 py-2 font-medium text-gray-900">{{ row.username }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.email }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.role }}</td>
                <td class="px-3 py-2 text-gray-700">{{ row.auth_source }}</td>
                <td class="px-3 py-2 text-gray-700">{{ displayRelayUserID(row) }}</td>
                <td class="max-w-sm break-all px-3 py-2 font-mono text-xs text-gray-700">{{ row.relay_auth_password || '-' }}</td>
                <td class="whitespace-nowrap px-3 py-2 text-gray-600">{{ formatDate(row.created_at) }}</td>
                <td class="whitespace-nowrap px-3 py-2 text-gray-600">{{ formatDate(row.updated_at) }}</td>
                <td class="whitespace-nowrap px-3 py-2">
                  <div class="flex flex-col gap-1">
                    <button
                      :data-testid="`copy-encrypted-${row.id}`"
                      class="rounded border border-gray-200 px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                      :disabled="!row.relay_auth_password"
                      @click="copyEncrypted(row)"
                    >
                      Copy encrypted
                    </button>
                    <button
                      :data-testid="`copy-plaintext-${row.id}`"
                      class="rounded border border-gray-200 px-2 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-40"
                      :disabled="!row.relay_auth_password"
                      @click="copyPlaintext(row)"
                    >
                      Copy plaintext
                    </button>
                    <span v-if="copiedState[row.id]" class="text-xs text-gray-500">{{ copiedState[row.id] }}</span>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="mt-3 text-sm text-gray-400">No users match current filters.</div>
      </div>
    </div>
  </AppLayout>
</template>
```

- [x] **Step 6: Run AdminUsersView tests and confirm they pass**

Run:

```bash
cd frontend && pnpm test src/__tests__/admin-users-view.test.ts
```

Expected result: all AdminUsersView tests pass.

- [x] **Step 7: Commit frontend admin users page**

Run:

```bash
git add frontend/src/types/index.ts frontend/src/api/adminUsers.ts frontend/src/views/admin/AdminUsersView.vue frontend/src/__tests__/admin-users-view.test.ts
git commit -m "feat(frontend): add admin users credentials page"
```

---

### Task 3: Route, Sidebar Entry, And Architecture Docs

**Files:**
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/components/AppSidebar.vue`
- Modify: `frontend/src/__tests__/app-sidebar.test.ts`
- Modify: `frontend/src/__tests__/router.test.ts`
- Modify: `docs/architecture.md`

- [x] **Step 1: Add failing sidebar tests**

In `frontend/src/__tests__/app-sidebar.test.ts`, update `createTestRouter` routes to include `/admin/users`:

```ts
{ path: '/admin/users', component: { template: '<div>Admin Users</div>' } },
```

Add these tests after the existing admin Settings test:

```ts
it('renders Users link for admin users', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()

  const { useAuthStore } = await import('@/stores/auth')
  const auth = useAuthStore(pinia)
  auth.user = { id: 1, username: 'admin', email: 'admin@example.com', role: 'admin', auth_source: 'relay_sso' }

  const wrapper = mount(AppSidebar, {
    global: { plugins: [pinia, router] },
  })

  const linkTexts = wrapper.findAll('a').map((l) => l.text())
  expect(linkTexts).toContain('Users')
})

it('hides Users link for regular users', async () => {
  const pinia = createPinia()
  setActivePinia(pinia)

  const router = createTestRouter()
  await router.push('/')
  await router.isReady()

  const { useAuthStore } = await import('@/stores/auth')
  const auth = useAuthStore(pinia)
  auth.user = { id: 2, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'ldap' }

  const wrapper = mount(AppSidebar, {
    global: { plugins: [pinia, router] },
  })

  const linkTexts = wrapper.findAll('a').map((l) => l.text())
  expect(linkTexts).not.toContain('Users')
})
```

In `frontend/src/__tests__/router.test.ts`, add a route registration test near the existing route checks:

```ts
it('includes admin users route requiring admin access', () => {
  const adminUsersRoute = router.getRoutes().find((r) => r.name === 'AdminUsers')
  expect(adminUsersRoute?.path).toBe('/admin/users')
  expect(adminUsersRoute?.meta.requireAdmin).toBe(true)
})
```

Add a guard behavior test inside `describe('Router Guards', ...)`:

```ts
it('redirects non-admin users away from admin users route', async () => {
  const { getMe: mockGetMe } = await import('@/api/auth')
  ;(mockGetMe as any).mockResolvedValue({
    data: { data: { id: 2, username: 'alice', email: 'alice@example.com', role: 'user', auth_source: 'ldap' } },
  })

  localStorage.setItem('token', 'valid-token')

  await router.push('/admin/users?case=non-admin')

  expect(router.currentRoute.value.path).toBe('/')
})
```

- [x] **Step 2: Run sidebar tests and confirm they fail**

Run:

```bash
cd frontend && pnpm test src/__tests__/app-sidebar.test.ts src/__tests__/router.test.ts
```

Expected result: the admin Users link test fails because the sidebar has no `/admin/users` link, and the route registration test fails because the route has not been added.

- [x] **Step 3: Add router route**

In `frontend/src/router/index.ts`, add this route before `/settings`:

```ts
{
  path: '/admin/users',
  name: 'AdminUsers',
  component: () => import('@/views/admin/AdminUsersView.vue'),
  meta: { requireAdmin: true },
},
```

- [x] **Step 4: Add sidebar Users link**

In `frontend/src/components/AppSidebar.vue`, add this admin-only `RouterLink` before the Settings link:

```vue
<RouterLink
  v-if="auth.isAdmin"
  to="/admin/users"
  class="flex items-center rounded-md px-3 py-2 text-sm font-medium hover:bg-gray-800"
  active-class="bg-gray-800"
>
  <svg class="mr-3 h-5 w-5" fill="none" stroke="currentColor" viewBox="0 0 24 24">
    <path stroke-linecap="round" stroke-linejoin="round" stroke-width="2"
      d="M17 20h5v-2a4 4 0 00-4-4h-1M9 20H4v-2a4 4 0 014-4h1m8-4a4 4 0 11-8 0 4 4 0 018 0zm-10 0a3 3 0 11-6 0 3 3 0 016 0z" />
  </svg>
  Users
</RouterLink>
```

- [x] **Step 5: Update architecture documentation**

In `docs/architecture.md`, update the Notes list near the existing `/user` and auth bullets by adding a bullet with this content:

```markdown
- The embedded SPA also exposes an admin-only `/admin/users` surface backed only by the local `users` table. Admins can search and paginate local users, inspect `username`, `email`, `role`, `auth_source`, `relay_user_id`, timestamps, and the encrypted `relay_auth_password` ciphertext. Plaintext relay password access is separated into an explicit per-user copy action that calls `/api/v1/admin/users/:id/relay-password/reveal`; the list API never returns plaintext, and the first version does not fetch relay remote user details, API keys, usage, group facts, or mutate users.
```

Also update the frontend module table row that lists views so it mentions admin users:

```markdown
| Views | `frontend/src/views` | Dashboard, repos, events, oauth, user self-serve, admin users, and admin/settings pages |
```

- [x] **Step 6: Run route and sidebar tests and confirm they pass**

Run:

```bash
cd frontend && pnpm test src/__tests__/app-sidebar.test.ts src/__tests__/router.test.ts
```

Expected result: sidebar and router tests pass.

- [x] **Step 7: Commit route, sidebar, and docs**

Run:

```bash
git add frontend/src/router/index.ts frontend/src/components/AppSidebar.vue frontend/src/__tests__/app-sidebar.test.ts frontend/src/__tests__/router.test.ts docs/architecture.md
git commit -m "feat(frontend): add admin users navigation"
```

---

### Task 4: Final Verification

**Files:**
- Read: `docs/superpowers/specs/2026-05-26-admin-users-local-credentials-design.md`
- Read: `docs/superpowers/plans/2026-05-26-admin-users-local-credentials.md`

- [x] **Step 1: Run targeted backend verification**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestAdminUsers' -count=1
```

Expected result: pass.

- [x] **Step 2: Run full backend handler verification**

Run:

```bash
cd backend && go test ./internal/handler -count=1
```

Expected result: pass. If a failure is unrelated to admin users, record the exact failing test and error before deciding whether to broaden investigation.

- [x] **Step 3: Run targeted frontend verification**

Run:

```bash
cd frontend && pnpm test src/__tests__/admin-users-view.test.ts src/__tests__/app-sidebar.test.ts src/__tests__/router.test.ts
```

Expected result: pass.

- [x] **Step 4: Run full frontend unit tests**

Run:

```bash
cd frontend && pnpm test
```

Expected result: pass.

- [x] **Step 5: Self-review changed files**

Run:

```bash
git diff --stat HEAD~3..HEAD
git diff HEAD~3..HEAD -- backend/internal/handler/admin_users.go backend/internal/handler/router.go frontend/src/views/admin/AdminUsersView.vue frontend/src/api/adminUsers.ts frontend/src/router/index.ts frontend/src/components/AppSidebar.vue docs/architecture.md
```

Check these points:

1. `GET /api/v1/admin/users` never calls `pkg.Decrypt`.
2. `POST /api/v1/admin/users/:id/relay-password/reveal` returns plaintext only in the reveal response.
3. No test fixture uses real user email domains, real passwords, real tokens, or real API keys.
4. `AdminUsersView.vue` never stores plaintext outside the local `copyPlaintext` function.
5. `/admin/users` has `meta: { requireAdmin: true }`.
6. Backend route group uses `auth.RequireAdmin()`.

- [x] **Step 6: Update plan checkboxes**

After completing each task above, update this file's checkboxes for the steps actually completed. Do not mark verification steps complete unless the command was run.

- [x] **Step 7: Final commit if verification changes files**

If final verification or plan checkbox updates changed files, commit them:

```bash
git add docs/superpowers/plans/2026-05-26-admin-users-local-credentials.md
git commit -m "docs(plans): update admin users implementation progress"
```

If no files changed after the task commits, do not create an empty commit.
