package deployment

import "strings"

// RequireExplicitDBDSN reports whether the current build must provide DB.DSN
// instead of using implicit SQLite fallback.
func RequireExplicitDBDSN(version VersionInfo, dsn string) bool {
	return strings.TrimSpace(version.Version) != "dev" && strings.TrimSpace(dsn) == ""
}
