package attributionlocal

import (
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

var duplicateGroupNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

func duplicateGroupCandidate(localKey, groupID, commitSHA string, buckets ...client.AttributionV2LocalUsageBucket) V2ClaimCandidate {
	c := V2ClaimCandidate{LocalKey: localKey, FirstSeenAt: duplicateGroupNow}
	c.Group.GroupID = groupID
	c.Group.ThreadID = localKey
	c.Group.TurnID = localKey + ":t1"
	c.Group.EvidenceDigest = "ev-" + groupID
	c.Group.TokenSource = client.AttributionV2TokenSourceCodexLocal
	c.Group.CommitAllocations = []client.AttributionV2CommitAllocation{{
		Sequence: 1, CommitSHA: commitSHA, CheckpointEventID: "event-" + commitSHA, EvidenceDigest: "ev-" + groupID,
	}}
	c.Group.LocalUsage = buckets
	return c
}

func duplicateGroupBucket(quarter time.Time, total int64) client.AttributionV2LocalUsageBucket {
	return client.AttributionV2LocalUsageBucket{
		RequestedModel: "claude-opus-5", BucketStartUTC: quarter,
		OutputTokens: total, TotalTokens: total, RequestCount: 1,
	}
}

func duplicateGroupTotal(g client.AttributionV2ClaimGroup) int64 {
	var n int64
	for _, b := range g.LocalUsage {
		n += b.TotalTokens
	}
	return n
}

// A Claude Code resume replays the same work under a new session id and a
// restarted turn counter, so local state keeps one claim per turn while both
// name the same group. The batch must carry that group id once — the backend
// rejects a second occurrence for disagreeing about which session and turn it
// came from, failing the whole batch.
//
// Their usage adds. The scan prices every response into exactly one turn's
// claim (the earliest occurrence wins), so claims meeting here carry disjoint
// partitions of the consumption — measured on live data, each candidate's
// buckets equalled its winner partition exactly. The earlier behaviour kept
// only the first partition, and because acknowledgements map back by group id,
// the dropped partitions were still marked delivered and permanently lost.
func TestUploadableGroupsCollapseOneGroupSeenUnderTwoTurns(t *testing.T) {
	q1 := duplicateGroupNow
	q2 := duplicateGroupNow.Add(15 * time.Minute)
	state := &V2ClaimState{Version: 1}
	MergeV2ClaimState(state, []V2ClaimCandidate{
		duplicateGroupCandidate("sess-a", "GROUP-X", "commit-1", duplicateGroupBucket(q1, 1000)),
		duplicateGroupCandidate("sess-b", "GROUP-X", "commit-1", duplicateGroupBucket(q2, 700)),
	}, duplicateGroupNow)

	groups := UploadableV2ClaimGroups(state.Claims)
	if len(groups) != 1 || groups[0].GroupID != "GROUP-X" {
		t.Fatalf("groups = %+v, want the id once", groups)
	}
	if got := duplicateGroupTotal(groups[0]); got != 1700 {
		t.Fatalf("collapsed usage = %d, want 1700: the partitions are disjoint and must both survive", got)
	}
	if len(groups[0].LocalUsage) != 2 {
		t.Fatalf("buckets = %+v, want both quarter hours", groups[0].LocalUsage)
	}
}

// Two turns consuming in the same quarter hour share a bucket key; the amounts
// add within the bucket rather than one replacing the other.
func TestUploadableGroupsAddPartitionsSharingAQuarterHour(t *testing.T) {
	state := &V2ClaimState{Version: 1}
	MergeV2ClaimState(state, []V2ClaimCandidate{
		duplicateGroupCandidate("sess-a", "GROUP-X", "commit-1", duplicateGroupBucket(duplicateGroupNow, 1000)),
		duplicateGroupCandidate("sess-b", "GROUP-X", "commit-1", duplicateGroupBucket(duplicateGroupNow, 700)),
	}, duplicateGroupNow)

	groups := UploadableV2ClaimGroups(state.Claims)
	if len(groups) != 1 || len(groups[0].LocalUsage) != 1 {
		t.Fatalf("groups = %+v, want one group with one merged bucket", groups)
	}
	bucket := groups[0].LocalUsage[0]
	if bucket.TotalTokens != 1700 || bucket.RequestCount != 2 {
		t.Fatalf("bucket = %+v, want totals and request counts added", bucket)
	}
}

// The shape measured live: an original turn and two giant replay turns all
// prove the same commit. Each carries its own disjoint partition; the batch
// must carry the group once with every partition's usage.
func TestUploadableGroupsCarryEveryDisjointPartitionOnce(t *testing.T) {
	state := &V2ClaimState{Version: 1}
	MergeV2ClaimState(state, []V2ClaimCandidate{
		duplicateGroupCandidate("orig", "GROUP-X", "commit-1", duplicateGroupBucket(duplicateGroupNow, 13931953)),
		duplicateGroupCandidate("replay-1", "GROUP-X", "commit-1", duplicateGroupBucket(duplicateGroupNow.Add(15*time.Minute), 21454210)),
		duplicateGroupCandidate("replay-2", "GROUP-X", "commit-1", duplicateGroupBucket(duplicateGroupNow.Add(30*time.Minute), 17297078)),
	}, duplicateGroupNow)

	groups := UploadableV2ClaimGroups(state.Claims)
	if len(groups) != 1 {
		t.Fatalf("groups = %d, want 1", len(groups))
	}
	if got := duplicateGroupTotal(groups[0]); got != 13931953+21454210+17297078 {
		t.Fatalf("collapsed usage = %d, want the full sum of the three partitions", got)
	}
}

// One turn's edits landing in two commits must still be billed once. Local
// state merges by turn, so the two commits become two allocations on a single
// group rather than two groups each carrying the turn's tokens.
func TestUploadableGroupsBillATurnOnceAcrossSeveralCommits(t *testing.T) {
	state := &V2ClaimState{Version: 1}
	MergeV2ClaimState(state, []V2ClaimCandidate{
		duplicateGroupCandidate("sess-a", "GROUP-A", "commit-1", duplicateGroupBucket(duplicateGroupNow, 1000)),
		duplicateGroupCandidate("sess-a", "GROUP-B", "commit-2", duplicateGroupBucket(duplicateGroupNow, 1000)),
	}, duplicateGroupNow)

	groups := UploadableV2ClaimGroups(state.Claims)
	var total int64
	for _, g := range groups {
		total += duplicateGroupTotal(g)
	}
	if len(groups) != 1 {
		t.Fatalf("groups = %d (%+v), want the turn's claims merged into one", len(groups), groups)
	}
	if len(groups[0].CommitAllocations) != 2 {
		t.Fatalf("allocations = %+v, want both commits on the one group", groups[0].CommitAllocations)
	}
	if total != 1000 {
		t.Fatalf("total = %d, want the turn billed once", total)
	}
}

// Credit adds across collapsed partitions exactly as tokens do.
func TestUploadableGroupsAddCreditAcrossPartitions(t *testing.T) {
	a := duplicateGroupCandidate("orig", "GROUP-K", "commit-1", duplicateGroupBucket(duplicateGroupNow, 0))
	a.Group.LocalUsage[0].CreditUsage = 0.05
	b := duplicateGroupCandidate("replay", "GROUP-K", "commit-1", duplicateGroupBucket(duplicateGroupNow, 0))
	b.Group.LocalUsage[0].CreditUsage = 0.03
	state := &V2ClaimState{Version: 1}
	MergeV2ClaimState(state, []V2ClaimCandidate{a, b}, duplicateGroupNow)

	groups := UploadableV2ClaimGroups(state.Claims)
	if len(groups) != 1 || len(groups[0].LocalUsage) != 1 {
		t.Fatalf("groups = %+v, want one group with one bucket", groups)
	}
	if got := groups[0].LocalUsage[0].CreditUsage; got != 0.08 {
		t.Fatalf("credit = %v, want the partitions added to 0.08", got)
	}
}
