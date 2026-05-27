package attributionlocal

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type BackendClient interface {
	SendToolUsageEvent(ctx context.Context, req client.ToolUsageEventRequest) error
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
	remaining := make([]LocalToolUsageEvent, 0, len(spooled))
	filterByBinding := hasStableRunBinding(opts)
	for idx, ev := range spooled {
		ev = normalizeObservedWindow(ev)
		if filterByBinding && !eventMatchesRunOptions(ev, opts) {
			_ = appendToolUsageLedger(opts.WorkspaceID, toolUsageLedgerRecord{
				Version:      1,
				Kind:         "tool_usage",
				DedupeKey:    ev.DedupeKey,
				ServerURL:    opts.ServerURL,
				AuthSubject:  opts.AuthSubject,
				RepoConfigID: opts.RepoConfigID,
				RepoKey:      opts.RepoKey,
				WorkspaceID:  opts.WorkspaceID,
				Status:       "skipped",
				AttemptCount: 1,
				AttemptedAt:  time.Now().UTC(),
				LastError:    "context mismatch",
			})
			continue
		}
		if err := e.Client.SendToolUsageEvent(ctx, toClientUsageRequest(ev)); err != nil {
			remaining = append(remaining, ev)
			for _, queued := range spooled[idx+1:] {
				queued = normalizeObservedWindow(queued)
				if filterByBinding && !eventMatchesRunOptions(queued, opts) {
					continue
				}
				remaining = append(remaining, queued)
			}
			if err := SaveJSON(e.spoolPath, remaining); err != nil {
				return err
			}
			return err
		}
	}
	return clearSpooledEvents(e.spoolPath)
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
		return nil, err
	}
	return out, nil
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
	return SaveJSON(path, dedupeAndSort(merged))
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
