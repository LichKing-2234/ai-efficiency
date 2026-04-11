package deployment

import "strings"

// RequireExplicitDBDSN reports whether the runtime must provide DB.DSN.
func RequireExplicitDBDSN(dsn string) bool {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return true
	}
	if strings.Contains(dsn, "://") {
		return !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://")
	}
	lower := strings.ToLower(dsn)
	for _, prefix := range []string{"host=", "user=", "password=", "dbname=", "sslmode=", "port="} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	return true
}
