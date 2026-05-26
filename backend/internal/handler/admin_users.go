package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/predicate"
	entuser "github.com/ai-efficiency/backend/ent/user"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

type AdminUsersHandler struct {
	entClient     *ent.Client
	encryptionKey string
}

type adminUserRow struct {
	ID                int       `json:"id"`
	Username          string    `json:"username"`
	Email             string    `json:"email"`
	Role              string    `json:"role"`
	AuthSource        string    `json:"auth_source"`
	RelayUserID       *int      `json:"relay_user_id,omitempty"`
	RelayAuthPassword string    `json:"relay_auth_password"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type adminUsersListRequest struct {
	Q        string
	Page     int
	PageSize int
}

func NewAdminUsersHandler(entClient *ent.Client, encryptionKey string) *AdminUsersHandler {
	return &AdminUsersHandler{
		entClient:     entClient,
		encryptionKey: strings.TrimSpace(encryptionKey),
	}
}

func (h *AdminUsersHandler) List(c *gin.Context) {
	req := parseAdminUsersListRequest(c)
	query := h.entClient.User.Query()
	if req.Q != "" {
		predicates := []predicate.User{
			entuser.UsernameContainsFold(req.Q),
			entuser.EmailContainsFold(req.Q),
		}
		if n, err := strconv.Atoi(req.Q); err == nil {
			predicates = append(predicates, entuser.IDEQ(n), entuser.RelayUserIDEQ(n))
		}
		query = query.Where(entuser.Or(predicates...))
	}

	total, err := query.Clone().Count(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list users: "+err.Error())
		return
	}

	users, err := query.
		Order(ent.Asc(entuser.FieldID)).
		Limit(req.PageSize).
		Offset((req.Page - 1) * req.PageSize).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "list users: "+err.Error())
		return
	}

	items := make([]adminUserRow, 0, len(users))
	for _, u := range users {
		relayPassword := ""
		if u.RelayAuthPassword != nil {
			relayPassword = strings.TrimSpace(*u.RelayAuthPassword)
		}
		items = append(items, adminUserRow{
			ID:                u.ID,
			Username:          u.Username,
			Email:             u.Email,
			Role:              string(u.Role),
			AuthSource:        string(u.AuthSource),
			RelayUserID:       u.RelayUserID,
			RelayAuthPassword: relayPassword,
			CreatedAt:         u.CreatedAt,
			UpdatedAt:         u.UpdatedAt,
		})
	}

	pkg.Success(c, gin.H{
		"items":     items,
		"total":     total,
		"page":      req.Page,
		"page_size": req.PageSize,
	})
}

func (h *AdminUsersHandler) RevealRelayPassword(c *gin.Context) {
	id, err := strconv.Atoi(strings.TrimSpace(c.Param("id")))
	if err != nil || id <= 0 {
		pkg.Error(c, http.StatusBadRequest, "invalid user id")
		return
	}

	u, err := h.entClient.User.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "user not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "get user: "+err.Error())
		return
	}

	if u.RelayAuthPassword == nil || strings.TrimSpace(*u.RelayAuthPassword) == "" {
		pkg.Error(c, http.StatusUnprocessableEntity, "relay auth password is not stored")
		return
	}
	if h.encryptionKey == "" {
		pkg.Error(c, http.StatusInternalServerError, "relay auth password cannot be decrypted")
		return
	}

	password, err := pkg.Decrypt(strings.TrimSpace(*u.RelayAuthPassword), h.encryptionKey)
	if err != nil || strings.TrimSpace(password) == "" {
		pkg.Error(c, http.StatusInternalServerError, "relay auth password cannot be decrypted")
		return
	}

	pkg.Success(c, gin.H{"password": password})
}

func parseAdminUsersListRequest(c *gin.Context) adminUsersListRequest {
	page := parseOptionalInt(c.DefaultQuery("page", "1"))
	if page <= 0 {
		page = 1
	}
	pageSize := parseOptionalInt(c.DefaultQuery("page_size", "20"))
	switch {
	case pageSize <= 0:
		pageSize = 20
	case pageSize > 100:
		pageSize = 100
	}
	return adminUsersListRequest{
		Q:        strings.TrimSpace(c.Query("q")),
		Page:     page,
		PageSize: pageSize,
	}
}
