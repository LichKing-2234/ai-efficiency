package representativescope

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestScopeVersionChangesWithAuthoritativeGuardDimensions(t *testing.T) {
	base := scopeGuard{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}
	baseVersion := scopeVersion(base)
	if baseVersion == "" {
		t.Fatal("scopeVersion() = empty, want opaque version")
	}
	if got := scopeVersion(base); got != baseVersion {
		t.Fatalf("scopeVersion() = %q, want deterministic %q", got, baseVersion)
	}

	variants := []scopeGuard{
		{ActorUserID: 8, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11},
		{ActorUserID: 7, ActorRole: "admin", DirectorySourceID: 3, DirectoryRunID: 11},
		{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 4, DirectoryRunID: 11},
		{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 12},
	}
	for _, guard := range variants {
		if got := scopeVersion(guard); got == baseVersion {
			t.Fatalf("scopeVersion(%+v) reused base version %q", guard, got)
		}
	}
}

func TestScopeCacheReusesVersionedScopeAndPreservesDepartmentData(t *testing.T) {
	cache, server := testScopeCache(t, "test")
	guard := scopeGuard{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}
	want := testCachedScope(guard.ActorUserID, "department-alpha", "department-beta")
	var loads atomic.Int32
	loader := func(context.Context) (*Scope, error) {
		loads.Add(1)
		return want, nil
	}

	first, err := cache.GetOrLoad(context.Background(), guard, loader)
	if err != nil {
		t.Fatalf("first GetOrLoad: %v", err)
	}
	second, err := cache.GetOrLoad(context.Background(), guard, loader)
	if err != nil {
		t.Fatalf("second GetOrLoad: %v", err)
	}
	wantVersion := scopeVersion(guard)
	if first.Version != wantVersion || second.Version != wantVersion {
		t.Fatalf("scope versions = %q/%q, want %q", first.Version, second.Version, wantVersion)
	}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Fatalf("cached scope mismatch: first=%#v second=%#v want=%#v", first, second, want)
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1", got)
	}
	key := scopeCacheKey("test", guard)
	if !server.Exists(key) {
		t.Fatalf("cache key %q was not written", key)
	}
	if ttl := server.TTL(key); ttl < 48*time.Minute || ttl > 54*time.Minute {
		t.Fatalf("cache TTL = %s, want 48m..54m", ttl)
	}
}

func TestScopeCacheIsolatesActorRunAndRole(t *testing.T) {
	cache, _ := testScopeCache(t, "test")
	guards := []scopeGuard{
		{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11},
		{ActorUserID: 8, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11},
		{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 4, DirectoryRunID: 12},
		{ActorUserID: 7, ActorRole: "admin", DirectorySourceID: 4, DirectoryRunID: 12},
	}
	var loads atomic.Int32
	for index, guard := range guards {
		guard := guard
		wantRoot := fmt.Sprintf("department-%d", index)
		for read := 0; read < 2; read++ {
			scope, err := cache.GetOrLoad(context.Background(), guard, func(context.Context) (*Scope, error) {
				loads.Add(1)
				return testCachedScope(guard.ActorUserID, wantRoot), nil
			})
			if err != nil {
				t.Fatalf("GetOrLoad guard %#v: %v", guard, err)
			}
			if got := scope.RepresentedDepartmentIDs; !reflect.DeepEqual(got, []string{wantRoot}) {
				t.Fatalf("guard %#v roots = %#v, want %#v", guard, got, []string{wantRoot})
			}
		}
	}
	if got, want := loads.Load(), int32(len(guards)); got != want {
		t.Fatalf("loader calls = %d, want %d isolated versions", got, want)
	}
}

func TestScopeCacheTreatsMalformedOrMismatchedValuesAsMisses(t *testing.T) {
	cache, server := testScopeCache(t, "test")
	guard := scopeGuard{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}
	key := scopeCacheKey("test", guard)
	server.Set(key, `{"schema_version":1,"actor_user_id":999}`)

	want := testCachedScope(guard.ActorUserID, "department-current")
	var loads atomic.Int32
	got, err := cache.GetOrLoad(context.Background(), guard, func(context.Context) (*Scope, error) {
		loads.Add(1)
		return want, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if !reflect.DeepEqual(got, want) || loads.Load() != 1 {
		t.Fatalf("got=%#v loads=%d, want authoritative rebuild %#v/1", got, loads.Load(), want)
	}
}

func TestScopeCacheCollapsesRefreshAcrossReplicas(t *testing.T) {
	server := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	store := readcache.NewRedisStore(redisClient)
	cacheA := newTestScopeCache(t, store, "test")
	cacheB := newTestScopeCache(t, store, "test")
	guard := scopeGuard{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}

	var loads atomic.Int32
	started := make(chan struct{})
	release := make(chan struct{})
	loader := func(ctx context.Context) (*Scope, error) {
		if loads.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return testCachedScope(guard.ActorUserID, "department-alpha"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	const callers = 20
	var ready sync.WaitGroup
	ready.Add(callers)
	start := make(chan struct{})
	errorsCh := make(chan error, callers)
	for i := 0; i < callers; i++ {
		cache := cacheA
		if i%2 == 1 {
			cache = cacheB
		}
		go func(cache *Cache) {
			ready.Done()
			<-start
			_, err := cache.GetOrLoad(context.Background(), guard, loader)
			errorsCh <- err
		}(cache)
	}
	ready.Wait()
	close(start)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("loader did not start")
	}
	time.Sleep(25 * time.Millisecond)
	close(release)
	for i := 0; i < callers; i++ {
		if err := <-errorsCh; err != nil {
			t.Fatalf("caller error: %v", err)
		}
	}
	if got := loads.Load(); got != 1 {
		t.Fatalf("loader calls = %d, want 1 across replicas", got)
	}
}

func TestScopeCacheRedisOutageFallsBackToAuthoritativeLoader(t *testing.T) {
	cache := newTestScopeCache(t, failingScopeStore{err: errors.New("redis unavailable")}, "test")
	guard := scopeGuard{ActorUserID: 7, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}
	want := testCachedScope(guard.ActorUserID, "department-alpha")
	var loads atomic.Int32

	got, err := cache.GetOrLoad(context.Background(), guard, func(context.Context) (*Scope, error) {
		loads.Add(1)
		return want, nil
	})
	if err != nil {
		t.Fatalf("GetOrLoad: %v", err)
	}
	if !reflect.DeepEqual(got, want) || loads.Load() != 1 {
		t.Fatalf("got=%#v loads=%d, want authoritative %#v/1", got, loads.Load(), want)
	}

	wantErr := errors.New("authoritative scope failed")
	got, err = cache.GetOrLoad(context.Background(), scopeGuard{ActorUserID: 8, ActorRole: "user", DirectorySourceID: 3, DirectoryRunID: 11}, func(context.Context) (*Scope, error) {
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) || got != nil {
		t.Fatalf("GetOrLoad failed source = %#v, %v; want nil, %v", got, err, wantErr)
	}
}

func testScopeCache(t *testing.T, namespace string) (*Cache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return newTestScopeCache(t, readcache.NewRedisStore(client), namespace), server
}

func newTestScopeCache(t *testing.T, store readcache.Store, namespace string) *Cache {
	t.Helper()
	cache, err := NewCache(store, CacheOptions{
		Namespace:      namespace,
		CommandTimeout: time.Second,
		RefreshTimeout: 5 * time.Second,
		LeaseTTL:       5 * time.Second,
		PollInterval:   5 * time.Millisecond,
		ReleaseTimeout: time.Second,
		RandFloat64:    func() float64 { return 0.5 },
		NewToken:       func() string { return fmt.Sprintf("lease-%d", time.Now().UnixNano()) },
	})
	if err != nil {
		t.Fatalf("NewCache: %v", err)
	}
	return cache
}

func testCachedScope(actorUserID int, roots ...string) *Scope {
	representedSubtrees := make(map[string]map[string]struct{}, len(roots))
	departments := make([]DepartmentScope, 0, len(roots))
	for _, root := range roots {
		representedSubtrees[root] = map[string]struct{}{root: {}}
		departments = append(departments, DepartmentScope{ExternalID: root, Name: root, DisplayPath: root})
	}
	return &Scope{
		ActorUserID:              actorUserID,
		ActorMemberExternalID:    fmt.Sprintf("member-%d", actorUserID),
		IsRepresentative:         true,
		RepresentedDepartmentIDs: append([]string(nil), roots...),
		RepresentedSubtreeIDs:    representedSubtrees,
		Departments:              departments,
		MemberTreeRootIDs:        append([]string(nil), roots...),
		MemberTreeDepartments:    append([]DepartmentScope(nil), departments...),
		Subjects:                 []Subject{},
		OverviewSubjects:         []Subject{},
		TargetRepresentedRoots:   map[int][]string{},
	}
}

type failingScopeStore struct {
	err error
}

func (s failingScopeStore) Get(context.Context, string) ([]byte, error) {
	return nil, s.err
}

func (s failingScopeStore) Set(context.Context, string, []byte, time.Duration) error {
	return s.err
}

func (s failingScopeStore) TryAcquireLease(context.Context, string, string, time.Duration) (bool, error) {
	return false, s.err
}

func (s failingScopeStore) LeaseTTL(context.Context, string) (time.Duration, error) {
	return 0, s.err
}

func (s failingScopeStore) ReleaseLease(context.Context, string, string) (bool, error) {
	return false, s.err
}
