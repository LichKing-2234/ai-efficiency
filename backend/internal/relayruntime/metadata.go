package relayruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
)

const metadataSchemaVersion = 1

type metadataEnvelope[T any] struct {
	SchemaVersion int `json:"schema_version"`
	Items         []T `json:"items"`
}

type MetadataLoader func(context.Context) ([]relay.ModelOption, error)

func (m *Manager) ListAllowedGroupsForUser(ctx context.Context, providerID int, configurationVersion, userID int64) ([]relay.Group, error) {
	if providerID <= 0 || configurationVersion <= 0 || userID <= 0 {
		return nil, fmt.Errorf("provider ID, configuration version, and relay user ID must be positive")
	}
	row, err := m.client.RelayProvider.Get(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("get relay provider %d: %w", providerID, err)
	}
	if row.ConfigurationVersion != configurationVersion {
		return nil, fmt.Errorf("%w: provider %d version %d", ErrStaleProviderConfiguration, providerID, configurationVersion)
	}
	provider, err := m.ResolveEntity(row)
	if err != nil {
		return nil, err
	}
	user, err := provider.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get current Relay user %d: %w", userID, err)
	}
	if user == nil {
		return []relay.Group{}, nil
	}

	groupsByID := make(map[int64]relay.Group, len(user.AllowedGroups)+len(user.AllowedGroupIDs))
	allowedIDs := make(map[int64]struct{}, len(user.AllowedGroups)+len(user.AllowedGroupIDs))
	needsMetadata := false
	for _, group := range user.AllowedGroups {
		if group.ID <= 0 {
			continue
		}
		allowedIDs[group.ID] = struct{}{}
		sanitized := sanitizeGroup(group)
		if sanitized.Platform == "" || sanitized.Name == "" {
			needsMetadata = true
		}
		mergeGroup(groupsByID, sanitized)
	}
	for _, groupID := range user.AllowedGroupIDs {
		if groupID <= 0 {
			continue
		}
		allowedIDs[groupID] = struct{}{}
		needsMetadata = true
	}
	if len(allowedIDs) == 0 {
		return []relay.Group{}, nil
	}

	if needsMetadata {
		lister, ok := provider.(relay.PlatformGroupLister)
		if !ok {
			groups, err := provider.ListAllowedGroupsForUser(ctx, userID)
			if err != nil {
				return nil, fmt.Errorf("list current allowed groups: %w", err)
			}
			return sanitizeAndSortGroups(groups), nil
		}
		metadata, err := m.platformGroups(ctx, row, lister)
		if err != nil {
			return nil, err
		}
		for _, group := range metadata {
			if _, allowed := allowedIDs[group.ID]; !allowed {
				continue
			}
			if isSubscriptionGroup(group) {
				if _, subscribed := groupsByID[group.ID]; !subscribed {
					continue
				}
			}
			mergeGroup(groupsByID, group)
		}
	}

	groups := make([]relay.Group, 0, len(groupsByID))
	for _, group := range groupsByID {
		if group.ID > 0 && strings.TrimSpace(group.Platform) != "" {
			groups = append(groups, sanitizeGroup(group))
		}
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	return cloneGroups(groups), nil
}

func (m *Manager) Models(ctx context.Context, row *ent.RelayProvider, platform, groupID string, loader MetadataLoader) ([]relay.ModelOption, error) {
	if row == nil || row.ID <= 0 || row.ConfigurationVersion <= 0 {
		return nil, fmt.Errorf("valid relay provider row is required")
	}
	platform = strings.TrimSpace(platform)
	groupID = strings.TrimSpace(groupID)
	if platform == "" || groupID == "" {
		return nil, fmt.Errorf("platform and group ID are required")
	}
	if loader == nil {
		return nil, fmt.Errorf("provider model loader is required")
	}
	current, err := m.client.RelayProvider.Get(ctx, row.ID)
	if err != nil {
		return nil, fmt.Errorf("get relay provider %d: %w", row.ID, err)
	}
	if current.ConfigurationVersion != row.ConfigurationVersion {
		return nil, fmt.Errorf("%w: provider %d version %d", ErrStaleProviderConfiguration, row.ID, row.ConfigurationVersion)
	}
	key := m.metadataKey("models", row.ID, row.ConfigurationVersion, platform, groupID)
	raw, err := m.loadMetadata(ctx, key, validModelEnvelope, func(loadCtx context.Context) ([]byte, error) {
		models, err := loader(loadCtx)
		if err != nil {
			return nil, fmt.Errorf("load provider models: %w", err)
		}
		models = sanitizeModels(models)
		encoded, err := json.Marshal(metadataEnvelope[relay.ModelOption]{SchemaVersion: metadataSchemaVersion, Items: models})
		if err != nil {
			return nil, fmt.Errorf("encode provider models: %w", err)
		}
		return encoded, nil
	})
	if err != nil {
		return nil, err
	}
	envelope, err := decodeMetadataEnvelope[relay.ModelOption](raw, validModels)
	if err != nil {
		return nil, fmt.Errorf("decode provider models: %w", err)
	}
	return cloneModels(envelope.Items), nil
}

func (m *Manager) platformGroups(ctx context.Context, row *ent.RelayProvider, lister relay.PlatformGroupLister) ([]relay.Group, error) {
	key := m.metadataKey("groups", row.ID, row.ConfigurationVersion, "", "")
	raw, err := m.loadMetadata(ctx, key, validGroupEnvelope, func(loadCtx context.Context) ([]byte, error) {
		groups, err := lister.ListPlatformGroups(loadCtx)
		if err != nil {
			return nil, fmt.Errorf("load provider group metadata: %w", err)
		}
		groups = sanitizeAndSortGroups(groups)
		encoded, err := json.Marshal(metadataEnvelope[relay.Group]{SchemaVersion: metadataSchemaVersion, Items: groups})
		if err != nil {
			return nil, fmt.Errorf("encode provider group metadata: %w", err)
		}
		return encoded, nil
	})
	if err != nil {
		return nil, err
	}
	envelope, err := decodeMetadataEnvelope[relay.Group](raw, validGroups)
	if err != nil {
		return nil, fmt.Errorf("decode provider group metadata: %w", err)
	}
	return cloneGroups(envelope.Items), nil
}

func (m *Manager) metadataKey(kind string, providerID int, version int64, platform, groupID string) string {
	canonical := strings.Join([]string{
		m.options.Namespace,
		kind,
		strconv.Itoa(providerID),
		strconv.FormatInt(version, 10),
		strings.ToLower(strings.TrimSpace(platform)),
		strings.TrimSpace(groupID),
	}, "\x00")
	digest := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%s:relay-metadata:v1:%s", m.options.Namespace, hex.EncodeToString(digest[:]))
}

func (m *Manager) loadMetadata(ctx context.Context, key string, valid func([]byte) bool, loader func(context.Context) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.metadataFlights.Do(ctx, key, m.options.RefreshTimeout, func(sharedCtx context.Context) ([]byte, error) {
		if m.options.Store == nil {
			return m.loadMetadataAuthoritative(sharedCtx, loader)
		}
		return m.loadMetadataWithLease(sharedCtx, key, valid, loader)
	})
}

func (m *Manager) loadMetadataWithLease(ctx context.Context, key string, valid func([]byte) bool, loader func(context.Context) ([]byte, error)) ([]byte, error) {
	if raw, hit, err := m.readMetadata(ctx, key, valid); hit {
		m.recordMetadata("fresh")
		return raw, nil
	} else if err != nil {
		m.recordMetadata("error")
		return m.loadMetadataAuthoritative(ctx, loader)
	}
	m.recordMetadata("miss")

	leaseKey := key + ":lease"
	waitLimit := m.options.RefreshTimeout / 3
	if waitLimit > time.Second {
		waitLimit = time.Second
	}
	waited := time.Duration(0)
	for {
		token := m.options.NewToken()
		acquired, err := m.acquireMetadataLease(ctx, leaseKey, token)
		if err != nil {
			m.recordMetadata("error")
			m.recordMetadata("lease_failed")
			return m.loadMetadataAuthoritative(ctx, loader)
		}
		if acquired {
			m.recordMetadata("lease_acquired")
			return m.loadMetadataAsLeaseHolder(ctx, key, leaseKey, token, valid, loader)
		}
		m.recordMetadata("lease_wait")
		for {
			if raw, hit, err := m.readMetadata(ctx, key, valid); hit {
				m.recordMetadata("fresh")
				return raw, nil
			} else if err != nil {
				m.recordMetadata("error")
				return m.loadMetadataAuthoritative(ctx, loader)
			}
			ttl, err := m.metadataLeaseTTL(ctx, leaseKey)
			if errors.Is(err, readcache.ErrMiss) {
				break
			}
			if err != nil {
				m.recordMetadata("error")
				m.recordMetadata("lease_failed")
				return m.loadMetadataAuthoritative(ctx, loader)
			}
			if ttl <= 0 {
				break
			}
			remaining := waitLimit - waited
			if remaining <= 0 {
				return m.loadMetadataAuthoritative(ctx, loader)
			}
			wait := m.options.PollInterval
			if ttl < wait {
				wait = ttl
			}
			if remaining < wait {
				wait = remaining
			}
			if err := m.options.Sleep(ctx, wait); err != nil {
				return nil, err
			}
			waited += wait
		}
	}
}

func (m *Manager) loadMetadataAsLeaseHolder(ctx context.Context, key, leaseKey, token string, valid func([]byte) bool, loader func(context.Context) ([]byte, error)) ([]byte, error) {
	defer m.releaseMetadataLease(leaseKey, token)
	if raw, hit, err := m.readMetadata(ctx, key, valid); hit {
		m.recordMetadata("fresh")
		return raw, nil
	} else if err != nil {
		m.recordMetadata("error")
		return m.loadMetadataAuthoritative(ctx, loader)
	}
	raw, err := m.loadMetadataAuthoritative(ctx, loader)
	if err != nil {
		return nil, err
	}
	if !valid(raw) {
		m.recordMetadata("error")
		return nil, fmt.Errorf("provider metadata loader returned invalid content")
	}
	if err := m.setMetadata(ctx, key, raw); err != nil {
		m.recordMetadata("error")
	}
	return append([]byte(nil), raw...), nil
}

func (m *Manager) loadMetadataAuthoritative(ctx context.Context, loader func(context.Context) ([]byte, error)) ([]byte, error) {
	m.recordMetadata("refresh")
	raw, err := loader(ctx)
	if err != nil {
		m.recordMetadata("error")
		return nil, err
	}
	return raw, nil
}

func (m *Manager) readMetadata(ctx context.Context, key string, valid func([]byte) bool) ([]byte, bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, m.options.CommandTimeout)
	defer cancel()
	raw, err := m.options.Store.Get(commandCtx, key)
	if errors.Is(err, readcache.ErrMiss) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !valid(raw) {
		return nil, false, nil
	}
	return append([]byte(nil), raw...), true, nil
}

func (m *Manager) setMetadata(ctx context.Context, key string, raw []byte) error {
	commandCtx, cancel := context.WithTimeout(ctx, m.options.CommandTimeout)
	defer cancel()
	return m.options.Store.Set(commandCtx, key, append([]byte(nil), raw...), m.metadataTTL(key))
}

func (m *Manager) metadataTTL(key string) time.Duration {
	digest := sha256.Sum256([]byte(key))
	jitterPermille := int64(100 + int(digest[0])*100/255)
	return m.options.MetadataTTL - time.Duration(int64(m.options.MetadataTTL)*jitterPermille/1000)
}

func (m *Manager) acquireMetadataLease(ctx context.Context, key, token string) (bool, error) {
	commandCtx, cancel := context.WithTimeout(ctx, m.options.CommandTimeout)
	defer cancel()
	return m.options.Store.TryAcquireLease(commandCtx, key, token, m.options.LeaseTTL)
}

func (m *Manager) metadataLeaseTTL(ctx context.Context, key string) (time.Duration, error) {
	commandCtx, cancel := context.WithTimeout(ctx, m.options.CommandTimeout)
	defer cancel()
	return m.options.Store.LeaseTTL(commandCtx, key)
}

func (m *Manager) releaseMetadataLease(key, token string) {
	ctx, cancel := context.WithTimeout(context.Background(), m.options.ReleaseTimeout)
	defer cancel()
	released, err := m.options.Store.ReleaseLease(ctx, key, token)
	if err != nil {
		m.recordMetadata("error")
	}
	if err != nil || !released {
		m.recordMetadata("lease_failed")
	}
}

func (m *Manager) recordMetadata(outcome string) {
	if m != nil && m.options.MetadataMetrics != nil {
		m.options.MetadataMetrics.Record(outcome)
	}
}

func validGroupEnvelope(raw []byte) bool {
	_, err := decodeMetadataEnvelope[relay.Group](raw, validGroups)
	return err == nil
}

func validModelEnvelope(raw []byte) bool {
	_, err := decodeMetadataEnvelope[relay.ModelOption](raw, validModels)
	return err == nil
}

func decodeMetadataEnvelope[T any](raw []byte, valid func([]T) bool) (metadataEnvelope[T], error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope metadataEnvelope[T]
	if err := decoder.Decode(&envelope); err != nil {
		return metadataEnvelope[T]{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return metadataEnvelope[T]{}, fmt.Errorf("trailing content")
	}
	if envelope.SchemaVersion != metadataSchemaVersion || envelope.Items == nil || !valid(envelope.Items) {
		return metadataEnvelope[T]{}, fmt.Errorf("invalid metadata envelope")
	}
	return envelope, nil
}

func sanitizeAndSortGroups(groups []relay.Group) []relay.Group {
	out := make([]relay.Group, 0, len(groups))
	seen := make(map[int64]struct{}, len(groups))
	for _, group := range groups {
		group = sanitizeGroup(group)
		if group.ID <= 0 || group.Platform == "" {
			continue
		}
		if _, exists := seen[group.ID]; exists {
			continue
		}
		seen[group.ID] = struct{}{}
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func sanitizeGroup(group relay.Group) relay.Group {
	return relay.Group{
		ID:               group.ID,
		Name:             strings.TrimSpace(group.Name),
		Platform:         strings.TrimSpace(group.Platform),
		IsExclusive:      group.IsExclusive,
		SubscriptionType: strings.TrimSpace(group.SubscriptionType),
	}
}

func mergeGroup(groups map[int64]relay.Group, incoming relay.Group) {
	if incoming.ID <= 0 {
		return
	}
	existing := groups[incoming.ID]
	if existing.ID == 0 {
		groups[incoming.ID] = incoming
		return
	}
	if existing.Name == "" {
		existing.Name = incoming.Name
	}
	if existing.Platform == "" {
		existing.Platform = incoming.Platform
	}
	if !existing.IsExclusive {
		existing.IsExclusive = incoming.IsExclusive
	}
	if existing.SubscriptionType == "" {
		existing.SubscriptionType = incoming.SubscriptionType
	}
	groups[incoming.ID] = existing
}

func isSubscriptionGroup(group relay.Group) bool {
	return strings.EqualFold(group.SubscriptionType, "subscription")
}

func validGroups(groups []relay.Group) bool {
	for _, group := range groups {
		if group.ID <= 0 || strings.TrimSpace(group.Platform) == "" || group.DailyLimitUSD != nil || group.WeeklyLimitUSD != nil || group.MonthlyLimitUSD != nil || group.RateMultiplier != nil {
			return false
		}
	}
	return true
}

func sanitizeModels(models []relay.ModelOption) []relay.ModelOption {
	out := make([]relay.ModelOption, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model.ID = strings.TrimSpace(model.ID)
		model.DisplayName = strings.TrimSpace(model.DisplayName)
		if model.ID == "" {
			continue
		}
		if _, exists := seen[model.ID]; exists {
			continue
		}
		seen[model.ID] = struct{}{}
		out = append(out, model)
	}
	return out
}

func validModels(models []relay.ModelOption) bool {
	for _, model := range models {
		if strings.TrimSpace(model.ID) == "" {
			return false
		}
	}
	return true
}

func cloneGroups(groups []relay.Group) []relay.Group {
	return append([]relay.Group(nil), groups...)
}

func cloneModels(models []relay.ModelOption) []relay.ModelOption {
	return append([]relay.ModelOption(nil), models...)
}
