package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
)

type UsageEvent struct {
	SessionID    string
	WorkspaceID  string
	RequestID    string
	ProviderName string
	Model        string
	StartedAt    time.Time
	FinishedAt   time.Time
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	HTTPStatus   int
	Error        string
	Status       string
}

type UsageRecorder interface {
	RecordUsage(UsageEvent)
}

type inMemoryUsageRecorder struct {
	mu     sync.Mutex
	events []UsageEvent
}

func (r *inMemoryUsageRecorder) RecordUsage(event UsageEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

type Server struct {
	cfg             RuntimeConfig
	httpClient      *http.Client
	recorder        UsageRecorder
	backendClient   backendEventClient
	credentialCache *credentialCache
}

type backendEventClient interface {
	SendSessionUsageEvent(ctx context.Context, req client.SessionUsageEventRequest) error
	SendCommitCheckpoint(ctx context.Context, req client.CommitCheckpointRequest) error
	SendCommitRewrite(ctx context.Context, req client.CommitRewriteRequest) error
	SendSessionEvent(ctx context.Context, req client.SessionEventRequest) error
}

func NewServer(cfg RuntimeConfig, recorder UsageRecorder, httpClient *http.Client) *Server {
	if recorder == nil {
		// Task 4 keeps usage local-only for now: runtime requests are recorded in memory.
		recorder = &inMemoryUsageRecorder{}
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	var backendClient backendEventClient
	var credentialFetcher credentialFetcher
	backendURL := strings.TrimSpace(cfg.BackendURL)
	backendToken := strings.TrimSpace(cfg.BackendToken)
	if backendURL != "" && backendToken != "" {
		fetcher := client.New(backendURL, backendToken)
		backendClient = fetcher
		credentialFetcher = fetcher
	}
	return &Server{
		cfg:             cfg,
		httpClient:      httpClient,
		recorder:        recorder,
		backendClient:   backendClient,
		credentialCache: newCredentialCache(credentialFetcher),
	}
}

type openAIUsage struct {
	Model        string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

type openAIUsageFields struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	InputTokens      int `json:"input_tokens"`
	OutputTokens     int `json:"output_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

func (s *Server) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleOpenAIRequest(w, r, "/chat/completions")
}

func (s *Server) handleOpenAIResponses(w http.ResponseWriter, r *http.Request) {
	s.handleOpenAIRequest(w, r, "/responses")
}

func (s *Server) handleOpenAIRequest(w http.ResponseWriter, r *http.Request, upstreamPath string) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	expectedAuth := "Bearer " + strings.TrimSpace(s.cfg.AuthToken)
	if strings.TrimSpace(s.cfg.AuthToken) == "" || strings.TrimSpace(r.Header.Get("Authorization")) != expectedAuth {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	reqID := newRequestID()
	startedAt := time.Now().UTC()

	cred, err := s.resolveProviderCredential(r.Context(), "openai")
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

	upstreamURL := strings.TrimRight(cred.BaseURL, "/") + upstreamPath
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, r.Body)
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
	upstreamReq.Header.Set("Authorization", "Bearer "+cred.APIKey)

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

	if isSSEContentType(resp.Header.Get("Content-Type")) {
		s.proxyOpenAIStream(w, resp, reqID, startedAt)
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
	usage := parseOpenAIUsage(body)
	status := "completed"
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		status = "upstream_http_error"
	}
	s.recordUsage(UsageEvent{
		SessionID:    s.cfg.SessionID,
		WorkspaceID:  s.cfg.WorkspaceID,
		RequestID:    reqID,
		ProviderName: firstNonEmptyProviderName(cred.ProviderName, "sub2api"),
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

func (s *Server) resolveProviderCredential(ctx context.Context, platform string) (*client.ProviderCredential, error) {
	if s.credentialCache == nil {
		return nil, fmt.Errorf("provider credential cache is not configured")
	}

	cred, err := s.credentialCache.Get(ctx, s.cfg.SessionID, platform)
	if err != nil {
		return nil, fmt.Errorf("fetch provider credential for %q: %w", platform, err)
	}
	return cred, nil
}

func firstNonEmptyProviderName(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Server) recordUsage(event UsageEvent) {
	if s.recorder != nil {
		s.recorder.RecordUsage(event)
	}
	if err := s.uploadUsageEvent(event); err != nil {
		_ = AppendDurableEvent(s.cfg.SessionID, EventEnvelope{
			EventType: "session_usage",
			SessionID: s.cfg.SessionID,
			Payload:   s.usageRequestFromEvent(event),
		})
	}
}

func (s *Server) uploadUsageEvent(event UsageEvent) error {
	if s.backendClient == nil {
		return fmt.Errorf("backend client is not configured")
	}
	req := s.usageRequestFromEvent(event)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return s.backendClient.SendSessionUsageEvent(ctx, req)
}

func (s *Server) usageRequestFromEvent(event UsageEvent) client.SessionUsageEventRequest {
	eventID := strings.TrimSpace(event.RequestID)
	if eventID == "" {
		eventID = newRequestID()
	}
	requestID := strings.TrimSpace(event.RequestID)
	if requestID == "" {
		requestID = eventID
	}
	startedAt := event.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	finishedAt := event.FinishedAt.UTC()
	if finishedAt.IsZero() || finishedAt.Before(startedAt) {
		finishedAt = startedAt
	}
	providerName := strings.TrimSpace(event.ProviderName)
	if providerName == "" {
		providerName = "sub2api"
	}
	model := strings.TrimSpace(event.Model)
	if model == "" {
		model = "unknown"
	}
	status := strings.TrimSpace(event.Status)
	if status == "" {
		status = "completed"
	}
	workspaceID := strings.TrimSpace(event.WorkspaceID)
	if workspaceID == "" {
		workspaceID = strings.TrimSpace(s.cfg.WorkspaceID)
	}

	req := client.SessionUsageEventRequest{
		EventID:      eventID,
		SessionID:    strings.TrimSpace(s.cfg.SessionID),
		WorkspaceID:  workspaceID,
		RequestID:    requestID,
		ProviderName: providerName,
		Model:        model,
		StartedAt:    startedAt,
		FinishedAt:   finishedAt,
		InputTokens:  int64(max(event.InputTokens, 0)),
		OutputTokens: int64(max(event.OutputTokens, 0)),
		TotalTokens:  int64(max(event.TotalTokens, 0)),
		Status:       status,
	}
	raw := map[string]any{
		"http_status": event.HTTPStatus,
	}
	if strings.TrimSpace(event.Error) != "" {
		raw["error"] = event.Error
	}
	req.RawMetadata = raw
	return req
}

func parseOpenAIUsage(body []byte) openAIUsage {
	var payload struct {
		Model string            `json:"model"`
		Usage openAIUsageFields `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return openAIUsage{}
	}
	inputTokens := payload.Usage.PromptTokens
	if inputTokens == 0 {
		inputTokens = payload.Usage.InputTokens
	}
	outputTokens := payload.Usage.CompletionTokens
	if outputTokens == 0 {
		outputTokens = payload.Usage.OutputTokens
	}
	totalTokens := payload.Usage.TotalTokens
	if totalTokens == 0 && (inputTokens > 0 || outputTokens > 0) {
		totalTokens = inputTokens + outputTokens
	}
	return openAIUsage{
		Model:        payload.Model,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
	}
}

func (s *Server) proxyOpenAIStream(w http.ResponseWriter, resp *http.Response, reqID string, startedAt time.Time) {
	for k, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	flusher, _ := w.(http.Flusher)

	acc := &openAISSEUsageAccumulator{}
	buf := make([]byte, 32*1024)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			acc.Consume(chunk)
			if _, writeErr := w.Write(chunk); writeErr != nil {
				usage, ok := acc.Usage()
				s.recordOpenAIUsage(reqID, startedAt, resp.StatusCode, usage, ok, "downstream_write_error", writeErr.Error())
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
			s.recordOpenAIUsage(reqID, startedAt, resp.StatusCode, usage, ok, "upstream_read_error", err.Error())
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
	s.recordOpenAIUsage(reqID, startedAt, resp.StatusCode, usage, ok, status, errMessage)
}

func (s *Server) recordOpenAIUsage(reqID string, startedAt time.Time, httpStatus int, usage openAIUsage, hasUsage bool, status string, errMessage string) {
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

type openAISSEUsageAccumulator struct {
	pending string
	usage   openAIUsage
	seen    bool
}

func (a *openAISSEUsageAccumulator) Consume(chunk []byte) {
	data := a.pending + string(chunk)
	lines := strings.Split(data, "\n")
	a.pending = lines[len(lines)-1]
	for _, line := range lines[:len(lines)-1] {
		a.consumeLine(strings.TrimSpace(strings.TrimSuffix(line, "\r")))
	}
}

func (a *openAISSEUsageAccumulator) consumeLine(line string) {
	if !strings.HasPrefix(line, "data:") {
		return
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" || payload == "[DONE]" {
		return
	}

	var event struct {
		Type     string            `json:"type"`
		Model    string            `json:"model"`
		Usage    openAIUsageFields `json:"usage"`
		Response struct {
			Model string            `json:"model"`
			Usage openAIUsageFields `json:"usage"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return
	}

	if event.Response.Model != "" {
		a.usage.Model = event.Response.Model
	}
	if event.Model != "" && a.usage.Model == "" {
		a.usage.Model = event.Model
	}
	a.applyUsage(event.Usage)
	a.applyUsage(event.Response.Usage)
}

func (a *openAISSEUsageAccumulator) applyUsage(usage openAIUsageFields) {
	inputTokens := usage.PromptTokens
	if inputTokens == 0 {
		inputTokens = usage.InputTokens
	}
	outputTokens := usage.CompletionTokens
	if outputTokens == 0 {
		outputTokens = usage.OutputTokens
	}
	totalTokens := usage.TotalTokens
	if totalTokens == 0 && (inputTokens > 0 || outputTokens > 0) {
		totalTokens = inputTokens + outputTokens
	}
	if inputTokens > 0 {
		a.usage.InputTokens = max(a.usage.InputTokens, inputTokens)
		a.seen = true
	}
	if outputTokens > 0 {
		a.usage.OutputTokens = max(a.usage.OutputTokens, outputTokens)
		a.seen = true
	}
	if totalTokens > 0 {
		a.usage.TotalTokens = max(a.usage.TotalTokens, totalTokens)
		a.seen = true
	}
}

func (a *openAISSEUsageAccumulator) Usage() (openAIUsage, bool) {
	if !a.seen {
		return openAIUsage{}, false
	}
	return a.usage, true
}

func copyJSONHeaders(dst, src http.Header) {
	for k, values := range src {
		if !strings.EqualFold(k, "Content-Type") && !strings.EqualFold(k, "Accept") {
			continue
		}
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}

func copyResponse(w http.ResponseWriter, statusCode int, header http.Header, body []byte) {
	for k, values := range header {
		for _, v := range values {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(statusCode)
	_, _ = w.Write(body)
}

func newRequestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "req-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return "req-" + hex.EncodeToString(b)
}
