package sessionbootstrap

import "github.com/ai-efficiency/backend/internal/relay"

func selectReusableKey(keys []relay.APIKey, platform, username, emailPrefix string) *relay.APIKey {
	usernameMatches := filterReusableKeys(keys, platform, username)
	if selected := pickReusableKey(usernameMatches); selected != nil {
		return selected
	}

	return pickReusableKey(filterReusableKeys(keys, platform, emailPrefix))
}

func filterReusableKeys(keys []relay.APIKey, platform, name string) []relay.APIKey {
	if name == "" {
		return nil
	}

	filtered := make([]relay.APIKey, 0, len(keys))
	for _, key := range keys {
		if key.Status != "active" {
			continue
		}
		if key.Group == nil || key.Group.Platform != platform {
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
