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
	if ok, err := mr.SIsMember(userRefreshSessionsKey(7), hash); err != nil || !ok {
		t.Fatal("user refresh-token set missing token hash")
	}
	if ok, err := mr.SIsMember(refreshFamilyKey("family-a"), hash); err != nil || !ok {
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
