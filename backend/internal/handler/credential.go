package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/ent/scmprovider"
	"github.com/ai-efficiency/backend/internal/credential"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
)

// CredentialHandler handles admin credential CRUD operations.
type CredentialHandler struct {
	entClient     *ent.Client
	encryptionKey string
}

func NewCredentialHandler(entClient *ent.Client, encryptionKey string) *CredentialHandler {
	return &CredentialHandler{
		entClient:     entClient,
		encryptionKey: encryptionKey,
	}
}

type createCredentialRequest struct {
	Name        string          `json:"name" binding:"required"`
	Description string          `json:"description"`
	Kind        credential.Kind `json:"kind" binding:"required"`
	Payload     json.RawMessage `json:"payload" binding:"required"`
}

type updateCredentialRequest struct {
	Name        *string         `json:"name"`
	Description *string         `json:"description"`
	Payload     json.RawMessage `json:"payload"`
}

type credentialResponse struct {
	ID          int            `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Kind        string         `json:"kind"`
	UsageCount  int            `json:"usage_count"`
	Summary     map[string]any `json:"summary"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

func (h *CredentialHandler) List(c *gin.Context) {
	rows, err := h.entClient.Credential.Query().
		Order(ent.Desc(entcredential.FieldCreatedAt)).
		All(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list credentials")
		return
	}

	resp := make([]credentialResponse, 0, len(rows))
	for _, row := range rows {
		mapped, err := h.toResponse(c, row)
		if err != nil {
			pkg.Error(c, http.StatusInternalServerError, err.Error())
			return
		}
		resp = append(resp, mapped)
	}
	pkg.Success(c, resp)
}

func (h *CredentialHandler) Get(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid credential id")
		return
	}

	row, err := h.entClient.Credential.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "credential not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get credential")
		return
	}

	resp, err := h.toResponse(c, row)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, resp)
}

func (h *CredentialHandler) Create(c *gin.Context) {
	var req createCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	payload, err := credential.ParsePayload(req.Kind, req.Payload)
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	encrypted, err := pkg.Encrypt(string(req.Payload), h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to encrypt credential payload")
		return
	}

	row, err := h.entClient.Credential.Create().
		SetName(req.Name).
		SetDescription(req.Description).
		SetKind(entcredential.Kind(req.Kind)).
		SetPayload(encrypted).
		Save(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to create credential")
		return
	}

	resp, err := h.toResponseFromPayload(c, row, payload)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Created(c, resp)
}

func (h *CredentialHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid credential id")
		return
	}

	row, err := h.entClient.Credential.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "credential not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to get credential")
		return
	}

	var req updateCredentialRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	update := h.entClient.Credential.UpdateOne(row)
	if req.Name != nil {
		update.SetName(*req.Name)
	}
	if req.Description != nil {
		update.SetDescription(*req.Description)
	}
	if req.Payload != nil {
		if _, err := credential.ParsePayload(credential.Kind(row.Kind), req.Payload); err != nil {
			pkg.Error(c, http.StatusBadRequest, err.Error())
			return
		}
		encrypted, err := pkg.Encrypt(string(req.Payload), h.encryptionKey)
		if err != nil {
			pkg.Error(c, http.StatusInternalServerError, "failed to encrypt credential payload")
			return
		}
		update.SetPayload(encrypted)
	}

	saved, err := update.Save(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to update credential")
		return
	}

	resp, err := h.toResponse(c, saved)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	pkg.Success(c, resp)
}

func (h *CredentialHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid credential id")
		return
	}

	count, err := h.entClient.ScmProvider.Query().
		Where(scmprovider.Or(
			scmprovider.APICredentialIDEQ(id),
			scmprovider.CloneCredentialIDEQ(id),
		)).
		Count(c.Request.Context())
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to check credential usage")
		return
	}
	if count > 0 {
		pkg.Error(c, http.StatusConflict, "credential is still referenced by scm providers")
		return
	}

	if err := h.entClient.Credential.DeleteOneID(id).Exec(c.Request.Context()); err != nil {
		if ent.IsNotFound(err) {
			pkg.Error(c, http.StatusNotFound, "credential not found")
			return
		}
		pkg.Error(c, http.StatusInternalServerError, "failed to delete credential")
		return
	}

	pkg.Success(c, gin.H{"deleted": true})
}

func (h *CredentialHandler) toResponse(c *gin.Context, row *ent.Credential) (credentialResponse, error) {
	raw, err := pkg.Decrypt(row.Payload, h.encryptionKey)
	if err != nil {
		return credentialResponse{}, err
	}
	payload, err := credential.ParsePayload(credential.Kind(row.Kind), json.RawMessage(raw))
	if err != nil {
		return credentialResponse{}, err
	}
	return h.toResponseFromPayload(c, row, payload)
}

func (h *CredentialHandler) toResponseFromPayload(c *gin.Context, row *ent.Credential, payload credential.Payload) (credentialResponse, error) {
	usageCount, err := h.entClient.ScmProvider.Query().
		Where(scmprovider.Or(
			scmprovider.APICredentialIDEQ(row.ID),
			scmprovider.CloneCredentialIDEQ(row.ID),
		)).
		Count(c.Request.Context())
	if err != nil {
		return credentialResponse{}, err
	}
	return credentialResponse{
		ID:          row.ID,
		Name:        row.Name,
		Description: row.Description,
		Kind:        row.Kind.String(),
		UsageCount:  usageCount,
		Summary:     payload.MaskedSummary(),
		CreatedAt:   row.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   row.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}, nil
}
