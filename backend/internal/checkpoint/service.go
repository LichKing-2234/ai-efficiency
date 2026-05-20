package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/commitrewrite"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	reposvc "github.com/ai-efficiency/backend/internal/repo"
	"github.com/ai-efficiency/backend/internal/toolusage"
)

var errRepoNotFound = errors.New("repo not found")

type CommitCheckpointRequest struct {
	EventID        string         `json:"event_id" binding:"required"`
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
	return s.recordCheckpoint(ctx, 0, req)
}

func (s *Service) RecordCheckpointForUser(ctx context.Context, userID int, req CommitCheckpointRequest) error {
	return s.recordCheckpoint(ctx, userID, req)
}

func (s *Service) recordCheckpoint(ctx context.Context, userID int, req CommitCheckpointRequest) error {
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

	rc, err := txSvc.resolveOrEnsureRepoConfig(ctx, req.RepoFullName, req.CloneURL, req.BranchSnapshot)
	if err != nil {
		return fmt.Errorf("record checkpoint: %w", err)
	}

	create := txSvc.entClient.CommitCheckpoint.Create().
		SetEventID(eventID).
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(rc.ID).
		SetCommitSha(commitSHA).
		SetParentShas(req.ParentSHAs).
		SetBindingSource(commitcheckpoint.BindingSource(bindingSource))

	if userID > 0 {
		create.SetUserID(userID)
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

	savedCheckpoint, err := create.Save(ctx)
	if err != nil {
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

	previousCapturedAt := time.Time{}
	prevCP, err := txSvc.entClient.CommitCheckpoint.Query().
		Where(
			commitcheckpoint.RepoConfigIDEQ(rc.ID),
			commitcheckpoint.WorkspaceIDEQ(workspaceID),
			commitcheckpoint.CapturedAtLT(savedCheckpoint.CapturedAt),
		).
		Order(ent.Desc(commitcheckpoint.FieldCapturedAt)).
		First(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return fmt.Errorf("record checkpoint: load previous checkpoint: %w", err)
	}
	if prevCP != nil {
		previousCapturedAt = prevCP.CapturedAt
	}

	boundCount, err := toolusage.NewService(txSvc.entClient).BindUsageEventsToCheckpoint(ctx, toolusage.BindUsageEventsRequest{
		WorkspaceID:        workspaceID,
		CommitCheckpointID: savedCheckpoint.ID,
		CommitCapturedAt:   savedCheckpoint.CapturedAt,
		PreviousCapturedAt: previousCapturedAt,
	})
	if err != nil {
		return fmt.Errorf("record checkpoint: bind tool usage events: %w", err)
	}
	_ = boundCount

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("record checkpoint: commit tx: %w", err)
	}
	txDone = true
	return nil
}

func (s *Service) RecordRewrite(ctx context.Context, req CommitRewriteRequest) error {
	return s.recordRewrite(ctx, 0, req)
}

func (s *Service) RecordRewriteForUser(ctx context.Context, userID int, req CommitRewriteRequest) error {
	return s.recordRewrite(ctx, userID, req)
}

func (s *Service) recordRewrite(ctx context.Context, userID int, req CommitRewriteRequest) error {
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

	rc, err := s.resolveOrEnsureRepoConfig(ctx, req.RepoFullName, req.CloneURL, "")
	if err != nil {
		return fmt.Errorf("record rewrite: %w", err)
	}

	create := s.entClient.CommitRewrite.Create().
		SetEventID(eventID).
		SetWorkspaceID(workspaceID).
		SetRepoConfigID(rc.ID).
		SetRewriteType(commitrewrite.RewriteType(rewriteType)).
		SetOldCommitSha(oldCommitSHA).
		SetNewCommitSha(newCommitSHA).
		SetBindingSource(commitrewrite.BindingSource(bindingSource))

	if userID > 0 {
		create.SetUserID(userID)
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

func (s *Service) resolveOrEnsureRepoConfig(ctx context.Context, repoFullName, cloneURL, branch string) (*ent.RepoConfig, error) {
	rc, err := s.resolveRepoConfig(ctx, repoFullName, cloneURL)
	if err == nil {
		return rc, nil
	}
	if !strings.Contains(err.Error(), "repo not found") {
		return nil, err
	}

	remoteURL := firstNonEmpty(cloneURL, repoFullName)
	if remoteURL == "" {
		return nil, err
	}

	repoService := reposvc.NewService(s.entClient, "", nil)
	return repoService.EnsureFromRemote(ctx, remoteURL, branch)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
