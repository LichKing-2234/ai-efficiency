# Redis-Backed Auth Session Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make ai-efficiency Web and CLI login state survive releases by storing refresh sessions and OAuth transient authorization state in the existing Redis connection.

**Architecture:** Access tokens remain short-lived JWTs signed by `AE_AUTH_JWT_SECRET`. Refresh tokens become opaque random values whose hashes are stored in Redis with TTL, user indexes, and token-family indexes. OAuth authorization-code and device-flow state moves from `oauth.Handler` in-memory maps to Redis-backed stores so rolling deploys and future multi-pod routing do not break in-progress CLI login flows.

**Tech Stack:** Go 1.23/1.24 toolchain, Gin, Ent, `github.com/redis/go-redis/v9`, Vue 3, Pinia, Axios, Helm values using existing `AE_REDIS_*`.

---

**Spec:** `docs/superpowers/specs/2026-05-25-redis-backed-auth-session-design.md`

**Status:** Planned. No implementation has been applied yet.

## Scope Check

This plan covers one shippable feature: release-stable auth state. It touches backend auth, OAuth state, frontend logout/refresh compatibility, and deploy documentation because those parts must change together for the feature to work end-to-end. It does not add a new Redis deployment, switch browser auth to cookies, or change relay/sub2api integration.

## File Structure

### Backend - Auth Session Store

| File | Responsibility |
| --- | --- |
| `backend/internal/auth/session_store.go` | Defines refresh-session data, token hashing, random refresh-token generation, Redis key prefixes, and `RefreshSessionStore` implementation. |
| `backend/internal/auth/session_store_test.go` | Tests Redis key prefixing, token hash storage, TTL behavior, user/family deletion, and test-only memory store helpers used by auth tests. |
| `backend/go.mod` / `backend/go.sum` | Adds `github.com/alicebob/miniredis/v2` for Redis store unit tests without requiring an external Redis process. |

### Backend - Auth Service and Handlers

| File | Responsibility |
| --- | --- |
| `backend/internal/auth/auth.go` | Stops issuing refresh JWTs, issues opaque refresh tokens via `RefreshSessionStore`, rotates refresh tokens on refresh, and exposes revoke methods. |
| `backend/internal/auth/auth_test.go` | Updates token-shape tests so refresh tokens are opaque and access tokens remain JWTs. |
| `backend/internal/auth/auth_service_test.go` | Covers login token creation, refresh rotation, old-token reuse rejection, user-not-found cleanup, and missing-store fail-closed behavior. |
| `backend/internal/auth/middleware_test.go` | Keeps access-token auth coverage aligned with opaque refresh token behavior. |
| `backend/internal/handler/auth.go` | Adds `Logout` and `LogoutAll` handlers while preserving existing login/refresh response shape. |
| `backend/internal/handler/auth_test.go` | Adds handler tests for logout and logout-all. Create this file if no auth handler test file currently exists. |
| `backend/internal/handler/router.go` | Registers `/api/v1/auth/logout` and `/api/v1/auth/logout-all`. |
| `backend/cmd/server/main.go` | Wires `auth.NewRedisRefreshSessionStore(redisClient)` into `authService`. |

### Backend - OAuth State Store

| File | Responsibility |
| --- | --- |
| `backend/internal/oauth/state_store.go` | Defines Redis-backed authorization-code and device-flow state store interfaces and implementation. |
| `backend/internal/oauth/state_store_test.go` | Tests consume-once authorization codes, device user-code lookup, device status updates, and shared-store behavior across handler instances. |
| `backend/internal/oauth/handler.go` | Replaces `codes`, `devices`, and `devicesByUserCode` maps with the state store. |
| `backend/internal/oauth/handler_test.go` | Updates constructor calls and authorization-code tests. |
| `backend/internal/oauth/device_internal_test.go` | Updates device-flow tests to use the state store and cover cross-handler state sharing. |
| `backend/cmd/server/main.go` | Wires `oauth.NewRedisStateStore(redisClient)` into `oauth.NewHandler`. |

### Frontend

| File | Responsibility |
| --- | --- |
| `frontend/src/api/auth.ts` | Adds `logout(refreshToken?: string)` helper. |
| `frontend/src/stores/auth.ts` | Calls logout API before clearing local tokens and still clears local state if the request fails. |
| `frontend/src/api/client.ts` | Keeps persisting the rotated refresh token returned by `/auth/refresh`. |
| `frontend/src/__tests__/auth-store.test.ts` | Covers logout revocation call plus local clearing on success and failure. |
| `frontend/src/__tests__/client.test.ts` | Keeps refresh-rotation retry tests aligned with opaque refresh tokens. |

### Deploy and Docs

| File | Responsibility |
| --- | --- |
| `deploy/config.example.yaml` | Documents Redis as auth-critical, not just a health dependency. |
| `deploy/docker-compose.yml` | Keeps the existing Redis service and documents that it stores auth sessions. |
| `deploy/docker-compose.dev.yml` | Keeps local Redis wiring unchanged for auth-session development. |
| Helm chart `ai-efficiency/values.yaml` | Keeps existing `AE_REDIS_*`; adds existing auth TTL env vars only if production TTLs are intentionally changed. |
| Helm chart `ai-efficiency/docs/deploy.md` | Documents shared external Redis, key-prefix isolation, and stable `AE_AUTH_JWT_SECRET` expectations. |

## Task 1: Add Redis Refresh Session Store

**Files:**
- Create: `backend/internal/auth/session_store.go`
- Create: `backend/internal/auth/session_store_test.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

- [ ] **Step 1: Add Redis test dependency**

Run:

```bash
cd backend && go get github.com/alicebob/miniredis/v2@latest
```

Expected:

```text
go: added github.com/alicebob/miniredis/v2 ...
```

- [ ] **Step 2: Write failing store tests**

Create `backend/internal/auth/session_store_test.go` with these tests and a test-only in-memory store helper:

```go
package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedisRefreshStore(t *testing.T) (*miniredis.Miniredis, RefreshSessionStore) {
	t.Helper()
	s := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return s, NewRedisRefreshSessionStore(rdb)
}

func TestRedisRefreshSessionStoreStoresOnlyHashWithTTLAndIndexes(t *testing.T) {
	ctx := context.Background()
	mr, store := newTestRedisRefreshStore(t)
	token := "rt_example_refresh_token"
	hash := hashRefreshToken(token)
	session := &RefreshSession{
		UserID:    7,
		Username:  "alice",
		Role:      "user",
		FamilyID:  "family-a",
		CreatedAt: time.Unix(100, 0).UTC(),
		ExpiresAt: time.Unix(200, 0).UTC(),
	}

	if err := store.StoreRefreshSession(ctx, hash, session, time.Hour); err != nil {
		t.Fatalf("StoreRefreshSession: %v", err)
	}
	if mr.Exists("rt_example_refresh_token") {
		t.Fatal("raw refresh token must never be stored as a Redis key")
	}

	got, err := store.GetRefreshSession(ctx, hash)
	if err != nil {
		t.Fatalf("GetRefreshSession: %v", err)
	}
	if got.UserID != 7 || got.Username != "alice" || got.Role != "user" || got.FamilyID != "family-a" {
		t.Fatalf("session mismatch: %#v", got)
	}
	if ttl := mr.TTL(refreshSessionKey(hash)); ttl <= 0 {
		t.Fatalf("refresh session TTL = %v, want positive", ttl)
	}
	if !mr.SIsMember(userRefreshSessionsKey(7), hash) {
		t.Fatal("user refresh-token set missing token hash")
	}
	if !mr.SIsMember(refreshFamilyKey("family-a"), hash) {
		t.Fatal("family refresh-token set missing token hash")
	}
}

func TestRedisRefreshSessionStoreDeletesSingleUserAndFamilySessions(t *testing.T) {
	ctx := context.Background()
	_, store := newTestRedisRefreshStore(t)
	firstHash := hashRefreshToken("rt_first")
	secondHash := hashRefreshToken("rt_second")

	for _, hash := range []string{firstHash, secondHash} {
		if err := store.StoreRefreshSession(ctx, hash, &RefreshSession{
			UserID:    9,
			Username:  "bob",
			Role:      "user",
			FamilyID:  "family-b",
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(time.Hour),
		}, time.Hour); err != nil {
			t.Fatalf("StoreRefreshSession(%s): %v", hash, err)
		}
	}

	if err := store.DeleteRefreshSession(ctx, firstHash); err != nil {
		t.Fatalf("DeleteRefreshSession: %v", err)
	}
	if _, err := store.GetRefreshSession(ctx, firstHash); !errors.Is(err, ErrRefreshSessionNotFound) {
		t.Fatalf("deleted token err = %v, want ErrRefreshSessionNotFound", err)
	}
	if _, err := store.GetRefreshSession(ctx, secondHash); err != nil {
		t.Fatalf("second token should still exist: %v", err)
	}

	if err := store.DeleteUserRefreshSessions(ctx, 9); err != nil {
		t.Fatalf("DeleteUserRefreshSessions: %v", err)
	}
	if _, err := store.GetRefreshSession(ctx, secondHash); !errors.Is(err, ErrRefreshSessionNotFound) {
		t.Fatalf("user-deleted token err = %v, want ErrRefreshSessionNotFound", err)
	}
}

func TestHashRefreshTokenIsStableAndDoesNotExposeToken(t *testing.T) {
	first := hashRefreshToken("rt_same")
	second := hashRefreshToken("rt_same")
	other := hashRefreshToken("rt_other")
	if first == "" || first != second {
		t.Fatalf("hash is not stable: %q %q", first, second)
	}
	if first == "rt_same" || first == other {
		t.Fatalf("hash should be non-raw and input-specific: first=%q other=%q", first, other)
	}
}

type memoryRefreshSessionStore struct {
	mu       sync.Mutex
	sessions map[string]*RefreshSession
	users    map[int]map[string]struct{}
	families map[string]map[string]struct{}
	err      error
}

func newMemoryRefreshSessionStore() *memoryRefreshSessionStore {
	return &memoryRefreshSessionStore{
		sessions: make(map[string]*RefreshSession),
		users:    make(map[int]map[string]struct{}),
		families: make(map[string]map[string]struct{}),
	}
}

func (s *memoryRefreshSessionStore) StoreRefreshSession(_ context.Context, tokenHash string, session *RefreshSession, _ time.Duration) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *session
	s.sessions[tokenHash] = &copy
	if s.users[session.UserID] == nil {
		s.users[session.UserID] = make(map[string]struct{})
	}
	s.users[session.UserID][tokenHash] = struct{}{}
	if s.families[session.FamilyID] == nil {
		s.families[session.FamilyID] = make(map[string]struct{})
	}
	s.families[session.FamilyID][tokenHash] = struct{}{}
	return nil
}

func (s *memoryRefreshSessionStore) GetRefreshSession(_ context.Context, tokenHash string) (*RefreshSession, error) {
	if s.err != nil {
		return nil, s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[tokenHash]
	if !ok {
		return nil, ErrRefreshSessionNotFound
	}
	copy := *session
	return &copy, nil
}

func (s *memoryRefreshSessionStore) DeleteRefreshSession(_ context.Context, tokenHash string) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, tokenHash)
	for _, hashes := range s.users {
		delete(hashes, tokenHash)
	}
	for _, hashes := range s.families {
		delete(hashes, tokenHash)
	}
	return nil
}

func (s *memoryRefreshSessionStore) DeleteUserRefreshSessions(_ context.Context, userID int) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash := range s.users[userID] {
		delete(s.sessions, hash)
	}
	delete(s.users, userID)
	return nil
}

func (s *memoryRefreshSessionStore) DeleteRefreshTokenFamily(_ context.Context, familyID string) error {
	if s.err != nil {
		return s.err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for hash := range s.families[familyID] {
		delete(s.sessions, hash)
	}
	delete(s.families, familyID)
	return nil
}
```

- [ ] **Step 3: Run store tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/auth -run 'TestRedisRefreshSessionStore|TestHashRefreshToken' -count=1
```

Expected:

```text
FAIL
undefined: RefreshSessionStore
undefined: NewRedisRefreshSessionStore
undefined: hashRefreshToken
```

- [ ] **Step 4: Implement Redis refresh session store**

Create `backend/internal/auth/session_store.go`:

```go
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	refreshTokenPrefix          = "rt_"
	refreshSessionKeyPrefix     = "ai-efficiency:auth:refresh:"
	userRefreshSessionsKeyPrefix = "ai-efficiency:auth:user:"
	refreshFamilyKeyPrefix      = "ai-efficiency:auth:family:"
)

var ErrRefreshSessionNotFound = errors.New("refresh session not found")

type RefreshSession struct {
	UserID    int       `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	FamilyID  string    `json:"family_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type RefreshSessionStore interface {
	StoreRefreshSession(ctx context.Context, tokenHash string, session *RefreshSession, ttl time.Duration) error
	GetRefreshSession(ctx context.Context, tokenHash string) (*RefreshSession, error)
	DeleteRefreshSession(ctx context.Context, tokenHash string) error
	DeleteUserRefreshSessions(ctx context.Context, userID int) error
	DeleteRefreshTokenFamily(ctx context.Context, familyID string) error
}

type RedisRefreshSessionStore struct {
	rdb *redis.Client
}

func NewRedisRefreshSessionStore(rdb *redis.Client) *RedisRefreshSessionStore {
	return &RedisRefreshSessionStore{rdb: rdb}
}

func refreshSessionKey(tokenHash string) string {
	return refreshSessionKeyPrefix + tokenHash
}

func userRefreshSessionsKey(userID int) string {
	return fmt.Sprintf("%s%d:refresh", userRefreshSessionsKeyPrefix, userID)
}

func refreshFamilyKey(familyID string) string {
	return refreshFamilyKeyPrefix + familyID + ":refresh"
}

func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func generateOpaqueRefreshToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate refresh token: %w", err)
	}
	return refreshTokenPrefix + hex.EncodeToString(b), nil
}

func generateTokenFamilyID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token family: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func (s *RedisRefreshSessionStore) StoreRefreshSession(ctx context.Context, tokenHash string, session *RefreshSession, ttl time.Duration) error {
	if s == nil || s.rdb == nil {
		return errors.New("refresh session store is not configured")
	}
	if session == nil {
		return errors.New("refresh session is nil")
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal refresh session: %w", err)
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, refreshSessionKey(tokenHash), payload, ttl)
	pipe.SAdd(ctx, userRefreshSessionsKey(session.UserID), tokenHash)
	pipe.Expire(ctx, userRefreshSessionsKey(session.UserID), ttl)
	pipe.SAdd(ctx, refreshFamilyKey(session.FamilyID), tokenHash)
	pipe.Expire(ctx, refreshFamilyKey(session.FamilyID), ttl)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return fmt.Errorf("store refresh session: %w", err)
	}
	return nil
}

func (s *RedisRefreshSessionStore) GetRefreshSession(ctx context.Context, tokenHash string) (*RefreshSession, error) {
	if s == nil || s.rdb == nil {
		return nil, errors.New("refresh session store is not configured")
	}
	payload, err := s.rdb.Get(ctx, refreshSessionKey(tokenHash)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrRefreshSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh session: %w", err)
	}
	var session RefreshSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("unmarshal refresh session: %w", err)
	}
	return &session, nil
}

func (s *RedisRefreshSessionStore) DeleteRefreshSession(ctx context.Context, tokenHash string) error {
	if s == nil || s.rdb == nil {
		return errors.New("refresh session store is not configured")
	}
	if err := s.rdb.Del(ctx, refreshSessionKey(tokenHash)).Err(); err != nil {
		return fmt.Errorf("delete refresh session: %w", err)
	}
	return nil
}

func (s *RedisRefreshSessionStore) DeleteUserRefreshSessions(ctx context.Context, userID int) error {
	if s == nil || s.rdb == nil {
		return errors.New("refresh session store is not configured")
	}
	indexKey := userRefreshSessionsKey(userID)
	hashes, err := s.rdb.SMembers(ctx, indexKey).Result()
	if err != nil {
		return fmt.Errorf("list user refresh sessions: %w", err)
	}
	keys := make([]string, 0, len(hashes)+1)
	for _, hash := range hashes {
		keys = append(keys, refreshSessionKey(hash))
	}
	keys = append(keys, indexKey)
	if len(keys) == 0 {
		return nil
	}
	if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("delete user refresh sessions: %w", err)
	}
	return nil
}

func (s *RedisRefreshSessionStore) DeleteRefreshTokenFamily(ctx context.Context, familyID string) error {
	if s == nil || s.rdb == nil {
		return errors.New("refresh session store is not configured")
	}
	indexKey := refreshFamilyKey(familyID)
	hashes, err := s.rdb.SMembers(ctx, indexKey).Result()
	if err != nil {
		return fmt.Errorf("list refresh token family: %w", err)
	}
	keys := make([]string, 0, len(hashes)+1)
	for _, hash := range hashes {
		keys = append(keys, refreshSessionKey(hash))
	}
	keys = append(keys, indexKey)
	if len(keys) == 0 {
		return nil
	}
	if err := s.rdb.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("delete refresh token family: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run store tests and verify they pass**

Run:

```bash
cd backend && go test ./internal/auth -run 'TestRedisRefreshSessionStore|TestHashRefreshToken' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/auth
```

- [ ] **Step 6: Commit store foundation**

Run:

```bash
git add backend/go.mod backend/go.sum backend/internal/auth/session_store.go backend/internal/auth/session_store_test.go
git commit -m "feat(auth): add redis refresh session store"
```

## Task 2: Replace Refresh JWTs with Redis-Backed Opaque Refresh Tokens

**Files:**
- Modify: `backend/internal/auth/auth.go`
- Modify: `backend/internal/auth/auth_test.go`
- Modify: `backend/internal/auth/auth_service_test.go`
- Modify: `backend/internal/auth/middleware_test.go`

- [ ] **Step 1: Update test helpers to provide a refresh session store**

In `backend/internal/auth/auth_test.go`, change `newTestService()`:

```go
func newTestService() *Service {
	svc := &Service{
		jwtSecret:       []byte("test-secret-key-for-unit-tests!!"),
		accessTokenTTL:  2 * time.Hour,
		refreshTokenTTL: 7 * 24 * time.Hour,
		now:             time.Now,
	}
	svc.SetRefreshSessionStore(newMemoryRefreshSessionStore())
	return svc
}
```

In `backend/internal/auth/auth_service_test.go`, change `newTestServiceWithDB(t)` so every DB-backed auth service also has a memory store:

```go
func newTestServiceWithDB(t *testing.T) (*Service, *ent.Client) {
	t.Helper()
	client := setupAuthEntClient(t)
	svc := NewService(client, "test-secret-key-for-unit-tests!!", 7200, 604800, zap.NewNop())
	svc.SetRefreshSessionStore(newMemoryRefreshSessionStore())
	return svc, client
}
```

- [ ] **Step 2: Write failing auth behavior tests**

Replace `TestValidateRefreshToken` in `backend/internal/auth/auth_test.go` with:

```go
func TestRefreshTokenIsOpaqueAndAccessTokenRemainsJWT(t *testing.T) {
	svc := newTestService()
	info := &UserInfo{ID: 42, Username: "bob", Role: "user"}
	pair, err := svc.generateTokenPair(context.Background(), info)
	if err != nil {
		t.Fatalf("generateTokenPair: %v", err)
	}

	if !strings.HasPrefix(pair.RefreshToken, refreshTokenPrefix) {
		t.Fatalf("refresh token = %q, want opaque %s prefix", pair.RefreshToken, refreshTokenPrefix)
	}
	if strings.Count(pair.RefreshToken, ".") != 0 {
		t.Fatalf("refresh token must not be a JWT: %q", pair.RefreshToken)
	}
	if _, err := svc.ValidateAccessToken(pair.AccessToken); err != nil {
		t.Fatalf("access token should remain a valid JWT: %v", err)
	}
}
```

Add these tests near the existing refresh-token tests in `backend/internal/auth/auth_service_test.go`:

```go
func TestRefreshTokenRotatesAndRejectsOldToken(t *testing.T) {
	svc, client := newTestServiceWithDB(t)
	ctx := context.Background()
	u, err := client.User.Create().
		SetUsername("rotateuser").
		SetEmail("rotate@example.com").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		Save(ctx)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	pair, err := svc.generateTokenPair(ctx, &UserInfo{ID: u.ID, Username: u.Username, Role: string(u.Role)})
	if err != nil {
		t.Fatalf("generateTokenPair: %v", err)
	}
	nextPair, _, err := svc.RefreshToken(ctx, pair.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if nextPair.RefreshToken == pair.RefreshToken {
		t.Fatal("refresh token was not rotated")
	}
	if _, _, err := svc.RefreshToken(ctx, pair.RefreshToken); err == nil {
		t.Fatal("old refresh token should be rejected after rotation")
	}
	if _, _, err := svc.RefreshToken(ctx, nextPair.RefreshToken); err != nil {
		t.Fatalf("latest refresh token should work: %v", err)
	}
}

func TestGenerateTokenPairFailsClosedWithoutRefreshStore(t *testing.T) {
	svc := &Service{
		jwtSecret:       []byte("test-secret-key-for-unit-tests!!"),
		accessTokenTTL:  time.Hour,
		refreshTokenTTL: time.Hour,
		now:             time.Now,
	}
	_, err := svc.generateTokenPair(context.Background(), &UserInfo{ID: 1, Username: "alice", Role: "user"})
	if err == nil || !strings.Contains(err.Error(), "refresh session store") {
		t.Fatalf("generateTokenPair error = %v, want refresh session store failure", err)
	}
}

func TestRefreshTokenDeletesFamilyWhenUserMissing(t *testing.T) {
	store := newMemoryRefreshSessionStore()
	svc, _ := newTestServiceWithDB(t)
	svc.SetRefreshSessionStore(store)

	pair, err := svc.generateTokenPair(context.Background(), &UserInfo{ID: 999999, Username: "ghost", Role: "user"})
	if err != nil {
		t.Fatalf("generateTokenPair: %v", err)
	}
	if _, _, err := svc.RefreshToken(context.Background(), pair.RefreshToken); err == nil {
		t.Fatal("expected refresh to fail for deleted user")
	}
	if _, err := store.GetRefreshSession(context.Background(), hashRefreshToken(pair.RefreshToken)); !errors.Is(err, ErrRefreshSessionNotFound) {
		t.Fatalf("session should be deleted after missing user, got err=%v", err)
	}
}
```

Add imports required by those tests:

```go
import (
	"errors"
	"strings"
)
```

- [ ] **Step 3: Run auth tests and verify they fail for JWT refresh behavior**

Run:

```bash
cd backend && go test ./internal/auth -run 'TestRefreshTokenIsOpaque|TestRefreshTokenRotates|TestGenerateTokenPairFailsClosed|TestRefreshTokenDeletesFamily' -count=1
```

Expected:

```text
FAIL
refresh token must not be a JWT
```

- [ ] **Step 4: Update auth service to use refresh session store**

In `backend/internal/auth/auth.go`, update imports:

```go
import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)
```

Extend `Service`:

```go
type Service struct {
	providers             []AuthProvider
	entClient             *ent.Client
	jwtSecret             []byte
	encryptionKey         string
	accessTokenTTL        time.Duration
	refreshTokenTTL       time.Duration
	refreshSessionStore   RefreshSessionStore
	now                   func() time.Time
	relayIdentityResolver *RelayIdentityResolver
	logger                *zap.Logger
}
```

Set `now` in `NewService`:

```go
return &Service{
	entClient:       entClient,
	jwtSecret:       []byte(jwtSecret),
	encryptionKey:   firstNonEmptyString(encryptionKeys...),
	accessTokenTTL:  time.Duration(accessTTL) * time.Second,
	refreshTokenTTL: time.Duration(refreshTTL) * time.Second,
	now:             time.Now,
	logger:          logger,
}
```

Add the setter:

```go
func (s *Service) SetRefreshSessionStore(store RefreshSessionStore) {
	s.refreshSessionStore = store
}
```

Replace `RefreshToken` with Redis-backed rotation:

```go
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (*TokenPair, *UserInfo, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if !strings.HasPrefix(refreshToken, refreshTokenPrefix) {
		return nil, nil, fmt.Errorf("invalid refresh token")
	}
	if s.refreshSessionStore == nil {
		return nil, nil, fmt.Errorf("refresh session store is not configured")
	}

	tokenHash := hashRefreshToken(refreshToken)
	session, err := s.refreshSessionStore.GetRefreshSession(ctx, tokenHash)
	if err != nil {
		if errors.Is(err, ErrRefreshSessionNotFound) {
			return nil, nil, fmt.Errorf("invalid refresh token")
		}
		return nil, nil, fmt.Errorf("get refresh session: %w", err)
	}
	if !s.now().Before(session.ExpiresAt) {
		_ = s.refreshSessionStore.DeleteRefreshSession(ctx, tokenHash)
		return nil, nil, fmt.Errorf("refresh token expired")
	}

	u, err := s.entClient.User.Get(ctx, session.UserID)
	if err != nil {
		if ent.IsNotFound(err) {
			_ = s.refreshSessionStore.DeleteRefreshTokenFamily(ctx, session.FamilyID)
		}
		return nil, nil, fmt.Errorf("get user: %w", err)
	}
	userInfo := &UserInfo{
		ID:         u.ID,
		Username:   u.Username,
		Email:      u.Email,
		Role:       string(u.Role),
		AuthSource: string(u.AuthSource),
	}

	if err := s.refreshSessionStore.DeleteRefreshSession(ctx, tokenHash); err != nil {
		return nil, nil, fmt.Errorf("rotate refresh token: %w", err)
	}
	tokens, err := s.generateTokenPairForFamily(ctx, userInfo, session.FamilyID)
	if err != nil {
		return nil, nil, fmt.Errorf("generate tokens: %w", err)
	}
	return tokens, userInfo, nil
}
```

In `Login`, replace the final token-generation block with:

```go
tokens, err := s.generateTokenPair(ctx, userInfo)
if err != nil {
	return nil, nil, fmt.Errorf("generate tokens: %w", err)
}

return tokens, userInfo, nil
```

Replace `GenerateTokenPairForUser`, `generateTokenPair`, and add `generateTokenPairForFamily` plus `generateAccessToken`:

```go
func (s *Service) GenerateTokenPairForUser(info *UserInfo) (*TokenPair, error) {
	return s.generateTokenPair(context.Background(), info)
}

func (s *Service) generateTokenPair(ctx context.Context, info *UserInfo) (*TokenPair, error) {
	return s.generateTokenPairForFamily(ctx, info, "")
}

func (s *Service) generateTokenPairForFamily(ctx context.Context, info *UserInfo, familyID string) (*TokenPair, error) {
	if s.refreshSessionStore == nil {
		return nil, fmt.Errorf("refresh session store is not configured")
	}
	now := s.now()
	accessStr, err := s.generateAccessToken(info, now)
	if err != nil {
		return nil, err
	}
	refreshStr, err := generateOpaqueRefreshToken()
	if err != nil {
		return nil, err
	}
	if familyID == "" {
		familyID, err = generateTokenFamilyID()
		if err != nil {
			return nil, err
		}
	}
	expiresAt := now.Add(s.refreshTokenTTL)
	session := &RefreshSession{
		UserID:    info.ID,
		Username:  info.Username,
		Role:      info.Role,
		FamilyID:  familyID,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	if err := s.refreshSessionStore.StoreRefreshSession(ctx, hashRefreshToken(refreshStr), session, s.refreshTokenTTL); err != nil {
		return nil, fmt.Errorf("store refresh session: %w", err)
	}
	return &TokenPair{
		AccessToken:  accessStr,
		RefreshToken: refreshStr,
		ExpiresIn:    int(s.accessTokenTTL.Seconds()),
	}, nil
}

func (s *Service) generateAccessToken(info *UserInfo, now time.Time) (string, error) {
	accessClaims := jwt.MapClaims{
		"user_id":  info.ID,
		"username": info.Username,
		"role":     info.Role,
		"type":     "access",
		"iat":      now.Unix(),
		"exp":      now.Add(s.accessTokenTTL).Unix(),
	}
	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessStr, err := accessToken.SignedString(s.jwtSecret)
	if err != nil {
		return "", fmt.Errorf("sign access token: %w", err)
	}
	return accessStr, nil
}
```

- [ ] **Step 5: Update tests that call `generateTokenPair` directly**

Because `generateTokenPair` now accepts `context.Context`, update direct calls in auth tests:

```go
pair, err := svc.generateTokenPair(context.Background(), info)
```

For tests that intentionally construct `Service` literals with expired access tokens, add memory store and `now`:

```go
svc := &Service{
	jwtSecret:       []byte("test-secret-key-for-unit-tests!!"),
	accessTokenTTL:  -1 * time.Hour,
	refreshTokenTTL: 7 * 24 * time.Hour,
	now:             time.Now,
}
svc.SetRefreshSessionStore(newMemoryRefreshSessionStore())
```

- [ ] **Step 6: Run targeted auth tests and verify they pass**

Run:

```bash
cd backend && go test ./internal/auth -run 'TestGenerateAndValidateAccessToken|TestRefreshToken|TestGenerateTokenPairForUser|TestRequireAuth' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/auth
```

- [ ] **Step 7: Commit auth token rotation**

Run:

```bash
git add backend/internal/auth/auth.go backend/internal/auth/auth_test.go backend/internal/auth/auth_service_test.go backend/internal/auth/middleware_test.go
git commit -m "feat(auth): rotate redis-backed refresh tokens"
```

## Task 3: Add Logout Revocation Endpoints

**Files:**
- Modify: `backend/internal/auth/auth.go`
- Modify: `backend/internal/handler/auth.go`
- Modify: `backend/internal/handler/router.go`
- Create: `backend/internal/handler/auth_test.go`
- Modify: `backend/internal/handler/handler_test.go`
- Modify: `backend/internal/handler/test_helpers_extra_test.go`

- [ ] **Step 1: Add service revoke methods**

In `backend/internal/auth/auth.go`, add:

```go
func (s *Service) RevokeRefreshToken(ctx context.Context, refreshToken string) error {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return nil
	}
	if !strings.HasPrefix(refreshToken, refreshTokenPrefix) {
		return fmt.Errorf("invalid refresh token")
	}
	if s.refreshSessionStore == nil {
		return fmt.Errorf("refresh session store is not configured")
	}
	return s.refreshSessionStore.DeleteRefreshSession(ctx, hashRefreshToken(refreshToken))
}

func (s *Service) RevokeUserRefreshSessions(ctx context.Context, userID int) error {
	if s.refreshSessionStore == nil {
		return fmt.Errorf("refresh session store is not configured")
	}
	return s.refreshSessionStore.DeleteUserRefreshSessions(ctx, userID)
}
```

- [ ] **Step 2: Write failing handler tests**

Create `backend/internal/handler/auth_test.go`:

```go
package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/testdb"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

func newAuthHandlerTestService(t *testing.T) (*auth.Service, *ent.Client, *AuthHandler) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	client := testdb.Open(t)
	svc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, zap.NewNop())
	svc.SetRefreshSessionStore(newHandlerTestRefreshSessionStore(t))
	return svc, client, NewAuthHandler(svc, client)
}

func newHandlerTestRefreshSessionStore(t *testing.T) auth.RefreshSessionStore {
	t.Helper()
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	return auth.NewRedisRefreshSessionStore(redisClient)
}

func createAuthHandlerTestUser(t *testing.T, client *ent.Client, username string) *ent.User {
	t.Helper()
	u, err := client.User.Create().
		SetUsername(username).
		SetEmail(username + "@test.local").
		SetAuthSource(entuser.AuthSourceLdap).
		SetRole(entuser.RoleUser).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

func TestLogoutRevokesRefreshTokenAndReturnsOK(t *testing.T) {
	svc, client, h := newAuthHandlerTestService(t)
	u := createAuthHandlerTestUser(t, client, "alice")

	pair, err := svc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:         u.ID,
		Username:   u.Username,
		Email:      u.Email,
		Role:       string(u.Role),
		AuthSource: string(u.AuthSource),
	})
	if err != nil {
		t.Fatalf("GenerateTokenPairForUser: %v", err)
	}

	r := gin.New()
	r.POST("/api/v1/auth/logout", h.Logout)
	body := bytes.NewBufferString(`{"refresh_token":"` + pair.RefreshToken + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, _, err := svc.RefreshToken(req.Context(), pair.RefreshToken); err == nil {
		t.Fatal("revoked refresh token should no longer refresh")
	}
}

func TestLogoutAllRevokesAuthenticatedUserSessions(t *testing.T) {
	svc, client, h := newAuthHandlerTestService(t)
	u := createAuthHandlerTestUser(t, client, "bob")

	pair, err := svc.GenerateTokenPairForUser(&auth.UserInfo{
		ID:         u.ID,
		Username:   u.Username,
		Email:      u.Email,
		Role:       string(u.Role),
		AuthSource: string(u.AuthSource),
	})
	if err != nil {
		t.Fatalf("GenerateTokenPairForUser: %v", err)
	}

	r := gin.New()
	r.POST("/api/v1/auth/logout-all", auth.RequireAuth(svc), h.LogoutAll)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout-all", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if _, _, err := svc.RefreshToken(req.Context(), pair.RefreshToken); err == nil {
		t.Fatal("logout-all should revoke the user's refresh token")
	}
}
```

- [ ] **Step 3: Update existing handler test environments to configure refresh storage**

In `backend/internal/handler/handler_test.go`, after the existing `authSvc := auth.NewService(...)` line inside `setupTestEnvWithOAuth`, add:

```go
authSvc.SetRefreshSessionStore(newHandlerTestRefreshSessionStore(t))
```

The resulting block should be:

```go
logger := zap.NewNop()
authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
authSvc.SetRefreshSessionStore(newHandlerTestRefreshSessionStore(t))
repoSvc := repo.NewService(client, "0000000000000000000000000000000000000000000000000000000000000000", logger)
```

In `backend/internal/handler/test_helpers_extra_test.go`, after the existing `authSvc := auth.NewService(...)` line inside `setupMockTestEnv`, add the same store setup:

```go
authSvc := auth.NewService(client, "test-jwt-secret-32-bytes-long!!!", 7200, 604800, logger)
authSvc.SetRefreshSessionStore(newHandlerTestRefreshSessionStore(t))
```

- [ ] **Step 4: Run handler tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestLogout' -count=1
```

Expected:

```text
FAIL
h.Logout undefined
h.LogoutAll undefined
```

- [ ] **Step 5: Implement logout handlers**

In `backend/internal/handler/auth.go`, add:

```go
// Logout handles POST /api/v1/auth/logout.
func (h *AuthHandler) Logout(c *gin.Context) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&req)
	if strings.TrimSpace(req.RefreshToken) != "" {
		if err := h.authService.RevokeRefreshToken(c.Request.Context(), req.RefreshToken); err != nil {
			pkg.Error(c, http.StatusUnauthorized, err.Error())
			return
		}
	}
	pkg.Success(c, gin.H{"status": "logged_out"})
}

// LogoutAll handles POST /api/v1/auth/logout-all.
func (h *AuthHandler) LogoutAll(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "not authenticated")
		return
	}
	if err := h.authService.RevokeUserRefreshSessions(c.Request.Context(), uc.UserID); err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, gin.H{"status": "logged_out_all"})
}
```

Add `strings` to the imports.

In `backend/internal/handler/router.go`, register:

```go
authGroup.POST("/logout", authHandler.Logout)
authGroup.POST("/logout-all", auth.RequireAuth(authService), authHandler.LogoutAll)
```

- [ ] **Step 6: Run logout tests and verify they pass**

Run:

```bash
cd backend && go test ./internal/handler -run 'TestLogout' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/handler
```

- [ ] **Step 7: Commit logout endpoints**

Run:

```bash
git add backend/internal/auth/auth.go backend/internal/handler/auth.go backend/internal/handler/auth_test.go backend/internal/handler/handler_test.go backend/internal/handler/test_helpers_extra_test.go backend/internal/handler/router.go
git commit -m "feat(auth): add refresh token logout revocation"
```

## Task 4: Wire Redis Refresh Store in Server Startup

**Files:**
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Wire the store after Redis client creation**

In `backend/cmd/server/main.go`, after `authService := auth.NewService(...)`, add:

```go
authService.SetRefreshSessionStore(auth.NewRedisRefreshSessionStore(redisClient))
```

The resulting block should look like:

```go
authService := auth.NewService(
	entClient,
	cfg.Auth.JWTSecret,
	cfg.Auth.AccessTokenTTL,
	cfg.Auth.RefreshTokenTTL,
	logger,
	cfg.Encryption.Key,
)
authService.SetRefreshSessionStore(auth.NewRedisRefreshSessionStore(redisClient))
```

- [ ] **Step 2: Build the server package**

Run:

```bash
cd backend && go test ./cmd/server -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/cmd/server
```

- [ ] **Step 3: Commit server wiring**

Run:

```bash
git add backend/cmd/server/main.go
git commit -m "chore(server): wire redis refresh session store"
```

## Task 5: Add Redis OAuth State Store

**Files:**
- Create: `backend/internal/oauth/state_store.go`
- Create: `backend/internal/oauth/state_store_test.go`

- [ ] **Step 1: Write failing OAuth state-store tests**

Create `backend/internal/oauth/state_store_test.go`:

```go
package oauth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestOAuthStateStore(t *testing.T) OAuthStateStore {
	t.Helper()
	s := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: s.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	return NewRedisStateStore(rdb)
}

func TestRedisStateStoreConsumesAuthorizationCodeOnce(t *testing.T) {
	ctx := context.Background()
	store := newTestOAuthStateStore(t)
	session := &AuthorizationCodeSession{
		Code:          "code-1",
		ClientID:      "ae-cli",
		RedirectURI:   "http://localhost:18234/callback",
		CodeChallenge: "challenge",
		UserID:        7,
		Username:      "alice",
		Role:          "user",
		State:         "state-1",
		CreatedAt:     time.Now().UTC(),
	}

	if err := store.StoreAuthorizationCode(ctx, session, time.Minute); err != nil {
		t.Fatalf("StoreAuthorizationCode: %v", err)
	}
	got, err := store.ConsumeAuthorizationCode(ctx, "code-1")
	if err != nil {
		t.Fatalf("ConsumeAuthorizationCode: %v", err)
	}
	if got.UserID != 7 || got.Username != "alice" {
		t.Fatalf("session mismatch: %#v", got)
	}
	if _, err := store.ConsumeAuthorizationCode(ctx, "code-1"); !errors.Is(err, ErrOAuthStateNotFound) {
		t.Fatalf("second consume err=%v, want ErrOAuthStateNotFound", err)
	}
}

func TestRedisStateStoreDeviceFlowSharedAcrossInstances(t *testing.T) {
	ctx := context.Background()
	store := newTestOAuthStateStore(t)
	device := &DeviceSession{
		DeviceCode:      "device-1",
		UserCode:        "ABCD-EFGH",
		NormalizedCode:  "ABCDEFGH",
		ClientID:        "ae-cli",
		Status:          deviceStatusPending,
		CreatedAt:       time.Now().UTC(),
		ExpiresAt:       time.Now().Add(15 * time.Minute).UTC(),
		PollIntervalSec: 5,
	}

	if err := store.StoreDeviceSession(ctx, device, 15*time.Minute); err != nil {
		t.Fatalf("StoreDeviceSession: %v", err)
	}
	byUserCode, err := store.GetDeviceSessionByUserCode(ctx, "abcd-efgh")
	if err != nil {
		t.Fatalf("GetDeviceSessionByUserCode: %v", err)
	}
	byUserCode.Status = deviceStatusApproved
	byUserCode.UserID = 11
	byUserCode.Username = "bob"
	byUserCode.Role = "user"
	if err := store.UpdateDeviceSession(ctx, byUserCode, 15*time.Minute); err != nil {
		t.Fatalf("UpdateDeviceSession: %v", err)
	}

	byDevice, err := store.GetDeviceSession(ctx, "device-1")
	if err != nil {
		t.Fatalf("GetDeviceSession: %v", err)
	}
	if byDevice.Status != deviceStatusApproved || byDevice.UserID != 11 {
		t.Fatalf("device state mismatch: %#v", byDevice)
	}

	consumed, err := store.ConsumeDeviceSession(ctx, "device-1")
	if err != nil {
		t.Fatalf("ConsumeDeviceSession: %v", err)
	}
	if consumed.Username != "bob" {
		t.Fatalf("consumed username=%q", consumed.Username)
	}
	if _, err := store.GetDeviceSessionByUserCode(ctx, "ABCD-EFGH"); !errors.Is(err, ErrOAuthStateNotFound) {
		t.Fatalf("user code after consume err=%v, want ErrOAuthStateNotFound", err)
	}
}
```

- [ ] **Step 2: Run state-store tests and verify they fail**

Run:

```bash
cd backend && go test ./internal/oauth -run 'TestRedisStateStore' -count=1
```

Expected:

```text
FAIL
undefined: OAuthStateStore
undefined: NewRedisStateStore
```

- [ ] **Step 3: Implement OAuth state store**

Create `backend/internal/oauth/state_store.go`:

```go
package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	oauthCodeKeyPrefix       = "ai-efficiency:oauth:code:"
	oauthDeviceKeyPrefix     = "ai-efficiency:oauth:device:"
	oauthUserCodeKeyPrefix   = "ai-efficiency:oauth:user_code:"
)

var ErrOAuthStateNotFound = errors.New("oauth state not found")

type AuthorizationCodeSession struct {
	Code          string    `json:"code"`
	ClientID      string    `json:"client_id"`
	RedirectURI   string    `json:"redirect_uri"`
	CodeChallenge string    `json:"code_challenge"`
	UserID        int       `json:"user_id"`
	Username      string    `json:"username"`
	Role          string    `json:"role"`
	State         string    `json:"state"`
	CreatedAt     time.Time `json:"created_at"`
}

type DeviceSession struct {
	DeviceCode      string    `json:"device_code"`
	UserCode        string    `json:"user_code"`
	NormalizedCode  string    `json:"normalized_code"`
	ClientID        string    `json:"client_id"`
	Status          string    `json:"status"`
	UserID          int       `json:"user_id"`
	Username        string    `json:"username"`
	Role            string    `json:"role"`
	CreatedAt       time.Time `json:"created_at"`
	ExpiresAt       time.Time `json:"expires_at"`
	LastPolledAt    time.Time `json:"last_polled_at"`
	PollIntervalSec int       `json:"poll_interval_sec"`
}

type OAuthStateStore interface {
	StoreAuthorizationCode(ctx context.Context, session *AuthorizationCodeSession, ttl time.Duration) error
	ConsumeAuthorizationCode(ctx context.Context, code string) (*AuthorizationCodeSession, error)
	StoreDeviceSession(ctx context.Context, session *DeviceSession, ttl time.Duration) error
	GetDeviceSession(ctx context.Context, deviceCode string) (*DeviceSession, error)
	GetDeviceSessionByUserCode(ctx context.Context, userCode string) (*DeviceSession, error)
	UpdateDeviceSession(ctx context.Context, session *DeviceSession, ttl time.Duration) error
	ConsumeDeviceSession(ctx context.Context, deviceCode string) (*DeviceSession, error)
	UserCodeExists(ctx context.Context, normalizedUserCode string) (bool, error)
}

type RedisStateStore struct {
	rdb *redis.Client
}

func NewRedisStateStore(rdb *redis.Client) *RedisStateStore {
	return &RedisStateStore{rdb: rdb}
}

func oauthHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])
}

func oauthCodeKey(code string) string {
	return oauthCodeKeyPrefix + oauthHash(code)
}

func oauthDeviceKey(deviceCode string) string {
	return oauthDeviceKeyPrefix + oauthHash(deviceCode)
}

func oauthUserCodeKey(normalizedUserCode string) string {
	return oauthUserCodeKeyPrefix + normalizeUserCode(normalizedUserCode)
}

func (s *RedisStateStore) StoreAuthorizationCode(ctx context.Context, session *AuthorizationCodeSession, ttl time.Duration) error {
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal authorization code: %w", err)
	}
	return s.rdb.Set(ctx, oauthCodeKey(session.Code), payload, ttl).Err()
}

func (s *RedisStateStore) ConsumeAuthorizationCode(ctx context.Context, code string) (*AuthorizationCodeSession, error) {
	payload, err := s.rdb.GetDel(ctx, oauthCodeKey(code)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrOAuthStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("consume authorization code: %w", err)
	}
	var session AuthorizationCodeSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("unmarshal authorization code: %w", err)
	}
	return &session, nil
}

func (s *RedisStateStore) StoreDeviceSession(ctx context.Context, session *DeviceSession, ttl time.Duration) error {
	if session.NormalizedCode == "" {
		session.NormalizedCode = normalizeUserCode(session.UserCode)
	}
	payload, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal device session: %w", err)
	}
	pipe := s.rdb.Pipeline()
	pipe.Set(ctx, oauthDeviceKey(session.DeviceCode), payload, ttl)
	pipe.Set(ctx, oauthUserCodeKey(session.NormalizedCode), session.DeviceCode, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (s *RedisStateStore) GetDeviceSession(ctx context.Context, deviceCode string) (*DeviceSession, error) {
	payload, err := s.rdb.Get(ctx, oauthDeviceKey(deviceCode)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrOAuthStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device session: %w", err)
	}
	var session DeviceSession
	if err := json.Unmarshal(payload, &session); err != nil {
		return nil, fmt.Errorf("unmarshal device session: %w", err)
	}
	return &session, nil
}

func (s *RedisStateStore) GetDeviceSessionByUserCode(ctx context.Context, userCode string) (*DeviceSession, error) {
	deviceCode, err := s.rdb.Get(ctx, oauthUserCodeKey(userCode)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, ErrOAuthStateNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device code by user code: %w", err)
	}
	return s.GetDeviceSession(ctx, deviceCode)
}

func (s *RedisStateStore) UpdateDeviceSession(ctx context.Context, session *DeviceSession, ttl time.Duration) error {
	return s.StoreDeviceSession(ctx, session, ttl)
}

func (s *RedisStateStore) ConsumeDeviceSession(ctx context.Context, deviceCode string) (*DeviceSession, error) {
	session, err := s.GetDeviceSession(ctx, deviceCode)
	if err != nil {
		return nil, err
	}
	pipe := s.rdb.Pipeline()
	pipe.Del(ctx, oauthDeviceKey(deviceCode))
	pipe.Del(ctx, oauthUserCodeKey(session.NormalizedCode))
	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("consume device session: %w", err)
	}
	return session, nil
}

func (s *RedisStateStore) UserCodeExists(ctx context.Context, normalizedUserCode string) (bool, error) {
	count, err := s.rdb.Exists(ctx, oauthUserCodeKey(normalizedUserCode)).Result()
	return count > 0, err
}
```

- [ ] **Step 4: Run OAuth state-store tests**

Run:

```bash
cd backend && go test ./internal/oauth -run 'TestRedisStateStore' -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/oauth
```

- [ ] **Step 5: Commit OAuth state store**

Run:

```bash
git add backend/internal/oauth/state_store.go backend/internal/oauth/state_store_test.go
git commit -m "feat(oauth): add redis oauth state store"
```

## Task 6: Refactor OAuth Handler to Use State Store

**Files:**
- Modify: `backend/internal/oauth/handler.go`
- Modify: `backend/internal/oauth/handler_test.go`
- Modify: `backend/internal/oauth/device_internal_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Update handler constructor and tests to pass a store**

Change `NewHandler` signature in `backend/internal/oauth/handler.go`:

```go
func NewHandler(server *Server, frontendURL string, tokenGen TokenGenerator, stateStore OAuthStateStore) *Handler
```

Update test setup helpers to pass `newTestOAuthStateStore(t)`:

```go
handler := NewHandler(NewServer(), "http://localhost:5173", deviceTokenGen{}, newTestOAuthStateStore(t))
```

Update `setupTestRouter()` in `handler_test.go` to accept `t *testing.T`:

```go
func setupTestRouter(t *testing.T) (*gin.Engine, *oauth.Handler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	oauthServer := oauth.NewServer()
	handler := oauth.NewHandler(oauthServer, "http://localhost:5173", &mockTokenGen{}, oauth.NewTestStateStore(t))
	// route setup stays the same
}
```

If the helper lives in external package `oauth_test`, expose a test helper through regular constructor calls to `oauth.NewRedisStateStore` with miniredis in that test file instead of using unexported helpers.

- [ ] **Step 2: Write cross-handler device-flow test**

Add this test to `backend/internal/oauth/device_internal_test.go`:

```go
func TestDeviceFlowStateSurvivesAcrossHandlersSharingStore(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	store := newTestOAuthStateStore(t)
	issuer := NewHandler(NewServer(), "http://localhost:5173", deviceTokenGen{}, store)
	issuer.now = func() time.Time { return now }
	verifier := NewHandler(NewServer(), "http://localhost:5173", deviceTokenGen{}, store)
	verifier.now = func() time.Time { return now.Add(6 * time.Second) }

	r := gin.New()
	r.POST("/issue/oauth/device/code", issuer.DeviceCode)
	verifyGroup := r.Group("/verify/oauth")
	verifyGroup.Use(func(c *gin.Context) {
		c.Set(authpkg.ContextKeyUser, &authpkg.UserContext{UserID: 7, Username: "alice", Role: "user"})
		c.Next()
	})
	verifyGroup.POST("/device/verify", verifier.VerifyDevice)
	r.POST("/verify/oauth/token", verifier.Token)

	payload, deviceCode := issueDeviceCodeAtPath(t, r, "/issue/oauth/device/code")
	body := bytes.NewBufferString(`{"user_code":"` + payload["user_code"].(string) + `","approved":true}`)
	verifyReq := httptest.NewRequest(http.MethodPost, "/verify/oauth/device/verify", body)
	verifyReq.Header.Set("Content-Type", "application/json")
	verifyW := httptest.NewRecorder()
	r.ServeHTTP(verifyW, verifyReq)
	if verifyW.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyW.Code, verifyW.Body.String())
	}

	tokenBody := url.Values{
		"grant_type":  {deviceGrantType},
		"device_code": {deviceCode},
		"client_id":   {"ae-cli"},
	}
	tokenReq := httptest.NewRequest(http.MethodPost, "/verify/oauth/token", strings.NewReader(tokenBody.Encode()))
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenW := httptest.NewRecorder()
	r.ServeHTTP(tokenW, tokenReq)
	if !strings.Contains(tokenW.Body.String(), "device-access-token") {
		t.Fatalf("token body=%s", tokenW.Body.String())
	}
}
```

Add helper:

```go
func issueDeviceCodeAtPath(t *testing.T, router *gin.Engine, path string) (map[string]any, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("client_id=ae-cli"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("device code status=%d body=%s", w.Code, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode device code: %v", err)
	}
	return payload, payload["device_code"].(string)
}
```

- [ ] **Step 3: Run OAuth tests and verify handler still depends on maps**

Run:

```bash
cd backend && go test ./internal/oauth -run 'TestDeviceFlowStateSurvivesAcrossHandlersSharingStore|TestTokenDeviceGrant' -count=1
```

Expected:

```text
FAIL
```

The cross-handler test should fail until state is read from the shared store.

- [ ] **Step 4: Replace handler maps with store calls**

In `backend/internal/oauth/handler.go`, remove `codes`, `devices`, `devicesByUserCode`, and `reapExpiredCodes` from `Handler`. The struct should contain:

```go
type Handler struct {
	server             *Server
	frontendURL        string
	tokenGen           TokenGenerator
	stateStore         OAuthStateStore
	now                func() time.Time
	deviceCodeExpiry   time.Duration
	devicePollInterval time.Duration
}
```

In `Approve`, replace map write with:

```go
session := &AuthorizationCodeSession{
	Code:          code,
	ClientID:      req.ClientID,
	RedirectURI:   req.RedirectURI,
	CodeChallenge: req.CodeChallenge,
	UserID:        uid,
	Username:      username,
	Role:          role,
	State:         req.State,
	CreatedAt:     h.now(),
}
if err := h.stateStore.StoreAuthorizationCode(c.Request.Context(), session, codeExpiry); err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
	return
}
```

In `exchangeAuthorizationCode`, replace map consume with:

```go
entry, err := h.stateStore.ConsumeAuthorizationCode(c.Request.Context(), code)
if err != nil {
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "code not found or already used"})
	return
}
```

In `DeviceCode`, replace user-code uniqueness and map writes with:

```go
userCode, normalizedUserCode, err := h.issueUniqueUserCode(c.Request.Context())
if err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
	return
}
session := &DeviceSession{
	DeviceCode:      deviceCode,
	UserCode:        userCode,
	NormalizedCode:  normalizedUserCode,
	ClientID:        clientID,
	Status:          deviceStatusPending,
	CreatedAt:       now,
	ExpiresAt:       now.Add(h.deviceCodeExpiry),
	PollIntervalSec: int(h.devicePollInterval / time.Second),
}
if err := h.stateStore.StoreDeviceSession(c.Request.Context(), session, h.deviceCodeExpiry); err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
	return
}
```

Replace `findDeviceByUserCodeLocked`, `isDeviceExpiredLocked`, and `issueUniqueUserCodeLocked` with:

```go
func (h *Handler) isDeviceExpired(entry *DeviceSession) bool {
	return !h.now().Before(entry.ExpiresAt)
}

func (h *Handler) issueUniqueUserCode(ctx context.Context) (string, string, error) {
	for range 8 {
		userCode, err := generateUserCodeFunc()
		if err != nil {
			return "", "", err
		}
		normalized := normalizeUserCode(userCode)
		exists, err := h.stateStore.UserCodeExists(ctx, normalized)
		if err != nil {
			return "", "", err
		}
		if exists {
			continue
		}
		return userCode, normalized, nil
	}
	return "", "", errRandomRead
}
```

In `VerifyDevice`, read and update through the store:

```go
entry, err := h.stateStore.GetDeviceSessionByUserCode(c.Request.Context(), req.UserCode)
if err != nil || entry.Status == deviceStatusConsumed || h.isDeviceExpired(entry) {
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_user_code", "message": "Code invalid or expired"})
	return
}
if entry.Status != deviceStatusPending {
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_user_code", "message": "Code invalid or expired"})
	return
}
if !req.Approved {
	entry.Status = deviceStatusDenied
	entry.LastPolledAt = time.Time{}
	_ = h.stateStore.UpdateDeviceSession(c.Request.Context(), entry, time.Until(entry.ExpiresAt))
	c.JSON(http.StatusOK, gin.H{"status": deviceStatusDenied})
	return
}
entry.Status = deviceStatusApproved
entry.LastPolledAt = time.Time{}
entry.UserID = uc.UserID
entry.Username = uc.Username
entry.Role = uc.Role
if err := h.stateStore.UpdateDeviceSession(c.Request.Context(), entry, time.Until(entry.ExpiresAt)); err != nil {
	c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
	return
}
c.JSON(http.StatusOK, gin.H{"status": deviceStatusApproved})
```

In `exchangeDeviceToken`, get/update/consume through the store:

```go
entry, err := h.stateStore.GetDeviceSession(c.Request.Context(), deviceCode)
if err != nil || entry.ClientID != clientID || entry.Status == deviceStatusConsumed {
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
	return
}
if h.isDeviceExpired(entry) {
	entry.Status = deviceStatusExpired
	_ = h.stateStore.UpdateDeviceSession(c.Request.Context(), entry, time.Second)
	c.JSON(http.StatusBadRequest, gin.H{"error": "expired_token"})
	return
}
if !entry.LastPolledAt.IsZero() && h.now().Sub(entry.LastPolledAt) < time.Duration(entry.PollIntervalSec)*time.Second {
	entry.LastPolledAt = h.now()
	_ = h.stateStore.UpdateDeviceSession(c.Request.Context(), entry, time.Until(entry.ExpiresAt))
	c.JSON(http.StatusBadRequest, gin.H{"error": "slow_down"})
	return
}
entry.LastPolledAt = h.now()

switch entry.Status {
case deviceStatusPending:
	_ = h.stateStore.UpdateDeviceSession(c.Request.Context(), entry, time.Until(entry.ExpiresAt))
	c.JSON(http.StatusBadRequest, gin.H{"error": "authorization_pending"})
	return
case deviceStatusDenied:
	c.JSON(http.StatusBadRequest, gin.H{"error": "access_denied"})
	return
case deviceStatusApproved:
	consumed, err := h.stateStore.ConsumeDeviceSession(c.Request.Context(), deviceCode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}
	entry = consumed
default:
	c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
	return
}
```

- [ ] **Step 5: Wire Redis OAuth state store in server startup**

In `backend/cmd/server/main.go`, change:

```go
oauthHandler := oauth.NewHandler(oauthServer, cfg.Server.FrontendURL, &authTokenAdapter{authService: authService})
```

to:

```go
oauthHandler := oauth.NewHandler(
	oauthServer,
	cfg.Server.FrontendURL,
	&authTokenAdapter{authService: authService},
	oauth.NewRedisStateStore(redisClient),
)
```

- [ ] **Step 6: Run OAuth tests and server package tests**

Run:

```bash
cd backend && go test ./internal/oauth ./cmd/server -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/oauth
ok  	github.com/ai-efficiency/backend/cmd/server
```

- [ ] **Step 7: Commit OAuth Redis state refactor**

Run:

```bash
git add backend/internal/oauth/handler.go backend/internal/oauth/handler_test.go backend/internal/oauth/device_internal_test.go backend/cmd/server/main.go
git commit -m "feat(oauth): persist authorization state in redis"
```

## Task 7: Update Frontend Logout and Refresh Compatibility

**Files:**
- Modify: `frontend/src/api/auth.ts`
- Modify: `frontend/src/stores/auth.ts`
- Modify: `frontend/src/api/client.ts`
- Modify: `frontend/src/__tests__/auth-store.test.ts`
- Modify: `frontend/src/__tests__/client.test.ts`

- [ ] **Step 1: Add logout API helper**

In `frontend/src/api/auth.ts`, add:

```ts
export function logout(refreshToken?: string | null) {
  return client.post<ApiResponse<{ status: string }>>('/auth/logout', {
    refresh_token: refreshToken || '',
  })
}
```

- [ ] **Step 2: Update auth store logout flow**

In `frontend/src/stores/auth.ts`, update imports:

```ts
import { login as apiLogin, getMe, logout as apiLogout } from '@/api/auth'
```

Replace `logout()` with:

```ts
async function logout() {
  const refreshToken = localStorage.getItem('refresh_token')
  try {
    if (refreshToken) {
      await apiLogout(refreshToken)
    }
  } catch {
    // Local logout must still complete if server-side revocation fails.
  } finally {
    token.value = null
    user.value = null
    localStorage.removeItem('token')
    localStorage.removeItem('refresh_token')
  }
}
```

Update `fetchMe()` to await logout:

```ts
if (error?.response?.status === 401) {
  await logout()
}
```

- [ ] **Step 3: Confirm refresh retry already persists rotated refresh token**

`frontend/src/api/client.ts` already contains:

```ts
const nextRefreshToken = data?.tokens?.refresh_token || data?.refresh_token || currentRefreshToken
localStorage.setItem('refresh_token', nextRefreshToken)
```

Keep this logic. If tests fail because refresh token is missing in mocked response, update the mock to include `tokens.refresh_token`.

- [ ] **Step 4: Add frontend logout tests**

In `frontend/src/__tests__/auth-store.test.ts`, update the auth API mock to include `logout`, then add:

```ts
it('logout revokes refresh token then clears local state', async () => {
  const { logout } = await import('@/api/auth')
  ;(logout as any).mockResolvedValue({ data: { data: { status: 'logged_out' } } })
  localStorage.setItem('token', 'access-token')
  localStorage.setItem('refresh_token', 'refresh-token')
  const store = useAuthStore()
  store.token = 'access-token'

  await store.logout()

  expect(logout).toHaveBeenCalledWith('refresh-token')
  expect(store.token).toBeNull()
  expect(localStorage.getItem('token')).toBeNull()
  expect(localStorage.getItem('refresh_token')).toBeNull()
})

it('logout clears local state even when revocation fails', async () => {
  const { logout } = await import('@/api/auth')
  ;(logout as any).mockRejectedValue(new Error('network'))
  localStorage.setItem('token', 'access-token')
  localStorage.setItem('refresh_token', 'refresh-token')
  const store = useAuthStore()
  store.token = 'access-token'

  await store.logout()

  expect(localStorage.getItem('token')).toBeNull()
  expect(localStorage.getItem('refresh_token')).toBeNull()
})
```

- [ ] **Step 5: Run frontend tests**

Run:

```bash
cd frontend && pnpm test -- auth-store client
```

Expected:

```text
PASS
```

- [ ] **Step 6: Commit frontend compatibility**

Run:

```bash
git add frontend/src/api/auth.ts frontend/src/stores/auth.ts frontend/src/api/client.ts frontend/src/__tests__/auth-store.test.ts frontend/src/__tests__/client.test.ts
git commit -m "feat(frontend): revoke refresh token on logout"
```

## Task 8: Document Redis Auth-Critical Deployment Contract

**Files:**
- Modify: `deploy/config.example.yaml`
- Modify: `deploy/docker-compose.yml`
- Modify: `deploy/docker-compose.dev.yml`
- Modify: Helm chart `ai-efficiency/docs/deploy.md`
- Modify: Helm chart `ai-efficiency/values.yaml` only if changing existing TTL env values

- [ ] **Step 1: Update repo deploy docs and compose comments**

In `deploy/config.example.yaml`, change the Redis section comment to:

```yaml
# Redis is auth-critical. It stores refresh-session metadata and OAuth
# authorization/device-flow state in addition to health/readiness checks.
# Production releases must keep this Redis data available across pod restarts.
redis:
  addr: "localhost:6379"
  password: ""
  db: 0
```

In `deploy/docker-compose.yml` and `deploy/docker-compose.dev.yml`, update the Redis service comment:

```yaml
  redis:
    # Stores auth refresh-session metadata and short-lived OAuth state.
    image: redis:7-alpine
```

- [ ] **Step 2: Update Helm deploy docs without adding Redis config**

In Helm chart `ai-efficiency/docs/deploy.md`, add a section:

```markdown
## Redis Auth State

The chart does not create Redis. `ai-efficiency` uses the existing `AE_REDIS_ADDR`,
`AE_REDIS_PASSWORD`, and `AE_REDIS_DB` values for auth-critical state:

- refresh-session metadata keyed under `ai-efficiency:auth:*`
- OAuth authorization-code and device-flow state keyed under `ai-efficiency:oauth:*`

Production may point these values at the same Alibaba Cloud Redis instance used by
sub2api. Isolation is by key prefix, not by a separate Redis database or a second
configuration surface.

`AE_AUTH_JWT_SECRET` must stay stable across normal releases. Rotating it is an
explicit security operation that invalidates outstanding access tokens.
```

- [ ] **Step 3: Confirm Helm values keep the existing Redis surface**

Run:

```bash
HELM_REPO_ROOT="${HELM_REPO_ROOT:-../helm}"
rg -n 'AE_REDIS_|AE_AUTH_ACCESS_TOKEN_TTL|AE_AUTH_REFRESH_TOKEN_TTL' "$HELM_REPO_ROOT/ai-efficiency"
```

Expected:

```text
values.yaml contains AE_REDIS_ADDR, AE_REDIS_PASSWORD, AE_REDIS_DB
```

Do not add a new Redis address, password, username, TLS, or DB field in this task.

- [ ] **Step 4: Commit deployment docs**

Run:

```bash
git add deploy/config.example.yaml deploy/docker-compose.yml deploy/docker-compose.dev.yml
git commit -m "docs(deploy): document redis-backed auth state"

HELM_REPO_ROOT="${HELM_REPO_ROOT:-../helm}"
git -C "$HELM_REPO_ROOT" add ai-efficiency/docs/deploy.md
git -C "$HELM_REPO_ROOT" commit -m "docs(ai-efficiency): document redis auth state"
```

## Task 9: Full Verification and Release Readiness

**Files:**
- No source files should be edited in this task unless a verification failure points to a concrete bug from earlier tasks.

- [ ] **Step 1: Run backend auth, OAuth, handler, and server tests**

Run:

```bash
cd backend && go test ./internal/auth ./internal/oauth ./internal/handler ./cmd/server -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/internal/auth
ok  	github.com/ai-efficiency/backend/internal/oauth
ok  	github.com/ai-efficiency/backend/internal/handler
ok  	github.com/ai-efficiency/backend/cmd/server
```

- [ ] **Step 2: Run wider backend tests with the known local Postgres override when local services are available**

Run:

```bash
cd backend && AE_TEST_POSTGRES_DSN='postgres://postgres:postgres@127.0.0.1:15432/postgres?sslmode=disable' go test ./... -count=1
```

Expected:

```text
ok  	github.com/ai-efficiency/backend/...
```

If local Postgres is not available, record the exact connection error in the plan before leaving this checkbox unchecked.

- [ ] **Step 3: Run frontend tests**

Run:

```bash
cd frontend && pnpm test
```

Expected:

```text
PASS
```

- [ ] **Step 4: Run manual Web restart validation**

Run the app with the existing dev compose stack, then validate:

```text
1. Log in through the Web UI.
2. Confirm localStorage has both token and refresh_token keys without copying their values.
3. Restart the backend container or pod.
4. Force an authenticated API request after the access token expires, or temporarily lower access TTL in a local-only run.
5. Confirm the frontend refreshes successfully and remains logged in.
6. Confirm Redis contains keys matching ai-efficiency:auth:* and no raw refresh token value is visible as a key.
```

- [ ] **Step 5: Run manual OAuth device-flow restart validation**

Validate:

```text
1. Start `ae-cli login --device`.
2. Confirm the CLI prints a user code.
3. Restart the backend process after the device code is issued.
4. Open `/oauth/device` in the browser, enter the user code, and approve.
5. Confirm the CLI polling completes and writes `~/.ae-cli/token.json`.
6. Confirm Redis contains `ai-efficiency:oauth:*` only during the flow and those keys disappear after consume or TTL.
```

- [ ] **Step 6: Commit verification notes if docs changed**

If verification adds notes to the plan, commit them:

```bash
git add docs/superpowers/plans/2026-05-25-redis-backed-auth-session.md
git commit -m "docs(plans): record redis auth verification status"
```

## Plan Self-Review

Spec coverage:

- Redis-backed refresh sessions: Task 1 and Task 2.
- Refresh rotation and old-token rejection: Task 2.
- Logout and logout-all revocation: Task 3.
- Existing `AE_REDIS_*` reuse with no new Redis config: Task 8.
- OAuth authorization/device state outside process memory: Task 5 and Task 6.
- Frontend refresh/logout compatibility: Task 7.
- Deployment and manual restart verification: Task 8 and Task 9.

Placeholder scan:

- Reserved placeholder phrases are absent from task instructions.
- Each task includes exact files, concrete commands, expected output, and a commit point.

Type consistency:

- Refresh-session store names use `RefreshSessionStore`, `RefreshSession`, `StoreRefreshSession`, `GetRefreshSession`, `DeleteRefreshSession`, `DeleteUserRefreshSessions`, and `DeleteRefreshTokenFamily` consistently.
- OAuth state names use `OAuthStateStore`, `AuthorizationCodeSession`, `DeviceSession`, `StoreAuthorizationCode`, `ConsumeAuthorizationCode`, `StoreDeviceSession`, `GetDeviceSession`, `GetDeviceSessionByUserCode`, `UpdateDeviceSession`, and `ConsumeDeviceSession` consistently.
