package handler

import (
	"net/http"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/checkpoint"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

type CheckpointHandler struct {
	service *checkpoint.Service
}

func NewCheckpointHandler(service *checkpoint.Service) *CheckpointHandler {
	return &CheckpointHandler{service: service}
}

func (h *CheckpointHandler) Commit(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req checkpoint.CommitCheckpointRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.RecordCheckpointForUser(c.Request.Context(), uc.UserID, req); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{"event_id": req.EventID})
}

func (h *CheckpointHandler) Rewrite(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req checkpoint.CommitRewriteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.service.RecordRewriteForUser(c.Request.Context(), uc.UserID, req); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, gin.H{"event_id": req.EventID})
}

type compactCommitCheckpointRequest struct {
	EventID         string     `json:"event_id"`
	RepoConfigID    int        `json:"repo_config_id"`
	RepoFullName    string     `json:"repo_full_name,omitempty"`
	WorkspaceID     string     `json:"workspace_id"`
	CommitSHA       string     `json:"commit_sha"`
	ParentSHAs      []string   `json:"parent_shas,omitempty"`
	BranchSnapshot  string     `json:"branch_snapshot,omitempty"`
	HeadSnapshot    string     `json:"head_snapshot,omitempty"`
	LineageKind     string     `json:"lineage_kind,omitempty"`
	SourceCommitSHA string     `json:"source_commit_sha,omitempty"`
	CommitPatchID   string     `json:"commit_patch_id,omitempty"`
	SourcePatchID   string     `json:"source_patch_id,omitempty"`
	BindingSource   string     `json:"binding_source"`
	CapturedAt      *time.Time `json:"captured_at,omitempty"`
}

type compactCommitRewriteRequest struct {
	EventID       string     `json:"event_id"`
	RepoConfigID  int        `json:"repo_config_id"`
	RepoFullName  string     `json:"repo_full_name,omitempty"`
	WorkspaceID   string     `json:"workspace_id"`
	RewriteType   string     `json:"rewrite_type"`
	OldCommitSHA  string     `json:"old_commit_sha"`
	NewCommitSHA  string     `json:"new_commit_sha"`
	BindingSource string     `json:"binding_source"`
	CapturedAt    *time.Time `json:"captured_at,omitempty"`
}

// CompactCommit accepts only the minimized reporter checkpoint contract. It
// deliberately excludes session and agent_snapshot payloads supported by the
// legacy user-authenticated endpoint.
func (h *CheckpointHandler) CompactCommit(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req compactCommitCheckpointRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	request := checkpoint.CommitCheckpointRequest{
		EventID: req.EventID, RepoConfigID: req.RepoConfigID, RepoFullName: req.RepoFullName,
		WorkspaceID: req.WorkspaceID, CommitSHA: req.CommitSHA, ParentSHAs: req.ParentSHAs,
		BranchSnapshot: req.BranchSnapshot, HeadSnapshot: req.HeadSnapshot,
		LineageKind: req.LineageKind, SourceCommitSHA: req.SourceCommitSHA,
		CommitPatchID: req.CommitPatchID, SourcePatchID: req.SourcePatchID,
		BindingSource: req.BindingSource, CapturedAt: req.CapturedAt,
	}
	if err := h.service.RecordCheckpointForUser(c.Request.Context(), uc.UserID, request); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Created(c, gin.H{"event_id": req.EventID})
}

func (h *CheckpointHandler) CompactRewrite(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req compactCommitRewriteRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	request := checkpoint.CommitRewriteRequest{
		EventID: req.EventID, RepoConfigID: req.RepoConfigID, RepoFullName: req.RepoFullName,
		WorkspaceID: req.WorkspaceID, RewriteType: req.RewriteType,
		OldCommitSHA: req.OldCommitSHA, NewCommitSHA: req.NewCommitSHA,
		BindingSource: req.BindingSource, CapturedAt: req.CapturedAt,
	}
	if err := h.service.RecordRewriteForUser(c.Request.Context(), uc.UserID, request); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Created(c, gin.H{"event_id": req.EventID})
}
