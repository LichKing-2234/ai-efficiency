package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type anthropicUsage struct {
	Model        string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	expectedToken := strings.TrimSpace(s.cfg.AuthToken)
	reqToken := strings.TrimSpace(r.Header.Get("x-api-key"))
	if reqToken == "" {
		reqToken = strings.TrimPrefix(strings.TrimSpace(r.Header.Get("Authorization")), "Bearer ")
		reqToken = strings.TrimSpace(reqToken)
	}
	if expectedToken == "" || reqToken != expectedToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	reqID := newRequestID()
	startedAt := time.Now().UTC()

	upstreamURL := strings.TrimRight(s.cfg.ProviderURL, "/") + "/v1/messages"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, r.Body)
	if err != nil {
		s.recorder.RecordUsage(UsageEvent{
			SessionID:    s.cfg.SessionID,
			RequestID:    reqID,
			ProviderName: "sub2api",
			StartedAt:    startedAt,
			FinishedAt:   time.Now().UTC(),
			HTTPStatus:   http.StatusBadGateway,
			Error:        err.Error(),
			Status:       "proxy_request_build_failed",
		})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyJSONHeaders(upstreamReq.Header, r.Header)
	upstreamReq.Header.Set("x-api-key", strings.TrimSpace(s.cfg.ProviderKey))
	upstreamReq.Header.Set("anthropic-version", firstNonEmptyHeader(r.Header.Get("anthropic-version"), "2023-06-01"))

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		s.recorder.RecordUsage(UsageEvent{
			SessionID:    s.cfg.SessionID,
			RequestID:    reqID,
			ProviderName: "sub2api",
			StartedAt:    startedAt,
			FinishedAt:   time.Now().UTC(),
			HTTPStatus:   http.StatusBadGateway,
			Error:        err.Error(),
			Status:       "upstream_transport_error",
		})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		s.recorder.RecordUsage(UsageEvent{
			SessionID:    s.cfg.SessionID,
			RequestID:    reqID,
			ProviderName: "sub2api",
			StartedAt:    startedAt,
			FinishedAt:   time.Now().UTC(),
			HTTPStatus:   resp.StatusCode,
			Error:        readErr.Error(),
			Status:       "upstream_read_error",
		})
		http.Error(w, readErr.Error(), http.StatusBadGateway)
		return
	}

	usage := parseAnthropicUsage(body)
	status := "completed"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "upstream_http_error"
	}
	s.recorder.RecordUsage(UsageEvent{
		SessionID:    s.cfg.SessionID,
		RequestID:    reqID,
		ProviderName: "sub2api",
		Model:        usage.Model,
		StartedAt:    startedAt,
		FinishedAt:   time.Now().UTC(),
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		HTTPStatus:   resp.StatusCode,
		Status:       status,
	})

	copyResponse(w, resp.StatusCode, resp.Header, body)
}

func parseAnthropicUsage(body []byte) anthropicUsage {
	var payload struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return anthropicUsage{}
	}
	total := payload.Usage.TotalTokens
	if total <= 0 {
		total = payload.Usage.InputTokens + payload.Usage.OutputTokens
	}
	return anthropicUsage{
		Model:        payload.Model,
		InputTokens:  payload.Usage.InputTokens,
		OutputTokens: payload.Usage.OutputTokens,
		TotalTokens:  total,
	}
}

func firstNonEmptyHeader(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
