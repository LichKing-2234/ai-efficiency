package deployment

import "strings"

// RequireExplicitDBDSN reports whether the runtime must provide DB.DSN.
func RequireExplicitDBDSN(dsn string) bool {
	return strings.TrimSpace(dsn) == ""
}
