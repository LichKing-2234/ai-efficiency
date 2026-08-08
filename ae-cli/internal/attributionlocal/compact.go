package attributionlocal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

const compactExtractorVersion = "ae-cli-codex-compact-poc-v1"

type CompactBackendClient interface {
	SendAttributionBuckets(context.Context, []client.AttributionBucket) error
	SendAttributionRevision(context.Context, string, client.AttributionRevision) error
}

type CompactRunOptions struct {
	InstallationID string
	RepoRoot       string
	RepoConfigID   int
	RepoKey        string
	WorkspaceID    string
	CommitSHA      string
	Branch         string
	TriggerKind    string
	Cutoff         time.Time
}

// CompactTrigger is the minimal durable evidence needed to close or restate a
// compact bucket. It intentionally contains no repository path or Git content.
type CompactTrigger struct {
	ID              string    `json:"id"`
	Kind            string    `json:"kind"`
	RepoConfigID    int       `json:"repo_config_id"`
	RepoKey         string    `json:"repo_key"`
	WorkspaceID     string    `json:"workspace_id"`
	CommitSHA       string    `json:"commit_sha,omitempty"`
	Branch          string    `json:"branch,omitempty"`
	OldCommitSHA    string    `json:"old_commit_sha,omitempty"`
	NewCommitSHA    string    `json:"new_commit_sha,omitempty"`
	RewriteType     string    `json:"rewrite_type,omitempty"`
	LineageKind     string    `json:"lineage_kind,omitempty"`
	SourceCommitSHA string    `json:"source_commit_sha,omitempty"`
	CapturedAt      time.Time `json:"captured_at"`
}

type CompactState struct {
	Version   int              `json:"version"`
	EnabledAt time.Time        `json:"enabled_at"`
	SeenAtoms map[string]bool  `json:"seen_atoms"`
	Pending   []CompactPending `json:"pending,omitempty"`
	Closed    []CompactClosed  `json:"closed,omitempty"`
	Triggers  []CompactTrigger `json:"triggers,omitempty"`
}

type CompactPending struct {
	Bucket           client.AttributionBucket `json:"bucket"`
	AtomIDs          []string                 `json:"atom_ids"`
	BindingCandidate client.AttributionTarget `json:"binding_candidate,omitempty"`
	CommitPatchID    string                   `json:"commit_patch_id,omitempty"`
}

type CompactClosed struct {
	BucketID         string                   `json:"bucket_id"`
	AtomIDs          []string                 `json:"atom_ids"`
	Tokens           client.AttributionTokens `json:"tokens"`
	ObservedEnd      time.Time                `json:"observed_end_at"`
	CurrentTarget    client.AttributionTarget `json:"current_target"`
	CurrentSequence  int                      `json:"current_sequence"`
	BindingCandidate client.AttributionTarget `json:"binding_candidate,omitempty"`
	CommitPatchID    string                   `json:"commit_patch_id,omitempty"`
}

type CompactSyncEngine struct {
	Client CompactBackendClient
}

func CompactStatePath() string {
	return filepath.Join(AttributionRootDir(), "compact", "state.json")
}

func InitializeCompactBaseline(ctx context.Context, enabledAt time.Time) error {
	state := CompactState{Version: 2, EnabledAt: enabledAt.UTC(), SeenAtoms: map[string]bool{}}
	if err := withCompactStateLock(ctx, func() error { return SaveJSON(CompactStatePath(), state) }); err != nil {
		return err
	}
	return nil
}

func LoadCompactState() (*CompactState, error) {
	var state CompactState
	if err := LoadJSON(CompactStatePath(), &state); err != nil {
		return nil, err
	}
	if state.SeenAtoms == nil {
		state.SeenAtoms = map[string]bool{}
	}
	if state.Version < 2 {
		state.Version = 2
	}
	return &state, nil
}

// QueueCompactTrigger records commit/rewrite evidence before an asynchronous
// runner starts. Hook task coalescing therefore cannot lose an intermediate
// commit or rewrite.
func QueueCompactTrigger(ctx context.Context, trigger CompactTrigger) error {
	trigger = normalizeCompactTrigger(trigger)
	if err := validateCompactTrigger(trigger); err != nil {
		return err
	}
	return withCompactStateLock(ctx, func() error {
		state, err := LoadCompactState()
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		appendCompactTrigger(state, trigger)
		return SaveJSON(CompactStatePath(), state)
	})
}

func (e *CompactSyncEngine) Run(ctx context.Context, opts CompactRunOptions) error {
	if e == nil || e.Client == nil {
		return fmt.Errorf("compact attribution client is required")
	}
	return withCompactRunLock(ctx, func() error {
		return e.run(ctx, opts)
	})
}

func (e *CompactSyncEngine) run(ctx context.Context, opts CompactRunOptions) error {
	var state *CompactState
	if err := withCompactStateLock(ctx, func() error {
		var err error
		state, err = LoadCompactState()
		if err != nil {
			return fmt.Errorf("load compact attribution state: %w", err)
		}
		if trigger, ok := compactTriggerFromRunOptions(opts); ok {
			appendCompactTrigger(state, trigger)
			return SaveJSON(CompactStatePath(), state)
		}
		return nil
	}); err != nil {
		return err
	}
	if len(state.Pending) > 0 {
		if err := e.uploadPending(ctx, state); err != nil {
			return err
		}
		if err := persistCompactState(ctx, state); err != nil {
			return err
		}
	}

	atoms, err := scanCompactCodexAtomsSince(ctx, opts.RepoRoot, state.EnabledAt)
	if err != nil {
		return err
	}
	cutoff := opts.Cutoff.UTC()
	if cutoff.IsZero() {
		cutoff = time.Now().UTC()
	}
	if err := e.addSharedAssociations(ctx, state, opts, atoms, cutoff); err != nil {
		return err
	}

	eligible := make([]CompactCodexAtom, 0, len(atoms))
	for _, atom := range atoms {
		if state.SeenAtoms[atom.ID] || atom.ObservedAt.Before(state.EnabledAt) || atom.ObservedAt.After(cutoff) {
			continue
		}
		eligible = append(eligible, atom)
	}
	pending := buildCompactBuckets(ctx, opts, eligible, relevantCompactTriggers(state.Triggers, opts))
	if len(pending) > 0 {
		state.Pending = append(state.Pending, pending...)
		if err := persistCompactState(ctx, state); err != nil {
			return err
		}
		if err := e.uploadPending(ctx, state); err != nil {
			return err
		}
	}

	if err := e.applyTriggers(ctx, state, opts); err != nil {
		return err
	}
	return persistCompactState(ctx, state)
}

func persistCompactState(ctx context.Context, state *CompactState) error {
	return withCompactStateLock(ctx, func() error {
		latest, err := LoadCompactState()
		if err != nil {
			return err
		}
		for _, trigger := range latest.Triggers {
			appendCompactTrigger(state, trigger)
		}
		return SaveJSON(CompactStatePath(), state)
	})
}

func (e *CompactSyncEngine) uploadPending(ctx context.Context, state *CompactState) error {
	if len(state.Pending) == 0 {
		return nil
	}
	buckets := make([]client.AttributionBucket, 0, len(state.Pending))
	for _, pending := range state.Pending {
		buckets = append(buckets, pending.Bucket)
	}
	if err := e.Client.SendAttributionBuckets(ctx, buckets); err != nil {
		return err
	}
	for _, pending := range state.Pending {
		for _, atomID := range pending.AtomIDs {
			state.SeenAtoms[atomID] = true
		}
		target := client.AttributionTarget{Status: "unbound"}
		if len(pending.Bucket.InitialRevision.Allocations) == 1 {
			target = pending.Bucket.InitialRevision.Allocations[0].Target
		}
		state.Closed = append(state.Closed, CompactClosed{
			BucketID:         pending.Bucket.BucketID,
			AtomIDs:          append([]string(nil), pending.AtomIDs...),
			Tokens:           pending.Bucket.Tokens,
			ObservedEnd:      pending.Bucket.ObservedEnd,
			CurrentTarget:    target,
			CurrentSequence:  pending.Bucket.InitialRevision.Sequence,
			BindingCandidate: pending.BindingCandidate,
			CommitPatchID:    pending.CommitPatchID,
		})
	}
	state.Pending = nil
	return nil
}

func (e *CompactSyncEngine) addSharedAssociations(ctx context.Context, state *CompactState, opts CompactRunOptions, atoms []CompactCodexAtom, restatedAt time.Time) error {
	if opts.RepoConfigID <= 0 {
		return nil
	}
	sharedAtomIDs := map[string]struct{}{}
	for _, atom := range atoms {
		if atom.Evidence == "multi_repo_shared" && state.SeenAtoms[atom.ID] {
			sharedAtomIDs[atom.ID] = struct{}{}
		}
	}
	if len(sharedAtomIDs) == 0 {
		return nil
	}
	for index := range state.Closed {
		closed := &state.Closed[index]
		if closed.CurrentTarget.Status != "multi_repo_shared" || !containsAnyString(closed.AtomIDs, sharedAtomIDs) || containsInt(closed.CurrentTarget.AssociatedRepoConfigIDs, opts.RepoConfigID) {
			continue
		}
		target := closed.CurrentTarget
		target.AssociatedRepoConfigIDs = append(target.AssociatedRepoConfigIDs, opts.RepoConfigID)
		sort.Ints(target.AssociatedRepoConfigIDs)
		revision := compactRevision(*closed, target, "another repository was observed for a shared Codex response", restatedAt)
		if err := e.Client.SendAttributionRevision(ctx, closed.BucketID, revision); err != nil {
			return err
		}
		closed.CurrentTarget = target
		closed.CurrentSequence = revision.Sequence
	}
	return nil
}

func (e *CompactSyncEngine) applyTriggers(ctx context.Context, state *CompactState, opts CompactRunOptions) error {
	relevant := relevantCompactTriggers(state.Triggers, opts)
	if len(relevant) == 0 {
		return nil
	}
	sort.Slice(relevant, func(i, j int) bool {
		if relevant[i].CapturedAt.Equal(relevant[j].CapturedAt) {
			return relevant[i].ID < relevant[j].ID
		}
		return relevant[i].CapturedAt.Before(relevant[j].CapturedAt)
	})
	for _, trigger := range relevant {
		switch trigger.Kind {
		case "post-commit":
			for index := range state.Closed {
				closed := &state.Closed[index]
				candidate := closed.BindingCandidate
				if closed.CurrentTarget.Status != "unbound" || candidate.RepoConfigID != trigger.RepoConfigID || candidate.WorkspaceID != trigger.WorkspaceID || closed.ObservedEnd.After(trigger.CapturedAt) {
					continue
				}
				target := candidate
				target.Status = "bound_auto"
				target.CommitSHA = trigger.CommitSHA
				target.Branch = trigger.Branch
				revision := compactRevision(*closed, target, "late commit checkpoint bound a compact Codex bucket", trigger.CapturedAt)
				if err := e.Client.SendAttributionRevision(ctx, closed.BucketID, revision); err != nil {
					return err
				}
				closed.CurrentTarget = target
				closed.CurrentSequence = revision.Sequence
				closed.CommitPatchID = compactCommitPatchID(ctx, opts.RepoRoot, trigger.CommitSHA)
			}
			if err := e.applyInheritedCommit(ctx, state, opts, trigger); err != nil {
				return err
			}
		case "post-rewrite":
			for index := range state.Closed {
				closed := &state.Closed[index]
				if !isCompactBound(closed.CurrentTarget.Status) {
					continue
				}
				target := closed.CurrentTarget
				changed := false
				if target.RepoConfigID == trigger.RepoConfigID && target.WorkspaceID == trigger.WorkspaceID && target.CommitSHA == trigger.OldCommitSHA {
					target.CommitSHA = trigger.NewCommitSHA
					target.Lineage = firstNonEmptyCompact(trigger.RewriteType, "rewrite")
					changed = true
				}
				for inheritedIndex := range target.InheritedCommits {
					inherited := &target.InheritedCommits[inheritedIndex]
					if inherited.RepoConfigID == trigger.RepoConfigID && inherited.WorkspaceID == trigger.WorkspaceID && inherited.CommitSHA == trigger.OldCommitSHA {
						inherited.CommitSHA = trigger.NewCommitSHA
						inherited.Lineage = firstNonEmptyCompact(trigger.RewriteType, "rewrite")
						changed = true
					}
				}
				if !changed {
					continue
				}
				revision := compactRevision(*closed, target, "Git rewrite moved the compact allocation to its replacement commit", trigger.CapturedAt)
				if err := e.Client.SendAttributionRevision(ctx, closed.BucketID, revision); err != nil {
					return err
				}
				closed.CurrentTarget = target
				closed.CurrentSequence = revision.Sequence
				if target.CommitSHA == trigger.NewCommitSHA {
					closed.CommitPatchID = compactCommitPatchID(ctx, opts.RepoRoot, trigger.NewCommitSHA)
				}
			}
		}
	}
	// Keep compact Git evidence after it has been applied. A Codex JSONL write
	// can become visible after the first detached runner has already scanned the
	// commit cutoff. Retaining the small trigger lets a later sync bind that late
	// atom to the first qualifying commit and replay any rewrite lineage without
	// querying raw backend checkpoints.
	return nil
}

func (e *CompactSyncEngine) applyInheritedCommit(ctx context.Context, state *CompactState, opts CompactRunOptions, trigger CompactTrigger) error {
	if trigger.LineageKind != "cherry-pick" || trigger.CommitSHA == "" || trigger.SourceCommitSHA == "" {
		return nil
	}
	patchID := compactCommitPatchID(ctx, opts.RepoRoot, trigger.CommitSHA)
	if patchID == "" {
		return nil
	}
	for index := range state.Closed {
		closed := &state.Closed[index]
		if !isCompactBound(closed.CurrentTarget.Status) || closed.CommitPatchID == "" || closed.CommitPatchID != patchID {
			continue
		}
		if closed.CurrentTarget.CommitSHA != trigger.SourceCommitSHA {
			continue
		}
		if closed.CurrentTarget.RepoConfigID == trigger.RepoConfigID && closed.CurrentTarget.WorkspaceID == trigger.WorkspaceID && closed.CurrentTarget.CommitSHA == trigger.CommitSHA {
			continue
		}
		reference := client.AttributionCommitReference{
			RepoConfigID: trigger.RepoConfigID,
			RepoKey:      trigger.RepoKey,
			WorkspaceID:  trigger.WorkspaceID,
			CommitSHA:    trigger.CommitSHA,
			Branch:       trigger.Branch,
			Lineage:      "cherry-pick",
		}
		if compactTargetHasInheritedCommit(closed.CurrentTarget, reference) {
			continue
		}
		target := closed.CurrentTarget
		target.InheritedCommits = append(target.InheritedCommits, reference)
		sort.Slice(target.InheritedCommits, func(i, j int) bool {
			return compactCommitReferenceKey(target.InheritedCommits[i]) < compactCommitReferenceKey(target.InheritedCommits[j])
		})
		revision := compactRevision(*closed, target, "cherry-picked commit inherited compact usage without duplicating Token allocation", trigger.CapturedAt)
		if err := e.Client.SendAttributionRevision(ctx, closed.BucketID, revision); err != nil {
			return err
		}
		closed.CurrentTarget = target
		closed.CurrentSequence = revision.Sequence
	}
	return nil
}

func compactTargetHasInheritedCommit(target client.AttributionTarget, reference client.AttributionCommitReference) bool {
	want := compactCommitReferenceKey(reference)
	for _, existing := range target.InheritedCommits {
		if compactCommitReferenceKey(existing) == want {
			return true
		}
	}
	return false
}

func compactCommitReferenceKey(reference client.AttributionCommitReference) string {
	return strings.Join([]string{
		fmt.Sprintf("%d", reference.RepoConfigID),
		reference.RepoKey,
		reference.WorkspaceID,
		reference.CommitSHA,
		reference.Branch,
		reference.Lineage,
	}, "\x1f")
}

func compactCommitPatchID(ctx context.Context, repoRoot, commitSHA string) string {
	repoRoot = strings.TrimSpace(repoRoot)
	commitSHA = strings.TrimSpace(commitSHA)
	if repoRoot == "" || commitSHA == "" {
		return ""
	}
	show := exec.CommandContext(ctx, "git", "-C", repoRoot, "show", "--pretty=format:", "--no-ext-diff", commitSHA)
	patch, err := show.Output()
	if err != nil || len(bytes.TrimSpace(patch)) == 0 {
		return ""
	}
	patchID := exec.CommandContext(ctx, "git", "patch-id", "--stable")
	patchID.Stdin = bytes.NewReader(patch)
	output, err := patchID.Output()
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(output))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

func buildCompactBuckets(ctx context.Context, opts CompactRunOptions, atoms []CompactCodexAtom, triggers []CompactTrigger) []CompactPending {
	type group struct {
		atoms     []CompactCodexAtom
		target    client.AttributionTarget
		candidate client.AttributionTarget
	}
	groups := map[string]*group{}
	for _, atom := range atoms {
		target, candidate := compactTargetForAtom(opts, atom, triggers)
		targetKey := compactTargetKey(target)
		key := strings.Join([]string{atom.ChangeSetID, atom.ConversationID, atom.Model, atom.Quality, targetKey}, "\x00")
		if groups[key] == nil {
			groups[key] = &group{target: target, candidate: candidate}
		} else if groups[key].candidate.Status == "" && candidate.Status != "" {
			groups[key].candidate = candidate
		}
		groups[key].atoms = append(groups[key].atoms, atom)
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]CompactPending, 0, len(keys))
	for _, key := range keys {
		group := groups[key]
		sort.Slice(group.atoms, func(i, j int) bool {
			if group.atoms[i].ObservedAt.Equal(group.atoms[j].ObservedAt) {
				return group.atoms[i].ID < group.atoms[j].ID
			}
			return group.atoms[i].ObservedAt.Before(group.atoms[j].ObservedAt)
		})
		first := group.atoms[0]
		last := group.atoms[len(group.atoms)-1]
		atomIDs := make([]string, 0, len(group.atoms))
		var tokens client.AttributionTokens
		coverageGaps := 0
		requestCount := 0
		for _, atom := range group.atoms {
			atomIDs = append(atomIDs, atom.ID)
			tokens.FreshInput += atom.FreshInput
			tokens.CacheRead += atom.CacheRead
			tokens.CacheWrite += atom.CacheWrite
			tokens.Output += atom.Output
			tokens.Reasoning += atom.Reasoning
			tokens.ProviderTotal += atom.ProviderTotal
			tokens.Processed += atom.Processed
			if atom.Quality == "invalid" {
				coverageGaps++
			} else {
				requestCount++
			}
		}
		sourceDigest := digestStrings(atomIDs)
		bucketID := digestSequence([]string{opts.InstallationID, "codex", sourceDigest})
		revisionID := digestSequence([]string{bucketID, "allocation", "1"})
		restatedAt := opts.Cutoff.UTC()
		if restatedAt.IsZero() {
			restatedAt = last.ObservedAt
		}
		bucket := client.AttributionBucket{
			SchemaVersion: 1,
			BucketID:      bucketID,
			Tool:          "codex",
			Model:         first.Model,
			ChangeSetID:   first.ChangeSetID,
			SessionSlices: []client.AttributionSessionSlice{{
				ConversationID: first.ConversationID,
				ObservedStart:  first.ObservedAt,
				ObservedEnd:    last.ObservedAt,
				TokenAtomCount: len(group.atoms),
				AtomSetDigest:  sourceDigest,
			}},
			ObservedStart:        first.ObservedAt,
			ObservedEnd:          last.ObservedAt,
			Tokens:               tokens,
			RequestCount:         requestCount,
			SourceEventCount:     len(group.atoms),
			SourceDigest:         sourceDigest,
			ExtractorVersion:     compactExtractorVersion,
			NormalizationVersion: 1,
			TokenQuality:         first.Quality,
			CoverageGapCount:     coverageGaps,
			InitialRevision: client.AttributionRevision{
				RevisionID:      revisionID,
				Sequence:        1,
				Reason:          compactRevisionReason(group.target.Status),
				EvidenceVersion: compactExtractorVersion,
				Allocations:     []client.AttributionAllocation{{Target: group.target, Tokens: tokens}},
				RestatedAt:      restatedAt,
			},
		}
		patchID := ""
		if isCompactBound(group.target.Status) {
			patchID = compactCommitPatchID(ctx, opts.RepoRoot, group.target.CommitSHA)
		}
		result = append(result, CompactPending{Bucket: bucket, AtomIDs: atomIDs, BindingCandidate: group.candidate, CommitPatchID: patchID})
	}
	return result
}

func compactTargetForAtom(opts CompactRunOptions, atom CompactCodexAtom, triggers []CompactTrigger) (client.AttributionTarget, client.AttributionTarget) {
	base := client.AttributionTarget{
		RepoConfigID: opts.RepoConfigID,
		RepoKey:      strings.TrimSpace(opts.RepoKey),
		WorkspaceID:  strings.TrimSpace(opts.WorkspaceID),
	}
	switch atom.Evidence {
	case "multi_repo_shared":
		target := client.AttributionTarget{Status: "multi_repo_shared"}
		if opts.RepoConfigID > 0 {
			target.AssociatedRepoConfigIDs = []int{opts.RepoConfigID}
		}
		return target, client.AttributionTarget{}
	case "direct":
		candidate := base
		candidate.Status = "unbound"
		if atom.Quality == "measured" {
			for _, trigger := range triggers {
				if trigger.Kind == "post-commit" && !atom.ObservedAt.After(trigger.CapturedAt) {
					target := base
					target.Status = "bound_auto"
					target.CommitSHA = trigger.CommitSHA
					target.Branch = trigger.Branch
					return target, candidate
				}
			}
		}
		return candidate, candidate
	default:
		base.Status = "unbound"
		return base, client.AttributionTarget{}
	}
}

func compactRevision(closed CompactClosed, target client.AttributionTarget, reason string, restatedAt time.Time) client.AttributionRevision {
	sequence := closed.CurrentSequence + 1
	return client.AttributionRevision{
		RevisionID:      digestSequence([]string{closed.BucketID, "allocation", fmt.Sprintf("%d", sequence), compactTargetKey(target)}),
		Sequence:        sequence,
		Reason:          reason,
		EvidenceVersion: compactExtractorVersion,
		Allocations:     []client.AttributionAllocation{{Target: target, Tokens: closed.Tokens}},
		RestatedAt:      restatedAt.UTC(),
	}
}

func compactTriggerFromRunOptions(opts CompactRunOptions) (CompactTrigger, bool) {
	if strings.TrimSpace(opts.TriggerKind) != "post-commit" || strings.TrimSpace(opts.CommitSHA) == "" {
		return CompactTrigger{}, false
	}
	trigger := CompactTrigger{
		Kind:         "post-commit",
		RepoConfigID: opts.RepoConfigID,
		RepoKey:      opts.RepoKey,
		WorkspaceID:  opts.WorkspaceID,
		CommitSHA:    opts.CommitSHA,
		Branch:       opts.Branch,
		CapturedAt:   opts.Cutoff.UTC(),
	}
	trigger = normalizeCompactTrigger(trigger)
	return trigger, validateCompactTrigger(trigger) == nil
}

func normalizeCompactTrigger(trigger CompactTrigger) CompactTrigger {
	trigger.Kind = strings.TrimSpace(trigger.Kind)
	trigger.RepoKey = strings.TrimSpace(trigger.RepoKey)
	trigger.WorkspaceID = strings.TrimSpace(trigger.WorkspaceID)
	trigger.CommitSHA = strings.TrimSpace(trigger.CommitSHA)
	trigger.Branch = strings.TrimSpace(trigger.Branch)
	trigger.OldCommitSHA = strings.TrimSpace(trigger.OldCommitSHA)
	trigger.NewCommitSHA = strings.TrimSpace(trigger.NewCommitSHA)
	trigger.RewriteType = strings.TrimSpace(trigger.RewriteType)
	trigger.LineageKind = strings.TrimSpace(trigger.LineageKind)
	trigger.SourceCommitSHA = strings.TrimSpace(trigger.SourceCommitSHA)
	trigger.CapturedAt = trigger.CapturedAt.UTC()
	if strings.TrimSpace(trigger.ID) == "" {
		trigger.ID = digestSequence([]string{
			trigger.Kind,
			fmt.Sprintf("%d", trigger.RepoConfigID),
			trigger.WorkspaceID,
			trigger.CommitSHA,
			trigger.OldCommitSHA,
			trigger.NewCommitSHA,
			trigger.RewriteType,
			trigger.LineageKind,
			trigger.SourceCommitSHA,
		})
	}
	return trigger
}

func validateCompactTrigger(trigger CompactTrigger) error {
	if trigger.ID == "" || trigger.RepoConfigID <= 0 || trigger.RepoKey == "" || trigger.WorkspaceID == "" || trigger.CapturedAt.IsZero() {
		return fmt.Errorf("compact trigger identity, repository, workspace, and timestamp are required")
	}
	switch trigger.Kind {
	case "post-commit":
		if trigger.CommitSHA == "" {
			return fmt.Errorf("compact commit trigger requires commit_sha")
		}
	case "post-rewrite":
		if trigger.OldCommitSHA == "" || trigger.NewCommitSHA == "" {
			return fmt.Errorf("compact rewrite trigger requires old and new commit sha")
		}
	default:
		return fmt.Errorf("unsupported compact trigger kind %q", trigger.Kind)
	}
	return nil
}

func appendCompactTrigger(state *CompactState, trigger CompactTrigger) {
	for index, existing := range state.Triggers {
		if existing.ID == trigger.ID || sameCompactTrigger(existing, trigger) {
			// The hook-queued trigger can contain richer lineage evidence than the
			// coalesced sync task. Merge only missing fields and keep its stable ID.
			if state.Triggers[index].Branch == "" {
				state.Triggers[index].Branch = trigger.Branch
			}
			if state.Triggers[index].LineageKind == "" {
				state.Triggers[index].LineageKind = trigger.LineageKind
			}
			if state.Triggers[index].SourceCommitSHA == "" {
				state.Triggers[index].SourceCommitSHA = trigger.SourceCommitSHA
			}
			if trigger.CapturedAt.Before(state.Triggers[index].CapturedAt) {
				state.Triggers[index].CapturedAt = trigger.CapturedAt
			}
			return
		}
	}
	state.Triggers = append(state.Triggers, trigger)
}

func sameCompactTrigger(left, right CompactTrigger) bool {
	return left.Kind == right.Kind &&
		left.RepoConfigID == right.RepoConfigID &&
		left.WorkspaceID == right.WorkspaceID &&
		left.CommitSHA == right.CommitSHA &&
		left.OldCommitSHA == right.OldCommitSHA &&
		left.NewCommitSHA == right.NewCommitSHA &&
		left.RewriteType == right.RewriteType
}

func relevantCompactTriggers(triggers []CompactTrigger, opts CompactRunOptions) []CompactTrigger {
	result := make([]CompactTrigger, 0, len(triggers))
	for _, trigger := range triggers {
		if trigger.RepoConfigID == opts.RepoConfigID && trigger.WorkspaceID == opts.WorkspaceID {
			result = append(result, trigger)
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].CapturedAt.Equal(result[j].CapturedAt) {
			return result[i].ID < result[j].ID
		}
		return result[i].CapturedAt.Before(result[j].CapturedAt)
	})
	return result
}

func compactRevisionReason(status string) string {
	switch status {
	case "bound_auto":
		return "commit checkpoint closed compact Codex bucket"
	case "multi_repo_shared":
		return "one Codex response touched multiple repositories"
	default:
		return "insufficient repository evidence"
	}
}

func compactTargetKey(target client.AttributionTarget) string {
	associated := append([]int(nil), target.AssociatedRepoConfigIDs...)
	sort.Ints(associated)
	parts := []string{target.Status, fmt.Sprintf("%d", target.RepoConfigID), target.RepoKey, target.WorkspaceID, target.CommitSHA, target.Branch, target.Lineage}
	for _, id := range associated {
		parts = append(parts, fmt.Sprintf("%d", id))
	}
	inherited := append([]client.AttributionCommitReference(nil), target.InheritedCommits...)
	sort.Slice(inherited, func(i, j int) bool {
		return compactCommitReferenceKey(inherited[i]) < compactCommitReferenceKey(inherited[j])
	})
	for _, reference := range inherited {
		parts = append(parts, compactCommitReferenceKey(reference))
	}
	return strings.Join(parts, "\x1f")
}

func isCompactBound(status string) bool {
	return status == "bound_auto" || status == "bound_manual"
}

func containsAnyString(values []string, candidates map[string]struct{}) bool {
	for _, value := range values {
		if _, ok := candidates[value]; ok {
			return true
		}
	}
	return false
}

func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func digestStrings(values []string) string {
	copyValues := append([]string(nil), values...)
	sort.Strings(copyValues)
	sum := sha256.Sum256([]byte(strings.Join(copyValues, "\x00")))
	return hex.EncodeToString(sum[:])
}

func digestSequence(values []string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(sum[:])
}

func withCompactStateLock(ctx context.Context, fn func() error) error {
	return withCompactFileLock(ctx, CompactStatePath()+".lock", "compact attribution state is busy", fn)
}

func withCompactRunLock(ctx context.Context, fn func() error) error {
	return withCompactFileLock(ctx, CompactStatePath()+".run.lock", "compact attribution sync is busy", fn)
}

func withCompactFileLock(ctx context.Context, lockPath, busyMessage string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return err
	}
	for attempt := 0; attempt < 200; attempt++ {
		file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_ = file.Close()
			defer os.Remove(lockPath)
			return fn()
		}
		if !os.IsExist(err) {
			return err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 5*time.Minute {
			_ = os.Remove(lockPath)
			continue
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	return fmt.Errorf("%s", busyMessage)
}
