package activity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/readcache"
)

const activityCacheSchemaVersion = 1

var activityCacheNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type CacheOptions struct {
	Namespace string
	TTL       time.Duration
}

type Cache struct {
	store     readcache.Store
	namespace string
	ttl       time.Duration
}

type cacheDimensions struct {
	SchemaVersion  int    `json:"schema_version"`
	Contract       string `json:"contract"`
	Kind           string `json:"kind"`
	ScopeVersion   string `json:"scope_version"`
	ActorUserID    int    `json:"actor_user_id"`
	Admin          bool   `json:"admin"`
	Representative bool   `json:"representative"`
	Subject        string `json:"subject"`
	FromUnixNano   int64  `json:"from_unix_nano"`
	ToUnixNano     int64  `json:"to_unix_nano"`
	PageOptions    any    `json:"page_options"`
}

func NewCache(store readcache.Store, options CacheOptions) (*Cache, error) {
	if store == nil {
		return nil, fmt.Errorf("activity cache store is required")
	}
	options.Namespace = strings.TrimSpace(options.Namespace)
	if !activityCacheNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Activity cache namespace %q", options.Namespace)
	}
	if options.TTL <= 0 {
		options.TTL = 15 * time.Second
	}
	return &Cache{store: store, namespace: options.Namespace, ttl: options.TTL}, nil
}

func (c *Cache) key(dimensions cacheDimensions) string {
	payload, _ := json.Marshal(dimensions)
	digest := sha256.Sum256(payload)
	return c.namespace + ":activity:read:v1:" + hex.EncodeToString(digest[:])
}

func (c *Cache) read(ctx context.Context, dimensions cacheDimensions, target any) bool {
	if c == nil || c.store == nil || target == nil {
		return false
	}
	payload, err := c.store.Get(ctx, c.key(dimensions))
	if err != nil || len(payload) == 0 {
		return false
	}
	return json.Unmarshal(payload, target) == nil
}

func (c *Cache) write(ctx context.Context, dimensions cacheDimensions, value any) {
	if c == nil || c.store == nil || value == nil {
		return
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return
	}
	_ = c.store.Set(ctx, c.key(dimensions), payload, c.ttl)
}

func activityCacheDimensions(kind string, authorization *authorizationScope, actorUserID int, subject string, window Window, pages any) cacheDimensions {
	return cacheDimensions{
		SchemaVersion: activityCacheSchemaVersion,
		Contract:      MetricContractVersion,
		Kind:          kind, ScopeVersion: authorization.Version, ActorUserID: actorUserID,
		Admin: authorization.Admin, Representative: authorization.Representative,
		Subject: subject, FromUnixNano: window.From.UnixNano(), ToUnixNano: window.To.UnixNano(), PageOptions: pages,
	}
}

func (c *Cache) ReadMemberDenominator(ctx context.Context, key V2MemberDenominatorCacheKey, target *V2Denominator) bool {
	return c.read(ctx, cacheDimensions{SchemaVersion: activityCacheSchemaVersion, Contract: V2MetricContractVersion, Kind: "member_denominator", ScopeVersion: key.ScopeVersion, ActorUserID: key.ActorUserID, Subject: fmt.Sprintf("member:%d", key.SubjectUserID), PageOptions: key}, target)
}

func (c *Cache) WriteMemberDenominator(ctx context.Context, key V2MemberDenominatorCacheKey, value V2Denominator) {
	c.write(ctx, cacheDimensions{SchemaVersion: activityCacheSchemaVersion, Contract: V2MetricContractVersion, Kind: "member_denominator", ScopeVersion: key.ScopeVersion, ActorUserID: key.ActorUserID, Subject: fmt.Sprintf("member:%d", key.SubjectUserID), PageOptions: key}, value)
}
