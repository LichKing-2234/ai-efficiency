package relayruntime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	// ownerAccountDirectoryTTL bounds how long one owner-account directory read is
	// reused. It matches the metadata cache ceiling: the directory carries
	// per-account snapshots that a mutation invalidates explicitly, so a longer TTL
	// would only delay an unrelated provider change.
	ownerAccountDirectoryTTL = 5 * time.Minute
	// ownerAccountDirectoryLoadDeadline bounds one directory load. A hit never
	// waits, and a load that exceeds this serves the last good directory instead of
	// holding the page.
	ownerAccountDirectoryLoadDeadline = 10 * time.Second
)

type ownerAccountDirectoryKey struct {
	providerID           int
	configurationVersion int64
	platform             string
}

func (k ownerAccountDirectoryKey) flightKey() string {
	return fmt.Sprintf("%d\x00%d\x00%s", k.providerID, k.configurationVersion, k.platform)
}

type ownerAccountDirectoryEntry struct {
	accounts []relay.OwnerAccount
	loadedAt time.Time
}

// OwnerAccounts returns the owner-safe account directory of one platform. The
// result is cached for five minutes per provider, configuration version and
// platform, concurrent misses share one upstream load, and a failed reload serves
// the last good directory instead of failing the page.
func (m *Manager) OwnerAccounts(ctx context.Context, providerID int, configurationVersion int64, platform string) ([]relay.OwnerAccount, error) {
	if m == nil || m.client == nil {
		return nil, fmt.Errorf("relay runtime is not configured")
	}
	if providerID <= 0 || configurationVersion <= 0 {
		return nil, fmt.Errorf("provider ID and configuration version must be positive")
	}
	platform = normalizeDirectoryPlatform(platform)
	if platform == "" {
		return nil, fmt.Errorf("platform is required")
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
	directory, ok := provider.(relay.OwnerAccountDirectoryReader)
	if !ok {
		return nil, fmt.Errorf("%w: relay provider %d cannot list owner accounts", relay.ErrCapabilityUnsupported, providerID)
	}

	key := ownerAccountDirectoryKey{providerID: providerID, configurationVersion: configurationVersion, platform: platform}
	if accounts, ok := m.readOwnerAccounts(key, m.options.Now()); ok {
		return accounts, nil
	}
	return m.loadOwnerAccounts(ctx, key, directory)
}

// InvalidateOwnerAccounts drops the cached directory of one provider and
// platform. An empty platform invalidates every platform of that provider. The
// next read for an invalidated entry reloads instead of serving pre-mutation
// snapshots, including a load that was already in flight.
func (m *Manager) InvalidateOwnerAccounts(providerID int, platform string) {
	if m == nil || providerID <= 0 {
		return
	}
	platform = normalizeDirectoryPlatform(platform)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ownerAccountGeneration[providerID]++
	for key := range m.ownerAccountDirectories {
		if key.providerID != providerID {
			continue
		}
		if platform != "" && key.platform != platform {
			continue
		}
		delete(m.ownerAccountDirectories, key)
	}
}

func (m *Manager) loadOwnerAccounts(ctx context.Context, key ownerAccountDirectoryKey, directory relay.OwnerAccountDirectoryReader) ([]relay.OwnerAccount, error) {
	m.mu.RLock()
	generation := m.ownerAccountGeneration[key.providerID]
	m.mu.RUnlock()

	accounts, err := m.ownerAccountFlights.Do(ctx, key.flightKey(), m.options.DirectoryLoadTimeout, func(loadCtx context.Context) ([]relay.OwnerAccount, error) {
		loaded, err := directory.ListOwnerAccounts(loadCtx, key.platform)
		if err != nil {
			return nil, err
		}
		m.storeOwnerAccounts(key, loaded, generation)
		return loaded, nil
	})
	if err != nil {
		// A caller that gave up is not a cache failure; surfacing the cancellation
		// keeps request deadlines honest instead of masking them with stale data.
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if stale, ok := m.readStaleOwnerAccounts(key); ok {
			return stale, nil
		}
		return nil, fmt.Errorf("load owner account directory for provider %d platform %s: %w", key.providerID, key.platform, err)
	}
	return cloneOwnerAccounts(accounts), nil
}

func (m *Manager) readOwnerAccounts(key ownerAccountDirectoryKey, now time.Time) ([]relay.OwnerAccount, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.ownerAccountDirectories[key]
	if !ok {
		return nil, false
	}
	if now.Sub(entry.loadedAt) > ownerAccountDirectoryTTL {
		return nil, false
	}
	return cloneOwnerAccounts(entry.accounts), true
}

// readStaleOwnerAccounts returns the last good directory regardless of its age.
// It is only reached after a reload failed, never on the happy path.
func (m *Manager) readStaleOwnerAccounts(key ownerAccountDirectoryKey) ([]relay.OwnerAccount, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.ownerAccountDirectories[key]
	if !ok {
		return nil, false
	}
	return cloneOwnerAccounts(entry.accounts), true
}

func (m *Manager) storeOwnerAccounts(key ownerAccountDirectoryKey, accounts []relay.OwnerAccount, generation int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// A directory load that started before an invalidation must not repopulate the
	// entry with pre-mutation snapshots.
	if m.ownerAccountGeneration[key.providerID] != generation {
		return
	}
	for existing := range m.ownerAccountDirectories {
		if existing.providerID == key.providerID && existing.configurationVersion != key.configurationVersion {
			delete(m.ownerAccountDirectories, existing)
		}
	}
	m.ownerAccountDirectories[key] = ownerAccountDirectoryEntry{
		accounts: cloneOwnerAccounts(accounts),
		loadedAt: m.options.Now(),
	}
}

func normalizeDirectoryPlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

func cloneOwnerAccounts(accounts []relay.OwnerAccount) []relay.OwnerAccount {
	if accounts == nil {
		return []relay.OwnerAccount{}
	}
	out := make([]relay.OwnerAccount, len(accounts))
	copy(out, accounts)
	for index := range out {
		if out[index].Groups != nil {
			out[index].Groups = append([]relay.OwnerAccountGroup(nil), out[index].Groups...)
		}
	}
	return out
}
