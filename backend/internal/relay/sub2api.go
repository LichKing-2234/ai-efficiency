package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	apiKey   string // LLM API key
	adminKey string // Admin API key
	model    string
	logger   *zap.Logger
}

type envelopeStatus struct {
	Success *bool `json:"success"`
	Code    *int  `json:"code"`
}

func (s envelopeStatus) ok() bool {
	if s.Success != nil {
		return *s.Success
	}
	if s.Code != nil {
		return *s.Code == 0
	}
	return false
}

// NewSub2apiProvider creates a new relay provider backed by a sub2api instance.
func NewSub2apiProvider(httpClient *http.Client, baseURL, adminURL, apiKey, model string, logger *zap.Logger) Provider {
	return &sub2apiRelay{
		client:   httpClient,
		baseURL:  normalizeInferenceBaseURL(baseURL),
		adminURL: strings.TrimRight(adminURL, "/"),
		apiKey:   apiKey,
		model:    model,
		logger:   logger,
	}
}

func normalizeInferenceBaseURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return ""
	}
	if strings.HasSuffix(raw, "/v1") {
		return raw
	}
	return raw + "/v1"
}

func (s *sub2apiRelay) Name() string { return "sub2api" }

func (s *sub2apiRelay) adminAPIKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if strings.TrimSpace(s.adminKey) != "" {
		return s.adminKey
	}
	return s.apiKey
}

func (s *sub2apiRelay) SetAdminAPIKey(apiKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.adminKey = strings.TrimSpace(apiKey)
}

func (s *sub2apiRelay) SetModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = strings.TrimSpace(model)
}

func (s *sub2apiRelay) inferenceAPIKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.apiKey)
}

func (s *sub2apiRelay) inferenceModel() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.model)
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
		envelopeStatus
		Data struct {
			User
			Subscriptions []struct {
				Status string `json:"status"`
				Group  Group  `json:"group"`
			} `json:"subscriptions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: get user: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("relay: get user: request failed")
	}
	user := result.Data.User
	if len(result.Data.Subscriptions) > 0 {
		user.AllowedGroups = make([]Group, 0, len(result.Data.Subscriptions))
		seen := make(map[int64]struct{}, len(result.Data.Subscriptions))
		for _, subscription := range result.Data.Subscriptions {
			if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") {
				continue
			}
			if subscription.Group.ID == 0 {
				continue
			}
			if _, ok := seen[subscription.Group.ID]; ok {
				continue
			}
			seen[subscription.Group.ID] = struct{}{}
			user.AllowedGroups = append(user.AllowedGroups, Group{
				ID:       subscription.Group.ID,
				Name:     strings.TrimSpace(subscription.Group.Name),
				Platform: strings.TrimSpace(subscription.Group.Platform),
			})
		}
	}
	return &user, nil
}

func (s *sub2apiRelay) ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]Group, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/users?page=1&page_size=200", nil)
	if err != nil {
		return nil, fmt.Errorf("relay: list allowed groups: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: list allowed groups: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		envelopeStatus
		Data struct {
			Items []struct {
				ID            int64 `json:"id"`
				Subscriptions []struct {
					Status string `json:"status"`
					Group  Group  `json:"group"`
				} `json:"subscriptions"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: list allowed groups: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("relay: list allowed groups: request failed")
	}

	for _, item := range result.Data.Items {
		if item.ID != userID {
			continue
		}
		groups := make([]Group, 0, len(item.Subscriptions))
		seen := make(map[int64]struct{}, len(item.Subscriptions))
		for _, subscription := range item.Subscriptions {
			if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") {
				continue
			}
			if subscription.Group.ID == 0 {
				continue
			}
			if _, ok := seen[subscription.Group.ID]; ok {
				continue
			}
			seen[subscription.Group.ID] = struct{}{}
			groups = append(groups, Group{
				ID:       subscription.Group.ID,
				Name:     strings.TrimSpace(subscription.Group.Name),
				Platform: strings.TrimSpace(subscription.Group.Platform),
			})
		}
		return groups, nil
	}
	return nil, nil
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by email: read body: %w", err)
	}

	users, ok, err := decodeUserLookupResponse(body)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by email: decode: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("relay: find user by email: request failed")
	}

	user := exactUserByEmail(users, email)
	if user == nil {
		return nil, nil
	}
	return user, nil
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by username: read body: %w", err)
	}

	users, ok, err := decodeUserLookupResponse(body)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by username: decode: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("relay: find user by username: request failed")
	}

	user := exactUserByUsername(users, username)
	if user == nil {
		return nil, nil
	}
	return user, nil
}

// decodeUserLookupResponse accepts both legacy list payloads and newer paginated user envelopes.
func decodeUserLookupResponse(body []byte) ([]User, bool, error) {
	var envelope struct {
		envelopeStatus
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, false, err
	}
	if !envelope.ok() {
		return nil, false, nil
	}

	users, err := decodeUserLookupData(envelope.Data)
	if err != nil {
		return nil, false, err
	}
	return users, true, nil
}

func decodeUserLookupData(data json.RawMessage) ([]User, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}

	switch data[0] {
	case '[':
		var users []User
		if err := json.Unmarshal(data, &users); err != nil {
			return nil, err
		}
		return users, nil
	case '{':
		var page struct {
			Items []User `json:"items"`
		}
		if err := json.Unmarshal(data, &page); err == nil && page.Items != nil {
			return page.Items, nil
		}

		var user User
		if err := json.Unmarshal(data, &user); err != nil {
			return nil, err
		}
		if user.ID == 0 && user.Username == "" && user.Email == "" && user.Role == "" {
			return nil, fmt.Errorf("unsupported user lookup data shape")
		}
		return []User{user}, nil
	default:
		return nil, fmt.Errorf("unsupported user lookup data shape")
	}
}

func exactUserByEmail(users []User, email string) *User {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil
	}
	for i := range users {
		if strings.EqualFold(strings.TrimSpace(users[i].Email), email) {
			return &users[i]
		}
	}
	return nil
}

func exactUserByUsername(users []User, username string) *User {
	username = strings.TrimSpace(username)
	if username == "" {
		return nil
	}
	for i := range users {
		if strings.EqualFold(strings.TrimSpace(users[i].Username), username) {
			return &users[i]
		}
	}
	if strings.Contains(username, "@") {
		return exactUserByEmail(users, username)
	}
	return nil
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
		envelopeStatus
		Data User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: create user: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("relay: create user: request failed")
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) UpdateUser(ctx context.Context, userID int64, req UpdateUserRequest) (*User, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("relay: update user: marshal: %w", err)
	}

	resp, err := s.doAdminRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/users/%d", userID), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: update user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: update user: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		envelopeStatus
		Data User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: update user: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("relay: update user: request failed")
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	req.Model = s.inferenceModel()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.inferenceAPIKey())
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("relay: chat completion: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: chat completion: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
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

// ChatCompletionForPlatform sends a small non-streaming probe using the protocol
// that sub2api exposes for the selected group platform.
func (s *sub2apiRelay) ChatCompletionForPlatform(ctx context.Context, platform string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "anthropic", "claude":
		return s.anthropicMessages(ctx, req, "")
	case "gemini":
		return s.geminiGenerateContent(ctx, req, "")
	case "antigravity":
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Model)), "gemini") ||
			strings.HasPrefix(strings.ToLower(s.inferenceModel()), "gemini") {
			return s.geminiGenerateContent(ctx, req, "/antigravity")
		}
		return s.anthropicMessages(ctx, req, "/antigravity")
	default:
		return s.ChatCompletion(ctx, req)
	}
}

func (s *sub2apiRelay) anthropicMessages(ctx context.Context, req ChatCompletionRequest, routePrefix string) (*ChatCompletionResponse, error) {
	req.Model = s.completionModel(req.Model)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("relay: messages: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.anthropicMessagesURL(routePrefix), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: messages: %w", err)
	}
	apiKey := s.inferenceAPIKey()
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("x-api-key", apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("relay: messages: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: messages: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}

	var messagesResp anthropicMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&messagesResp); err != nil {
		return nil, fmt.Errorf("relay: messages: decode: %w", err)
	}

	return &ChatCompletionResponse{
		Content:    messagesResp.contentText(),
		TokensUsed: messagesResp.Usage.InputTokens + messagesResp.Usage.OutputTokens,
	}, nil
}

func (s *sub2apiRelay) geminiGenerateContent(ctx context.Context, req ChatCompletionRequest, routePrefix string) (*ChatCompletionResponse, error) {
	model := s.completionModel(req.Model)
	prompt := firstUserMessage(req.Messages)
	if prompt == "" {
		prompt = "Hi"
	}
	maxTokens := 64
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = *req.MaxTokens
	}

	payload := geminiGenerateRequest{
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: prompt}},
			},
		},
		GenerationConfig: geminiGenerationConfig{MaxOutputTokens: maxTokens},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("relay: gemini generate content: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.geminiGenerateURL(routePrefix, model), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: gemini generate content: %w", err)
	}
	apiKey := s.inferenceAPIKey()
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	httpReq.Header.Set("x-goog-api-key", apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("relay: gemini generate content: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: gemini generate content: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}

	var geminiResp geminiGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("relay: gemini generate content: decode: %w", err)
	}

	return &ChatCompletionResponse{
		Content:    geminiResp.contentText(),
		TokensUsed: geminiResp.UsageMetadata.TotalTokenCount,
	}, nil
}

func (s *sub2apiRelay) completionModel(fallback string) string {
	if model := s.inferenceModel(); model != "" {
		return model
	}
	return strings.TrimSpace(fallback)
}

func (s *sub2apiRelay) gatewayRootURL() string {
	return strings.TrimSuffix(strings.TrimRight(s.baseURL, "/"), "/v1")
}

func (s *sub2apiRelay) anthropicMessagesURL(routePrefix string) string {
	if routePrefix == "" {
		return s.baseURL + "/messages"
	}
	return s.gatewayRootURL() + routePrefix + "/v1/messages"
}

func (s *sub2apiRelay) geminiGenerateURL(routePrefix, model string) string {
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if routePrefix == "" {
		return s.gatewayRootURL() + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
	}
	return s.gatewayRootURL() + routePrefix + "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
}

func firstUserMessage(messages []ChatMessage) string {
	for _, msg := range messages {
		if strings.EqualFold(strings.TrimSpace(msg.Role), "user") {
			return strings.TrimSpace(msg.Content)
		}
	}
	if len(messages) > 0 {
		return strings.TrimSpace(messages[0].Content)
	}
	return ""
}

func relayErrorMessageSuffix(body io.Reader) string {
	if body == nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(body, 4096))
	if err != nil {
		return ""
	}
	message := extractRelayErrorMessage(data)
	if message == "" {
		return ""
	}
	return ": " + message
}

func extractRelayErrorMessage(data []byte) string {
	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return strings.TrimSpace(string(data))
	}
	if message := strings.TrimSpace(payload.Error.Message); message != "" {
		return message
	}
	return strings.TrimSpace(payload.Message)
}

func (s *sub2apiRelay) ChatCompletionWithTools(ctx context.Context, req ChatCompletionRequest, tools []ToolDef) (*ChatCompletionWithToolsResponse, error) {
	req.Model = s.inferenceModel()

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
	httpReq.Header.Set("Authorization", "Bearer "+s.inferenceAPIKey())
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
	var all []APIKey
	for page := 1; ; page++ {
		resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/users/%d/api-keys?page=%d&page_size=100", userID, page), nil)
		if err != nil {
			return nil, fmt.Errorf("relay: list api keys: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return nil, fmt.Errorf("relay: list api keys: unexpected status %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("relay: list api keys: read body: %w", err)
		}

		var paginated struct {
			envelopeStatus
			Data struct {
				Items []APIKey `json:"items"`
				Page  int      `json:"page"`
				Pages int      `json:"pages"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &paginated); err == nil && (paginated.Data.Items != nil || paginated.Success != nil || paginated.Code != nil) {
			if !paginated.ok() {
				return nil, fmt.Errorf("relay: list api keys: request failed")
			}
			all = append(all, paginated.Data.Items...)
			if paginated.Data.Pages <= 1 || page >= paginated.Data.Pages {
				return all, nil
			}
			continue
		}

		var legacy struct {
			envelopeStatus
			Data []APIKey `json:"data"`
		}
		if err := json.Unmarshal(body, &legacy); err != nil {
			return nil, fmt.Errorf("relay: list api keys: decode: %w", err)
		}
		if !legacy.ok() {
			return nil, fmt.Errorf("relay: list api keys: request failed")
		}
		return legacy.Data, nil
	}
}

func (s *sub2apiRelay) CreateUserAPIKey(ctx context.Context, userID int64, req APIKeyCreateRequest) (*APIKeyWithSecret, error) {
	if login, password, ok := UserCredentialsFromContext(ctx); ok {
		token, user, err := s.loginSessionToken(ctx, login, password)
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

func (s *sub2apiRelay) UpdateUserAPIKeyStatus(ctx context.Context, keyID int64, status string) error {
	login, password, ok := UserCredentialsFromContext(ctx)
	if !ok {
		return fmt.Errorf("relay: update api key status: user credentials are required")
	}
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return fmt.Errorf("relay: update api key status via jwt: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"status": strings.TrimSpace(status),
	})
	if err != nil {
		return fmt.Errorf("relay: update api key status via jwt: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/api/v1/keys/%d", s.adminURL, keyID), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("relay: update api key status via jwt: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("relay: update api key status via jwt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: update api key status via jwt: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("relay: update api key status via jwt: decode: %w", err)
	}
	if result.Code != 0 {
		return fmt.Errorf("relay: update api key status via jwt: request failed")
	}
	return nil
}

func (s *sub2apiRelay) ResolveDefaultGroupID(ctx context.Context) (string, error) {
	return s.resolveDefaultGroupID(ctx, "")
}

func (s *sub2apiRelay) ResolveDefaultGroupIDForPlatform(ctx context.Context, platform string) (string, error) {
	return s.resolveDefaultGroupID(ctx, platform)
}

func (s *sub2apiRelay) ListPlatformGroups(ctx context.Context) ([]Group, error) {
	type groupItem struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Platform string `json:"platform"`
		Status   string `json:"status"`
	}
	type pageData struct {
		Items []groupItem `json:"items"`
		Page  int         `json:"page"`
		Pages int         `json:"pages"`
	}

	var groups []Group
	for page := 1; ; page++ {
		resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/groups?page=%d&page_size=200", page), nil)
		if err != nil {
			return nil, fmt.Errorf("relay: list platform groups: %w", err)
		}

		var result struct {
			Code int      `json:"code"`
			Data pageData `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("relay: list platform groups: decode: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("relay: list platform groups: unexpected status %d", resp.StatusCode)
		}
		if result.Code != 0 {
			return nil, fmt.Errorf("relay: list platform groups: request failed")
		}

		for _, item := range result.Data.Items {
			if !strings.EqualFold(strings.TrimSpace(item.Status), "active") {
				continue
			}
			if strings.TrimSpace(item.Platform) == "" {
				continue
			}
			groups = append(groups, Group{
				ID:       item.ID,
				Name:     strings.TrimSpace(item.Name),
				Platform: strings.TrimSpace(item.Platform),
			})
		}

		if result.Data.Pages <= 1 || page >= result.Data.Pages {
			break
		}
	}
	return groups, nil
}

func (s *sub2apiRelay) resolveDefaultGroupID(ctx context.Context, platform string) (string, error) {
	type groupItem struct {
		ID                 int64  `json:"id"`
		Name               string `json:"name"`
		Platform           string `json:"platform"`
		Status             string `json:"status"`
		AccountCount       int64  `json:"account_count"`
		ActiveAccountCount int64  `json:"active_account_count"`
	}
	type pageData struct {
		Items []groupItem `json:"items"`
		Page  int         `json:"page"`
		Pages int         `json:"pages"`
	}

	bestID := int64(0)
	bestAccountCount := int64(-1)
	bestActiveAccountCount := int64(-1)
	platform = strings.TrimSpace(platform)

	for page := 1; ; page++ {
		resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/groups?page=%d&page_size=200", page), nil)
		if err != nil {
			return "", fmt.Errorf("relay: resolve default group: %w", err)
		}

		var result struct {
			Code int      `json:"code"`
			Data pageData `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return "", fmt.Errorf("relay: resolve default group: decode: %w", err)
		}
		resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("relay: resolve default group: unexpected status %d", resp.StatusCode)
		}
		if result.Code != 0 {
			return "", fmt.Errorf("relay: resolve default group: request failed")
		}

		for _, item := range result.Data.Items {
			if !strings.EqualFold(strings.TrimSpace(item.Status), "active") {
				continue
			}
			if platform != "" && !strings.EqualFold(strings.TrimSpace(item.Platform), platform) {
				continue
			}
			if item.AccountCount > bestAccountCount ||
				(item.AccountCount == bestAccountCount && item.ActiveAccountCount > bestActiveAccountCount) ||
				(item.AccountCount == bestAccountCount && item.ActiveAccountCount == bestActiveAccountCount && (bestID == 0 || item.ID < bestID)) {
				bestID = item.ID
				bestAccountCount = item.AccountCount
				bestActiveAccountCount = item.ActiveAccountCount
			}
		}

		if result.Data.Pages <= 1 || page >= result.Data.Pages {
			break
		}
	}

	if bestID == 0 {
		return "", nil
	}
	return strconv.FormatInt(bestID, 10), nil
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
		envelopeStatus
		Data []UsageLog `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: list usage logs by api key: decode: %w", err)
	}
	if !result.ok() {
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

type anthropicMessagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (r anthropicMessagesResponse) contentText() string {
	parts := make([]string, 0, len(r.Content))
	for _, item := range r.Content {
		if text := strings.TrimSpace(item.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

type geminiGenerateRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig,omitempty"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiGenerationConfig struct {
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
}

type geminiGenerateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []geminiPart `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	UsageMetadata struct {
		TotalTokenCount int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (r geminiGenerateResponse) contentText() string {
	var parts []string
	for _, candidate := range r.Candidates {
		for _, part := range candidate.Content.Parts {
			if text := strings.TrimSpace(part.Text); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
}
