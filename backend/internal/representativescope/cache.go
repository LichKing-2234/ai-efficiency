package representativescope

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"regexp"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/google/uuid"
)

const scopeCacheSchemaVersion = 2

var scopeCacheNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type scopeGuard struct {
	ActorUserID       int    `json:"actor_user_id"`
	ActorRole         string `json:"actor_role"`
	DirectorySourceID int    `json:"directory_source_id"`
	DirectoryRunID    int    `json:"directory_run_id"`
}

type scopeVersionDimensions struct {
	SchemaVersion     int    `json:"schema_version"`
	ActorUserID       int    `json:"actor_user_id"`
	ActorRole         string `json:"actor_role"`
	DirectorySourceID int    `json:"directory_source_id"`
	DirectoryRunID    int    `json:"directory_run_id"`
}

type ScopeLoader func(context.Context) (*Scope, error)

type CacheOptions struct {
	Namespace      string
	CommandTimeout time.Duration
	RefreshTimeout time.Duration
	LeaseTTL       time.Duration
	PollInterval   time.Duration
	ReleaseTimeout time.Duration
	RandFloat64    func() float64
	NewToken       func() string
	Sleep          func(context.Context, time.Duration) error
}

type Cache struct {
	store   readcache.Store
	options CacheOptions
	flights readcache.FlightGroup[*Scope]
}

type scopeValueEnvelope struct {
	SchemaVersion int        `json:"schema_version"`
	Guard         scopeGuard `json:"guard"`
	Scope         *Scope     `json:"scope"`
}

func NewCache(store readcache.Store, options CacheOptions) (*Cache, error) {
	if store == nil {
		return nil, fmt.Errorf("representative scope cache store is required")
	}
	if !scopeCacheNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	applyScopeCacheDefaults(&options)
	return &Cache{store: store, options: options}, nil
}

func applyScopeCacheDefaults(options *CacheOptions) {
	if options.CommandTimeout <= 0 {
		options.CommandTimeout = 100 * time.Millisecond
	}
	if options.RefreshTimeout <= 0 {
		options.RefreshTimeout = 15 * time.Second
	}
	if options.LeaseTTL <= 0 {
		options.LeaseTTL = 20 * time.Second
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 25 * time.Millisecond
	}
	if options.ReleaseTimeout <= 0 {
		options.ReleaseTimeout = 100 * time.Millisecond
	}
	if options.RandFloat64 == nil {
		options.RandFloat64 = rand.Float64
	}
	if options.NewToken == nil {
		options.NewToken = uuid.NewString
	}
	if options.Sleep == nil {
		options.Sleep = readcache.Sleep
	}
}

func (c *Cache) GetOrLoad(ctx context.Context, guard scopeGuard, loader ScopeLoader) (*Scope, error) {
	if c == nil || c.store == nil {
		return nil, fmt.Errorf("representative scope cache is not configured")
	}
	if err := validateScopeGuard(guard); err != nil {
		return nil, err
	}
	if loader == nil {
		return nil, fmt.Errorf("representative scope loader is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	key := scopeCacheKey(c.options.Namespace, guard)
	return c.flights.Do(ctx, key, c.options.RefreshTimeout, func(sharedCtx context.Context) (*Scope, error) {
		return c.loadWithLease(sharedCtx, key, guard, loader)
	})
}

func validateScopeGuard(guard scopeGuard) error {
	if guard.ActorUserID <= 0 {
		return fmt.Errorf("representative scope actor ID must be positive")
	}
	if guard.ActorRole != "user" && guard.ActorRole != "admin" {
		return fmt.Errorf("invalid representative scope actor role %q", guard.ActorRole)
	}
	if guard.DirectorySourceID <= 0 || guard.DirectoryRunID <= 0 {
		return fmt.Errorf("representative scope directory snapshot must be positive")
	}
	return nil
}

func scopeVersion(guard scopeGuard) string {
	encoded, _ := json.Marshal(scopeVersionDimensions{
		SchemaVersion:     scopeCacheSchemaVersion,
		ActorUserID:       guard.ActorUserID,
		ActorRole:         guard.ActorRole,
		DirectorySourceID: guard.DirectorySourceID,
		DirectoryRunID:    guard.DirectoryRunID,
	})
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest)
}

func (c *Cache) loadWithLease(ctx context.Context, key string, guard scopeGuard, loader ScopeLoader) (*Scope, error) {
	if scope, hit, err := c.read(ctx, key, guard); hit {
		return scope, nil
	} else if err != nil {
		return c.loadAuthoritative(ctx, guard, loader)
	}

	leaseKey := key + ":lease"
	for {
		token := c.options.NewToken()
		acquired, err := c.acquireLease(ctx, leaseKey, token)
		if err != nil {
			return c.loadAuthoritative(ctx, guard, loader)
		}
		if acquired {
			return c.loadAsLeaseHolder(ctx, key, leaseKey, token, guard, loader)
		}

		for {
			if scope, hit, err := c.read(ctx, key, guard); hit {
				return scope, nil
			} else if err != nil {
				return c.loadAuthoritative(ctx, guard, loader)
			}
			ttl, err := c.leaseTTL(ctx, leaseKey)
			if errors.Is(err, readcache.ErrMiss) || ttl <= 0 {
				break
			}
			if err != nil {
				return c.loadAuthoritative(ctx, guard, loader)
			}
			wait := c.options.PollInterval
			if ttl < wait {
				wait = ttl
			}
			if err := c.options.Sleep(ctx, wait); err != nil {
				return nil, err
			}
		}
	}
}

func (c *Cache) loadAsLeaseHolder(ctx context.Context, key, leaseKey, token string, guard scopeGuard, loader ScopeLoader) (*Scope, error) {
	defer c.releaseLease(leaseKey, token)

	if scope, hit, err := c.read(ctx, key, guard); hit {
		return scope, nil
	} else if err != nil {
		return c.loadAuthoritative(ctx, guard, loader)
	}

	scope, err := c.loadAuthoritative(ctx, guard, loader)
	if err != nil {
		return nil, err
	}
	value, err := json.Marshal(scopeValueEnvelope{
		SchemaVersion: scopeCacheSchemaVersion,
		Guard:         guard,
		Scope:         scope,
	})
	if err != nil {
		return nil, fmt.Errorf("encode representative scope cache value: %w", err)
	}
	_ = c.set(ctx, key, value, c.valueTTL())
	return scope, nil
}

func (c *Cache) loadAuthoritative(ctx context.Context, guard scopeGuard, loader ScopeLoader) (*Scope, error) {
	scope, err := loader(ctx)
	if err != nil {
		return nil, err
	}
	if scope != nil {
		scope.Version = scopeVersion(guard)
	}
	if !validScopeForGuard(scope, guard) {
		return nil, fmt.Errorf("representative scope loader returned an invalid scope")
	}
	return scope, nil
}

func (c *Cache) read(ctx context.Context, key string, guard scopeGuard) (*Scope, bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	value, err := c.store.Get(commandCtx, key)
	if errors.Is(err, readcache.ErrMiss) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.DisallowUnknownFields()
	var envelope scopeValueEnvelope
	if err := decoder.Decode(&envelope); err != nil ||
		envelope.SchemaVersion != scopeCacheSchemaVersion ||
		envelope.Guard != guard ||
		!validScopeForGuard(envelope.Scope, guard) {
		return nil, false, nil
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, false, nil
	}
	return envelope.Scope, true, nil
}

func validScopeForGuard(scope *Scope, guard scopeGuard) bool {
	if scope == nil || scope.Version != scopeVersion(guard) || scope.ActorUserID != guard.ActorUserID {
		return false
	}
	if !scope.IsRepresentative {
		return len(scope.RepresentedDepartmentIDs) == 0 &&
			len(scope.Departments) == 0 &&
			len(scope.Subjects) == 0 &&
			len(scope.OverviewSubjects) == 0
	}
	if strings.TrimSpace(scope.ActorMemberExternalID) == "" || len(scope.RepresentedDepartmentIDs) == 0 {
		return false
	}
	for _, root := range scope.RepresentedDepartmentIDs {
		if strings.TrimSpace(root) == "" {
			return false
		}
	}
	for _, subject := range append(append([]Subject(nil), scope.Subjects...), scope.OverviewSubjects...) {
		if subject.SubjectType != "member" || subject.UserID < 0 {
			return false
		}
		if subject.Selectable && (subject.UserID <= 0 || subject.RelayUserID == nil) {
			return false
		}
	}
	return true
}

func (c *Cache) acquireLease(ctx context.Context, key, token string) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.TryAcquireLease(commandCtx, key, token, c.options.LeaseTTL)
}

func (c *Cache) leaseTTL(ctx context.Context, key string) (time.Duration, error) {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.LeaseTTL(commandCtx, key)
}

func (c *Cache) set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	commandCtx, cancel := context.WithTimeout(ctx, c.options.CommandTimeout)
	defer cancel()
	return c.store.Set(commandCtx, key, value, ttl)
}

func (c *Cache) releaseLease(key, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), c.options.ReleaseTimeout)
	defer cancel()
	_, _ = c.store.ReleaseLease(ctx, key, token)
}

func (c *Cache) valueTTL() time.Duration {
	random := c.options.RandFloat64()
	if random < 0 {
		random = 0
	} else if random > 1 {
		random = 1
	}
	const maximum = 60 * time.Minute
	return maximum - time.Duration((0.1+0.1*random)*float64(maximum))
}

func scopeCacheKey(namespace string, guard scopeGuard) string {
	return fmt.Sprintf(
		"ae:%s:representative-scope:v2:actor:%d:directory-source:%d:directory-run:%d:role:%s",
		namespace,
		guard.ActorUserID,
		guard.DirectorySourceID,
		guard.DirectoryRunID,
		guard.ActorRole,
	)
}
