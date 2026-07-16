package relayruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"go.uber.org/zap"
)

const maximumClientTTL = 5 * time.Minute

var ErrStaleProviderConfiguration = errors.New("relay provider configuration is stale")

type ProviderFactory func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error)

type Options struct {
	Namespace string
	Store     readcache.Store
	Bus       InvalidationBus
	ClientTTL time.Duration
	Now       func() time.Time
	Factory   ProviderFactory
}

type clientKey struct {
	providerID int
	version    int64
}

type clientEntry struct {
	provider  relay.Provider
	createdAt time.Time
}

type Manager struct {
	client        *ent.Client
	encryptionKey string
	logger        *zap.Logger
	options       Options

	mu             sync.RWMutex
	clients        map[clientKey]clientEntry
	minimumVersion map[int]int64
	latestVersion  map[int]int64
}

func NewManager(client *ent.Client, encryptionKey string, logger *zap.Logger, options Options) (*Manager, error) {
	if client == nil {
		return nil, fmt.Errorf("relay runtime Ent client is required")
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	if options.ClientTTL <= 0 || options.ClientTTL > maximumClientTTL {
		options.ClientTTL = maximumClientTTL
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Factory == nil {
		options.Factory = func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error) {
			return relay.NewSub2apiProvider(http.DefaultClient, row.BaseURL, adminAPIKey, row.DefaultModel, logger), nil
		}
	}
	if options.Store != nil && !namespacePattern.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	return &Manager{
		client:         client,
		encryptionKey:  strings.TrimSpace(encryptionKey),
		logger:         logger,
		options:        options,
		clients:        make(map[clientKey]clientEntry),
		minimumVersion: make(map[int]int64),
		latestVersion:  make(map[int]int64),
	}, nil
}

func (m *Manager) Start(ctx context.Context) {
	if m == nil || m.options.Bus == nil || ctx == nil {
		return
	}
	go func() {
		if err := m.options.Bus.Subscribe(ctx, m.handleInvalidation); err != nil && ctx.Err() == nil {
			m.logger.Warn("relay provider invalidation subscription stopped", zap.Error(err))
		}
	}()
}

func (m *Manager) Resolve(ctx context.Context, providerID int) (relay.Provider, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("relay runtime is not configured")
	}
	if providerID <= 0 {
		return nil, fmt.Errorf("relay provider ID must be positive")
	}
	row, err := m.client.RelayProvider.Get(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("get relay provider %d: %w", providerID, err)
	}
	return m.ResolveEntity(row)
}

func (m *Manager) ResolveEntity(row *ent.RelayProvider) (relay.Provider, error) {
	if m == nil {
		return nil, fmt.Errorf("relay runtime is not configured")
	}
	if row == nil || row.ID <= 0 || row.ConfigurationVersion <= 0 {
		return nil, fmt.Errorf("valid relay provider row is required")
	}
	key := clientKey{providerID: row.ID, version: row.ConfigurationVersion}
	now := m.options.Now()

	m.mu.RLock()
	minimumVersion := m.minimumVersion[row.ID]
	latestVersion := m.latestVersion[row.ID]
	entry, ok := m.clients[key]
	m.mu.RUnlock()
	if row.ConfigurationVersion < minimumVersion || row.ConfigurationVersion < latestVersion {
		return nil, fmt.Errorf("%w: provider %d version %d", ErrStaleProviderConfiguration, row.ID, row.ConfigurationVersion)
	}
	if ok && now.Sub(entry.createdAt) <= m.options.ClientTTL {
		return entry.provider, nil
	}

	adminKey, err := pkg.Decrypt(row.AdminAPIKey, m.encryptionKey)
	if err != nil {
		m.logger.Warn("relay provider admin key is not encrypted with the current key", zap.Int("provider_id", row.ID), zap.Error(err))
		adminKey = row.AdminAPIKey
	}
	created, err := m.options.Factory(row, adminKey)
	if err != nil {
		return nil, fmt.Errorf("create relay provider %d version %d: %w", row.ID, row.ConfigurationVersion, err)
	}
	if created == nil {
		return nil, fmt.Errorf("create relay provider %d version %d: nil provider", row.ID, row.ConfigurationVersion)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if row.ConfigurationVersion < m.minimumVersion[row.ID] || row.ConfigurationVersion < m.latestVersion[row.ID] {
		return nil, fmt.Errorf("%w: provider %d version %d", ErrStaleProviderConfiguration, row.ID, row.ConfigurationVersion)
	}
	if current, exists := m.clients[key]; exists && now.Sub(current.createdAt) <= m.options.ClientTTL {
		return current.provider, nil
	}
	for existingKey := range m.clients {
		if existingKey.providerID == row.ID {
			delete(m.clients, existingKey)
		}
	}
	m.latestVersion[row.ID] = row.ConfigurationVersion
	m.clients[key] = clientEntry{provider: created, createdAt: now}
	return created, nil
}

func (m *Manager) Invalidate(ctx context.Context, providerID int, configurationVersion int64) error {
	event := InvalidationEvent{
		SchemaVersion:        invalidationSchemaVersion,
		ProviderID:           providerID,
		ConfigurationVersion: configurationVersion,
	}
	if err := validateInvalidation(event); err != nil {
		return err
	}
	m.handleInvalidation(event)
	if m.options.Bus == nil {
		return nil
	}
	if err := m.options.Bus.Publish(ctx, event); err != nil {
		return fmt.Errorf("publish relay provider %d version %d invalidation: %w", providerID, configurationVersion, err)
	}
	return nil
}

func (m *Manager) handleInvalidation(event InvalidationEvent) {
	if err := validateInvalidation(event); err != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.ConfigurationVersion > m.minimumVersion[event.ProviderID] {
		m.minimumVersion[event.ProviderID] = event.ConfigurationVersion
	}
	if event.ConfigurationVersion > m.latestVersion[event.ProviderID] {
		m.latestVersion[event.ProviderID] = event.ConfigurationVersion
	}
	for key := range m.clients {
		if key.providerID == event.ProviderID {
			delete(m.clients, key)
		}
	}
}
