package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/usersetup"
	"github.com/gin-gonic/gin"
)

type userSetupService interface {
	ListProviders(ctx context.Context, req usersetup.ListProvidersRequest) (*usersetup.ListProvidersResponse, error)
	CreateGroupCredential(ctx context.Context, req usersetup.CreateGroupCredentialRequest) (*usersetup.CreateGroupCredentialResult, error)
	RegenerateGroupCredential(ctx context.Context, req usersetup.RegenerateGroupCredentialRequest) (*usersetup.CreateGroupCredentialResult, error)
}

type UserSetupHandler struct {
	service userSetupService
}

func NewUserSetupHandler(service userSetupService) *UserSetupHandler {
	return &UserSetupHandler{service: service}
}

func (h *UserSetupHandler) ListProviders(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	resp, err := h.service.ListProviders(c.Request.Context(), usersetup.ListProvidersRequest{UserID: uc.UserID})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Success(c, gin.H{
		"providers": resp.Providers,
		"message":   resp.Message,
	})
}

func (h *UserSetupHandler) CreateGroupCredential(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	providerID, err := parsePathInt(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid provider id")
		return
	}
	groupID := strings.TrimSpace(c.Param("group_id"))
	if groupID == "" {
		pkg.Error(c, http.StatusBadRequest, "invalid group id")
		return
	}

	resp, err := h.service.CreateGroupCredential(c.Request.Context(), usersetup.CreateGroupCredentialRequest{
		UserID:     uc.UserID,
		ProviderID: providerID,
		GroupID:    groupID,
	})
	if err != nil {
		if errors.Is(err, usersetup.ErrManagedKeyAlreadyExists) {
			pkg.Error(c, http.StatusConflict, err.Error())
			return
		}
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Success(c, resp)
}

func (h *UserSetupHandler) RegenerateGroupCredential(c *gin.Context) {
	uc := auth.GetUserContext(c)
	if uc == nil {
		pkg.Error(c, http.StatusUnauthorized, "unauthorized")
		return
	}

	providerID, err := parsePathInt(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid provider id")
		return
	}
	groupID := strings.TrimSpace(c.Param("group_id"))
	if groupID == "" {
		pkg.Error(c, http.StatusBadRequest, "invalid group id")
		return
	}

	resp, err := h.service.RegenerateGroupCredential(c.Request.Context(), usersetup.RegenerateGroupCredentialRequest{
		UserID:     uc.UserID,
		ProviderID: providerID,
		GroupID:    groupID,
	})
	if err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, err.Error())
		return
	}

	pkg.Success(c, resp)
}

func parsePathInt(raw string) (int, error) {
	return strconv.Atoi(raw)
}
