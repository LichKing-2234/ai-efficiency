package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/web"
	"github.com/gin-gonic/gin"
)

const codeExpiry = 5 * time.Minute

const (
	deviceCodeExpiryDefault  = 15 * time.Minute
	devicePollIntervalDefault = 5 * time.Second
	deviceStatusPending      = "pending"
	deviceStatusApproved     = "approved"
	deviceStatusDenied       = "denied"
	deviceStatusExpired      = "expired"
	deviceStatusConsumed     = "consumed"
	deviceGrantType          = "urn:ietf:params:oauth:grant-type:device_code"
)

// authCodeEntry stores a pending authorization code with its metadata.
type authCodeEntry struct {
	Code          string
	ClientID      string
	RedirectURI   string
	CodeChallenge string
	UserID        int
	Username      string
	Role          string
	State         string
	CreatedAt     time.Time
}

type deviceEntry struct {
	DeviceCode      string
	UserCode        string
	ClientID        string
	Status          string
	UserID          int
	Username        string
	Role            string
	CreatedAt       time.Time
	ExpiresAt       time.Time
	LastPolledAt    time.Time
	PollIntervalSec int
}

// Handler handles OAuth2 endpoints.
type Handler struct {
	server      *Server
	frontendURL string
	tokenGen    TokenGenerator
	now         func() time.Time
	deviceCodeExpiry  time.Duration
	devicePollInterval time.Duration

	mu      sync.Mutex
	codes   map[string]*authCodeEntry
	devices map[string]*deviceEntry
}

// NewHandler creates a new OAuth handler.
func NewHandler(server *Server, frontendURL string, tokenGen TokenGenerator) *Handler {
	h := &Handler{
		server:             server,
		frontendURL:        frontendURL,
		tokenGen:           tokenGen,
		now:                time.Now,
		deviceCodeExpiry:   deviceCodeExpiryDefault,
		devicePollInterval: devicePollIntervalDefault,
		codes:              make(map[string]*authCodeEntry),
		devices:            make(map[string]*deviceEntry),
	}
	// Background goroutine to reap expired auth codes every minute.
	go h.reapExpiredCodes()
	return h
}

// reapExpiredCodes periodically removes expired authorization codes from memory.
func (h *Handler) reapExpiredCodes() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		h.mu.Lock()
		now := h.now()
		for code, entry := range h.codes {
			if now.Sub(entry.CreatedAt) > codeExpiry {
				delete(h.codes, code)
			}
		}
		for code, entry := range h.devices {
			if h.isDeviceExpiredLocked(entry) || entry.Status == deviceStatusConsumed {
				delete(h.devices, code)
			}
		}
		h.mu.Unlock()
	}
}

// Authorize handles GET /oauth/authorize.
// Validates parameters, then 302 redirects to frontend authorize page.
func (h *Handler) Authorize(c *gin.Context) {
	clientID := c.Query("client_id")
	redirectURI := c.Query("redirect_uri")
	codeChallenge := c.Query("code_challenge")
	codeChallengeMethod := c.Query("code_challenge_method")
	responseType := c.Query("response_type")

	if responseType != "code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_response_type"})
		return
	}
	if !h.server.IsValidClient(clientID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client"})
		return
	}
	if !ValidateRedirectURI(redirectURI) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_redirect_uri"})
		return
	}
	if codeChallenge == "" || codeChallengeMethod != "S256" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_code_challenge"})
		return
	}

	if h.shouldServeEmbeddedPath(c, "/oauth/authorize") && web.ServeEmbeddedIndex(c) {
		return
	}

	frontendURL := strings.TrimRight(h.frontendURL, "/") + "/oauth/authorize?" + c.Request.URL.RawQuery
	c.Redirect(http.StatusFound, frontendURL)
}

func (h *Handler) shouldServeEmbeddedPath(c *gin.Context, path string) bool {
	if !web.HasEmbeddedFrontend() || h.frontendURL == "" {
		return false
	}

	target, err := url.Parse(strings.TrimRight(h.frontendURL, "/") + path)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return false
	}

	return strings.EqualFold(target.Scheme, requestScheme(c)) &&
		strings.EqualFold(target.Host, requestHost(c)) &&
		target.Path == c.Request.URL.Path
}

func requestScheme(c *gin.Context) string {
	if scheme := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); scheme != "" {
		return scheme
	}
	if c.Request.TLS != nil {
		return "https"
	}
	return "http"
}

func requestHost(c *gin.Context) string {
	if host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); host != "" {
		return host
	}
	return c.Request.Host
}

// ApproveRequest is the request body for POST /oauth/authorize/approve.
type ApproveRequest struct {
	ClientID            string `json:"client_id" binding:"required"`
	RedirectURI         string `json:"redirect_uri" binding:"required"`
	CodeChallenge       string `json:"code_challenge" binding:"required"`
	CodeChallengeMethod string `json:"code_challenge_method" binding:"required"`
	State               string `json:"state" binding:"required"`
	Approved            bool   `json:"approved"`
}

type verifyDeviceRequest struct {
	UserCode string `json:"user_code" binding:"required"`
	Approved bool   `json:"approved"`
}

// buildRedirectURI constructs a redirect URI with properly URL-encoded query parameters.
func buildRedirectURI(base string, params map[string]string) string {
	v := url.Values{}
	for k, val := range params {
		v.Set(k, val)
	}
	return base + "?" + v.Encode()
}

// Approve handles POST /oauth/authorize/approve (requires user JWT).
func (h *Handler) Approve(c *gin.Context) {
	var req ApproveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if !ValidateRedirectURI(req.RedirectURI) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_redirect_uri"})
		return
	}
	if !h.server.IsValidClient(req.ClientID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client"})
		return
	}

	if !req.Approved {
		c.JSON(http.StatusOK, gin.H{
			"redirect_uri": buildRedirectURI(req.RedirectURI, map[string]string{
				"error": "access_denied",
				"state": req.State,
			}),
		})
		return
	}

	// Extract user from JWT context set by RequireAuth middleware.
	uc := auth.GetUserContext(c)
	if uc == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user context"})
		return
	}
	uid := uc.UserID
	username := uc.Username
	role := uc.Role

	code := generateCode()

	h.mu.Lock()
	h.codes[code] = &authCodeEntry{
		Code:          code,
		ClientID:      req.ClientID,
		RedirectURI:   req.RedirectURI,
		CodeChallenge: req.CodeChallenge,
		UserID:        uid,
		Username:      username,
		Role:          role,
		State:         req.State,
		CreatedAt:     h.now(),
	}
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"redirect_uri": buildRedirectURI(req.RedirectURI, map[string]string{
			"code":  code,
			"state": req.State,
		}),
	})
}

// DeviceCode handles POST /oauth/device/code.
func (h *Handler) DeviceCode(c *gin.Context) {
	clientID := strings.TrimSpace(c.PostForm("client_id"))
	if !h.server.IsValidClient(clientID) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_client"})
		return
	}

	now := h.now()
	deviceCode := generateCode()
	userCode := generateUserCode()

	h.mu.Lock()
	h.devices[deviceCode] = &deviceEntry{
		DeviceCode:      deviceCode,
		UserCode:        normalizeUserCode(userCode),
		ClientID:        clientID,
		Status:          deviceStatusPending,
		CreatedAt:       now,
		ExpiresAt:       now.Add(h.deviceCodeExpiry),
		PollIntervalSec: int(h.devicePollInterval / time.Second),
	}
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"device_code":      deviceCode,
		"user_code":        userCode,
		"verification_uri": strings.TrimRight(h.frontendURL, "/") + "/oauth/device",
		"expires_in":       int(h.deviceCodeExpiry / time.Second),
		"interval":         int(h.devicePollInterval / time.Second),
	})
}

// DevicePage handles GET /oauth/device.
func (h *Handler) DevicePage(c *gin.Context) {
	if h.shouldServeEmbeddedPath(c, "/oauth/device") && web.ServeEmbeddedIndex(c) {
		return
	}
	c.Redirect(http.StatusFound, strings.TrimRight(h.frontendURL, "/")+"/oauth/device")
}

// VerifyDevice handles POST /oauth/device/verify (requires user JWT).
func (h *Handler) VerifyDevice(c *gin.Context) {
	var req verifyDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	uc := auth.GetUserContext(c)
	if uc == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "missing user context"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	entry := h.findDeviceByUserCodeLocked(req.UserCode)
	if entry == nil || entry.Status == deviceStatusConsumed || h.isDeviceExpiredLocked(entry) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_user_code", "message": "Code invalid or expired"})
		return
	}

	if entry.Status != deviceStatusPending {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_user_code", "message": "Code invalid or expired"})
		return
	}

	if !req.Approved {
		entry.Status = deviceStatusDenied
		entry.LastPolledAt = time.Time{}
		c.JSON(http.StatusOK, gin.H{"status": deviceStatusDenied})
		return
	}

	entry.Status = deviceStatusApproved
	entry.LastPolledAt = time.Time{}
	entry.UserID = uc.UserID
	entry.Username = uc.Username
	entry.Role = uc.Role

	c.JSON(http.StatusOK, gin.H{"status": deviceStatusApproved})
}

// Token handles POST /oauth/token (authorization code -> JWT exchange).
func (h *Handler) Token(c *gin.Context) {
	grantType := c.PostForm("grant_type")
	switch grantType {
	case "authorization_code":
		h.exchangeAuthorizationCode(c)
	case deviceGrantType:
		h.exchangeDeviceToken(c)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
	}
}

func (h *Handler) exchangeAuthorizationCode(c *gin.Context) {
	code := c.PostForm("code")
	redirectURI := c.PostForm("redirect_uri")
	codeVerifier := c.PostForm("code_verifier")
	clientID := c.PostForm("client_id")

	if code == "" || redirectURI == "" || codeVerifier == "" || clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	h.mu.Lock()
	entry, ok := h.codes[code]
	if ok {
		delete(h.codes, code)
	}
	h.mu.Unlock()

	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "code not found or already used"})
		return
	}

	if h.now().Sub(entry.CreatedAt) > codeExpiry {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "code expired"})
		return
	}

	if entry.ClientID != clientID || entry.RedirectURI != redirectURI {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "client_id or redirect_uri mismatch"})
		return
	}

	if !VerifyCodeChallenge(codeVerifier, entry.CodeChallenge, "S256") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "PKCE verification failed"})
		return
	}

	if h.tokenGen == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error", "error_description": "token generator not configured"})
		return
	}

	accessToken, refreshToken, expiresIn, err := h.tokenGen.GenerateAccessToken(entry.UserID, entry.Username, entry.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    expiresIn,
	})
}

func (h *Handler) exchangeDeviceToken(c *gin.Context) {
	deviceCode := c.PostForm("device_code")
	clientID := c.PostForm("client_id")

	if deviceCode == "" || clientID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request"})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.devices[deviceCode]
	if !ok || entry.ClientID != clientID || entry.Status == deviceStatusConsumed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}

	if h.isDeviceExpiredLocked(entry) {
		entry.Status = deviceStatusExpired
		c.JSON(http.StatusBadRequest, gin.H{"error": "expired_token"})
		return
	}

	if !entry.LastPolledAt.IsZero() && h.now().Sub(entry.LastPolledAt) < time.Duration(entry.PollIntervalSec)*time.Second {
		entry.LastPolledAt = h.now()
		c.JSON(http.StatusBadRequest, gin.H{"error": "slow_down"})
		return
	}
	entry.LastPolledAt = h.now()

	switch entry.Status {
	case deviceStatusPending:
		c.JSON(http.StatusBadRequest, gin.H{"error": "authorization_pending"})
		return
	case deviceStatusDenied:
		c.JSON(http.StatusBadRequest, gin.H{"error": "access_denied"})
		return
	case deviceStatusApproved:
		entry.Status = deviceStatusConsumed
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}

	if h.tokenGen == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error", "error_description": "token generator not configured"})
		return
	}

	accessToken, refreshToken, expiresIn, err := h.tokenGen.GenerateAccessToken(entry.UserID, entry.Username, entry.Role)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
		"expires_in":    expiresIn,
	})
}

func (h *Handler) findDeviceByUserCodeLocked(userCode string) *deviceEntry {
	normalized := normalizeUserCode(userCode)
	for _, entry := range h.devices {
		if entry.UserCode == normalized {
			return entry
		}
	}
	return nil
}

func (h *Handler) isDeviceExpiredLocked(entry *deviceEntry) bool {
	return !h.now().Before(entry.ExpiresAt)
}

func generateCode() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateUserCode() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	const groupSize = 4
	const groups = 2

	random := make([]byte, groupSize*groups)
	rand.Read(random)

	code := make([]byte, 0, len(random)+1)
	for i, b := range random {
		if i == groupSize {
			code = append(code, '-')
		}
		code = append(code, alphabet[int(b)%len(alphabet)])
	}
	return string(code)
}

func normalizeUserCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	return strings.ReplaceAll(code, " ", "")
}
