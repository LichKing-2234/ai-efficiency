package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// ErrNotFound is returned when the backend responds with 404.
var ErrNotFound = errors.New("not found")

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

var toolUsageRetryBackoffs = []time.Duration{250 * time.Millisecond, time.Second}
var toolUsageAttemptTimeout = 15 * time.Second

const toolUsageErrorBodyLimit = 4096

type ProviderInfo struct {
	Name         string                   `json:"name"`
	DisplayName  string                   `json:"display_name"`
	BaseURL      string                   `json:"base_url"`
	APIKey       string                   `json:"api_key"`
	APIKeyID     int64                    `json:"api_key_id"`
	DefaultModel string                   `json:"default_model"`
	IsPrimary    bool                     `json:"is_primary"`
	Credentials  []ProviderCredentialInfo `json:"credentials,omitempty"`
}

type ProviderCredentialInfo struct {
	Platform  string `json:"platform"`
	GroupID   string `json:"group_id,omitempty"`
	GroupName string `json:"group_name,omitempty"`
	APIKey    string `json:"api_key"`
	APIKeyID  int64  `json:"api_key_id"`
	Status    string `json:"status,omitempty"`
}

type userProviderGroup struct {
	GroupID    string `json:"group_id"`
	GroupName  string `json:"group_name"`
	Platform   string `json:"platform"`
	Credential struct {
		APIKeyID int64  `json:"api_key_id"`
		Key      string `json:"key"`
		State    string `json:"state"`
		Status   string `json:"status"`
	} `json:"credential"`
}

type CommitCheckpointRequest struct {
	EventID        string         `json:"event_id"`
	SessionID      string         `json:"session_id,omitempty"`
	RepoConfigID   int            `json:"repo_config_id,omitempty"`
	RepoFullName   string         `json:"repo_full_name"`
	WorkspaceID    string         `json:"workspace_id"`
	CommitSHA      string         `json:"commit_sha"`
	ParentSHAs     []string       `json:"parent_shas,omitempty"`
	BranchSnapshot string         `json:"branch_snapshot,omitempty"`
	HeadSnapshot   string         `json:"head_snapshot,omitempty"`
	BindingSource  string         `json:"binding_source"`
	AgentSnapshot  map[string]any `json:"agent_snapshot,omitempty"`
	CapturedAt     *time.Time     `json:"captured_at,omitempty"`
}

type CommitRewriteRequest struct {
	EventID       string     `json:"event_id"`
	SessionID     string     `json:"session_id,omitempty"`
	RepoConfigID  int        `json:"repo_config_id,omitempty"`
	RepoFullName  string     `json:"repo_full_name"`
	WorkspaceID   string     `json:"workspace_id"`
	RewriteType   string     `json:"rewrite_type"`
	OldCommitSHA  string     `json:"old_commit_sha"`
	NewCommitSHA  string     `json:"new_commit_sha"`
	BindingSource string     `json:"binding_source"`
	CapturedAt    *time.Time `json:"captured_at,omitempty"`
}

type ToolUsageEventRequest struct {
	RepoConfigID      int            `json:"repo_config_id,omitempty"`
	Tool              string         `json:"tool"`
	WorkspaceID       string         `json:"workspace_id"`
	ToolSessionID     string         `json:"tool_session_id"`
	ToolEventID       string         `json:"tool_event_id,omitempty"`
	DedupeKey         string         `json:"dedupe_key"`
	UsageUnit         string         `json:"usage_unit"`
	RequestCount      int            `json:"request_count"`
	InputTokens       int64          `json:"input_tokens"`
	OutputTokens      int64          `json:"output_tokens"`
	CachedInputTokens int64          `json:"cached_input_tokens"`
	ReasoningTokens   int64          `json:"reasoning_tokens"`
	CreditUsage       float64        `json:"credit_usage"`
	ContextUsagePct   float64        `json:"context_usage_pct"`
	ObservedStartAt   time.Time      `json:"observed_start_at"`
	ObservedEndAt     time.Time      `json:"observed_end_at"`
	RawSourcePath     string         `json:"raw_source_path,omitempty"`
	RawSourceLocator  string         `json:"raw_source_locator,omitempty"`
	RawPayload        map[string]any `json:"raw_payload,omitempty"`
}

type ToolUsageEventsBatchRequest struct {
	Events []ToolUsageEventRequest `json:"events"`
}

type RepoEnsureResponse struct {
	ID            int    `json:"id"`
	RepoKey       string `json:"repo_key"`
	FullName      string `json:"full_name"`
	CloneURL      string `json:"clone_url"`
	DefaultBranch string `json:"default_branch"`
	BindingState  string `json:"binding_state"`
	SCMProviderID *int   `json:"scm_provider_id,omitempty"`
}

const RepoEligibilityVersion = "repo-eligibility-v1"

type ResolveRepoRequest struct {
	RemoteURL          string `json:"remote_url"`
	Branch             string `json:"branch,omitempty"`
	ClientCacheVersion string `json:"client_cache_version,omitempty"`
}

type RepoEligibilityResponse struct {
	Eligible     bool   `json:"eligible"`
	RepoConfigID int    `json:"repo_config_id,omitempty"`
	RepoKey      string `json:"repo_key,omitempty"`
	FullName     string `json:"full_name,omitempty"`
	CloneURL     string `json:"clone_url,omitempty"`
	Status       string `json:"status,omitempty"`
	BindingState string `json:"binding_state,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

type HookEligibleRepoRequest struct {
	RepoKey   string `json:"repo_key"`
	RemoteURL string `json:"remote_url"`
}

type BatchHookEligibleResponse struct {
	Repos      []RepoEligibilityResponse `json:"repos"`
	Ineligible []RepoEligibilityResponse `json:"ineligible"`
	Version    string                    `json:"version"`
}

func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *Client) SendCommitCheckpoint(ctx context.Context, req CommitCheckpointRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling checkpoint request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/checkpoints/commit", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating checkpoint request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending checkpoint request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected checkpoint status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) SendCommitRewrite(ctx context.Context, req CommitRewriteRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling rewrite request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/checkpoints/rewrite", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating rewrite request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending rewrite request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected rewrite status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) SendToolUsageEvent(ctx context.Context, req ToolUsageEventRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal tool usage event: %w", err)
	}
	var lastErr error
	attempts := len(toolUsageRetryBackoffs) + 1
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx := ctx
		cancel := func() {}
		if toolUsageAttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, toolUsageAttemptTimeout)
		}
		httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.baseURL+"/api/v1/tool-usage-events", bytes.NewReader(body))
		if err != nil {
			cancel()
			return fmt.Errorf("create tool usage event request: %w", err)
		}
		c.setHeaders(httpReq)
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("send tool usage event: %w", err)
		} else {
			if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
				_ = resp.Body.Close()
				cancel()
				return nil
			}
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, toolUsageErrorBodyLimit))
			_ = resp.Body.Close()
			cancel()
			lastErr = fmt.Errorf("unexpected tool usage status %d: %s", resp.StatusCode, string(respBody))
			if !isRetryableToolUsageStatus(resp.StatusCode) {
				return lastErr
			}
		}
		if attempt < len(toolUsageRetryBackoffs) {
			if err := sleepWithContext(ctx, toolUsageRetryBackoffs[attempt]); err != nil {
				return err
			}
		}
	}
	return lastErr
}

func (c *Client) SendToolUsageEvents(ctx context.Context, reqs []ToolUsageEventRequest) error {
	if len(reqs) == 0 {
		return nil
	}
	body, err := json.Marshal(ToolUsageEventsBatchRequest{Events: reqs})
	if err != nil {
		return fmt.Errorf("marshal tool usage events batch: %w", err)
	}
	var lastErr error
	attempts := len(toolUsageRetryBackoffs) + 1
	for attempt := 0; attempt < attempts; attempt++ {
		attemptCtx := ctx
		cancel := func() {}
		if toolUsageAttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, toolUsageAttemptTimeout)
		}
		httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodPost, c.baseURL+"/api/v1/tool-usage-events/batch", bytes.NewReader(body))
		if err != nil {
			cancel()
			return fmt.Errorf("create tool usage events batch request: %w", err)
		}
		c.setHeaders(httpReq)
		resp, err := c.httpClient.Do(httpReq)
		if err != nil {
			cancel()
			lastErr = fmt.Errorf("send tool usage events batch: %w", err)
		} else {
			if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
				_ = resp.Body.Close()
				cancel()
				return nil
			}
			respBody, _ := io.ReadAll(io.LimitReader(resp.Body, toolUsageErrorBodyLimit))
			_ = resp.Body.Close()
			cancel()
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusUnprocessableEntity {
				return c.sendToolUsageEventsIndividually(ctx, reqs, fmt.Errorf("unexpected tool usage batch status %d: %s", resp.StatusCode, string(respBody)))
			}
			lastErr = fmt.Errorf("unexpected tool usage batch status %d: %s", resp.StatusCode, string(respBody))
			if !isRetryableToolUsageStatus(resp.StatusCode) {
				return lastErr
			}
		}
		if attempt < len(toolUsageRetryBackoffs) {
			if err := sleepWithContext(ctx, toolUsageRetryBackoffs[attempt]); err != nil {
				return err
			}
		}
	}
	return lastErr
}

func (c *Client) sendToolUsageEventsIndividually(ctx context.Context, reqs []ToolUsageEventRequest, batchErr error) error {
	for _, req := range reqs {
		if err := c.SendToolUsageEvent(ctx, req); err != nil {
			return fmt.Errorf("%w; fallback single upload failed: %w", batchErr, err)
		}
	}
	return nil
}

func isRetryableToolUsageStatus(status int) bool {
	return status == http.StatusTooManyRequests ||
		status == http.StatusBadGateway ||
		status == http.StatusServiceUnavailable ||
		status == http.StatusGatewayTimeout
}

func sleepWithContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) EnsureRepoFromRemote(ctx context.Context, remoteURL, branch string) (*RepoEnsureResponse, error) {
	payload := map[string]string{
		"remote_url": remoteURL,
		"branch":     branch,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal ensure repo request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/repos/ensure-remote", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create ensure repo request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send ensure repo request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read ensure repo response: %w", err)
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("unexpected ensure repo status %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		Data RepoEnsureResponse `json:"data"`
	}
	if err := json.Unmarshal(respBody, &envelope); err != nil {
		return nil, fmt.Errorf("decode ensure repo response: %w", err)
	}
	return &envelope.Data, nil
}

func (c *Client) ResolveRepoFromRemote(ctx context.Context, req ResolveRepoRequest) (*RepoEligibilityResponse, error) {
	if req.ClientCacheVersion == "" {
		req.ClientCacheVersion = RepoEligibilityVersion
	}
	var envelope struct {
		Data RepoEligibilityResponse `json:"data"`
	}
	if err := c.postJSON(ctx, "/api/v1/repos/resolve-remote", req, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func (c *Client) BatchHookEligible(ctx context.Context, repos []HookEligibleRepoRequest) (*BatchHookEligibleResponse, error) {
	var envelope struct {
		Data BatchHookEligibleResponse `json:"data"`
	}
	if err := c.postJSON(ctx, "/api/v1/repos/hook-eligible", map[string]any{"repos": repos}, &envelope); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func (c *Client) ListProviders(ctx context.Context) ([]ProviderInfo, error) {
	providers, err := c.listUserProviders(ctx)
	if err == nil && len(providers) > 0 {
		return providers, nil
	}
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, err
	}
	return c.listLegacyProviders(ctx)
}

func (c *Client) listLegacyProviders(ctx context.Context) ([]ProviderInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/providers", nil)
	if err != nil {
		return nil, fmt.Errorf("create providers request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send providers request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected providers status %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		Data struct {
			Providers []ProviderInfo `json:"providers"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode providers response: %w", err)
	}
	return envelope.Data.Providers, nil
}

func (c *Client) listUserProviders(ctx context.Context) ([]ProviderInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/user/providers", nil)
	if err != nil {
		return nil, fmt.Errorf("create user providers request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("send user providers request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected user providers status %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		Data struct {
			Providers []struct {
				Name         string              `json:"name"`
				DisplayName  string              `json:"display_name"`
				BaseURL      string              `json:"base_url"`
				DefaultModel string              `json:"default_model"`
				IsPrimary    bool                `json:"is_primary"`
				Groups       []userProviderGroup `json:"groups"`
			} `json:"providers"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decode user providers response: %w", err)
	}

	out := make([]ProviderInfo, 0, len(envelope.Data.Providers))
	for _, provider := range envelope.Data.Providers {
		credentials := selectUserProviderCredentials(provider.Groups)
		apiKey, apiKeyID := firstUserProviderCredential(credentials)
		out = append(out, ProviderInfo{
			Name:         provider.Name,
			DisplayName:  provider.DisplayName,
			BaseURL:      provider.BaseURL,
			APIKey:       apiKey,
			APIKeyID:     apiKeyID,
			DefaultModel: provider.DefaultModel,
			IsPrimary:    provider.IsPrimary,
			Credentials:  credentials,
		})
	}
	return out, nil
}

func selectUserProviderCredential(groups []userProviderGroup) (string, int64) {
	return firstUserProviderCredential(selectUserProviderCredentials(groups))
}

func selectUserProviderCredentials(groups []userProviderGroup) []ProviderCredentialInfo {
	out := make([]ProviderCredentialInfo, 0, len(groups))
	for _, group := range groups {
		key := strings.TrimSpace(group.Credential.Key)
		status := strings.TrimSpace(group.Credential.Status)
		state := strings.TrimSpace(group.Credential.State)
		platform := strings.TrimSpace(group.Platform)
		if key == "" || platform == "" {
			continue
		}
		if strings.EqualFold(state, "missing") {
			continue
		}
		if status != "" && !strings.EqualFold(status, "active") {
			continue
		}
		out = append(out, ProviderCredentialInfo{
			Platform:  platform,
			GroupID:   strings.TrimSpace(group.GroupID),
			GroupName: strings.TrimSpace(group.GroupName),
			APIKey:    key,
			APIKeyID:  group.Credential.APIKeyID,
			Status:    firstNonEmptyString(status, "active"),
		})
	}
	return out
}

func firstUserProviderCredential(credentials []ProviderCredentialInfo) (string, int64) {
	if len(credentials) == 0 {
		return "", 0
	}
	return credentials[0].APIKey, credentials[0].APIKeyID
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

func (c *Client) AuthToken() string {
	if c == nil {
		return ""
	}
	return c.token
}

func (c *Client) postJSON(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
