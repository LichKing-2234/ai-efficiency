package proxy

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProxyChildProcess(t *testing.T) {
	if os.Getenv(testChildEnv) != "1" {
		t.Skip("helper process")
	}
	args := os.Args
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+1 >= len(args) {
		fmt.Fprintln(os.Stderr, "missing runtime config path")
		os.Exit(2)
	}
	if err := ServeFromConfigFile(args[sep+1]); err != nil {
		fmt.Fprintf(os.Stderr, "proxy child failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestSpawnStartsProxyAndReturnsReadyAddress(t *testing.T) {
	cfg := RuntimeConfig{
		SessionID:   "sess-test",
		ListenAddr:  "127.0.0.1:0",
		AuthToken:   "tok-test",
		ProviderURL: "http://example.local",
		ProviderKey: "provider-key",
	}

	result, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer func() {
		_ = Stop(StopRequest{
			PID:        result.PID,
			ListenAddr: result.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: result.ConfigPath,
		})
	}()

	if result.PID <= 0 {
		t.Fatalf("pid = %d, want > 0", result.PID)
	}
	if strings.TrimSpace(result.ListenAddr) == "" {
		t.Fatal("listenAddr should not be empty")
	}

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Get("http://" + result.ListenAddr + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestStopTerminatesProxyProcess(t *testing.T) {
	cfg := RuntimeConfig{
		SessionID:  "sess-stop",
		ListenAddr: "127.0.0.1:0",
		AuthToken:  "tok-stop",
	}

	result, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	if err := Stop(StopRequest{
		PID:        result.PID,
		ListenAddr: result.ListenAddr,
		AuthToken:  cfg.AuthToken,
		ConfigPath: result.ConfigPath,
	}); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, err := (&http.Client{Timeout: 150 * time.Millisecond}).Get("http://" + result.ListenAddr + "/healthz")
		if err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("proxy still reachable after stop at %s", result.ListenAddr)
}

func TestSpawnForcedChildProcessUsesConfigHandoffAndCleansTempFiles(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)
	t.Setenv(forceChildEnv, "1")

	cfg := RuntimeConfig{
		SessionID:   "sess-child",
		ListenAddr:  "127.0.0.1:0",
		AuthToken:   "tok-child",
		ProviderURL: "http://provider.local",
		ProviderKey: "provider-secret",
	}

	result, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "ae-proxy-config-*"))
	if err != nil {
		t.Fatalf("glob config dirs: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("config dirs = %d, want 1", len(matches))
	}
	raw, err := os.ReadFile(filepath.Join(matches[0], "runtime.json"))
	if err != nil {
		t.Fatalf("reading runtime config: %v", err)
	}
	var saved RuntimeConfig
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("parsing runtime config: %v", err)
	}
	if saved.SessionID != cfg.SessionID || saved.AuthToken != cfg.AuthToken {
		t.Fatalf("runtime config mismatch: got session=%q token=%q", saved.SessionID, saved.AuthToken)
	}
	if saved.ListenAddr != result.ListenAddr {
		t.Fatalf("runtime listen_addr = %q, want %q", saved.ListenAddr, result.ListenAddr)
	}
	if strings.TrimSpace(result.ConfigPath) == "" {
		t.Fatal("expected Spawn result to include config path")
	}

	if err := Stop(StopRequest{
		PID:        result.PID,
		ListenAddr: result.ListenAddr,
		AuthToken:  cfg.AuthToken,
		ConfigPath: result.ConfigPath,
	}); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	matches, err = filepath.Glob(filepath.Join(tmpDir, "ae-proxy-config-*"))
	if err != nil {
		t.Fatalf("glob config dirs after stop: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no proxy temp config dirs after stop, found %d", len(matches))
	}
}

func TestProxyOpenAIResponses_ForwardsRequestAndRecordsUsage(t *testing.T) {
	fx := startProxyWithFakeOpenAIUpstream(t)
	srv := fx.Server

	requestBody := `{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`
	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/chat/completions",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tok-openai")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := fx.Upstream.Hits(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
	if got := fx.Upstream.LastAuthorization(); got != "Bearer provider-key" {
		t.Fatalf("upstream authorization = %q, want %q", got, "Bearer provider-key")
	}
	if got := fx.Upstream.LastBody(); got != requestBody {
		t.Fatalf("upstream body = %q, want %q", got, requestBody)
	}

	lastUsage, ok := fx.Recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.TotalTokens != 140 {
		t.Fatalf("total_tokens = %d, want 140", lastUsage.TotalTokens)
	}
	if lastUsage.Status != "completed" {
		t.Fatalf("status = %q, want %q", lastUsage.Status, "completed")
	}
	if lastUsage.SessionID != "sess-openai" {
		t.Fatalf("session_id = %q, want %q", lastUsage.SessionID, "sess-openai")
	}
}

func TestProxyOpenAIResponses_RejectsUnauthorizedRequest(t *testing.T) {
	fx := startProxyWithFakeOpenAIUpstream(t)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := fx.Upstream.Hits(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0", got)
	}
	if got := fx.Recorder.EventCount(); got != 0 {
		t.Fatalf("usage events = %d, want 0", got)
	}
}

func TestProxyOpenAIResponses_Upstream5xxRecordsFailureUsage(t *testing.T) {
	fx := startProxyWithFakeOpenAIUpstream(t)
	fx.Upstream.SetResponse(
		http.StatusBadGateway,
		`{"error":{"message":"upstream failed"}}`,
	)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tok-openai")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	lastUsage, ok := fx.Recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.Status == "completed" {
		t.Fatalf("status = %q, want non-completed", lastUsage.Status)
	}
}

func TestProxyOpenAIResponses_TransportErrorRecordsFailureUsage(t *testing.T) {
	recorder := &fakeOpenAIRecorder{}
	origNewProxyServer := newProxyServer
	newProxyServer = func(cfg RuntimeConfig) *Server {
		return NewServer(cfg, recorder, &http.Client{Timeout: 300 * time.Millisecond})
	}
	t.Cleanup(func() {
		newProxyServer = origNewProxyServer
	})

	unreachableAddr := reserveListenAddrForTest(t)
	cfg := RuntimeConfig{
		SessionID:   "sess-openai-transport-failure",
		ListenAddr:  reserveListenAddrForTest(t),
		AuthToken:   "tok-openai",
		ProviderURL: "http://" + unreachableAddr,
		ProviderKey: "provider-key",
	}

	srv, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        srv.PID,
			ListenAddr: srv.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: srv.ConfigPath,
		})
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}]}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tok-openai")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	lastUsage, ok := recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.Status == "completed" {
		t.Fatalf("status = %q, want non-completed", lastUsage.Status)
	}
}

func TestProxyOpenAIResponsesRoute_ForwardsRequestAndRecordsUsage(t *testing.T) {
	fx := startProxyWithFakeOpenAIResponsesUpstream(t)
	srv := fx.Server

	requestBody := `{"model":"gpt-5.4","input":"hi"}`
	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/responses",
		strings.NewReader(requestBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tok-openai")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := fx.Upstream.Hits(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
	if got := fx.Upstream.LastAuthorization(); got != "Bearer provider-key" {
		t.Fatalf("upstream authorization = %q, want %q", got, "Bearer provider-key")
	}
	if got := fx.Upstream.LastBody(); got != requestBody {
		t.Fatalf("upstream body = %q, want %q", got, requestBody)
	}

	lastUsage, ok := fx.Recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.TotalTokens != 140 {
		t.Fatalf("total_tokens = %d, want 140", lastUsage.TotalTokens)
	}
	if lastUsage.Status != "completed" {
		t.Fatalf("status = %q, want %q", lastUsage.Status, "completed")
	}
}

func TestProxyOpenAIResponsesRoute_RejectsUnauthorizedRequest(t *testing.T) {
	fx := startProxyWithFakeOpenAIResponsesUpstream(t)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/responses",
		strings.NewReader(`{"model":"gpt-5.4","input":"hi"}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := fx.Upstream.Hits(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0", got)
	}
	if got := fx.Recorder.EventCount(); got != 0 {
		t.Fatalf("usage events = %d, want 0", got)
	}
}

func TestProxyOpenAIResponsesRoute_Upstream5xxRecordsFailureUsage(t *testing.T) {
	fx := startProxyWithFakeOpenAIResponsesUpstream(t)
	fx.Upstream.SetResponse(
		http.StatusBadGateway,
		`{"error":{"message":"upstream failed"}}`,
	)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/responses",
		strings.NewReader(`{"model":"gpt-5.4","input":"hi"}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tok-openai")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	lastUsage, ok := fx.Recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.Status == "completed" {
		t.Fatalf("status = %q, want non-completed", lastUsage.Status)
	}
}

func TestProxyAnthropicMessages_ForwardsRequestAndRecordsUsage(t *testing.T) {
	fx := startProxyWithFakeAnthropicUpstream(t)
	srv := fx.Server

	reqBody := `{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`
	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/anthropic/v1/messages",
		strings.NewReader(reqBody),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "tok-anthropic")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "tools-2024-04-04")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := fx.Upstream.Hits(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
	if got := fx.Upstream.LastAPIKey(); got != "provider-key" {
		t.Fatalf("upstream x-api-key = %q, want %q", got, "provider-key")
	}
	if got := fx.Upstream.LastAnthropicVersion(); got != "2023-06-01" {
		t.Fatalf("upstream anthropic-version = %q, want %q", got, "2023-06-01")
	}
	if got := fx.Upstream.LastAnthropicBeta(); got != "tools-2024-04-04" {
		t.Fatalf("upstream anthropic-beta = %q, want %q", got, "tools-2024-04-04")
	}

	lastUsage, ok := fx.Recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.TotalTokens != 210 {
		t.Fatalf("total_tokens = %d, want 210", lastUsage.TotalTokens)
	}
	if lastUsage.Status != "completed" {
		t.Fatalf("status = %q, want %q", lastUsage.Status, "completed")
	}
	if lastUsage.SessionID != "sess-anthropic" {
		t.Fatalf("session_id = %q, want %q", lastUsage.SessionID, "sess-anthropic")
	}
}

func TestProxyAnthropicMessages_Upstream5xxRecordsFailureUsage(t *testing.T) {
	fx := startProxyWithFakeAnthropicUpstream(t)
	fx.Upstream.SetResponse(
		http.StatusBadGateway,
		`{"type":"error","error":{"type":"api_error","message":"upstream failed"}}`,
	)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/anthropic/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "tok-anthropic")
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	lastUsage, ok := fx.Recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.Status == "completed" {
		t.Fatalf("status = %q, want non-completed", lastUsage.Status)
	}
}

func TestProxyAnthropicMessages_RejectsUnauthorizedRequest(t *testing.T) {
	fx := startProxyWithFakeAnthropicUpstream(t)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/anthropic/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
	if got := fx.Upstream.Hits(); got != 0 {
		t.Fatalf("upstream hits = %d, want 0", got)
	}
	if got := fx.Recorder.EventCount(); got != 0 {
		t.Fatalf("usage events = %d, want 0", got)
	}
}

func TestProxyAnthropicMessages_AuthorizationBearerFallback(t *testing.T) {
	fx := startProxyWithFakeAnthropicUpstream(t)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/anthropic/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tok-anthropic")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := fx.Upstream.Hits(); got != 1 {
		t.Fatalf("upstream hits = %d, want 1", got)
	}
}

func TestProxyAnthropicMessages_DefaultAnthropicVersion(t *testing.T) {
	fx := startProxyWithFakeAnthropicUpstream(t)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/anthropic/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "tok-anthropic")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if got := fx.Upstream.LastAnthropicVersion(); got != "2023-06-01" {
		t.Fatalf("upstream anthropic-version = %q, want %q", got, "2023-06-01")
	}
}

func TestProxyAnthropicMessages_TransportErrorRecordsFailureUsage(t *testing.T) {
	recorder := &fakeOpenAIRecorder{}
	origNewProxyServer := newProxyServer
	newProxyServer = func(cfg RuntimeConfig) *Server {
		return NewServer(cfg, recorder, &http.Client{Timeout: 300 * time.Millisecond})
	}
	t.Cleanup(func() {
		newProxyServer = origNewProxyServer
	})

	unreachableAddr := reserveListenAddrForTest(t)
	cfg := RuntimeConfig{
		SessionID:   "sess-anthropic-transport-failure",
		ListenAddr:  reserveListenAddrForTest(t),
		AuthToken:   "tok-anthropic",
		ProviderURL: "http://" + unreachableAddr,
		ProviderKey: "provider-key",
	}

	srv, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        srv.PID,
			ListenAddr: srv.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: srv.ConfigPath,
		})
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/anthropic/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "tok-anthropic")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadGateway)
	}

	lastUsage, ok := recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.Status == "completed" {
		t.Fatalf("status = %q, want non-completed", lastUsage.Status)
	}
}

func TestProxyAnthropicMessages_StreamingPassthroughAndRecordsUsage(t *testing.T) {
	recorder := &fakeOpenAIRecorder{}
	firstChunkSent := make(chan struct{})
	allowFinish := make(chan struct{})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-sonnet-4-20250514\",\"usage\":{\"input_tokens\":11,\"cache_creation_input_tokens\":7,\"cache_read_input_tokens\":5}}}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
		close(firstChunkSent)

		<-allowFinish
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":13}}\n\n"))
		if flusher != nil {
			flusher.Flush()
		}
	}))
	t.Cleanup(upstream.Close)

	origNewProxyServer := newProxyServer
	newProxyServer = func(cfg RuntimeConfig) *Server {
		return NewServer(cfg, recorder, nil)
	}
	t.Cleanup(func() {
		newProxyServer = origNewProxyServer
	})

	cfg := RuntimeConfig{
		SessionID:   "sess-anthropic-stream",
		ListenAddr:  reserveListenAddrForTest(t),
		AuthToken:   "tok-anthropic",
		ProviderURL: upstream.URL,
		ProviderKey: "provider-key",
	}
	srv, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        srv.PID,
			ListenAddr: srv.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: srv.ConfigPath,
		})
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/anthropic/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","stream":true,"messages":[{"role":"user","content":"hello"}],"max_tokens":64}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "tok-anthropic")

	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := strings.TrimSpace(resp.Header.Get("Content-Type")); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content-type = %q, want prefix %q", got, "text/event-stream")
	}

	select {
	case <-firstChunkSent:
	case <-time.After(1 * time.Second):
		t.Fatal("upstream did not send first stream chunk")
	}

	reader := bufio.NewReader(resp.Body)
	firstLineCh := make(chan string, 1)
	firstErrCh := make(chan error, 1)
	go func() {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			firstErrCh <- readErr
			return
		}
		firstLineCh <- line
	}()
	select {
	case line := <-firstLineCh:
		if !strings.Contains(line, "event: message_start") {
			t.Fatalf("first streamed line = %q, want message_start event", line)
		}
	case err := <-firstErrCh:
		t.Fatalf("failed reading first stream line: %v", err)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("stream did not pass through incrementally before upstream completion")
	}

	close(allowFinish)
	rest, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read rest stream: %v", err)
	}
	restBody := string(rest)
	if !strings.Contains(restBody, "message_delta") {
		t.Fatalf("stream body missing message_delta event: %q", restBody)
	}

	lastUsage, ok := recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.Status != "completed" {
		t.Fatalf("status = %q, want %q", lastUsage.Status, "completed")
	}
	if lastUsage.TotalTokens != 36 {
		t.Fatalf("total_tokens = %d, want 36", lastUsage.TotalTokens)
	}
}

func TestProxyAnthropicMessages_CacheUsageAccounting(t *testing.T) {
	fx := startProxyWithFakeAnthropicUpstream(t)
	fx.Upstream.SetResponse(
		http.StatusOK,
		`{"id":"msg_2","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":40}}`,
	)
	srv := fx.Server

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/anthropic/v1/messages",
		strings.NewReader(`{"model":"claude-sonnet-4-20250514","messages":[{"role":"user","content":"hello"}],"max_tokens":64}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "tok-anthropic")

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	lastUsage, ok := fx.Recorder.LastEvent()
	if !ok {
		t.Fatal("expected usage event")
	}
	if lastUsage.InputTokens != 80 {
		t.Fatalf("input_tokens = %d, want 80", lastUsage.InputTokens)
	}
	if lastUsage.TotalTokens != 100 {
		t.Fatalf("total_tokens = %d, want 100", lastUsage.TotalTokens)
	}
}

func TestProxyOpenAIUsageUploadsToBackend(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","model":"gpt-5.4","usage":{"prompt_tokens":11,"completion_tokens":13,"total_tokens":24},"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	var usageReq struct {
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
		RawMetadata  map[string]any `json:"raw_metadata"`
	}
	usageCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session-usage-events" {
			http.NotFound(w, r)
			return
		}
		if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer backend-token" {
			t.Fatalf("backend auth header = %q, want %q", got, "Bearer backend-token")
		}
		usageCalls++
		if err := json.NewDecoder(r.Body).Decode(&usageReq); err != nil {
			t.Fatalf("decode usage event: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer backend.Close()

	cfg := RuntimeConfig{
		SessionID:    "sess-usage-upload",
		WorkspaceID:  "ws-usage-upload",
		ListenAddr:   reserveListenAddrForTest(t),
		AuthToken:    "tok-openai",
		ProviderURL:  upstream.URL,
		ProviderKey:  "provider-key",
		BackendURL:   backend.URL,
		BackendToken: "backend-token",
	}
	srv, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        srv.PID,
			ListenAddr: srv.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: srv.ConfigPath,
		})
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if usageCalls != 1 {
		t.Fatalf("backend usage calls = %d, want 1", usageCalls)
	}
	if usageReq.SessionID != cfg.SessionID {
		t.Fatalf("usage session_id = %q, want %q", usageReq.SessionID, cfg.SessionID)
	}
	if usageReq.WorkspaceID != cfg.WorkspaceID {
		t.Fatalf("usage workspace_id = %q, want %q", usageReq.WorkspaceID, cfg.WorkspaceID)
	}
	if usageReq.ProviderName != "sub2api" {
		t.Fatalf("usage provider_name = %q, want %q", usageReq.ProviderName, "sub2api")
	}
	if usageReq.Model != "gpt-5.4" {
		t.Fatalf("usage model = %q, want %q", usageReq.Model, "gpt-5.4")
	}
	if usageReq.EventID == "" || usageReq.RequestID == "" {
		t.Fatalf("usage ids should be non-empty: event_id=%q request_id=%q", usageReq.EventID, usageReq.RequestID)
	}
	if usageReq.TotalTokens != 24 || usageReq.InputTokens != 11 || usageReq.OutputTokens != 13 {
		t.Fatalf("usage tokens = in:%d out:%d total:%d, want 11/13/24", usageReq.InputTokens, usageReq.OutputTokens, usageReq.TotalTokens)
	}
	if _, err := os.Stat(EventSpoolPath(cfg.SessionID)); !os.IsNotExist(err) {
		t.Fatalf("expected no local spool file on successful backend delivery, stat err=%v", err)
	}
}

func TestProxyOpenAIUsageBackendFailureFallsBackToLocalSpool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","model":"gpt-5.4","usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7},"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/session-usage-events" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("backend down"))
	}))
	defer backend.Close()

	cfg := RuntimeConfig{
		SessionID:    "sess-usage-spool",
		WorkspaceID:  "ws-usage-spool",
		ListenAddr:   reserveListenAddrForTest(t),
		AuthToken:    "tok-openai",
		ProviderURL:  upstream.URL,
		ProviderKey:  "provider-key",
		BackendURL:   backend.URL,
		BackendToken: "backend-token",
	}
	srv, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        srv.PID,
			ListenAddr: srv.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: srv.ConfigPath,
		})
	})

	req, err := http.NewRequest(
		http.MethodPost,
		"http://"+srv.ListenAddr+"/openai/v1/chat/completions",
		strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`),
	)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	events := readDurableEvents(t, cfg.SessionID)
	if len(events) != 1 {
		t.Fatalf("spooled events = %d, want 1", len(events))
	}
	if events[0].EventType != "session_usage" {
		t.Fatalf("spooled event_type = %q, want %q", events[0].EventType, "session_usage")
	}
	payload, ok := events[0].Payload.(map[string]any)
	if !ok {
		t.Fatalf("spooled payload type = %T, want map[string]any", events[0].Payload)
	}
	if payload["session_id"] != cfg.SessionID {
		t.Fatalf("spooled payload.session_id = %v, want %q", payload["session_id"], cfg.SessionID)
	}
	if payload["workspace_id"] != cfg.WorkspaceID {
		t.Fatalf("spooled payload.workspace_id = %v, want %q", payload["workspace_id"], cfg.WorkspaceID)
	}
}

func TestProxyOpenAILazilyFetchesCredentialFromBackend(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-existing-openai" {
			t.Fatalf("authorization = %q, want %q", got, "Bearer sk-existing-openai")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"chatcmpl-1","model":"gpt-5.4","usage":{"prompt_tokens":11,"completion_tokens":13,"total_tokens":24},"choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer upstream.Close()

	backendCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/sessions/sess-openai/provider-credentials":
			backendCalls++
			if got := r.URL.Query().Get("platform"); got != "openai" {
				t.Fatalf("platform query = %q, want openai", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"provider_name": "sub2api",
					"platform":      "openai",
					"api_key_id":    900,
					"api_key":       "sk-existing-openai",
					"base_url":      upstream.URL,
				},
			})
		case "/api/v1/session-usage-events":
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	cfg := RuntimeConfig{
		SessionID:    "sess-openai",
		WorkspaceID:  "ws-openai",
		ListenAddr:   reserveListenAddrForTest(t),
		AuthToken:    "tok-openai",
		BackendURL:   backend.URL,
		BackendToken: "backend-token",
	}
	srv, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        srv.PID,
			ListenAddr: srv.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: srv.ConfigPath,
		})
	})

	for i := 0; i < 2; i++ {
		req, err := http.NewRequest(
			http.MethodPost,
			"http://"+srv.ListenAddr+"/openai/v1/chat/completions",
			strings.NewReader(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hello"}]}`),
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)

		resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
		if err != nil {
			t.Fatalf("post proxy: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	}

	if backendCalls != 1 {
		t.Fatalf("backend credential calls = %d, want 1", backendCalls)
	}
}

func TestProxyAnthropicLazilyFetchesCredentialFromBackend(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("x-api-key"); got != "sk-existing-anthropic" {
			t.Fatalf("x-api-key = %q, want %q", got, "sk-existing-anthropic")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg-1","model":"claude-sonnet-4-20250514","usage":{"input_tokens":9,"output_tokens":3,"total_tokens":12},"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer upstream.Close()

	backendCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/sessions/sess-anthropic/provider-credentials":
			backendCalls++
			if got := r.URL.Query().Get("platform"); got != "anthropic" {
				t.Fatalf("platform query = %q, want anthropic", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"provider_name": "sub2api",
					"platform":      "anthropic",
					"api_key_id":    901,
					"api_key":       "sk-existing-anthropic",
					"base_url":      upstream.URL,
				},
			})
		case "/api/v1/session-usage-events":
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, r)
		}
	}))
	defer backend.Close()

	cfg := RuntimeConfig{
		SessionID:    "sess-anthropic",
		WorkspaceID:  "ws-anthropic",
		ListenAddr:   reserveListenAddrForTest(t),
		AuthToken:    "tok-anthropic",
		BackendURL:   backend.URL,
		BackendToken: "backend-token",
	}
	srv, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        srv.PID,
			ListenAddr: srv.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: srv.ConfigPath,
		})
	})

	for i := 0; i < 2; i++ {
		req, err := http.NewRequest(
			http.MethodPost,
			"http://"+srv.ListenAddr+"/anthropic/v1/messages",
			strings.NewReader(`{"model":"claude-sonnet-4-20250514","max_tokens":16,"messages":[{"role":"user","content":"hello"}]}`),
		)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("x-api-key", cfg.AuthToken)
		req.Header.Set("anthropic-version", "2023-06-01")

		resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
		if err != nil {
			t.Fatalf("post proxy: %v", err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	}

	if backendCalls != 1 {
		t.Fatalf("backend credential calls = %d, want 1", backendCalls)
	}
}

func TestSessionEventsPostCommitIngressCreatesBackendCheckpoint(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var got struct {
		EventID        string         `json:"event_id"`
		SessionID      string         `json:"session_id"`
		RepoFullName   string         `json:"repo_full_name"`
		WorkspaceID    string         `json:"workspace_id"`
		CommitSHA      string         `json:"commit_sha"`
		ParentSHAs     []string       `json:"parent_shas"`
		BranchSnapshot string         `json:"branch_snapshot"`
		HeadSnapshot   string         `json:"head_snapshot"`
		BindingSource  string         `json:"binding_source"`
		AgentSnapshot  map[string]any `json:"agent_snapshot"`
		CapturedAt     *time.Time     `json:"captured_at"`
	}
	checkpointCalls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/checkpoints/commit" {
			http.NotFound(w, r)
			return
		}
		checkpointCalls++
		if gotAuth := strings.TrimSpace(r.Header.Get("Authorization")); gotAuth != "Bearer backend-token" {
			t.Fatalf("backend auth header = %q, want %q", gotAuth, "Bearer backend-token")
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode checkpoint request: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer backend.Close()

	cfg := RuntimeConfig{
		SessionID:    "sess-event-commit",
		WorkspaceID:  "ws-event-commit",
		ListenAddr:   reserveListenAddrForTest(t),
		AuthToken:    "tok-local",
		BackendURL:   backend.URL,
		BackendToken: "backend-token",
	}
	srv, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        srv.PID,
			ListenAddr: srv.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: srv.ConfigPath,
		})
	})

	reqBody := `{"event_type":"post_commit","payload":{"event_id":"evt-commit-1","repo_full_name":"https://github.com/org/repo.git","workspace_id":"ws-event-commit","binding_source":"marker","commit_sha":"abc123","parent_shas":["def456"],"branch_snapshot":"main","head_snapshot":"abc123","captured_at":"2026-04-03T09:20:00Z","agent_snapshot":{"tool":"codex"}}}`
	req, err := http.NewRequest(http.MethodPost, "http://"+srv.ListenAddr+"/api/v1/session-events", strings.NewReader(reqBody))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)

	resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("post proxy event: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	if checkpointCalls != 1 {
		t.Fatalf("checkpoint backend calls = %d, want 1", checkpointCalls)
	}
	if got.SessionID != cfg.SessionID {
		t.Fatalf("checkpoint session_id = %q, want %q", got.SessionID, cfg.SessionID)
	}
	if got.EventID != "evt-commit-1" || got.CommitSHA != "abc123" {
		t.Fatalf("unexpected checkpoint payload: %+v", got)
	}
	if got.BindingSource != "marker" {
		t.Fatalf("binding_source = %q, want %q", got.BindingSource, "marker")
	}
	if got.CapturedAt == nil || got.CapturedAt.UTC().Format(time.RFC3339) != "2026-04-03T09:20:00Z" {
		t.Fatalf("captured_at = %v, want %q", got.CapturedAt, "2026-04-03T09:20:00Z")
	}
	if _, err := os.Stat(EventSpoolPath(cfg.SessionID)); !os.IsNotExist(err) {
		t.Fatalf("expected no local spool file on successful backend delivery, stat err=%v", err)
	}
}

func TestSessionEventsBackendFailureFallsBackToLocalSpool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	tests := []struct {
		name            string
		eventType       string
		payload         map[string]any
		wantBackendPath string
	}{
		{
			name:      "post_commit",
			eventType: "post_commit",
			payload: map[string]any{
				"event_id":        "evt-commit-fail",
				"repo_full_name":  "https://github.com/org/repo.git",
				"workspace_id":    "ws-event-fail",
				"binding_source":  "marker",
				"commit_sha":      "abc123",
				"parent_shas":     []string{"def456"},
				"branch_snapshot": "main",
				"head_snapshot":   "abc123",
				"captured_at":     "2026-04-03T09:20:00Z",
			},
			wantBackendPath: "/api/v1/checkpoints/commit",
		},
		{
			name:      "post_rewrite",
			eventType: "post_rewrite",
			payload: map[string]any{
				"event_id":       "evt-rewrite-fail",
				"repo_full_name": "https://github.com/org/repo.git",
				"workspace_id":   "ws-event-fail",
				"binding_source": "marker",
				"rewrite_type":   "amend",
				"old_commit_sha": "old123",
				"new_commit_sha": "new456",
				"captured_at":    "2026-04-03T09:21:00Z",
			},
			wantBackendPath: "/api/v1/checkpoints/rewrite",
		},
		{
			name:      "generic_session_event",
			eventType: "user_prompt_submit",
			payload: map[string]any{
				"event_id":     "evt-session-fail",
				"workspace_id": "ws-event-fail",
				"source":       "hook",
				"captured_at":  "2026-04-03T09:22:00Z",
				"prompt":       "hello",
			},
			wantBackendPath: "/api/v1/session-events",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionID := "sess-" + tt.name
			backendPathCalls := map[string]int{}
			backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				backendPathCalls[r.URL.Path]++
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte("backend failed"))
			}))
			defer backend.Close()

			cfg := RuntimeConfig{
				SessionID:    sessionID,
				WorkspaceID:  "ws-event-fail",
				ListenAddr:   reserveListenAddrForTest(t),
				AuthToken:    "tok-local",
				BackendURL:   backend.URL,
				BackendToken: "backend-token",
			}
			srv, err := Spawn(cfg)
			if err != nil {
				t.Fatalf("Spawn: %v", err)
			}
			t.Cleanup(func() {
				_ = Stop(StopRequest{
					PID:        srv.PID,
					ListenAddr: srv.ListenAddr,
					AuthToken:  cfg.AuthToken,
					ConfigPath: srv.ConfigPath,
				})
			})

			body := map[string]any{
				"event_type": tt.eventType,
				"payload":    tt.payload,
			}
			raw, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal request: %v", err)
			}

			req, err := http.NewRequest(http.MethodPost, "http://"+srv.ListenAddr+"/api/v1/session-events", strings.NewReader(string(raw)))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+cfg.AuthToken)

			resp, err := (&http.Client{Timeout: 1 * time.Second}).Do(req)
			if err != nil {
				t.Fatalf("post proxy event: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusCreated {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
			}

			if backendPathCalls[tt.wantBackendPath] != 1 {
				t.Fatalf("backend calls[%s] = %d, want 1", tt.wantBackendPath, backendPathCalls[tt.wantBackendPath])
			}
			events := readDurableEvents(t, sessionID)
			if len(events) != 1 {
				t.Fatalf("spooled events = %d, want 1", len(events))
			}
			if events[0].EventType != tt.eventType {
				t.Fatalf("spooled event_type = %q, want %q", events[0].EventType, tt.eventType)
			}
			if events[0].SessionID != sessionID {
				t.Fatalf("spooled session_id = %q, want %q", events[0].SessionID, sessionID)
			}
		})
	}
}

func readDurableEvents(t *testing.T, sessionID string) []EventEnvelope {
	t.Helper()

	data, err := os.ReadFile(EventSpoolPath(sessionID))
	if err != nil {
		t.Fatalf("read durable events: %v", err)
	}

	sc := bufio.NewScanner(strings.NewReader(string(data)))
	var events []EventEnvelope
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev EventEnvelope
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("parse durable event line: %v", err)
		}
		events = append(events, ev)
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan durable events: %v", err)
	}
	return events
}

type fakeOpenAIRecorder struct {
	mu     sync.Mutex
	events []UsageEvent
}

func (r *fakeOpenAIRecorder) RecordUsage(u UsageEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, u)
}

func (r *fakeOpenAIRecorder) LastEvent() (UsageEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return UsageEvent{}, false
	}
	return r.events[len(r.events)-1], true
}

func (r *fakeOpenAIRecorder) EventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

type fakeUpstream struct {
	mu             sync.Mutex
	hits           int
	lastAuth       string
	lastAPIKey     string
	lastVersion    string
	lastBeta       string
	lastBody       string
	responseStatus int
	responseBody   string
}

func (u *fakeUpstream) Hit(auth, apiKey, version, beta, body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.hits++
	u.lastAuth = auth
	u.lastAPIKey = apiKey
	u.lastVersion = version
	u.lastBeta = beta
	u.lastBody = body
}

func (u *fakeUpstream) CurrentResponse() (int, string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.responseStatus, u.responseBody
}

func (u *fakeUpstream) SetResponse(status int, body string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.responseStatus = status
	u.responseBody = body
}

func (u *fakeUpstream) Hits() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.hits
}

func (u *fakeUpstream) LastAuthorization() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastAuth
}

func (u *fakeUpstream) LastAPIKey() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastAPIKey
}

func (u *fakeUpstream) LastAnthropicVersion() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastVersion
}

func (u *fakeUpstream) LastAnthropicBeta() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastBeta
}

func (u *fakeUpstream) LastBody() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.lastBody
}

type openAIFixture struct {
	Server   SpawnResult
	Recorder *fakeOpenAIRecorder
	Upstream *fakeUpstream
}

func startProxyWithFakeOpenAIUpstream(t *testing.T) openAIFixture {
	t.Helper()

	recorder := &fakeOpenAIRecorder{}
	upstreamSpy := &fakeUpstream{
		responseStatus: http.StatusOK,
		responseBody:   `{"id":"chatcmpl-1","model":"gpt-5.4","usage":{"prompt_tokens":70,"completion_tokens":70,"total_tokens":140},"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`,
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		upstreamSpy.Hit(
			strings.TrimSpace(r.Header.Get("Authorization")),
			strings.TrimSpace(r.Header.Get("x-api-key")),
			strings.TrimSpace(r.Header.Get("anthropic-version")),
			strings.TrimSpace(r.Header.Get("anthropic-beta")),
			string(body),
		)

		status, respBody := upstreamSpy.CurrentResponse()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(upstream.Close)

	origNewProxyServer := newProxyServer
	newProxyServer = func(cfg RuntimeConfig) *Server {
		return NewServer(cfg, recorder, nil)
	}
	t.Cleanup(func() {
		newProxyServer = origNewProxyServer
	})

	cfg := RuntimeConfig{
		SessionID:   "sess-openai",
		ListenAddr:  reserveListenAddrForTest(t),
		AuthToken:   "tok-openai",
		ProviderURL: upstream.URL,
		ProviderKey: "provider-key",
	}

	result, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        result.PID,
			ListenAddr: result.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: result.ConfigPath,
		})
	})
	return openAIFixture{
		Server:   result,
		Recorder: recorder,
		Upstream: upstreamSpy,
	}
}

func startProxyWithFakeOpenAIResponsesUpstream(t *testing.T) openAIFixture {
	t.Helper()

	recorder := &fakeOpenAIRecorder{}
	upstreamSpy := &fakeUpstream{
		responseStatus: http.StatusOK,
		responseBody:   `{"id":"resp_1","model":"gpt-5.4","usage":{"input_tokens":70,"output_tokens":70,"total_tokens":140},"output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}]}`,
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		upstreamSpy.Hit(
			strings.TrimSpace(r.Header.Get("Authorization")),
			strings.TrimSpace(r.Header.Get("x-api-key")),
			strings.TrimSpace(r.Header.Get("anthropic-version")),
			strings.TrimSpace(r.Header.Get("anthropic-beta")),
			string(body),
		)

		status, respBody := upstreamSpy.CurrentResponse()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(upstream.Close)

	origNewProxyServer := newProxyServer
	newProxyServer = func(cfg RuntimeConfig) *Server {
		return NewServer(cfg, recorder, nil)
	}
	t.Cleanup(func() {
		newProxyServer = origNewProxyServer
	})

	cfg := RuntimeConfig{
		SessionID:   "sess-openai",
		ListenAddr:  reserveListenAddrForTest(t),
		AuthToken:   "tok-openai",
		ProviderURL: upstream.URL,
		ProviderKey: "provider-key",
	}

	result, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        result.PID,
			ListenAddr: result.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: result.ConfigPath,
		})
	})
	return openAIFixture{
		Server:   result,
		Recorder: recorder,
		Upstream: upstreamSpy,
	}
}

type anthropicFixture struct {
	Server   SpawnResult
	Recorder *fakeOpenAIRecorder
	Upstream *fakeUpstream
}

func startProxyWithFakeAnthropicUpstream(t *testing.T) anthropicFixture {
	t.Helper()

	recorder := &fakeOpenAIRecorder{}
	upstreamSpy := &fakeUpstream{
		responseStatus: http.StatusOK,
		responseBody:   `{"id":"msg_1","type":"message","role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":{"input_tokens":120,"output_tokens":90}}`,
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()
		upstreamSpy.Hit(
			strings.TrimSpace(r.Header.Get("Authorization")),
			strings.TrimSpace(r.Header.Get("x-api-key")),
			strings.TrimSpace(r.Header.Get("anthropic-version")),
			strings.TrimSpace(r.Header.Get("anthropic-beta")),
			string(body),
		)

		status, respBody := upstreamSpy.CurrentResponse()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(upstream.Close)

	origNewProxyServer := newProxyServer
	newProxyServer = func(cfg RuntimeConfig) *Server {
		return NewServer(cfg, recorder, nil)
	}
	t.Cleanup(func() {
		newProxyServer = origNewProxyServer
	})

	cfg := RuntimeConfig{
		SessionID:   "sess-anthropic",
		ListenAddr:  reserveListenAddrForTest(t),
		AuthToken:   "tok-anthropic",
		ProviderURL: upstream.URL,
		ProviderKey: "provider-key",
	}

	result, err := Spawn(cfg)
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	t.Cleanup(func() {
		_ = Stop(StopRequest{
			PID:        result.PID,
			ListenAddr: result.ListenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: result.ConfigPath,
		})
	})
	return anthropicFixture{
		Server:   result,
		Recorder: recorder,
		Upstream: upstreamSpy,
	}
}

func reserveListenAddrForTest(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}
