package deployment

import "strings"

// RequireExplicitDBDSN reports whether the runtime must provide DB.DSN.
func RequireExplicitDBDSN(version VersionInfo, dsn string) bool {
	_ = version
	return strings.TrimSpace(dsn) == ""
}
