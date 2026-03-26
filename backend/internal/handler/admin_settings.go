package handler

import (
	"crypto/tls"
	"net/http"
	"sync/atomic"

	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/gin-gonic/gin"
	"github.com/go-ldap/ldap/v3"
)

// AdminSettingsHandler handles LDAP configuration management via config.yaml.
type AdminSettingsHandler struct {
	configPath string
	ldapConfig *atomic.Pointer[config.LDAPConfig]
}

// NewAdminSettingsHandler creates a new admin settings handler.
func NewAdminSettingsHandler(configPath string, ldapConfig *atomic.Pointer[config.LDAPConfig]) *AdminSettingsHandler {
	return &AdminSettingsHandler{
		configPath: configPath,
		ldapConfig: ldapConfig,
	}
}

type ldapSettingsResponse struct {
	URL          string `json:"url"`
	BaseDN       string `json:"base_dn"`
	BindDN       string `json:"bind_dn"`
	BindPassword string `json:"bind_password"`
	UserFilter   string `json:"user_filter"`
	TLS          bool   `json:"tls"`
}

type ldapSettingsRequest struct {
	URL          string `json:"url" binding:"required"`
	BaseDN       string `json:"base_dn" binding:"required"`
	BindDN       string `json:"bind_dn" binding:"required"`
	BindPassword string `json:"bind_password"`
	UserFilter   string `json:"user_filter" binding:"required"`
	TLS          bool   `json:"tls"`
}

// GetLDAP handles GET /api/v1/admin/settings/ldap
func (h *AdminSettingsHandler) GetLDAP(c *gin.Context) {
	cfg := h.ldapConfig.Load()
	if cfg == nil {
		pkg.Success(c, ldapSettingsResponse{})
		return
	}
	pkg.Success(c, ldapSettingsResponse{
		URL:          cfg.URL,
		BaseDN:       cfg.BaseDN,
		BindDN:       cfg.BindDN,
		BindPassword: "***",
		UserFilter:   cfg.UserFilter,
		TLS:          cfg.TLS,
	})
}

// UpdateLDAP handles PUT /api/v1/admin/settings/ldap
func (h *AdminSettingsHandler) UpdateLDAP(c *gin.Context) {
	var req ldapSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// If password is masked, keep the current one
	bindPassword := req.BindPassword
	if bindPassword == "" || bindPassword == "***" {
		if current := h.ldapConfig.Load(); current != nil {
			bindPassword = current.BindPassword
		}
	}

	newCfg := &config.LDAPConfig{
		URL:          req.URL,
		BaseDN:       req.BaseDN,
		BindDN:       req.BindDN,
		BindPassword: bindPassword,
		UserFilter:   req.UserFilter,
		TLS:          req.TLS,
	}

	// Persist to config.yaml
	if err := h.persistLDAPConfig(newCfg); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to save config: "+err.Error())
		return
	}

	// Hot-reload
	h.ldapConfig.Store(newCfg)

	pkg.Success(c, gin.H{"message": "LDAP settings updated"})
}

// TestLDAP handles POST /api/v1/admin/settings/ldap/test
func (h *AdminSettingsHandler) TestLDAP(c *gin.Context) {
	var req ldapSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	// If password is empty or masked, fall back to the current config.
	bindPassword := req.BindPassword
	if bindPassword == "" || bindPassword == "***" {
		if current := h.ldapConfig.Load(); current != nil {
			bindPassword = current.BindPassword
		}
	}

	conn, err := ldap.DialURL(req.URL)
	if err != nil {
		pkg.Error(c, http.StatusBadGateway, "failed to connect to LDAP server: "+err.Error())
		return
	}
	defer conn.Close()

	if req.TLS {
		if err := conn.StartTLS(&tls.Config{InsecureSkipVerify: false}); err != nil {
			pkg.Error(c, http.StatusBadGateway, "LDAP StartTLS failed: "+err.Error())
			return
		}
	}

	if err := conn.Bind(req.BindDN, bindPassword); err != nil {
		pkg.Error(c, http.StatusUnprocessableEntity, "LDAP bind failed: "+err.Error())
		return
	}

	pkg.Success(c, gin.H{"message": "LDAP connection successful"})
}

func (h *AdminSettingsHandler) persistLDAPConfig(ldapCfg *config.LDAPConfig) error {
	return updateYAMLSection(h.configPath, []string{"auth", "ldap"}, map[string]interface{}{
		"url":           ldapCfg.URL,
		"base_dn":       ldapCfg.BaseDN,
		"bind_dn":       ldapCfg.BindDN,
		"bind_password": ldapCfg.BindPassword,
		"user_filter":   ldapCfg.UserFilter,
		"tls":           ldapCfg.TLS,
	})
}
