package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/commitcheckpoint"
	"github.com/ai-efficiency/backend/ent/repoconfig"
	"github.com/ai-efficiency/backend/ent/session"
	"github.com/ai-efficiency/backend/ent/sessionevent"
	"github.com/ai-efficiency/backend/ent/sessionusageevent"
	"github.com/ai-efficiency/backend/ent/sessionworkspace"
	"github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/sessionbootstrap"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SessionHandler handles session API requests from ae-cli.
type SessionHandler struct {
	entClient    *ent.Client
	bootstrapSvc *sessionbootstrap.Service
}

// NewSessionHandler creates a new session handler.
func NewSessionHandler(entClient *ent.Client, bootstrapSvc *sessionbootstrap.Service) *SessionHandler {
	return &SessionHandler{entClient: entClient, bootstrapSvc: bootstrapSvc}
}

type createSessionRequest struct {
	ID           string                   `json:"id" binding:"required"`
	RepoFullName string                   `json:"repo_full_name" binding:"required"`
	Branch       string                   `json:"branch" binding:"required"`
	ToolConfigs  []map[string]interface{} `json:"tool_configs"`
}

type bootstrapSessionRequest struct {
	RepoFullName   string `json:"repo_full_name" binding:"required"`
	BranchSnapshot string `json:"branch_snapshot" binding:"required"`
	HeadSHA        string `json:"head_sha" binding:"required"`
	WorkspaceRoot  string `json:"workspace_root" binding:"required"`
	GitDir         string `json:"git_dir" binding:"required"`
	GitCommonDir   string `json:"git_common_dir" binding:"required"`
	WorkspaceID    string `json:"workspace_id" binding:"required"`
}

type addInvocationRequest struct {
	Tool  string `json:"tool" binding:"required"`
	Start string `json:"start" binding:"required"`
	End   string `json:"end"`
}

type providerCredentialQuery struct {
	Platform string `form:"platform" binding:"required"`
}

func isAdminUser(c *gin.Context) bool {
	uc := auth.GetUserContext(c)
	return uc != nil && uc.Role == "admin"
}

func requestedOwnerScope(c *gin.Context) string {
	scope := strings.TrimSpace(c.DefaultQuery("owner_scope", ""))
	if scope == "" {
		if isAdminUser(c) {
			return "all"
		}
		return "mine"
	}
	if !isAdminUser(c) {
		return "mine"
	}
	switch scope {
	case "all", "mine", "unowned":
		return scope
	default:
		return "all"
	}
}

// Bootstrap handles POST /api/v1/sessions/bootstrap
func (h *SessionHandler) Bootstrap(c *gin.Context) {
	if h.bootstrapSvc == nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "bootstrap service not configured")
		return
	}

	var req bootstrapSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	userID := uc.UserID

	resp, err := h.bootstrapSvc.Bootstrap(c.Request.Context(), userID, sessionbootstrap.BootstrapRequest{
		RepoFullName:   req.RepoFullName,
		BranchSnapshot: req.BranchSnapshot,
		HeadSHA:        req.HeadSHA,
		WorkspaceRoot:  req.WorkspaceRoot,
		GitDir:         req.GitDir,
		GitCommonDir:   req.GitCommonDir,
		WorkspaceID:    req.WorkspaceID,
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Created(c, resp)
}

// ProviderCredential handles GET /api/v1/sessions/:id/provider-credentials
func (h *SessionHandler) ProviderCredential(c *gin.Context) {
	if h.bootstrapSvc == nil {
		pkg.Error(c, http.StatusServiceUnavailable, "bootstrap service not configured")
		return
	}
	sessionID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}
	var query providerCredentialQuery
	if err := c.ShouldBindQuery(&query); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}
	resp, err := h.bootstrapSvc.ResolveProviderCredential(c.Request.Context(), uc.UserID, sessionID, strings.TrimSpace(query.Platform))
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pkg.Success(c, resp)
}

// Create handles POST /api/v1/sessions
func (h *SessionHandler) Create(c *gin.Context) {
	var req createSessionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	sessionID, err := uuid.Parse(req.ID)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id: must be UUID")
		return
	}

	// Resolve repo_config_id from full_name or clone_url
	rc, err := h.entClient.RepoConfig.Query().
		Where(repoconfig.FullNameEQ(req.RepoFullName)).
		Only(c.Request.Context())
	if err != nil && ent.IsNotFound(err) {
		// Fallback: try matching by clone_url
		rc, err = h.entClient.RepoConfig.Query().
			Where(repoconfig.CloneURLEQ(req.RepoFullName)).
			Only(c.Request.Context())
	}
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "repo not found: "+req.RepoFullName)
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to find repo")
		return
	}

	create := h.entClient.Session.Create().
		SetID(sessionID).
		SetRepoConfigID(rc.ID).
		SetBranch(req.Branch).
		SetStartedAt(time.Now())

	// Set user edge from JWT
	if userID, exists := c.Get("user_id"); exists {
		create.SetUserID(userID.(int))
	}

	// Set tool_configs if provided
	if len(req.ToolConfigs) > 0 {
		create.SetToolConfigs(req.ToolConfigs)
		if tc := req.ToolConfigs[0]; tc != nil {
			if pn, ok := tc["provider_name"].(string); ok {
				create.SetProviderName(pn)
			}
			if keyID, ok := tc["relay_api_key_id"].(float64); ok {
				create.SetRelayAPIKeyID(int(keyID))
			}
		}
	}

	s, err := create.Save(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to create session")
		return
	}

	pkg.Created(c, s)
}

// Update handles PUT /api/v1/sessions/:id (heartbeat)
func (h *SessionHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}

	var s *ent.Session
	if h.bootstrapSvc != nil {
		s, err = h.bootstrapSvc.Heartbeat(c.Request.Context(), id)
	} else {
		// Compatibility fallback
		s, err = h.entClient.Session.UpdateOneID(id).
			Save(c.Request.Context())
	}
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "session not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to update session")
		return
	}

	pkg.Success(c, s)
}

// Stop handles POST /api/v1/sessions/:id/stop
func (h *SessionHandler) Stop(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}

	var s *ent.Session
	if h.bootstrapSvc != nil {
		s, err = h.bootstrapSvc.Stop(c.Request.Context(), id)
	} else {
		// Compatibility fallback
		now := time.Now()
		s, err = h.entClient.Session.UpdateOneID(id).
			SetEndedAt(now).
			SetStatus(session.StatusCompleted).
			Save(c.Request.Context())
	}
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "session not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to stop session")
		return
	}

	pkg.Success(c, s)
}

// List handles GET /api/v1/sessions
func (h *SessionHandler) List(c *gin.Context) {
	query := h.entClient.Session.Query().
		WithRepoConfig().
		WithUser().
		Order(ent.Desc(session.FieldStartedAt))
	if uc := auth.GetUserContext(c); uc != nil {
		switch requestedOwnerScope(c) {
		case "mine":
			query = query.Where(session.HasUserWith(user.IDEQ(uc.UserID)))
		case "unowned":
			query = query.Where(session.Not(session.HasUser()))
		case "all":
			// Admin users can view all sessions.
		}
	}

	// Filter by status
	if status := c.Query("status"); status != "" {
		query = query.Where(session.StatusEQ(session.Status(status)))
	}

	// Filter by repo_config_id
	if repoID := c.Query("repo_id"); repoID != "" {
		id, err := strconv.Atoi(repoID)
		if err == nil {
			query = query.Where(session.HasRepoConfigWith(repoconfig.IDEQ(id)))
		}
	}

	if branch := strings.TrimSpace(c.Query("branch")); branch != "" {
		query = query.Where(session.BranchContainsFold(branch))
	}

	if repoQuery := strings.TrimSpace(c.Query("repo_query")); repoQuery != "" {
		query = query.Where(session.HasRepoConfigWith(
			repoconfig.Or(
				repoconfig.FullNameContainsFold(repoQuery),
				repoconfig.CloneURLContainsFold(repoQuery),
			),
		))
	}

	if ownerQuery := strings.TrimSpace(c.Query("owner_query")); ownerQuery != "" {
		query = query.Where(session.HasUserWith(
			user.Or(
				user.UsernameContainsFold(ownerQuery),
				user.EmailContainsFold(ownerQuery),
			),
		))
	}

	// Pagination
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	total, err := query.Clone().Count(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to count sessions")
		return
	}

	sessions, err := query.
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list sessions")
		return
	}

	pkg.Success(c, gin.H{
		"items":     sessions,
		"total":     total,
		"page":      page,
		"page_size": pageSize,
	})
}

// Get handles GET /api/v1/sessions/:id
func (h *SessionHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}

	query := h.entClient.Session.Query().
		Where(session.IDEQ(id))
	if uc := auth.GetUserContext(c); uc != nil && uc.Role != "admin" {
		query = query.Where(session.HasUserWith(user.IDEQ(uc.UserID)))
	}

	s, err := query.
		WithRepoConfig().
		WithUser().
		WithSessionWorkspaces(func(q *ent.SessionWorkspaceQuery) {
			q.Order(ent.Desc(sessionworkspace.FieldLastSeenAt)).
				Limit(20)
		}).
		WithCommitCheckpoints(func(q *ent.CommitCheckpointQuery) {
			q.Order(ent.Desc(commitcheckpoint.FieldCapturedAt)).
				Limit(50)
		}).
		WithSessionUsageEvents(func(q *ent.SessionUsageEventQuery) {
			q.Order(ent.Desc(sessionusageevent.FieldStartedAt)).
				Limit(100)
		}).
		WithSessionEvents(func(q *ent.SessionEventQuery) {
			q.Order(ent.Desc(sessionevent.FieldCapturedAt)).
				Limit(100)
		}).
		Only(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "session not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get session")
		return
	}

	pkg.Success(c, s)
}

// AddInvocation handles POST /api/v1/sessions/:id/invocations
func (h *SessionHandler) AddInvocation(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid session id")
		return
	}

	var req addInvocationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// Append invocation to tool_invocations atomically using a transaction
	tx, err := h.entClient.Tx(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to start transaction")
		return
	}

	// Lock the row by reading within transaction
	s, err := tx.Session.Get(c.Request.Context(), id)
	if err != nil {
		tx.Rollback()
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "session not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get session")
		return
	}

	invocations := s.ToolInvocations
	invocations = append(invocations, map[string]interface{}{
		"tool":  req.Tool,
		"start": req.Start,
		"end":   req.End,
	})

	s, err = tx.Session.UpdateOneID(id).
		SetToolInvocations(invocations).
		Save(c.Request.Context())
	if err != nil {
		tx.Rollback()
		pkg.Error(c, http.StatusInternalServerError, "failed to add invocation")
		return
	}

	if err := tx.Commit(); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	pkg.Success(c, s)
}
