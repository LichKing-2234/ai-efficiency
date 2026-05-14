package attributionlocal

import (
	"context"
	"os"
	"path/filepath"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type BackendClient interface {
	SendToolUsageEvent(ctx context.Context, req client.ToolUsageEventRequest) error
	BindToolUsageEvents(ctx context.Context, req client.BindToolUsageEventsRequest) error
}

type SyncEngine struct {
	Scanner   *Scanner
	Client    BackendClient
	spoolPath string
}

func NewSyncEngine(c BackendClient) *SyncEngine {
	return &SyncEngine{
		Scanner: NewScanner(),
		Client:  c,
	}
}

func (e *SyncEngine) Replay(ctx context.Context, workspaceRoot string) error {
	spooled, err := loadSpooledEvents(e.spoolPath)
	if err != nil {
		return err
	}
	if len(spooled) == 0 {
		return nil
	}

	remaining := make([]LocalToolUsageEvent, 0, len(spooled))
	for idx, ev := range spooled {
		if err := e.Client.SendToolUsageEvent(ctx, toClientUsageRequest(ev)); err != nil {
			remaining = append(remaining, spooled[idx:]...)
			if err := SaveJSON(e.spoolPath, remaining); err != nil {
				return err
			}
			return err
		}
	}
	return clearSpooledEvents(e.spoolPath)
}

func (e *SyncEngine) RunForWorkspace(ctx context.Context, workspaceRoot string) error {
	if e.Scanner == nil {
		e.Scanner = NewScanner()
	}
	statePath := filepath.Join(AttributionRootDir(), "scan-state.json")
	spoolPath := filepath.Join(AttributionRootDir(), "spool.json")
	e.spoolPath = spoolPath

	var state ScanState
	if err := LoadJSON(statePath, &state); err != nil && !os.IsNotExist(err) {
		return err
	}

	if err := e.Replay(ctx, workspaceRoot); err != nil {
		return err
	}

	events, nextState, err := e.Scanner.ScanWorkspace(workspaceRoot, state)
	if err != nil {
		return err
	}
	for _, ev := range events {
		if err := e.Client.SendToolUsageEvent(ctx, toClientUsageRequest(ev)); err != nil {
			return err
		}
	}
	return SaveJSON(statePath, nextState)
}

func toClientUsageRequest(ev LocalToolUsageEvent) client.ToolUsageEventRequest {
	return client.ToolUsageEventRequest{
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
		RawSourcePath:     ev.RawSourcePath,
		RawSourceLocator:  ev.RawSourceLocator,
		RawPayload:        ev.RawPayload,
	}
}

func loadSpooledEvents(path string) ([]LocalToolUsageEvent, error) {
	if path == "" {
		return nil, nil
	}
	var out []LocalToolUsageEvent
	if err := LoadJSON(path, &out); err != nil {
		return nil, err
	}
	return out, nil
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
