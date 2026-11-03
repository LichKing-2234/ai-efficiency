package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/ai-efficiency/backend/internal/authstate"
	"github.com/ai-efficiency/backend/internal/web"
	"github.com/gin-gonic/gin"
)

const codeExpiry = 5 * time.Minute

const (
	deviceCodeExpiryDefault   = 15 * time.Minute
	devicePollIntervalDefault = 5 * time.Second
	deviceStatusPending       = "pending"
	deviceStatusApproved      = "approved"
	deviceStatusDenied        = "denied"
	deviceStatusExpired       = "expired"
	deviceStatusConsumed      = "consumed"
	deviceGrantType           = "urn:ietf:params:oauth:grant-type:device_code"
)

const (
	authCodeKeyBase   = "oauth:code:"
	deviceCodeKeyBase = "oauth:device:"
	deviceUserKeyBase = "oauth:device-user-code:"
)

var (
	errRandomRead        = errors.New("oauth: random read failed")
	generateCodeFunc     = generateCode
	generateUserCodeFunc = generateUserCode
)

func authCodeKey(code string) string       { return authCodeKeyBase + code }
func deviceCodeKey(code string) string     { return deviceCodeKeyBase + code }
func deviceUserCodeKey(code string) string { return deviceUserKeyBase + code }

func encodeState(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode oauth state: %w", err)
	}
	return raw, nil
}

func decodeState[T any](raw []byte) (T, error) {
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, fmt.Errorf("decode oauth state: %w", err)
	}
	return value, nil
}

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
	server             *Server
	frontendURL        string
	tokenGen           TokenGenerator
	now                func() time.Time
	deviceCodeExpiry   time.Duration
	devicePollInterval time.Duration

	state authstate.Store
}

// NewHandler creates a new OAuth handler with a process-local auth state store.
// Single-process deployments and tests keep working; the server wires the
// shared Redis store through NewHandlerWithStore.
func NewHandler(server *Server, frontendURL string, tokenGen TokenGenerator) *Handler {
	return NewHandlerWithStore(server, frontendURL, tokenGen, authstate.NewMemoryStore())
}

// NewHandlerWithStore creates a new OAuth handler backed by the given auth
// state store.
func NewHandlerWithStore(server *Server, frontendURL string, tokenGen TokenGenerator, state authstate.Store) *Handler {
	if state == nil {
		state = authstate.NewMemoryStore()
	}
	return &Handler{
		server:             server,
		frontendURL:        frontendURL,
		tokenGen:           tokenGen,
		now:                time.Now,
		deviceCodeExpiry:   deviceCodeExpiryDefault,
		devicePollInterval: devicePollIntervalDefault,
		state:              state,
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
	if !web.HasEmbeddedFrontend() || c.Request == nil || c.Request.URL == nil {
		return false
	}

	targetPath := path
	if h.frontendURL != "" {
		target, err := url.Parse(strings.TrimRight(h.frontendURL, "/") + path)
		if err == nil && target.Path != "" {
			targetPath = target.Path
		}
	}
	return targetPath == c.Request.URL.Path
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

	code, err := generateCodeFunc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	entry := authCodeEntry{
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
	encoded, err := encodeState(entry)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if err := h.state.Set(c.Request.Context(), authCodeKey(code), encoded, codeExpiry); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

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
	deviceCode, err := generateCodeFunc()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

	entry := deviceEntry{
		DeviceCode:      deviceCode,
		ClientID:        clientID,
		Status:          deviceStatusPending,
		CreatedAt:       now,
		ExpiresAt:       now.Add(h.deviceCodeExpiry),
		PollIntervalSec: int(h.devicePollInterval / time.Second),
	}
	var (
		userCode           string
		normalizedUserCode string
	)
	for range 8 {
		generated, genErr := generateUserCodeFunc()
		if genErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		normalized := normalizeUserCode(generated)
		stored, storeErr := h.state.SetIfAbsent(c.Request.Context(), deviceUserCodeKey(normalized), []byte(deviceCode), h.deviceCodeExpiry)
		if storeErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
			return
		}
		if stored {
			userCode, normalizedUserCode = generated, normalized
			break
		}
	}
	if userCode == "" {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	entry.UserCode = normalizedUserCode
	encoded, err := encodeState(entry)
	if err != nil {
		_ = h.state.Delete(c.Request.Context(), deviceUserCodeKey(normalizedUserCode))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if err := h.state.Set(c.Request.Context(), deviceCodeKey(deviceCode), encoded, h.deviceCodeExpiry); err != nil {
		_ = h.state.Delete(c.Request.Context(), deviceUserCodeKey(normalizedUserCode))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}

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

	rawDeviceCode, found, err := h.state.Get(c.Request.Context(), deviceUserCodeKey(normalizeUserCode(req.UserCode)))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_user_code", "message": "Code invalid or expired"})
		return
	}
	deviceCode := string(rawDeviceCode)

	resolvedStatus := ""
	found, err = h.state.Update(c.Request.Context(), deviceCodeKey(deviceCode), h.deviceCodeExpiry, func(current []byte) ([]byte, error) {
		entry, err := decodeState[deviceEntry](current)
		if err != nil {
			return nil, err
		}
		if entry.Status == deviceStatusConsumed || h.isDeviceExpired(&entry) || entry.Status != deviceStatusPending {
			return nil, authstate.ErrUpdateAborted
		}
		if !req.Approved {
			entry.Status = deviceStatusDenied
			resolvedStatus = deviceStatusDenied
		} else {
			entry.Status = deviceStatusApproved
			entry.UserID = uc.UserID
			entry.Username = uc.Username
			entry.Role = uc.Role
			resolvedStatus = deviceStatusApproved
		}
		entry.LastPolledAt = time.Time{}
		return encodeState(entry)
	})
	if err != nil {
		if errors.Is(err, authstate.ErrUpdateAborted) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_user_code", "message": "Code invalid or expired"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if !found || resolvedStatus == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_user_code", "message": "Code invalid or expired"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": resolvedStatus})
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

	rawEntry, found, err := h.state.Take(c.Request.Context(), authCodeKey(code))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant", "error_description": "code not found or already used"})
		return
	}
	entry, err := decodeState[authCodeEntry](rawEntry)
	if err != nil {
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

	var (
		outcome string
		granted deviceEntry
	)
	found, err := h.state.Update(c.Request.Context(), deviceCodeKey(deviceCode), h.deviceCodeExpiry, func(current []byte) ([]byte, error) {
		entry, err := decodeState[deviceEntry](current)
		if err != nil {
			return nil, err
		}
		if entry.ClientID != clientID || entry.Status == deviceStatusConsumed {
			outcome = "invalid_grant"
			return nil, authstate.ErrUpdateAborted
		}
		if h.isDeviceExpired(&entry) {
			entry.Status = deviceStatusExpired
			outcome = "expired_token"
			return encodeState(entry)
		}
		if !entry.LastPolledAt.IsZero() && h.now().Sub(entry.LastPolledAt) < time.Duration(entry.PollIntervalSec)*time.Second {
			entry.LastPolledAt = h.now()
			outcome = "slow_down"
			return encodeState(entry)
		}
		entry.LastPolledAt = h.now()
		switch entry.Status {
		case deviceStatusPending:
			outcome = "authorization_pending"
		case deviceStatusDenied:
			outcome = "access_denied"
		case deviceStatusApproved:
			entry.Status = deviceStatusConsumed
			outcome = "token"
			granted = entry
		default:
			outcome = "invalid_grant"
			return nil, authstate.ErrUpdateAborted
		}
		return encodeState(entry)
	})
	if err != nil {
		if errors.Is(err, authstate.ErrUpdateAborted) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "server_error"})
		return
	}
	if !found {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}
	switch outcome {
	case "expired_token":
		c.JSON(http.StatusBadRequest, gin.H{"error": "expired_token"})
		return
	case "slow_down":
		c.JSON(http.StatusBadRequest, gin.H{"error": "slow_down"})
		return
	case "authorization_pending":
		c.JSON(http.StatusBadRequest, gin.H{"error": "authorization_pending"})
		return
	case "access_denied":
		c.JSON(http.StatusBadRequest, gin.H{"error": "access_denied"})
		return
	case "token":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_grant"})
		return
	}
	entry := granted

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

func (h *Handler) isDeviceExpired(entry *deviceEntry) bool {
	return !h.now().Before(entry.ExpiresAt)
}

func generateCode() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", errors.Join(errRandomRead, err)
	}
	return hex.EncodeToString(b), nil
}

func generateUserCode() (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	const groupSize = 4
	const groups = 2

	random := make([]byte, groupSize*groups)
	if _, err := rand.Read(random); err != nil {
		return "", errors.Join(errRandomRead, err)
	}

	code := make([]byte, 0, len(random)+1)
	for i, b := range random {
		if i == groupSize {
			code = append(code, '-')
		}
		code = append(code, alphabet[int(b)%len(alphabet)])
	}
	return string(code), nil
}

func normalizeUserCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	replacer := strings.NewReplacer(
		" ", "",
		"-", "",
		"_", "",
		".", "",
	)
	return replacer.Replace(code)
}
