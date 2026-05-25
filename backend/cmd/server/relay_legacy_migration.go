package main

import (
	"context"
	"database/sql"
	"fmt"
)

func dropLegacyRelayProviderAdminURL(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.ExecContext(ctx, `ALTER TABLE relay_providers DROP COLUMN IF EXISTS admin_url`); err != nil {
		return fmt.Errorf("drop relay_providers.admin_url: %w", err)
	}
	return nil
}
