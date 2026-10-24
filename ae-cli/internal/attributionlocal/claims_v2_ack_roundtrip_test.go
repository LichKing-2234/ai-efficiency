package attributionlocal

import (
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

// The full collapse-then-acknowledge roundtrip. Local state holds two claims
// naming one group — an original turn and its replay — the batch carries the
// collapsed group once, and the backend accepts it. Both claims must record
// the acknowledgement: the backend now holds everything either of them wanted
// delivered, and recording anything else re-sends an accepted batch forever.
func TestAcknowledgingACollapsedGroupSettlesEveryClaimItCovers(t *testing.T) {
	now := time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)
	state := &V2ClaimState{Version: 1}
	MergeV2ClaimState(state, []V2ClaimCandidate{
		duplicateGroupCandidate("orig", "GROUP-X", "commit-1", duplicateGroupBucket(now, 1000)),
		duplicateGroupCandidate("replay", "GROUP-X", "commit-1", duplicateGroupBucket(now.Add(15*time.Minute), 700)),
	}, now)

	groups := UploadableV2ClaimGroups(state.Claims)
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want the collapsed one", len(groups))
	}
	result := &client.AttributionV2ClaimBatchResult{
		LedgerEpoch: "shadow_v2", V1WritePolicy: "accept",
		Results: []client.AttributionV2ClaimResult{{
			Group: client.AttributionV2ItemStatus{ID: "GROUP-X", Status: "persisted"},
		}},
	}
	err := ApplyV2ClaimAcknowledgements(state, groups, result, testShadowProtocol, now)

	acknowledged, upgradeRequired := 0, 0
	for _, claim := range state.Claims {
		if claim.GroupAcknowledged {
			acknowledged++
		}
		if claim.DeliveryStatus == V2DeliveryUpgradeRequired {
			upgradeRequired++
		}
	}
	t.Logf("groups=%d claims=%d acknowledged=%d upgrade_required=%d err=%v",
		len(groups), len(state.Claims), acknowledged, upgradeRequired, err)
	if err != nil {
		t.Fatalf("an accepted batch reported an error: %v", err)
	}
	if acknowledged != len(state.Claims) {
		t.Fatalf("acknowledged = %d of %d claims", acknowledged, len(state.Claims))
	}
	if left := UploadableV2ClaimGroups(state.Claims); len(left) != 0 {
		t.Fatalf("still uploadable after acceptance: %d groups — this re-sends forever", len(left))
	}
}
