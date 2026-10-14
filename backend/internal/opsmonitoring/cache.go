package opsmonitoring

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

const (
	opsMonitoringCacheSchemaVersion = 1

	// groupsCacheTTL keeps stale snapshots readable so a page load can answer
	// immediately from cache while a refresh runs in the background.
	groupsCacheTTL = 10 * time.Minute
	// proxyEgressCacheTTL matches the static nature of a fixed egress address.
	proxyEgressCacheTTL = 6 * time.Hour
)

var opsMonitoringCacheNamespaceRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,62}$`)

type CacheOptions struct {
	Namespace string
}

// Cache stores reconstructible Ops Monitoring read models. Every entry is a
// cache of upstream facts, never authority.
type Cache struct {
	store     readcache.Store
	namespace string
}

func NewCache(store readcache.Store, options CacheOptions) (*Cache, error) {
	if store == nil {
		return nil, fmt.Errorf("ops monitoring cache store is required")
	}
	options.Namespace = strings.TrimSpace(options.Namespace)
	if !opsMonitoringCacheNamespaceRE.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Ops Monitoring cache namespace %q", options.Namespace)
	}
	return &Cache{store: store, namespace: options.Namespace}, nil
}

func (c *Cache) key(kind string, dimensions any) string {
	payload, _ := json.Marshal(dimensions)
	digest := sha256.Sum256(payload)
	return c.namespace + ":opsmonitoring:" + kind + ":v1:" + hex.EncodeToString(digest[:])
}

type groupsCacheDimensions struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	ProviderID    int    `json:"provider_id"`
	Window        string `json:"window"`
}

func (c *Cache) readGroups(ctx context.Context, dimensions groupsCacheDimensions) (*GroupsSnapshot, bool) {
	if c == nil || c.store == nil {
		return nil, false
	}
	payload, err := c.store.Get(ctx, c.key("groups", dimensions))
	if err != nil || len(payload) == 0 {
		return nil, false
	}
	var snapshot GroupsSnapshot
	if err := json.Unmarshal(payload, &snapshot); err != nil {
		return nil, false
	}
	return &snapshot, true
}

func (c *Cache) writeGroups(ctx context.Context, dimensions groupsCacheDimensions, snapshot *GroupsSnapshot) {
	if c == nil || c.store == nil || snapshot == nil {
		return
	}
	// Cached snapshots are never served as "refreshing"; that state belongs to
	// the read that decided a refresh was due.
	stored := *snapshot
	stored.Refreshing = false
	payload, err := json.Marshal(stored)
	if err != nil {
		return
	}
	_ = c.store.Set(ctx, c.key("groups", dimensions), payload, groupsCacheTTL)
}

type proxyEgressCacheDimensions struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	ProviderID    int    `json:"provider_id"`
	ProxyID       int64  `json:"proxy_id"`
}

func (c *Cache) readProxyEgress(ctx context.Context, providerID int, proxyID int64) (*ProxyEgressRecord, bool) {
	if c == nil || c.store == nil {
		return nil, false
	}
	dimensions := proxyEgressCacheDimensions{
		SchemaVersion: opsMonitoringCacheSchemaVersion,
		Kind:          "proxy_egress",
		ProviderID:    providerID,
		ProxyID:       proxyID,
	}
	payload, err := c.store.Get(ctx, c.key("proxy_egress", dimensions))
	if err != nil || len(payload) == 0 {
		return nil, false
	}
	var record ProxyEgressRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return nil, false
	}
	return &record, true
}

func (c *Cache) writeProxyEgress(ctx context.Context, providerID int, record *ProxyEgressRecord) {
	if c == nil || c.store == nil || record == nil {
		return
	}
	dimensions := proxyEgressCacheDimensions{
		SchemaVersion: opsMonitoringCacheSchemaVersion,
		Kind:          "proxy_egress",
		ProviderID:    providerID,
		ProxyID:       record.ProxyID,
	}
	payload, err := json.Marshal(record)
	if err != nil {
		return
	}
	_ = c.store.Set(ctx, c.key("proxy_egress", dimensions), payload, proxyEgressCacheTTL)
}
