package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type EventEnvelope struct {
	EventType string `json:"event_type"`
	SessionID string `json:"session_id,omitempty"`
	Payload   any    `json:"payload,omitempty"`
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
