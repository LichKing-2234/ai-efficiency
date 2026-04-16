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
	Model                       string
	InputTokens                 int
	CacheCreationInputTokens    int
	CacheReadInputTokens        int
	OutputTokens                int
	TotalTokens                 int
	HasCacheCreationInputTokens bool
	HasCacheReadInputTokens     bool
	HasCachedInputTokens        bool
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

	cred, err := s.resolveProviderCredential(r.Context(), "anthropic")
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
			Status:       "credential_resolve_failed",
		})
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	upstreamURL := strings.TrimRight(cred.BaseURL, "/") + "/v1/messages"
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
	upstreamReq.Header.Set("x-api-key", strings.TrimSpace(cred.APIKey))
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
		SessionID:                   s.cfg.SessionID,
		WorkspaceID:                 s.cfg.WorkspaceID,
		RequestID:                   reqID,
		ProviderName:                firstNonEmptyProviderName(cred.ProviderName, "sub2api"),
		Model:                       usage.Model,
		StartedAt:                   startedAt,
		FinishedAt:                  time.Now().UTC(),
		InputTokens:                 usage.InputTokens,
		CachedInputTokens:           usage.CacheCreationInputTokens + usage.CacheReadInputTokens,
		CacheCreationInputTokens:    usage.CacheCreationInputTokens,
		CacheReadInputTokens:        usage.CacheReadInputTokens,
		HasCachedInputTokens:        usage.HasCachedInputTokens,
		HasCacheCreationInputTokens: usage.HasCacheCreationInputTokens,
		HasCacheReadInputTokens:     usage.HasCacheReadInputTokens,
		OutputTokens:                usage.OutputTokens,
		TotalTokens:                 usage.TotalTokens,
		HTTPStatus:                  resp.StatusCode,
		RawResponse:                 wrapJSONRawResponse(body),
		Status:                      status,
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
	InputTokens              int  `json:"input_tokens"`
	OutputTokens             int  `json:"output_tokens"`
	TotalTokens              int  `json:"total_tokens"`
	CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
	CachedTokens             *int `json:"cached_tokens"`
	CacheCreation            *struct {
		Ephemeral5mInputTokens *int `json:"ephemeral_5m_input_tokens"`
		Ephemeral1hInputTokens *int `json:"ephemeral_1h_input_tokens"`
	} `json:"cache_creation"`
}

func composeAnthropicUsage(model string, usage anthropicUsageFields) anthropicUsage {
	cacheCreationInputTokens, hasCacheCreationInputTokens := anthropicCacheCreationInputTokens(usage)
	cacheReadInputTokens, hasCacheReadInputTokens := anthropicCacheReadInputTokens(usage)
	total := usage.TotalTokens
	if usage.InputTokens+cacheCreationInputTokens+cacheReadInputTokens+usage.OutputTokens > total {
		total = usage.InputTokens + cacheCreationInputTokens + cacheReadInputTokens + usage.OutputTokens
	}
	return anthropicUsage{
		Model:                       model,
		InputTokens:                 usage.InputTokens,
		CacheCreationInputTokens:    cacheCreationInputTokens,
		CacheReadInputTokens:        cacheReadInputTokens,
		OutputTokens:                usage.OutputTokens,
		TotalTokens:                 total,
		HasCacheCreationInputTokens: hasCacheCreationInputTokens,
		HasCacheReadInputTokens:     hasCacheReadInputTokens,
		HasCachedInputTokens:        hasCacheCreationInputTokens || hasCacheReadInputTokens,
	}
}

func anthropicCacheCreationInputTokens(usage anthropicUsageFields) (int, bool) {
	if usage.CacheCreationInputTokens != nil && *usage.CacheCreationInputTokens > 0 {
		return *usage.CacheCreationInputTokens, true
	}
	v5m := 0
	v1h := 0
	has5m := false
	has1h := false
	if usage.CacheCreation != nil {
		has5m = usage.CacheCreation.Ephemeral5mInputTokens != nil
		has1h = usage.CacheCreation.Ephemeral1hInputTokens != nil
		if has5m {
			v5m = *usage.CacheCreation.Ephemeral5mInputTokens
		}
		if has1h {
			v1h = *usage.CacheCreation.Ephemeral1hInputTokens
		}
	}
	if has5m || has1h {
		return v5m + v1h, true
	}
	if usage.CacheCreationInputTokens != nil {
		return *usage.CacheCreationInputTokens, true
	}
	return 0, false
}

func anthropicCacheReadInputTokens(usage anthropicUsageFields) (int, bool) {
	if usage.CacheReadInputTokens != nil && *usage.CacheReadInputTokens > 0 {
		return *usage.CacheReadInputTokens, true
	}
	if usage.CachedTokens != nil {
		return *usage.CachedTokens, true
	}
	if usage.CacheReadInputTokens != nil {
		return *usage.CacheReadInputTokens, true
	}
	return 0, false
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
				s.recordAnthropicUsage(reqID, startedAt, resp.StatusCode, usage, nil, ok, "downstream_write_error", writeErr.Error())
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
			s.recordAnthropicUsage(reqID, startedAt, resp.StatusCode, usage, nil, ok, "upstream_read_error", err.Error())
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
	s.recordAnthropicUsage(reqID, startedAt, resp.StatusCode, usage, wrapSSERawResponse(acc.RawEvents()), ok, status, errMessage)
}

func (s *Server) recordAnthropicUsage(reqID string, startedAt time.Time, httpStatus int, usage anthropicUsage, rawResponse map[string]any, hasUsage bool, status string, errMessage string) {
	model := usage.Model
	inputTokens := usage.InputTokens
	cacheCreationInputTokens := usage.CacheCreationInputTokens
	cacheReadInputTokens := usage.CacheReadInputTokens
	outputTokens := usage.OutputTokens
	totalTokens := usage.TotalTokens
	if !hasUsage {
		model = ""
		inputTokens = 0
		cacheCreationInputTokens = 0
		cacheReadInputTokens = 0
		outputTokens = 0
		totalTokens = 0
	}
	s.recordUsage(UsageEvent{
		SessionID:                   s.cfg.SessionID,
		WorkspaceID:                 s.cfg.WorkspaceID,
		RequestID:                   reqID,
		ProviderName:                "sub2api",
		Model:                       model,
		StartedAt:                   startedAt,
		FinishedAt:                  time.Now().UTC(),
		InputTokens:                 inputTokens,
		CachedInputTokens:           cacheCreationInputTokens + cacheReadInputTokens,
		CacheCreationInputTokens:    cacheCreationInputTokens,
		CacheReadInputTokens:        cacheReadInputTokens,
		HasCachedInputTokens:        usage.HasCachedInputTokens,
		HasCacheCreationInputTokens: usage.HasCacheCreationInputTokens,
		HasCacheReadInputTokens:     usage.HasCacheReadInputTokens,
		OutputTokens:                outputTokens,
		TotalTokens:                 totalTokens,
		HTTPStatus:                  httpStatus,
		RawResponse:                 rawResponse,
		Error:                       errMessage,
		Status:                      status,
	})
}

type anthropicSSEUsageAccumulator struct {
	pending      string
	model        string
	usage        anthropicUsage
	seen         bool
	currentEvent string
	rawEvents    []map[string]any
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
	if line == "" {
		a.currentEvent = ""
		return
	}
	if strings.HasPrefix(line, "event:") {
		a.currentEvent = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		return
	}
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
	var raw map[string]any
	if err := json.Unmarshal([]byte(payload), &raw); err == nil && len(raw) > 0 {
		a.rawEvents = append(a.rawEvents, map[string]any{
			"event": a.currentEvent,
			"data":  raw,
		})
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
	cacheCreationInputTokens, hasCacheCreationInputTokens := anthropicCacheCreationInputTokens(usage)
	if hasCacheCreationInputTokens {
		a.usage.CacheCreationInputTokens = max(a.usage.CacheCreationInputTokens, cacheCreationInputTokens)
		a.usage.HasCacheCreationInputTokens = true
		a.usage.HasCachedInputTokens = true
		a.seen = true
	}
	cacheReadInputTokens, hasCacheReadInputTokens := anthropicCacheReadInputTokens(usage)
	if hasCacheReadInputTokens {
		a.usage.CacheReadInputTokens = max(a.usage.CacheReadInputTokens, cacheReadInputTokens)
		a.usage.HasCacheReadInputTokens = true
		a.usage.HasCachedInputTokens = true
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
	a.usage.Model = a.model
	if a.usage.TotalTokens == 0 && (a.usage.InputTokens > 0 || a.usage.CacheCreationInputTokens > 0 || a.usage.CacheReadInputTokens > 0 || a.usage.OutputTokens > 0) {
		a.usage.TotalTokens = a.usage.InputTokens + a.usage.CacheCreationInputTokens + a.usage.CacheReadInputTokens + a.usage.OutputTokens
	}
	return a.usage, true
}

func (a *anthropicSSEUsageAccumulator) RawEvents() []map[string]any {
	if len(a.rawEvents) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(a.rawEvents))
	for _, event := range a.rawEvents {
		out = append(out, event)
	}
	return out
}
