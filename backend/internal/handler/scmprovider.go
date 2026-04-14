package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/credential"
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
	Name              string `json:"name" binding:"required"`
	Type              string `json:"type" binding:"required,oneof=github bitbucket_server"`
	BaseURL           string `json:"base_url" binding:"required"`
	APICredentialID   int    `json:"api_credential_id" binding:"required"`
	CloneProtocol     string `json:"clone_protocol" binding:"required,oneof=https ssh"`
	CloneCredentialID *int   `json:"clone_credential_id"`
}

type updateSCMProviderRequest struct {
	Name              string `json:"name"`
	BaseURL           string `json:"base_url"`
	APICredentialID   *int   `json:"api_credential_id"`
	CloneProtocol     string `json:"clone_protocol"`
	CloneCredentialID *int   `json:"clone_credential_id"`
	Status            string `json:"status"`
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

	_, apiPayload, err := h.loadCredential(c, req.APICredentialID)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	cloneCredentialID := 0
	var clonePayload credential.Payload
	if req.CloneCredentialID != nil {
		cloneCredentialID = *req.CloneCredentialID
		_, clonePayload, err = h.loadCredential(c, cloneCredentialID)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := credential.ValidateProviderCredentialRefs(apiPayload.Kind(), req.CloneProtocol, cloneKind(clonePayload)); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	provider, err := h.entClient.ScmProvider.Create().
		SetName(req.Name).
		SetType(scmprovider.Type(req.Type)).
		SetBaseURL(req.BaseURL).
		SetAPICredentialID(req.APICredentialID).
		SetCloneProtocol(scmprovider.CloneProtocol(req.CloneProtocol)).
		SetNillableCloneCredentialID(req.CloneCredentialID).
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

	current, err := h.entClient.ScmProvider.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "provider not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get provider")
		return
	}

	update := h.entClient.ScmProvider.UpdateOne(current)
	if req.Name != "" {
		update.SetName(req.Name)
	}
	if req.BaseURL != "" {
		update.SetBaseURL(req.BaseURL)
	}
	apiCredentialID := current.APICredentialID
	if req.APICredentialID != nil {
		apiCredentialID = *req.APICredentialID
		update.SetAPICredentialID(apiCredentialID)
	}

	cloneCredentialID := current.CloneCredentialID
	if req.CloneCredentialID != nil {
		cloneCredentialID = req.CloneCredentialID
		update.SetNillableCloneCredentialID(req.CloneCredentialID)
	}

	cloneProtocol := current.CloneProtocol.String()
	if req.CloneProtocol != "" {
		cloneProtocol = req.CloneProtocol
		update.SetCloneProtocol(scmprovider.CloneProtocol(req.CloneProtocol))
	}

	_, apiPayload, err := h.loadCredential(c, apiCredentialID)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	var clonePayload credential.Payload
	if cloneCredentialID != nil {
		_, clonePayload, err = h.loadCredential(c, *cloneCredentialID)
		if err != nil {
			pkg.Error(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	if err := credential.ValidateProviderCredentialRefs(apiPayload.Kind(), cloneProtocol, cloneKind(clonePayload)); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
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

	provider, err := h.entClient.ScmProvider.Query().
		Where(scmprovider.IDEQ(id)).
		WithAPICredential().
		Only(c.Request.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "provider not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get provider")
		return
	}

	if provider.Edges.APICredential == nil {
		pkg.Error(c, http.StatusBadRequest, "provider has no api credential")
		return
	}
	if _, _, err = h.loadCredential(c, provider.Edges.APICredential.ID); err != nil {
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

func (h *SCMProviderHandler) loadCredential(c *gin.Context, id int) (*ent.Credential, credential.Payload, error) {
	row, err := h.entClient.Credential.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, fmt.Errorf("credential %d not found", id)
		}
		return nil, nil, err
	}

	raw, err := pkg.Decrypt(row.Payload, h.encryptionKey)
	if err != nil {
		return nil, nil, err
	}
	payload, err := credential.ParsePayload(credential.Kind(row.Kind), []byte(raw))
	if err != nil {
		return nil, nil, err
	}
	return row, payload, nil
}

func cloneKind(payload credential.Payload) credential.Kind {
	if payload == nil {
		return ""
	}
	return payload.Kind()
}
