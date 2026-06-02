package attributionlocal

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type BackendClient interface {
	SendToolUsageEvent(ctx context.Context, req client.ToolUsageEventRequest) error
}

type BatchBackendClient interface {
	SendToolUsageEvents(ctx context.Context, reqs []client.ToolUsageEventRequest) error
}

type SyncEngine struct {
	Scanner   *Scanner
	Client    BackendClient
	spoolPath string
}

type RunOptions struct {
	WorkspaceRoot string
	WorkspaceID   string
	ServerURL     string
	AuthSubject   string
	RepoConfigID  int
	RepoKey       string
	DurableReplay bool
	ManagedUpload bool
}

const toolUsageReplayBatchSize = 100

func NewSyncEngine(c BackendClient) *SyncEngine {
	return &SyncEngine{
		Scanner: NewScanner(),
		Client:  c,
	}
}

func (e *SyncEngine) Replay(ctx context.Context, workspaceRoot string) error {
	return e.replay(ctx, workspaceRoot, RunOptions{})
}

func (e *SyncEngine) replay(ctx context.Context, workspaceRoot string, opts RunOptions) error {
	if e == nil || e.Client == nil {
		return nil
	}
	spooled, err := loadSpooledEvents(e.spoolPath)
	if err != nil {
		return err
	}
	if len(spooled) == 0 {
		return nil
	}

	sortSpooledEventsForReplay(spooled)
	candidates := make([]LocalToolUsageEvent, 0, len(spooled))
	var deferred []LocalToolUsageEvent
	filterByBinding := hasStableRunBinding(opts)
	for _, ev := range spooled {
		ev = normalizeObservedWindow(ev)
		if filterByBinding && !eventMatchesRunOptions(ev, opts) {
			deferred = append(deferred, ev)
			_ = appendToolUsageLedger(opts.WorkspaceID, toolUsageLedgerRecord{
				Version:      1,
				Kind:         "tool_usage",
				DedupeKey:    ev.DedupeKey,
				ServerURL:    opts.ServerURL,
				AuthSubject:  opts.AuthSubject,
				RepoConfigID: opts.RepoConfigID,
				RepoKey:      opts.RepoKey,
				WorkspaceID:  opts.WorkspaceID,
				Status:       "deferred",
				AttemptCount: 1,
				AttemptedAt:  time.Now().UTC(),
				LastError:    "context mismatch",
			})
			continue
		}
		candidates = append(candidates, ev)
	}
	persistRemaining := func(uploaded int) error {
		remaining := append([]LocalToolUsageEvent{}, deferred...)
		remaining = append(remaining, candidates[uploaded:]...)
		if len(remaining) == 0 {
			return clearSpooledEvents(e.spoolPath)
		}
		return saveSpooledEvents(e.spoolPath, remaining)
	}
	uploaded, err := e.sendSpooledEvents(ctx, candidates, persistRemaining)
	if err != nil {
		if err := persistRemaining(uploaded); err != nil {
			return err
		}
		return err
	}
	return persistRemaining(len(candidates))
}

func (e *SyncEngine) sendSpooledEvents(ctx context.Context, events []LocalToolUsageEvent, onProgress func(uploaded int) error) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}
	if batchClient, ok := e.Client.(BatchBackendClient); ok {
		for start := 0; start < len(events); start += toolUsageReplayBatchSize {
			end := start + toolUsageReplayBatchSize
			if end > len(events) {
				end = len(events)
			}
			reqs := make([]client.ToolUsageEventRequest, 0, end-start)
			for _, ev := range events[start:end] {
				reqs = append(reqs, toClientUsageRequest(ev))
			}
			if err := batchClient.SendToolUsageEvents(ctx, reqs); err != nil {
				if client.IsToolUsageBatchIsolationError(err) {
					uploaded, singleErr := e.sendSpooledEventsIndividually(ctx, events[start:end], start, onProgress)
					if singleErr != nil {
						return uploaded, singleErr
					}
					continue
				}
				return start, err
			}
			if onProgress != nil {
				if err := onProgress(end); err != nil {
					return end, err
				}
			}
		}
		return len(events), nil
	}
	return e.sendSpooledEventsIndividually(ctx, events, 0, onProgress)
}

func (e *SyncEngine) sendSpooledEventsIndividually(ctx context.Context, events []LocalToolUsageEvent, offset int, onProgress func(uploaded int) error) (int, error) {
	for idx, ev := range events {
		uploaded := offset + idx
		if err := e.Client.SendToolUsageEvent(ctx, toClientUsageRequest(ev)); err != nil {
			if client.IsPermanentToolUsageError(err) {
				if dlErr := appendToolUsageDeadLetter(e.spoolPath, ev, err); dlErr != nil {
					return uploaded, dlErr
				}
				if onProgress != nil {
					if progressErr := onProgress(uploaded + 1); progressErr != nil {
						return uploaded + 1, progressErr
					}
				}
				continue
			}
			return uploaded, err
		}
		if onProgress != nil {
			if err := onProgress(uploaded + 1); err != nil {
				return uploaded + 1, err
			}
		}
	}
	return offset + len(events), nil
}

func (e *SyncEngine) RunForWorkspace(ctx context.Context, workspaceRoot string) error {
	return e.Run(ctx, RunOptions{WorkspaceRoot: workspaceRoot, DurableReplay: true})
}

func (e *SyncEngine) Run(ctx context.Context, opts RunOptions) error {
	if e.Scanner == nil {
		e.Scanner = NewScanner()
	}
	statePath, spoolPath, workspaceID, err := workspaceStatePaths(opts.WorkspaceRoot)
	if err != nil {
		return err
	}
	if opts.WorkspaceID != "" {
		workspaceID = opts.WorkspaceID
		base := filepath.Join(AttributionRootDir(), "workspaces", workspaceID)
		statePath = filepath.Join(base, "scan-state.json")
		spoolPath = filepath.Join(base, "spool.json")
	}
	e.spoolPath = spoolPath

	if opts.DurableReplay && e.Client != nil {
		if err := e.replay(ctx, opts.WorkspaceRoot, opts); err != nil {
			return err
		}
	}

	var state ScanState
	if err := LoadJSON(statePath, &state); err != nil && !os.IsNotExist(err) {
		return err
	}

	events, nextState, err := e.Scanner.ScanWorkspaceContext(ctx, opts.WorkspaceRoot, state)
	if err != nil {
		return err
	}
	for idx := range events {
		events[idx] = normalizeObservedWindow(events[idx])
		events[idx].WorkspaceID = workspaceID
		events[idx].ServerURL = opts.ServerURL
		events[idx].AuthSubject = opts.AuthSubject
		events[idx].RepoConfigID = opts.RepoConfigID
		events[idx].RepoKey = opts.RepoKey
		events[idx].ManagedUpload = opts.ManagedUpload
	}

	if e.Client == nil {
		if opts.DurableReplay {
			if err := appendSpooledEvents(spoolPath, events); err != nil {
				return err
			}
		}
		return SaveJSON(statePath, nextState)
	}

	if opts.DurableReplay {
		if len(events) > 0 {
			if err := appendSpooledEvents(spoolPath, events); err != nil {
				return err
			}
			if err := SaveJSON(statePath, nextState); err != nil {
				return err
			}
		}
		if err := e.replay(ctx, opts.WorkspaceRoot, opts); err != nil {
			return err
		}
		return nil
	}

	for idx, ev := range events {
		if err := e.Client.SendToolUsageEvent(ctx, toClientUsageRequest(ev)); err != nil {
			if opts.DurableReplay {
				if spoolErr := appendSpooledEvents(spoolPath, events[idx:]); spoolErr != nil {
					return spoolErr
				}
			}
			if saveErr := SaveJSON(statePath, nextState); saveErr != nil {
				return saveErr
			}
			return err
		}
	}
	return SaveJSON(statePath, nextState)
}

func (e *SyncEngine) runLegacy(ctx context.Context, workspaceRoot string) error {
	if e.Scanner == nil {
		e.Scanner = NewScanner()
	}
	statePath, spoolPath, workspaceID, err := workspaceStatePaths(workspaceRoot)
	if err != nil {
		return err
	}
	e.spoolPath = spoolPath
	_ = workspaceID

	var state ScanState
	if err := LoadJSON(statePath, &state); err != nil && !os.IsNotExist(err) {
		return err
	}

	events, nextState, err := e.Scanner.ScanWorkspaceContext(ctx, workspaceRoot, state)
	if err != nil {
		return err
	}
	for idx := range events {
		events[idx] = normalizeObservedWindow(events[idx])
	}

	if e.Client == nil {
		if err := appendSpooledEvents(spoolPath, events); err != nil {
			return err
		}
		return SaveJSON(statePath, nextState)
	}

	if len(events) > 0 {
		if err := appendSpooledEvents(spoolPath, events); err != nil {
			return err
		}
		if err := SaveJSON(statePath, nextState); err != nil {
			return err
		}
	}
	if err := e.Replay(ctx, workspaceRoot); err != nil {
		return err
	}
	return nil
}

func sortSpooledEventsForReplay(events []LocalToolUsageEvent) {
	for idx := range events {
		events[idx] = normalizeObservedWindow(events[idx])
	}
	sort.SliceStable(events, func(i, j int) bool {
		left := events[i].ObservedEndAt
		right := events[j].ObservedEndAt
		if left.IsZero() {
			return false
		}
		if right.IsZero() {
			return true
		}
		return left.After(right)
	})
}

func (e *SyncEngine) runLegacyDirectUpload(ctx context.Context, statePath, spoolPath string, events []LocalToolUsageEvent, nextState ScanState) error {
	for idx, ev := range events {
		if err := e.Client.SendToolUsageEvent(ctx, toClientUsageRequest(ev)); err != nil {
			if spoolErr := appendSpooledEvents(spoolPath, events[idx:]); spoolErr != nil {
				return spoolErr
			}
			if saveErr := SaveJSON(statePath, nextState); saveErr != nil {
				return saveErr
			}
			return err
		}
	}
	return SaveJSON(statePath, nextState)
}

func toClientUsageRequest(ev LocalToolUsageEvent) client.ToolUsageEventRequest {
	req := client.ToolUsageEventRequest{
		RepoConfigID:      ev.RepoConfigID,
		Tool:              ev.Tool,
		WorkspaceID:       ev.WorkspaceID,
		ToolSessionID:     ev.ToolSessionID,
		ToolEventID:       ev.ToolEventID,
		DedupeKey:         ev.DedupeKey,
		UsageUnit:         string(ev.UsageUnit),
		RequestCount:      ev.RequestCount,
		InputTokens:       ev.InputTokens,
		OutputTokens:      ev.OutputTokens,
		CachedInputTokens: ev.CachedInputTokens,
		ReasoningTokens:   ev.ReasoningTokens,
		CreditUsage:       ev.CreditUsage,
		ContextUsagePct:   ev.ContextUsagePct,
		ObservedStartAt:   ev.ObservedStartAt,
		ObservedEndAt:     ev.ObservedEndAt,
	}
	if !ev.ManagedUpload {
		req.RawSourcePath = ev.RawSourcePath
		req.RawSourceLocator = ev.RawSourceLocator
		req.RawPayload = ev.RawPayload
	}
	return req
}

func eventMatchesRunOptions(ev LocalToolUsageEvent, opts RunOptions) bool {
	return ev.WorkspaceID == opts.WorkspaceID &&
		ev.ServerURL == opts.ServerURL &&
		ev.AuthSubject == opts.AuthSubject &&
		ev.RepoConfigID == opts.RepoConfigID &&
		ev.RepoKey == opts.RepoKey
}

func hasStableRunBinding(opts RunOptions) bool {
	return opts.DurableReplay &&
		opts.WorkspaceID != "" &&
		opts.ServerURL != "" &&
		opts.AuthSubject != "" &&
		opts.RepoConfigID > 0 &&
		opts.RepoKey != ""
}

func loadSpooledEvents(path string) ([]LocalToolUsageEvent, error) {
	if path == "" {
		return nil, nil
	}
	var out []LocalToolUsageEvent
	if err := LoadJSON(path, &out); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		if strings.Contains(err.Error(), "unmarshal json:") {
			if qerr := quarantineCorruptSpool(path); qerr != nil {
				return nil, qerr
			}
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

type toolUsageDeadLetter struct {
	Version    int                 `json:"version"`
	Event      LocalToolUsageEvent `json:"event"`
	Error      string              `json:"error"`
	RecordedAt time.Time           `json:"recorded_at"`
}

func toolUsageDeadLetterPath(workspaceDir string) string {
	return filepath.Join(workspaceDir, "dead-letter-tool-usage.jsonl")
}

func CountToolUsageDeadLetters(workspaceID string) (int, error) {
	if strings.TrimSpace(workspaceID) == "" {
		return 0, nil
	}
	path := toolUsageDeadLetterPath(filepath.Join(AttributionRootDir(), "workspaces", workspaceID))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	count := 0
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) > 0 {
			count++
		}
	}
	return count, nil
}

func appendToolUsageDeadLetter(spoolPath string, ev LocalToolUsageEvent, uploadErr error) error {
	if spoolPath == "" {
		return nil
	}
	path := toolUsageDeadLetterPath(filepath.Dir(spoolPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	rec := toolUsageDeadLetter{
		Version:    1,
		Event:      ev,
		Error:      uploadErr.Error(),
		RecordedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

func loadToolUsageDeadLetters(workspaceDir string) ([]toolUsageDeadLetter, error) {
	path := toolUsageDeadLetterPath(workspaceDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []toolUsageDeadLetter
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec toolUsageDeadLetter
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func quarantineCorruptSpool(path string) error {
	backup := fmt.Sprintf("%s.corrupt.%d", path, time.Now().UTC().UnixNano())
	if err := os.Rename(path, backup); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("quarantine corrupt spool: %w", err)
	}
	return nil
}

func appendSpooledEvents(path string, events []LocalToolUsageEvent) error {
	if path == "" || len(events) == 0 {
		return nil
	}
	existing, err := loadSpooledEvents(path)
	if err != nil {
		return err
	}
	merged := append(existing, events...)
	return saveSpooledEvents(path, dedupeAndSort(merged))
}

func clearSpooledEvents(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func saveSpooledEvents(path string, events []LocalToolUsageEvent) error {
	if path == "" {
		return nil
	}
	if len(events) == 0 {
		return clearSpooledEvents(path)
	}
	events = compactManagedSpooledEvents(events)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create spool dir: %w", err)
	}
	data, err := json.Marshal(events)
	if err != nil {
		return fmt.Errorf("marshal spool json: %w", err)
	}
	tmp := fmt.Sprintf("%s.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp spool json: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename spool json: %w", err)
	}
	return nil
}

func compactManagedSpooledEvents(events []LocalToolUsageEvent) []LocalToolUsageEvent {
	if len(events) == 0 {
		return events
	}
	out := make([]LocalToolUsageEvent, len(events))
	copy(out, events)
	for idx := range out {
		if !out[idx].ManagedUpload {
			continue
		}
		out[idx].RawSourcePath = ""
		out[idx].RawSourceLocator = ""
		out[idx].RawPayload = nil
	}
	return out
}

type toolUsageLedgerRecord struct {
	Version      int        `json:"version"`
	Kind         string     `json:"kind"`
	DedupeKey    string     `json:"dedupe_key"`
	ServerURL    string     `json:"server_url"`
	AuthSubject  string     `json:"auth_subject"`
	RepoConfigID int        `json:"repo_config_id"`
	RepoKey      string     `json:"repo_key"`
	WorkspaceID  string     `json:"workspace_id"`
	Status       string     `json:"status"`
	AttemptCount int        `json:"attempt_count"`
	AttemptedAt  time.Time  `json:"attempted_at"`
	UploadedAt   *time.Time `json:"uploaded_at,omitempty"`
	HTTPStatus   int        `json:"http_status,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

func appendToolUsageLedger(workspaceID string, rec toolUsageLedgerRecord) error {
	if workspaceID == "" {
		return nil
	}
	path := filepath.Join(AttributionRootDir(), "workspaces", workspaceID, "upload-ledger.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if rec.Version == 0 {
		rec.Version = 1
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	return err
}

func workspaceStatePaths(workspaceRoot string) (statePath, spoolPath, workspaceID string, err error) {
	workspaceID, err = mustWorkspaceID(workspaceRoot)
	if err != nil {
		return "", "", "", err
	}
	base := filepath.Join(AttributionRootDir(), "workspaces", workspaceID)
	return filepath.Join(base, "scan-state.json"), filepath.Join(base, "spool.json"), workspaceID, nil
}
