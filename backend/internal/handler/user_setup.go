package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/usersetup"
	"github.com/gin-gonic/gin"
)

type userSetupService interface {
	ListProviders(ctx context.Context, req usersetup.ListProvidersRequest) (*usersetup.ListProvidersResponse, error)
	CreateManagedKey(ctx context.Context, req usersetup.CreateManagedKeyRequest) (*usersetup.CreateManagedKeyResult, error)
	RegenerateManagedKey(ctx context.Context, req usersetup.RegenerateManagedKeyRequest) (*usersetup.CreateManagedKeyResult, error)
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

func (h *UserSetupHandler) CreateManagedKey(c *gin.Context) {
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

	resp, err := h.service.CreateManagedKey(c.Request.Context(), usersetup.CreateManagedKeyRequest{
		UserID:     uc.UserID,
		ProviderID: providerID,
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

func (h *UserSetupHandler) RegenerateManagedKey(c *gin.Context) {
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

	resp, err := h.service.RegenerateManagedKey(c.Request.Context(), usersetup.RegenerateManagedKeyRequest{
		UserID:     uc.UserID,
		ProviderID: providerID,
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
