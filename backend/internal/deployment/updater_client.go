package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ApplyRequest struct {
	TargetVersion string `json:"target_version"`
}

type UpdateStatus struct {
	Phase         string `json:"phase"`
	TargetVersion string `json:"target_version,omitempty"`
	Message       string `json:"message,omitempty"`
}

type Updater interface {
	Status(context.Context) (UpdateStatus, error)
	Apply(context.Context, ApplyRequest) (UpdateStatus, error)
	Rollback(context.Context) (UpdateStatus, error)
}

type UpdaterClient struct {
	baseURL string
	client  *http.Client
}

func NewUpdaterClient(client *http.Client, baseURL string) *UpdaterClient {
	if client == nil {
		client = http.DefaultClient
	}
	return &UpdaterClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		client:  client,
	}
}

func (c *UpdaterClient) Status(ctx context.Context) (UpdateStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/status", nil)
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("build status request: %w", err)
	}
	return c.do(req, "status")
}

func (c *UpdaterClient) Apply(ctx context.Context, reqBody ApplyRequest) (UpdateStatus, error) {
	body, err := json.Marshal(reqBody)
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("marshal apply request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/apply", bytes.NewReader(body))
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("build apply request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, "apply")
}

func (c *UpdaterClient) Rollback(ctx context.Context) (UpdateStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/rollback", nil)
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("build rollback request: %w", err)
	}
	return c.do(req, "rollback")
}

func (c *UpdaterClient) do(req *http.Request, op string) (UpdateStatus, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return UpdateStatus{}, fmt.Errorf("request updater %s: %w", op, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return UpdateStatus{}, fmt.Errorf("updater %s failed: status=%d body=%q", op, resp.StatusCode, string(body))
	}

	var out UpdateStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return UpdateStatus{}, fmt.Errorf("decode updater %s response: %w", op, err)
	}
	return out, nil
}
