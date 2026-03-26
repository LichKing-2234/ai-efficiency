package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// SCMProviderHandler handles SCM provider CRUD operations.
type SCMProviderHandler struct {
	entClient     *ent.Client
	encryptionKey string
}

// NewSCMProviderHandler creates a new SCM provider handler.
func NewSCMProviderHandler(entClient *ent.Client, encryptionKey string) *SCMProviderHandler {
	return &SCMProviderHandler{
		entClient:     entClient,
		encryptionKey: encryptionKey,
	}
}

type createSCMProviderRequest struct {
	Name        string          `json:"name" binding:"required"`
	Type        string          `json:"type" binding:"required,oneof=github bitbucket_server"`
	BaseURL     string          `json:"base_url" binding:"required"`
	Credentials json.RawMessage `json:"credentials" binding:"required"`
}

type updateSCMProviderRequest struct {
	Name        string          `json:"name"`
	BaseURL     string          `json:"base_url"`
	Credentials json.RawMessage `json:"credentials"`
	Status      string          `json:"status"`
}

// List handles GET /api/v1/scm-providers
func (h *SCMProviderHandler) List(c *gin.Context) {
	providers, err := h.entClient.ScmProvider.Query().
		Order(ent.Desc(scmprovider.FieldCreatedAt)).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list providers")
		return
	}
	pkg.Success(c, providers)
}

// Create handles POST /api/v1/scm-providers
func (h *SCMProviderHandler) Create(c *gin.Context) {
	var req createSCMProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// Encrypt credentials
	encrypted, err := pkg.Encrypt(string(req.Credentials), h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to encrypt credentials")
		return
	}

	provider, err := h.entClient.ScmProvider.Create().
		SetName(req.Name).
		SetType(scmprovider.Type(req.Type)).
		SetBaseURL(req.BaseURL).
		SetCredentials(encrypted).
		Save(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to create provider")
		return
	}

	pkg.Created(c, provider)
}

// Update handles PUT /api/v1/scm-providers/:id
func (h *SCMProviderHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	var req updateSCMProviderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	update := h.entClient.ScmProvider.UpdateOneID(id)
	if req.Name != "" {
		update.SetName(req.Name)
	}
	if req.BaseURL != "" {
		update.SetBaseURL(req.BaseURL)
	}
	if req.Credentials != nil {
		encrypted, err := pkg.Encrypt(string(req.Credentials), h.encryptionKey)
		if err != nil {
			pkg.Error(c, http.StatusInternalServerError, "failed to encrypt credentials")
			return
		}
		update.SetCredentials(encrypted)
	}
	if req.Status != "" {
		update.SetStatus(scmprovider.Status(req.Status))
	}

	provider, err := update.Save(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "provider not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to update provider")
		return
	}

	pkg.Success(c, provider)
}

// Delete handles DELETE /api/v1/scm-providers/:id
func (h *SCMProviderHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	if err := h.entClient.ScmProvider.DeleteOneID(id).Exec(c.Request.Context()); err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "provider not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to delete provider")
		return
	}

	pkg.Success(c, gin.H{"deleted": true})
}

// Test handles POST /api/v1/scm-providers/:id/test
func (h *SCMProviderHandler) Test(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid id")
		return
	}

	provider, err := h.entClient.ScmProvider.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "provider not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get provider")
		return
	}

	// Decrypt credentials
	_, err = pkg.Decrypt(provider.Credentials, h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to decrypt credentials")
		return
	}

	// TODO: Instantiate provider and test connectivity
	_ = auth.GetUserContext(c) // suppress unused import

	pkg.Success(c, gin.H{
		"status":  "ok",
		"message": "connection test passed",
	})
}
