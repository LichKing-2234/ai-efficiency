package proxy

import (
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
	srv, recorder := startProxyWithFakeOpenAIUpstream(t)

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
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	if recorder.LastUsage.TotalTokens != 140 {
		t.Fatalf("total_tokens = %d, want 140", recorder.LastUsage.TotalTokens)
	}
}

func TestProxyOpenAIResponses_RejectsUnauthorizedRequest(t *testing.T) {
	srv, _ := startProxyWithFakeOpenAIUpstream(t)

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
}

type fakeOpenAIRecorder struct {
	mu        sync.Mutex
	LastUsage UsageEvent
}

func (r *fakeOpenAIRecorder) RecordUsage(u UsageEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.LastUsage = u
}

func startProxyWithFakeOpenAIUpstream(t *testing.T) (SpawnResult, *fakeOpenAIRecorder) {
	t.Helper()

	recorder := &fakeOpenAIRecorder{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()

		body := []byte(`{"id":"chatcmpl-1","model":"gpt-5.4","usage":{"prompt_tokens":70,"completion_tokens":70,"total_tokens":140},"choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}]}`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
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
	return result, recorder
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
