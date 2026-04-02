package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type UsageEvent struct {
	RequestID    string
	ProviderName string
	Model        string
	StartedAt    time.Time
	FinishedAt   time.Time
	InputTokens  int
	OutputTokens int
	TotalTokens  int
	Status       string
}

type UsageRecorder interface {
	RecordUsage(UsageEvent)
}

type noopUsageRecorder struct{}

func (noopUsageRecorder) RecordUsage(UsageEvent) {}

type Server struct {
	cfg        RuntimeConfig
	httpClient *http.Client
	recorder   UsageRecorder
}

func NewServer(cfg RuntimeConfig, recorder UsageRecorder, httpClient *http.Client) *Server {
	if recorder == nil {
		recorder = noopUsageRecorder{}
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Server{
		cfg:        cfg,
		httpClient: httpClient,
		recorder:   recorder,
	}
}

type openAIUsage struct {
	Model        string
	InputTokens  int
	OutputTokens int
	TotalTokens  int
}

func (s *Server) handleOpenAIChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	reqID := newRequestID()
	startedAt := time.Now().UTC()

	upstreamURL := strings.TrimRight(s.cfg.ProviderURL, "/") + "/chat/completions"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyJSONHeaders(upstreamReq.Header, r.Header)
	upstreamReq.Header.Set("Authorization", "Bearer "+s.cfg.ProviderKey)

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	usage := parseOpenAIUsage(body)
	s.recorder.RecordUsage(UsageEvent{
		RequestID:    reqID,
		ProviderName: "sub2api",
		Model:        usage.Model,
		StartedAt:    startedAt,
		FinishedAt:   time.Now().UTC(),
		InputTokens:  usage.InputTokens,
		OutputTokens: usage.OutputTokens,
		TotalTokens:  usage.TotalTokens,
		Status:       "completed",
	})

	copyResponse(w, resp.StatusCode, resp.Header, body)
}

func parseOpenAIUsage(body []byte) openAIUsage {
	var payload struct {
		Model string `json:"model"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return openAIUsage{}
	}
	return openAIUsage{
		Model:        payload.Model,
		InputTokens:  payload.Usage.PromptTokens,
		OutputTokens: payload.Usage.CompletionTokens,
		TotalTokens:  payload.Usage.TotalTokens,
	}
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
