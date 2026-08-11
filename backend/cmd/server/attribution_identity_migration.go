package main

import (
	"context"
	"database/sql"
	"fmt"
)

// dropLegacyAttributionIdentityIndexes removes the two repository-global
// constraints replaced by owner/worktree-scoped event identity in v2.
func dropLegacyAttributionIdentityIndexes(ctx context.Context, db *sql.DB) error {
	for _, name := range []string{
		"commitcheckpoint_repo_config_id_commit_sha",
		"commitrewrite_repo_config_id_old_commit_sha_new_commit_sha_rewrite_type",
	} {
		if _, err := db.ExecContext(ctx, `DROP INDEX IF EXISTS `+name); err != nil {
			return fmt.Errorf("drop legacy attribution index %s: %w", name, err)
		}
	}
	return nil
}
