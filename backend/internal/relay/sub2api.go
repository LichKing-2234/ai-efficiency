package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"
)

type sub2apiRelay struct {
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

	if len(result.Data) == 0 {
		return nil, nil
	}
	return &result.Data[0], nil
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
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
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
	httpReq.Header.Set("Authorization", "Bearer "+s.apiKey)
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

func (s *sub2apiRelay) CreateUserAPIKey(ctx context.Context, userID int64, keyName string) (*APIKeyWithSecret, error) {
	payload, _ := json.Marshal(map[string]any{
		"user_id": userID,
		"name":    keyName,
	})

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

// doAdminRequest is a helper that sends an authenticated request to the admin API.
func (s *sub2apiRelay) doAdminRequest(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.adminURL+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
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
