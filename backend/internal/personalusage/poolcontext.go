package personalusage

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/ent/relaygroupmapping"
	"github.com/ai-efficiency/backend/internal/readcache"
	"github.com/ai-efficiency/backend/internal/relay"
)

const (
	// poolContextTTL bounds how stale the managed-pool directory may be. The
	// directory is a provider-wide read, so it must never run per request.
	poolContextTTL = 5 * time.Minute
	// poolContextLoadTimeout covers listing every provider account and user.
	poolContextLoadTimeout = 60 * time.Second
)

// poolContext is the managed-pool directory: the upstream accounts behind each
// mapped relay group and the provider users observed in it. Only mapped groups
// are present, so platform access groups never appear.
type poolContext struct {
	accounts map[int64][]relay.UserUsageGroupPoolUsageAccount
	members  map[int64][]string
}

type poolContextEntry struct {
	value    *poolContext
	loadedAt time.Time
}

// poolContextCache keys by provider and provider version so a multi-provider
// deployment never serves one provider's directory for another.
type poolContextCache struct {
	mu      sync.Mutex
	entries map[string]*poolContextEntry
	flight  readcache.FlightGroup[*poolContext]
}

func (c *poolContextCache) get(ctx context.Context, key string, load func(context.Context) (*poolContext, error)) (*poolContext, error) {
	c.mu.Lock()
	entry := c.entries[key]
	if entry != nil && time.Since(entry.loadedAt) < poolContextTTL {
		value := entry.value
		c.mu.Unlock()
		return value, nil
	}
	c.mu.Unlock()
	return c.flight.Do(ctx, key, poolContextLoadTimeout, func(loadCtx context.Context) (*poolContext, error) {
		value, err := load(loadCtx)
		if err != nil {
			// A stale directory still beats dropping the tooltip detail.
			c.mu.Lock()
			stale := c.entries[key]
			c.mu.Unlock()
			if stale != nil {
				return stale.value, nil
			}
			return nil, err
		}
		c.mu.Lock()
		if c.entries == nil {
			c.entries = make(map[string]*poolContextEntry)
		}
		cutoff := time.Now().Add(-poolContextTTL)
		for existingKey, existing := range c.entries {
			if existing.loadedAt.Before(cutoff) {
				delete(c.entries, existingKey)
			}
		}
		c.entries[key] = &poolContextEntry{value: value, loadedAt: time.Now()}
		c.mu.Unlock()
		return value, nil
	})
}

// managedPoolGroups reads the groups owned by active mappings for one provider,
// with the platforms those mappings cover. It is a small local read, and it lets
// a user with no mapped pool skip the provider-wide directory entirely.
func (s *Service) managedPoolGroups(ctx context.Context, providerID int) (map[int64]struct{}, map[string]struct{}, error) {
	rows, err := s.client.RelayGroupMapping.Query().
		Where(relaygroupmapping.ProviderIDEQ(providerID), relaygroupmapping.StatusEQ("active")).
		Select(relaygroupmapping.FieldPlatform, relaygroupmapping.FieldGroupIds).
		All(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("pool context: load mappings: %w", err)
	}
	managed := make(map[int64]struct{})
	platforms := make(map[string]struct{})
	for _, row := range rows {
		for _, groupID := range row.GroupIds {
			if groupID > 0 {
				managed[groupID] = struct{}{}
			}
		}
		if platform := strings.TrimSpace(row.Platform); platform != "" {
			platforms[platform] = struct{}{}
		}
	}
	return managed, platforms, nil
}

// loadPoolContext reads the whole managed-pool directory once. The mappings own
// the group set; the Relay owns the accounts and the observed members.
func (s *Service) loadPoolContext(ctx context.Context, subject *resolvedSubject, managed map[int64]struct{}, platforms map[string]struct{}) (*poolContext, error) {
	result := &poolContext{
		accounts: make(map[int64][]relay.UserUsageGroupPoolUsageAccount),
		members:  make(map[int64][]string),
	}

	if reader, ok := subject.provider.(relay.AccountRelationshipReader); ok {
		names := make([]string, 0, len(platforms))
		for platform := range platforms {
			names = append(names, platform)
		}
		slices.Sort(names)
		for _, platform := range names {
			accounts, err := reader.ListAccountsForPlatform(ctx, platform)
			if err != nil {
				return nil, fmt.Errorf("pool context: list %s accounts: %w", platform, err)
			}
			for _, account := range accounts {
				// The pool metric counts OAuth accounts only; show the same set.
				if !strings.EqualFold(strings.TrimSpace(account.Type), "oauth") {
					continue
				}
				for _, relationship := range account.GroupRelationships {
					if _, ok := managed[relationship.GroupID]; !ok {
						continue
					}
					result.accounts[relationship.GroupID] = append(result.accounts[relationship.GroupID], relay.UserUsageGroupPoolUsageAccount{
						ID:       account.ID,
						Name:     account.Name,
						PlanType: account.PlanType,
					})
				}
			}
		}
	}

	if directory, ok := subject.provider.(relay.UserSubscriptionDirectoryProvider); ok {
		users, activeGroups, err := directory.ListUsersWithActiveSubscriptions(ctx)
		if err != nil {
			return nil, fmt.Errorf("pool context: list provider users: %w", err)
		}
		emailByUserID := make(map[int64]string, len(users))
		for _, user := range users {
			if email := strings.TrimSpace(user.Email); email != "" {
				emailByUserID[user.ID] = email
			}
		}
		for userID, groupIDs := range activeGroups {
			email, ok := emailByUserID[userID]
			if !ok {
				continue
			}
			for _, groupID := range groupIDs {
				if _, ok := managed[groupID]; !ok {
					continue
				}
				result.members[groupID] = append(result.members[groupID], email)
			}
		}
	}

	for groupID, accounts := range result.accounts {
		slices.SortFunc(accounts, func(left, right relay.UserUsageGroupPoolUsageAccount) int {
			return cmp.Compare(left.ID, right.ID)
		})
		result.accounts[groupID] = accounts
	}
	for groupID, members := range result.members {
		slices.Sort(members)
		result.members[groupID] = slices.Compact(members)
	}
	return result, nil
}

// attachPoolContext decorates mapped pools with their upstream accounts and
// observed members. It is best effort: the pool metrics stay usable when the
// directory is unavailable.
func (s *Service) attachPoolContext(ctx context.Context, subject *resolvedSubject, groups []relay.UserUsageGroupPoolUsageGroupItem) {
	if s.poolContext == nil || len(groups) == 0 {
		return
	}
	managed, platforms, err := s.managedPoolGroups(ctx, subject.providerID)
	if err != nil || len(managed) == 0 {
		return
	}
	if !reportsManagedGroup(groups, managed) {
		return
	}
	key := fmt.Sprintf("%d:%d", subject.providerID, subject.providerVersion)
	directory, err := s.poolContext.get(ctx, key, func(loadCtx context.Context) (*poolContext, error) {
		return s.loadPoolContext(loadCtx, subject, managed, platforms)
	})
	if err != nil {
		return
	}
	for index := range groups {
		groupID, err := strconv.ParseInt(groups[index].GroupID, 10, 64)
		if err != nil {
			continue
		}
		groups[index].Accounts = directory.accounts[groupID]
		groups[index].Members = directory.members[groupID]
	}
}

func reportsManagedGroup(groups []relay.UserUsageGroupPoolUsageGroupItem, managed map[int64]struct{}) bool {
	for _, group := range groups {
		groupID, err := strconv.ParseInt(group.GroupID, 10, 64)
		if err != nil {
			continue
		}
		if _, ok := managed[groupID]; ok {
			return true
		}
	}
	return false
}
