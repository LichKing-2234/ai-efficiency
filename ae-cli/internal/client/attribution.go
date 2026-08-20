package client

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
)

type EnsureInstallationRequest struct {
	InstallationID string `json:"installation_id"`
	Label          string `json:"label,omitempty"`
	ClientVersion  string `json:"client_version,omitempty"`
}

type InstallationCredentials struct {
	InstallationID   string              `json:"installation_id"`
	ReporterToken    string              `json:"reporter_token,omitempty"`
	Created          bool                `json:"created"`
	ReportingEnabled bool                `json:"reporting_enabled"`
	Protocol         AttributionProtocol `json:"protocol"`
}

const (
	AttributionLedgerEpochShadowV2        = "shadow_v2"
	AttributionLedgerEpochFormalV2        = "formal_v2"
	AttributionV1WritePolicyAccept        = "accept"
	AttributionV1WritePolicyUpgradeNeeded = "upgrade_required"
)

type AttributionProtocol struct {
	LedgerEpoch       string `json:"ledger_epoch"`
	V1WritePolicy     string `json:"v1_write_policy"`
	MinimumCLIVersion string `json:"minimum_cli_version,omitempty"`
}

type AttributionUpgradeRequiredError struct {
	MinimumCLIVersion string
	Message           string
}

func (e *AttributionUpgradeRequiredError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "ae-cli upgrade required"
	}
	if minimum := strings.TrimSpace(e.MinimumCLIVersion); minimum != "" {
		return fmt.Sprintf("%s (minimum_cli_version=%s)", message, minimum)
	}
	return message
}

func (p AttributionProtocol) Validate() error {
	epoch := strings.TrimSpace(p.LedgerEpoch)
	policy := strings.TrimSpace(p.V1WritePolicy)
	minimum := strings.TrimSpace(p.MinimumCLIVersion)
	switch {
	case epoch == AttributionLedgerEpochShadowV2 && policy == AttributionV1WritePolicyAccept && minimum == "":
		return nil
	case (epoch == AttributionLedgerEpochShadowV2 || epoch == AttributionLedgerEpochFormalV2) && policy == AttributionV1WritePolicyUpgradeNeeded && minimum != "":
		return nil
	default:
		return fmt.Errorf("unsupported or contradictory attribution protocol: ledger_epoch=%q v1_write_policy=%q minimum_cli_version=%q", epoch, policy, minimum)
	}
}

type SetInstallationEnabledRequest struct {
	ReportingEnabled *bool `json:"reporting_enabled,omitempty"`
	OTelEnabled      *bool `json:"otel_enabled,omitempty"`
}

type AttributionV2ClaimGroup struct {
	SchemaVersion     int                             `json:"schema_version"`
	GroupID           string                          `json:"group_id"`
	RelayProviderID   int                             `json:"relay_provider_id"`
	TokenSource       string                          `json:"token_source,omitempty"`
	ThreadID          string                          `json:"thread_id"`
	TurnID            string                          `json:"turn_id"`
	EvidenceDigest    string                          `json:"evidence_digest"`
	Calibration       *AttributionV2Calibration       `json:"calibration,omitempty"`
	LocalUsage        []AttributionV2LocalUsageBucket `json:"local_usage,omitempty"`
	CommitAllocations []AttributionV2CommitAllocation `json:"commit_allocations"`
	RequestIDs        []string                        `json:"request_ids"`
}

const (
	AttributionV2TokenSourceRelayOfficial = "relay_official"
	AttributionV2TokenSourceCodexLocal    = "codex_local"
)

type AttributionV2LocalUsageBucket struct {
	RequestedModel      string    `json:"requested_model"`
	BucketStartUTC      time.Time `json:"bucket_start_utc"`
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	TotalTokens         int64     `json:"total_tokens"`
	RequestCount        int       `json:"request_count"`
}

type AttributionV2Calibration struct {
	Digest              string `json:"digest"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	CacheCreationTokens int64  `json:"cache_creation_tokens"`
	CacheReadTokens     int64  `json:"cache_read_tokens"`
	TotalTokens         int64  `json:"total_tokens"`
}

type AttributionV2CommitAllocation struct {
	Sequence          int    `json:"sequence"`
	RepoConfigID      int    `json:"repo_config_id"`
	RepoKey           string `json:"repo_key,omitempty"`
	WorkspaceID       string `json:"workspace_id"`
	CheckpointEventID string `json:"checkpoint_event_id"`
	CommitSHA         string `json:"commit_sha"`
	EvidenceDigest    string `json:"evidence_digest"`
}

type AttributionV2ClaimBatchRequest struct {
	Groups []AttributionV2ClaimGroup `json:"groups"`
}

type AttributionV2ItemStatus struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

type AttributionV2ClaimResult struct {
	Group       AttributionV2ItemStatus   `json:"group"`
	Calibration AttributionV2ItemStatus   `json:"calibration"`
	Requests    []AttributionV2ItemStatus `json:"requests"`
}

type AttributionV2ClaimBatchResult struct {
	LedgerEpoch       string                     `json:"ledger_epoch"`
	V1WritePolicy     string                     `json:"v1_write_policy"`
	MinimumCLIVersion string                     `json:"minimum_cli_version,omitempty"`
	Results           []AttributionV2ClaimResult `json:"results"`
}

func (r AttributionV2ClaimBatchResult) Protocol() AttributionProtocol {
	return AttributionProtocol{LedgerEpoch: r.LedgerEpoch, V1WritePolicy: r.V1WritePolicy, MinimumCLIVersion: r.MinimumCLIVersion}
}

func (c *Client) EnsureAttributionInstallation(ctx context.Context, req EnsureInstallationRequest) (*InstallationCredentials, error) {
	var response struct {
		Data InstallationCredentials `json:"data"`
	}
	if err := c.doAttributionJSON(ctx, http.MethodPost, "/api/v1/attribution/installations", req, &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}

func (c *Client) SetAttributionInstallationEnabled(ctx context.Context, installationID string, req SetInstallationEnabledRequest) (*InstallationCredentials, error) {
	var response struct {
		Data InstallationCredentials `json:"data"`
	}
	path := "/api/v1/attribution/installations/" + url.PathEscape(strings.TrimSpace(installationID))
	if err := c.doAttributionJSON(ctx, http.MethodPut, path, req, &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}

func (c *Client) RotateAttributionInstallationCredentials(ctx context.Context, installationID string) (*InstallationCredentials, error) {
	var response struct {
		Data InstallationCredentials `json:"data"`
	}
	path := "/api/v1/attribution/installations/" + url.PathEscape(strings.TrimSpace(installationID)) + "/credentials/rotate"
	if err := c.doAttributionJSON(ctx, http.MethodPost, path, struct{}{}, &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}

func (c *Client) SendAttributionV2Claims(ctx context.Context, groups []AttributionV2ClaimGroup) (*AttributionV2ClaimBatchResult, error) {
	var response struct {
		Data AttributionV2ClaimBatchResult `json:"data"`
	}
	if err := c.doAttributionJSON(ctx, http.MethodPost, "/api/v1/attribution/v2/claim-groups/batch", AttributionV2ClaimBatchRequest{Groups: groups}, &response); err != nil {
		return nil, err
	}
	return &response.Data, nil
}

func (c *Client) EnsureAttributionRepoFromRemote(ctx context.Context, req ResolveRepoRequest) (*RepoEligibilityResponse, error) {
	var response struct {
		Data RepoEligibilityResponse `json:"data"`
	}
	if err := c.doAttributionJSON(ctx, http.MethodPost, "/api/v1/attribution/repos/ensure-remote", req, &response); err != nil {
		return nil, fmt.Errorf("ensure attribution repository: %w", err)
	}
	return &response.Data, nil
}

func (c *Client) SendAttributionCommitCheckpoint(ctx context.Context, req CommitCheckpointRequest) error {
	return c.doAttributionJSON(ctx, http.MethodPost, "/api/v1/attribution/checkpoints/commit", req, nil)
}

func (c *Client) SendAttributionCommitRewrite(ctx context.Context, req CommitRewriteRequest) error {
	return c.doAttributionJSON(ctx, http.MethodPost, "/api/v1/attribution/checkpoints/rewrite", req, nil)
}

func (c *Client) doAttributionJSON(ctx context.Context, method, path string, request any, response any) error {
	body, err := json.Marshal(request)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	c.setHeaders(httpReq)
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var apiError struct {
			Message string `json:"message"`
			Details struct {
				ErrorCode         string `json:"error_code"`
				MinimumCLIVersion string `json:"minimum_cli_version"`
			} `json:"details"`
		}
		if resp.StatusCode == http.StatusConflict && json.Unmarshal(payload, &apiError) == nil && strings.TrimSpace(apiError.Details.ErrorCode) == AttributionV1WritePolicyUpgradeNeeded {
			return &AttributionUpgradeRequiredError{MinimumCLIVersion: strings.TrimSpace(apiError.Details.MinimumCLIVersion), Message: strings.TrimSpace(apiError.Message)}
		}
		return fmt.Errorf("attribution endpoint %s returned %d: %s", path, resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	if response != nil && len(payload) > 0 {
		if err := json.Unmarshal(payload, response); err != nil {
			return err
		}
	}
	return nil
}
