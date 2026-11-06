package main

import (
	"context"
	"database/sql"
)

// dropLegacyTeamAccountDepartmentUnique removes the old one-account-per-
// department constraint. Replacement accounts now reserve the department only
// while they are active; the generated partial unique index
// team_accounts_department_active_key enforces that rule instead.
func dropLegacyTeamAccountDepartmentUnique(ctx context.Context, db *sql.DB) error {
	// Older releases used a plain unique index (ent creates indexes, not
	// constraints). Drop both shapes so the active-only partial index is the only
	// department rule.
	if _, err := db.ExecContext(ctx, `ALTER TABLE team_accounts DROP CONSTRAINT IF EXISTS team_accounts_department_external_id_key`); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS team_accounts_department_external_id_key`)
	return err
}
