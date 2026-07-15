package dbguard

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
)

type recordingExecutor struct {
	statements []string
	err        error
}

func (e *recordingExecutor) ExecContext(_ context.Context, statement string, _ ...any) (sql.Result, error) {
	e.statements = append(e.statements, statement)
	return nil, e.err
}

func TestInstallQuotaResetRequestEventsAppendOnlyGuardIsIdempotent(t *testing.T) {
	executor := &recordingExecutor{}
	for range 2 {
		if err := InstallQuotaResetRequestEventsAppendOnlyGuard(context.Background(), executor); err != nil {
			t.Fatalf("install guard: %v", err)
		}
	}
	if len(executor.statements) != 2 || executor.statements[0] != executor.statements[1] {
		t.Fatalf("statements = %#v, want two identical installations", executor.statements)
	}
	for _, required := range []string{
		"CREATE OR REPLACE FUNCTION ae_quota_reset_request_events_reject_mutation()",
		"CREATE TRIGGER ae_quota_reset_request_events_append_only",
		"BEFORE UPDATE OR DELETE ON quota_reset_request_events",
		"FOR EACH STATEMENT",
	} {
		if !strings.Contains(executor.statements[0], required) {
			t.Fatalf("guard statement missing %q: %s", required, executor.statements[0])
		}
	}
}

func TestInstallQuotaResetRequestEventsAppendOnlyGuardWrapsErrors(t *testing.T) {
	sentinel := errors.New("synthetic executor failure")
	err := InstallQuotaResetRequestEventsAppendOnlyGuard(
		context.Background(),
		&recordingExecutor{err: sentinel},
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("error = %v, want wrapped sentinel", err)
	}
	if !strings.Contains(err.Error(), "install quota reset request events append-only guard") {
		t.Fatalf("error = %v, want useful context", err)
	}
}
