package attributionlocal

import (
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

var duplicateGroupNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

func duplicateGroupCandidate(localKey, groupID, commitSHA, evidence string) V2ClaimCandidate {
	eventID := "event-" + commitSHA
	c := V2ClaimCandidate{LocalKey: localKey, FirstSeenAt: duplicateGroupNow}
	c.Group.GroupID = groupID
	c.Group.ThreadID = localKey
	c.Group.TurnID = localKey + ":t1"
	c.Group.EvidenceDigest = evidence
	c.Group.TokenSource = client.AttributionV2TokenSourceCodexLocal
	c.Group.CommitAllocations = []client.AttributionV2CommitAllocation{{
		Sequence: 1, CommitSHA: commitSHA, CheckpointEventID: eventID, EvidenceDigest: evidence,
	}}
	c.Group.LocalUsage = []client.AttributionV2LocalUsageBucket{{
		RequestedModel: "claude-opus-5", BucketStartUTC: duplicateGroupNow, TotalTokens: 1000, RequestCount: 1,
	}}
	return c
}

func duplicateGroupUsageTotal(g client.AttributionV2ClaimGroup) int64 {
	var n int64
	for _, b := range g.LocalUsage {
		n += b.TotalTokens
	}
	return n
}

// A Claude Code resume replays the same work under a new session id and a
// restarted turn counter. Local state is keyed by the turn, so it keeps two
// entries; both name the same group, because a group is named by the commit and
// the evidence. Sending both put one group id in a batch twice, and the backend
// rejected the second for disagreeing about which session and turn it came
// from — failing the whole batch.
func TestUploadableGroupsCollapseOneGroupSeenUnderTwoTurns(t *testing.T) {
	state := &V2ClaimState{Version: 1}
	MergeV2ClaimState(state, []V2ClaimCandidate{
		duplicateGroupCandidate("sess-a", "GROUP-X", "commit-1", "ev-1"),
		duplicateGroupCandidate("sess-b", "GROUP-X", "commit-1", "ev-1"),
	}, duplicateGroupNow)

	groups := UploadableV2ClaimGroups(state.Claims)
	seen := map[string]int{}
	for _, g := range groups {
		seen[g.GroupID]++
	}
	t.Logf("local entries=%d uploadable groups=%d ids=%v", len(state.Claims), len(groups), seen)
	if seen["GROUP-X"] > 1 {
		t.Errorf("one batch carries GroupID %q %d times; the backend rejects the second for a session/turn mismatch", "GROUP-X", seen["GROUP-X"])
	}
}

// One turn's edits landing in two commits must still be billed once. Local
// state merges by turn, so the two commits become two allocations on a single
// group rather than two groups each carrying the turn's tokens.
func TestUploadableGroupsBillATurnOnceAcrossSeveralCommits(t *testing.T) {
	state := &V2ClaimState{Version: 1}
	MergeV2ClaimState(state, []V2ClaimCandidate{
		duplicateGroupCandidate("sess-a", "GROUP-A", "commit-1", "ev-1"),
		duplicateGroupCandidate("sess-a", "GROUP-B", "commit-2", "ev-2"),
	}, duplicateGroupNow)

	groups := UploadableV2ClaimGroups(state.Claims)
	var total int64
	for _, g := range groups {
		total += duplicateGroupUsageTotal(g)
		t.Logf("group %s allocations=%d tokens=%d", g.GroupID, len(g.CommitAllocations), duplicateGroupUsageTotal(g))
	}
	t.Logf("local entries=%d uploadable groups=%d total tokens=%d", len(state.Claims), len(groups), total)
	if total > 1000 {
		t.Errorf("one turn's 1000 tokens became %d across commits", total)
	}
}
