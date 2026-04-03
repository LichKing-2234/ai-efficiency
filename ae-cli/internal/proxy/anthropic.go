package proxy

import (
	"bytes"
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
	requestBody, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		s.recordUsage(UsageEvent{
			SessionID:    s.cfg.SessionID,
			WorkspaceID:  s.cfg.WorkspaceID,
			RequestID:    reqID,
			ProviderName: "sub2api",
			StartedAt:    startedAt,
			FinishedAt:   time.Now().UTC(),
			HTTPStatus:   http.StatusBadRequest,
			Error:        readErr.Error(),
			Status:       "proxy_request_read_failed",
		})
		http.Error(w, readErr.Error(), http.StatusBadRequest)
		return
	}
	requestMeta := parseAnthropicRequestMeta(requestBody)

	upstreamURL := strings.TrimRight(s.cfg.ProviderURL, "/") + "/v1/messages"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(requestBody))
	if err != nil {
		s.recordUsage(UsageEvent{
			SessionID:    s.cfg.SessionID,
			WorkspaceID:  s.cfg.WorkspaceID,
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
	copyAnthropicHeaders(upstreamReq.Header, r.Header)
	upstreamReq.Header.Set("x-api-key", strings.TrimSpace(s.cfg.ProviderKey))
	upstreamReq.Header.Set("anthropic-version", firstNonEmptyHeader(r.Header.Get("anthropic-version"), "2023-06-01"))

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		s.recordUsage(UsageEvent{
			SessionID:    s.cfg.SessionID,
			WorkspaceID:  s.cfg.WorkspaceID,
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

	if requestMeta.Stream && isSSEContentType(resp.Header.Get("Content-Type")) {
		s.proxyAnthropicStream(w, resp, reqID, startedAt, requestMeta.Model)
		return
	}

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		s.recordUsage(UsageEvent{
			SessionID:    s.cfg.SessionID,
			WorkspaceID:  s.cfg.WorkspaceID,
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
	s.recordUsage(UsageEvent{
		SessionID:    s.cfg.SessionID,
		WorkspaceID:  s.cfg.WorkspaceID,
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
		Model string               `json:"model"`
		Usage anthropicUsageFields `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return anthropicUsage{}
	}
	usage := composeAnthropicUsage(payload.Model, payload.Usage)
	if usage.TotalTokens <= 0 {
		return anthropicUsage{}
	}
	return usage
}

type anthropicRequestMeta struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func parseAnthropicRequestMeta(body []byte) anthropicRequestMeta {
	var meta anthropicRequestMeta
	_ = json.Unmarshal(body, &meta)
	return meta
}

type anthropicUsageFields struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	TotalTokens              int `json:"total_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func composeAnthropicUsage(model string, usage anthropicUsageFields) anthropicUsage {
	inputTotal := usage.InputTokens + usage.CacheCreationInputTokens + usage.CacheReadInputTokens
	total := usage.TotalTokens
	if inputTotal+usage.OutputTokens > total {
		total = inputTotal + usage.OutputTokens
	}
	return anthropicUsage{
		Model:        model,
		InputTokens:  inputTotal,
		OutputTokens: usage.OutputTokens,
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

func copyAnthropicHeaders(dst, src http.Header) {
	for k, values := range src {
		if !strings.EqualFold(k, "anthropic-beta") {
			continue
		}
		for _, v := range values {
			dst.Add("anthropic-beta", v)
		}
	}
}

func isSSEContentType(contentType string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(contentType)), "text/event-stream")
}

func (s *Server) proxyAnthropicStream(w http.ResponseWriter, resp *http.Response, reqID string, startedAt time.Time, model string) {
	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)

	acc := newAnthropicSSEUsageAccumulator(model)
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			acc.Consume(chunk)
			if _, writeErr := w.Write(chunk); writeErr != nil {
				usage, ok := acc.Usage()
				s.recordAnthropicUsage(reqID, startedAt, resp.StatusCode, usage, ok, "downstream_write_error", writeErr.Error())
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}

		if err == io.EOF {
			break
		}
		if err != nil {
			usage, ok := acc.Usage()
			s.recordAnthropicUsage(reqID, startedAt, resp.StatusCode, usage, ok, "upstream_read_error", err.Error())
			return
		}
	}

	usage, ok := acc.Usage()
	status := "completed"
	errMessage := ""
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "upstream_http_error"
	}
	if status == "completed" && !ok {
		status = "stream_usage_unavailable"
		errMessage = "stream completed without usage payload"
	}
	s.recordAnthropicUsage(reqID, startedAt, resp.StatusCode, usage, ok, status, errMessage)
}

func (s *Server) recordAnthropicUsage(reqID string, startedAt time.Time, httpStatus int, usage anthropicUsage, hasUsage bool, status string, errMessage string) {
	model := usage.Model
	inputTokens := usage.InputTokens
	outputTokens := usage.OutputTokens
	totalTokens := usage.TotalTokens
	if !hasUsage {
		model = ""
		inputTokens = 0
		outputTokens = 0
		totalTokens = 0
	}
	s.recordUsage(UsageEvent{
		SessionID:    s.cfg.SessionID,
		WorkspaceID:  s.cfg.WorkspaceID,
		RequestID:    reqID,
		ProviderName: "sub2api",
		Model:        model,
		StartedAt:    startedAt,
		FinishedAt:   time.Now().UTC(),
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
		HTTPStatus:   httpStatus,
		Error:        errMessage,
		Status:       status,
	})
}

type anthropicSSEUsageAccumulator struct {
	pending string
	model   string
	usage   anthropicUsageFields
	seen    bool
}

func newAnthropicSSEUsageAccumulator(model string) *anthropicSSEUsageAccumulator {
	return &anthropicSSEUsageAccumulator{model: model}
}

func (a *anthropicSSEUsageAccumulator) Consume(chunk []byte) {
	data := a.pending + string(chunk)
	lines := strings.Split(data, "\n")
	a.pending = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		a.consumeLine(strings.TrimSpace(strings.TrimSuffix(line, "\r")))
	}
}

func (a *anthropicSSEUsageAccumulator) consumeLine(line string) {
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}

	var event struct {
		Type    string               `json:"type"`
		Usage   anthropicUsageFields `json:"usage"`
		Message struct {
			Model string               `json:"model"`
			Usage anthropicUsageFields `json:"usage"`
		} `json:"message"`
	}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return
	}

	if event.Message.Model != "" {
		a.model = event.Message.Model
	}
	a.applyUsage(event.Message.Usage)
	a.applyUsage(event.Usage)
}

func (a *anthropicSSEUsageAccumulator) applyUsage(usage anthropicUsageFields) {
	if usage.InputTokens > 0 {
		a.usage.InputTokens = max(a.usage.InputTokens, usage.InputTokens)
		a.seen = true
	}
	if usage.OutputTokens > 0 {
		a.usage.OutputTokens = max(a.usage.OutputTokens, usage.OutputTokens)
		a.seen = true
	}
	if usage.CacheCreationInputTokens > 0 {
		a.usage.CacheCreationInputTokens = max(a.usage.CacheCreationInputTokens, usage.CacheCreationInputTokens)
		a.seen = true
	}
	if usage.CacheReadInputTokens > 0 {
		a.usage.CacheReadInputTokens = max(a.usage.CacheReadInputTokens, usage.CacheReadInputTokens)
		a.seen = true
	}
	if usage.TotalTokens > 0 {
		a.usage.TotalTokens = max(a.usage.TotalTokens, usage.TotalTokens)
		a.seen = true
	}
}

func (a *anthropicSSEUsageAccumulator) Usage() (anthropicUsage, bool) {
	if !a.seen {
		return anthropicUsage{}, false
	}
	return composeAnthropicUsage(a.model, a.usage), true
}
