package relayruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/httpclient"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const (
	maximumClientTTL   = 5 * time.Minute
	maximumMetadataTTL = 5 * time.Minute

	// longRunningOverallTimeout bounds one owner-account account operation end to
	// end: a reset is bounded at roughly 20 seconds upstream plus 8 seconds of
	// detached post-processing, and an invite at roughly 20 plus 8 and 3.
	longRunningOverallTimeout = 60 * time.Second
	// longRunningResponseHeaderTimeout leaves five seconds of that budget for
	// reading the response body.
	longRunningResponseHeaderTimeout = 55 * time.Second
)

var ErrStaleProviderConfiguration = errors.New("relay provider configuration is stale")

type ProviderFactory func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error)

type Options struct {
	Namespace      string
	Store          readcache.Store
	Bus            InvalidationBus
	ClientTTL      time.Duration
	MetadataTTL    time.Duration
	CommandTimeout time.Duration
	RefreshTimeout time.Duration
	LeaseTTL       time.Duration
	PollInterval   time.Duration
	ReleaseTimeout time.Duration
	Now            func() time.Time
	Factory        ProviderFactory
	// LongRunningFactory builds the provider used only by owner-account account
	// operations, which hold one upstream call for up to a minute. It is separate
	// from Factory so those calls cannot starve the shared runtime client pool.
	LongRunningFactory ProviderFactory
	// DirectoryLoadTimeout bounds one owner-account directory load. Values above
	// the hard ceiling are clamped so no configuration can hold a page.
	DirectoryLoadTimeout time.Duration
	NewToken             func() string
	Sleep                func(context.Context, time.Duration) error
	MetadataMetrics      readcache.Metrics
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

	mu                 sync.RWMutex
	clients            map[clientKey]clientEntry
	longRunningClients map[clientKey]clientEntry
	minimumVersion     map[int]int64
	latestVersion      map[int]int64
	metadataFlights    readcache.FlightGroup[[]byte]

	ownerAccountFlights     readcache.FlightGroup[[]relay.OwnerAccount]
	ownerAccountDirectories map[ownerAccountDirectoryKey]ownerAccountDirectoryEntry
	ownerAccountGeneration  map[int]int64
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
	if options.MetadataTTL <= 0 || options.MetadataTTL > maximumMetadataTTL {
		options.MetadataTTL = maximumMetadataTTL
	}
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
	if options.NewToken == nil {
		options.NewToken = uuid.NewString
	}
	if options.Sleep == nil {
		options.Sleep = readcache.Sleep
	}
	if options.Factory == nil {
		client := httpclient.NewDefault(30 * time.Second)
		options.Factory = func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error) {
			return relay.NewSub2apiProvider(client, row.BaseURL, adminAPIKey, row.DefaultModel, logger), nil
		}
	}
	if options.DirectoryLoadTimeout <= 0 || options.DirectoryLoadTimeout > ownerAccountDirectoryLoadDeadline {
		options.DirectoryLoadTimeout = ownerAccountDirectoryLoadDeadline
	}
	if options.LongRunningFactory == nil {
		client := httpclient.New(httpclient.Options{
			ConnectTimeout:        5 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ResponseHeaderTimeout: longRunningResponseHeaderTimeout,
			OverallTimeout:        longRunningOverallTimeout,
			IdleConnTimeout:       90 * time.Second,
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   20,
			MaxConnsPerHost:       50,
		})
		options.LongRunningFactory = func(row *ent.RelayProvider, adminAPIKey string) (relay.Provider, error) {
			return relay.NewSub2apiProvider(client, row.BaseURL, adminAPIKey, row.DefaultModel, logger), nil
		}
	}
	if options.Store != nil && !namespacePattern.MatchString(options.Namespace) {
		return nil, fmt.Errorf("invalid Redis namespace %q", options.Namespace)
	}
	return &Manager{
		client:                  client,
		encryptionKey:           strings.TrimSpace(encryptionKey),
		logger:                  logger,
		options:                 options,
		clients:                 make(map[clientKey]clientEntry),
		longRunningClients:      make(map[clientKey]clientEntry),
		minimumVersion:          make(map[int]int64),
		latestVersion:           make(map[int]int64),
		ownerAccountDirectories: make(map[ownerAccountDirectoryKey]ownerAccountDirectoryEntry),
		ownerAccountGeneration:  make(map[int]int64),
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

func (m *Manager) ResolveLongRunning(ctx context.Context, providerID int) (relay.Provider, error) {
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
	return m.ResolveLongRunningEntity(row)
}

// ResolveLongRunningEntity resolves the provider used only by owner-account
// account operations. It is cached independently, so the long-running client
// never displaces the shared runtime client of the same provider version.
func (m *Manager) ResolveLongRunningEntity(row *ent.RelayProvider) (relay.Provider, error) {
	return m.resolveEntity(row, true)
}

func (m *Manager) ResolveEntity(row *ent.RelayProvider) (relay.Provider, error) {
	return m.resolveEntity(row, false)
}

func (m *Manager) resolveEntity(row *ent.RelayProvider, longRunning bool) (relay.Provider, error) {
	if m == nil {
		return nil, fmt.Errorf("relay runtime is not configured")
	}
	if row == nil || row.ID <= 0 || row.ConfigurationVersion <= 0 {
		return nil, fmt.Errorf("valid relay provider row is required")
	}
	key := clientKey{providerID: row.ID, version: row.ConfigurationVersion}
	now := m.options.Now()
	clients := m.clients
	factory := m.options.Factory
	if longRunning {
		clients = m.longRunningClients
		factory = m.options.LongRunningFactory
	}

	m.mu.RLock()
	minimumVersion := m.minimumVersion[row.ID]
	latestVersion := m.latestVersion[row.ID]
	entry, ok := clients[key]
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
	created, err := factory(row, adminKey)
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
	if current, exists := clients[key]; exists && now.Sub(current.createdAt) <= m.options.ClientTTL {
		return current.provider, nil
	}
	for existingKey := range clients {
		if existingKey.providerID == row.ID {
			delete(clients, existingKey)
		}
	}
	m.latestVersion[row.ID] = row.ConfigurationVersion
	clients[key] = clientEntry{provider: created, createdAt: now}
	return created, nil
}

// NewUserScopedProvider creates an uncached provider for one user credential.
func (m *Manager) NewUserScopedProvider(row *ent.RelayProvider, apiKey, model string) (relay.Provider, error) {
	if m == nil {
		return nil, fmt.Errorf("relay runtime is not configured")
	}
	if row == nil || row.ID <= 0 || row.ConfigurationVersion <= 0 {
		return nil, fmt.Errorf("valid relay provider row is required")
	}
	if strings.TrimSpace(apiKey) == "" {
		return nil, fmt.Errorf("user API key is required")
	}

	scopedRow := *row
	scopedRow.DefaultModel = strings.TrimSpace(model)
	created, err := m.options.Factory(&scopedRow, apiKey)
	if err != nil {
		return nil, fmt.Errorf("create user-scoped relay provider %d version %d: %w", row.ID, row.ConfigurationVersion, err)
	}
	if created == nil {
		return nil, fmt.Errorf("create user-scoped relay provider %d version %d: nil provider", row.ID, row.ConfigurationVersion)
	}
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
	for key := range m.longRunningClients {
		if key.providerID == event.ProviderID {
			delete(m.longRunningClients, key)
		}
	}
}
