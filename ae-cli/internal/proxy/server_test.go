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
