package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ErrNotFound is returned when the backend responds with 404.
var ErrNotFound = errors.New("not found")

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type CreateSessionRequest struct {
	ID           string                   `json:"id"`
	RepoFullName string                   `json:"repo_full_name"`
	Branch       string                   `json:"branch"`
	ToolConfigs  []map[string]interface{} `json:"tool_configs,omitempty"`
}

type Session struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
}

type Invocation struct {
	Tool  string    `json:"tool"`
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type BootstrapSessionRequest struct {
	RepoFullName   string `json:"repo_full_name"`
	BranchSnapshot string `json:"branch_snapshot"`
	HeadSHA        string `json:"head_sha"`
	WorkspaceRoot  string `json:"workspace_root"`
	GitDir         string `json:"git_dir"`
	GitCommonDir   string `json:"git_common_dir"`
	WorkspaceID    string `json:"workspace_id"`
}

type BootstrapSessionResponse struct {
	SessionID          string            `json:"session_id"`
	StartedAt          time.Time         `json:"started_at"`
	RelayUserID        int64             `json:"relay_user_id"`
	RelayAPIKeyID      int64             `json:"relay_api_key_id"`
	ProviderName       string            `json:"provider_name"`
	GroupID            string            `json:"group_id"`
	RouteBindingSource string            `json:"route_binding_source"`
	RuntimeRef         string            `json:"runtime_ref"`
	EnvBundle          map[string]string `json:"env_bundle"`
	KeyExpiresAt       time.Time         `json:"key_expires_at"`
}

type ProviderCredential struct {
	ProviderName string `json:"provider_name"`
	Platform     string `json:"platform"`
	APIKeyID     int64  `json:"api_key_id"`
	APIKey       string `json:"api_key"`
	BaseURL      string `json:"base_url"`
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

type SessionEventRequest struct {
	EventID     string         `json:"event_id"`
	SessionID   string         `json:"session_id"`
	WorkspaceID string         `json:"workspace_id"`
	EventType   string         `json:"event_type"`
	Source      string         `json:"source"`
	CapturedAt  time.Time      `json:"captured_at"`
	RawPayload  map[string]any `json:"raw_payload,omitempty"`
}

type SessionUsageEventRequest struct {
	EventID      string         `json:"event_id"`
	SessionID    string         `json:"session_id"`
	WorkspaceID  string         `json:"workspace_id"`
	RequestID    string         `json:"request_id"`
	ProviderName string         `json:"provider_name"`
	Model        string         `json:"model"`
	StartedAt    time.Time      `json:"started_at"`
	FinishedAt   time.Time      `json:"finished_at"`
	InputTokens  int64          `json:"input_tokens"`
	OutputTokens int64          `json:"output_tokens"`
	TotalTokens  int64          `json:"total_tokens"`
	Status       string         `json:"status"`
	RawMetadata  map[string]any `json:"raw_metadata,omitempty"`
	RawResponse  map[string]any `json:"raw_response,omitempty"`
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

func (c *Client) CreateSession(ctx context.Context, req CreateSessionRequest) (*Session, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/sessions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	// Backend wraps response in {"code":..., "data":...}
	var envelope struct {
		Data Session `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &envelope.Data, nil
}

func (c *Client) BootstrapSession(ctx context.Context, req BootstrapSessionRequest) (*BootstrapSessionResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/sessions/bootstrap", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		Data BootstrapSessionResponse `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &envelope.Data, nil
}

func (c *Client) Heartbeat(ctx context.Context, sessionID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, c.baseURL+"/api/v1/sessions/"+sessionID, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *Client) StopSession(ctx context.Context, sessionID string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/sessions/"+sessionID+"/stop", nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// GetSession checks whether a session exists on the backend.
func (c *Client) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/sessions/"+sessionID, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		Data Session `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}

	return &envelope.Data, nil
}

func (c *Client) GetSessionProviderCredential(ctx context.Context, sessionID, platform string) (*ProviderCredential, error) {
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		c.baseURL+"/api/v1/sessions/"+sessionID+"/provider-credentials?platform="+url.QueryEscape(platform),
		nil,
	)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var envelope struct {
		Data ProviderCredential `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &envelope.Data, nil
}

func (c *Client) AddInvocation(ctx context.Context, sessionID string, inv Invocation) error {
	body, err := json.Marshal(inv)
	if err != nil {
		return fmt.Errorf("marshaling invocation: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/sessions/"+sessionID+"/invocations", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
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

func (c *Client) SendSessionEvent(ctx context.Context, req SessionEventRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal session event: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/session-events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create session event request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send session event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected session event status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (c *Client) SendSessionUsageEvent(ctx context.Context, req SessionUsageEventRequest) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal session usage event: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/session-usage-events", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create session usage event request: %w", err)
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send session usage event: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected session usage status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
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
