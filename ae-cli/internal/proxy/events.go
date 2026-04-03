package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type EventEnvelope struct {
	EventType string `json:"event_type"`
	SessionID string `json:"session_id,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

const eventSpoolFileName = "proxy-session-events.jsonl"

func EventSpoolPath(sessionID string) string {
	return filepath.Join(runtimeQueueDir(sessionID), eventSpoolFileName)
}

func AppendDurableEvent(sessionID string, event EventEnvelope) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is empty")
	}
	event.SessionID = sessionID

	queueDir := runtimeQueueDir(sessionID)
	if err := os.MkdirAll(queueDir, 0o700); err != nil {
		return fmt.Errorf("create event queue dir: %w", err)
	}

	line, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event spool line: %w", err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(EventSpoolPath(sessionID), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open event spool file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("write event spool line: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync event spool file: %w", err)
	}
	return nil
}

func runtimeQueueDir(sessionID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".ae-cli", "runtime", sessionID, "queue")
}

func PostEvent(ctx context.Context, listenAddr, authToken string, event EventEnvelope) error {
	listenAddr = strings.TrimSpace(listenAddr)
	if listenAddr == "" {
		return fmt.Errorf("listen addr is empty")
	}

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal proxy event: %w", err)
	}

	baseURL := listenAddr
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}
	url := strings.TrimRight(baseURL, "/") + "/api/v1/session-events"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create proxy event request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token := strings.TrimSpace(authToken); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("send proxy event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unexpected proxy event status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

type checkpointIngressPayload struct {
	EventID        string         `json:"event_id"`
	RepoFullName   string         `json:"repo_full_name"`
	WorkspaceID    string         `json:"workspace_id"`
	BindingSource  string         `json:"binding_source"`
	CommitSHA      string         `json:"commit_sha"`
	ParentSHAs     []string       `json:"parent_shas"`
	BranchSnapshot string         `json:"branch_snapshot"`
	HeadSnapshot   string         `json:"head_snapshot"`
	AgentSnapshot  map[string]any `json:"agent_snapshot"`
	CapturedAt     string         `json:"captured_at"`
}

type rewriteIngressPayload struct {
	EventID       string `json:"event_id"`
	RepoFullName  string `json:"repo_full_name"`
	WorkspaceID   string `json:"workspace_id"`
	BindingSource string `json:"binding_source"`
	RewriteType   string `json:"rewrite_type"`
	OldCommitSHA  string `json:"old_commit_sha"`
	NewCommitSHA  string `json:"new_commit_sha"`
	CapturedAt    string `json:"captured_at"`
}

type sessionIngressPayload struct {
	EventID     string `json:"event_id"`
	WorkspaceID string `json:"workspace_id"`
	Source      string `json:"source"`
	CapturedAt  string `json:"captured_at"`
}

func (s *Server) ingestSessionEvent(event EventEnvelope) error {
	if s.backendClient == nil {
		return fmt.Errorf("backend client is not configured")
	}

	eventType := strings.TrimSpace(event.EventType)
	switch eventType {
	case "post_commit":
		req, err := s.buildCommitCheckpointRequest(event)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return s.backendClient.SendCommitCheckpoint(ctx, req)
	case "post_rewrite":
		req, err := s.buildCommitRewriteRequest(event)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return s.backendClient.SendCommitRewrite(ctx, req)
	default:
		req, err := s.buildSessionEventRequest(event)
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return s.backendClient.SendSessionEvent(ctx, req)
	}
}

func (s *Server) buildCommitCheckpointRequest(event EventEnvelope) (client.CommitCheckpointRequest, error) {
	var payload checkpointIngressPayload
	if err := decodePayload(event.Payload, &payload); err != nil {
		return client.CommitCheckpointRequest{}, fmt.Errorf("decode post_commit payload: %w", err)
	}
	capturedAt, err := parseCapturedAtPtr(payload.CapturedAt)
	if err != nil {
		return client.CommitCheckpointRequest{}, fmt.Errorf("parse post_commit captured_at: %w", err)
	}
	eventID := strings.TrimSpace(payload.EventID)
	if eventID == "" {
		eventID = newRequestID()
	}
	workspaceID := firstNonEmptyHeader(payload.WorkspaceID, s.cfg.WorkspaceID)
	bindingSource := firstNonEmptyHeader(payload.BindingSource, "marker")

	return client.CommitCheckpointRequest{
		EventID:        eventID,
		SessionID:      strings.TrimSpace(s.cfg.SessionID),
		RepoFullName:   strings.TrimSpace(payload.RepoFullName),
		WorkspaceID:    workspaceID,
		CommitSHA:      strings.TrimSpace(payload.CommitSHA),
		ParentSHAs:     payload.ParentSHAs,
		BranchSnapshot: strings.TrimSpace(payload.BranchSnapshot),
		HeadSnapshot:   strings.TrimSpace(payload.HeadSnapshot),
		BindingSource:  bindingSource,
		AgentSnapshot:  payload.AgentSnapshot,
		CapturedAt:     capturedAt,
	}, nil
}

func (s *Server) buildCommitRewriteRequest(event EventEnvelope) (client.CommitRewriteRequest, error) {
	var payload rewriteIngressPayload
	if err := decodePayload(event.Payload, &payload); err != nil {
		return client.CommitRewriteRequest{}, fmt.Errorf("decode post_rewrite payload: %w", err)
	}
	capturedAt, err := parseCapturedAtPtr(payload.CapturedAt)
	if err != nil {
		return client.CommitRewriteRequest{}, fmt.Errorf("parse post_rewrite captured_at: %w", err)
	}
	eventID := strings.TrimSpace(payload.EventID)
	if eventID == "" {
		eventID = newRequestID()
	}
	workspaceID := firstNonEmptyHeader(payload.WorkspaceID, s.cfg.WorkspaceID)
	bindingSource := firstNonEmptyHeader(payload.BindingSource, "marker")

	return client.CommitRewriteRequest{
		EventID:       eventID,
		SessionID:     strings.TrimSpace(s.cfg.SessionID),
		RepoFullName:  strings.TrimSpace(payload.RepoFullName),
		WorkspaceID:   workspaceID,
		RewriteType:   strings.TrimSpace(payload.RewriteType),
		OldCommitSHA:  strings.TrimSpace(payload.OldCommitSHA),
		NewCommitSHA:  strings.TrimSpace(payload.NewCommitSHA),
		BindingSource: bindingSource,
		CapturedAt:    capturedAt,
	}, nil
}

func (s *Server) buildSessionEventRequest(event EventEnvelope) (client.SessionEventRequest, error) {
	var payload sessionIngressPayload
	if err := decodePayload(event.Payload, &payload); err != nil {
		return client.SessionEventRequest{}, fmt.Errorf("decode session event payload: %w", err)
	}
	capturedAt, err := parseCapturedAt(payload.CapturedAt)
	if err != nil {
		return client.SessionEventRequest{}, fmt.Errorf("parse session event captured_at: %w", err)
	}
	rawPayload, err := payloadMap(event.Payload)
	if err != nil {
		return client.SessionEventRequest{}, fmt.Errorf("encode session event raw payload: %w", err)
	}
	eventID := strings.TrimSpace(payload.EventID)
	if eventID == "" {
		eventID = newRequestID()
	}
	workspaceID := firstNonEmptyHeader(payload.WorkspaceID, s.cfg.WorkspaceID)
	source := firstNonEmptyHeader(payload.Source, "proxy")

	return client.SessionEventRequest{
		EventID:     eventID,
		SessionID:   strings.TrimSpace(s.cfg.SessionID),
		WorkspaceID: workspaceID,
		EventType:   strings.TrimSpace(event.EventType),
		Source:      source,
		CapturedAt:  capturedAt,
		RawPayload:  rawPayload,
	}, nil
}

func decodePayload(payload any, out any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 || string(b) == "null" {
		b = []byte("{}")
	}
	if err := json.Unmarshal(b, out); err != nil {
		return err
	}
	return nil
}

func payloadMap(payload any) (map[string]any, error) {
	if payload == nil {
		return map[string]any{}, nil
	}
	if m, ok := payload.(map[string]any); ok {
		return m, nil
	}
	var out map[string]any
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func parseCapturedAtPtr(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	ts = ts.UTC()
	return &ts, nil
}

func parseCapturedAt(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC(), nil
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, err
	}
	return ts.UTC(), nil
}
