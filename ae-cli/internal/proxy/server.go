package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	testChildEnv      = "AE_PROXY_TEST_CHILD"
	forceChildEnv     = "AE_PROXY_FORCE_CHILD"
	internalConfigArg = "--config"
)

type SpawnResult struct {
	PID        int
	ListenAddr string
	ConfigPath string
}

type StopRequest struct {
	PID        int
	ListenAddr string
	AuthToken  string
	ConfigPath string
}

var (
	inProcessMu    sync.Mutex
	inProcessNext  = 100000
	inProcessProxy = map[int]func(){}
	newProxyServer = func(cfg RuntimeConfig) *Server {
		return NewServer(cfg, nil, nil)
	}
)

func Spawn(cfg RuntimeConfig) (SpawnResult, error) {
	if strings.TrimSpace(cfg.SessionID) == "" {
		return SpawnResult{}, fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return SpawnResult{}, fmt.Errorf("auth_token is required")
	}

	listenAddr := strings.TrimSpace(cfg.ListenAddr)
	if listenAddr == "" || strings.HasSuffix(listenAddr, ":0") {
		resolved, err := reserveLoopbackAddr()
		if err != nil {
			return SpawnResult{}, fmt.Errorf("resolving listen addr: %w", err)
		}
		listenAddr = resolved
	}
	cfg.ListenAddr = listenAddr

	configPath, err := writeRuntimeConfig(cfg)
	if err != nil {
		return SpawnResult{}, err
	}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			_ = cleanupConfigPath(configPath)
		}
	}()

	exe, err := os.Executable()
	if err != nil {
		return SpawnResult{}, fmt.Errorf("resolving executable: %w", err)
	}

	if isTestBinary(exe) && os.Getenv(forceChildEnv) != "1" {
		pid, err := spawnInProcess(cfg)
		if err != nil {
			return SpawnResult{}, err
		}
		cleanupOnError = false
		return SpawnResult{
			PID:        pid,
			ListenAddr: listenAddr,
			ConfigPath: configPath,
		}, nil
	}

	cmd, err := childCommand(exe, configPath)
	if err != nil {
		return SpawnResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return SpawnResult{}, fmt.Errorf("starting proxy process: %w", err)
	}

	if err := waitForReady(listenAddr, 3*time.Second); err != nil {
		_ = Stop(StopRequest{
			PID:        cmd.Process.Pid,
			ListenAddr: listenAddr,
			AuthToken:  cfg.AuthToken,
			ConfigPath: configPath,
		})
		return SpawnResult{}, err
	}
	cleanupOnError = false
	return SpawnResult{
		PID:        cmd.Process.Pid,
		ListenAddr: listenAddr,
		ConfigPath: configPath,
	}, nil
}

func Stop(req StopRequest) error {
	if req.PID <= 0 && strings.TrimSpace(req.ListenAddr) == "" {
		return cleanupConfigPath(req.ConfigPath)
	}

	inProcessMu.Lock()
	stopper, ok := inProcessProxy[req.PID]
	if ok {
		delete(inProcessProxy, req.PID)
	}
	inProcessMu.Unlock()
	if ok {
		stopper()
		return cleanupConfigPath(req.ConfigPath)
	}

	stopped, err := stopByAuthenticatedEndpoint(req.ListenAddr, req.AuthToken)
	if err != nil {
		return err
	}
	if !stopped {
		stopped, err = stopByPID(req.PID)
		if err != nil {
			return err
		}
	}
	if !stopped {
		return fmt.Errorf("failed to stop proxy process pid=%d", req.PID)
	}
	return cleanupConfigPath(req.ConfigPath)
}

func stopByPID(pid int) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false, fmt.Errorf("finding process %d: %w", pid, err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		// Process already exited.
		return true, nil
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := p.Signal(syscall.Signal(0)); err != nil {
			return true, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := p.Signal(syscall.SIGKILL); err != nil {
		return false, nil
	}
	return true, nil
}

func stopByAuthenticatedEndpoint(listenAddr, authToken string) (bool, error) {
	listenAddr = strings.TrimSpace(listenAddr)
	authToken = strings.TrimSpace(authToken)
	if listenAddr == "" || authToken == "" {
		return false, nil
	}

	req, err := http.NewRequest(http.MethodPost, "http://"+listenAddr+"/__internal/stop", nil)
	if err != nil {
		return false, fmt.Errorf("creating shutdown request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+authToken)

	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		return false, nil
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return false, nil
	}
	if err := waitForDown(listenAddr, 2*time.Second); err != nil {
		return false, nil
	}
	return true, nil
}

func Serve(ctx context.Context, cfg RuntimeConfig) error {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return fmt.Errorf("listen_addr is required")
	}

	internalToken := strings.TrimSpace(cfg.AuthToken)
	proxyServer := newProxyServer(cfg)
	stopCh := make(chan struct{})
	var stopOnce sync.Once

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})
	mux.HandleFunc("/__internal/stop", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		want := "Bearer " + internalToken
		if internalToken == "" || strings.TrimSpace(r.Header.Get("Authorization")) != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		stopOnce.Do(func() {
			close(stopCh)
		})
		w.WriteHeader(http.StatusAccepted)
	})
	mux.HandleFunc("/api/v1/session-events", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		want := "Bearer " + internalToken
		if internalToken == "" || strings.TrimSpace(r.Header.Get("Authorization")) != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		var event EventEnvelope
		if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(event.EventType) == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Minimal ingress behavior for Task 6: acknowledge validated event locally.
		w.WriteHeader(http.StatusCreated)
	})
	mux.HandleFunc("/openai/v1/chat/completions", proxyServer.handleOpenAIChatCompletions)
	mux.HandleFunc("/anthropic/v1/messages", proxyServer.handleAnthropicMessages)

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 3 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-errCh
		return nil
	case <-stopCh:
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		<-errCh
		return nil
	case err := <-errCh:
		return err
	}
}

func ServeFromConfigFile(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("reading config: %w", err)
	}
	var cfg RuntimeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parsing config: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	go func() {
		<-sigCh
		cancel()
	}()
	return Serve(ctx, cfg)
}

func childCommand(exe, configPath string) (*exec.Cmd, error) {
	var cmd *exec.Cmd
	if isTestBinary(exe) {
		cmd = exec.Command(exe, "-test.run=TestProxyChildProcess", "--", configPath)
		cmd.Env = append(os.Environ(), testChildEnv+"=1")
	} else {
		cmd = exec.Command(exe, "proxy-serve-internal", internalConfigArg, configPath)
	}
	return cmd, nil
}

func isTestBinary(exe string) bool {
	base := filepath.Base(exe)
	return strings.HasSuffix(base, ".test")
}

func reserveLoopbackAddr() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer ln.Close()
	return ln.Addr().String(), nil
}

func writeRuntimeConfig(cfg RuntimeConfig) (string, error) {
	d, err := os.MkdirTemp("", "ae-proxy-config-*")
	if err != nil {
		return "", fmt.Errorf("creating config dir: %w", err)
	}
	path := filepath.Join(d, "runtime.json")
	data, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("writing config: %w", err)
	}
	return path, nil
}

func cleanupConfigPath(configPath string) error {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return nil
	}
	if err := os.RemoveAll(filepath.Dir(configPath)); err != nil {
		return fmt.Errorf("removing proxy config dir: %w", err)
	}
	return nil
}

func waitForReady(listenAddr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := fmt.Sprintf("http://%s/healthz", listenAddr)
	client := &http.Client{Timeout: 250 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(40 * time.Millisecond)
	}
	return fmt.Errorf("proxy process did not become ready at %s", listenAddr)
}

func waitForDown(listenAddr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 250 * time.Millisecond}
	url := fmt.Sprintf("http://%s/healthz", listenAddr)
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err != nil {
			return nil
		}
		_ = resp.Body.Close()
		time.Sleep(40 * time.Millisecond)
	}
	return fmt.Errorf("proxy did not stop at %s", listenAddr)
}

func spawnInProcess(cfg RuntimeConfig) (int, error) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- Serve(ctx, cfg)
	}()
	if err := waitForReady(cfg.ListenAddr, 3*time.Second); err != nil {
		cancel()
		<-errCh
		return 0, err
	}

	inProcessMu.Lock()
	inProcessNext++
	pid := inProcessNext
	inProcessProxy[pid] = func() {
		cancel()
		<-errCh
	}
	inProcessMu.Unlock()
	return pid, nil
}
