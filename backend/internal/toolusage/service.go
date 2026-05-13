package toolusage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
)

type CreateUsageEventRequest struct {
	Tool              string
	WorkspaceID       string
	RepoConfigID      int
	UserID            int
	ToolSessionID     string
	ToolEventID       string
	DedupeKey         string
	UsageUnit         string
	RequestCount      int
	InputTokens       int64
	OutputTokens      int64
	CachedInputTokens int64
	ReasoningTokens   int64
	CreditUsage       float64
	ContextUsagePct   float64
	ObservedStartAt   time.Time
	ObservedEndAt     time.Time
	RawSourcePath     string
	RawSourceLocator  string
	RawPayload        map[string]any
}

type BindUsageEventsRequest struct {
	WorkspaceID        string
	CommitCheckpointID int
	CommitCapturedAt   time.Time
	PreviousCapturedAt time.Time
}

type Service struct {
	entClient *ent.Client
}

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) CreateUsageEvent(ctx context.Context, req CreateUsageEventRequest) error {
	if s.entClient == nil {
		return fmt.Errorf("create tool usage event: ent client is required")
	}

	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return fmt.Errorf("create tool usage event: workspace_id is required")
	}
	if req.RepoConfigID <= 0 {
		return fmt.Errorf("create tool usage event: repo_config_id is required")
	}
	if req.UserID <= 0 {
		return fmt.Errorf("create tool usage event: user_id is required")
	}
	dedupeKey := strings.TrimSpace(req.DedupeKey)
	if dedupeKey == "" {
		return fmt.Errorf("create tool usage event: dedupe_key is required")
	}

	exists, err := s.entClient.ToolUsageEvent.Query().
		Where(toolusageevent.DedupeKeyEQ(dedupeKey)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check dedupe: %w", err)
	}
	if exists {
		return nil
	}

	create := s.entClient.ToolUsageEvent.Create().
		SetTool(strings.TrimSpace(req.Tool)).
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(req.RepoConfigID).
		SetUserID(req.UserID).
		SetToolSessionID(strings.TrimSpace(req.ToolSessionID)).
		SetDedupeKey(dedupeKey).
		SetUsageUnit(toolusageevent.UsageUnit(strings.TrimSpace(req.UsageUnit))).
		SetRequestCount(req.RequestCount).
		SetInputTokens(req.InputTokens).
		SetOutputTokens(req.OutputTokens).
		SetCachedInputTokens(req.CachedInputTokens).
		SetReasoningTokens(req.ReasoningTokens).
		SetCreditUsage(req.CreditUsage).
		SetContextUsagePct(req.ContextUsagePct).
		SetObservedStartAt(req.ObservedStartAt.UTC()).
		SetObservedEndAt(req.ObservedEndAt.UTC())

	if v := strings.TrimSpace(req.ToolEventID); v != "" {
		create.SetToolEventID(v)
	}
	if v := strings.TrimSpace(req.RawSourcePath); v != "" {
		create.SetRawSourcePath(v)
	}
	if v := strings.TrimSpace(req.RawSourceLocator); v != "" {
		create.SetRawSourceLocator(v)
	}
	if req.RawPayload != nil {
		create.SetRawPayload(req.RawPayload)
	}

	if err := create.Exec(ctx); err != nil {
		if ent.IsConstraintError(err) {
			exists, qerr := s.entClient.ToolUsageEvent.Query().
				Where(toolusageevent.DedupeKeyEQ(dedupeKey)).
				Exist(ctx)
			if qerr == nil && exists {
				return nil
			}
		}
		return fmt.Errorf("create tool usage event: %w", err)
	}

	return nil
}

func (s *Service) BindUsageEventsToCheckpoint(ctx context.Context, req BindUsageEventsRequest) (int, error) {
	if s.entClient == nil {
		return 0, fmt.Errorf("bind tool usage events: ent client is required")
	}

	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return 0, fmt.Errorf("bind tool usage events: workspace_id is required")
	}

	items, err := s.entClient.ToolUsageEvent.Query().
		Where(
			toolusageevent.WorkspaceIDEQ(workspaceID),
			toolusageevent.CommitCheckpointIDIsNil(),
			toolusageevent.ObservedEndAtLTE(req.CommitCapturedAt),
			toolusageevent.ObservedEndAtGT(req.PreviousCapturedAt),
		).
		All(ctx)
	if err != nil {
		return 0, fmt.Errorf("query tool usage events: %w", err)
	}

	for _, item := range items {
		if _, err := s.entClient.ToolUsageEvent.UpdateOneID(item.ID).
			SetCommitCheckpointID(req.CommitCheckpointID).
			Save(ctx); err != nil {
			return 0, fmt.Errorf("bind tool usage event %d: %w", item.ID, err)
		}
	}

	return len(items), nil
}
