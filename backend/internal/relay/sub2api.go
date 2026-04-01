package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type sub2apiRelay struct {
	mu       sync.RWMutex
	client   *http.Client
	baseURL  string // LLM API endpoint, e.g. http://localhost:3000/v1
	adminURL string // Admin API endpoint, e.g. http://localhost:3000
	apiKey   string // Admin API key
	model    string
	logger   *zap.Logger
}

// NewSub2apiProvider creates a new relay provider backed by a sub2api instance.
func NewSub2apiProvider(httpClient *http.Client, baseURL, adminURL, apiKey, model string, logger *zap.Logger) Provider {
	return &sub2apiRelay{
		client:   httpClient,
		baseURL:  strings.TrimRight(baseURL, "/"),
		adminURL: strings.TrimRight(adminURL, "/"),
		apiKey:   apiKey,
		model:    model,
		logger:   logger,
	}
}

func (s *sub2apiRelay) Name() string { return "sub2api" }

func (s *sub2apiRelay) adminAPIKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.apiKey
}

func (s *sub2apiRelay) SetAdminAPIKey(apiKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiKey = strings.TrimSpace(apiKey)
}

func (s *sub2apiRelay) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.adminURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("relay: ping: %w", err)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("relay: ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: ping: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (s *sub2apiRelay) Authenticate(ctx context.Context, username, password string) (*User, error) {
	// Step 1: Login to get session token.
	loginBody, _ := json.Marshal(map[string]string{
		"email":    username,
		"password": password,
	})
	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.adminURL+"/api/v1/auth/login", bytes.NewReader(loginBody))
	if err != nil {
		return nil, fmt.Errorf("relay: authenticate: %w", err)
	}
	loginReq.Header.Set("Content-Type", "application/json")

	loginResp, err := s.client.Do(loginReq)
	if err != nil {
		return nil, fmt.Errorf("relay: authenticate: %w", err)
	}
	defer loginResp.Body.Close()

	if loginResp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}

	rawBody, err := io.ReadAll(loginResp.Body)
	if err != nil {
		return nil, fmt.Errorf("relay: authenticate: read body: %w", err)
	}

	// Check for extra verification requirements.
	bodyStr := string(rawBody)
	if strings.Contains(bodyStr, "requires_2fa") || strings.Contains(bodyStr, "turnstile") {
		return nil, ErrExtraVerificationRequired
	}

	var loginResult struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rawBody, &loginResult); err != nil {
		return nil, fmt.Errorf("relay: authenticate: decode login: %w", err)
	}
	if loginResult.Code != 0 || loginResult.Data.AccessToken == "" {
		return nil, fmt.Errorf("relay: authenticate: login failed")
	}

	// Step 2: Get user info with session token.
	meReq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.adminURL+"/api/v1/auth/me", nil)
	if err != nil {
		return nil, fmt.Errorf("relay: authenticate: %w", err)
	}
	meReq.Header.Set("Authorization", "Bearer "+loginResult.Data.AccessToken)

	meResp, err := s.client.Do(meReq)
	if err != nil {
		return nil, fmt.Errorf("relay: authenticate: %w", err)
	}
	defer meResp.Body.Close()

	if meResp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}

	var meResult struct {
		Code int  `json:"code"`
		Data User `json:"data"`
	}
	if err := json.NewDecoder(meResp.Body).Decode(&meResult); err != nil {
		return nil, fmt.Errorf("relay: authenticate: decode me: %w", err)
	}
	if meResult.Code != 0 {
		return nil, fmt.Errorf("relay: authenticate: /me returned code %d", meResult.Code)
	}

	user := &meResult.Data
	// sub2api may return empty username; fall back to email
	if user.Username == "" && user.Email != "" {
		user.Username = user.Email
	}
	return user, nil
}

func (s *sub2apiRelay) GetUser(ctx context.Context, userID int64) (*User, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/users/%d", userID), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: get user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: get user: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool `json:"success"`
		Data    User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: get user: decode: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("relay: get user: request failed")
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/users?email="+url.QueryEscape(email), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by email: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: find user by email: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool   `json:"success"`
		Data    []User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: find user by email: decode: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("relay: find user by email: request failed")
	}

	if len(result.Data) == 0 {
		return nil, nil
	}
	return &result.Data[0], nil
}

func (s *sub2apiRelay) FindUserByUsername(ctx context.Context, username string) (*User, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/users?username="+url.QueryEscape(username), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by username: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: find user by username: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool   `json:"success"`
		Data    []User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: find user by username: decode: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("relay: find user by username: request failed")
	}

	if len(result.Data) == 0 {
		return nil, nil
	}
	return &result.Data[0], nil
}

func (s *sub2apiRelay) CreateUser(ctx context.Context, req CreateUserRequest) (*User, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("relay: create user: marshal: %w", err)
	}

	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: create user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: create user: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool `json:"success"`
		Data    User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: create user: decode: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("relay: create user: request failed")
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	req.Model = s.model

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.adminAPIKey())
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: chat completion: unexpected status %d", resp.StatusCode)
	}

	var openAIResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("relay: chat completion: decode: %w", err)
	}

	var content string
	if len(openAIResp.Choices) > 0 {
		content = openAIResp.Choices[0].Message.Content
	}

	return &ChatCompletionResponse{
		Content:    content,
		TokensUsed: openAIResp.Usage.TotalTokens,
	}, nil
}

func (s *sub2apiRelay) ChatCompletionWithTools(ctx context.Context, req ChatCompletionRequest, tools []ToolDef) (*ChatCompletionWithToolsResponse, error) {
	req.Model = s.model

	payload := struct {
		ChatCompletionRequest
		Tools []ToolDef `json:"tools"`
	}{
		ChatCompletionRequest: req,
		Tools:                 tools,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion with tools: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion with tools: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.adminAPIKey())
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion with tools: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: chat completion with tools: unexpected status %d", resp.StatusCode)
	}

	var openAIResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("relay: chat completion with tools: decode: %w", err)
	}

	result := &ChatCompletionWithToolsResponse{
		TokensUsed: openAIResp.Usage.TotalTokens,
	}
	if len(openAIResp.Choices) > 0 {
		result.Content = openAIResp.Choices[0].Message.Content
		result.ToolCalls = openAIResp.Choices[0].Message.ToolCalls
	}

	return result, nil
}

func (s *sub2apiRelay) GetUsageStats(ctx context.Context, userID int64, from, to time.Time) (*UsageStats, error) {
	path := fmt.Sprintf("/api/v1/admin/users/%d/usage?from=%s&to=%s",
		userID, from.Format(time.RFC3339), to.Format(time.RFC3339))

	resp, err := s.doAdminRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("relay: get usage stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: get usage stats: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool       `json:"success"`
		Data    UsageStats `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: get usage stats: decode: %w", err)
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) ListUserAPIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/users/%d/api-keys", userID), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: list api keys: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: list api keys: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool     `json:"success"`
		Data    []APIKey `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: list api keys: decode: %w", err)
	}

	return result.Data, nil
}

func (s *sub2apiRelay) CreateUserAPIKey(ctx context.Context, userID int64, req APIKeyCreateRequest) (*APIKeyWithSecret, error) {
	if userEmail := strings.TrimSpace(os.Getenv("AE_RELAY_JWT_EMAIL")); userEmail != "" {
		userPassword := strings.TrimSpace(os.Getenv("AE_RELAY_JWT_PASSWORD"))
		if userPassword == "" {
			return nil, fmt.Errorf("relay: create api key: AE_RELAY_JWT_PASSWORD is required when AE_RELAY_JWT_EMAIL is set")
		}
		token, user, err := s.loginSessionToken(ctx, userEmail, userPassword)
		if err != nil {
			return nil, fmt.Errorf("relay: create api key via jwt: %w", err)
		}
		if user == nil || user.ID != userID {
			return nil, fmt.Errorf("relay: create api key via jwt: authenticated user %d does not match requested user %d", user.ID, userID)
		}
		return s.createUserAPIKeyWithJWT(ctx, token, userID, req)
	}

	payloadMap := map[string]any{
		"user_id": userID,
		"name":    req.Name,
	}
	if req.ExpiresAt != nil {
		payloadMap["expires_at"] = req.ExpiresAt
	}
	if req.GroupID != "" {
		payloadMap["group_id"] = req.GroupID
	}

	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("relay: create api key: marshal: %w", err)
	}

	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/keys", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("relay: create api key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: create api key: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool             `json:"success"`
		Data    APIKeyWithSecret `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: create api key: decode: %w", err)
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) createUserAPIKeyWithJWT(ctx context.Context, token string, userID int64, req APIKeyCreateRequest) (*APIKeyWithSecret, error) {
	payloadMap := map[string]any{
		"name": req.Name,
	}
	if req.GroupID != "" {
		if groupID, err := strconv.ParseInt(req.GroupID, 10, 64); err == nil {
			payloadMap["group_id"] = groupID
		}
	}
	if req.ExpiresAt != nil {
		days := int(time.Until(*req.ExpiresAt).Hours() / 24)
		if days < 1 {
			days = 1
		}
		payloadMap["expires_in_days"] = days
	}

	payload, err := json.Marshal(payloadMap)
	if err != nil {
		return nil, fmt.Errorf("relay: create api key via jwt: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.adminURL+"/api/v1/keys", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("relay: create api key via jwt: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("relay: create api key via jwt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: create api key via jwt: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code int `json:"code"`
		Data struct {
			ID     int64  `json:"id"`
			UserID int64  `json:"user_id"`
			Name   string `json:"name"`
			Status string `json:"status"`
			Key    string `json:"key"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: create api key via jwt: decode: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("relay: create api key via jwt: request failed")
	}

	return &APIKeyWithSecret{
		APIKey: APIKey{
			ID:     result.Data.ID,
			UserID: result.Data.UserID,
			Name:   result.Data.Name,
			Status: result.Data.Status,
		},
		Secret: result.Data.Key,
	}, nil
}

func (s *sub2apiRelay) RevokeUserAPIKey(ctx context.Context, keyID int64) error {
	resp, err := s.doAdminRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/keys/%d/revoke", keyID), nil)
	if err != nil {
		return fmt.Errorf("relay: revoke api key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: revoke api key: unexpected status %d", resp.StatusCode)
	}
	return nil
}

func (s *sub2apiRelay) loginSessionToken(ctx context.Context, username, password string) (string, *User, error) {
	loginBody, _ := json.Marshal(map[string]string{
		"email":    username,
		"password": password,
	})
	loginReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.adminURL+"/api/v1/auth/login", bytes.NewReader(loginBody))
	if err != nil {
		return "", nil, fmt.Errorf("relay: authenticate: %w", err)
	}
	loginReq.Header.Set("Content-Type", "application/json")

	loginResp, err := s.client.Do(loginReq)
	if err != nil {
		return "", nil, fmt.Errorf("relay: authenticate: %w", err)
	}
	defer loginResp.Body.Close()

	if loginResp.StatusCode == http.StatusUnauthorized {
		return "", nil, ErrInvalidCredentials
	}

	rawBody, err := io.ReadAll(loginResp.Body)
	if err != nil {
		return "", nil, fmt.Errorf("relay: authenticate: read body: %w", err)
	}

	bodyStr := string(rawBody)
	if strings.Contains(bodyStr, "requires_2fa") || strings.Contains(bodyStr, "turnstile") {
		return "", nil, ErrExtraVerificationRequired
	}

	var loginResult struct {
		Code int `json:"code"`
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rawBody, &loginResult); err != nil {
		return "", nil, fmt.Errorf("relay: authenticate: decode login: %w", err)
	}
	if loginResult.Code != 0 || loginResult.Data.AccessToken == "" {
		return "", nil, fmt.Errorf("relay: authenticate: login failed")
	}

	meReq, err := http.NewRequestWithContext(ctx, http.MethodGet, s.adminURL+"/api/v1/auth/me", nil)
	if err != nil {
		return "", nil, fmt.Errorf("relay: authenticate: %w", err)
	}
	meReq.Header.Set("Authorization", "Bearer "+loginResult.Data.AccessToken)

	meResp, err := s.client.Do(meReq)
	if err != nil {
		return "", nil, fmt.Errorf("relay: authenticate: %w", err)
	}
	defer meResp.Body.Close()

	if meResp.StatusCode == http.StatusUnauthorized {
		return "", nil, ErrInvalidCredentials
	}

	var meResult struct {
		Code int  `json:"code"`
		Data User `json:"data"`
	}
	if err := json.NewDecoder(meResp.Body).Decode(&meResult); err != nil {
		return "", nil, fmt.Errorf("relay: authenticate: decode me: %w", err)
	}
	if meResult.Code != 0 {
		return "", nil, fmt.Errorf("relay: authenticate: /me returned code %d", meResult.Code)
	}

	user := &meResult.Data
	if user.Username == "" && user.Email != "" {
		user.Username = user.Email
	}
	return loginResult.Data.AccessToken, user, nil
}

func (s *sub2apiRelay) ListUsageLogsByAPIKeyExact(ctx context.Context, apiKeyID int64, from, to time.Time) ([]UsageLog, error) {
	path := fmt.Sprintf("/api/v1/admin/usage_logs?api_key_id=%d&from=%s&to=%s",
		apiKeyID, url.QueryEscape(from.Format(time.RFC3339)), url.QueryEscape(to.Format(time.RFC3339)))

	resp, err := s.doAdminRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("relay: list usage logs by api key: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: list usage logs by api key: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Success bool       `json:"success"`
		Data    []UsageLog `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: list usage logs by api key: decode: %w", err)
	}
	if !result.Success {
		return nil, fmt.Errorf("relay: list usage logs by api key: request failed")
	}

	return result.Data, nil
}

// doAdminRequest is a helper that sends an authenticated request to the admin API.
func (s *sub2apiRelay) doAdminRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.adminURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", s.adminAPIKey())
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.client.Do(req)
}

// openAIChatResponse is the internal representation of the OpenAI-compatible response.
type openAIChatResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls,omitempty"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}
