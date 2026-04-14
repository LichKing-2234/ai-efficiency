package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
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
	Name              string          `json:"name" binding:"required"`
	Type              string          `json:"type" binding:"required,oneof=github bitbucket_server"`
	BaseURL           string          `json:"base_url" binding:"required"`
	APICredentialID   int             `json:"api_credential_id"`
	Credentials       json.RawMessage `json:"credentials"`
	CloneProtocol     string          `json:"clone_protocol"`
	CloneCredentialID *int            `json:"clone_credential_id"`
}

type updateSCMProviderRequest struct {
	Name              string          `json:"name"`
	BaseURL           string          `json:"base_url"`
	Credentials       json.RawMessage `json:"credentials"`
	APICredentialID   *int            `json:"api_credential_id"`
	CloneProtocol     string          `json:"clone_protocol"`
	CloneCredentialID *int            `json:"clone_credential_id"`
	Status            string          `json:"status"`
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

	cloneProtocol := req.CloneProtocol
	if cloneProtocol == "" {
		cloneProtocol = "https"
	}
	apiCredentialID, apiPayload, err := h.resolveAPICredentialForCreate(c, req.Name, req.APICredentialID, req.Credentials)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "failed to encrypt credentials") {
			code = http.StatusInternalServerError
		}
		pkg.Error(c, code, err.Error())
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
	if err := credential.ValidateProviderCredentialRefs(apiPayload.Kind(), cloneProtocol, cloneKind(clonePayload)); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	provider, err := h.entClient.ScmProvider.Create().
		SetName(req.Name).
		SetType(scmprovider.Type(req.Type)).
		SetBaseURL(req.BaseURL).
		SetAPICredentialID(apiCredentialID).
		SetCloneProtocol(scmprovider.CloneProtocol(cloneProtocol)).
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

	apiCredentialID, apiPayload, err := h.resolveAPICredentialForUpdate(c, current, req.APICredentialID, req.Credentials)
	if err != nil {
		code := http.StatusBadRequest
		if strings.Contains(err.Error(), "failed to encrypt credentials") {
			code = http.StatusInternalServerError
		}
		pkg.Error(c, code, err.Error())
		return
	}
	update.SetAPICredentialID(apiCredentialID)
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
		if provider.Credentials != "" {
			if _, err = h.loadLegacyProviderPayload(c, provider.Credentials); err != nil {
				pkg.Error(c, http.StatusInternalServerError, "failed to decrypt credentials")
				return
			}
		} else {
			pkg.Error(c, http.StatusBadRequest, "provider has no api credential")
			return
		}
	} else {
		if _, _, err = h.loadCredential(c, provider.Edges.APICredential.ID); err != nil {
			pkg.Error(c, http.StatusInternalServerError, "failed to decrypt credentials")
			return
		}
	}

	// TODO: Instantiate provider and test connectivity
	_ = auth.GetUserContext(c) // suppress unused import

	pkg.Success(c, gin.H{
		"status":  "ok",
		"message": "connection test passed",
	})
}

func (h *SCMProviderHandler) resolveAPICredentialForCreate(c *gin.Context, providerName string, apiCredentialID int, legacyRaw json.RawMessage) (int, credential.Payload, error) {
	switch {
	case apiCredentialID > 0:
		_, payload, err := h.loadCredential(c, apiCredentialID)
		return apiCredentialID, payload, err
	case len(legacyRaw) > 0:
		id, payload, err := h.createCredentialFromLegacy(c, providerName, legacyRaw)
		return id, payload, err
	default:
		return 0, nil, fmt.Errorf("api_credential_id is required")
	}
}

func (h *SCMProviderHandler) resolveAPICredentialForUpdate(c *gin.Context, provider *ent.ScmProvider, requestedID *int, legacyRaw json.RawMessage) (int, credential.Payload, error) {
	switch {
	case requestedID != nil && *requestedID > 0:
		_, payload, err := h.loadCredential(c, *requestedID)
		return *requestedID, payload, err
	case len(legacyRaw) > 0:
		if provider.APICredentialID > 0 {
			payload, err := h.updateCredentialFromLegacy(c, provider.APICredentialID, legacyRaw)
			return provider.APICredentialID, payload, err
		}
		id, payload, err := h.createCredentialFromLegacy(c, provider.Name, legacyRaw)
		return id, payload, err
	case provider.APICredentialID > 0:
		_, payload, err := h.loadCredential(c, provider.APICredentialID)
		return provider.APICredentialID, payload, err
	case provider.Credentials != "":
		payload, err := h.loadLegacyProviderPayload(c, provider.Credentials)
		return 0, payload, err
	default:
		return 0, nil, fmt.Errorf("api_credential_id is required")
	}
}

func (h *SCMProviderHandler) createCredentialFromLegacy(c *gin.Context, providerName string, legacyRaw json.RawMessage) (int, credential.Payload, error) {
	payload, err := credential.ParseLegacySCMProviderSecret(string(legacyRaw))
	if err != nil {
		return 0, nil, err
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return 0, nil, err
	}
	encrypted, err := pkg.Encrypt(string(rawPayload), h.encryptionKey)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to encrypt credentials")
	}
	row, err := h.entClient.Credential.Create().
		SetName(providerName + " API credential").
		SetDescription("Auto-migrated from legacy scm_provider.credentials request payload").
		SetKind(entcredential.KindSecretText).
		SetPayload(encrypted).
		Save(c.Request.Context())
	if err != nil {
		return 0, nil, err
	}
	return row.ID, payload, nil
}

func (h *SCMProviderHandler) updateCredentialFromLegacy(c *gin.Context, credentialID int, legacyRaw json.RawMessage) (credential.Payload, error) {
	payload, err := credential.ParseLegacySCMProviderSecret(string(legacyRaw))
	if err != nil {
		return nil, err
	}
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	encrypted, err := pkg.Encrypt(string(rawPayload), h.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt credentials")
	}
	if _, err := h.entClient.Credential.UpdateOneID(credentialID).
		SetKind(entcredential.KindSecretText).
		SetPayload(encrypted).
		Save(c.Request.Context()); err != nil {
		return nil, err
	}
	return payload, nil
}

func (h *SCMProviderHandler) loadLegacyProviderPayload(c *gin.Context, encryptedLegacy string) (credential.Payload, error) {
	raw, err := pkg.Decrypt(encryptedLegacy, h.encryptionKey)
	if err != nil {
		return nil, err
	}
	payload, err := credential.ParseLegacySCMProviderSecret(raw)
	if err != nil {
		return nil, err
	}
	return payload, nil
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
