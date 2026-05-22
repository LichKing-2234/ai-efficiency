package usersetup

import (
	"sort"
	"strings"

	"github.com/ai-efficiency/backend/internal/relay"
)

func preferredCredentialName(username, email string) string {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username != "" {
		if strings.Contains(username, "@") && (email == "" || strings.EqualFold(username, email)) {
			username = ""
		}
	}
	if username != "" {
		return username
	}
	if at := strings.Index(email, "@"); at > 0 {
		return email[:at]
	}
	return email
}

func filterReusableKeys(keys []relay.APIKey, platform, name string) []relay.APIKey {
	if strings.TrimSpace(name) == "" {
		return nil
	}
	filtered := make([]relay.APIKey, 0, len(keys))
	for _, key := range keys {
		if !strings.EqualFold(strings.TrimSpace(key.Status), "active") {
			continue
		}
		if key.Group == nil || !strings.EqualFold(strings.TrimSpace(key.Group.Platform), strings.TrimSpace(platform)) {
			continue
		}
		if key.Name != name {
			continue
		}
		filtered = append(filtered, key)
	}
	return filtered
}

func pickReusableKey(keys []relay.APIKey) *relay.APIKey {
	if len(keys) == 0 {
		return nil
	}
	best := keys[0]
	for _, candidate := range keys[1:] {
		if prefersReusableKey(candidate, best) {
			best = candidate
		}
	}
	return &best
}

func prefersReusableKey(candidate, current relay.APIKey) bool {
	switch {
	case candidate.LastUsedAt != nil && current.LastUsedAt == nil:
		return true
	case candidate.LastUsedAt == nil && current.LastUsedAt != nil:
		return false
	case candidate.LastUsedAt != nil && current.LastUsedAt != nil:
		if candidate.LastUsedAt.After(*current.LastUsedAt) {
			return true
		}
		if current.LastUsedAt.After(*candidate.LastUsedAt) {
			return false
		}
	}
	return candidate.CreatedAt.After(current.CreatedAt)
}

func canonicalGroupOrder(groups []GroupCredentialSummary) {
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Platform == groups[j].Platform {
			return groups[i].GroupID < groups[j].GroupID
		}
		return groups[i].Platform < groups[j].Platform
	})
}
