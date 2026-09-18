package attributionlocal

import (
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

var testShadowProtocol = client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}

func TestApplyV2ClaimAcknowledgementsConsumesOnlyAcknowledgedItems(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	calibration := &client.AttributionV2Calibration{Digest: "calibration-1", TotalTokens: 12}
	group := client.AttributionV2ClaimGroup{GroupID: "group-1", RequestIDs: []string{"req-1", "req-2"}, Calibration: calibration}
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{LocalKey: "local-1", Group: group, FirstSeenAt: now}}}
	result := &client.AttributionV2ClaimBatchResult{LedgerEpoch: "shadow_v2", V1WritePolicy: "accept", Results: []client.AttributionV2ClaimResult{{
		Group:       client.AttributionV2ItemStatus{ID: "group-1", Status: "persisted"},
		Calibration: client.AttributionV2ItemStatus{ID: "calibration-1", Status: "conflict", Error: "calibration differs"},
		Requests:    []client.AttributionV2ItemStatus{{ID: "req-1", Status: "persisted"}, {ID: "req-2", Status: "conflict", Error: "claimed elsewhere"}},
	}}}
	if err := ApplyV2ClaimAcknowledgements(state, []client.AttributionV2ClaimGroup{group}, result, testShadowProtocol, now); err == nil {
		t.Fatal("partial conflict error = nil")
	}
	claim := state.Claims[0]
	if strings.Join(claim.Group.RequestIDs, ",") != "req-2" || claim.Group.Calibration == nil || claim.DeliveryStatus != V2DeliveryConflict {
		t.Fatalf("claim after partial ACK = %+v", claim)
	}
	if len(claim.AcknowledgedRequestDigests) != 1 || claim.AcknowledgedRequestDigests[0] == "req-1" {
		t.Fatalf("acknowledged request audit = %v, want one non-raw digest", claim.AcknowledgedRequestDigests)
	}
}

func TestApplyV2ClaimAcknowledgementsRetainsDigestOnlyAuditAndAcceptsLateRequest(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	calibration := &client.AttributionV2Calibration{Digest: "calibration-1", TotalTokens: 12}
	group := client.AttributionV2ClaimGroup{GroupID: "group-1", RequestIDs: []string{"req-1"}, Calibration: calibration}
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{LocalKey: "local-1", Group: group, FirstSeenAt: now}}}
	result := &client.AttributionV2ClaimBatchResult{LedgerEpoch: "shadow_v2", V1WritePolicy: "accept", Results: []client.AttributionV2ClaimResult{{
		Group:       client.AttributionV2ItemStatus{ID: "group-1", Status: "duplicate_identical"},
		Calibration: client.AttributionV2ItemStatus{ID: "calibration-1", Status: "persisted"},
		Requests:    []client.AttributionV2ItemStatus{{ID: "req-1", Status: "duplicate_identical"}},
	}}}
	if err := ApplyV2ClaimAcknowledgements(state, []client.AttributionV2ClaimGroup{group}, result, testShadowProtocol, now); err != nil {
		t.Fatal(err)
	}
	claim := state.Claims[0]
	if len(claim.Group.RequestIDs) != 0 || claim.Group.Calibration != nil || claim.DeliveryStatus != "" || claim.AcknowledgedCalibrationDigest != "calibration-1" {
		t.Fatalf("digest-only audit claim = %+v", claim)
	}
	late := V2ClaimCandidate{LocalKey: "local-1", Group: client.AttributionV2ClaimGroup{GroupID: "group-1", RequestIDs: []string{"req-1", "req-2"}, Calibration: calibration}, FirstSeenAt: now}
	MergeV2ClaimState(state, []V2ClaimCandidate{late}, now.Add(time.Minute))
	if got := strings.Join(state.Claims[0].Group.RequestIDs, ","); got != "req-2" {
		t.Fatalf("late requests = %q, want only unacknowledged req-2", got)
	}
	if state.Claims[0].Group.Calibration != nil {
		t.Fatal("acknowledged calibration was reintroduced")
	}
}

func TestApplyV2ClaimAcknowledgementsPreservesUnknownResponse(t *testing.T) {
	now := time.Now().UTC()
	group := client.AttributionV2ClaimGroup{GroupID: "group-1", RequestIDs: []string{"req-1"}}
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{LocalKey: "local-1", Group: group, FirstSeenAt: now}}}
	if err := ApplyV2ClaimAcknowledgements(state, []client.AttributionV2ClaimGroup{group}, &client.AttributionV2ClaimBatchResult{LedgerEpoch: "future"}, testShadowProtocol, now); err == nil {
		t.Fatal("unknown epoch error = nil")
	}
	if strings.Join(state.Claims[0].Group.RequestIDs, ",") != "req-1" || state.Claims[0].DeliveryStatus != V2DeliveryUpgradeRequired {
		t.Fatalf("unknown response mutated claim = %+v", state.Claims[0])
	}
}

func TestApplyV2ClaimAcknowledgementsRejectsMismatchedValidProtocolWithoutConsumption(t *testing.T) {
	now := time.Now().UTC()
	group := client.AttributionV2ClaimGroup{GroupID: "group-1", RequestIDs: []string{"req-1"}}
	formal := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochFormalV2, V1WritePolicy: client.AttributionV1WritePolicyUpgradeNeeded, MinimumCLIVersion: "0.2.0-preview.5"}
	shadowUpgrade := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyUpgradeNeeded, MinimumCLIVersion: "0.2.0-preview.5"}

	for _, tt := range []struct {
		name     string
		expected client.AttributionProtocol
		actual   client.AttributionProtocol
	}{
		{name: "formal enrollment cannot accept shadow ACK", expected: formal, actual: shadowUpgrade},
		{name: "shadow enrollment cannot accept formal ACK", expected: shadowUpgrade, actual: formal},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state := &V2ClaimState{Claims: []V2ClaimCandidate{{LocalKey: "local-1", Group: group, FirstSeenAt: now}}}
			result := &client.AttributionV2ClaimBatchResult{
				LedgerEpoch: tt.actual.LedgerEpoch, V1WritePolicy: tt.actual.V1WritePolicy, MinimumCLIVersion: tt.actual.MinimumCLIVersion,
				Results: []client.AttributionV2ClaimResult{{
					Group:    client.AttributionV2ItemStatus{ID: "group-1", Status: "persisted"},
					Requests: []client.AttributionV2ItemStatus{{ID: "req-1", Status: "persisted"}},
				}},
			}
			if err := ApplyV2ClaimAcknowledgements(state, []client.AttributionV2ClaimGroup{group}, result, tt.expected, now); err == nil {
				t.Fatal("mismatched protocol error = nil")
			}
			if len(state.Claims) != 1 || len(state.Claims[0].Group.RequestIDs) != 1 || state.Claims[0].Group.RequestIDs[0] != "req-1" {
				t.Fatalf("mismatched protocol consumed local claim: %+v", state.Claims)
			}
		})
	}
}

func TestApplyV2ClaimAcknowledgementsRejectsMalformedItemListsWithoutConsumption(t *testing.T) {
	now := time.Now().UTC()
	calibration := &client.AttributionV2Calibration{Digest: "calibration-1", TotalTokens: 12}
	group := client.AttributionV2ClaimGroup{GroupID: "group-1", RequestIDs: []string{"req-1", "req-2"}, Calibration: calibration}
	tests := []struct {
		name     string
		requests []client.AttributionV2ItemStatus
		calID    string
	}{
		{name: "duplicate request", requests: []client.AttributionV2ItemStatus{{ID: "req-1", Status: "persisted"}, {ID: "req-1", Status: "persisted"}}, calID: "calibration-1"},
		{name: "unknown request", requests: []client.AttributionV2ItemStatus{{ID: "req-3", Status: "persisted"}}, calID: "calibration-1"},
		{name: "unknown calibration", requests: []client.AttributionV2ItemStatus{{ID: "req-1", Status: "persisted"}}, calID: "calibration-2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &V2ClaimState{Claims: []V2ClaimCandidate{{LocalKey: "local-1", Group: group, FirstSeenAt: now}}}
			result := &client.AttributionV2ClaimBatchResult{LedgerEpoch: "shadow_v2", V1WritePolicy: "accept", Results: []client.AttributionV2ClaimResult{{
				Group: client.AttributionV2ItemStatus{ID: "group-1", Status: "persisted"}, Requests: tt.requests,
				Calibration: client.AttributionV2ItemStatus{ID: tt.calID, Status: "persisted"},
			}}}
			if err := ApplyV2ClaimAcknowledgements(state, []client.AttributionV2ClaimGroup{group}, result, testShadowProtocol, now); err == nil {
				t.Fatal("malformed acknowledgement error = nil")
			}
			claim := state.Claims[0]
			if strings.Join(claim.Group.RequestIDs, ",") != "req-1,req-2" || claim.Group.Calibration == nil || claim.DeliveryStatus != V2DeliveryUpgradeRequired {
				t.Fatalf("malformed acknowledgement consumed local data: %+v", claim)
			}
		})
	}
}

func TestApplyV2ClaimAcknowledgementsConsumesExplicitItemsAndRetainsMissing(t *testing.T) {
	now := time.Now().UTC()
	group := client.AttributionV2ClaimGroup{GroupID: "group-1", RequestIDs: []string{"req-1", "req-2"}}
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{LocalKey: "local-1", Group: group, FirstSeenAt: now}}}
	result := &client.AttributionV2ClaimBatchResult{LedgerEpoch: "shadow_v2", V1WritePolicy: "accept", Results: []client.AttributionV2ClaimResult{{
		Group:    client.AttributionV2ItemStatus{ID: "group-1", Status: "persisted"},
		Requests: []client.AttributionV2ItemStatus{{ID: "req-1", Status: "persisted"}},
	}}}
	if err := ApplyV2ClaimAcknowledgements(state, []client.AttributionV2ClaimGroup{group}, result, testShadowProtocol, now); err == nil {
		t.Fatal("missing item acknowledgement error = nil")
	}
	claim := state.Claims[0]
	if strings.Join(claim.Group.RequestIDs, ",") != "req-2" || claim.DeliveryStatus != V2DeliveryUpgradeRequired {
		t.Fatalf("partial acknowledgement state = %+v", claim)
	}
}

func TestApplyV2ClaimAcknowledgementsDoesNotAcknowledgeNewerAllocation(t *testing.T) {
	now := time.Now().UTC()
	oldAllocation := client.AttributionV2CommitAllocation{Sequence: 1, CommitSHA: "commit-1", CheckpointEventID: "checkpoint-1"}
	newAllocation := client.AttributionV2CommitAllocation{Sequence: 2, CommitSHA: "commit-2", CheckpointEventID: "checkpoint-2"}
	sent := client.AttributionV2ClaimGroup{GroupID: "group-1", RequestIDs: []string{"req-1"}, EvidenceDigest: "evidence-1", CommitAllocations: []client.AttributionV2CommitAllocation{oldAllocation}}
	current := sent
	current.EvidenceDigest = "evidence-2"
	current.CommitAllocations = []client.AttributionV2CommitAllocation{oldAllocation, newAllocation}
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{LocalKey: "local-1", Group: current, FirstSeenAt: now}}}
	result := &client.AttributionV2ClaimBatchResult{LedgerEpoch: "shadow_v2", V1WritePolicy: "accept", Results: []client.AttributionV2ClaimResult{{
		Group:    client.AttributionV2ItemStatus{ID: "group-1", Status: "persisted"},
		Requests: []client.AttributionV2ItemStatus{{ID: "req-1", Status: "persisted"}},
	}}}
	if err := ApplyV2ClaimAcknowledgements(state, []client.AttributionV2ClaimGroup{sent}, result, testShadowProtocol, now); err == nil {
		t.Fatal("stale group acknowledgement error = nil")
	}
	claim := state.Claims[0]
	if claim.GroupAcknowledged || len(claim.Group.RequestIDs) != 0 || claim.DeliveryStatus != V2DeliveryUpgradeRequired {
		t.Fatalf("stale group acknowledgement state = %+v", claim)
	}
	if groups := UploadableV2ClaimGroups(state.Claims); len(groups) != 1 || len(groups[0].CommitAllocations) != 2 {
		t.Fatalf("newer allocation was not retained for upload: %+v", groups)
	}
}

func TestSummarizeV2ClaimDeliveryExcludesCompletedAudit(t *testing.T) {
	state := &V2ClaimState{Claims: []V2ClaimCandidate{
		{Group: client.AttributionV2ClaimGroup{GroupID: "completed"}, GroupAcknowledged: true},
		{Group: client.AttributionV2ClaimGroup{GroupID: "pending", RequestIDs: []string{"req-1"}}},
		{Group: client.AttributionV2ClaimGroup{GroupID: "missing"}, GapReason: v2GapMissingRequestID},
		{Group: client.AttributionV2ClaimGroup{GroupID: "ambiguous"}, GapReason: v2GapAmbiguousRequestEvidence},
		{Group: client.AttributionV2ClaimGroup{GroupID: "expired"}, GapReason: v2GapRequestEvidenceExpired},
	}}
	if summary := SummarizeV2ClaimDelivery(state); summary.Pending != 1 || summary.Accepted != 1 || summary.Conflict != 0 || summary.UpgradeRequired != 0 || summary.MissingRequestID != 1 || summary.AmbiguousRequestEvidence != 1 || summary.RequestEvidenceExpired != 1 {
		t.Fatalf("delivery summary = %+v", summary)
	}
}

func TestLocalUsageAcknowledgementIsReopenedByLateToken(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	usage := client.AttributionV2LocalUsageBucket{RequestedModel: "gpt-test", BucketStartUTC: now, InputTokens: 10, OutputTokens: 2, TotalTokens: 12, RequestCount: 1}
	group := client.AttributionV2ClaimGroup{GroupID: "group-local", TokenSource: client.AttributionV2TokenSourceCodexLocal, LocalUsage: []client.AttributionV2LocalUsageBucket{usage}}
	state := &V2ClaimState{Claims: []V2ClaimCandidate{{LocalKey: "local-turn", Group: group, FirstSeenAt: now}}}
	result := &client.AttributionV2ClaimBatchResult{LedgerEpoch: "shadow_v2", V1WritePolicy: "accept", Results: []client.AttributionV2ClaimResult{{
		Group: client.AttributionV2ItemStatus{ID: group.GroupID, Status: "persisted"},
	}}}
	if err := ApplyV2ClaimAcknowledgements(state, []client.AttributionV2ClaimGroup{group}, result, testShadowProtocol, now); err != nil {
		t.Fatal(err)
	}
	if !state.Claims[0].GroupAcknowledged || len(UploadableV2ClaimGroups(state.Claims)) != 0 {
		t.Fatalf("acknowledged local claim = %+v", state.Claims[0])
	}
	usage.InputTokens = 20
	usage.TotalTokens = 22
	usage.RequestCount = 2
	late := V2ClaimCandidate{LocalKey: "local-turn", Group: client.AttributionV2ClaimGroup{GroupID: group.GroupID, TokenSource: group.TokenSource, LocalUsage: []client.AttributionV2LocalUsageBucket{usage}}, FirstSeenAt: now}
	MergeV2ClaimState(state, []V2ClaimCandidate{late}, now.Add(time.Minute))
	if state.Claims[0].GroupAcknowledged || len(UploadableV2ClaimGroups(state.Claims)) != 1 {
		t.Fatalf("late local usage was not reopened: %+v", state.Claims[0])
	}
}
