package attributionlocal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PilotV2ClaimScan is the claim source backed by LoongSuite Pilot, standing
// where the Codex session-file scan stood.
//
// It presents Pilot's whole output directory as a single source rather than one
// source per file, because a turn is not confined to a file: a conversation
// running past midnight has its tool call in one day's file and the response
// that priced it in the next. Splitting them into separate sources would split
// the turn, and half a turn proves nothing.
//
// Rescanning a source that has already been scanned is cheap in consequence,
// though not in time: a claim is named by its commit and its evidence digest,
// both content-derived, so a second scan of the same work produces the same
// group and the backend resolves it to no change. That is what makes a
// whole-directory source acceptable — the directory grows with every turn, so
// its key changes constantly, and a key that changes constantly would be
// unusable if rescanning double counted.
type PilotV2ClaimScan struct {
	outputDir string
	sourceKey string
	files     []string
}

// PrepareLocalV2ClaimScan resolves what Pilot currently holds.
//
// Files last written before the cutoff are left out. They describe work whose
// commits were claimed long ago, and reading them would grow the cost of every
// scan without changing its result.
func PrepareLocalV2ClaimScan(outputDir string, cutoff time.Time) (*PilotV2ClaimScan, error) {
	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = DefaultPilotOutputDir()
	}
	if dir == "" {
		return nil, fmt.Errorf("resolve Pilot output directory")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Pilot is not installed on this machine. An empty scan produces no
			// claims and no error, which is what the caller wants: usage still
			// flows through the per-agent readers.
			return &PilotV2ClaimScan{outputDir: dir}, nil
		}
		return nil, fmt.Errorf("read Pilot output directory: %w", err)
	}

	type stamped struct {
		path string
		mod  int64
		size int64
	}
	var kept []stamped
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !cutoff.IsZero() && info.ModTime().UTC().Before(cutoff) {
			continue
		}
		kept = append(kept, stamped{
			path: filepath.Join(dir, entry.Name()),
			mod:  info.ModTime().UnixNano(),
			size: info.Size(),
		})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].path < kept[j].path })

	scan := &PilotV2ClaimScan{outputDir: dir}
	if len(kept) == 0 {
		return scan, nil
	}
	parts := make([]string, 0, len(kept)*3)
	for _, item := range kept {
		parts = append(parts, item.path, fmt.Sprintf("%d", item.mod), fmt.Sprintf("%d", item.size))
		scan.files = append(scan.files, item.path)
	}
	scan.sourceKey = claimDigest(parts...)
	return scan, nil
}

// SourceKeys is the one key naming everything Pilot currently holds, or nothing
// when it holds nothing.
func (s *PilotV2ClaimScan) SourceKeys() []string {
	if s == nil || s.sourceKey == "" {
		return nil
	}
	return []string{s.sourceKey}
}

// SourceEvidenceKey has nothing to report.
//
// Its Codex counterpart returns a digest of the relay request evidence covering
// a source's turns, so that evidence arriving late re-opens a source that was
// already scanned. Pilot claims are priced from what Pilot observed and never
// wait on the relay, so no such evidence exists to arrive late.
func (s *PilotV2ClaimScan) SourceEvidenceKey(turnKeys []string) string {
	return ""
}

// FinalizeCandidates has nothing to do, for the same reason.
func (s *PilotV2ClaimScan) FinalizeCandidates(candidates []V2ClaimCandidate) {}

// ScanSource reads Pilot's output once and returns the claims it proves for
// each commit asked about.
func (s *PilotV2ClaimScan) ScanSource(ctx context.Context, sourceKey string, options []V2ClaimScanOptions) ([]V2ClaimCandidate, error) {
	if s == nil || sourceKey != s.sourceKey || len(options) == 0 {
		return nil, nil
	}
	var out []V2ClaimCandidate
	// Resolved once per repository rather than once per commit: every trigger in
	// one runner pass shares a repository, and the lookup reads the opening line
	// of every Codex session file.
	sessionsByRepo := map[string]map[string]struct{}{}
	for _, option := range options {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		sessions, resolved := sessionsByRepo[option.RepoRoot]
		if !resolved {
			sessions = CodexWorkspaceSessionIDs(ctx, "", option.RepoRoot)
			sessionsByRepo[option.RepoRoot] = sessions
		}
		result, err := ScanPilotClaims(ctx, PilotScanOptions{
			V2ClaimScanOptions:  option,
			OutputDir:           s.outputDir,
			WorkspaceSessionIDs: sessions,
		})
		if err != nil {
			return nil, fmt.Errorf("scan Pilot claims: %w", err)
		}
		out = append(out, result.Claims...)
	}
	return out, nil
}
