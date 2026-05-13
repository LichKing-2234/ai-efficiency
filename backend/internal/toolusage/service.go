package toolusage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/session"
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
	dedupeKey := strings.TrimSpace(req.DedupeKey)
	if dedupeKey == "" {
		return fmt.Errorf("create tool usage event: dedupe_key is required")
	}

	scope, err := s.resolveScopeByWorkspace(ctx, workspaceID)
	if err != nil {
		return err
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
		userID := firstUserIDForRepo(ctx, s.entClient, checkpoint.RepoConfigID)
		if userID <= 0 {
			return nil, fmt.Errorf("create tool usage event: workspace_id %q has no known user scope", workspaceID)
		}
		return &scopeResolution{
			RepoConfigID: checkpoint.RepoConfigID,
			UserID:       userID,
		}, nil
	}
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("resolve workspace scope from checkpoints: %w", err)
	}

	return nil, fmt.Errorf("create tool usage event: workspace_id %q has no known repo/user scope", workspaceID)
}

func firstUserIDForRepo(ctx context.Context, entClient *ent.Client, repoConfigID int) int {
	sess, err := entClient.Session.Query().
		Where(session.HasRepoConfigWith(repoconfig.IDEQ(repoConfigID))).
		WithUser().
		Order(ent.Desc(session.FieldStartedAt)).
		First(ctx)
	if err == nil && sess.Edges.User != nil {
		return sess.Edges.User.ID
	}
	return 0
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
