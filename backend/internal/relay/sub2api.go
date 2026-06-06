package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	apiKey   string // Relay API key used for both admin and inference requests.
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
func NewSub2apiProvider(httpClient *http.Client, baseURL, apiKey, model string, logger *zap.Logger) Provider {
	return &sub2apiRelay{
		client:   httpClient,
		baseURL:  normalizeInferenceBaseURL(baseURL),
		adminURL: normalizeAdminBaseURL(baseURL),
		apiKey:   strings.TrimSpace(apiKey),
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

func normalizeAdminBaseURL(raw string) string {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	return strings.TrimSuffix(raw, "/v1")
}

func (s *sub2apiRelay) Name() string { return "sub2api" }

func (s *sub2apiRelay) adminAPIKey() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return strings.TrimSpace(s.apiKey)
}

func (s *sub2apiRelay) SetAdminAPIKey(apiKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apiKey = strings.TrimSpace(apiKey)
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

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: get user: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		envelopeStatus
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: get user: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("relay: get user: request failed")
	}

	user, err := decodeUserWithFacts(result.Data)
	if err != nil {
		return nil, fmt.Errorf("relay: get user: decode user: %w", err)
	}

	if !hasUserGroupFacts(&user) {
		if listed, err := s.getUserFromAdminList(ctx, userID); err == nil && listed != nil && hasUserGroupFacts(listed) {
			user.AllowedGroups = listed.AllowedGroups
			user.AllowedGroupIDs = listed.AllowedGroupIDs
		}
	}
	return &user, nil
}

func decodeUserWithFacts(data json.RawMessage) (User, error) {
	var user User
	if err := json.Unmarshal(data, &user); err != nil {
		return User{}, err
	}
	var facts struct {
		Subscriptions []struct {
			Status string `json:"status"`
			Group  Group  `json:"group"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(data, &facts); err != nil {
		return User{}, err
	}
	if len(facts.Subscriptions) == 0 {
		return user, nil
	}

	groupsByID := make(map[int64]Group, len(user.AllowedGroups)+len(facts.Subscriptions))
	for _, group := range user.AllowedGroups {
		addGroupByID(groupsByID, group)
	}
	for _, subscription := range facts.Subscriptions {
		if !strings.EqualFold(strings.TrimSpace(subscription.Status), "active") {
			continue
		}
		if subscription.Group.ID == 0 {
			continue
		}
		addGroupByID(groupsByID, Group{
			ID:               subscription.Group.ID,
			Name:             strings.TrimSpace(subscription.Group.Name),
			Platform:         strings.TrimSpace(subscription.Group.Platform),
			IsExclusive:      subscription.Group.IsExclusive,
			SubscriptionType: strings.TrimSpace(subscription.Group.SubscriptionType),
		})
	}
	user.AllowedGroups = sortedGroups(groupsByID)
	return user, nil
}

func hasUserGroupFacts(user *User) bool {
	return user != nil && (len(user.AllowedGroups) > 0 || len(user.AllowedGroupIDs) > 0)
}

func (s *sub2apiRelay) getUserFromAdminList(ctx context.Context, userID int64) (*User, error) {
	for page := 1; ; page++ {
		resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/users?page=%d&page_size=200", page), nil)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		var result struct {
			envelopeStatus
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		if !result.ok() {
			return nil, fmt.Errorf("request failed")
		}

		items, pages, err := decodeUserListItems(result.Data)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			var identity struct {
				ID int64 `json:"id"`
			}
			if err := json.Unmarshal(item, &identity); err != nil {
				return nil, err
			}
			if identity.ID != userID {
				continue
			}
			user, err := decodeUserWithFacts(item)
			if err != nil {
				return nil, err
			}
			return &user, nil
		}
		if pages <= 1 || page >= pages {
			return nil, nil
		}
	}
}

func decodeUserListItems(data json.RawMessage) ([]json.RawMessage, int, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, 0, nil
	}
	if data[0] == '[' {
		var items []json.RawMessage
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, 0, err
		}
		return items, 1, nil
	}
	var page struct {
		Items []json.RawMessage `json:"items"`
		Pages int               `json:"pages"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, err
	}
	return page.Items, page.Pages, nil
}

func (s *sub2apiRelay) ListAllowedGroupsForUser(ctx context.Context, userID int64) ([]Group, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("relay: list allowed groups: %w", err)
	}
	if user == nil {
		return nil, nil
	}

	groupsByID := make(map[int64]Group, len(user.AllowedGroups)+len(user.AllowedGroupIDs))
	for _, group := range user.AllowedGroups {
		addGroupByID(groupsByID, group)
	}
	if len(user.AllowedGroupIDs) > 0 {
		allowedIDs := make(map[int64]struct{}, len(user.AllowedGroupIDs))
		for _, id := range user.AllowedGroupIDs {
			if id > 0 {
				allowedIDs[id] = struct{}{}
			}
		}
		if len(allowedIDs) > 0 {
			allGroups, err := s.ListPlatformGroups(ctx)
			if err != nil {
				return nil, fmt.Errorf("resolve allowed group details: %w", err)
			}
			for _, group := range allGroups {
				if _, ok := allowedIDs[group.ID]; !ok {
					continue
				}
				if isSubscriptionGroup(group) {
					if _, subscribed := groupsByID[group.ID]; !subscribed {
						continue
					}
				}
				addGroupByID(groupsByID, group)
			}
		}
	}
	return sortedGroups(groupsByID), nil
}

func addGroupByID(groups map[int64]Group, group Group) {
	if group.ID <= 0 || strings.TrimSpace(group.Platform) == "" {
		return
	}
	existing, ok := groups[group.ID]
	if !ok {
		groups[group.ID] = group
		return
	}
	if strings.TrimSpace(existing.Name) == "" {
		existing.Name = group.Name
	}
	if strings.TrimSpace(existing.Platform) == "" {
		existing.Platform = group.Platform
	}
	if !existing.IsExclusive {
		existing.IsExclusive = group.IsExclusive
	}
	if strings.TrimSpace(existing.SubscriptionType) == "" {
		existing.SubscriptionType = group.SubscriptionType
	}
	groups[group.ID] = existing
}

func sortedGroups(groups map[int64]Group) []Group {
	out := make([]Group, 0, len(groups))
	for _, group := range groups {
		out = append(out, group)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out
}

func isSubscriptionGroup(group Group) bool {
	return strings.EqualFold(strings.TrimSpace(group.SubscriptionType), "subscription")
}

func (s *sub2apiRelay) FindUserByEmail(ctx context.Context, email string) (*User, error) {
	users, ok, err := s.findUsersBySearch(ctx, email)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by email: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("relay: find user by email: request failed")
	}

	user := exactUserByEmail(users, email)
	if user != nil {
		return user, nil
	}
	user, err = s.findUserInAdminList(ctx, func(user User) bool {
		return strings.EqualFold(strings.TrimSpace(user.Email), strings.TrimSpace(email))
	})
	if err != nil {
		return nil, fmt.Errorf("relay: find user by email: fallback list: %w", err)
	}
	return user, nil
}

func (s *sub2apiRelay) FindUserByUsername(ctx context.Context, username string) (*User, error) {
	users, ok, err := s.findUsersBySearch(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("relay: find user by username: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("relay: find user by username: request failed")
	}

	user := exactUserByUsername(users, username)
	if user != nil {
		return user, nil
	}
	username = strings.TrimSpace(username)
	user, err = s.findUserInAdminList(ctx, func(user User) bool {
		if strings.EqualFold(strings.TrimSpace(user.Username), username) {
			return true
		}
		return strings.Contains(username, "@") && strings.EqualFold(strings.TrimSpace(user.Email), username)
	})
	if err != nil {
		return nil, fmt.Errorf("relay: find user by username: fallback list: %w", err)
	}
	return user, nil
}

func (s *sub2apiRelay) findUsersBySearch(ctx context.Context, search string) ([]User, bool, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/users?search="+url.QueryEscape(search), nil)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, false, fmt.Errorf("read body: %w", err)
	}

	users, ok, err := decodeUserLookupResponse(body)
	if err != nil {
		return nil, false, fmt.Errorf("decode: %w", err)
	}
	return users, ok, nil
}

func (s *sub2apiRelay) findUserInAdminList(ctx context.Context, match func(User) bool) (*User, error) {
	if match == nil {
		return nil, nil
	}
	for page := 1; ; page++ {
		resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/users?page=%d&page_size=200", page), nil)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
		}

		var result struct {
			envelopeStatus
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, err
		}
		if !result.ok() {
			return nil, fmt.Errorf("request failed")
		}

		items, pages, err := decodeUserListItems(result.Data)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			user, err := decodeUserWithFacts(item)
			if err != nil {
				return nil, err
			}
			if match(user) {
				return &user, nil
			}
		}
		if pages <= 1 || page >= pages {
			return nil, nil
		}
	}
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

	if err := s.assignDefaultSubscriptionsForUser(ctx, result.Data.ID); err != nil {
		return nil, fmt.Errorf("relay: create user: assign default subscriptions: %w", err)
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

func (s *sub2apiRelay) ListModelsForPlatform(ctx context.Context, platform string) ([]ModelOption, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.modelListURL(platform), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: list models: %w", err)
	}
	apiKey := s.inferenceAPIKey()
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("x-goog-api-key", apiKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay: list models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: list models: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	models, err := decodeModelListResponse(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("relay: list models: %w", err)
	}
	return models, nil
}

func (s *sub2apiRelay) modelListURL(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "gemini":
		return s.gatewayRootURL() + "/v1beta/models"
	case "antigravity":
		return s.gatewayRootURL() + "/antigravity/models"
	default:
		return strings.TrimRight(s.baseURL, "/") + "/models"
	}
}

type modelListItem struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	DisplayName      string `json:"display_name"`
	DisplayNameCamel string `json:"displayName"`
}

func decodeModelListResponse(body io.Reader) ([]ModelOption, error) {
	data, err := io.ReadAll(io.LimitReader(body, 2<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if payload, ok := raw["data"]; ok {
		var items []modelListItem
		if err := json.Unmarshal(payload, &items); err != nil {
			return nil, err
		}
		return normalizeModelListItems(items), nil
	}
	if payload, ok := raw["models"]; ok {
		var items []modelListItem
		if err := json.Unmarshal(payload, &items); err != nil {
			return nil, err
		}
		return normalizeModelListItems(items), nil
	}
	return nil, fmt.Errorf("unsupported response shape")
}

func normalizeModelListItems(items []modelListItem) []ModelOption {
	models := make([]ModelOption, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		id := strings.TrimPrefix(strings.TrimSpace(firstNonEmpty(item.ID, item.Name)), "models/")
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		displayName := strings.TrimSpace(firstNonEmpty(item.DisplayName, item.DisplayNameCamel, id))
		models = append(models, ModelOption{ID: id, DisplayName: displayName})
	}
	return models
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
	return relayErrorMessageSuffixFromData(data)
}

func relayErrorMessageSuffixFromData(data []byte) string {
	message := extractRelayErrorMessage(data)
	if message == "" {
		return ""
	}
	return ": " + message
}

func extractRelayErrorMessage(data []byte) string {
	var payload struct {
		Error struct {
			Message  string         `json:"message"`
			Metadata map[string]any `json:"metadata"`
		} `json:"error"`
		Message  string         `json:"message"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return strings.TrimSpace(string(data))
	}
	message := strings.TrimSpace(firstNonEmpty(payload.Error.Message, payload.Message))
	detail := relayErrorMetadataDetail(payload.Error.Metadata, payload.Metadata)
	if message != "" && detail != "" {
		return message + " (" + detail + ")"
	}
	if message != "" {
		return message
	}
	return detail
}

func relayErrorMetadataDetail(maps ...map[string]any) string {
	for _, metadata := range maps {
		if len(metadata) == 0 {
			continue
		}
		if reason := metadataString(metadata, "conflict_reason"); reason != "" {
			return "conflict_reason: " + reason
		}
	}
	return ""
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key]
	if !ok || value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprint(typed))
	}
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
	return nil, fmt.Errorf("relay: create api key: user credentials are required")
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
		return nil, fmt.Errorf("relay: create api key via jwt: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
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
		ID               int64  `json:"id"`
		Name             string `json:"name"`
		Platform         string `json:"platform"`
		Status           string `json:"status"`
		IsExclusive      bool   `json:"is_exclusive"`
		SubscriptionType string `json:"subscription_type"`
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
				ID:               item.ID,
				Name:             strings.TrimSpace(item.Name),
				Platform:         strings.TrimSpace(item.Platform),
				IsExclusive:      item.IsExclusive,
				SubscriptionType: strings.TrimSpace(item.SubscriptionType),
			})
		}

		if result.Data.Pages <= 1 || page >= result.Data.Pages {
			break
		}
	}
	return groups, nil
}

type defaultSubscriptionSetting struct {
	GroupID      int64 `json:"group_id"`
	ValidityDays int   `json:"validity_days"`
}

type subscriptionAssignment struct {
	GroupID      int64
	ValidityDays int
	Notes        string
}

type sub2apiUserSubscription struct {
	ID      int64  `json:"id"`
	UserID  int64  `json:"user_id"`
	GroupID int64  `json:"group_id"`
	Status  string `json:"status"`
	Group   *Group `json:"group"`
}

func (s *sub2apiRelay) assignDefaultSubscriptionsForUser(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return nil
	}
	defaults, err := s.listDefaultSubscriptions(ctx)
	if err != nil {
		return err
	}
	for _, item := range defaults {
		if item.GroupID <= 0 {
			continue
		}
		if err := s.assignSubscription(ctx, userID, subscriptionAssignment{
			GroupID:      item.GroupID,
			ValidityDays: item.ValidityDays,
			Notes:        "auto assigned by ai-efficiency relay provisioning",
		}); err != nil {
			return err
		}
	}
	return nil
}

// AssignDefaultSubscriptionsForUser applies relay-configured default subscriptions to an existing user.
func (s *sub2apiRelay) AssignDefaultSubscriptionsForUser(ctx context.Context, userID int64) error {
	return s.assignDefaultSubscriptionsForUser(ctx, userID)
}

// AssignSubscriptionForUser assigns one subscription group to a relay user.
func (s *sub2apiRelay) AssignSubscriptionForUser(ctx context.Context, userID, groupID int64, validityDays int) error {
	if userID <= 0 {
		return fmt.Errorf("assign subscription: user id is required")
	}
	if groupID <= 0 {
		return fmt.Errorf("assign subscription: group id is required")
	}
	if validityDays <= 0 {
		return fmt.Errorf("assign subscription: validity days is required")
	}
	return s.assignSubscription(ctx, userID, subscriptionAssignment{
		GroupID:      groupID,
		ValidityDays: validityDays,
		Notes:        "assigned by ai-efficiency admin",
	})
}

// ExtendSubscriptionForUser extends an existing subscription group for a relay user.
func (s *sub2apiRelay) ExtendSubscriptionForUser(ctx context.Context, userID, groupID int64, days int) error {
	if userID <= 0 {
		return fmt.Errorf("extend subscription: user id is required")
	}
	if groupID <= 0 {
		return fmt.Errorf("extend subscription: group id is required")
	}
	if days <= 0 {
		return fmt.Errorf("extend subscription: days is required")
	}
	subscription, err := s.findSubscriptionForUserGroup(ctx, userID, groupID)
	if err != nil {
		return fmt.Errorf("extend subscription: %w", err)
	}

	payload, err := json.Marshal(map[string]any{"days": days})
	if err != nil {
		return fmt.Errorf("extend subscription: marshal: %w", err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/subscriptions/%d/extend", subscription.ID), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("extend subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("extend subscription: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result envelopeStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("extend subscription: decode: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("extend subscription: request failed")
	}
	return nil
}

// RemoveSubscriptionForUser revokes an existing subscription group for a relay user.
func (s *sub2apiRelay) RemoveSubscriptionForUser(ctx context.Context, userID, groupID int64) error {
	if userID <= 0 {
		return fmt.Errorf("remove subscription: user id is required")
	}
	if groupID <= 0 {
		return fmt.Errorf("remove subscription: group id is required")
	}
	subscription, err := s.findSubscriptionForUserGroup(ctx, userID, groupID)
	if err != nil {
		return fmt.Errorf("remove subscription: %w", err)
	}

	resp, err := s.doAdminRequest(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/admin/subscriptions/%d", subscription.ID), nil)
	if err != nil {
		return fmt.Errorf("remove subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("remove subscription: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result envelopeStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("remove subscription: decode: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("remove subscription: request failed")
	}
	return nil
}

func (s *sub2apiRelay) listDefaultSubscriptions(ctx context.Context) ([]defaultSubscriptionSetting, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/settings", nil)
	if err != nil {
		return nil, fmt.Errorf("list default subscriptions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list default subscriptions: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		envelopeStatus
		Data struct {
			DefaultSubscriptions []defaultSubscriptionSetting `json:"default_subscriptions"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list default subscriptions: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("list default subscriptions: request failed")
	}
	return result.Data.DefaultSubscriptions, nil
}

func (s *sub2apiRelay) findSubscriptionForUserGroup(ctx context.Context, userID, groupID int64) (*sub2apiUserSubscription, error) {
	subscriptions, err := s.listUserSubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range subscriptions {
		if subscriptions[i].GroupID == 0 && subscriptions[i].Group != nil {
			subscriptions[i].GroupID = subscriptions[i].Group.ID
		}
		if subscriptions[i].GroupID == groupID {
			return &subscriptions[i], nil
		}
	}
	return nil, fmt.Errorf("subscription not found for user %d group %d", userID, groupID)
}

func (s *sub2apiRelay) listUserSubscriptions(ctx context.Context, userID int64) ([]sub2apiUserSubscription, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/users/%d/subscriptions", userID), nil)
	if err != nil {
		return nil, fmt.Errorf("list user subscriptions: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list user subscriptions: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result struct {
		envelopeStatus
		Data []sub2apiUserSubscription `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("list user subscriptions: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("list user subscriptions: request failed")
	}
	return result.Data, nil
}

func (s *sub2apiRelay) assignSubscription(ctx context.Context, userID int64, item subscriptionAssignment) error {
	payload, err := json.Marshal(map[string]any{
		"user_id":       userID,
		"group_id":      item.GroupID,
		"validity_days": item.ValidityDays,
		"notes":         item.Notes,
	})
	if err != nil {
		return fmt.Errorf("assign subscription: marshal: %w", err)
	}

	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/admin/subscriptions/assign", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("assign subscription: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if readErr != nil {
			return fmt.Errorf("assign subscription: unexpected status %d", resp.StatusCode)
		}
		if isExistingSubscriptionAlreadyAssigned(resp.StatusCode, body) {
			return nil
		}
		return fmt.Errorf("assign subscription: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffixFromData(body))
	}
	var result envelopeStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("assign subscription: decode: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("assign subscription: request failed")
	}
	return nil
}

func isExistingSubscriptionAlreadyAssigned(statusCode int, body []byte) bool {
	if statusCode != http.StatusConflict {
		return false
	}
	var payload struct {
		Reason  string `json:"reason"`
		Message string `json:"message"`
		Error   struct {
			Reason  string `json:"reason"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil {
		reason := strings.ToUpper(strings.TrimSpace(firstNonEmpty(payload.Reason, payload.Error.Reason)))
		switch reason {
		case "SUBSCRIPTION_ALREADY_EXISTS":
			return true
		case "SUBSCRIPTION_ASSIGN_CONFLICT":
			return false
		}
		if reason != "" {
			return false
		}
		message := strings.ToLower(strings.TrimSpace(firstNonEmpty(payload.Message, payload.Error.Message)))
		if strings.Contains(message, "subscription already exists") || strings.Contains(message, "already has subscription") {
			return true
		}
	}
	bodyText := strings.ToLower(strings.TrimSpace(string(body)))
	return strings.Contains(bodyText, "subscription already exists") || strings.Contains(bodyText, "already has subscription")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func (s *sub2apiRelay) GetUserUsageStats(ctx context.Context, login, password string) (*UserUsageStats, error) {
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, fmt.Errorf("relay: login for usage stats: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.adminURL+"/api/v1/usage/dashboard/stats", nil)
	if err != nil {
		return nil, fmt.Errorf("relay: create usage stats request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay: fetch usage stats: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: usage stats: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code int            `json:"code"`
		Data UserUsageStats `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: decode usage stats: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("relay: usage stats: code %d", result.Code)
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) GetUserUsageTrend(ctx context.Context, login, password string, params UsageTrendParams) (*UsageTrendResponse, error) {
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, fmt.Errorf("relay: login for usage trend: %w", err)
	}

	u, err := url.Parse(s.adminURL + "/api/v1/usage/dashboard/trend")
	if err != nil {
		return nil, fmt.Errorf("relay: parse usage trend url: %w", err)
	}
	q := u.Query()
	if params.StartDate != "" {
		q.Set("start_date", params.StartDate)
	}
	if params.EndDate != "" {
		q.Set("end_date", params.EndDate)
	}
	if params.Granularity != "" {
		q.Set("granularity", params.Granularity)
	}
	if params.Timezone != "" {
		q.Set("timezone", params.Timezone)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: create usage trend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay: fetch usage trend: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: usage trend: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code int                `json:"code"`
		Data UsageTrendResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: decode usage trend: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("relay: usage trend: code %d", result.Code)
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) GetUserUsageModels(ctx context.Context, login, password string, params UsageModelParams) (*UsageModelResponse, error) {
	token, _, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, fmt.Errorf("relay: login for usage models: %w", err)
	}

	u, err := url.Parse(s.adminURL + "/api/v1/usage/dashboard/models")
	if err != nil {
		return nil, fmt.Errorf("relay: parse usage models url: %w", err)
	}
	q := u.Query()
	if params.StartDate != "" {
		q.Set("start_date", params.StartDate)
	}
	if params.EndDate != "" {
		q.Set("end_date", params.EndDate)
	}
	if params.Timezone != "" {
		q.Set("timezone", params.Timezone)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: create usage models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay: fetch usage models: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: usage models: unexpected status %d", resp.StatusCode)
	}

	var result struct {
		Code int                `json:"code"`
		Data UsageModelResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: decode usage models: %w", err)
	}
	if result.Code != 0 {
		return nil, fmt.Errorf("relay: usage models: code %d", result.Code)
	}

	return &result.Data, nil
}
