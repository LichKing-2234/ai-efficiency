package relayplanning

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestRecoveryPreviewAndMappingReadAreSafeAndReadOnly(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, provider, mapping, operation := createRecoveryFixture(t, ctx, client, false)

	mappings, err := service.ListMappings(ctx, mapping.ProviderID)
	if err != nil {
		t.Fatalf("ListMappings() error = %v", err)
	}
	if len(mappings) != 1 || mappings[0].Alignment != "operating" || mappings[0].ActiveOperation == nil || mappings[0].ActiveOperation.ID != operation.ID {
		t.Fatalf("Mapping operation state = %+v, want operating Operation %d", mappings, operation.ID)
	}
	preview, err := service.PreviewRecovery(ctx, operation.ID, RecoveryResume)
	if err != nil {
		t.Fatalf("PreviewRecovery() error = %v", err)
	}
	if preview.RelationshipFingerprint == "" || preview.BaselineRevisions["1"] != 1 || preview.Operation.AttemptCount != 1 || preview.ExternalBlocker != nil {
		t.Fatalf("Recovery Preview = %+v", preview)
	}
	if provider.assignmentCalls != 0 || len(provider.bound) != 0 {
		t.Fatalf("read-only Preview wrote Relay: assignments=%d bindings=%v", provider.assignmentCalls, provider.bound)
	}
}

func TestRecoveryConfirmRejectsStaleFactsAndBaselineBeforeWrites(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, provider, mapping, operation := createRecoveryFixture(t, ctx, client, false)
	preview, err := service.PreviewRecovery(ctx, operation.ID, RecoveryResume)
	if err != nil {
		t.Fatalf("PreviewRecovery() error = %v", err)
	}
	provider.keys[0].GroupID = 102
	_, err = service.ConfirmRecovery(ctx, RecoveryConfirmRequest{OperationID: operation.ID, Direction: RecoveryResume, ExpectedBaselineRevisions: preview.BaselineRevisions, ExpectedRelationshipFingerprint: preview.RelationshipFingerprint, InitiatedByUserID: 1})
	var stale *StaleRecoveryError
	if !errors.As(err, &stale) || stale.Reason != "relationship_fingerprint" {
		t.Fatalf("relationship stale error = %#v", err)
	}
	if provider.assignmentCalls != 0 || len(provider.bound) != 0 {
		t.Fatalf("stale Confirm wrote Relay: assignments=%d bindings=%v", provider.assignmentCalls, provider.bound)
	}

	provider.keys[0].GroupID = 101
	preview, err = service.PreviewRecovery(ctx, operation.ID, RecoveryResume)
	if err != nil {
		t.Fatalf("second PreviewRecovery() error = %v", err)
	}
	client.RelayGroupMapping.UpdateOneID(mapping.ID).AddBaselineRevision(1).SaveX(ctx)
	_, err = service.ConfirmRecovery(ctx, RecoveryConfirmRequest{OperationID: operation.ID, Direction: RecoveryResume, ExpectedBaselineRevisions: preview.BaselineRevisions, ExpectedRelationshipFingerprint: preview.RelationshipFingerprint, InitiatedByUserID: 1})
	if !errors.As(err, &stale) || stale.Reason != "baseline_revision" {
		t.Fatalf("baseline stale error = %#v", err)
	}
}

func TestRecoveryPreviewReportsExactExternalBlockerWithoutGenericRetry(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, provider, _, operation := createRecoveryFixture(t, ctx, client, false)
	provider.keys = provider.keys[1:]

	preview, err := service.PreviewRecovery(ctx, operation.ID, RecoveryResume)
	if err != nil {
		t.Fatalf("PreviewRecovery() error = %v", err)
	}
	if preview.ExternalBlocker["resource_type"] != "api_key" || int64Value(preview.ExternalBlocker["resource_id"]) != 501 {
		t.Fatalf("external blocker = %v, want API Key 501", preview.ExternalBlocker)
	}
	if provider.assignmentCalls != 0 || len(provider.bound) != 0 {
		t.Fatalf("blocked Preview wrote Relay: assignments=%d bindings=%v", provider.assignmentCalls, provider.bound)
	}
}
