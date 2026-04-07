package sessionbootstrap

import (
	"strings"

	"github.com/ai-efficiency/backend/internal/relay"
)

func selectReusableKey(keys []relay.APIKey, platform, username, emailPrefix string) *relay.APIKey {
	usernameMatches := filterKeysByStatus(keys, platform, username, "active")
	if selected := pickReusableKey(usernameMatches); selected != nil {
		return selected
	}

	return pickReusableKey(filterKeysByStatus(keys, platform, emailPrefix, "active"))
}

func selectReactivatableKey(keys []relay.APIKey, platform, username, emailPrefix string) *relay.APIKey {
	usernameMatches := filterKeysByStatus(keys, platform, username, "inactive")
	if selected := pickReusableKey(usernameMatches); selected != nil {
		return selected
	}

	return pickReusableKey(filterKeysByStatus(keys, platform, emailPrefix, "inactive"))
}

func filterKeysByStatus(keys []relay.APIKey, platform, name, status string) []relay.APIKey {
	if name == "" {
		return nil
	}

	filtered := make([]relay.APIKey, 0, len(keys))
	for _, key := range keys {
		if key.Status != status {
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

func preferredKeyName(username, email string) string {
	username = strings.TrimSpace(username)
	email = strings.TrimSpace(email)
	if username != "" {
		// Relay SSO may backfill an empty username with the email address.
		// Treat that as "no username" so API key names use the email prefix instead.
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
