package toolusage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/toolusageevent"
)

type CreateUsageEventRequest struct {
	RepoConfigID      int
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

type CreateUsageEventsResult struct {
	Accepted   int
	Created    int
	Duplicates int
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

	scope, err := s.resolveScope(ctx, userID, req.RepoConfigID, workspaceID)
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

func (s *Service) CreateUsageEvents(ctx context.Context, userID int, reqs []CreateUsageEventRequest) (CreateUsageEventsResult, error) {
	var result CreateUsageEventsResult
	if s.entClient == nil {
		return result, fmt.Errorf("create tool usage events: ent client is required")
	}
	if userID <= 0 {
		return result, fmt.Errorf("create tool usage events: authenticated user is required")
	}
	if len(reqs) == 0 {
		return result, fmt.Errorf("create tool usage events: events are required")
	}

	type normalizedEvent struct {
		req         CreateUsageEventRequest
		workspaceID string
		dedupeKey   string
		scope       *scopeResolution
	}

	scopeCache := make(map[string]*scopeResolution)
	seenInBatch := make(map[string]struct{})
	dedupeKeys := make([]string, 0, len(reqs))
	normalized := make([]normalizedEvent, 0, len(reqs))
	for idx, req := range reqs {
		result.Accepted++
		workspaceID := strings.TrimSpace(req.WorkspaceID)
		if workspaceID == "" {
			return result, fmt.Errorf("create tool usage events: events[%d].workspace_id is required", idx)
		}
		dedupeKey := strings.TrimSpace(req.DedupeKey)
		if dedupeKey == "" {
			return result, fmt.Errorf("create tool usage events: events[%d].dedupe_key is required", idx)
		}
		if _, ok := seenInBatch[dedupeKey]; ok {
			result.Duplicates++
			continue
		}
		seenInBatch[dedupeKey] = struct{}{}

		scopeKey := fmt.Sprintf("%d\x00%s", req.RepoConfigID, workspaceID)
		scope, ok := scopeCache[scopeKey]
		if !ok {
			resolved, err := s.resolveScope(ctx, userID, req.RepoConfigID, workspaceID)
			if err != nil {
				return result, err
			}
			scope = resolved
			scopeCache[scopeKey] = scope
		}
		if scope.UserID != userID {
			return result, fmt.Errorf("create tool usage events: %w", ErrUsageEventForbidden)
		}

		req.WorkspaceID = workspaceID
		req.DedupeKey = dedupeKey
		normalized = append(normalized, normalizedEvent{
			req:         req,
			workspaceID: workspaceID,
			dedupeKey:   dedupeKey,
			scope:       scope,
		})
		dedupeKeys = append(dedupeKeys, dedupeKey)
	}

	existing := make(map[string]struct{})
	if len(dedupeKeys) > 0 {
		rows, err := s.entClient.ToolUsageEvent.Query().
			Where(toolusageevent.DedupeKeyIn(dedupeKeys...)).
			All(ctx)
		if err != nil {
			return result, fmt.Errorf("check batch dedupe: %w", err)
		}
		for _, row := range rows {
			existing[row.DedupeKey] = struct{}{}
		}
	}

	type checkpointGroupKey struct {
		repoConfigID int
		workspaceID  string
	}
	type pendingEvent struct {
		normalizedEvent
		checkpointID  int
		hasCheckpoint bool
	}

	minObserved := make(map[checkpointGroupKey]time.Time)
	pending := make([]pendingEvent, 0, len(normalized))
	for _, item := range normalized {
		if _, ok := existing[item.dedupeKey]; ok {
			result.Duplicates++
			continue
		}
		groupKey := checkpointGroupKey{repoConfigID: item.scope.RepoConfigID, workspaceID: item.workspaceID}
		observed := item.req.ObservedEndAt.UTC()
		if !observed.IsZero() {
			current, ok := minObserved[groupKey]
			if !ok || observed.Before(current) {
				minObserved[groupKey] = observed
			}
		}
		pending = append(pending, pendingEvent{normalizedEvent: item})
	}
	if len(pending) == 0 {
		return result, nil
	}

	checkpoints := make(map[checkpointGroupKey][]*ent.CommitCheckpoint)
	for groupKey, minTime := range minObserved {
		rows, err := s.entClient.CommitCheckpoint.Query().
			Where(
				commitcheckpoint.WorkspaceIDEQ(groupKey.workspaceID),
				commitcheckpoint.RepoConfigIDEQ(groupKey.repoConfigID),
				commitcheckpoint.CapturedAtGTE(minTime),
			).
			Order(ent.Asc(commitcheckpoint.FieldCapturedAt)).
			All(ctx)
		if err != nil {
			return result, fmt.Errorf("query candidate checkpoints: %w", err)
		}
		checkpoints[groupKey] = rows
	}

	for idx := range pending {
		item := &pending[idx]
		observed := item.req.ObservedEndAt.UTC()
		if observed.IsZero() {
			continue
		}
		groupKey := checkpointGroupKey{repoConfigID: item.scope.RepoConfigID, workspaceID: item.workspaceID}
		for _, checkpoint := range checkpoints[groupKey] {
			if !checkpoint.CapturedAt.Before(observed) {
				item.checkpointID = checkpoint.ID
				item.hasCheckpoint = true
				break
			}
		}
	}

	for _, item := range pending {
		create := s.newUsageEventCreate(item.req, item.scope, item.workspaceID, item.dedupeKey, item.checkpointID, item.hasCheckpoint)
		if err := create.Exec(ctx); err != nil {
			if ent.IsConstraintError(err) {
				exists, qerr := s.entClient.ToolUsageEvent.Query().
					Where(toolusageevent.DedupeKeyEQ(item.dedupeKey)).
					Exist(ctx)
				if qerr == nil && exists {
					result.Duplicates++
					continue
				}
			}
			return result, fmt.Errorf("create tool usage event %q: %w", item.dedupeKey, err)
		}
		result.Created++
	}

	return result, nil
}

func (s *Service) newUsageEventCreate(req CreateUsageEventRequest, scope *scopeResolution, workspaceID, dedupeKey string, checkpointID int, hasCheckpoint bool) *ent.ToolUsageEventCreate {
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

	if hasCheckpoint {
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
	return create
}

type scopeResolution struct {
	RepoConfigID int
	UserID       int
}

func (s *Service) resolveScope(ctx context.Context, userID, repoConfigID int, workspaceID string) (*scopeResolution, error) {
	if repoConfigID > 0 {
		exists, err := s.entClient.RepoConfig.Query().
			Where(repoconfig.IDEQ(repoConfigID)).
			Exist(ctx)
		if err != nil {
			return nil, fmt.Errorf("create tool usage event: query repo_config_id: %w", err)
		}
		if !exists {
			return nil, fmt.Errorf("create tool usage event: repo_config_id %d not found", repoConfigID)
		}
		return &scopeResolution{RepoConfigID: repoConfigID, UserID: userID}, nil
	}
	return s.resolveScopeByWorkspace(ctx, workspaceID)
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
