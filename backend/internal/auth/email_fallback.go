package auth

import "strings"

const defaultFallbackDomain = "ldap.local"

// ensureNonEmptyEmail returns email if present; otherwise derives one from username.
//
// Keep this logic centralized because:
// - local Ent schema requires email NotEmpty
// - some LDAP deployments omit "mail"
// - relay provisioning needs a stable, deterministic email when none is available
func ensureNonEmptyEmail(email, username, fallbackDomain string) string {
	email = strings.TrimSpace(email)
	if email != "" {
		return email
	}

	username = strings.TrimSpace(username)
	if username == "" {
		return ""
	}
	// If the "username" we were given is already an email (e.g. login via email alias),
	// preserve it as-is.
	if strings.Contains(username, "@") {
		return username
	}

	if strings.TrimSpace(fallbackDomain) == "" {
		fallbackDomain = defaultFallbackDomain
	}
	return username + "@" + fallbackDomain
}

