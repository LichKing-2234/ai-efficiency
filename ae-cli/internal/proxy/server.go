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

var (
	inProcessMu    sync.Mutex
	inProcessNext  = 100000
	inProcessProxy = map[int]context.CancelFunc{}
)

func Spawn(cfg RuntimeConfig) (int, string, error) {
	if strings.TrimSpace(cfg.SessionID) == "" {
		return 0, "", fmt.Errorf("session_id is required")
	}
	if strings.TrimSpace(cfg.AuthToken) == "" {
		return 0, "", fmt.Errorf("auth_token is required")
	}

	listenAddr := strings.TrimSpace(cfg.ListenAddr)
	if listenAddr == "" || strings.HasSuffix(listenAddr, ":0") {
		resolved, err := reserveLoopbackAddr()
		if err != nil {
			return 0, "", fmt.Errorf("resolving listen addr: %w", err)
		}
		listenAddr = resolved
	}
	cfg.ListenAddr = listenAddr

	configPath, err := writeRuntimeConfig(cfg)
	if err != nil {
		return 0, "", err
	}

	exe, err := os.Executable()
	if err != nil {
		return 0, "", fmt.Errorf("resolving executable: %w", err)
	}

	if isTestBinary(exe) && os.Getenv(forceChildEnv) != "1" {
		pid, err := spawnInProcess(cfg)
		if err != nil {
			return 0, "", err
		}
		return pid, listenAddr, nil
	}

	cmd, err := childCommand(exe, configPath)
	if err != nil {
		return 0, "", err
	}
	if err := cmd.Start(); err != nil {
		return 0, "", fmt.Errorf("starting proxy process: %w", err)
	}

	if err := waitForReady(listenAddr, 3*time.Second); err != nil {
		_ = Stop(cmd.Process.Pid)
		return 0, "", err
	}
	return cmd.Process.Pid, listenAddr, nil
}

func Stop(pid int) error {
	if pid <= 0 {
		return nil
	}
	inProcessMu.Lock()
	cancel, ok := inProcessProxy[pid]
	if ok {
		delete(inProcessProxy, pid)
	}
	inProcessMu.Unlock()
	if ok {
		cancel()
		return nil
	}

	p, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("finding process %d: %w", pid, err)
	}
	if err := p.Signal(syscall.SIGTERM); err != nil {
		// Process already exited.
		return nil
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := p.Signal(syscall.Signal(0)); err != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := p.Signal(syscall.SIGKILL); err != nil {
		return nil
	}
	return nil
}

func Serve(ctx context.Context, cfg RuntimeConfig) error {
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		return fmt.Errorf("listen_addr is required")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready"))
	})

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
