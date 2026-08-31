package attributionlocal

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/hookstate"
	"github.com/ai-efficiency/ae-cli/internal/pilot"
	"github.com/ai-efficiency/ae-cli/internal/session"
)

type ScanState struct {
	CodexSQLite     map[string]CodexSQLiteWatermark `json:"codex_sqlite,omitempty"`
	CodexSessionIDs map[string]CodexSessionIDCache  `json:"codex_session_ids,omitempty"`
	FileModUnix     map[string]int64                `json:"file_mod_unix,omitempty"`
}

type Scanner struct {
	// PilotOutputDir overrides where LoongSuite Pilot's normalized local JSONL is
	// read from. Empty means the default install location.
	PilotOutputDir string
	// PilotRunning overrides the collector liveness check. Nil means the real
	// one.
	PilotRunning func() bool
}

// pilotCollecting reports whether Pilot is currently collecting, and so whether
// its output is the right source to read.
//
// A stopped collector must not be read as the source. Its output directory
// survives being turned off, full of everything it recorded before, so deciding
// on the directory alone would keep the per-agent readers suppressed while
// nothing was replacing them — and every agent turn taken from then on would go
// uncounted, silently. Falling back instead means a stopped collector costs
// duplicate keys for the overlap rather than the whole gap.
func (s *Scanner) pilotCollecting() bool {
	if s != nil && s.PilotRunning != nil {
		return s.PilotRunning()
	}
	return pilot.Checker{DataDir: s.pilotDataDir()}.Running()
}

// pilotDataDir derives Pilot's data directory from the configured output
// directory, so a test that redirects the output also redirects the liveness
// check.
func (s *Scanner) pilotDataDir() string {
	dir := strings.TrimSpace(s.PilotOutputDir)
	if dir == "" {
		return ""
	}
	return filepath.Dir(filepath.Dir(dir))
}

func NewScanner() *Scanner {
	return &Scanner{}
}

func (s *Scanner) ScanWorkspace(workspaceRoot string, state ScanState) ([]LocalToolUsageEvent, ScanState, error) {
	return s.ScanWorkspaceContext(context.Background(), workspaceRoot, state)
}

func (s *Scanner) ScanWorkspaceContext(ctx context.Context, workspaceRoot string, state ScanState) ([]LocalToolUsageEvent, ScanState, error) {
	var out []LocalToolUsageEvent
	workspaceID, err := mustWorkspaceID(workspaceRoot)
	if err != nil {
		return nil, state, err
	}
	homeDir, _ := os.UserHomeDir()
	nextState := cloneScanState(state)

	// Pilot, when installed, is the source for every agent it instruments. The
	// per-agent readers below stay as the fallback for a machine without it.
	if usage, ok := s.scanPilotUsage(ctx, workspaceRoot, workspaceID); ok {
		out = append(out, usage...)
		kiroIDE, err := s.scanKiroIDE(ctx, workspaceRoot, workspaceID, homeDir, state, &nextState)
		if err != nil {
			return nil, state, err
		}
		return dedupeAndSort(append(out, kiroIDE...)), nextState, nil
	}

	codexJSONLFiles := findCodexJSONLFiles(workspaceRoot, homeDir)
	codexSessionIDs := make(map[string]struct{})
	for _, path := range codexJSONLFiles {
		sessionIDs, err := cachedCodexWorkspaceSessionIDs(ctx, &nextState, path, workspaceRoot)
		if err != nil {
			return nil, state, err
		}
		for _, sessionID := range sessionIDs {
			codexSessionIDs[sessionID] = struct{}{}
		}
	}

	for _, path := range findCodexSQLiteFiles(homeDir) {
		parser := NewCodexSQLiteParser()
		events, watermark, err := parser.Parse(path, nextState.CodexSQLite[path])
		if err != nil {
			continue
		}
		nextState.CodexSQLite[path] = watermark
		for _, item := range events {
			if _, ok := codexSessionIDs[item.ToolSessionID]; !ok {
				continue
			}
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range codexJSONLFiles {
		if err := ctx.Err(); err != nil {
			return nil, state, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseCodexJSONLFallbackContext(ctx, path, workspaceRoot)
		if err != nil {
			if ctx.Err() != nil {
				return nil, state, ctx.Err()
			}
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range findClaudeJSONLFiles(homeDir) {
		if err := ctx.Err(); err != nil {
			return nil, state, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseClaudeJSONL(path, workspaceRoot)
		if err != nil {
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	for _, path := range findKiroCLISQLiteFiles(homeDir) {
		if err := ctx.Err(); err != nil {
			return nil, state, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseKiroCLISQLite(path, workspaceRoot)
		if err != nil {
			continue
		}
		rememberFileScan(&nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	kiroIDE, err := s.scanKiroIDE(ctx, workspaceRoot, workspaceID, homeDir, state, &nextState)
	if err != nil {
		return nil, state, err
	}
	return dedupeAndSort(append(out, kiroIDE...)), nextState, nil
}

func mustWorkspaceID(workspaceRoot string) (string, error) {
	gitDir, err := resolveWorkspaceGitDir(workspaceRoot)
	if err != nil {
		gitDir = filepath.Join(workspaceRoot, ".git")
	}
	gitCommonDir := filepath.Clean(resolveGitCommonDir(gitDir))
	return session.DeriveWorkspaceID(workspaceRoot, workspaceRoot, gitDir, gitCommonDir)
}

func resolveWorkspaceGitDir(workspaceRoot string) (string, error) {
	gitPath := filepath.Join(workspaceRoot, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return filepath.EvalSymlinks(gitPath)
	}

	data, err := os.ReadFile(gitPath)
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(string(data))
	const prefix = "gitdir:"
	if !strings.HasPrefix(strings.ToLower(line), prefix) {
		return filepath.EvalSymlinks(gitPath)
	}

	rawGitDir := strings.TrimSpace(line[len(prefix):])
	if rawGitDir == "" {
		return "", os.ErrInvalid
	}
	if !filepath.IsAbs(rawGitDir) {
		rawGitDir = filepath.Join(workspaceRoot, rawGitDir)
	}
	return filepath.EvalSymlinks(rawGitDir)
}

func dedupeAndSort(items []LocalToolUsageEvent) []LocalToolUsageEvent {
	if len(items) == 0 {
		return nil
	}
	seen := map[string]LocalToolUsageEvent{}
	for _, item := range items {
		seen[spooledEventIdentity(item)] = item
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]LocalToolUsageEvent, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return out
}

func spooledEventIdentity(item LocalToolUsageEvent) string {
	return strings.Join([]string{
		strings.TrimSpace(item.DedupeKey),
		strings.TrimSpace(item.WorkspaceID),
		hookstate.NormalizeServerURL(item.ServerURL),
		strings.TrimSpace(item.AuthSubject),
		fmt.Sprintf("%d", item.RepoConfigID),
		strings.TrimSpace(item.RepoKey),
	}, "\x1f")
}

func findCodexJSONLFiles(workspaceRoot, homeDir string) []string {
	_ = workspaceRoot
	paths := walkFiles(filepath.Join(homeDir, ".codex", "sessions"), ".jsonl")
	paths = append(paths, walkFiles(filepath.Join(homeDir, ".codex", "archived_sessions"), ".jsonl")...)
	sort.Strings(paths)
	return paths
}

func findCodexSQLiteFiles(homeDir string) []string {
	dbPath := filepath.Join(strings.TrimSpace(homeDir), ".codex", "logs_2.sqlite")
	if strings.TrimSpace(homeDir) == "" {
		return nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil
	}
	return []string{dbPath}
}

func findClaudeJSONLFiles(homeDir string) []string {
	return walkFiles(filepath.Join(homeDir, ".claude"), ".jsonl")
}

func findKiroJSONFiles(homeDir string) []string {
	return walkFiles(filepath.Join(homeDir, ".kiro"), ".json")
}

const (
	kiroCLIDataDirName = "kiro-cli"
	kiroCLIDBFileName  = "data.sqlite3"
)

// kiroCLISQLiteDBCandidates resolves the Kiro CLI transcript database for one
// platform, in the order LoongSuite Pilot's hooks/kiro-cli/db-path.mjs uses:
//
//	KIRO_CLI_DB          explicit database file
//	KIRO_CLI_DATA_DIR    explicit data directory, + /data.sqlite3
//	macOS default        <home>/Library/Application Support/kiro-cli
//	Linux default        $XDG_DATA_HOME or <home>/.local/share, + /kiro-cli
//	Windows default      %APPDATA%/kiro-cli
//
// An explicit override resolves to exactly that path so a misconfigured
// override never silently falls through to another database. Windows appends
// %LOCALAPPDATA%/kiro-cli, which is where Kiro CLI's open-source ancestor puts
// the file (dirs::data_local_dir() in crates/agent/src/agent/util/directories.rs
// maps to LocalAppData on Windows); Pilot's %APPDATA% entry stays first.
func kiroCLISQLiteDBCandidates(homeDir, goos string) []string {
	homeDir = strings.TrimSpace(homeDir)

	if dbPath := strings.TrimSpace(os.Getenv("KIRO_CLI_DB")); dbPath != "" {
		return []string{expandKiroCLIHomePrefix(dbPath, homeDir)}
	}
	if dataDir := strings.TrimSpace(os.Getenv("KIRO_CLI_DATA_DIR")); dataDir != "" {
		return []string{filepath.Join(expandKiroCLIHomePrefix(dataDir, homeDir), kiroCLIDBFileName)}
	}

	switch goos {
	case "darwin":
		if homeDir == "" {
			return nil
		}
		return []string{filepath.Join(homeDir, "Library", "Application Support", kiroCLIDataDirName, kiroCLIDBFileName)}
	case "windows":
		var out []string
		if roaming := strings.TrimSpace(os.Getenv("APPDATA")); roaming != "" {
			out = append(out, filepath.Join(roaming, kiroCLIDataDirName, kiroCLIDBFileName))
		} else if homeDir != "" {
			out = append(out, filepath.Join(homeDir, "AppData", "Roaming", kiroCLIDataDirName, kiroCLIDBFileName))
		}
		if local := strings.TrimSpace(os.Getenv("LOCALAPPDATA")); local != "" {
			out = append(out, filepath.Join(local, kiroCLIDataDirName, kiroCLIDBFileName))
		} else if homeDir != "" {
			out = append(out, filepath.Join(homeDir, "AppData", "Local", kiroCLIDataDirName, kiroCLIDBFileName))
		}
		return out
	default:
		if xdgDataHome := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdgDataHome != "" {
			return []string{filepath.Join(expandKiroCLIHomePrefix(xdgDataHome, homeDir), kiroCLIDataDirName, kiroCLIDBFileName)}
		}
		if homeDir == "" {
			return nil
		}
		return []string{filepath.Join(homeDir, ".local", "share", kiroCLIDataDirName, kiroCLIDBFileName)}
	}
}

func expandKiroCLIHomePrefix(path, homeDir string) string {
	path = strings.TrimSpace(path)
	homeDir = strings.TrimSpace(homeDir)
	if homeDir == "" || path == "" {
		return path
	}
	if path == "~" {
		return homeDir
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(homeDir, path[2:])
	}
	return path
}

// FindKiroCLISQLiteFilesForCollector exposes the same platform-aware Kiro CLI
// transcript database resolution the workspace scanner uses.
func FindKiroCLISQLiteFilesForCollector(homeDir string) []string {
	return findKiroCLISQLiteFiles(homeDir)
}

func findKiroCLISQLiteFiles(homeDir string) []string {
	for _, dbPath := range kiroCLISQLiteDBCandidates(homeDir, runtime.GOOS) {
		if strings.TrimSpace(dbPath) == "" {
			continue
		}
		if _, err := os.Stat(dbPath); err != nil {
			continue
		}
		return []string{dbPath}
	}
	return nil
}

func walkFiles(root string, ext string) []string {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	var out []string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ext) {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	return out
}

func cloneScanState(state ScanState) ScanState {
	next := ScanState{
		CodexSQLite:     make(map[string]CodexSQLiteWatermark, len(state.CodexSQLite)),
		CodexSessionIDs: make(map[string]CodexSessionIDCache, len(state.CodexSessionIDs)),
		FileModUnix:     make(map[string]int64, len(state.FileModUnix)),
	}
	for path, watermark := range state.CodexSQLite {
		next.CodexSQLite[path] = watermark
	}
	for path, cache := range state.CodexSessionIDs {
		next.CodexSessionIDs[path] = CodexSessionIDCache{
			ModUnix:    cache.ModUnix,
			Size:       cache.Size,
			SessionIDs: append([]string(nil), cache.SessionIDs...),
		}
	}
	for path, mod := range state.FileModUnix {
		next.FileModUnix[path] = mod
	}
	return next
}

func cachedCodexWorkspaceSessionIDs(ctx context.Context, state *ScanState, path, workspaceRoot string) ([]string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}
	if state != nil && state.CodexSessionIDs != nil {
		if cached, ok := state.CodexSessionIDs[path]; ok && cached.ModUnix == info.ModTime().Unix() && cached.Size == info.Size() {
			return append([]string(nil), cached.SessionIDs...), nil
		}
	}

	sessionIDs, err := findCodexWorkspaceSessionIDsContext(ctx, path, workspaceRoot)
	if err != nil {
		return nil, err
	}
	if state != nil {
		if state.CodexSessionIDs == nil {
			state.CodexSessionIDs = map[string]CodexSessionIDCache{}
		}
		state.CodexSessionIDs[path] = CodexSessionIDCache{
			ModUnix:    info.ModTime().Unix(),
			Size:       info.Size(),
			SessionIDs: append([]string(nil), sessionIDs...),
		}
	}
	return sessionIDs, nil
}

func shouldScanFile(path string, state ScanState) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	if state.FileModUnix == nil {
		return true
	}
	return info.ModTime().Unix() > state.FileModUnix[path]
}

func rememberFileScan(state *ScanState, path string) {
	if state == nil {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if state.FileModUnix == nil {
		state.FileModUnix = map[string]int64{}
	}
	state.FileModUnix[path] = info.ModTime().Unix()
}

// scanPilotUsage reads usage from LoongSuite Pilot's normalized local output,
// and reports whether Pilot is present at all.
//
// When it is, Pilot is the single source for every agent it instruments —
// Codex, Claude Code, and Kiro CLI — and the per-agent readers for those three
// are not consulted. Pilot does not instrument the Kiro IDE, so the two Kiro IDE
// readers run either way.
//
// A Pilot directory that is missing or unreadable reports absent, so a broken
// collector degrades to the per-agent readers instead of halting accounting.
func (s *Scanner) scanPilotUsage(ctx context.Context, workspaceRoot, workspaceID string) ([]LocalToolUsageEvent, bool) {
	dir := strings.TrimSpace(s.PilotOutputDir)
	if dir == "" {
		dir = DefaultPilotOutputDir()
	}
	if dir == "" {
		return nil, false
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, false
	}
	if !s.pilotCollecting() {
		return nil, false
	}

	result, err := ScanPilotClaims(ctx, PilotScanOptions{
		V2ClaimScanOptions: V2ClaimScanOptions{
			RepoRoot:    workspaceRoot,
			WorkspaceID: workspaceID,
		},
		OutputDir:           dir,
		WorkspaceSessionIDs: CodexWorkspaceSessionIDs(ctx, "", workspaceRoot),
	})
	if err != nil {
		return nil, false
	}

	out := make([]LocalToolUsageEvent, 0, len(result.Usage))
	for _, item := range result.Usage {
		item.WorkspaceID = workspaceID
		out = append(out, item)
	}
	return out, true
}

// scanKiroIDE reads the two Kiro IDE surfaces. Pilot does not instrument the
// Kiro IDE, so these run whether or not Pilot is installed.
func (s *Scanner) scanKiroIDE(ctx context.Context, workspaceRoot, workspaceID, homeDir string, state ScanState, nextState *ScanState) ([]LocalToolUsageEvent, error) {
	var out []LocalToolUsageEvent

	for _, path := range findKiroJSONFiles(homeDir) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseKiroJSON(path, workspaceRoot)
		if err != nil {
			continue
		}
		rememberFileScan(nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	kiroIDESessionIDs := FindKiroIDESessionIDs(homeDir, workspaceRoot)
	for _, path := range FindKiroIDEExecutionFiles(homeDir) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !shouldScanFile(path, state) {
			continue
		}
		items, err := ParseKiroIDEExecution(path, kiroIDESessionIDs)
		if err != nil {
			continue
		}
		rememberFileScan(nextState, path)
		for _, item := range items {
			item.WorkspaceID = workspaceID
			out = append(out, item)
		}
	}

	return out, nil
}
