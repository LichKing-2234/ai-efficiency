package hooks

import (
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
)

var unprovenNow = time.Date(2026, 8, 28, 12, 0, 0, 0, time.UTC)

func commitOption(sha, eventID string) attributionlocal.V2ClaimScanOptions {
	return attributionlocal.V2ClaimScanOptions{
		RepoRoot: "/repo", CommitSHA: sha, CheckpointEventID: eventID,
		RelayProviderID: 7, RepoConfigID: 8, WorkspaceID: "workspace-8",
	}
}

func provenCandidate(sha, eventID string) attributionlocal.V2ClaimCandidate {
	return attributionlocal.V2ClaimCandidate{
		Group: client.AttributionV2ClaimGroup{
			CommitAllocations: []client.AttributionV2CommitAllocation{{
				CommitSHA: sha, CheckpointEventID: eventID, EvidenceDigest: "digest",
			}},
		},
	}
}

// A commit whose evidence had not arrived yet has to be remembered, or it is
// never looked at again: the commits a scan considers come from the triggers of
// the task that started it, and that task is over.
func TestMergeUnprovenCommitsRemembersWhatAScanCouldNotProve(t *testing.T) {
	got := mergeUnprovenCommits(nil, []attributionlocal.V2ClaimScanOptions{commitOption("sha-a", "event-a")}, nil, unprovenNow)
	if len(got) != 1 || got[0].CommitSHA != "sha-a" {
		t.Fatalf("pending = %+v, want the unproven commit kept", got)
	}
	if got[0].RelayProviderID != 7 || got[0].WorkspaceID != "workspace-8" {
		t.Fatalf("pending = %+v, want enough context to retry the scan", got[0])
	}
}

func TestMergeUnprovenCommitsForgetsWhatWasProved(t *testing.T) {
	existing := mergeUnprovenCommits(nil, []attributionlocal.V2ClaimScanOptions{commitOption("sha-a", "event-a")}, nil, unprovenNow)
	proven := provenCommitKeys([]attributionlocal.V2ClaimCandidate{provenCandidate("sha-a", "event-a")})
	got := mergeUnprovenCommits(existing, nil, proven, unprovenNow.Add(time.Hour))
	if len(got) != 0 {
		t.Fatalf("pending = %+v, want the proved commit forgotten", got)
	}
}

// Retrying forever would cost a scan every time and can no longer succeed once
// the evidence is older than the window a scan reads.
func TestMergeUnprovenCommitsDropsCommitsPastTheEvidenceWindow(t *testing.T) {
	stale := []V2UnprovenCommit{{
		CommitSHA: "sha-old", CheckpointEventID: "event-old", RepoRoot: "/repo",
		FirstSeenAt: unprovenNow.Add(-v2ClaimSourceWindow - time.Hour),
	}}
	if got := mergeUnprovenCommits(stale, nil, nil, unprovenNow); len(got) != 0 {
		t.Fatalf("pending = %+v, want the aged-out commit dropped", got)
	}
}

// A candidate carrying a gap proved nothing, so its commit stays pending.
func TestProvenCommitKeysIgnoresGappedCandidates(t *testing.T) {
	gapped := provenCandidate("sha-a", "event-a")
	gapped.GapReason = "commit_content_mismatch"
	if got := provenCommitKeys([]attributionlocal.V2ClaimCandidate{gapped}); len(got) != 0 {
		t.Fatalf("proven = %v, want a gapped candidate to prove nothing", got)
	}

	noEvidence := provenCandidate("sha-b", "event-b")
	noEvidence.Group.CommitAllocations[0].EvidenceDigest = ""
	if got := provenCommitKeys([]attributionlocal.V2ClaimCandidate{noEvidence}); len(got) != 0 {
		t.Fatalf("proven = %v, want an allocation without evidence to prove nothing", got)
	}
}

func TestAppendUnprovenCommitOptionsAddsPendingWithoutRepeating(t *testing.T) {
	pending := []V2UnprovenCommit{
		{CommitSHA: "sha-a", CheckpointEventID: "event-a", RepoRoot: "/repo"},
		{CommitSHA: "sha-b", CheckpointEventID: "event-b", RepoRoot: "/repo"},
	}
	got := appendUnprovenCommitOptions([]attributionlocal.V2ClaimScanOptions{commitOption("sha-a", "event-a")}, pending)
	if len(got) != 2 {
		t.Fatalf("options = %d (%+v), want the task's own commit plus the one other pending commit", len(got), got)
	}
	seen := map[string]int{}
	for _, option := range got {
		seen[option.CommitSHA]++
	}
	if seen["sha-a"] != 1 || seen["sha-b"] != 1 {
		t.Fatalf("options = %+v, want each commit exactly once", got)
	}
}

func TestAppendUnprovenCommitOptionsLeavesOptionsAloneWithoutProgress(t *testing.T) {
	options := []attributionlocal.V2ClaimScanOptions{commitOption("sha-a", "event-a")}
	if got := appendUnprovenCommitOptions(options, nil); len(got) != 1 {
		t.Fatalf("options = %+v, want them unchanged", got)
	}
}

// The pending commits must outlive the scan progress, which finishV2ClaimScan
// deletes the moment a scan finishes. Keeping them in the progress file meant
// every completed scan threw away the retries it had just recorded.
func TestUnprovenCommitsSurviveClearingScanProgress(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const workspaceID = "workspace-survives"
	pending := []V2UnprovenCommit{{
		CommitSHA: "sha-a", CheckpointEventID: "event-a", RepoRoot: "/repo", FirstSeenAt: unprovenNow,
	}}
	if err := SaveV2UnprovenCommits(workspaceID, pending, unprovenNow); err != nil {
		t.Fatal(err)
	}
	if err := SaveV2ClaimScanProgress(&V2ClaimScanProgress{
		Version: v2ClaimScanProgressVersion, WorkspaceID: workspaceID, ContextID: "ctx", StartedAt: unprovenNow,
	}); err != nil {
		t.Fatal(err)
	}
	if err := finishV2ClaimScan(workspaceID, true); err != nil {
		t.Fatal(err)
	}

	got, err := LoadV2UnprovenCommits(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].CommitSHA != "sha-a" {
		t.Fatalf("pending = %+v, want the retry to survive the progress being cleared", got)
	}
}

// An empty list removes the file rather than leaving an empty one behind, so a
// machine with nothing pending has nothing to read.
func TestSaveV2UnprovenCommitsClearsWhenNothingIsPending(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const workspaceID = "workspace-clears"
	if err := SaveV2UnprovenCommits(workspaceID, []V2UnprovenCommit{{
		CommitSHA: "sha-a", CheckpointEventID: "event-a", FirstSeenAt: unprovenNow,
	}}, unprovenNow); err != nil {
		t.Fatal(err)
	}
	if err := SaveV2UnprovenCommits(workspaceID, nil, unprovenNow); err != nil {
		t.Fatal(err)
	}
	got, err := LoadV2UnprovenCommits(workspaceID)
	if err != nil || len(got) != 0 {
		t.Fatalf("pending = %+v, err = %v; want nothing left", got, err)
	}
}
