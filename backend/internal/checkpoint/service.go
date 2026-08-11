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
	RepoConfigID   int            `json:"repo_config_id,omitempty"`
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
	RepoConfigID  int        `json:"repo_config_id,omitempty"`
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
	entClient   *ent.Client
	repoService *reposvc.Service
}

type ServiceOptions struct {
	InventoryRevisionStore reposvc.InventoryRevisionInvalidator
	RepoService            *reposvc.Service
}

func NewService(entClient *ent.Client, options ...ServiceOptions) *Service {
	opt := ServiceOptions{}
	if len(options) > 0 {
		opt = options[0]
	}
	repoService := opt.RepoService
	if repoService == nil {
		repoService = reposvc.NewService(entClient, "", nil, reposvc.ServiceOptions{
			InventoryRevisionStore: opt.InventoryRevisionStore,
		})
	}
	return &Service{entClient: entClient, repoService: repoService}
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

	pendingAutoBind := make([]int, 0, 1)
	txSvc := &Service{
		entClient: tx.Client(),
		repoService: s.repoService.WithTransaction(tx, func(repoID int) {
			pendingAutoBind = append(pendingAutoBind, repoID)
		}),
	}

	existing, err := txSvc.entClient.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ(eventID)).Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			return fmt.Errorf("record checkpoint: query event_id: %w", err)
		}
	}
	if existing != nil {
		replayRepo, replayErr := txSvc.resolveReplayRepoConfig(ctx, req.RepoConfigID, req.RepoFullName, req.CloneURL)
		if replayErr == nil && checkpointReplayMatches(existing, replayRepo.ID, userID, req) {
			return nil
		}
		return fmt.Errorf("record checkpoint: event_id conflict")
	}

	rc, err := txSvc.resolveRepoConfigForIngest(ctx, req.RepoConfigID, req.RepoFullName, req.CloneURL, req.BranchSnapshot)
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
			existing, qerr := s.entClient.CommitCheckpoint.Query().Where(commitcheckpoint.EventIDEQ(eventID)).Only(ctx)
			if qerr == nil {
				replayRepo, replayErr := s.resolveReplayRepoConfig(ctx, req.RepoConfigID, req.RepoFullName, req.CloneURL)
				if replayErr == nil && checkpointReplayMatches(existing, replayRepo.ID, userID, req) {
					return nil
				}
				return fmt.Errorf("record checkpoint: event_id conflict")
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
	for _, repoID := range pendingAutoBind {
		_, _ = s.repoService.AutoBindRepo(ctx, repoID)
	}
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

	existing, err := s.entClient.CommitRewrite.Query().Where(commitrewrite.EventIDEQ(eventID)).Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			return fmt.Errorf("record rewrite: query event_id: %w", err)
		}
	}
	if existing != nil {
		replayRepo, replayErr := s.resolveReplayRepoConfig(ctx, req.RepoConfigID, req.RepoFullName, req.CloneURL)
		if replayErr == nil && rewriteReplayMatches(existing, replayRepo.ID, userID, req) {
			return nil
		}
		return fmt.Errorf("record rewrite: event_id conflict")
	}

	rc, err := s.resolveRepoConfigForIngest(ctx, req.RepoConfigID, req.RepoFullName, req.CloneURL, "")
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
			existing, qerr := s.entClient.CommitRewrite.Query().Where(commitrewrite.EventIDEQ(eventID)).Only(ctx)
			if qerr == nil {
				replayRepo, replayErr := s.resolveReplayRepoConfig(ctx, req.RepoConfigID, req.RepoFullName, req.CloneURL)
				if replayErr == nil && rewriteReplayMatches(existing, replayRepo.ID, userID, req) {
					return nil
				}
				return fmt.Errorf("record rewrite: event_id conflict")
			}
		}
		return fmt.Errorf("record rewrite: create rewrite: %w", err)
	}

	return nil
}

func checkpointReplayMatches(existing *ent.CommitCheckpoint, repoConfigID, userID int, req CommitCheckpointRequest) bool {
	if existing == nil || existing.RepoConfigID != repoConfigID || existing.WorkspaceID != strings.TrimSpace(req.WorkspaceID) || existing.CommitSha != strings.TrimSpace(req.CommitSHA) || existing.BindingSource.String() != strings.TrimSpace(req.BindingSource) {
		return false
	}
	if (userID == 0) != (existing.UserID == nil) || userID > 0 && *existing.UserID != userID {
		return false
	}
	if !equalStrings(existing.ParentShas, req.ParentSHAs) || optionalString(existing.BranchSnapshot) != strings.TrimSpace(req.BranchSnapshot) || optionalString(existing.HeadSnapshot) != strings.TrimSpace(req.HeadSnapshot) {
		return false
	}
	return true
}

func rewriteReplayMatches(existing *ent.CommitRewrite, repoConfigID, userID int, req CommitRewriteRequest) bool {
	if existing == nil || existing.RepoConfigID != repoConfigID || existing.WorkspaceID != strings.TrimSpace(req.WorkspaceID) || existing.RewriteType.String() != strings.TrimSpace(req.RewriteType) || existing.OldCommitSha != strings.TrimSpace(req.OldCommitSHA) || existing.NewCommitSha != strings.TrimSpace(req.NewCommitSHA) || existing.BindingSource.String() != strings.TrimSpace(req.BindingSource) {
		return false
	}
	if (userID == 0) != (existing.UserID == nil) || userID > 0 && *existing.UserID != userID {
		return false
	}
	return true
}

func (s *Service) resolveReplayRepoConfig(ctx context.Context, repoConfigID int, repoFullName, cloneURL string) (*ent.RepoConfig, error) {
	if repoConfigID > 0 {
		return s.entClient.RepoConfig.Get(ctx, repoConfigID)
	}
	return s.resolveRepoConfig(ctx, repoFullName, cloneURL)
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func optionalString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
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

	return s.repoService.EnsureFromRemote(ctx, remoteURL, branch)
}

func (s *Service) resolveRepoConfigForIngest(ctx context.Context, repoConfigID int, repoFullName, cloneURL, branch string) (*ent.RepoConfig, error) {
	if repoConfigID > 0 {
		rc, err := s.entClient.RepoConfig.Query().
			Where(repoconfig.IDEQ(repoConfigID)).
			Only(ctx)
		if err != nil {
			return nil, fmt.Errorf("repo_config_id %d not found: %w", repoConfigID, err)
		}
		switch rc.Status {
		case repoconfig.StatusActive, repoconfig.StatusWebhookFailed:
			return rc, nil
		default:
			return nil, fmt.Errorf("repo_config_id %d is not reporting-enabled", repoConfigID)
		}
	}
	return s.resolveOrEnsureRepoConfig(ctx, repoFullName, cloneURL, branch)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
