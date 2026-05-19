package toolusage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
)

type CreateUsageEventRequest struct {
	Tool              string
	WorkspaceID       string
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

var ErrUsageEventForbidden = errors.New("tool usage event does not belong to authenticated user")

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) CreateUsageEvent(ctx context.Context, userID int, req CreateUsageEventRequest) error {
	if s.entClient == nil {
		return fmt.Errorf("create tool usage event: ent client is required")
	}
	if userID <= 0 {
		return fmt.Errorf("create tool usage event: authenticated user is required")
	}

	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return fmt.Errorf("create tool usage event: workspace_id is required")
	}
	dedupeKey := strings.TrimSpace(req.DedupeKey)
	if dedupeKey == "" {
		return fmt.Errorf("create tool usage event: dedupe_key is required")
	}

	scope, err := s.resolveScopeByWorkspace(ctx, workspaceID)
	if err != nil {
		return err
	}
	if scope.UserID != userID {
		return fmt.Errorf("create tool usage event: %w", ErrUsageEventForbidden)
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
		SetRepoConfigID(scope.RepoConfigID).
		SetUserID(scope.UserID).
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

	if checkpointID, ok, err := s.resolveCheckpointBinding(ctx, scope.RepoConfigID, workspaceID, req.ObservedEndAt.UTC()); err != nil {
		return fmt.Errorf("resolve checkpoint binding: %w", err)
	} else if ok {
		create.SetCommitCheckpointID(checkpointID)
	}

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

type scopeResolution struct {
	RepoConfigID int
	UserID       int
}

func (s *Service) resolveScopeByWorkspace(ctx context.Context, workspaceID string) (*scopeResolution, error) {
	latestUsage, err := s.entClient.ToolUsageEvent.Query().
		Where(toolusageevent.WorkspaceIDEQ(workspaceID)).
		Order(ent.Desc(toolusageevent.FieldObservedEndAt)).
		First(ctx)
	if err == nil {
		return &scopeResolution{
			RepoConfigID: latestUsage.RepoConfigID,
			UserID:       latestUsage.UserID,
		}, nil
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("resolve workspace scope from tool usage events: %w", err)
	}

	checkpoint, err := s.entClient.CommitCheckpoint.Query().
		Where(commitcheckpoint.WorkspaceIDEQ(workspaceID)).
		Order(ent.Desc(commitcheckpoint.FieldCapturedAt)).
		First(ctx)
	if err == nil {
		if checkpoint.UserID == nil || *checkpoint.UserID <= 0 {
			return nil, fmt.Errorf("create tool usage event: workspace_id %q has no known user scope", workspaceID)
		}
		return &scopeResolution{
			RepoConfigID: checkpoint.RepoConfigID,
			UserID:       *checkpoint.UserID,
		}, nil
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("resolve workspace scope from checkpoints: %w", err)
	}

	return nil, fmt.Errorf("create tool usage event: workspace_id %q has no known repo/user scope", workspaceID)
}

func (s *Service) resolveCheckpointBinding(ctx context.Context, repoConfigID int, workspaceID string, observedEndAt time.Time) (int, bool, error) {
	if observedEndAt.IsZero() {
		return 0, false, nil
	}

	checkpoint, err := s.entClient.CommitCheckpoint.Query().
		Where(
			commitcheckpoint.WorkspaceIDEQ(workspaceID),
			commitcheckpoint.RepoConfigIDEQ(repoConfigID),
			commitcheckpoint.CapturedAtGTE(observedEndAt),
		).
		Order(ent.Asc(commitcheckpoint.FieldCapturedAt)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("query candidate checkpoint: %w", err)
	}

	return checkpoint.ID, true, nil
}

func (s *Service) BindUsageEventsToCheckpoint(ctx context.Context, req BindUsageEventsRequest) (int, error) {
	if s.entClient == nil {
		return 0, fmt.Errorf("bind tool usage events: ent client is required")
	}

	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return 0, fmt.Errorf("bind tool usage events: workspace_id is required")
	}

	checkpoint, err := s.entClient.CommitCheckpoint.Get(ctx, req.CommitCheckpointID)
	if err != nil {
		return 0, fmt.Errorf("load checkpoint: %w", err)
	}

	items, err := s.entClient.ToolUsageEvent.Query().
		Where(
			toolusageevent.WorkspaceIDEQ(workspaceID),
			toolusageevent.RepoConfigIDEQ(checkpoint.RepoConfigID),
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
