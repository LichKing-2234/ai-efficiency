package schema_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/lib/pq"

	entmigrate "github.com/ai-efficiency/backend/ent/migrate"
	"github.com/ai-efficiency/backend/ent/owneraccountoperation"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestOwnerAccountOperationSchemaPersistsMutationLifecycle(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	startedAt := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	operation := client.OwnerAccountOperation.Create().
		SetProviderID(7).
		SetAccountID(42).
		SetActorUserID(11).
		SetAction(owneraccountoperation.ActionResetCredit).
		SetReason("consume a banked reset credit").
		SetHeartbeatAt(startedAt).
		SetStartedAt(startedAt).
		SaveX(ctx)

	if operation.Status != owneraccountoperation.StatusRunning {
		t.Fatalf("status = %q, want %q", operation.Status, owneraccountoperation.StatusRunning)
	}
	if operation.FinishedAt != nil {
		t.Fatalf("finished_at = %v, want nil for a running operation", operation.FinishedAt)
	}
	if operation.InviteEmail != nil {
		t.Fatalf("invite_email = %v, want nil for a non-invitation operation", operation.InviteEmail)
	}

	finishedAt := startedAt.Add(20 * time.Second)
	client.OwnerAccountOperation.UpdateOneID(operation.ID).
		SetStatus(owneraccountoperation.StatusSucceeded).
		SetFinishedAt(finishedAt).
		SetUpstreamCode("reset_ok").
		SetResult(map[string]any{"windows_reset": 1}).
		SaveX(ctx)

	reloaded := client.OwnerAccountOperation.GetX(ctx, operation.ID)
	if reloaded.Action != owneraccountoperation.ActionResetCredit {
		t.Fatalf("persisted action = %q, want %q", reloaded.Action, owneraccountoperation.ActionResetCredit)
	}
	if reloaded.Status != owneraccountoperation.StatusSucceeded {
		t.Fatalf("persisted status = %q, want %q", reloaded.Status, owneraccountoperation.StatusSucceeded)
	}
	if reloaded.FinishedAt == nil || !reloaded.FinishedAt.Equal(finishedAt) {
		t.Fatalf("persisted finished_at = %v, want %v", reloaded.FinishedAt, finishedAt)
	}
	if reloaded.UpstreamCode == nil || *reloaded.UpstreamCode != "reset_ok" {
		t.Fatalf("persisted upstream_code = %v, want reset_ok", reloaded.UpstreamCode)
	}
	if got := reloaded.Result["windows_reset"]; got != float64(1) {
		t.Fatalf("persisted result windows_reset = %v, want 1", got)
	}
}

func TestOwnerAccountOperationSchemaPersistsInvitationEvidence(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	operation := client.OwnerAccountOperation.Create().
		SetProviderID(7).
		SetAccountID(42).
		SetActorUserID(11).
		SetAction(owneraccountoperation.ActionSendInvitation).
		SetInviteEmail("alice@example.com").
		SetStatus(owneraccountoperation.StatusUnknown).
		SaveX(ctx)

	reloaded := client.OwnerAccountOperation.GetX(ctx, operation.ID)
	if reloaded.InviteEmail == nil || *reloaded.InviteEmail != "alice@example.com" {
		t.Fatalf("persisted invite_email = %v, want alice@example.com", reloaded.InviteEmail)
	}
	if reloaded.Status != owneraccountoperation.StatusUnknown {
		t.Fatalf("persisted status = %q, want %q", reloaded.Status, owneraccountoperation.StatusUnknown)
	}
}

func TestOwnerAccountOperationPartialUniqueIndexAllowsOneRunningMutationPerAccount(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)

	startedAt := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	createRunning := func(providerID int, accountID int64) error {
		return client.OwnerAccountOperation.Create().
			SetProviderID(providerID).
			SetAccountID(accountID).
			SetActorUserID(11).
			SetAction(owneraccountoperation.ActionResetCredit).
			SetHeartbeatAt(startedAt).
			SetStartedAt(startedAt).
			Exec(ctx)
	}

	firstRunning := client.OwnerAccountOperation.Create().
		SetProviderID(7).
		SetAccountID(42).
		SetActorUserID(11).
		SetAction(owneraccountoperation.ActionResetCredit).
		SetHeartbeatAt(startedAt).
		SetStartedAt(startedAt).
		SaveX(ctx)
	if firstRunning.Status != owneraccountoperation.StatusRunning {
		t.Fatalf("first running mutation status = %q, want running", firstRunning.Status)
	}

	err := createRunning(7, 42)
	var uniqueViolation *pq.Error
	if !errors.As(err, &uniqueViolation) || uniqueViolation.Code != "23505" {
		t.Fatalf("second running mutation error = %v, want unique violation", err)
	}

	if err := createRunning(7, 43); err != nil {
		t.Fatalf("running mutation for another account error = %v, want accepted", err)
	}
	if err := createRunning(8, 42); err != nil {
		t.Fatalf("running mutation for another provider error = %v, want accepted", err)
	}

	runningCount, err := client.OwnerAccountOperation.Query().
		Where(owneraccountoperation.StatusEQ(owneraccountoperation.StatusRunning)).
		Count(ctx)
	if err != nil {
		t.Fatalf("count running mutations: %v", err)
	}
	if runningCount != 3 {
		t.Fatalf("running mutations = %d, want 3", runningCount)
	}

	// Terminal rows fall outside the partial index, so the account may start a
	// new mutation after its previous one finished.
	client.OwnerAccountOperation.UpdateOneID(firstRunning.ID).
		SetStatus(owneraccountoperation.StatusSucceeded).
		SetFinishedAt(startedAt.Add(time.Minute)).
		SaveX(ctx)

	if err := createRunning(7, 42); err != nil {
		t.Fatalf("running mutation after terminal row error = %v, want accepted", err)
	}
}

func TestOwnerAccountOperationSchemaRegistersPartialUniqueRunningIndex(t *testing.T) {
	var definition *schema.Index
	for _, candidate := range entmigrate.OwnerAccountOperationsTable.Indexes {
		if candidate.Name == "owneraccountoperation_running_unique" {
			definition = candidate
			break
		}
	}
	if definition == nil {
		t.Fatal("owneraccountoperation_running_unique index is not registered")
	}
	if !definition.Unique {
		t.Fatal("owneraccountoperation_running_unique index is not unique")
	}
	if definition.Annotation == nil || definition.Annotation.Where != "status = 'running'" {
		t.Fatalf("owneraccountoperation_running_unique predicate = %#v, want status = 'running'", definition.Annotation)
	}
	columns := make([]string, 0, len(definition.Columns))
	for _, column := range definition.Columns {
		columns = append(columns, column.Name)
	}
	if len(columns) != 2 || columns[0] != "provider_id" || columns[1] != "account_id" {
		t.Fatalf("owneraccountoperation_running_unique columns = %v, want [provider_id account_id]", columns)
	}
}
