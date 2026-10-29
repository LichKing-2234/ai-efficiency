package relayplanning

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
)

func TestRecoveryPreviewAndMappingReadAreSafeAndReadOnly(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service, provider, mapping, operation := createRecoveryFixture(t, ctx, client, false)
	provider.accounts = append(provider.accounts,
		relay.Account{ID: 21, Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}},
		relay.Account{ID: 22, Platform: "openai", GroupRelationships: []relay.AccountGroupRelationship{{GroupID: 101, Priority: 1}}},
	)

	mappings, err := service.ListMappings(ctx, mapping.ProviderID)
	if err != nil {
		t.Fatalf("ListMappings() error = %v", err)
	}
	if len(mappings) != 1 || mappings[0].Alignment != "operating" || mappings[0].ActiveOperation == nil || mappings[0].ActiveOperation.ID != operation.ID {
		t.Fatalf("Mapping operation state = %+v, want operating Operation %d", mappings, operation.ID)
	}
	if len(mappings[0].Warnings) == 0 || len(mappings[0].AlignmentDifferences) == 0 {
		t.Fatalf("operating Mapping lost advisory or difference evidence: %+v", mappings[0])
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

// This projection invariant complements the Mapping-list behavior tests without
// exposing a formatter or making production text replaceable for tests.
func TestMappingAlignmentDoesNotInterpretPresentationText(t *testing.T) {
	ctx := context.Background()
	client := testdb.Open(t)
	service := NewService(client, nil, nil)
	for _, advisory := range []string{
		"target group 101 has multiple Accounts",
		"target group 101 uses two Accounts",
		"relationship snapshot includes multiple Accounts",
	} {
		t.Run(advisory, func(t *testing.T) {
			mappings := []Mapping{{ID: 1, Warnings: []string{advisory, advisory}}}
			if err := service.decorateMappingOperationState(ctx, mappings); err != nil {
				t.Fatal(err)
			}
			if mappings[0].Alignment != "aligned" || len(mappings[0].AlignmentDifferences) != 0 || !reflect.DeepEqual(mappings[0].Warnings, []string{advisory}) {
				t.Fatalf("advisory changed Alignment: %+v", mappings[0])
			}
		})
	}

	difference := "target group 101 has multiple Accounts outside the reviewed pool"
	mappings := []Mapping{{ID: 1, AlignmentDifferences: []string{difference, difference}}}
	if err := service.decorateMappingOperationState(ctx, mappings); err != nil {
		t.Fatal(err)
	}
	if mappings[0].Alignment != "drifted" || !reflect.DeepEqual(mappings[0].AlignmentDifferences, []string{difference}) || len(mappings[0].Warnings) != 0 {
		t.Fatalf("difference wording changed Alignment: %+v", mappings[0])
	}
}
