package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/agentmetadataevent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/google/uuid"
)

var errRepoNotFound = errors.New("repo not found")

type CommitCheckpointRequest struct {
	EventID        string         `json:"event_id" binding:"required"`
	SessionID      string         `json:"session_id"`
	RepoFullName   string         `json:"repo_full_name"`
	CloneURL       string         `json:"clone_url"`
	WorkspaceID    string         `json:"workspace_id" binding:"required"`
	CommitSHA      string         `json:"commit_sha" binding:"required"`
	ParentSHAs     []string       `json:"parent_shas"`
	BranchSnapshot string         `json:"branch_snapshot"`
	HeadSnapshot   string         `json:"head_snapshot"`
	BindingSource  string         `json:"binding_source" binding:"required"`
	AgentSnapshot  map[string]any `json:"agent_snapshot"`
	CapturedAt     *time.Time     `json:"captured_at"`
}

type CommitRewriteRequest struct {
	EventID       string     `json:"event_id" binding:"required"`
	SessionID     string     `json:"session_id"`
	RepoFullName  string     `json:"repo_full_name"`
	CloneURL      string     `json:"clone_url"`
	WorkspaceID   string     `json:"workspace_id" binding:"required"`
	RewriteType   string     `json:"rewrite_type" binding:"required"`
	OldCommitSHA  string     `json:"old_commit_sha" binding:"required"`
	NewCommitSHA  string     `json:"new_commit_sha" binding:"required"`
	BindingSource string     `json:"binding_source" binding:"required"`
	CapturedAt    *time.Time `json:"captured_at"`
}

type Service struct {
	entClient *ent.Client
}

func NewService(entClient *ent.Client) *Service {
	return &Service{entClient: entClient}
}

func (s *Service) RecordCheckpoint(ctx context.Context, req CommitCheckpointRequest) error {
	if s.entClient == nil {
		return fmt.Errorf("record checkpoint: ent client is required")
	}

	eventID := strings.TrimSpace(req.EventID)
	if eventID == "" {
		return fmt.Errorf("record checkpoint: event_id is required")
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return fmt.Errorf("record checkpoint: workspace_id is required")
	}
	commitSHA := strings.TrimSpace(req.CommitSHA)
	if commitSHA == "" {
		return fmt.Errorf("record checkpoint: commit_sha is required")
	}
	bindingSource := strings.TrimSpace(req.BindingSource)
	if bindingSource == "" {
		return fmt.Errorf("record checkpoint: binding_source is required")
	}

	sessionID, hasSession, err := parseSessionID(req.SessionID)
	if err != nil {
		return fmt.Errorf("record checkpoint: %w", err)
	}

	tx, err := s.entClient.Tx(ctx)
	if err != nil {
		return fmt.Errorf("record checkpoint: start tx: %w", err)
	}
	txDone := false
	defer func() {
		if !txDone {
			_ = tx.Rollback()
		}
	}()

	txSvc := &Service{entClient: tx.Client()}

	exists, err := txSvc.entClient.CommitCheckpoint.Query().
		Where(commitcheckpoint.EventIDEQ(eventID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("record checkpoint: query event_id: %w", err)
	}
	if exists {
		return nil
	}

	rc, err := txSvc.resolveRepoConfig(ctx, req.RepoFullName, req.CloneURL)
	if err != nil {
		return fmt.Errorf("record checkpoint: %w", err)
	}
	if hasSession {
		if err := txSvc.validateSessionRepo(ctx, sessionID, rc.ID); err != nil {
			return fmt.Errorf("record checkpoint: %w", err)
		}
	}

	create := txSvc.entClient.CommitCheckpoint.Create().
		SetEventID(eventID).
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(rc.ID).
		SetCommitSha(commitSHA).
		SetParentShas(req.ParentSHAs).
		SetBindingSource(commitcheckpoint.BindingSource(bindingSource))

	if hasSession {
		create.SetSessionID(sessionID)
	}
	if v := strings.TrimSpace(req.BranchSnapshot); v != "" {
		create.SetBranchSnapshot(v)
	}
	if v := strings.TrimSpace(req.HeadSnapshot); v != "" {
		create.SetHeadSnapshot(v)
	}
	if len(req.AgentSnapshot) > 0 {
		create.SetAgentSnapshot(req.AgentSnapshot)
	}
	if req.CapturedAt != nil && !req.CapturedAt.IsZero() {
		create.SetCapturedAt(req.CapturedAt.UTC())
	}

	if _, err := create.Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			_ = tx.Rollback()
			txDone = true
			exists, qerr := s.entClient.CommitCheckpoint.Query().
				Where(commitcheckpoint.EventIDEQ(eventID)).
				Exist(ctx)
			if qerr == nil && exists {
				return nil
			}
		}
		return fmt.Errorf("record checkpoint: create checkpoint: %w", err)
	}

	if len(req.AgentSnapshot) > 0 && hasSession {
		if err := txSvc.createAgentMetadataEvent(ctx, sessionID, workspaceID, req.AgentSnapshot); err != nil {
			return fmt.Errorf("record checkpoint: create agent metadata event: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record checkpoint: commit tx: %w", err)
	}
	txDone = true
	return nil
}

func (s *Service) RecordRewrite(ctx context.Context, req CommitRewriteRequest) error {
	if s.entClient == nil {
		return fmt.Errorf("record rewrite: ent client is required")
	}

	eventID := strings.TrimSpace(req.EventID)
	if eventID == "" {
		return fmt.Errorf("record rewrite: event_id is required")
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID == "" {
		return fmt.Errorf("record rewrite: workspace_id is required")
	}
	oldCommitSHA := strings.TrimSpace(req.OldCommitSHA)
	if oldCommitSHA == "" {
		return fmt.Errorf("record rewrite: old_commit_sha is required")
	}
	newCommitSHA := strings.TrimSpace(req.NewCommitSHA)
	if newCommitSHA == "" {
		return fmt.Errorf("record rewrite: new_commit_sha is required")
	}
	rewriteType := strings.TrimSpace(req.RewriteType)
	if rewriteType == "" {
		return fmt.Errorf("record rewrite: rewrite_type is required")
	}
	bindingSource := strings.TrimSpace(req.BindingSource)
	if bindingSource == "" {
		return fmt.Errorf("record rewrite: binding_source is required")
	}

	exists, err := s.entClient.CommitRewrite.Query().
		Where(commitrewrite.EventIDEQ(eventID)).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("record rewrite: query event_id: %w", err)
	}
	if exists {
		return nil
	}

	rc, err := s.resolveRepoConfig(ctx, req.RepoFullName, req.CloneURL)
	if err != nil {
		return fmt.Errorf("record rewrite: %w", err)
	}

	sessionID, hasSession, err := parseSessionID(req.SessionID)
	if err != nil {
		return fmt.Errorf("record rewrite: %w", err)
	}
	if hasSession {
		if err := s.validateSessionRepo(ctx, sessionID, rc.ID); err != nil {
			return fmt.Errorf("record rewrite: %w", err)
		}
	}

	create := s.entClient.CommitRewrite.Create().
		SetEventID(eventID).
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(rc.ID).
		SetRewriteType(commitrewrite.RewriteType(rewriteType)).
		SetOldCommitSha(oldCommitSHA).
		SetNewCommitSha(newCommitSHA).
		SetBindingSource(commitrewrite.BindingSource(bindingSource))

	if hasSession {
		create.SetSessionID(sessionID)
	}
	if req.CapturedAt != nil && !req.CapturedAt.IsZero() {
		create.SetCapturedAt(req.CapturedAt.UTC())
	}

	if _, err := create.Save(ctx); err != nil {
		if ent.IsConstraintError(err) {
			exists, qerr := s.entClient.CommitRewrite.Query().
				Where(commitrewrite.EventIDEQ(eventID)).
				Exist(ctx)
			if qerr == nil && exists {
				return nil
			}
		}
		return fmt.Errorf("record rewrite: create rewrite: %w", err)
	}

	return nil
}

func (s *Service) validateSessionRepo(ctx context.Context, sessionID uuid.UUID, repoConfigID int) error {
	ok, err := s.entClient.Session.Query().
		Where(
			session.IDEQ(sessionID),
			session.HasRepoConfigWith(repoconfig.IDEQ(repoConfigID)),
		).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("load session: %w", err)
	}
	if !ok {
		return fmt.Errorf("session %s does not belong to repo %d", sessionID, repoConfigID)
	}
	return nil
}

func (s *Service) resolveRepoConfig(ctx context.Context, repoFullName, cloneURL string) (*ent.RepoConfig, error) {
	repoFullName = strings.TrimSpace(repoFullName)
	cloneURL = strings.TrimSpace(cloneURL)

	if repoFullName == "" && cloneURL == "" {
		return nil, fmt.Errorf("repo_full_name or clone_url is required")
	}

	tryFind := func(candidate string) (*ent.RepoConfig, error) {
		if candidate == "" {
			return nil, errRepoNotFound
		}

		rc, err := s.entClient.RepoConfig.Query().
			Where(repoconfig.FullNameEQ(candidate)).
			Only(ctx)
		if err == nil {
			return rc, nil
		}
		if !ent.IsNotFound(err) {
			return nil, fmt.Errorf("query repo by full_name: %w", err)
		}

		rc, err = s.entClient.RepoConfig.Query().
			Where(repoconfig.CloneURLEQ(candidate)).
			Only(ctx)
		if err == nil {
			return rc, nil
		}
		if !ent.IsNotFound(err) {
			return nil, fmt.Errorf("query repo by clone_url: %w", err)
		}

		return nil, errRepoNotFound
	}

	rc, err := tryFind(repoFullName)
	if err == nil {
		return rc, nil
	}
	if !errors.Is(err, errRepoNotFound) {
		return nil, err
	}

	rc, err = tryFind(cloneURL)
	if err == nil {
		return rc, nil
	}
	if !errors.Is(err, errRepoNotFound) {
		return nil, err
	}

	return nil, fmt.Errorf("repo not found: %s", firstNonEmpty(repoFullName, cloneURL))
}

func (s *Service) createAgentMetadataEvent(ctx context.Context, sessionID uuid.UUID, workspaceID string, snapshot map[string]any) error {
	source := normalizeSource(asString(snapshot["source"]))
	usageUnit := normalizeUsageUnit(asString(snapshot["usage_unit"]))

	rawPayload, _ := snapshot["raw_payload"].(map[string]any)
	if rawPayload == nil {
		rawPayload = snapshot
	}

	create := s.entClient.AgentMetadataEvent.Create().
		SetSessionID(sessionID).
		SetSource(agentmetadataevent.Source(source)).
		SetUsageUnit(agentmetadataevent.UsageUnit(usageUnit)).
		SetRawPayload(rawPayload)

	if workspaceID != "" {
		create.SetWorkspaceID(workspaceID)
	}
	if sourceSessionID := strings.TrimSpace(asString(snapshot["source_session_id"])); sourceSessionID != "" {
		create.SetSourceSessionID(sourceSessionID)
	}
	if v, ok := asInt64(snapshot["input_tokens"]); ok {
		create.SetInputTokens(v)
	}
	if v, ok := asInt64(snapshot["output_tokens"]); ok {
		create.SetOutputTokens(v)
	}
	if v, ok := asInt64(snapshot["cached_input_tokens"]); ok {
		create.SetCachedInputTokens(v)
	}
	if v, ok := asInt64(snapshot["reasoning_tokens"]); ok {
		create.SetReasoningTokens(v)
	}
	if v, ok := asFloat64(snapshot["credit_usage"]); ok {
		create.SetCreditUsage(v)
	}
	if v, ok := asFloat64(snapshot["context_usage_pct"]); ok {
		create.SetContextUsagePct(v)
	}
	if t, ok := asTime(snapshot["observed_at"]); ok && !t.IsZero() {
		create.SetObservedAt(t.UTC())
	}

	if _, err := create.Save(ctx); err != nil {
		return fmt.Errorf("save metadata event: %w", err)
	}
	return nil
}

func parseSessionID(raw string) (uuid.UUID, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return uuid.Nil, false, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false, fmt.Errorf("invalid session_id: %w", err)
	}
	return id, true, nil
}

func normalizeSource(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case string(agentmetadataevent.SourceCodex):
		return string(agentmetadataevent.SourceCodex)
	case string(agentmetadataevent.SourceClaude):
		return string(agentmetadataevent.SourceClaude)
	case string(agentmetadataevent.SourceKiro):
		return string(agentmetadataevent.SourceKiro)
	default:
		return string(agentmetadataevent.SourceCodex)
	}
}

func normalizeUsageUnit(v string) string {
	switch strings.TrimSpace(strings.ToLower(v)) {
	case string(agentmetadataevent.UsageUnitToken):
		return string(agentmetadataevent.UsageUnitToken)
	case string(agentmetadataevent.UsageUnitCredit):
		return string(agentmetadataevent.UsageUnitCredit)
	default:
		return string(agentmetadataevent.UsageUnitUnknown)
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asInt64(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int8:
		return int64(n), true
	case int16:
		return int64(n), true
	case int32:
		return int64(n), true
	case int64:
		return n, true
	case float32:
		return int64(n), true
	case float64:
		return int64(n), true
	case string:
		if n == "" {
			return 0, false
		}
		parsed, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func asFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		if n == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(n, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func asTime(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case string:
		parsed, err := time.Parse(time.RFC3339, t)
		if err != nil {
			return time.Time{}, false
		}
		return parsed, true
	default:
		return time.Time{}, false
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
