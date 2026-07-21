package teamusage

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestPrewarmCacheKeysIsolateAllGenerationDimensions(t *testing.T) {
	const (
		namespace       = "test"
		schemaVersion   = 1
		providerID      = 7
		providerVersion = int64(11)
		anchorDate      = "2026-07-21"
	)
	timezoneDigest := prewarmTimezoneDigest("UTC")
	generationID := strings.Repeat("a", 64)

	baseManifest, err := prewarmManifestKey(namespace, schemaVersion, providerID, providerVersion, timezoneDigest, anchorDate)
	if err != nil {
		t.Fatalf("prewarmManifestKey() error = %v", err)
	}
	manifestVariants := []struct {
		name string
		key  func() (string, error)
	}{
		{name: "namespace", key: func() (string, error) {
			return prewarmManifestKey("other", schemaVersion, providerID, providerVersion, timezoneDigest, anchorDate)
		}},
		{name: "schema", key: func() (string, error) {
			return prewarmManifestKey(namespace, schemaVersion+1, providerID, providerVersion, timezoneDigest, anchorDate)
		}},
		{name: "provider", key: func() (string, error) {
			return prewarmManifestKey(namespace, schemaVersion, providerID+1, providerVersion, timezoneDigest, anchorDate)
		}},
		{name: "provider version", key: func() (string, error) {
			return prewarmManifestKey(namespace, schemaVersion, providerID, providerVersion+1, timezoneDigest, anchorDate)
		}},
		{name: "timezone digest", key: func() (string, error) {
			return prewarmManifestKey(namespace, schemaVersion, providerID, providerVersion, strings.Repeat("b", 64), anchorDate)
		}},
		{name: "anchor", key: func() (string, error) {
			return prewarmManifestKey(namespace, schemaVersion, providerID, providerVersion, timezoneDigest, "2026-07-22")
		}},
	}
	for _, variant := range manifestVariants {
		key, keyErr := variant.key()
		if keyErr != nil {
			t.Fatalf("%s manifest variant error = %v", variant.name, keyErr)
		}
		if key == baseManifest {
			t.Fatalf("%s manifest variant reused %q", variant.name, baseManifest)
		}
	}

	baseSegment, err := prewarmSegmentKey(namespace, schemaVersion, providerID, providerVersion, timezoneDigest, anchorDate, SegmentTodayHour, generationID)
	if err != nil {
		t.Fatalf("prewarmSegmentKey() error = %v", err)
	}
	segmentVariants := []struct {
		name string
		key  func() (string, error)
	}{
		{name: "namespace", key: func() (string, error) {
			return prewarmSegmentKey("other", schemaVersion, providerID, providerVersion, timezoneDigest, anchorDate, SegmentTodayHour, generationID)
		}},
		{name: "schema", key: func() (string, error) {
			return prewarmSegmentKey(namespace, schemaVersion+1, providerID, providerVersion, timezoneDigest, anchorDate, SegmentTodayHour, generationID)
		}},
		{name: "provider", key: func() (string, error) {
			return prewarmSegmentKey(namespace, schemaVersion, providerID+1, providerVersion, timezoneDigest, anchorDate, SegmentTodayHour, generationID)
		}},
		{name: "provider version", key: func() (string, error) {
			return prewarmSegmentKey(namespace, schemaVersion, providerID, providerVersion+1, timezoneDigest, anchorDate, SegmentTodayHour, generationID)
		}},
		{name: "timezone digest", key: func() (string, error) {
			return prewarmSegmentKey(namespace, schemaVersion, providerID, providerVersion, strings.Repeat("b", 64), anchorDate, SegmentTodayHour, generationID)
		}},
		{name: "anchor", key: func() (string, error) {
			return prewarmSegmentKey(namespace, schemaVersion, providerID, providerVersion, timezoneDigest, "2026-07-22", SegmentTodayHour, generationID)
		}},
		{name: "class", key: func() (string, error) {
			return prewarmSegmentKey(namespace, schemaVersion, providerID, providerVersion, timezoneDigest, anchorDate, SegmentHistory6d, generationID)
		}},
		{name: "generation", key: func() (string, error) {
			return prewarmSegmentKey(namespace, schemaVersion, providerID, providerVersion, timezoneDigest, anchorDate, SegmentTodayHour, strings.Repeat("c", 64))
		}},
	}
	for _, variant := range segmentVariants {
		key, keyErr := variant.key()
		if keyErr != nil {
			t.Fatalf("%s segment variant error = %v", variant.name, keyErr)
		}
		if key == baseSegment {
			t.Fatalf("%s segment variant reused %q", variant.name, baseSegment)
		}
	}

	currentA, err := prewarmCurrentStatsKey(namespace, schemaVersion, providerID, providerVersion, generationID)
	if err != nil {
		t.Fatalf("prewarmCurrentStatsKey() error = %v", err)
	}
	currentVariants := []struct {
		name string
		key  func() (string, error)
	}{
		{name: "namespace", key: func() (string, error) {
			return prewarmCurrentStatsKey("other", schemaVersion, providerID, providerVersion, generationID)
		}},
		{name: "schema", key: func() (string, error) {
			return prewarmCurrentStatsKey(namespace, schemaVersion+1, providerID, providerVersion, generationID)
		}},
		{name: "provider", key: func() (string, error) {
			return prewarmCurrentStatsKey(namespace, schemaVersion, providerID+1, providerVersion, generationID)
		}},
		{name: "provider version", key: func() (string, error) {
			return prewarmCurrentStatsKey(namespace, schemaVersion, providerID, providerVersion+1, generationID)
		}},
		{name: "generation", key: func() (string, error) {
			return prewarmCurrentStatsKey(namespace, schemaVersion, providerID, providerVersion, strings.Repeat("d", 64))
		}},
	}
	for _, variant := range currentVariants {
		key, keyErr := variant.key()
		if keyErr != nil {
			t.Fatalf("%s current variant error = %v", variant.name, keyErr)
		}
		if key == currentA {
			t.Fatalf("%s current variant reused %q", variant.name, currentA)
		}
	}
}

func TestPrewarmCacheGenerationLimitCountsTrendSegmentsOnly(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return generatedAt })
	identity := testPrewarmIdentity()
	manifest := testPrewarmManifest(t, cache, identity, generatedAt)
	manifest.CurrentStats.SerializedBytes = 1 << 20
	manifest.History29d.SerializedBytes = prewarmSegmentMaxBytes - 1
	manifest.History6d.SerializedBytes = prewarmSegmentMaxBytes - 2
	manifest.TodayHour.SerializedBytes = 1

	if err := validatePrewarmManifest("test", identity, manifest, generatedAt, false); err != nil {
		t.Fatalf("validatePrewarmManifest(trend below 16MiB) error = %v", err)
	}
}

func TestPrewarmCacheRejectsKeyReferencesOver512Bytes(t *testing.T) {
	cache, _ := newRedisPrewarmCache(t, time.Now)
	base := testPrewarmManifest(t, cache, testPrewarmIdentity(), testPrewarmGeneratedAt())
	tests := []struct {
		name   string
		mutate func(*PrewarmManifest)
	}{
		{name: "over width", mutate: func(manifest *PrewarmManifest) {
			manifest.CurrentStats.Key = strings.Repeat("k", prewarmKeyReferenceMaxBytes+1)
		}},
		{name: "other namespace", mutate: func(manifest *PrewarmManifest) {
			manifest.CurrentStats.Key = strings.Replace(manifest.CurrentStats.Key, "ae:test:", "ae:other:", 1)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := base
			test.mutate(&manifest)
			published, err := cache.PublishManifest(context.Background(), "lease", "owner", manifest)
			if err == nil || published {
				t.Fatalf("PublishManifest(invalid reference) = %v, %v, want false and error", published, err)
			}
		})
	}
}

func TestPrewarmCachePublishLastExpiresBeforeEarliestMovingHardExpiry(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	now := generatedAt.Add(time.Minute + time.Second)
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	manifest := testPrewarmManifest(t, cache, testPrewarmIdentity(), generatedAt)

	published, err := cache.PublishManifest(context.Background(), "lease", "owner", manifest)
	if err == nil || published {
		t.Fatalf("PublishManifest(late generation) = %v, %v, want expiry relationship rejection", published, err)
	}
}

func TestPrewarmCacheImmutableValueRejectsGenerationReuse(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	cache, server := newRedisPrewarmCache(t, func() time.Time { return generatedAt })
	value := testPrewarmCurrentStats(generatedAt, "a")
	ref, err := cache.WriteCurrentStats(context.Background(), value)
	if err != nil {
		t.Fatalf("first WriteCurrentStats() error = %v", err)
	}
	if _, err := cache.WriteCurrentStats(context.Background(), value); err != nil {
		t.Fatalf("idempotent WriteCurrentStats() error = %v", err)
	}

	changed := value
	changed.Stats = append([]PrewarmCurrentStat(nil), value.Stats...)
	changed.Stats[0].TodayActualCost = 2.25
	if _, err := cache.WriteCurrentStats(context.Background(), changed); err == nil {
		t.Fatal("WriteCurrentStats(reused generation) error = nil, want immutable conflict")
	}
	stored, err := server.Get(ref.Key)
	if err != nil {
		t.Fatalf("read immutable value error = %v", err)
	}
	var decoded PrewarmCurrentStatsEnvelope
	if err := json.Unmarshal([]byte(stored), &decoded); err != nil {
		t.Fatalf("decode immutable value error = %v", err)
	}
	if decoded.Stats[0].TodayActualCost != 1.25 {
		t.Fatalf("immutable value cost = %v, want original 1.25", decoded.Stats[0].TodayActualCost)
	}
}

func TestPrewarmCacheConcurrentIdenticalWriteWaitsForClaimPublication(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	baseStore := readcache.NewRedisStore(client)
	generatedAt := testPrewarmGeneratedAt()
	value := testPrewarmCurrentStats(generatedAt, "a")
	encoded, err := encodePrewarmJSON(value, prewarmCurrentStatsMaxBytes)
	if err != nil {
		t.Fatalf("encode current stats error = %v", err)
	}
	key, err := prewarmCurrentStatsKey("test", prewarmCacheSchemaVersion, value.ProviderID, value.ProviderVersion, value.GenerationID)
	if err != nil {
		t.Fatalf("prewarmCurrentStatsKey() error = %v", err)
	}
	digest := sha256.Sum256(encoded)
	claimToken := "immutable:" + fmt.Sprintf("%x", digest)
	acquired, err := baseStore.TryAcquireLease(context.Background(), key, claimToken, prewarmImmutableClaimTTL)
	if err != nil || !acquired {
		t.Fatalf("controlled claim acquire = %v, %v", acquired, err)
	}

	controlled := &controlledClaimPrewarmStore{
		BatchStore: baseStore, key: key, claimToken: claimToken,
		observed: make(chan struct{}), release: make(chan struct{}),
	}
	cache := mustNewPrewarmCache(t, controlled, func() time.Time { return generatedAt })
	result := make(chan error, 1)
	go func() {
		_, writeErr := cache.WriteCurrentStats(context.Background(), value)
		result <- writeErr
	}()
	<-controlled.observed
	written, err := baseStore.SetIfLeaseOwned(context.Background(), key, claimToken, key, encoded, movingValueTTL)
	if err != nil || !written {
		t.Fatalf("controlled claim publication = %v, %v", written, err)
	}
	close(controlled.release)
	if err := <-result; err != nil {
		t.Fatalf("concurrent identical WriteCurrentStats() error = %v", err)
	}
}

func TestPrewarmCachePublishLastRechecksClockAfterValidationRead(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	now := generatedAt
	store := newRecordingPrewarmStore()
	cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
	identity := testPrewarmIdentity()
	leaseKey := cache.LeaseKey("moving", "delayed-publication")
	acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, "owner", 90*time.Second)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
	}
	manifest := testPrewarmManifest(t, cache, identity, generatedAt)
	store.mgetAfter = func() { now = generatedAt.Add(59 * time.Second) }

	published, err := cache.PublishManifest(context.Background(), leaseKey, "owner", manifest)
	if err == nil || published {
		t.Fatalf("PublishManifest(delayed validation) = %v, %v, want fresh-clock expiry rejection", published, err)
	}
	manifestKey, _ := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
	if store.HasValue(manifestKey) {
		t.Fatal("late publication created a discoverable manifest")
	}
}

func TestPrewarmCachePublishManifestWithLeasesRequiresAllClaims(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	cache, server := newRedisPrewarmCache(t, func() time.Time { return generatedAt })
	manifest := testPrewarmManifest(t, cache, testPrewarmIdentity(), generatedAt)
	claims := []PrewarmLeaseClaim{
		{Key: cache.LeaseKey("coordinator", "multi"), Token: "cycle-owner"},
		{Key: cache.LeaseKey("segment", "multi"), Token: "segment-owner"},
	}
	for _, claim := range claims {
		acquired, err := cache.TryAcquireLease(context.Background(), claim.Key, claim.Token, time.Minute)
		if err != nil || !acquired {
			t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
		}
	}
	wrong := append([]PrewarmLeaseClaim(nil), claims...)
	wrong[1].Token = "wrong"
	published, err := cache.PublishManifestWithLeases(context.Background(), wrong, manifest)
	if err != nil || published {
		t.Fatalf("PublishManifestWithLeases(wrong) = %v, %v", published, err)
	}
	manifestKey, _ := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, testPrewarmIdentity())
	if server.Exists(manifestKey) {
		t.Fatal("wrong multi-lease claim published manifest")
	}
	server.Del(claims[0].Key)
	published, err = cache.PublishManifestWithLeases(context.Background(), claims, manifest)
	if err != nil || published || server.Exists(manifestKey) {
		t.Fatalf("PublishManifestWithLeases(missing) = %v, %v, exists=%v", published, err, server.Exists(manifestKey))
	}
	acquired, err := cache.TryAcquireLease(context.Background(), claims[0].Key, claims[0].Token, time.Minute)
	if err != nil || !acquired {
		t.Fatalf("reacquire coordinator = %v, %v", acquired, err)
	}
	published, err = cache.PublishManifestWithLeases(context.Background(), claims, manifest)
	if err != nil || !published {
		t.Fatalf("PublishManifestWithLeases(all owned) = %v, %v", published, err)
	}
}

func TestPrewarmCacheReadReturnsPerReferenceStatusForPartialGeneration(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	now := generatedAt.Add(30 * time.Second)
	cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	manifest := testPrewarmManifest(t, cache, identity, generatedAt)
	leaseKey := cache.LeaseKey("moving", "partial")
	acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, "owner", 90*time.Second)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
	}
	published, err := cache.PublishManifest(context.Background(), leaseKey, "owner", manifest)
	if err != nil || !published {
		t.Fatalf("PublishManifest() = %v, %v", published, err)
	}
	server.Del(manifest.TodayHour.Key)

	result, found, err := cache.Read(context.Background(), identity)
	if err != nil || !found || result == nil {
		t.Fatalf("Read(partial) = %#v, %v, %v", result, found, err)
	}
	if result.Complete || result.CurrentStats == nil || result.Segments.History29d == nil ||
		result.Segments.History6d == nil || result.Segments.TodayHour != nil {
		t.Fatalf("partial result = %#v, want only today missing", result)
	}
	if result.CurrentStatsStatus != PrewarmValueFresh || result.History29dStatus != PrewarmValueFresh ||
		result.History6dStatus != PrewarmValueFresh || result.TodayHourStatus != PrewarmValueMissing {
		t.Fatalf("partial statuses = %q/%q/%q/%q", result.CurrentStatsStatus, result.History29dStatus, result.History6dStatus, result.TodayHourStatus)
	}
	if result.Manifest.TodayHour.Key != manifest.TodayHour.Key {
		t.Fatalf("partial result lost resolved manifest reference: %#v", result.Manifest.TodayHour)
	}
}

func TestPrewarmCacheRecoveryStatusesAndSingleBatchRead(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	identity := testPrewarmIdentity()
	t.Run("missing references", func(t *testing.T) {
		targets := []struct {
			name      string
			selectRef func(PrewarmManifest) PrewarmValueReference
			status    func(*PrewarmCacheResult) PrewarmValueStatus
		}{
			{name: "current stats", selectRef: func(m PrewarmManifest) PrewarmValueReference { return m.CurrentStats }, status: func(r *PrewarmCacheResult) PrewarmValueStatus { return r.CurrentStatsStatus }},
			{name: "history 29d", selectRef: func(m PrewarmManifest) PrewarmValueReference { return m.History29d }, status: func(r *PrewarmCacheResult) PrewarmValueStatus { return r.History29dStatus }},
			{name: "history 6d", selectRef: func(m PrewarmManifest) PrewarmValueReference { return m.History6d }, status: func(r *PrewarmCacheResult) PrewarmValueStatus { return r.History6dStatus }},
		}
		for _, target := range targets {
			t.Run(target.name, func(t *testing.T) {
				now := generatedAt
				store := newRecordingPrewarmStore()
				cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
				manifest := testPrewarmManifest(t, cache, identity, generatedAt)
				publishTestPrewarmManifest(t, cache, manifest, "missing-"+target.name)
				store.DeleteRaw(target.selectRef(manifest).Key)
				before := store.MGetCalls()
				result, found, err := cache.Read(context.Background(), identity)
				if err != nil || !found || result == nil || result.Complete {
					t.Fatalf("Read(missing %s) = %#v, %v, %v", target.name, result, found, err)
				}
				if target.status(result) != PrewarmValueMissing {
					t.Fatalf("%s status = %q, want missing", target.name, target.status(result))
				}
				if got := store.MGetCalls() - before; got != 1 {
					t.Fatalf("Read(missing %s) MGET calls = %d, want 1", target.name, got)
				}
			})
		}
	})

	t.Run("hard expired references", func(t *testing.T) {
		targets := []struct {
			name    string
			replace func(*testing.T, *PrewarmCache, *PrewarmManifest, time.Time) time.Time
			status  func(*PrewarmCacheResult) PrewarmValueStatus
		}{
			{name: "current stats", replace: func(t *testing.T, cache *PrewarmCache, manifest *PrewarmManifest, hardAt time.Time) time.Time {
				generated := hardAt.Add(-movingHard)
				ref, err := cache.WriteCurrentStats(context.Background(), testPrewarmCurrentStats(generated, "8"))
				if err != nil {
					t.Fatalf("WriteCurrentStats(old) error = %v", err)
				}
				manifest.CurrentStats = ref
				return ref.HardExpiresAt
			}, status: func(r *PrewarmCacheResult) PrewarmValueStatus { return r.CurrentStatsStatus }},
			{name: "history 29d", replace: func(t *testing.T, cache *PrewarmCache, manifest *PrewarmManifest, hardAt time.Time) time.Time {
				generated := hardAt.Add(-historyHard)
				ref, err := cache.WriteSegment(context.Background(), testPrewarmSegment(t, identity, generated, SegmentHistory29d, "8"))
				if err != nil {
					t.Fatalf("WriteSegment(old history29d) error = %v", err)
				}
				manifest.History29d = ref
				return ref.HardExpiresAt
			}, status: func(r *PrewarmCacheResult) PrewarmValueStatus { return r.History29dStatus }},
			{name: "history 6d", replace: func(t *testing.T, cache *PrewarmCache, manifest *PrewarmManifest, hardAt time.Time) time.Time {
				generated := hardAt.Add(-historyHard)
				ref, err := cache.WriteSegment(context.Background(), testPrewarmSegment(t, identity, generated, SegmentHistory6d, "8"))
				if err != nil {
					t.Fatalf("WriteSegment(old history6d) error = %v", err)
				}
				manifest.History6d = ref
				return ref.HardExpiresAt
			}, status: func(r *PrewarmCacheResult) PrewarmValueStatus { return r.History6dStatus }},
		}
		for _, target := range targets {
			t.Run(target.name, func(t *testing.T) {
				now := generatedAt
				store := newRecordingPrewarmStore()
				cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
				manifest := testPrewarmManifest(t, cache, identity, generatedAt)
				hardAt := generatedAt.Add(3*time.Minute + 30*time.Second)
				hardAt = target.replace(t, cache, &manifest, hardAt)
				publishTestPrewarmManifest(t, cache, manifest, "hard-"+target.name)
				now = hardAt
				before := store.MGetCalls()
				result, found, err := cache.Read(context.Background(), identity)
				if err != nil || !found || result == nil || result.Complete {
					t.Fatalf("Read(hard expired %s) = %#v, %v, %v", target.name, result, found, err)
				}
				if target.status(result) != PrewarmValueHardExpired {
					t.Fatalf("%s status = %q, want hard_expired", target.name, target.status(result))
				}
				if got := store.MGetCalls() - before; got != 1 {
					t.Fatalf("Read(hard expired %s) MGET calls = %d, want 1", target.name, got)
				}
			})
		}
	})

	t.Run("stale but hard valid", func(t *testing.T) {
		for _, class := range []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d} {
			t.Run(string(class), func(t *testing.T) {
				now := generatedAt
				store := newRecordingPrewarmStore()
				cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
				manifest := testPrewarmManifest(t, cache, identity, generatedAt)
				ref, err := cache.WriteSegment(context.Background(), testPrewarmSegment(t, identity, generatedAt.Add(-26*time.Hour), class, "8"))
				if err != nil {
					t.Fatalf("WriteSegment(stale %s) error = %v", class, err)
				}
				if class == SegmentHistory29d {
					manifest.History29d = ref
				} else {
					manifest.History6d = ref
				}
				publishTestPrewarmManifest(t, cache, manifest, "stale-"+string(class))
				before := store.MGetCalls()
				result, found, err := cache.Read(context.Background(), identity)
				if err != nil || !found || result == nil || !result.Complete {
					t.Fatalf("Read(stale %s) = %#v, %v, %v", class, result, found, err)
				}
				status := result.History29dStatus
				if class == SegmentHistory6d {
					status = result.History6dStatus
				}
				if status != PrewarmValueStale {
					t.Fatalf("stale %s status = %q", class, status)
				}
				if got := store.MGetCalls() - before; got != 1 {
					t.Fatalf("Read(stale %s) MGET calls = %d, want 1", class, got)
				}
			})
		}
	})

	t.Run("invalid current and history reject", func(t *testing.T) {
		targets := []struct {
			name      string
			selectRef func(PrewarmManifest) PrewarmValueReference
		}{
			{name: "current stats", selectRef: func(m PrewarmManifest) PrewarmValueReference { return m.CurrentStats }},
			{name: "history 29d", selectRef: func(m PrewarmManifest) PrewarmValueReference { return m.History29d }},
			{name: "history 6d", selectRef: func(m PrewarmManifest) PrewarmValueReference { return m.History6d }},
		}
		for _, target := range targets {
			t.Run(target.name, func(t *testing.T) {
				now := generatedAt
				store := newRecordingPrewarmStore()
				cache := mustNewPrewarmCache(t, store, func() time.Time { return now })
				manifest := testPrewarmManifest(t, cache, identity, generatedAt)
				publishTestPrewarmManifest(t, cache, manifest, "invalid-"+target.name)
				store.SetRaw(target.selectRef(manifest).Key, []byte(`{"schema_version":1`), movingValueTTL)
				before := store.MGetCalls()
				result, found, err := cache.Read(context.Background(), identity)
				if err == nil || found || result != nil {
					t.Fatalf("Read(invalid %s) = %#v, %v, %v, want rejection", target.name, result, found, err)
				}
				if got := store.MGetCalls() - before; got != 1 {
					t.Fatalf("Read(invalid %s) MGET calls = %d, want 1", target.name, got)
				}
			})
		}
	})
}

func TestPrewarmCacheReadReturnsPartialForInvalidOrHardExpiredToday(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	identity := testPrewarmIdentity()
	tests := []struct {
		name       string
		wantStatus PrewarmValueStatus
		prepare    func(*testing.T, *PrewarmCache, *miniredis.Miniredis, *PrewarmManifest, *time.Time)
	}{
		{
			name:       "malformed",
			wantStatus: PrewarmValueInvalid,
			prepare: func(_ *testing.T, _ *PrewarmCache, server *miniredis.Miniredis, manifest *PrewarmManifest, _ *time.Time) {
				server.Set(manifest.TodayHour.Key, `{"schema_version":1`)
				server.SetTTL(manifest.TodayHour.Key, movingValueTTL)
			},
		},
		{
			name:       "metadata mismatch",
			wantStatus: PrewarmValueInvalid,
			prepare: func(t *testing.T, _ *PrewarmCache, server *miniredis.Miniredis, manifest *PrewarmManifest, _ *time.Time) {
				value := testPrewarmSegment(t, identity, generatedAt, SegmentTodayHour, "d")
				value.ProviderVersion++
				encoded, err := json.Marshal(value)
				if err != nil {
					t.Fatalf("encode mismatched today error = %v", err)
				}
				server.Set(manifest.TodayHour.Key, string(encoded))
				server.SetTTL(manifest.TodayHour.Key, movingValueTTL)
			},
		},
		{
			name:       "hard expired",
			wantStatus: PrewarmValueHardExpired,
			prepare: func(t *testing.T, cache *PrewarmCache, _ *miniredis.Miniredis, manifest *PrewarmManifest, now *time.Time) {
				todayGeneratedAt := generatedAt.Add(-30 * time.Second)
				ref, err := cache.WriteSegment(context.Background(), testPrewarmSegment(t, identity, todayGeneratedAt, SegmentTodayHour, "8"))
				if err != nil {
					t.Fatalf("WriteSegment(older today) error = %v", err)
				}
				manifest.TodayHour = ref
				*now = todayGeneratedAt.Add(movingHard)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := generatedAt
			cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
			manifest := testPrewarmManifest(t, cache, identity, generatedAt)
			if test.name == "hard expired" {
				test.prepare(t, cache, server, &manifest, &now)
				now = generatedAt
			}
			leaseKey := cache.LeaseKey("moving", "partial", test.name)
			acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, "owner", 90*time.Second)
			if err != nil || !acquired {
				t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
			}
			published, err := cache.PublishManifest(context.Background(), leaseKey, "owner", manifest)
			if err != nil || !published {
				t.Fatalf("PublishManifest() = %v, %v", published, err)
			}
			if test.name == "hard expired" {
				now = manifest.TodayHour.HardExpiresAt
			} else {
				test.prepare(t, cache, server, &manifest, &now)
			}

			result, found, err := cache.Read(context.Background(), identity)
			if err != nil || !found || result == nil {
				t.Fatalf("Read(partial today) = %#v, %v, %v", result, found, err)
			}
			assertPartialTodayResult(t, result, test.wantStatus)
		})
	}
}

func TestPrewarmCachePublishLastChecksReusedHistoryHardExpiry(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	now := generatedAt
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	manifest := testPrewarmManifest(t, cache, identity, generatedAt)
	reused := testPrewarmSegment(t, identity, generatedAt.Add(-historyHard+2*time.Minute), SegmentHistory29d, "9")
	ref, err := cache.WriteSegment(context.Background(), reused)
	if err != nil {
		t.Fatalf("WriteSegment(reused history) error = %v", err)
	}
	manifest.History29d = ref

	published, err := cache.PublishManifest(context.Background(), "lease", "owner", manifest)
	if err == nil || published {
		t.Fatalf("PublishManifest(reused expiring history) = %v, %v, want earliest-reference rejection", published, err)
	}
}

func TestPrewarmCacheRejectsCommandTimeoutOverTwoSeconds(t *testing.T) {
	store := newRecordingPrewarmStore()
	tests := []struct {
		name   string
		mutate func(*PrewarmCacheOptions)
	}{
		{name: "read", mutate: func(options *PrewarmCacheOptions) { options.ReadTimeout = 2*time.Second + time.Nanosecond }},
		{name: "write", mutate: func(options *PrewarmCacheOptions) { options.WriteTimeout = 2*time.Second + time.Nanosecond }},
		{name: "lease", mutate: func(options *PrewarmCacheOptions) { options.LeaseTimeout = 2*time.Second + time.Nanosecond }},
		{name: "release", mutate: func(options *PrewarmCacheOptions) { options.ReleaseTimeout = 2*time.Second + time.Nanosecond }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			options := PrewarmCacheOptions{Namespace: "test"}
			test.mutate(&options)
			if _, err := NewPrewarmCache(store, options); err == nil {
				t.Fatal("NewPrewarmCache(over-cap timeout) error = nil")
			}
		})
	}
}

func TestPrewarmCachePublishLastWritesImmutableValuesBeforeManifest(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	store := newRecordingPrewarmStore()
	cache := mustNewPrewarmCache(t, store, func() time.Time { return generatedAt })
	identity := testPrewarmIdentity()
	leaseKey := cache.LeaseKey("moving", "provider-7", "version-11", identity.Timezone, identity.AnchorDate)
	acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, "owner", 90*time.Second)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
	}
	manifest := testPrewarmManifest(t, cache, identity, generatedAt)
	published, err := cache.PublishManifest(context.Background(), leaseKey, "owner", manifest)
	if err != nil || !published {
		t.Fatalf("PublishManifest() = %v, %v", published, err)
	}

	events := store.Events()
	manifestKey, _ := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
	manifestEvent := "publish " + manifestKey
	if len(events) == 0 || events[len(events)-1] != manifestEvent {
		t.Fatalf("last event = %q, want manifest publication; all=%#v", events[len(events)-1], events)
	}
	for _, ref := range []PrewarmValueReference{manifest.CurrentStats, manifest.History29d, manifest.History6d, manifest.TodayHour} {
		valueEvent := "publish " + ref.Key
		if eventIndex(events, valueEvent) < 0 || eventIndex(events, valueEvent) >= eventIndex(events, manifestEvent) {
			t.Fatalf("immutable value %q was not written before manifest; all=%#v", ref.Key, events)
		}
	}
	if ttl := store.TTL(manifest.CurrentStats.Key); ttl != movingValueTTL {
		t.Fatalf("current stats TTL = %s, want %s", ttl, movingValueTTL)
	}
	if ttl := store.TTL(manifest.TodayHour.Key); ttl != movingValueTTL {
		t.Fatalf("today TTL = %s, want %s", ttl, movingValueTTL)
	}
	if ttl := store.TTL(manifest.History29d.Key); ttl != historyValueTTL {
		t.Fatalf("history TTL = %s, want %s", ttl, historyValueTTL)
	}
	if ttl := store.TTL(manifestKey); ttl != manifestTTL {
		t.Fatalf("manifest TTL = %s, want %s", ttl, manifestTTL)
	}
	if !(manifestTTL < movingHard && movingValueTTL > manifestTTL+44*time.Second && historyValueTTL > manifestTTL+44*time.Second) {
		t.Fatal("prewarm TTL relationships do not cover discovery and bounded requests")
	}
}

func TestPrewarmCacheReaderUsesOneManifestOnly(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	store := newRecordingPrewarmStore()
	cache := mustNewPrewarmCache(t, store, func() time.Time { return generatedAt.Add(30 * time.Second) })
	identity := testPrewarmIdentity()
	manifestA := testPrewarmManifestWithGeneration(t, cache, identity, generatedAt, "a")
	manifestB := testPrewarmManifestWithGeneration(t, cache, identity, generatedAt.Add(time.Second), "e")
	manifestKey, _ := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
	encodedA, _ := json.Marshal(manifestA)
	encodedB, _ := json.Marshal(manifestB)
	store.SetRaw(manifestKey, encodedA, manifestTTL)
	store.getAfter = func(key string) {
		if key == manifestKey {
			store.SetRaw(manifestKey, encodedB, manifestTTL)
		}
	}

	result, found, err := cache.Read(context.Background(), identity)
	if err != nil || !found || result == nil {
		t.Fatalf("Read() = %#v, %v, %v", result, found, err)
	}
	if result.Manifest.CurrentStats.GenerationID != strings.Repeat("a", 64) || result.CurrentStats.GenerationID != strings.Repeat("a", 64) ||
		result.Manifest.TodayHour.GenerationID != strings.Repeat("d", 64) || result.Segments.TodayHour.GenerationID != strings.Repeat("d", 64) {
		t.Fatalf("Read() mixed generations: manifest current=%q value current=%q manifest today=%q value today=%q",
			result.Manifest.CurrentStats.GenerationID, result.CurrentStats.GenerationID,
			result.Manifest.TodayHour.GenerationID, result.Segments.TodayHour.GenerationID)
	}
	if store.GetCalls() != 1 {
		t.Fatalf("manifest GET calls = %d, want exactly 1", store.GetCalls())
	}
	if store.MGetCalls() != 1 {
		t.Fatalf("value MGET calls = %d, want exactly 1", store.MGetCalls())
	}
	if got := store.LastMGet(); !equalStrings(got, []string{manifestA.CurrentStats.Key, manifestA.History29d.Key, manifestA.History6d.Key, manifestA.TodayHour.Key}) {
		t.Fatalf("MGET keys = %#v, want first manifest references", got)
	}
}

func TestPrewarmCacheRejectsMalformedOversizedAndHardExpiredValues(t *testing.T) {
	generatedAt := testPrewarmGeneratedAt()
	identity := testPrewarmIdentity()
	tests := []struct {
		name   string
		mutate func(*PrewarmCache, *miniredis.Miniredis, PrewarmManifest, *time.Time)
	}{
		{
			name: "malformed manifest",
			mutate: func(_ *PrewarmCache, server *miniredis.Miniredis, _ PrewarmManifest, _ *time.Time) {
				key, _ := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
				server.Set(key, `{"schema_version":1`)
				server.SetTTL(key, manifestTTL)
			},
		},
		{
			name: "oversized manifest",
			mutate: func(_ *PrewarmCache, server *miniredis.Miniredis, _ PrewarmManifest, _ *time.Time) {
				key, _ := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
				server.Set(key, strings.Repeat("x", prewarmManifestMaxBytes))
				server.SetTTL(key, manifestTTL)
			},
		},
		{
			name: "malformed segment",
			mutate: func(_ *PrewarmCache, server *miniredis.Miniredis, manifest PrewarmManifest, _ *time.Time) {
				server.Set(manifest.History6d.Key, `{"schema_version":1`)
				server.SetTTL(manifest.History6d.Key, historyValueTTL)
			},
		},
		{
			name: "oversized current stats",
			mutate: func(_ *PrewarmCache, server *miniredis.Miniredis, manifest PrewarmManifest, _ *time.Time) {
				server.Set(manifest.CurrentStats.Key, strings.Repeat("x", prewarmCurrentStatsMaxBytes))
				server.SetTTL(manifest.CurrentStats.Key, movingValueTTL)
			},
		},
		{
			name: "oversized segment",
			mutate: func(_ *PrewarmCache, server *miniredis.Miniredis, manifest PrewarmManifest, _ *time.Time) {
				server.Set(manifest.History6d.Key, strings.Repeat("x", prewarmSegmentMaxBytes))
				server.SetTTL(manifest.History6d.Key, historyValueTTL)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			now := generatedAt.Add(30 * time.Second)
			cache, server := newRedisPrewarmCache(t, func() time.Time { return now })
			manifest := testPrewarmManifest(t, cache, identity, generatedAt)
			leaseKey := cache.LeaseKey("moving", "reject", test.name)
			acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, "owner", 90*time.Second)
			if err != nil || !acquired {
				t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
			}
			published, err := cache.PublishManifest(context.Background(), leaseKey, "owner", manifest)
			if err != nil || !published {
				t.Fatalf("PublishManifest() = %v, %v", published, err)
			}
			test.mutate(cache, server, manifest, &now)

			result, found, _ := cache.Read(context.Background(), identity)
			if found || result != nil {
				t.Fatalf("Read() = %#v, %v, want rejected", result, found)
			}
		})
	}
}

func TestPrewarmCacheOldAnchorRemainsReadableForInflightRequest(t *testing.T) {
	generatedAt := time.Date(2026, 7, 21, 23, 59, 0, 0, time.UTC)
	now := generatedAt
	cache, _ := newRedisPrewarmCache(t, func() time.Time { return now })
	identity := testPrewarmIdentity()
	manifest := testPrewarmManifest(t, cache, identity, generatedAt)
	leaseKey := cache.LeaseKey("moving", "old-anchor")
	acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, "owner", 90*time.Second)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease() = %v, %v", acquired, err)
	}
	published, err := cache.PublishManifest(context.Background(), leaseKey, "owner", manifest)
	if err != nil || !published {
		t.Fatalf("PublishManifest() = %v, %v", published, err)
	}

	now = time.Date(2026, 7, 22, 0, 0, 30, 0, time.UTC)
	result, found, err := cache.Read(context.Background(), identity)
	if err != nil || !found || result == nil {
		t.Fatalf("Read(old anchor after rollover) = %#v, %v, %v", result, found, err)
	}
	if result.Manifest.AnchorDate != "2026-07-21" {
		t.Fatalf("manifest anchor = %q, want 2026-07-21", result.Manifest.AnchorDate)
	}
}

func TestPrewarmCacheRedisFailuresRemainFailOpen(t *testing.T) {
	synthetic := errors.New("synthetic Redis failure")
	generatedAt := testPrewarmGeneratedAt()
	identity := testPrewarmIdentity()

	t.Run("manifest read", func(t *testing.T) {
		store := newRecordingPrewarmStore()
		store.getErr = synthetic
		cache := mustNewPrewarmCache(t, store, time.Now)
		result, found, err := cache.Read(context.Background(), identity)
		if result != nil || found || !errors.Is(err, synthetic) {
			t.Fatalf("Read() = %#v, %v, %v, want fail-open miss and Redis error", result, found, err)
		}
	})

	t.Run("batch read", func(t *testing.T) {
		store := newRecordingPrewarmStore()
		cache := mustNewPrewarmCache(t, store, func() time.Time { return generatedAt.Add(time.Second) })
		manifest := testPrewarmManifest(t, cache, identity, generatedAt)
		manifestKey, _ := prewarmManifestKeyForIdentity("test", prewarmCacheSchemaVersion, identity)
		encoded, _ := json.Marshal(manifest)
		store.SetRaw(manifestKey, encoded, manifestTTL)
		store.mgetErr = synthetic
		result, found, err := cache.Read(context.Background(), identity)
		if result != nil || found || !errors.Is(err, synthetic) {
			t.Fatalf("Read() = %#v, %v, %v, want fail-open miss and Redis error", result, found, err)
		}
	})

	t.Run("immutable write", func(t *testing.T) {
		store := newRecordingPrewarmStore()
		store.setErr = synthetic
		cache := mustNewPrewarmCache(t, store, func() time.Time { return generatedAt })
		_, err := cache.WriteCurrentStats(context.Background(), testPrewarmCurrentStats(generatedAt, "a"))
		if !errors.Is(err, synthetic) {
			t.Fatalf("WriteCurrentStats() error = %v, want Redis failure", err)
		}
	})

	t.Run("atomic publish", func(t *testing.T) {
		store := newRecordingPrewarmStore()
		cache := mustNewPrewarmCache(t, store, func() time.Time { return generatedAt })
		manifest := testPrewarmManifest(t, cache, identity, generatedAt)
		store.publishErr = synthetic
		published, err := cache.PublishManifest(context.Background(), "lease", "owner", manifest)
		if published || !errors.Is(err, synthetic) {
			t.Fatalf("PublishManifest() = %v, %v, want false and Redis error", published, err)
		}
	})

	t.Run("lease", func(t *testing.T) {
		store := newRecordingPrewarmStore()
		store.leaseErr = synthetic
		cache := mustNewPrewarmCache(t, store, time.Now)
		acquired, err := cache.TryAcquireLease(context.Background(), cache.LeaseKey("moving", "lease-error"), "owner", time.Minute)
		if acquired || !errors.Is(err, synthetic) {
			t.Fatalf("TryAcquireLease() = %v, %v, want false and Redis error", acquired, err)
		}
	})
}

func testPrewarmManifest(t *testing.T, cache *PrewarmCache, identity PrewarmCacheIdentity, generatedAt time.Time) PrewarmManifest {
	t.Helper()
	return testPrewarmManifestWithGeneration(t, cache, identity, generatedAt, "a")
}

func testPrewarmManifestWithGeneration(t *testing.T, cache *PrewarmCache, identity PrewarmCacheIdentity, generatedAt time.Time, seed string) PrewarmManifest {
	t.Helper()
	seeds := []string{seed, string(seed[0] + 1), string(seed[0] + 2), string(seed[0] + 3)}
	currentRef, err := cache.WriteCurrentStats(context.Background(), testPrewarmCurrentStats(generatedAt, seeds[0]))
	if err != nil {
		t.Fatalf("WriteCurrentStats() error = %v", err)
	}
	refs := make([]PrewarmValueReference, 0, 3)
	for index, class := range []PrewarmSegmentClass{SegmentHistory29d, SegmentHistory6d, SegmentTodayHour} {
		ref, writeErr := cache.WriteSegment(context.Background(), testPrewarmSegment(t, identity, generatedAt, class, seeds[index+1]))
		if writeErr != nil {
			t.Fatalf("WriteSegment(%s) error = %v", class, writeErr)
		}
		refs = append(refs, ref)
	}
	return PrewarmManifest{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		Timezone: identity.Timezone, TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), AnchorDate: identity.AnchorDate,
		CreatedAt: generatedAt, CurrentStats: currentRef, History29d: refs[0], History6d: refs[1], TodayHour: refs[2],
	}
}

func testPrewarmCurrentStats(generatedAt time.Time, generationSeed string) PrewarmCurrentStatsEnvelope {
	tokens := int64(123)
	return PrewarmCurrentStatsEnvelope{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: 7, ProviderVersion: 11,
		GenerationID: strings.Repeat(generationSeed, 64), GeneratedAt: generatedAt,
		RosterCount: 1, RosterDigest: strings.Repeat("f", 64), ResponseBytes: 512,
		Stats: []PrewarmCurrentStat{{UserID: 101, TodayActualCost: 1.25, TotalActualCost: 12.5, TotalTokens: &tokens}},
	}
}

func testPrewarmSegment(t *testing.T, identity PrewarmCacheIdentity, generatedAt time.Time, class PrewarmSegmentClass, generationSeed string) PrewarmTrendSegment {
	t.Helper()
	coverage, err := prewarmSegmentCoverage(class, identity.AnchorDate, identity.Timezone)
	if err != nil {
		t.Fatalf("prewarmSegmentCoverage(%s) error = %v", class, err)
	}
	label := coverage.StartDate
	if class == SegmentTodayHour {
		label += " 00:00"
	}
	tokens := int64(42)
	return PrewarmTrendSegment{
		SchemaVersion: prewarmCacheSchemaVersion, ProviderID: identity.ProviderID, ProviderVersion: identity.ProviderVersion,
		TimezoneDigest: prewarmTimezoneDigest(identity.Timezone), GenerationID: strings.Repeat(generationSeed, 64), GeneratedAt: generatedAt,
		Timezone: identity.Timezone, AnchorDate: identity.AnchorDate, Class: class, Coverage: coverage,
		Points:        []relay.ProviderWideTrendPoint{{UserID: 101, Date: label, ActualCost: 0.5, TotalTokens: &tokens}},
		ResponseBytes: 128, PointCount: 1, UniqueUserCount: 1, Complete: true,
	}
}

func testPrewarmIdentity() PrewarmCacheIdentity {
	return PrewarmCacheIdentity{ProviderID: 7, ProviderVersion: 11, Timezone: "UTC", AnchorDate: "2026-07-21"}
}

func testPrewarmGeneratedAt() time.Time {
	return time.Date(2026, 7, 21, 8, 0, 0, 0, time.UTC)
}

func newRedisPrewarmCache(t *testing.T, now func() time.Time) (*PrewarmCache, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return mustNewPrewarmCache(t, readcache.NewRedisStore(client), now), server
}

func mustNewPrewarmCache(t *testing.T, store readcache.BatchStore, now func() time.Time) *PrewarmCache {
	t.Helper()
	cache, err := NewPrewarmCache(store, PrewarmCacheOptions{Namespace: "test", Now: now})
	if err != nil {
		t.Fatalf("NewPrewarmCache() error = %v", err)
	}
	return cache
}

type recordingPrewarmStore struct {
	mu         sync.Mutex
	values     map[string][]byte
	ttls       map[string]time.Duration
	leases     map[string]string
	events     []string
	lastMGet   []string
	getCalls   int
	getAfter   func(string)
	getErr     error
	mgetErr    error
	mgetAfter  func()
	mgetCalls  int
	setErr     error
	publishErr error
	leaseErr   error
}

func newRecordingPrewarmStore() *recordingPrewarmStore {
	return &recordingPrewarmStore{values: make(map[string][]byte), ttls: make(map[string]time.Duration), leases: make(map[string]string)}
}

func (s *recordingPrewarmStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	if s.getErr != nil {
		err := s.getErr
		s.mu.Unlock()
		return nil, err
	}
	s.getCalls++
	value, ok := s.values[key]
	cloned := append([]byte(nil), value...)
	after := s.getAfter
	s.mu.Unlock()
	if after != nil {
		after(key)
	}
	if !ok {
		return nil, readcache.ErrMiss
	}
	return cloned, nil
}

func (s *recordingPrewarmStore) MGet(_ context.Context, keys ...string) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "mget "+strings.Join(keys, ","))
	s.mgetCalls++
	s.lastMGet = append([]string(nil), keys...)
	if s.mgetErr != nil {
		return nil, s.mgetErr
	}
	values := make([][]byte, len(keys))
	for index, key := range keys {
		if value, ok := s.values[key]; ok {
			values[index] = append([]byte(nil), value...)
		}
	}
	if s.mgetAfter != nil {
		s.mgetAfter()
	}
	return values, nil
}

func (s *recordingPrewarmStore) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "set "+key)
	if s.setErr != nil {
		return s.setErr
	}
	s.values[key] = append([]byte(nil), value...)
	s.ttls[key] = ttl
	return nil
}

func (s *recordingPrewarmStore) SetIfLeaseOwned(_ context.Context, leaseKey, token, key string, value []byte, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "publish "+key)
	if leaseKey == key && s.setErr != nil {
		return false, s.setErr
	}
	if s.publishErr != nil {
		return false, s.publishErr
	}
	if s.leases[leaseKey] != token {
		return false, nil
	}
	s.values[key] = append([]byte(nil), value...)
	s.ttls[key] = ttl
	if leaseKey == key {
		delete(s.leases, leaseKey)
	}
	return true, nil
}

func (s *recordingPrewarmStore) SetIfLeasesOwned(
	_ context.Context,
	leaseKeys, tokens []string,
	key string,
	value []byte,
	ttl time.Duration,
) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "publish "+key)
	if s.publishErr != nil {
		return false, s.publishErr
	}
	if len(leaseKeys) == 0 || len(leaseKeys) != len(tokens) {
		return false, fmt.Errorf("invalid multi-lease claim")
	}
	for index, leaseKey := range leaseKeys {
		if s.leases[leaseKey] != tokens[index] {
			return false, nil
		}
	}
	s.values[key] = append([]byte(nil), value...)
	s.ttls[key] = ttl
	return true, nil
}

func (s *recordingPrewarmStore) TryAcquireLease(_ context.Context, key, token string, _ time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, "acquire "+key)
	if s.leaseErr != nil {
		return false, s.leaseErr
	}
	if _, exists := s.leases[key]; exists {
		return false, nil
	}
	if _, exists := s.values[key]; exists {
		return false, nil
	}
	s.leases[key] = token
	return true, nil
}

func (s *recordingPrewarmStore) LeaseTTL(_ context.Context, key string) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseErr != nil {
		return 0, s.leaseErr
	}
	if _, exists := s.leases[key]; !exists {
		return 0, readcache.ErrMiss
	}
	return time.Minute, nil
}

func (s *recordingPrewarmStore) ReleaseLease(_ context.Context, key, token string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseErr != nil {
		return false, s.leaseErr
	}
	if s.leases[key] != token {
		return false, nil
	}
	delete(s.leases, key)
	return true, nil
}

func (s *recordingPrewarmStore) Events() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...)
}

func (s *recordingPrewarmStore) TTL(key string) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ttls[key]
}

func (s *recordingPrewarmStore) SetRaw(key string, value []byte, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[key] = append([]byte(nil), value...)
	s.ttls[key] = ttl
}

func (s *recordingPrewarmStore) GetCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.getCalls
}

func (s *recordingPrewarmStore) LastMGet() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lastMGet...)
}

func (s *recordingPrewarmStore) MGetCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mgetCalls
}

func (s *recordingPrewarmStore) DeleteRaw(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
	delete(s.ttls, key)
}

func (s *recordingPrewarmStore) HasValue(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.values[key]
	return ok
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func eventIndex(events []string, target string) int {
	for index, event := range events {
		if event == target {
			return index
		}
	}
	return -1
}

func assertPartialTodayResult(t *testing.T, result *PrewarmCacheResult, wantStatus PrewarmValueStatus) {
	t.Helper()
	if result.Complete || result.CurrentStats == nil || result.Segments.History29d == nil ||
		result.Segments.History6d == nil || result.Segments.TodayHour != nil {
		t.Fatalf("partial result = %#v, want only today unavailable", result)
	}
	currentAvailable := result.CurrentStatsStatus == PrewarmValueFresh || result.CurrentStatsStatus == PrewarmValueStale
	if !currentAvailable || result.History29dStatus != PrewarmValueFresh ||
		result.History6dStatus != PrewarmValueFresh || result.TodayHourStatus != wantStatus {
		t.Fatalf("partial statuses = %q/%q/%q/%q, want today %q",
			result.CurrentStatsStatus, result.History29dStatus, result.History6dStatus, result.TodayHourStatus, wantStatus)
	}
}

func publishTestPrewarmManifest(t *testing.T, cache *PrewarmCache, manifest PrewarmManifest, seed string) {
	t.Helper()
	leaseKey := cache.LeaseKey("moving", seed)
	acquired, err := cache.TryAcquireLease(context.Background(), leaseKey, "owner", 90*time.Second)
	if err != nil || !acquired {
		t.Fatalf("TryAcquireLease(%s) = %v, %v", seed, acquired, err)
	}
	published, err := cache.PublishManifest(context.Background(), leaseKey, "owner", manifest)
	if err != nil || !published {
		t.Fatalf("PublishManifest(%s) = %v, %v", seed, published, err)
	}
}

type controlledClaimPrewarmStore struct {
	readcache.BatchStore
	key        string
	claimToken string
	observed   chan struct{}
	release    chan struct{}
	once       sync.Once
}

func (s *controlledClaimPrewarmStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.BatchStore.Get(ctx, key)
	if err == nil && key == s.key && string(value) == s.claimToken {
		s.once.Do(func() { close(s.observed) })
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return value, err
}
