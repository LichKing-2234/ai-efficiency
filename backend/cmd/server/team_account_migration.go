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
	_, err := db.ExecContext(ctx, `ALTER TABLE team_accounts DROP CONSTRAINT IF EXISTS team_accounts_department_external_id_key`)
	return err
}
