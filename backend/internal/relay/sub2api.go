package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const maxPingResponseBodyBytes int64 = 4 * 1024

const (
	providerDirectoryPageSize      = 1000
	providerDirectoryUserLimit     = 5000
	providerDirectoryResponseLimit = 16 << 20
	providerCurrentStatsChunkLimit = 500
	providerCurrentStatsBodyLimit  = 2 << 20
)

type sub2apiRelay struct {
	mu       sync.RWMutex
	client   *http.Client
	baseURL  string // LLM API endpoint, e.g. http://localhost:3000/v1
	adminURL string // Admin API endpoint, e.g. http://localhost:3000
	apiKey   string // Relay API key used for both admin and inference requests.
	model    string
	logger   *zap.Logger

	providerWideTrendPointLimit int
}

const userUsageOriginTimeout = 12 * time.Second

type envelopeStatus struct {
	Success *bool  `json:"success"`
	Code    *int   `json:"code"`
	Message string `json:"message"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
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

func (s envelopeStatus) messageSuffix() string {
	messages := make([]string, 0, 2)
	for _, message := range []string{s.Message, s.Error.Message} {
		message = strings.TrimSpace(message)
		if message == "" {
			continue
		}
		if len(messages) > 0 && messages[len(messages)-1] == message {
			continue
		}
		messages = append(messages, message)
	}
	if len(messages) == 0 {
		return ""
	}
	return ": " + strings.Join(messages, ": ")
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
	normalized := strings.TrimSpace(apiKey)
	s.mu.Lock()
	s.apiKey = normalized
	s.mu.Unlock()
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
	if _, err := io.CopyN(io.Discard, resp.Body, maxPingResponseBodyBytes+1); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("relay: ping: read body: %w", err)
	}
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
			ID:                    subscription.Group.ID,
			Name:                  strings.TrimSpace(subscription.Group.Name),
			Platform:              strings.TrimSpace(subscription.Group.Platform),
			IsExclusive:           subscription.Group.IsExclusive,
			SubscriptionType:      strings.TrimSpace(subscription.Group.SubscriptionType),
			AllowMessagesDispatch: subscription.Group.AllowMessagesDispatch,
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
	if !existing.AllowMessagesDispatch {
		existing.AllowMessagesDispatch = group.AllowMessagesDispatch
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

func (s *sub2apiRelay) ListUsers(ctx context.Context) ([]User, error) {
	users, err := s.listUsersFromAdminList(ctx)
	if err != nil {
		return nil, fmt.Errorf("relay: list users: %w", err)
	}
	return users, nil
}

type providerDirectoryItem struct {
	ID int64 `json:"id"`
}

type providerDirectoryPage struct {
	Items    []providerDirectoryItem `json:"items"`
	Page     *int                    `json:"page"`
	PageSize *int                    `json:"page_size"`
	Pages    *int                    `json:"pages"`
	Total    *int                    `json:"total"`
}

type providerDirectoryEnvelope struct {
	envelopeStatus
	Data *providerDirectoryPage `json:"data"`
}

func (s *sub2apiRelay) GetProviderUserIDs(ctx context.Context) (ProviderDirectoryResult, error) {
	var result ProviderDirectoryResult
	var declaredPages, declaredTotal int
	var previousID int64

	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", strconv.Itoa(providerDirectoryPageSize))
		query.Set("include_subscriptions", "false")
		query.Set("sort_by", "id")
		query.Set("sort_order", "asc")

		resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/users?"+query.Encode(), nil)
		if err != nil {
			return ProviderDirectoryResult{}, fmt.Errorf("relay: provider directory: fetch page %d: %w", page, err)
		}
		body, readErr := readBodyStrictlyBelow(resp.Body, providerDirectoryResponseLimit)
		resp.Body.Close()
		if readErr != nil {
			return ProviderDirectoryResult{}, fmt.Errorf("relay: provider directory: read page %d: %w", page, readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return ProviderDirectoryResult{}, fmt.Errorf("relay: provider directory: unexpected status %d", resp.StatusCode)
		}

		var envelope providerDirectoryEnvelope
		if err := decodeSingleJSON(body, &envelope); err != nil {
			return ProviderDirectoryResult{}, fmt.Errorf("relay: provider directory: decode page %d: %w", page, err)
		}
		if !envelope.ok() {
			return ProviderDirectoryResult{}, fmt.Errorf("relay: provider directory: request failed")
		}
		if envelope.Data == nil {
			return ProviderDirectoryResult{}, fmt.Errorf("relay: provider directory: page %d missing data", page)
		}

		data := envelope.Data
		if data.Page == nil || *data.Page != page {
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: response page does not match request page %d", page))
		}
		if data.PageSize != nil && *data.PageSize != providerDirectoryPageSize {
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: page %d has invalid page-size metadata", page))
		}
		if len(data.Items) > providerDirectoryPageSize {
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionProviderIDBound, fmt.Errorf("relay: provider directory: page %d exceeds item limit", page))
		}
		if data.Pages == nil || data.Total == nil {
			if page == 1 && len(data.Items) == 0 && data.Pages == nil && data.Total == nil {
				result.ResponseBytes = int64(len(body))
				result.PageCount = 1
				return result, nil
			}
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: page %d missing authoritative pagination", page))
		}
		if *data.Pages < 0 || *data.Total < 0 || (len(data.Items) > 0 && (*data.Pages == 0 || *data.Total == 0)) {
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: page %d has invalid authoritative pagination", page))
		}

		if page == 1 {
			declaredPages, declaredTotal = *data.Pages, *data.Total
			if declaredTotal == 0 {
				if len(data.Items) != 0 || declaredPages > 1 {
					return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: empty roster has inconsistent pagination"))
				}
				result.ResponseBytes = int64(len(body))
				result.PageCount = 1
				return result, nil
			}
		} else if *data.Pages != declaredPages || *data.Total != declaredTotal {
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: page %d changed authoritative pagination", page))
		}
		if declaredPages < page {
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: page %d exceeds declared pages", page))
		}
		if page < declaredPages && len(data.Items) == 0 {
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: page %d is empty before final page", page))
		}

		for itemIndex, item := range data.Items {
			if item.ID <= 0 {
				return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionProviderIDBound, fmt.Errorf("relay: provider directory: page %d item %d has invalid ID", page, itemIndex))
			}
			if item.ID <= previousID {
				return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionProviderIDBound, fmt.Errorf("relay: provider directory: page %d item %d is not strictly ascending", page, itemIndex))
			}
			previousID = item.ID
			result.UserIDs = append(result.UserIDs, item.ID)
			if len(result.UserIDs) >= providerDirectoryUserLimit {
				return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionProviderIDBound, fmt.Errorf("relay: provider directory: user count reached limit %d", providerDirectoryUserLimit))
			}
		}
		result.ResponseBytes += int64(len(body))
		result.PageCount++
		if len(result.UserIDs) > declaredTotal {
			return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: cumulative count exceeds authoritative total"))
		}
		if page == declaredPages {
			if len(result.UserIDs) != declaredTotal {
				return ProviderDirectoryResult{}, NewProviderSourceRejection(ProviderSourceRejectionDirectoryPagination, fmt.Errorf("relay: provider directory: final count does not match authoritative total"))
			}
			return result, nil
		}
	}
}

func (s *sub2apiRelay) GetProviderCurrentUsageStats(ctx context.Context, userIDs []int64) (ProviderCurrentStatsResult, error) {
	requested := make(map[int64]struct{}, len(userIDs))
	if len(userIDs) == 0 || len(userIDs) > providerCurrentStatsChunkLimit {
		return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: requested ID count must be between 1 and %d", providerCurrentStatsChunkLimit)
	}
	for index, userID := range userIDs {
		if userID <= 0 {
			return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: requested ID at index %d is invalid", index)
		}
		if _, exists := requested[userID]; exists {
			return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: requested IDs are not unique")
		}
		requested[userID] = struct{}{}
	}

	payload, err := json.Marshal(struct {
		UserIDs []int64 `json:"user_ids"`
	}{UserIDs: userIDs})
	if err != nil {
		return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: marshal: %w", err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/admin/dashboard/users-usage", bytes.NewReader(payload))
	if err != nil {
		return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: fetch: %w", err)
	}
	body, readErr := readBodyStrictlyBelow(resp.Body, providerCurrentStatsBodyLimit)
	resp.Body.Close()
	if readErr != nil {
		return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: read: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: unexpected status %d", resp.StatusCode)
	}

	var envelope struct {
		envelopeStatus
		Data *struct {
			Stats json.RawMessage `json:"stats"`
		} `json:"data"`
	}
	if err := decodeSingleJSON(body, &envelope); err != nil {
		return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: decode envelope: %w", err)
	}
	if !envelope.ok() {
		return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: request failed")
	}
	if envelope.Data == nil || len(envelope.Data.Stats) == 0 {
		return ProviderCurrentStatsResult{}, fmt.Errorf("relay: provider current stats: missing stats data")
	}
	stats, err := decodeExactProviderCurrentStats(envelope.Data.Stats, requested)
	if err != nil {
		return ProviderCurrentStatsResult{}, NewProviderSourceRejection(ProviderSourceRejectionStatsExactCoverage, fmt.Errorf("relay: provider current stats: decode stats: %w", err))
	}
	return ProviderCurrentStatsResult{Stats: stats, ResponseBytes: int64(len(body))}, nil
}

func decodeExactProviderCurrentStats(raw json.RawMessage, requested map[int64]struct{}) (map[int64]TeamUserUsageStats, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("stats value must be an object")
	}

	stats := make(map[int64]TeamUserUsageStats, len(requested))
	rawKeys := make(map[string]struct{}, len(requested))
	recordIndex := 0
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		rawKey, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("record %d has invalid object key", recordIndex)
		}
		if _, exists := rawKeys[rawKey]; exists {
			return nil, fmt.Errorf("record %d repeats an object key", recordIndex)
		}
		rawKeys[rawKey] = struct{}{}

		userID, err := strconv.ParseInt(rawKey, 10, 64)
		if err != nil || userID <= 0 || strconv.FormatInt(userID, 10) != rawKey {
			return nil, fmt.Errorf("record %d has invalid object key", recordIndex)
		}
		if _, exists := requested[userID]; !exists {
			return nil, fmt.Errorf("record %d is outside requested coverage", recordIndex)
		}
		if _, exists := stats[userID]; exists {
			return nil, fmt.Errorf("record %d repeats a decoded ID", recordIndex)
		}

		var item TeamUserUsageStats
		if err := decoder.Decode(&item); err != nil {
			return nil, fmt.Errorf("decode record %d: %w", recordIndex, err)
		}
		if item.UserID != userID {
			return nil, fmt.Errorf("record %d embedded ID does not match object key", recordIndex)
		}
		if err := validateProviderCurrentStat(item); err != nil {
			return nil, fmt.Errorf("record %d: %w", recordIndex, err)
		}
		stats[userID] = item
		recordIndex++
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON")
		}
		return nil, err
	}
	if len(stats) != len(requested) {
		return nil, fmt.Errorf("record count does not match requested coverage")
	}
	return stats, nil
}

func validateProviderCurrentStat(item TeamUserUsageStats) error {
	if item.TodayActualCost < 0 || math.IsNaN(item.TodayActualCost) || math.IsInf(item.TodayActualCost, 0) {
		return fmt.Errorf("today actual cost must be finite and non-negative")
	}
	if item.TotalActualCost < 0 || math.IsNaN(item.TotalActualCost) || math.IsInf(item.TotalActualCost, 0) {
		return fmt.Errorf("total actual cost must be finite and non-negative")
	}
	if item.TotalTokens != nil && *item.TotalTokens < 0 {
		return fmt.Errorf("total tokens must be non-negative")
	}
	if item.RangeActualCost != nil && (*item.RangeActualCost < 0 || math.IsNaN(*item.RangeActualCost) || math.IsInf(*item.RangeActualCost, 0)) {
		return fmt.Errorf("range actual cost must be finite and non-negative")
	}
	if item.RangeTotalTokens != nil && *item.RangeTotalTokens < 0 {
		return fmt.Errorf("range total tokens must be non-negative")
	}
	return nil
}

type responseBodyLimitError struct {
	limit int64
}

func (e *responseBodyLimitError) Error() string {
	return fmt.Sprintf("response body reached %d-byte limit", e.limit)
}

func readBodyStrictlyBelow(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) >= limit {
		return nil, &responseBodyLimitError{limit: limit}
	}
	return body, nil
}

func decodeSingleJSON(body []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON")
		}
		return err
	}
	return nil
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

func (s *sub2apiRelay) listUsersFromAdminList(ctx context.Context) ([]User, error) {
	var users []User
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
			users = append(users, user)
		}
		if pages <= 1 || page >= pages {
			return users, nil
		}
	}
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
		return nil, fmt.Errorf("relay: update user: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}

	var result struct {
		envelopeStatus
		Data User `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: update user: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("relay: update user: request failed%s", result.envelopeStatus.messageSuffix())
	}

	return &result.Data, nil
}

func (s *sub2apiRelay) DisableUser(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return fmt.Errorf("relay: disable user: user id is required")
	}
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return fmt.Errorf("relay: disable user: %w", err)
	}
	if user == nil {
		return fmt.Errorf("relay: disable user: user not found")
	}
	if strings.EqualFold(strings.TrimSpace(user.Role), "admin") {
		return fmt.Errorf("relay: disable user: cannot disable admin relay user")
	}
	if _, err := s.UpdateUser(ctx, userID, UpdateUserRequest{Status: "disabled"}); err != nil {
		return fmt.Errorf("relay: disable user: %w", err)
	}
	return nil
}

func (s *sub2apiRelay) ChatCompletion(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	return s.chatCompletion(ctx, req, relayErrorMessageSuffix, false)
}

func (s *sub2apiRelay) chatCompletion(ctx context.Context, req ChatCompletionRequest, errorSuffix func(io.Reader) string, requireTerminal bool) (*ChatCompletionResponse, error) {
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
		return nil, fmt.Errorf("relay: chat completion: unexpected status %d%s", resp.StatusCode, errorSuffix(resp.Body))
	}

	var openAIResp openAIChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("relay: chat completion: decode: %w", err)
	}
	if requireTerminal && (len(openAIResp.Choices) == 0 || strings.TrimSpace(openAIResp.Choices[0].FinishReason) == "") {
		return nil, fmt.Errorf("relay: chat completion: missing terminal finish_reason")
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

func (s *sub2apiRelay) CompletionForProtocol(ctx context.Context, platform, protocol string, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	stream := false
	req.Stream = &stream
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case ProtocolResponses:
		return s.openAIResponses(ctx, req)
	case ProtocolChatCompletions:
		return s.chatCompletion(ctx, req, relayProbeErrorMessageSuffix, true)
	case ProtocolMessages:
		routePrefix := ""
		if strings.EqualFold(strings.TrimSpace(platform), "antigravity") {
			routePrefix = "/antigravity"
		}
		return s.anthropicMessages(ctx, req, routePrefix)
	case ProtocolGenerateContent:
		return s.geminiGenerateContent(ctx, req, "")
	case ProtocolAntigravityGenerateContent:
		return s.geminiGenerateContent(ctx, req, "/antigravity")
	default:
		return nil, fmt.Errorf("relay: unsupported completion protocol %q", protocol)
	}
}

func (s *sub2apiRelay) openAIResponses(ctx context.Context, req ChatCompletionRequest) (*ChatCompletionResponse, error) {
	prompt := firstUserMessage(req.Messages)
	if prompt == "" {
		prompt = "Hi"
	}
	payload := openAIResponsesRequest{
		Model: s.completionModel(req.Model),
		Input: []openAIResponsesInput{{
			Role: "user",
			Content: []openAIResponsesContent{{
				Type: "input_text",
				Text: prompt,
			}},
		}},
		MaxOutputTokens: req.MaxTokens,
		Stream:          false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("relay: responses: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("relay: responses: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+s.inferenceAPIKey())
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("relay: responses: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: responses: unexpected status %d%s", resp.StatusCode, relayProbeErrorMessageSuffix(resp.Body))
	}

	var responsesResp openAIResponsesResponse
	if err := json.NewDecoder(resp.Body).Decode(&responsesResp); err != nil {
		return nil, fmt.Errorf("relay: responses: decode: %w", err)
	}
	if responsesResp.Status != "completed" {
		return nil, fmt.Errorf("relay: responses: expected terminal status completed, got %q", responsesResp.Status)
	}
	return &ChatCompletionResponse{
		Content:    responsesResp.contentText(),
		TokensUsed: responsesResp.Usage.TotalTokens,
	}, nil
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
		return nil, fmt.Errorf("relay: messages: unexpected status %d%s", resp.StatusCode, relayProbeErrorMessageSuffix(resp.Body))
	}

	var messagesResp anthropicMessagesResponse
	if err := json.NewDecoder(resp.Body).Decode(&messagesResp); err != nil {
		return nil, fmt.Errorf("relay: messages: decode: %w", err)
	}
	if strings.TrimSpace(messagesResp.StopReason) == "" {
		return nil, fmt.Errorf("relay: messages: missing terminal stop_reason")
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
		return nil, fmt.Errorf("relay: gemini generate content: unexpected status %d%s", resp.StatusCode, relayProbeErrorMessageSuffix(resp.Body))
	}

	var geminiResp geminiGenerateResponse
	if err := json.NewDecoder(resp.Body).Decode(&geminiResp); err != nil {
		return nil, fmt.Errorf("relay: gemini generate content: decode: %w", err)
	}
	if !geminiResp.hasTerminalCandidate() {
		return nil, fmt.Errorf("relay: gemini generate content: missing terminal finishReason")
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

func relayProbeErrorMessageSuffix(body io.Reader) string {
	if body == nil {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil {
		return ""
	}
	message := strings.TrimSpace(string(data))
	if message == "" {
		return ""
	}
	return ": " + message
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
		ID                    int64  `json:"id"`
		Name                  string `json:"name"`
		Platform              string `json:"platform"`
		Status                string `json:"status"`
		IsExclusive           bool   `json:"is_exclusive"`
		SubscriptionType      string `json:"subscription_type"`
		AllowMessagesDispatch bool   `json:"allow_messages_dispatch"`
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
				ID:                    item.ID,
				Name:                  strings.TrimSpace(item.Name),
				Platform:              strings.TrimSpace(item.Platform),
				IsExclusive:           item.IsExclusive,
				SubscriptionType:      strings.TrimSpace(item.SubscriptionType),
				AllowMessagesDispatch: item.AllowMessagesDispatch,
			})
		}

		if result.Data.Pages <= 1 || page >= result.Data.Pages {
			break
		}
	}
	return groups, nil
}

// DuplicateGroup creates an inactive copy of a group through sub2api's
// idempotent admin endpoint. The operation key is supplied by the caller so a
// retried planning execution cannot create a second copy.
func (s *sub2apiRelay) DuplicateGroup(ctx context.Context, sourceGroupID int64, operationKey string) (*Group, error) {
	if sourceGroupID <= 0 {
		return nil, fmt.Errorf("relay: duplicate group: source group id is required")
	}
	if strings.TrimSpace(operationKey) == "" {
		return nil, fmt.Errorf("relay: duplicate group: operation key is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.adminURL+fmt.Sprintf("/api/v1/admin/groups/%d/duplicate", sourceGroupID), nil)
	if err != nil {
		return nil, fmt.Errorf("relay: duplicate group: %w", err)
	}
	req.Header.Set("X-API-Key", s.adminAPIKey())
	req.Header.Set("Idempotency-Key", strings.TrimSpace(operationKey))
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("relay: duplicate group: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: duplicate group: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result struct {
		envelopeStatus
		Data Group `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: duplicate group: decode: %w", err)
	}
	if !result.ok() || result.Data.ID <= 0 {
		return nil, fmt.Errorf("relay: duplicate group: request failed")
	}
	return &result.Data, nil
}

func (s *sub2apiRelay) UpdateGroupStatus(ctx context.Context, groupID int64, status string) error {
	if groupID <= 0 {
		return fmt.Errorf("relay: update group status: group id is required")
	}
	status = strings.ToLower(strings.TrimSpace(status))
	if status != "active" && status != "inactive" {
		return fmt.Errorf("relay: update group status: unsupported status %q", status)
	}
	payload, err := json.Marshal(map[string]string{"status": status})
	if err != nil {
		return fmt.Errorf("relay: update group status: marshal: %w", err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/groups/%d", groupID), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("relay: update group status: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: update group status: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result envelopeStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("relay: update group status: decode: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("relay: update group status: request failed")
	}
	return nil
}

func (s *sub2apiRelay) BindAPIKeyToGroup(ctx context.Context, keyID, groupID int64) error {
	if keyID <= 0 || groupID <= 0 {
		return fmt.Errorf("relay: bind api key: key and group ids are required")
	}
	payload, err := json.Marshal(map[string]int64{"group_id": groupID})
	if err != nil {
		return fmt.Errorf("relay: bind api key: marshal: %w", err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/api-keys/%d", keyID), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("relay: bind api key: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: bind api key: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result envelopeStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("relay: bind api key: decode: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("relay: bind api key: request failed")
	}
	return nil
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
	ID              int64      `json:"id"`
	UserID          int64      `json:"user_id"`
	GroupID         int64      `json:"group_id"`
	Status          string     `json:"status"`
	DailyUsageUSD   float64    `json:"daily_usage_usd"`
	WeeklyUsageUSD  float64    `json:"weekly_usage_usd"`
	MonthlyUsageUSD float64    `json:"monthly_usage_usd"`
	DailyResetAt    *time.Time `json:"daily_reset_at,omitempty"`
	WeeklyResetAt   *time.Time `json:"weekly_reset_at,omitempty"`
	MonthlyResetAt  *time.Time `json:"monthly_reset_at,omitempty"`
	Group           *Group     `json:"group"`
}

type sub2apiUsageWindowProgress struct {
	ResetsAt *time.Time `json:"resets_at"`
}

type sub2apiSubscriptionProgress struct {
	Subscription sub2apiUserSubscription `json:"subscription"`
	Progress     *struct {
		Daily   *sub2apiUsageWindowProgress `json:"daily"`
		Weekly  *sub2apiUsageWindowProgress `json:"weekly"`
		Monthly *sub2apiUsageWindowProgress `json:"monthly"`
	} `json:"progress"`
}

type sub2apiAccountSummary struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Platform      string  `json:"platform"`
	Type          string  `json:"type"`
	Status        string  `json:"status"`
	Schedulable   bool    `json:"schedulable"`
	GroupIDs      []int64 `json:"group_ids"`
	AccountGroups []struct {
		GroupID  int64 `json:"group_id"`
		Priority int   `json:"priority"`
	} `json:"account_groups"`
}

type sub2apiUsageProgress struct {
	Utilization float64    `json:"utilization"`
	ResetsAt    *time.Time `json:"resets_at"`
}

type sub2apiUsageInfo struct {
	UpdatedAt *time.Time            `json:"updated_at"`
	SevenDay  *sub2apiUsageProgress `json:"seven_day"`
}

func (s *sub2apiRelay) ListUserSubscriptions(ctx context.Context, userID int64) ([]UserSubscription, error) {
	items, err := s.listUserSubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]UserSubscription, 0, len(items))
	for _, item := range items {
		out = append(out, userSubscriptionFromSub2API(item))
	}
	return out, nil
}

func userSubscriptionFromSub2API(item sub2apiUserSubscription) UserSubscription {
	return UserSubscription{
		ID:              item.ID,
		UserID:          item.UserID,
		GroupID:         item.GroupID,
		Status:          item.Status,
		DailyUsageUSD:   item.DailyUsageUSD,
		WeeklyUsageUSD:  item.WeeklyUsageUSD,
		MonthlyUsageUSD: item.MonthlyUsageUSD,
		DailyResetAt:    item.DailyResetAt,
		WeeklyResetAt:   item.WeeklyResetAt,
		MonthlyResetAt:  item.MonthlyResetAt,
		Group:           item.Group,
	}
}

func (s *sub2apiRelay) listUserSubscriptionsWithProgress(ctx context.Context, login, password string, userID int64) ([]UserSubscription, error) {
	token, authenticatedUser, err := s.loginSessionToken(ctx, login, password)
	if err != nil {
		return nil, err
	}
	if authenticatedUser == nil || authenticatedUser.ID != userID {
		return nil, fmt.Errorf("relay: subscription progress user mismatch")
	}
	return s.listUserSubscriptionsWithProgressToken(ctx, token, userID)
}

func (s *sub2apiRelay) listUserSubscriptionsWithProgressToken(ctx context.Context, token string, userID int64) ([]UserSubscription, error) {
	var items []sub2apiSubscriptionProgress
	if err := s.getUserDashboardJSON(ctx, token, "/api/v1/subscriptions/progress", nil, &items); err != nil {
		return nil, fmt.Errorf("relay: list user subscriptions with progress: %w", err)
	}

	progressByID := make(map[int64]UserSubscription, len(items))
	for _, item := range items {
		subscription := userSubscriptionFromSub2API(item.Subscription)
		if subscription.ID <= 0 {
			continue
		}
		if item.Progress != nil {
			if item.Progress.Daily != nil {
				subscription.DailyResetAt = item.Progress.Daily.ResetsAt
			}
			if item.Progress.Weekly != nil {
				subscription.WeeklyResetAt = item.Progress.Weekly.ResetsAt
			}
			if item.Progress.Monthly != nil {
				subscription.MonthlyResetAt = item.Progress.Monthly.ResetsAt
			}
		}
		progressByID[subscription.ID] = subscription
	}

	baseItems, err := s.listUserSubscriptions(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("relay: list user subscriptions for progress merge: %w", err)
	}

	out := make([]UserSubscription, 0, len(baseItems)+len(progressByID))
	seen := make(map[int64]struct{}, len(baseItems))
	for _, item := range baseItems {
		subscription := userSubscriptionFromSub2API(item)
		if progress, ok := progressByID[subscription.ID]; ok {
			subscription = mergeSubscriptionProgress(subscription, progress)
		}
		out = append(out, subscription)
		if subscription.ID > 0 {
			seen[subscription.ID] = struct{}{}
		}
	}
	for _, subscription := range progressByID {
		if _, ok := seen[subscription.ID]; ok {
			continue
		}
		out = append(out, subscription)
	}
	return out, nil
}

func mergeSubscriptionProgress(base, progress UserSubscription) UserSubscription {
	base.DailyUsageUSD = progress.DailyUsageUSD
	base.WeeklyUsageUSD = progress.WeeklyUsageUSD
	base.MonthlyUsageUSD = progress.MonthlyUsageUSD
	if progress.DailyResetAt != nil {
		base.DailyResetAt = progress.DailyResetAt
	}
	if progress.WeeklyResetAt != nil {
		base.WeeklyResetAt = progress.WeeklyResetAt
	}
	if progress.MonthlyResetAt != nil {
		base.MonthlyResetAt = progress.MonthlyResetAt
	}
	if base.Group == nil {
		base.Group = progress.Group
	}
	return base
}

func (s *sub2apiRelay) ReadGroupOAuthPoolUsage(ctx context.Context, groupIDs []int64) (UserUsageGroupPoolUsageState, error) {
	uniqueGroupIDs := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID > 0 {
			uniqueGroupIDs[groupID] = struct{}{}
		}
	}
	if len(uniqueGroupIDs) == 0 {
		return UserUsageGroupPoolUsageState{Status: "empty", Groups: []UserUsageGroupPoolUsageGroupItem{}}, nil
	}

	accountIDsByGroup := make(map[int64]map[int64]struct{}, len(uniqueGroupIDs))
	allAccountIDs := make(map[int64]struct{})
	orderedGroupIDs := make([]int64, 0, len(uniqueGroupIDs))
	for groupID := range uniqueGroupIDs {
		orderedGroupIDs = append(orderedGroupIDs, groupID)
	}
	sort.Slice(orderedGroupIDs, func(i, j int) bool { return orderedGroupIDs[i] < orderedGroupIDs[j] })
	for _, groupID := range orderedGroupIDs {
		accounts, err := s.listActiveOAuthAccountsByGroup(ctx, groupID)
		if err != nil {
			return UserUsageGroupPoolUsageState{}, err
		}
		ids := make(map[int64]struct{}, len(accounts))
		for _, account := range accounts {
			if account.ID <= 0 || !strings.EqualFold(strings.TrimSpace(account.Type), "oauth") || !strings.EqualFold(strings.TrimSpace(account.Status), "active") {
				continue
			}
			ids[account.ID] = struct{}{}
			allAccountIDs[account.ID] = struct{}{}
		}
		accountIDsByGroup[groupID] = ids
	}
	if len(allAccountIDs) == 0 {
		return UserUsageGroupPoolUsageState{Status: "empty", Groups: []UserUsageGroupPoolUsageGroupItem{}}, nil
	}

	orderedAccountIDs := make([]int64, 0, len(allAccountIDs))
	for accountID := range allAccountIDs {
		orderedAccountIDs = append(orderedAccountIDs, accountID)
	}
	sort.Slice(orderedAccountIDs, func(i, j int) bool { return orderedAccountIDs[i] < orderedAccountIDs[j] })
	usageByAccount, err := s.getBatchAccountUsage(ctx, orderedAccountIDs)
	if err != nil {
		return UserUsageGroupPoolUsageState{}, err
	}

	now := time.Now().UTC()
	result := UserUsageGroupPoolUsageState{Status: "ok", Groups: make([]UserUsageGroupPoolUsageGroupItem, 0, len(orderedGroupIDs))}
	for _, groupID := range orderedGroupIDs {
		accountIDs := accountIDsByGroup[groupID]
		if len(accountIDs) == 0 {
			continue
		}
		var totalUtilization float64
		validCount := 0
		var nextResetAt *time.Time
		var asOf *time.Time
		for accountID := range accountIDs {
			usage := usageByAccount[strconv.FormatInt(accountID, 10)]
			if usage == nil || usage.SevenDay == nil {
				continue
			}
			validCount++
			totalUtilization += usage.SevenDay.Utilization
			if usage.UpdatedAt != nil && (asOf == nil || usage.UpdatedAt.After(*asOf)) {
				value := usage.UpdatedAt.UTC()
				asOf = &value
			}
			if usage.SevenDay.ResetsAt != nil && usage.SevenDay.ResetsAt.After(now) && (nextResetAt == nil || usage.SevenDay.ResetsAt.Before(*nextResetAt)) {
				value := usage.SevenDay.ResetsAt.UTC()
				nextResetAt = &value
			}
		}
		if validCount == 0 {
			continue
		}
		status := "ok"
		if validCount < len(accountIDs) {
			status = "partial"
		}
		result.Groups = append(result.Groups, UserUsageGroupPoolUsageGroupItem{
			GroupID:                  strconv.FormatInt(groupID, 10),
			Status:                   status,
			AverageWeeklyUtilization: totalUtilization / float64(validCount),
			ValidOAuthAccounts:       validCount,
			TotalActiveOAuthAccounts: len(accountIDs),
			NextResetAt:              nextResetAt,
			AsOf:                     asOf,
		})
	}
	if len(result.Groups) == 0 {
		result.Status = "empty"
	}
	return result, nil
}

func (s *sub2apiRelay) listActiveOAuthAccountsByGroup(ctx context.Context, groupID int64) ([]sub2apiAccountSummary, error) {
	var all []sub2apiAccountSummary
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", "1000")
		query.Set("type", "oauth")
		query.Set("group", strconv.FormatInt(groupID, 10))
		resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/accounts?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("relay: list OAuth accounts for group %d: %w", groupID, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("relay: list OAuth accounts for group %d: read body: %w", groupID, readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("relay: list OAuth accounts for group %d: unexpected status %d%s", groupID, resp.StatusCode, relayErrorMessageSuffixFromData(body))
		}
		var envelope struct {
			envelopeStatus
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("relay: list OAuth accounts for group %d: decode: %w", groupID, err)
		}
		if !envelope.ok() {
			return nil, fmt.Errorf("relay: list OAuth accounts for group %d: request failed%s", groupID, envelope.messageSuffix())
		}
		items, pages, err := decodeAccountPage(envelope.Data)
		if err != nil {
			return nil, fmt.Errorf("relay: list OAuth accounts for group %d: decode page: %w", groupID, err)
		}
		all = append(all, items...)
		if pages <= 1 || page >= pages {
			return all, nil
		}
	}
}

func (s *sub2apiRelay) ListAccountsForPlatform(ctx context.Context, platform string) ([]Account, error) {
	platform = strings.TrimSpace(platform)
	if platform == "" {
		return nil, fmt.Errorf("relay: list accounts: platform is required")
	}
	var out []Account
	for page := 1; ; page++ {
		query := url.Values{}
		query.Set("page", strconv.Itoa(page))
		query.Set("page_size", "1000")
		query.Set("platform", platform)
		resp, err := s.doAdminRequest(ctx, http.MethodGet, "/api/v1/admin/accounts?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("relay: list accounts for platform %s: %w", platform, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("relay: list accounts for platform %s: read body: %w", platform, readErr)
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("relay: list accounts for platform %s: unexpected status %d%s", platform, resp.StatusCode, relayErrorMessageSuffixFromData(body))
		}
		var envelope struct {
			envelopeStatus
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("relay: list accounts for platform %s: decode: %w", platform, err)
		}
		if !envelope.ok() {
			return nil, fmt.Errorf("relay: list accounts for platform %s: request failed%s", platform, envelope.messageSuffix())
		}
		items, pages, err := decodeAccountPage(envelope.Data)
		if err != nil {
			return nil, fmt.Errorf("relay: list accounts for platform %s: decode page: %w", platform, err)
		}
		for _, item := range items {
			account := safeAccountFromSub2API(item)
			out = append(out, account)
		}
		if pages <= 1 || page >= pages {
			return out, nil
		}
	}
}

func (s *sub2apiRelay) SetAccountGroupRelationship(ctx context.Context, accountID, groupID int64, expected []AccountGroupRelationship, desiredPriority *int) error {
	if accountID <= 0 || groupID <= 0 {
		return fmt.Errorf("relay: set account group relationship: account and group ids are required")
	}
	current, err := s.getAccountRelationshipSnapshot(ctx, accountID)
	if err != nil {
		return fmt.Errorf("relay: set account group relationship: load reviewed snapshot: %w", err)
	}
	if !sameAccountRelationships(current.GroupRelationships, expected) {
		return fmt.Errorf("%w: account %d no longer matches the reviewed snapshot", ErrAccountRelationshipsChanged, accountID)
	}

	ordered := append([]AccountGroupRelationship(nil), current.GroupRelationships...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Priority < ordered[j].Priority })
	groupIDs := make([]int64, 0, len(ordered)+1)
	for _, relationship := range ordered {
		if relationship.GroupID > 0 && relationship.GroupID != groupID {
			groupIDs = append(groupIDs, relationship.GroupID)
		}
	}
	if desiredPriority != nil {
		if *desiredPriority <= 0 || *desiredPriority > len(groupIDs)+1 {
			return fmt.Errorf("relay: set account group relationship: priority must be between 1 and %d", len(groupIDs)+1)
		}
		index := *desiredPriority - 1
		groupIDs = append(groupIDs, 0)
		copy(groupIDs[index+1:], groupIDs[index:])
		groupIDs[index] = groupID
	}
	payload, err := json.Marshal(map[string][]int64{"group_ids": groupIDs})
	if err != nil {
		return fmt.Errorf("relay: set account group relationship: marshal: %w", err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/accounts/%d", accountID), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("relay: set account group relationship: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("relay: set account group relationship: read response: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: set account group relationship: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffixFromData(body))
	}
	var envelope envelopeStatus
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("relay: set account group relationship: decode response: %w", err)
	}
	if !envelope.ok() {
		return fmt.Errorf("relay: set account group relationship: request failed%s", envelope.messageSuffix())
	}
	verified, err := s.getAccountRelationshipSnapshot(ctx, accountID)
	if err != nil {
		return fmt.Errorf("relay: set account group relationship: verify update: %w", err)
	}
	desired := make([]AccountGroupRelationship, len(groupIDs))
	for index, desiredGroupID := range groupIDs {
		desired[index] = AccountGroupRelationship{GroupID: desiredGroupID, Priority: index + 1}
	}
	if !sameAccountRelationships(verified.GroupRelationships, desired) {
		return fmt.Errorf("%w: account %d update verification failed", ErrAccountRelationshipsChanged, accountID)
	}
	return nil
}

func (s *sub2apiRelay) getAccountRelationshipSnapshot(ctx context.Context, accountID int64) (Account, error) {
	resp, err := s.doAdminRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/admin/accounts/%d", accountID), nil)
	if err != nil {
		return Account{}, fmt.Errorf("relay: get account %d relationships: %w", accountID, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Account{}, fmt.Errorf("relay: get account %d relationships: unexpected status %d%s", accountID, resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var envelope struct {
		envelopeStatus
		Data sub2apiAccountSummary `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return Account{}, fmt.Errorf("relay: get account %d relationships: decode: %w", accountID, err)
	}
	if !envelope.ok() {
		return Account{}, fmt.Errorf("relay: get account %d relationships: request failed%s", accountID, envelope.messageSuffix())
	}
	return safeAccountFromSub2API(envelope.Data), nil
}

func safeAccountFromSub2API(item sub2apiAccountSummary) Account {
	account := Account{ID: item.ID, Name: item.Name, Platform: item.Platform, Type: item.Type, Status: item.Status, Schedulable: item.Schedulable, GroupRelationships: make([]AccountGroupRelationship, 0, len(item.AccountGroups))}
	for _, relationship := range item.AccountGroups {
		account.GroupRelationships = append(account.GroupRelationships, AccountGroupRelationship{GroupID: relationship.GroupID, Priority: relationship.Priority})
	}
	if len(account.GroupRelationships) == 0 {
		for index, groupID := range item.GroupIDs {
			account.GroupRelationships = append(account.GroupRelationships, AccountGroupRelationship{GroupID: groupID, Priority: index + 1})
		}
	}
	return account
}

func sameAccountRelationships(left, right []AccountGroupRelationship) bool {
	if len(left) != len(right) {
		return false
	}
	want := make(map[int64]int, len(right))
	for _, relationship := range right {
		if relationship.GroupID <= 0 || relationship.Priority <= 0 {
			return false
		}
		want[relationship.GroupID] = relationship.Priority
	}
	if len(want) != len(right) {
		return false
	}
	for _, relationship := range left {
		if want[relationship.GroupID] != relationship.Priority {
			return false
		}
	}
	return true
}

func decodeAccountPage(data json.RawMessage) ([]sub2apiAccountSummary, int, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, 1, nil
	}
	if data[0] == '[' {
		var items []sub2apiAccountSummary
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, 0, err
		}
		return items, 1, nil
	}
	var page struct {
		Items []sub2apiAccountSummary `json:"items"`
		Pages int                     `json:"pages"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, err
	}
	if page.Pages <= 0 {
		page.Pages = 1
	}
	return page.Items, page.Pages, nil
}

func (s *sub2apiRelay) getBatchAccountUsage(ctx context.Context, accountIDs []int64) (map[string]*sub2apiUsageInfo, error) {
	payload, err := json.Marshal(map[string]any{"account_ids": accountIDs, "force": false})
	if err != nil {
		return nil, fmt.Errorf("relay: batch OAuth usage: marshal: %w", err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/admin/accounts/usage/batch", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("relay: batch OAuth usage: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: batch OAuth usage: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result struct {
		envelopeStatus
		Data struct {
			Usage  map[string]*sub2apiUsageInfo `json:"usage"`
			Errors map[string]string            `json:"errors"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("relay: batch OAuth usage: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("relay: batch OAuth usage: request failed%s", result.messageSuffix())
	}
	return result.Data.Usage, nil
}

func (s *sub2apiRelay) assignDefaultSubscriptionsForUser(ctx context.Context, userID int64) error {
	if userID <= 0 {
		return nil
	}
	defaults, err := s.listDefaultSubscriptions(ctx)
	if err != nil {
		return err
	}
	existingGroups, _ := s.activeSubscriptionGroupIDs(ctx, userID)
	if existingGroups == nil {
		existingGroups = make(map[int64]bool)
	}
	for _, item := range defaults {
		if item.GroupID <= 0 {
			continue
		}
		if existingGroups[item.GroupID] {
			continue
		}
		if err := s.assignSubscription(ctx, userID, subscriptionAssignment{
			GroupID:      item.GroupID,
			ValidityDays: item.ValidityDays,
			Notes:        "auto assigned by ai-efficiency relay provisioning",
		}); err != nil {
			if assigned, checkErr := s.hasActiveSubscriptionGroup(ctx, userID, item.GroupID); checkErr == nil && assigned {
				existingGroups[item.GroupID] = true
				continue
			}
			return err
		}
		existingGroups[item.GroupID] = true
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

// ResetSubscriptionQuotaForUser resets daily, weekly, and monthly usage for an existing subscription group.
func (s *sub2apiRelay) ResetSubscriptionQuotaForUser(ctx context.Context, userID, groupID int64) error {
	if userID <= 0 {
		return fmt.Errorf("reset subscription quota: user id is required")
	}
	if groupID <= 0 {
		return fmt.Errorf("reset subscription quota: group id is required")
	}
	subscription, err := s.findSubscriptionForUserGroup(ctx, userID, groupID)
	if err != nil {
		return fmt.Errorf("reset subscription quota: %w", err)
	}

	payload, err := json.Marshal(map[string]any{
		"daily":   true,
		"weekly":  true,
		"monthly": true,
	})
	if err != nil {
		return fmt.Errorf("reset subscription quota: marshal: %w", err)
	}
	resp, err := s.doAdminRequest(ctx, http.MethodPost, fmt.Sprintf("/api/v1/admin/subscriptions/%d/reset-quota", subscription.ID), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("reset subscription quota: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("reset subscription quota: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffix(resp.Body))
	}
	var result envelopeStatus
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("reset subscription quota: decode: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("reset subscription quota: request failed")
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

func (s *sub2apiRelay) activeSubscriptionGroupIDs(ctx context.Context, userID int64) (map[int64]bool, error) {
	subscriptions, err := s.listUserSubscriptions(ctx, userID)
	if err != nil {
		return nil, err
	}
	groups := make(map[int64]bool, len(subscriptions))
	for _, subscription := range subscriptions {
		groupID := subscription.GroupID
		if groupID == 0 && subscription.Group != nil {
			groupID = subscription.Group.ID
		}
		if groupID > 0 && isActiveSubscriptionStatus(subscription.Status) {
			groups[groupID] = true
		}
	}
	return groups, nil
}

func (s *sub2apiRelay) hasActiveSubscriptionGroup(ctx context.Context, userID, groupID int64) (bool, error) {
	groups, err := s.activeSubscriptionGroupIDs(ctx, userID)
	if err != nil {
		return false, err
	}
	return groups[groupID], nil
}

func isActiveSubscriptionStatus(status string) bool {
	status = strings.TrimSpace(status)
	return status == "" || strings.EqualFold(status, "active")
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
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

type openAIResponsesRequest struct {
	Model           string                 `json:"model"`
	Input           []openAIResponsesInput `json:"input"`
	MaxOutputTokens *int                   `json:"max_output_tokens,omitempty"`
	Stream          bool                   `json:"stream"`
}

type openAIResponsesInput struct {
	Role    string                   `json:"role"`
	Content []openAIResponsesContent `json:"content"`
}

type openAIResponsesContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIResponsesResponse struct {
	Status string `json:"status"`
	Output []struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"output"`
	Usage struct {
		TotalTokens int `json:"total_tokens"`
	} `json:"usage"`
}

func (r openAIResponsesResponse) contentText() string {
	var parts []string
	for _, output := range r.Output {
		if output.Type != "message" {
			continue
		}
		for _, content := range output.Content {
			if content.Type == "output_text" && strings.TrimSpace(content.Text) != "" {
				parts = append(parts, strings.TrimSpace(content.Text))
			}
		}
	}
	return strings.Join(parts, "\n")
}

type anthropicMessagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

func (r anthropicMessagesResponse) contentText() string {
	parts := make([]string, 0, len(r.Content))
	for _, item := range r.Content {
		if item.Type != "text" {
			continue
		}
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
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		TotalTokenCount int `json:"totalTokenCount"`
	} `json:"usageMetadata"`
}

func (r geminiGenerateResponse) hasTerminalCandidate() bool {
	for _, candidate := range r.Candidates {
		if strings.TrimSpace(candidate.FinishReason) != "" {
			return true
		}
	}
	return false
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

type userUsageTrendEnvelope struct {
	Trend       []UserUsageTrendPoint `json:"trend"`
	StartDate   string                `json:"start_date"`
	EndDate     string                `json:"end_date"`
	Granularity string                `json:"granularity"`
}

type userUsageModelsEnvelope struct {
	Models    []UserUsageModelStat `json:"models"`
	StartDate string               `json:"start_date"`
	EndDate   string               `json:"end_date"`
}

func (s *sub2apiRelay) GetUsageDashboardForUser(ctx context.Context, relayUserID int64, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error) {
	stats, err := s.getAdminUsageStatsForUser(ctx, relayUserID)
	if err != nil {
		return nil, err
	}
	trend, err := s.getAdminUsageTrendForUser(ctx, relayUserID, params)
	if err != nil {
		return nil, err
	}
	models, err := s.getAdminUsageModelsForUser(ctx, relayUserID, params)
	if err != nil {
		return nil, err
	}

	return &UserUsageDashboardResponse{
		Configured: true,
		Range: UserUsageDashboardRange{
			StartDate:   firstNonEmpty(trend.StartDate, params.StartDate),
			EndDate:     firstNonEmpty(trend.EndDate, params.EndDate),
			Granularity: firstNonEmpty(trend.Granularity, params.Granularity, "day"),
			Timezone:    strings.TrimSpace(params.Timezone),
		},
		Stats:  stats,
		Trend:  trend.Trend,
		Models: models.Models,
	}, nil
}

func (s *sub2apiRelay) GetBatchUserUsageStats(ctx context.Context, userIDs []int64, params TeamUsageSummaryParams) (map[int64]TeamUserUsageStats, error) {
	payload, err := json.Marshal(map[string]any{
		"user_ids":    userIDs,
		"start_date":  strings.TrimSpace(params.StartDate),
		"end_date":    strings.TrimSpace(params.EndDate),
		"granularity": strings.TrimSpace(params.Granularity),
		"timezone":    strings.TrimSpace(params.Timezone),
	})
	if err != nil {
		return nil, fmt.Errorf("relay: batch user usage stats: marshal: %w", err)
	}

	resp, err := s.doAdminRequest(ctx, http.MethodPost, "/api/v1/admin/dashboard/users-usage", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("relay: batch user usage stats: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, fmt.Errorf("relay: batch user usage stats: read: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("relay: batch user usage stats: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffixFromData(body))
	}

	var result struct {
		envelopeStatus
		Data struct {
			Stats map[string]TeamUserUsageStats `json:"stats"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("relay: batch user usage stats: decode: %w", err)
	}
	if !result.ok() {
		return nil, fmt.Errorf("relay: batch user usage stats: request failed%s", result.envelopeStatus.messageSuffix())
	}

	out := make(map[int64]TeamUserUsageStats, len(result.Data.Stats))
	for rawUserID, item := range result.Data.Stats {
		if item.UserID == 0 {
			parsedUserID, err := strconv.ParseInt(strings.TrimSpace(rawUserID), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("relay: batch user usage stats: parse user id %q: %w", rawUserID, err)
			}
			item.UserID = parsedUserID
		}
		out[item.UserID] = item
	}
	return out, nil
}

func (s *sub2apiRelay) GetUsageTrendForUsers(ctx context.Context, relayUserIDs []int64, params TeamMemberTrendParams) (map[int64][]UsageTrendPoint, error) {
	requested := make([]int64, 0, len(relayUserIDs))
	seen := make(map[int64]struct{}, len(relayUserIDs))
	for _, relayUserID := range relayUserIDs {
		if relayUserID <= 0 {
			return nil, fmt.Errorf("relay: team trend batch: invalid requested user ID %d", relayUserID)
		}
		if _, exists := seen[relayUserID]; exists {
			continue
		}
		seen[relayUserID] = struct{}{}
		requested = append(requested, relayUserID)
	}
	if len(requested) == 0 {
		return map[int64][]UsageTrendPoint{}, nil
	}

	limit := teamTrendBatchLimit(len(requested))
	result, err := s.getTeamTrendFallback(ctx, requested, TeamMemberTrendParams{
		StartDate: strings.TrimSpace(params.StartDate), EndDate: strings.TrimSpace(params.EndDate),
		Granularity: strings.TrimSpace(params.Granularity), Timezone: strings.TrimSpace(params.Timezone),
	}, limit)
	if err != nil {
		return nil, err
	}
	if !result.Complete {
		return nil, fmt.Errorf("relay: team trend batch: response may be truncated at limit %d", limit)
	}
	for _, relayUserID := range requested {
		if _, exists := result.PointsByUser[relayUserID]; !exists {
			result.PointsByUser[relayUserID] = []UsageTrendPoint{}
		}
	}
	return result.PointsByUser, nil
}

func (s *sub2apiRelay) ListGroupRateMultipliers(ctx context.Context, groupID int64) ([]UserGroupRateEntry, error) {
	var entries []UserGroupRateEntry
	if err := s.getAdminEnvelopeJSON(ctx, fmt.Sprintf("/api/v1/admin/groups/%d/rate-multipliers", groupID), nil, &entries); err != nil {
		return nil, fmt.Errorf("relay: list group rate multipliers: %w", err)
	}
	return entries, nil
}

func (s *sub2apiRelay) GroupRateMultipliersForGroups(ctx context.Context, groupIDs []int64) []GroupRateMultiplierReadResult {
	const (
		maxConcurrentRequests = 4
		requestTimeout        = 2 * time.Second
		batchTimeout          = 5 * time.Second
	)

	uniqueGroupIDs := make([]int64, 0, len(groupIDs))
	seen := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if _, exists := seen[groupID]; exists {
			continue
		}
		seen[groupID] = struct{}{}
		uniqueGroupIDs = append(uniqueGroupIDs, groupID)
	}

	results := make([]GroupRateMultiplierReadResult, len(uniqueGroupIDs))
	for i, groupID := range uniqueGroupIDs {
		results[i].GroupID = groupID
	}
	if len(results) == 0 {
		return results
	}

	batchCtx, cancelBatch := context.WithTimeout(ctx, batchTimeout)
	defer cancelBatch()

	workerCount := maxConcurrentRequests
	if len(results) < workerCount {
		workerCount = len(results)
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for resultIndex := range jobs {
				if err := batchCtx.Err(); err != nil {
					results[resultIndex].Err = err
					continue
				}

				requestCtx, cancelRequest := context.WithTimeout(batchCtx, requestTimeout)
				entries, err := s.ListGroupRateMultipliers(requestCtx, results[resultIndex].GroupID)
				cancelRequest()
				results[resultIndex].Entries = entries
				results[resultIndex].Err = err
			}
		}()
	}

	for resultIndex := range results {
		jobs <- resultIndex
	}
	close(jobs)
	wg.Wait()
	return results
}

func (s *sub2apiRelay) ReplaceGroupRateMultipliers(ctx context.Context, groupID int64, entries []GroupRateMultiplierInput) error {
	payload, err := json.Marshal(map[string]any{"entries": entries})
	if err != nil {
		return fmt.Errorf("relay: replace group rate multipliers: marshal: %w", err)
	}

	resp, err := s.doAdminRequest(ctx, http.MethodPut, fmt.Sprintf("/api/v1/admin/groups/%d/rate-multipliers", groupID), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("relay: replace group rate multipliers: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("relay: replace group rate multipliers: read: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("relay: replace group rate multipliers: unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffixFromData(body))
	}

	var result envelopeStatus
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("relay: replace group rate multipliers: decode: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("relay: replace group rate multipliers: request failed%s", result.messageSuffix())
	}
	return nil
}

func (s *sub2apiRelay) GetUserUsageDashboard(ctx context.Context, login, password string, params UserUsageDashboardParams) (*UserUsageDashboardResponse, error) {
	result, err := s.ReadUserUsageOrigin(ctx, UserUsageOriginRequest{
		Login:    login,
		Password: password,
		Params:   params,
		Branches: UserUsageOriginBranches{Usage: true},
	})
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("relay: usage origin returned an empty result")
	}
	if result.UsageErr != nil {
		return nil, result.UsageErr
	}
	if result.Usage == nil {
		return nil, fmt.Errorf("relay: usage origin returned no usage data")
	}
	return result.Usage, nil
}

func (s *sub2apiRelay) ReadUserUsageOrigin(ctx context.Context, request UserUsageOriginRequest) (*UserUsageOriginResult, error) {
	if !request.Branches.Usage && !request.Branches.Quota {
		return nil, fmt.Errorf("relay: user usage origin requires at least one branch")
	}
	if request.Branches.Quota && request.RelayUserID <= 0 {
		return nil, fmt.Errorf("relay: user usage quota branch requires a relay user ID")
	}

	originCtx, cancel := context.WithTimeout(ctx, userUsageOriginTimeout)
	defer cancel()

	var token string
	if request.Branches.Usage {
		var authenticatedUser *User
		var err error
		token, authenticatedUser, err = s.loginSessionToken(originCtx, request.Login, request.Password)
		if err != nil {
			return nil, fmt.Errorf("relay: login for usage origin: %w", err)
		}
		if request.RelayUserID > 0 && authenticatedUser != nil && authenticatedUser.ID != request.RelayUserID {
			return nil, fmt.Errorf("relay: usage origin authenticated user %d does not match requested user %d", authenticatedUser.ID, request.RelayUserID)
		}
	}

	type branchKind int
	const (
		usageStatsBranch branchKind = iota
		usageTrendBranch
		usageModelsBranch
		usageKeysBranch
		usageSubscriptionsBranch
	)
	type branchResult struct {
		kind  branchKind
		value any
		err   error
	}
	type branchTask struct {
		kind branchKind
		run  func() (any, error)
	}

	tasks := make([]branchTask, 0, 5)
	if request.Branches.Usage {
		tasks = append(tasks,
			branchTask{kind: usageStatsBranch, run: func() (any, error) {
				return s.getUserUsageDashboardStats(originCtx, token)
			}},
			branchTask{kind: usageTrendBranch, run: func() (any, error) {
				return s.getUserUsageDashboardTrend(originCtx, token, request.Params)
			}},
			branchTask{kind: usageModelsBranch, run: func() (any, error) {
				return s.getUserUsageDashboardModels(originCtx, token, request.Params)
			}},
		)
	}
	if request.Branches.Quota {
		tasks = append(tasks,
			branchTask{kind: usageKeysBranch, run: func() (any, error) {
				return s.ListUserAPIKeys(originCtx, request.RelayUserID)
			}},
			branchTask{kind: usageSubscriptionsBranch, run: func() (any, error) {
				if strings.TrimSpace(request.Login) != "" && strings.TrimSpace(request.Password) != "" {
					var subscriptions []UserSubscription
					var err error
					if token != "" {
						subscriptions, err = s.listUserSubscriptionsWithProgressToken(originCtx, token, request.RelayUserID)
					} else {
						subscriptions, err = s.listUserSubscriptionsWithProgress(originCtx, request.Login, request.Password, request.RelayUserID)
					}
					if err == nil {
						return subscriptions, nil
					}
					// Progress is an enrichment endpoint. Keep the existing quota projection
					// available when it is missing or temporarily unavailable.
				}
				return s.ListUserSubscriptions(originCtx, request.RelayUserID)
			}},
		)
	}

	results := make(chan branchResult, len(tasks))
	for _, task := range tasks {
		task := task
		go func() {
			value, err := task.run()
			results <- branchResult{kind: task.kind, value: value, err: err}
		}()
	}

	result := &UserUsageOriginResult{}
	var stats *UserUsageDashboardStats
	var trend *userUsageTrendEnvelope
	var models *userUsageModelsEnvelope
	var statsErr, trendErr, modelsErr error
	var keysErr, subscriptionsErr error
	for range tasks {
		branch := <-results
		switch branch.kind {
		case usageStatsBranch:
			stats, _ = branch.value.(*UserUsageDashboardStats)
			statsErr = branch.err
		case usageTrendBranch:
			trend, _ = branch.value.(*userUsageTrendEnvelope)
			trendErr = branch.err
		case usageModelsBranch:
			models, _ = branch.value.(*userUsageModelsEnvelope)
			modelsErr = branch.err
		case usageKeysBranch:
			result.APIKeys, _ = branch.value.([]APIKey)
			keysErr = branch.err
		case usageSubscriptionsBranch:
			result.Subscriptions, _ = branch.value.([]UserSubscription)
			subscriptionsErr = branch.err
		}
	}

	if request.Branches.Usage {
		result.UsageErr = firstError(statsErr, trendErr, modelsErr)
		if result.UsageErr == nil {
			if stats == nil || trend == nil || models == nil {
				result.UsageErr = fmt.Errorf("relay: usage origin returned an incomplete generation")
			} else {
				result.Usage = &UserUsageDashboardResponse{
					Configured: true,
					Range: UserUsageDashboardRange{
						StartDate:   firstNonEmpty(trend.StartDate, request.Params.StartDate),
						EndDate:     firstNonEmpty(trend.EndDate, request.Params.EndDate),
						Granularity: firstNonEmpty(trend.Granularity, request.Params.Granularity, "day"),
						Timezone:    strings.TrimSpace(request.Params.Timezone),
					},
					Stats:  stats,
					Trend:  trend.Trend,
					Models: models.Models,
				}
			}
		}
	}
	if request.Branches.Quota {
		result.QuotaErr = firstError(keysErr, subscriptionsErr)
		if result.QuotaErr != nil {
			result.APIKeys = nil
			result.Subscriptions = nil
		}
	}
	return result, nil
}

func firstError(errors ...error) error {
	for _, err := range errors {
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *sub2apiRelay) getUserUsageDashboardStats(ctx context.Context, token string) (*UserUsageDashboardStats, error) {
	var stats UserUsageDashboardStats
	if err := s.getUserDashboardJSON(ctx, token, "/api/v1/usage/dashboard/stats", nil, &stats); err != nil {
		return nil, fmt.Errorf("relay: usage dashboard stats: %w", err)
	}
	return &stats, nil
}

func (s *sub2apiRelay) getUserUsageDashboardTrend(ctx context.Context, token string, params UserUsageDashboardParams) (*userUsageTrendEnvelope, error) {
	query := url.Values{}
	addUserUsageDashboardQuery(query, params, true)
	var trend userUsageTrendEnvelope
	if err := s.getUserDashboardJSON(ctx, token, "/api/v1/usage/dashboard/trend", query, &trend); err != nil {
		return nil, fmt.Errorf("relay: usage dashboard trend: %w", err)
	}
	return &trend, nil
}

func (s *sub2apiRelay) getUserUsageDashboardModels(ctx context.Context, token string, params UserUsageDashboardParams) (*userUsageModelsEnvelope, error) {
	query := url.Values{}
	addUserUsageDashboardQuery(query, params, false)
	var models userUsageModelsEnvelope
	if err := s.getUserDashboardJSON(ctx, token, "/api/v1/usage/dashboard/models", query, &models); err != nil {
		return nil, fmt.Errorf("relay: usage dashboard models: %w", err)
	}
	return &models, nil
}

func (s *sub2apiRelay) getAdminUsageStatsForUser(ctx context.Context, relayUserID int64) (*UserUsageDashboardStats, error) {
	query := url.Values{}
	query.Set("user_id", strconv.FormatInt(relayUserID, 10))
	var stats UserUsageDashboardStats
	if err := s.getAdminEnvelopeJSON(ctx, "/api/v1/admin/usage/stats", query, &stats); err != nil {
		return nil, fmt.Errorf("relay: subject usage dashboard stats: %w", err)
	}
	return &stats, nil
}

func (s *sub2apiRelay) ReadRequestUsage(ctx context.Context, requestID string, limit int) ([]RequestUsage, error) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return nil, fmt.Errorf("relay: request usage: request ID is required")
	}
	if limit != 2 {
		return nil, fmt.Errorf("relay: request usage: limit must be 2")
	}
	query := url.Values{}
	query.Set("request_id", requestID)
	query.Set("page", "1")
	query.Set("page_size", strconv.Itoa(limit))
	query.Set("exact_total", "true")
	var page struct {
		Items []struct {
			RequestID           string    `json:"request_id"`
			UserID              int64     `json:"user_id"`
			Model               string    `json:"model"`
			InputTokens         int64     `json:"input_tokens"`
			OutputTokens        int64     `json:"output_tokens"`
			CacheCreationTokens int64     `json:"cache_creation_tokens"`
			CacheReadTokens     int64     `json:"cache_read_tokens"`
			TotalTokens         *int64    `json:"total_tokens"`
			CreatedAt           time.Time `json:"created_at"`
		} `json:"items"`
		Total int64 `json:"total"`
	}
	if err := s.getAdminEnvelopeJSON(ctx, "/api/v1/admin/usage", query, &page); err != nil {
		return nil, fmt.Errorf("relay: request usage: %w", err)
	}
	expectedRows := page.Total
	if expectedRows > int64(limit) {
		expectedRows = int64(limit)
	}
	if page.Total < 0 || int64(len(page.Items)) != expectedRows {
		return nil, fmt.Errorf("relay: request usage: inconsistent exact pagination")
	}
	result := make([]RequestUsage, 0, len(page.Items))
	for _, item := range page.Items {
		result = append(result, RequestUsage{
			RequestID: strings.TrimSpace(item.RequestID), UserID: item.UserID, RequestedModel: strings.TrimSpace(item.Model), UsageAt: item.CreatedAt,
			InputTokens: item.InputTokens, OutputTokens: item.OutputTokens,
			CacheCreationTokens: item.CacheCreationTokens, CacheReadTokens: item.CacheReadTokens,
			TotalTokens: item.TotalTokens,
		})
	}
	return result, nil
}

func (s *sub2apiRelay) getAdminUsageTrendForUser(ctx context.Context, relayUserID int64, params UserUsageDashboardParams) (*userUsageTrendEnvelope, error) {
	query := url.Values{}
	addUserUsageDashboardQuery(query, params, true)
	query.Set("user_id", strconv.FormatInt(relayUserID, 10))
	var raw json.RawMessage
	if err := s.getAdminEnvelopeJSON(ctx, "/api/v1/admin/dashboard/trend", query, &raw); err != nil {
		return nil, fmt.Errorf("relay: subject usage dashboard trend: %w", err)
	}
	trend, err := decodeUserUsageTrendEnvelope(raw)
	if err != nil {
		return nil, fmt.Errorf("relay: subject usage dashboard trend: decode: %w", err)
	}
	return trend, nil
}

func (s *sub2apiRelay) getAdminUsageModelsForUser(ctx context.Context, relayUserID int64, params UserUsageDashboardParams) (*userUsageModelsEnvelope, error) {
	query := url.Values{}
	addUserUsageDashboardQuery(query, params, false)
	query.Set("user_id", strconv.FormatInt(relayUserID, 10))
	var raw json.RawMessage
	if err := s.getAdminEnvelopeJSON(ctx, "/api/v1/admin/dashboard/models", query, &raw); err != nil {
		return nil, fmt.Errorf("relay: subject usage dashboard models: %w", err)
	}
	models, err := decodeUserUsageModelsEnvelope(raw)
	if err != nil {
		return nil, fmt.Errorf("relay: subject usage dashboard models: decode: %w", err)
	}
	return models, nil
}

func (s *sub2apiRelay) getUserDashboardJSON(ctx context.Context, token, path string, query url.Values, dst any) error {
	u, err := url.Parse(s.adminURL + path)
	if err != nil {
		return fmt.Errorf("parse url: %w", err)
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var result struct {
		envelopeStatus
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("request failed")
	}
	if len(result.Data) == 0 {
		return fmt.Errorf("missing data")
	}
	if err := json.Unmarshal(result.Data, dst); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	return nil
}

func (s *sub2apiRelay) getAdminEnvelopeJSON(ctx context.Context, path string, query url.Values, dst any) error {
	if len(query) > 0 {
		path += "?" + query.Encode()
	}

	resp, err := s.doAdminRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return fmt.Errorf("read body: %w", readErr)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d%s", resp.StatusCode, relayErrorMessageSuffixFromData(body))
	}

	var result struct {
		envelopeStatus
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode envelope: %w", err)
	}
	if !result.ok() {
		return fmt.Errorf("request failed%s", result.envelopeStatus.messageSuffix())
	}
	if dst == nil {
		return nil
	}
	if err := json.Unmarshal(result.Data, dst); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}
	return nil
}

func decodeUserUsageTrendEnvelope(raw json.RawMessage) (*userUsageTrendEnvelope, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return &userUsageTrendEnvelope{}, nil
	}
	if raw[0] == '[' {
		var points []UserUsageTrendPoint
		if err := json.Unmarshal(raw, &points); err != nil {
			return nil, err
		}
		return &userUsageTrendEnvelope{Trend: points}, nil
	}
	var trend userUsageTrendEnvelope
	if err := json.Unmarshal(raw, &trend); err != nil {
		return nil, err
	}
	return &trend, nil
}

func decodeUserUsageModelsEnvelope(raw json.RawMessage) (*userUsageModelsEnvelope, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return &userUsageModelsEnvelope{}, nil
	}
	if raw[0] == '[' {
		var models []UserUsageModelStat
		if err := json.Unmarshal(raw, &models); err != nil {
			return nil, err
		}
		return &userUsageModelsEnvelope{Models: models}, nil
	}
	var models userUsageModelsEnvelope
	if err := json.Unmarshal(raw, &models); err != nil {
		return nil, err
	}
	return &models, nil
}

func addUserUsageDashboardQuery(query url.Values, params UserUsageDashboardParams, includeGranularity bool) {
	if v := strings.TrimSpace(params.StartDate); v != "" {
		query.Set("start_date", v)
	}
	if v := strings.TrimSpace(params.EndDate); v != "" {
		query.Set("end_date", v)
	}
	if includeGranularity {
		if v := strings.TrimSpace(params.Granularity); v != "" {
			query.Set("granularity", v)
		}
	}
	if v := strings.TrimSpace(params.Timezone); v != "" {
		query.Set("timezone", v)
	}
}
