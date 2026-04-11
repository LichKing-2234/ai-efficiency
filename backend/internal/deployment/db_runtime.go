package deployment

import "strings"

// RequireExplicitDBDSN reports whether the runtime must provide DB.DSN.
func RequireExplicitDBDSN(dsn string) bool {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return true
	}
	lower := strings.ToLower(dsn)
	if strings.Contains(dsn, "://") {
		return !strings.HasPrefix(lower, "postgres://") && !strings.HasPrefix(lower, "postgresql://")
	}
	for _, prefix := range []string{"host=", "user=", "password=", "dbname=", "sslmode=", "port="} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return true
}
