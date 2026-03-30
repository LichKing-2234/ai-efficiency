package handler

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/ent/relayprovider"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ProviderHandler handles relay provider management and API key delivery.
type ProviderHandler struct {
	entClient     *ent.Client
	encryptionKey string
	logger        *zap.Logger

	mu            sync.RWMutex
	providerCache map[int]relay.Provider
}

// NewProviderHandler creates a new provider handler.
func NewProviderHandler(entClient *ent.Client, encryptionKey string, logger *zap.Logger) *ProviderHandler {
	return &ProviderHandler{
		entClient:     entClient,
		encryptionKey: encryptionKey,
		logger:        logger,
		providerCache: make(map[int]relay.Provider),
	}
}

func (h *ProviderHandler) getOrCreateRelayProvider(p *ent.RelayProvider) relay.Provider {
	h.mu.RLock()
	rp, ok := h.providerCache[p.ID]
	h.mu.RUnlock()
	if ok {
		return rp
	}

	adminKey, err := decryptAESGCM(p.AdminAPIKey, h.encryptionKey)
	if err != nil {
		h.logger.Error("failed to decrypt admin_api_key", zap.String("provider", p.Name), zap.Error(err))
		adminKey = p.AdminAPIKey
	}

	rp = relay.NewSub2apiProvider(
		http.DefaultClient,
		p.BaseURL,
		p.AdminURL,
		adminKey,
		p.DefaultModel,
		h.logger,
	)

	h.mu.Lock()
	h.providerCache[p.ID] = rp
	h.mu.Unlock()
	return rp
}

func (h *ProviderHandler) invalidateCache() {
	h.mu.Lock()
	h.providerCache = make(map[int]relay.Provider)
	h.mu.Unlock()
}

type providerResponse struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	APIKeyID     int64  `json:"api_key_id"`
	DefaultModel string `json:"default_model"`
	IsPrimary    bool   `json:"is_primary"`
}

// ListForUser handles GET /api/v1/providers — returns providers with user's API keys.
func (h *ProviderHandler) ListForUser(c *gin.Context) {
	ctx := c.Request.Context()

	userID, _ := c.Get("user_id")

	user, err := h.entClient.User.Get(ctx, userID.(int))
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to get user")
		return
	}

	if user.RelayUserID == nil {
		primaryProvider, err := h.entClient.RelayProvider.Query().
			Where(relayprovider.IsPrimaryEQ(true), relayprovider.EnabledEQ(true)).
			First(ctx)
		if err != nil || primaryProvider == nil {
			pkg.Success(c, gin.H{"providers": []providerResponse{}})
			return
		}
		rp := h.getOrCreateRelayProvider(primaryProvider)
		relayUser, err := rp.FindUserByEmail(ctx, user.Email)
		if err != nil || relayUser == nil {
			pkg.Success(c, gin.H{
				"providers": []providerResponse{},
				"message":   "当前账号未关联 relay server，无法自动配置 AI 工具。请联系管理员。",
			})
			return
		}
		relayID := int(relayUser.ID)
		h.entClient.User.UpdateOneID(user.ID).SetRelayUserID(relayID).Save(ctx)
		user.RelayUserID = &relayID
	}

	providers, err := h.entClient.RelayProvider.Query().
		Where(relayprovider.EnabledEQ(true)).
		All(ctx)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list providers")
		return
	}

	var result []providerResponse
	for _, p := range providers {
		rp := h.getOrCreateRelayProvider(p)

		// Try to find existing ae-cli-auto key first; only create if none exists.
		var apiKey string
		var apiKeyID int64
		keys, err := rp.ListUserAPIKeys(ctx, int64(*user.RelayUserID))
		if err == nil {
			for _, k := range keys {
				if k.Name == "ae-cli-auto" && k.Status == "active" {
					apiKeyID = k.ID
					break
				}
			}
		}
		if apiKeyID == 0 {
			// No existing key found — create a new one.
			newKey, err := rp.CreateUserAPIKey(ctx, int64(*user.RelayUserID), relay.APIKeyCreateRequest{Name: "ae-cli-auto"})
			if err != nil {
				h.logger.Warn("failed to create API key", zap.String("provider", p.Name), zap.Error(err))
				continue
			}
			apiKey = newKey.Secret
			apiKeyID = newKey.ID
		}

		result = append(result, providerResponse{
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			BaseURL:      p.BaseURL,
			APIKey:       apiKey,
			APIKeyID:     apiKeyID,
			DefaultModel: p.DefaultModel,
			IsPrimary:    p.IsPrimary,
		})
	}

	pkg.Success(c, gin.H{"providers": result})
}

type adminProviderResponse struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	BaseURL      string `json:"base_url"`
	AdminURL     string `json:"admin_url"`
	RelayType    string `json:"relay_type"`
	AdminAPIKey  string `json:"admin_api_key"`
	DefaultModel string `json:"default_model"`
	IsPrimary    bool   `json:"is_primary"`
	Enabled      bool   `json:"enabled"`
}

// List handles GET /api/v1/admin/providers — admin list all providers.
func (h *ProviderHandler) List(c *gin.Context) {
	ctx := c.Request.Context()
	providers, err := h.entClient.RelayProvider.Query().All(ctx)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to list providers")
		return
	}
	var result []adminProviderResponse
	for _, p := range providers {
		result = append(result, adminProviderResponse{
			ID:           p.ID,
			Name:         p.Name,
			DisplayName:  p.DisplayName,
			BaseURL:      p.BaseURL,
			AdminURL:     p.AdminURL,
			RelayType:    p.RelayType,
			AdminAPIKey:  "***",
			DefaultModel: p.DefaultModel,
			IsPrimary:    p.IsPrimary,
			Enabled:      p.Enabled,
		})
	}
	pkg.Success(c, result)
}

// Create handles POST /api/v1/admin/providers
func (h *ProviderHandler) Create(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		DisplayName  string `json:"display_name" binding:"required"`
		BaseURL      string `json:"base_url" binding:"required"`
		AdminURL     string `json:"admin_url" binding:"required"`
		RelayType    string `json:"relay_type"`
		AdminAPIKey  string `json:"admin_api_key" binding:"required"`
		DefaultModel string `json:"default_model"`
		IsPrimary    bool   `json:"is_primary"`
		Enabled      bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()

	// Encrypt admin API key before storing
	encryptedKey, err := encryptAESGCM(req.AdminAPIKey, h.encryptionKey)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to encrypt API key")
		return
	}

	create := h.entClient.RelayProvider.Create().
		SetName(req.Name).
		SetDisplayName(req.DisplayName).
		SetBaseURL(req.BaseURL).
		SetAdminURL(req.AdminURL).
		SetAdminAPIKey(encryptedKey).
		SetIsPrimary(req.IsPrimary).
		SetEnabled(req.Enabled)

	if req.RelayType != "" {
		create.SetRelayType(req.RelayType)
	}
	if req.DefaultModel != "" {
		create.SetDefaultModel(req.DefaultModel)
	}

	p, err := create.Save(ctx)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to create provider")
		return
	}
	h.invalidateCache()
	pkg.Created(c, p)
}

// Update handles PUT /api/v1/admin/providers/:id
func (h *ProviderHandler) Update(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid provider id")
		return
	}

	var req struct {
		DisplayName  string `json:"display_name"`
		BaseURL      string `json:"base_url"`
		AdminURL     string `json:"admin_url"`
		RelayType    string `json:"relay_type"`
		AdminAPIKey  string `json:"admin_api_key"`
		DefaultModel string `json:"default_model"`
		IsPrimary    *bool  `json:"is_primary"`
		Enabled      *bool  `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		pkg.Error(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx := c.Request.Context()
	update := h.entClient.RelayProvider.UpdateOneID(id)

	if req.DisplayName != "" {
		update.SetDisplayName(req.DisplayName)
	}
	if req.BaseURL != "" {
		update.SetBaseURL(req.BaseURL)
	}
	if req.AdminURL != "" {
		update.SetAdminURL(req.AdminURL)
	}
	if req.RelayType != "" {
		update.SetRelayType(req.RelayType)
	}
	if req.AdminAPIKey != "" {
		encryptedKey, err := encryptAESGCM(req.AdminAPIKey, h.encryptionKey)
		if err != nil {
			pkg.Error(c, http.StatusInternalServerError, "failed to encrypt API key")
			return
		}
		update.SetAdminAPIKey(encryptedKey)
	}
	if req.DefaultModel != "" {
		update.SetDefaultModel(req.DefaultModel)
	}
	if req.IsPrimary != nil {
		update.SetIsPrimary(*req.IsPrimary)
	}
	if req.Enabled != nil {
		update.SetEnabled(*req.Enabled)
	}

	p, err := update.Save(ctx)
	if err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to update provider")
		return
	}
	h.invalidateCache()
	pkg.Success(c, p)
}

// Delete handles DELETE /api/v1/admin/providers/:id
func (h *ProviderHandler) Delete(c *gin.Context) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		pkg.Error(c, http.StatusBadRequest, "invalid provider id")
		return
	}

	ctx := c.Request.Context()
	if err := h.entClient.RelayProvider.DeleteOneID(id).Exec(ctx); err != nil {
		pkg.Error(c, http.StatusInternalServerError, "failed to delete provider")
		return
	}
	h.invalidateCache()
	pkg.Success(c, gin.H{"message": "deleted"})
}

func encryptAESGCM(plaintext, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

func decryptAESGCM(ciphertextHex, keyHex string) (string, error) {
	key, err := hex.DecodeString(keyHex)
	if err != nil {
		return "", err
	}
	ciphertext, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
