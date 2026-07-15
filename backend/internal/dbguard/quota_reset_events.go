package dbguard

import (
	"context"
	"database/sql"
	"fmt"
)

type Executor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

const quotaResetRequestEventsAppendOnlySQL = `
CREATE OR REPLACE FUNCTION ae_quota_reset_request_events_reject_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $function$
BEGIN
    RAISE EXCEPTION 'quota_reset_request_events is append-only'
        USING ERRCODE = '55000';
END;
$function$;

DO $guard$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_trigger
        WHERE tgname = 'ae_quota_reset_request_events_append_only'
          AND tgrelid = 'quota_reset_request_events'::regclass
          AND NOT tgisinternal
    ) THEN
        EXECUTE 'CREATE TRIGGER ae_quota_reset_request_events_append_only
            BEFORE UPDATE OR DELETE ON quota_reset_request_events
            FOR EACH STATEMENT
            EXECUTE FUNCTION ae_quota_reset_request_events_reject_mutation()';
    END IF;
END;
$guard$;
`

func InstallQuotaResetRequestEventsAppendOnlyGuard(ctx context.Context, executor Executor) error {
	if _, err := executor.ExecContext(ctx, quotaResetRequestEventsAppendOnlySQL); err != nil {
		return fmt.Errorf("install quota reset request events append-only guard: %w", err)
	}
	return nil
}
