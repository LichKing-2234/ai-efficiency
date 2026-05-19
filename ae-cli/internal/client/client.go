package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ErrNotFound is returned when the backend responds with 404.
var ErrNotFound = errors.New("not found")

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type ProviderInfo struct {
	Name         string `json:"name"`
	DisplayName  string `json:"display_name"`
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`
	APIKeyID     int64  `json:"api_key_id"`
	DefaultModel string `json:"default_model"`
	IsPrimary    bool   `json:"is_primary"`
}

type CommitCheckpointRequest struct {
	EventID        string         `json:"event_id"`
	SessionID      string         `json:"session_id,omitempty"`
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
	RepoFullName  string     `json:"repo_full_name"`
	WorkspaceID   string     `json:"workspace_id"`
	RewriteType   string     `json:"rewrite_type"`
	OldCommitSHA  string     `json:"old_commit_sha"`
	NewCommitSHA  string     `json:"new_commit_sha"`
	BindingSource string     `json:"binding_source"`
	CapturedAt    *time.Time `json:"captured_at,omitempty"`
}

type ToolUsageEventRequest struct {
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/tool-usage-events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create tool usage event request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send tool usage event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected tool usage status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) ListProviders(ctx context.Context) ([]ProviderInfo, error) {
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

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}
