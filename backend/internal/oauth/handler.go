package oauth

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ai-efficiency/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

const codeExpiry = 5 * time.Minute

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

// Handler handles OAuth2 endpoints.
type Handler struct {
	server      *Server
	frontendURL string
	tokenGen    TokenGenerator

	mu    sync.Mutex
	codes map[string]*authCodeEntry
}

// NewHandler creates a new OAuth handler.
func NewHandler(server *Server, frontendURL string, tokenGen TokenGenerator) *Handler {
	h := &Handler{
		server:      server,
		frontendURL: frontendURL,
		tokenGen:    tokenGen,
		codes:       make(map[string]*authCodeEntry),
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
		for code, entry := range h.codes {
			if time.Since(entry.CreatedAt) > codeExpiry {
				delete(h.codes, code)
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

	frontendURL := h.frontendURL + "/oauth/authorize?" + c.Request.URL.RawQuery
	c.Redirect(http.StatusFound, frontendURL)
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
		CreatedAt:     time.Now(),
	}
	h.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"redirect_uri": buildRedirectURI(req.RedirectURI, map[string]string{
			"code":  code,
			"state": req.State,
		}),
	})
}

// Token handles POST /oauth/token (authorization code -> JWT exchange).
func (h *Handler) Token(c *gin.Context) {
	grantType := c.PostForm("grant_type")
	if grantType != "authorization_code" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported_grant_type"})
		return
	}

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

	if time.Since(entry.CreatedAt) > codeExpiry {
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

func generateCode() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
