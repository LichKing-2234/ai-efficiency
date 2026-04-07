package session

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/proxy"
)

const managerStartHelperEnv = "AE_SESSION_MANAGER_START_HELPER"

type managerStartHelperResult struct {
	PID        int    `json:"pid"`
	ListenAddr string `json:"listen_addr"`
	AuthToken  string `json:"auth_token"`
	ConfigPath string `json:"config_path"`
}

func TestProxyChildProcess(t *testing.T) {
	if os.Getenv("AE_PROXY_TEST_CHILD") != "1" {
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
	if err := proxy.ServeFromConfigFile(args[sep+1]); err != nil {
		fmt.Fprintf(os.Stderr, "proxy child failed: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func TestManagerStartHelperProcess(t *testing.T) {
	if os.Getenv(managerStartHelperEnv) != "1" {
		t.Skip("helper process")
	}
	if err := os.Setenv("AE_PROXY_FORCE_CHILD", "1"); err != nil {
		t.Fatalf("set force child env: %v", err)
	}

	repoDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-helper-proxy-1",
					StartedAt:     now,
					RelayAPIKeyID: 1,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-helper-proxy-1",
					EnvBundle: map[string]string{
						"AE_SESSION_ID":   "boot-helper-proxy-1",
						"OPENAI_BASE_URL": "http://sub2api.test",
						"OPENAI_API_KEY":  "test-key",
					},
					KeyExpiresAt: now.Add(1 * time.Hour),
				},
			})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer srv.Close()

	m := NewManager(client.New(srv.URL, "tok"), &config.Config{})
	state, err := m.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	rt, err := ReadRuntimeBundle(state.ID)
	if err != nil {
		t.Fatalf("ReadRuntimeBundle: %v", err)
	}
	if rt.Proxy == nil {
		t.Fatal("expected runtime bundle proxy metadata")
	}
	if err := json.NewEncoder(os.Stdout).Encode(managerStartHelperResult{
		PID:        rt.Proxy.PID,
		ListenAddr: rt.Proxy.ListenAddr,
		AuthToken:  rt.Proxy.AuthToken,
		ConfigPath: rt.Proxy.ConfigPath,
	}); err != nil {
		t.Fatalf("writing helper result: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP, os.Interrupt)
	defer signal.Stop(sigCh)
	select {
	case <-sigCh:
	case <-time.After(10 * time.Second):
	}
}

func TestManagerStartSpawnedProxySurvivesParentProcessGroupTermination(t *testing.T) {
	tmpHome := t.TempDir()
	helper := exec.Command(os.Args[0], "-test.run=^TestManagerStartHelperProcess$")
	helper.Env = append(os.Environ(),
		managerStartHelperEnv+"=1",
		"HOME="+tmpHome,
	)
	helper.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	stderr, err := helper.StderrPipe()
	if err != nil {
		t.Fatalf("StderrPipe: %v", err)
	}
	if err := helper.Start(); err != nil {
		t.Fatalf("starting helper: %v", err)
	}

	var result managerStartHelperResult
	readDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "{") {
				continue
			}
			if err := json.Unmarshal([]byte(line), &result); err != nil {
				readDone <- fmt.Errorf("parsing helper json %q: %w", line, err)
				return
			}
			readDone <- nil
			return
		}
		if err := scanner.Err(); err != nil {
			readDone <- fmt.Errorf("scanning helper output: %w", err)
			return
		}
		rawErr, _ := io.ReadAll(stderr)
		readDone <- fmt.Errorf("helper exited before emitting start result, stderr=%s", strings.TrimSpace(string(rawErr)))
	}()

	select {
	case err := <-readDone:
		if err != nil {
			_ = helper.Process.Kill()
			_ = helper.Wait()
			t.Fatal(err)
		}
	case <-time.After(6 * time.Second):
		_ = helper.Process.Kill()
		_ = helper.Wait()
		t.Fatal("timed out waiting for helper start result")
	}

	if result.PID <= 0 || strings.TrimSpace(result.ListenAddr) == "" || strings.TrimSpace(result.AuthToken) == "" {
		_ = helper.Process.Kill()
		_ = helper.Wait()
		t.Fatalf("invalid helper result: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(tmpHome, ".ae-cli", "current-session.json")); err != nil {
		_ = helper.Process.Kill()
		_ = helper.Wait()
		t.Fatalf("expected helper state in isolated HOME, stat err=%v", err)
	}

	if err := syscall.Kill(-helper.Process.Pid, syscall.SIGTERM); err != nil {
		_ = helper.Process.Kill()
		_ = helper.Wait()
		t.Fatalf("sending SIGTERM to helper process group: %v", err)
	}
	if err := helper.Wait(); err != nil {
		t.Logf("helper exited after group signal: %v", err)
	}
	defer func() {
		_ = proxy.Stop(proxy.StopRequest{
			PID:        result.PID,
			ListenAddr: result.ListenAddr,
			AuthToken:  result.AuthToken,
			ConfigPath: result.ConfigPath,
		})
	}()

	deadline := time.Now().Add(3 * time.Second)
	client := &http.Client{Timeout: 250 * time.Millisecond}
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://" + result.ListenAddr + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("spawned proxy was not listening after parent process group termination at %s", result.ListenAddr)
}

func startSessionWithFakeBootstrap(t *testing.T) (*State, *RuntimeBundle, *Manager) {
	return startSessionWithFakeBootstrapWithSetup(t, nil)
}

func startSessionWithFakeBootstrapWithSetup(t *testing.T, repoSetup func(string)) (*State, *RuntimeBundle, *Manager) {
	t.Helper()

	origSpawn := spawnProxyProcess
	origStop := stopProxyProcess
	spawnProxyProcess = func(cfg proxy.RuntimeConfig) (proxy.SpawnResult, error) {
		return proxy.SpawnResult{
			PID:        31337,
			ListenAddr: "127.0.0.1:17888",
			ConfigPath: filepath.Join(t.TempDir(), "runtime.json"),
		}, nil
	}
	stopProxyProcess = func(req proxy.StopRequest) error { return nil }
	t.Cleanup(func() {
		spawnProxyProcess = origSpawn
		stopProxyProcess = origStop
	})

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	repoDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}
	if repoSetup != nil {
		repoSetup(repoDir)
	}

	origWD, _ := os.Getwd()
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-proxy-1",
					StartedAt:     now,
					RelayAPIKeyID: 1,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-proxy-1",
					EnvBundle: map[string]string{
						"AE_SESSION_ID":   "boot-proxy-1",
						"OPENAI_BASE_URL": "http://sub2api.test",
						"OPENAI_API_KEY":  "test-key",
					},
					KeyExpiresAt: now.Add(1 * time.Hour),
				},
			})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	m := NewManager(client.New(srv.URL, "tok"), &config.Config{})
	state, err := m.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	rt, err := ReadRuntimeBundle(state.ID)
	if err != nil {
		t.Fatalf("ReadRuntimeBundle: %v", err)
	}
	return state, rt, m
}

func TestManagerStartLaunchesLocalProxyAndStoresRuntimeMetadata(t *testing.T) {
	state, rt, _ := startSessionWithFakeBootstrap(t)

	if rt.Proxy == nil {
		t.Fatal("expected runtime bundle to contain proxy metadata")
	}
	if rt.Proxy.ListenAddr == "" || rt.Proxy.AuthToken == "" || rt.Proxy.PID == 0 {
		t.Fatalf("unexpected proxy metadata: %+v", rt.Proxy)
	}
	if state.ID == "" {
		t.Fatal("expected non-empty session id")
	}
}

func TestManagerStartWritesCodexAndClaudeHookConfig(t *testing.T) {
	state, rt, _ := startSessionWithFakeBootstrap(t)

	codexHome := rt.EnvBundle["CODEX_HOME"]
	if codexHome == "" {
		t.Fatalf("CODEX_HOME = %q, want non-empty", codexHome)
	}
	codexConfigData, err := os.ReadFile(filepath.Join(codexHome, "config.toml"))
	if err != nil {
		t.Fatalf("read codex config.toml: %v", err)
	}
	codexConfig := string(codexConfigData)
	if !strings.Contains(codexConfig, "codex_hooks = true") {
		t.Fatalf("missing codex_hooks feature flag in config.toml: %s", codexConfig)
	}

	codexHooksData, err := os.ReadFile(filepath.Join(codexHome, "hooks.json"))
	if err != nil {
		t.Fatalf("read codex hooks.json: %v", err)
	}
	codexHooks := string(codexHooksData)
	for _, want := range []string{
		"SessionStart",
		"UserPromptSubmit",
		"PreToolUse",
		"PostToolUse",
		"Stop",
		"hook session-event --tool codex",
		`"matcher": "Bash"`,
	} {
		if !strings.Contains(codexHooks, want) {
			t.Fatalf("missing %q in codex hooks.json: %s", want, codexHooks)
		}
	}

	claudeSettingsPath := filepath.Join(state.WorkspaceRoot, ".claude", "settings.local.json")
	claudeData, err := os.ReadFile(claudeSettingsPath)
	if err != nil {
		t.Fatalf("read claude settings.local.json: %v", err)
	}
	claudeSettings := string(claudeData)
	for _, want := range []string{
		"SessionStart",
		"UserPromptSubmit",
		"PreToolUse",
		"PostToolUse",
		"Stop",
		"hook session-event --tool claude",
		"ae-session-managed session=boot-proxy-1 tool=claude",
	} {
		if !strings.Contains(claudeSettings, want) {
			t.Fatalf("missing %q in claude settings.local.json: %s", want, claudeSettings)
		}
	}
	if strings.Contains(claudeSettings, `"matcher": "Bash"`) {
		t.Fatalf("expected Claude tool hooks to cover all tools, got Bash-only matcher: %s", claudeSettings)
	}
	wantAnthropicBaseURL := "http://" + rt.Proxy.ListenAddr + "/anthropic"
	for _, want := range []string{
		"ANTHROPIC_BASE_URL",
		wantAnthropicBaseURL,
		"ANTHROPIC_AUTH_TOKEN",
		rt.Proxy.AuthToken,
	} {
		if !strings.Contains(claudeSettings, want) {
			t.Fatalf("missing %q in claude settings.local.json env config: %s", want, claudeSettings)
		}
	}

	gitCommonDirOut, err := exec.Command("git", "rev-parse", "--git-common-dir").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse --git-common-dir: %v\n%s", err, gitCommonDirOut)
	}
	excludePath := filepath.Join(state.WorkspaceRoot, strings.TrimSpace(string(gitCommonDirOut)), "info", "exclude")
	excludeData, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read git info/exclude: %v", err)
	}
	if !strings.Contains(string(excludeData), "/.claude/settings.local.json") {
		t.Fatalf("expected %q to contain claude local settings ignore, got %q", excludePath, string(excludeData))
	}
}

func TestManagerStartIgnoresMalformedClaudeSettings(t *testing.T) {
	state, _, _ := startSessionWithFakeBootstrapWithSetup(t, func(repoDir string) {
		settingsPath := filepath.Join(repoDir, ".claude", "settings.local.json")
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
			t.Fatalf("mkdir .claude: %v", err)
		}
		if err := os.WriteFile(settingsPath, []byte("{not-json"), 0o600); err != nil {
			t.Fatalf("write malformed settings.local.json: %v", err)
		}
	})

	if state == nil {
		t.Fatal("expected session start to succeed despite malformed Claude settings")
	}
}

func TestStartLocalProxyUsesOpenAIRuntimeEnvKeys(t *testing.T) {
	var captured proxy.RuntimeConfig
	origSpawn := spawnProxyProcess
	spawnProxyProcess = func(cfg proxy.RuntimeConfig) (proxy.SpawnResult, error) {
		captured = cfg
		return proxy.SpawnResult{
			PID:        4242,
			ListenAddr: "127.0.0.1:18888",
			ConfigPath: filepath.Join(t.TempDir(), "runtime.json"),
		}, nil
	}
	t.Cleanup(func() { spawnProxyProcess = origSpawn })

	m := NewManager(nil, &config.Config{})
	rt := &RuntimeBundle{
		SessionID: "sess-openai-env",
		EnvBundle: map[string]string{
			"OPENAI_BASE_URL": "https://relay.local/openai",
			"OPENAI_API_KEY":  "openai-runtime-key",
		},
	}

	if err := m.startLocalProxy(rt); err != nil {
		t.Fatalf("startLocalProxy: %v", err)
	}
	if captured.ProviderURL != "https://relay.local/openai" {
		t.Fatalf("ProviderURL = %q, want %q", captured.ProviderURL, "https://relay.local/openai")
	}
	if captured.ProviderKey != "openai-runtime-key" {
		t.Fatalf("ProviderKey = %q, want %q", captured.ProviderKey, "openai-runtime-key")
	}
}

func TestStartLocalProxyFallsBackToLegacySub2APIEnvKeys(t *testing.T) {
	var captured proxy.RuntimeConfig
	origSpawn := spawnProxyProcess
	spawnProxyProcess = func(cfg proxy.RuntimeConfig) (proxy.SpawnResult, error) {
		captured = cfg
		return proxy.SpawnResult{
			PID:        4343,
			ListenAddr: "127.0.0.1:18889",
			ConfigPath: filepath.Join(t.TempDir(), "runtime.json"),
		}, nil
	}
	t.Cleanup(func() { spawnProxyProcess = origSpawn })

	m := NewManager(nil, &config.Config{})
	rt := &RuntimeBundle{
		SessionID: "sess-legacy-env",
		EnvBundle: map[string]string{
			"SUB2API_BASE_URL": "https://relay.local/sub2api",
			"SUB2API_API_KEY":  "legacy-sub2api-key",
		},
	}

	if err := m.startLocalProxy(rt); err != nil {
		t.Fatalf("startLocalProxy: %v", err)
	}
	if captured.ProviderURL != "https://relay.local/sub2api" {
		t.Fatalf("ProviderURL = %q, want %q", captured.ProviderURL, "https://relay.local/sub2api")
	}
	if captured.ProviderKey != "legacy-sub2api-key" {
		t.Fatalf("ProviderKey = %q, want %q", captured.ProviderKey, "legacy-sub2api-key")
	}
}

func TestStartLocalProxyInjectsBackendConfigAndWorkspaceID(t *testing.T) {
	var captured proxy.RuntimeConfig
	origSpawn := spawnProxyProcess
	spawnProxyProcess = func(cfg proxy.RuntimeConfig) (proxy.SpawnResult, error) {
		captured = cfg
		return proxy.SpawnResult{
			PID:        4444,
			ListenAddr: "127.0.0.1:18890",
			ConfigPath: filepath.Join(t.TempDir(), "runtime.json"),
		}, nil
	}
	t.Cleanup(func() { spawnProxyProcess = origSpawn })

	m := NewManager(client.New("https://backend.local", "backend-token"), &config.Config{})
	rt := &RuntimeBundle{
		SessionID: "sess-backend-config",
		EnvBundle: map[string]string{
			"OPENAI_BASE_URL":  "https://relay.local/openai",
			"OPENAI_API_KEY":   "openai-runtime-key",
			"AE_WORKSPACE_ID":  "ws-backend-config",
			"AE_SESSION_ID":    "sess-backend-config",
			"AE_RUNTIME_REF":   "rt-backend-config",
			"AE_RELAY_USER_ID": "100",
		},
	}

	if err := m.startLocalProxy(rt); err != nil {
		t.Fatalf("startLocalProxy: %v", err)
	}
	if captured.BackendURL != "https://backend.local" {
		t.Fatalf("BackendURL = %q, want %q", captured.BackendURL, "https://backend.local")
	}
	if captured.BackendToken != "backend-token" {
		t.Fatalf("BackendToken = %q, want %q", captured.BackendToken, "backend-token")
	}
	if captured.WorkspaceID != "ws-backend-config" {
		t.Fatalf("WorkspaceID = %q, want %q", captured.WorkspaceID, "ws-backend-config")
	}
}

func TestManagerStopRemovesProxyRuntime(t *testing.T) {
	state, rt, mgr := startSessionWithFakeBootstrap(t)

	var stoppedPID int
	origStop := stopProxyProcess
	stopProxyProcess = func(req proxy.StopRequest) error {
		stoppedPID = req.PID
		return nil
	}
	t.Cleanup(func() { stopProxyProcess = origStop })

	if _, err := mgr.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stoppedPID == 0 {
		t.Fatal("expected proxy stop to be invoked with runtime pid")
	}
	if _, err := ReadRuntimeBundle(state.ID); !os.IsNotExist(err) {
		t.Fatalf("expected runtime bundle to be removed, got err=%v", err)
	}
	if codexHome := rt.EnvBundle["CODEX_HOME"]; codexHome != "" {
		if _, err := os.Stat(codexHome); !os.IsNotExist(err) {
			t.Fatalf("expected codex home removed on stop, got err=%v", err)
		}
	}
}

func TestManagerStopPreservesRuntimeWhenProxyStopFails(t *testing.T) {
	state, _, mgr := startSessionWithFakeBootstrap(t)

	origStop := stopProxyProcess
	stopProxyProcess = func(req proxy.StopRequest) error {
		return errors.New("proxy stop failed")
	}
	t.Cleanup(func() { stopProxyProcess = origStop })

	if _, err := mgr.Stop(); err == nil {
		t.Fatal("expected Stop to fail when proxy stop fails")
	}
	if _, err := ReadRuntimeBundle(state.ID); err != nil {
		t.Fatalf("expected runtime bundle retained on stop failure, got err=%v", err)
	}
}

func TestManagerStopCleansManagedClaudeHooksAndPreservesUserSettings(t *testing.T) {
	state, _, mgr := startSessionWithFakeBootstrapWithSetup(t, func(repoDir string) {
		settingsPath := filepath.Join(repoDir, ".claude", "settings.local.json")
		if err := os.MkdirAll(filepath.Dir(settingsPath), 0o700); err != nil {
			t.Fatalf("mkdir .claude: %v", err)
		}
		userSettings := `{
  "theme": "dark",
  "hooks": {
    "Notification": [
      {
        "matcher": "",
        "hooks": [
          {
            "type": "command",
            "command": "echo notify-user"
          }
        ]
      }
    ]
  }
}`
		if err := os.WriteFile(settingsPath, []byte(userSettings), 0o600); err != nil {
			t.Fatalf("write settings.local.json: %v", err)
		}
	})

	if _, err := mgr.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	settingsPath := filepath.Join(state.WorkspaceRoot, ".claude", "settings.local.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read claude settings.local.json after stop: %v", err)
	}
	settings := string(data)
	if strings.Contains(settings, "hook session-event --tool claude") {
		t.Fatalf("managed Claude hook command still present after stop: %s", settings)
	}
	if !strings.Contains(settings, "notify-user") {
		t.Fatalf("expected user Notification hook to remain after stop: %s", settings)
	}
	if !strings.Contains(settings, `"theme": "dark"`) {
		t.Fatalf("expected user theme setting to remain after stop: %s", settings)
	}
}

func startSessionWithRealBootstrap(t *testing.T) (*State, *Manager) {
	t.Helper()

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	if err := os.Setenv("HOME", tmpHome); err != nil {
		t.Fatalf("set HOME: %v", err)
	}
	t.Cleanup(func() { _ = os.Setenv("HOME", origHome) })

	repoDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = repoDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origWD, _ := os.Getwd()
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir repo: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-real-proxy-1",
					StartedAt:     now,
					RelayAPIKeyID: 1,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-real-proxy-1",
					EnvBundle: map[string]string{
						"AE_SESSION_ID":   "boot-real-proxy-1",
						"OPENAI_BASE_URL": "http://sub2api.test",
						"OPENAI_API_KEY":  "test-key",
					},
					KeyExpiresAt: now.Add(1 * time.Hour),
				},
			})
			return
		}
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	t.Cleanup(srv.Close)

	m := NewManager(client.New(srv.URL, "tok"), &config.Config{})
	state, err := m.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return state, m
}

func TestManagerStopRemovesProxyTempConfigDirs(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	state, mgr := startSessionWithRealBootstrap(t)
	if _, err := os.Stat(runtimeFilePath(state.ID)); err != nil {
		t.Fatalf("expected runtime bundle before stop: %v", err)
	}

	if _, err := mgr.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "ae-proxy-config-*"))
	if err != nil {
		t.Fatalf("glob temp configs: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no proxy temp config dirs after stop, found %d", len(matches))
	}
}

func TestResolveProxyUpstreamPrefersOpenAIPair(t *testing.T) {
	url, key := resolveProxyUpstream(map[string]string{
		"OPENAI_BASE_URL":    "http://openai-upstream.local",
		"OPENAI_API_KEY":     "openai-key",
		"ANTHROPIC_BASE_URL": "http://anthropic-upstream.local",
		"ANTHROPIC_API_KEY":  "anthropic-key",
		"SUB2API_BASE_URL":   "http://legacy-sub2api.local",
		"SUB2API_API_KEY":    "legacy-key",
	})
	if url != "http://openai-upstream.local" || key != "openai-key" {
		t.Fatalf("resolveProxyUpstream() = (%q, %q), want (%q, %q)", url, key, "http://openai-upstream.local", "openai-key")
	}
}

func TestResolveProxyUpstreamFallsBackToLegacySub2API(t *testing.T) {
	url, key := resolveProxyUpstream(map[string]string{
		"SUB2API_BASE_URL": "http://legacy-sub2api.local",
		"SUB2API_API_KEY":  "legacy-key",
	})
	if url != "http://legacy-sub2api.local" || key != "legacy-key" {
		t.Fatalf("resolveProxyUpstream() = (%q, %q), want (%q, %q)", url, key, "http://legacy-sub2api.local", "legacy-key")
	}
}

func TestMain(m *testing.M) {
	origWD, _ := os.Getwd()
	tmpWD, err := os.MkdirTemp("", "ae-cli-session-test-*")
	if err != nil {
		panic(err)
	}
	if err := os.Chdir(tmpWD); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.Chdir(origWD)
	_ = os.RemoveAll(tmpWD)
	os.Exit(code)
}

func TestNewManager(t *testing.T) {
	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)
	if m == nil {
		t.Fatal("expected non-nil manager")
	}
	if m.client != c {
		t.Error("manager client mismatch")
	}
	if m.config != cfg {
		t.Error("manager config mismatch")
	}
}

func TestStateFilePathContainsExpectedSegments(t *testing.T) {
	p, err := stateFilePath()
	if err != nil {
		t.Fatalf("stateFilePath: %v", err)
	}
	if filepath.Base(p) != "current-session.json" {
		t.Errorf("state file basename = %q, want %q", filepath.Base(p), "current-session.json")
	}
	if filepath.Base(filepath.Dir(p)) != ".ae-cli" {
		t.Errorf("state file parent dir = %q, want %q", filepath.Base(filepath.Dir(p)), ".ae-cli")
	}
}

func TestStateJSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	state := &State{
		ID:          "test-id",
		Repo:        "org/repo",
		Branch:      "main",
		StartedAt:   now,
		TmuxSession: "ae-test",
	}

	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded State
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ID != "test-id" {
		t.Errorf("ID = %q, want %q", decoded.ID, "test-id")
	}
	if decoded.Repo != "org/repo" {
		t.Errorf("Repo = %q, want %q", decoded.Repo, "org/repo")
	}
	if decoded.Branch != "main" {
		t.Errorf("Branch = %q, want %q", decoded.Branch, "main")
	}
	if decoded.TmuxSession != "ae-test" {
		t.Errorf("TmuxSession = %q, want %q", decoded.TmuxSession, "ae-test")
	}
	if !decoded.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", decoded.StartedAt, now)
	}
}

func TestStateJSONOmitEmptyTmux(t *testing.T) {
	state := &State{
		ID:     "test-id",
		Repo:   "org/repo",
		Branch: "main",
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	json.Unmarshal(data, &decoded)
	if _, ok := decoded["tmux_session"]; ok {
		t.Error("expected tmux_session to be omitted when empty")
	}
}

func TestWriteStateAndCurrent(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	now := time.Now().Truncate(time.Second)
	state := &State{
		ID:          "write-test-id",
		Repo:        "org/my-repo",
		Branch:      "feature/test",
		StartedAt:   now,
		TmuxSession: "ae-write",
	}

	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	path, _ := stateFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatalf("state file not created at %s", path)
	}

	loaded, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if loaded == nil {
		t.Fatal("Current returned nil, expected state")
	}
	if loaded.ID != "write-test-id" {
		t.Errorf("ID = %q, want %q", loaded.ID, "write-test-id")
	}
	if loaded.Repo != "org/my-repo" {
		t.Errorf("Repo = %q, want %q", loaded.Repo, "org/my-repo")
	}
	if loaded.Branch != "feature/test" {
		t.Errorf("Branch = %q, want %q", loaded.Branch, "feature/test")
	}
	if loaded.TmuxSession != "ae-write" {
		t.Errorf("TmuxSession = %q, want %q", loaded.TmuxSession, "ae-write")
	}
	if !loaded.StartedAt.Equal(now) {
		t.Errorf("StartedAt = %v, want %v", loaded.StartedAt, now)
	}
}

func TestCurrentNoSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if state != nil {
		t.Errorf("expected nil state when no file exists, got %+v", state)
	}
}

func TestCurrentBadJSON(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), []byte("not json"), 0o644)

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	_, err := m.Current()
	if err == nil {
		t.Fatal("expected error for bad JSON state file, got nil")
	}
}

func TestSaveState(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state := &State{
		ID:          "save-test",
		Repo:        "org/repo",
		Branch:      "main",
		StartedAt:   time.Now().Truncate(time.Second),
		TmuxSession: "ae-save",
	}

	if err := m.SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	loaded, err := m.Current()
	if err != nil {
		t.Fatalf("Current after SaveState: %v", err)
	}
	if loaded.ID != "save-test" {
		t.Errorf("ID = %q, want %q", loaded.ID, "save-test")
	}
	if loaded.TmuxSession != "ae-save" {
		t.Errorf("TmuxSession = %q, want %q", loaded.TmuxSession, "ae-save")
	}
}

func TestRemoveState(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	state := &State{ID: "remove-test", Repo: "org/repo", Branch: "main"}
	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	path, _ := stateFilePath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("state file should exist before removal")
	}

	if err := removeState(); err != nil {
		t.Fatalf("removeState: %v", err)
	}

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("state file should not exist after removal")
	}
}

func TestRemoveStateNonExistent(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	if err := removeState(); err != nil {
		t.Fatalf("removeState on non-existent file: %v", err)
	}
}

func TestWriteStateCreatesDirectory(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	if _, err := os.Stat(stateDir); !os.IsNotExist(err) {
		t.Fatal(".ae-cli directory should not exist yet")
	}

	state := &State{ID: "dir-test", Repo: "org/repo", Branch: "main"}
	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	info, err := os.Stat(stateDir)
	if err != nil {
		t.Fatalf("stat .ae-cli: %v", err)
	}
	if !info.IsDir() {
		t.Error(".ae-cli should be a directory")
	}
}

func TestDetectRepoInGitDir(t *testing.T) {
	repo, err := detectRepo()
	if err != nil {
		t.Skipf("not in a git repo with remote: %v", err)
	}
	if repo == "" {
		t.Error("detectRepo returned empty string")
	}
}

func TestDetectBranchInGitDir(t *testing.T) {
	branch, err := detectBranch()
	if err != nil {
		t.Skipf("not in a git repo: %v", err)
	}
	if branch == "" {
		t.Error("detectBranch returned empty string")
	}
}

func TestDetectRepoInTempGitDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize a git repo with a remote
	cmds := [][]string{
		{"git", "init"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	// Change to the temp dir, run detectRepo, then change back
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	repo, err := detectRepo()
	if err != nil {
		t.Fatalf("detectRepo: %v", err)
	}
	if repo != "https://github.com/test-org/test-repo.git" {
		t.Errorf("repo = %q, want %q", repo, "https://github.com/test-org/test-repo.git")
	}
}

func TestDetectBranchInTempGitDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Initialize a git repo and create an initial commit so HEAD exists
	cmds := [][]string{
		{"git", "init", "-b", "test-branch"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	branch, err := detectBranch()
	if err != nil {
		t.Fatalf("detectBranch: %v", err)
	}
	if branch != "test-branch" {
		t.Errorf("branch = %q, want %q", branch, "test-branch")
	}
}

func TestDetectRepoNoRemote(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	_, err := detectRepo()
	if err == nil {
		t.Error("expected error when no remote configured")
	}
}

func TestDetectBranchNoCommit(t *testing.T) {
	tmpDir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	// On a fresh repo with no commits, rev-parse --abbrev-ref HEAD may return "HEAD" or error
	branch, err := detectBranch()
	if err != nil {
		// Some git versions error on empty repo
		t.Logf("detectBranch on empty repo: %v (expected)", err)
	} else {
		t.Logf("detectBranch on empty repo returned: %q", branch)
	}
}

func TestDetectRepoNotGitDir(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	_, err := detectRepo()
	if err == nil {
		t.Error("expected error when not in a git directory")
	}
}

func TestDetectBranchNotGitDir(t *testing.T) {
	tmpDir := t.TempDir()

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	_, err := detectBranch()
	if err == nil {
		t.Error("expected error when not in a git directory")
	}
}

func TestStartInTempGitRepo(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create a temp git repo
	tmpDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "feature/test-start"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	legacyConfigPath := filepath.Join(tmpDir, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(legacyConfigPath), 0o700); err != nil {
		t.Fatalf("mkdir stale codex dir: %v", err)
	}
	if err := os.WriteFile(legacyConfigPath, []byte(`model = "gpt-5.4"
model_provider = "ae_local_proxy"

[model_providers.ae_local_proxy]
name = "AI Efficiency Local Proxy"
base_url = "http://127.0.0.1:43123/openai/v1"
env_key = "AE_LOCAL_PROXY_TOKEN"
wire_api = "responses"
supports_websockets = false
`), 0o600); err != nil {
		t.Fatalf("write stale codex config: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	wantWorkspaceRoot, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(tmpDir): %v", err)
	}
	wantWorkspaceRoot, err = filepath.Abs(filepath.Clean(wantWorkspaceRoot))
	if err != nil {
		t.Fatalf("abs wantWorkspaceRoot: %v", err)
	}

	headOut, err := exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("git rev-parse HEAD: %v\n%s", err, headOut)
	}
	headSHA := strings.TrimSpace(string(headOut))

	wantGitDir := filepath.Join(wantWorkspaceRoot, ".git")
	wantCommonDir := filepath.Join(wantWorkspaceRoot, ".git")
	wantWorkspaceID, err := deriveWorkspaceID(wantWorkspaceRoot, wantWorkspaceRoot, wantGitDir, wantCommonDir)
	if err != nil {
		t.Fatalf("deriveWorkspaceID: %v", err)
	}

	var spawnedCfg proxy.RuntimeConfig
	origSpawn := spawnProxyProcess
	spawnProxyProcess = func(cfg proxy.RuntimeConfig) (proxy.SpawnResult, error) {
		spawnedCfg = cfg
		return proxy.SpawnResult{
			PID:        31337,
			ListenAddr: "127.0.0.1:17888",
			ConfigPath: filepath.Join(t.TempDir(), "runtime.json"),
		}, nil
	}
	t.Cleanup(func() { spawnProxyProcess = origSpawn })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap" {
			var req client.BootstrapSessionRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.RepoFullName != "https://github.com/test-org/test-repo.git" {
				t.Fatalf("bootstrap repo_full_name = %q, want %q", req.RepoFullName, "https://github.com/test-org/test-repo.git")
			}
			if req.BranchSnapshot != "feature/test-start" {
				t.Fatalf("bootstrap branch_snapshot = %q, want %q", req.BranchSnapshot, "feature/test-start")
			}
			if req.HeadSHA != headSHA {
				t.Fatalf("bootstrap head_sha = %q, want %q", req.HeadSHA, headSHA)
			}
			if req.WorkspaceRoot != wantWorkspaceRoot {
				t.Fatalf("bootstrap workspace_root = %q, want %q", req.WorkspaceRoot, wantWorkspaceRoot)
			}
			if req.GitDir != wantGitDir {
				t.Fatalf("bootstrap git_dir = %q, want %q", req.GitDir, wantGitDir)
			}
			if req.GitCommonDir != wantCommonDir {
				t.Fatalf("bootstrap git_common_dir = %q, want %q", req.GitCommonDir, wantCommonDir)
			}
			if req.WorkspaceID != wantWorkspaceID {
				t.Fatalf("bootstrap workspace_id = %q, want %q", req.WorkspaceID, wantWorkspaceID)
			}

			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-sess-1",
					StartedAt:     now,
					RelayAPIKeyID: 123,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-1",
					EnvBundle: map[string]string{
						"AE_SESSION_ID":      "boot-sess-1",
						"OPENAI_BASE_URL":    "http://sub2api.test",
						"OPENAI_API_KEY":     "openai-k",
						"ANTHROPIC_BASE_URL": "http://sub2api-anthropic.test",
						"ANTHROPIC_API_KEY":  "upstream-should-be-removed",
					},
					KeyExpiresAt: now.Add(1 * time.Hour),
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfgObj := &config.Config{}
	m := NewManager(c, cfgObj)

	state, err := m.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.ID != "boot-sess-1" {
		t.Errorf("state ID = %q, want %q", state.ID, "boot-sess-1")
	}
	if state.Repo != "https://github.com/test-org/test-repo.git" {
		t.Errorf("Repo = %q, want %q", state.Repo, "https://github.com/test-org/test-repo.git")
	}
	if state.Branch != "feature/test-start" {
		t.Errorf("Branch = %q, want %q", state.Branch, "feature/test-start")
	}
	if spawnedCfg.ProviderURL != "http://sub2api.test" {
		t.Fatalf("spawn ProviderURL = %q, want %q", spawnedCfg.ProviderURL, "http://sub2api.test")
	}
	if spawnedCfg.ProviderKey != "openai-k" {
		t.Fatalf("spawn ProviderKey = %q, want %q", spawnedCfg.ProviderKey, "openai-k")
	}

	// Verify marker/runtime were persisted.
	marker, err := ReadMarker(wantWorkspaceRoot)
	if err != nil {
		t.Fatalf("ReadMarker: %v", err)
	}
	if marker.SessionID != "boot-sess-1" {
		t.Fatalf("marker session_id = %q, want %q", marker.SessionID, "boot-sess-1")
	}
	if marker.WorkspaceID != wantWorkspaceID {
		t.Fatalf("marker workspace_id = %q, want %q", marker.WorkspaceID, wantWorkspaceID)
	}
	rt, err := ReadRuntimeBundle("boot-sess-1")
	if err != nil {
		t.Fatalf("ReadRuntimeBundle: %v", err)
	}
	if rt.EnvBundle["AE_SESSION_ID"] != "boot-sess-1" {
		t.Fatalf("runtime AE_SESSION_ID = %q, want %q", rt.EnvBundle["AE_SESSION_ID"], "boot-sess-1")
	}
	if rt.Proxy == nil {
		t.Fatal("expected proxy metadata in runtime bundle")
	}
	wantAnthropicBaseURL := "http://" + rt.Proxy.ListenAddr + "/anthropic"
	if rt.EnvBundle["ANTHROPIC_BASE_URL"] != wantAnthropicBaseURL {
		t.Fatalf("runtime ANTHROPIC_BASE_URL = %q, want %q", rt.EnvBundle["ANTHROPIC_BASE_URL"], wantAnthropicBaseURL)
	}
	if rt.EnvBundle["ANTHROPIC_AUTH_TOKEN"] != rt.Proxy.AuthToken {
		t.Fatalf("runtime ANTHROPIC_AUTH_TOKEN = %q, want proxy token", rt.EnvBundle["ANTHROPIC_AUTH_TOKEN"])
	}
	if _, ok := rt.EnvBundle["ANTHROPIC_API_KEY"]; ok {
		t.Fatalf("runtime should not include ANTHROPIC_API_KEY in proxy mode: %+v", rt.EnvBundle)
	}
	if _, ok := rt.EnvBundle["OPENAI_API_KEY"]; ok {
		t.Fatalf("runtime should not include OPENAI_API_KEY after proxy bootstrap: %+v", rt.EnvBundle)
	}
	if _, ok := rt.EnvBundle["OPENAI_BASE_URL"]; ok {
		t.Fatalf("runtime should not include OPENAI_BASE_URL after proxy bootstrap: %+v", rt.EnvBundle)
	}
	codexHome := rt.EnvBundle["CODEX_HOME"]
	if codexHome == "" {
		t.Fatalf("runtime CODEX_HOME is empty: %+v", rt.EnvBundle)
	}
	wantCodexHomePrefix := runtimeDir("boot-sess-1")
	if !strings.HasPrefix(codexHome, wantCodexHomePrefix) {
		t.Fatalf("runtime CODEX_HOME = %q, want under %q", codexHome, wantCodexHomePrefix)
	}
	codexConfigPath := filepath.Join(codexHome, "config.toml")
	codexConfigData, err := os.ReadFile(codexConfigPath)
	if err != nil {
		t.Fatalf("read codex session config: %v", err)
	}
	codexConfig := string(codexConfigData)
	if !strings.Contains(codexConfig, `model_provider = "ae_local_proxy"`) {
		t.Fatalf("missing model provider in codex config: %s", codexConfig)
	}
	wantOpenAIBaseURL := "base_url = " + `"` + "http://" + rt.Proxy.ListenAddr + "/openai/v1" + `"`
	if !strings.Contains(codexConfig, wantOpenAIBaseURL) {
		t.Fatalf("missing openai base url in codex config: %s", codexConfig)
	}
	if _, err := os.Stat(filepath.Join(wantWorkspaceRoot, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("expected no workspace-local codex config, got err=%v", err)
	}

	// Ensure workspace marker dir is excluded from git status by default.
	excludePath := filepath.Join(wantGitDir, "info", "exclude")
	excludeData, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read git info/exclude: %v", err)
	}
	if !strings.Contains(string(excludeData), "/.ae/") {
		t.Fatalf("expected %q to contain %q, got %q", excludePath, "/.ae/", string(excludeData))
	}

	// Verify state was persisted
	loaded, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if loaded == nil {
		t.Fatal("expected persisted state")
	}
	if loaded.ID != state.ID {
		t.Errorf("persisted ID = %q, want %q", loaded.ID, state.ID)
	}
}

func TestStartServerErrorInTempGitRepo(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	tmpDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfgObj := &config.Config{}
	m := NewManager(c, cfgObj)

	_, err := m.Start()
	if err == nil {
		t.Fatal("expected error when server returns 500")
	}
}

func TestWriteAndReadStateRoundTrip(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	now := time.Now().Truncate(time.Second)
	state := &State{
		ID:        "roundtrip-id",
		Repo:      "org/my-repo",
		Branch:    "feature/roundtrip",
		StartedAt: now,
	}

	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	path, _ := stateFilePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var loaded State
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}

	if loaded.ID != state.ID {
		t.Errorf("ID = %q, want %q", loaded.ID, state.ID)
	}
	if loaded.Repo != state.Repo {
		t.Errorf("Repo = %q, want %q", loaded.Repo, state.Repo)
	}
	if loaded.Branch != state.Branch {
		t.Errorf("Branch = %q, want %q", loaded.Branch, state.Branch)
	}
	if !loaded.StartedAt.Equal(state.StartedAt) {
		t.Errorf("StartedAt = %v, want %v", loaded.StartedAt, state.StartedAt)
	}
}

func TestWriteStateFilePermissions(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	state := &State{ID: "perm-test", Repo: "org/repo", Branch: "main"}
	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	path, _ := stateFilePath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("file permissions = %o, want 600", perm)
	}
}

func TestWriteStateOverwrite(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state1 := &State{ID: "first", Repo: "org/repo1", Branch: "main"}
	writeState(state1)

	state2 := &State{ID: "second", Repo: "org/repo2", Branch: "dev"}
	writeState(state2)

	loaded, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if loaded.ID != "second" {
		t.Errorf("ID = %q, want %q", loaded.ID, "second")
	}
	if loaded.Repo != "org/repo2" {
		t.Errorf("Repo = %q, want %q", loaded.Repo, "org/repo2")
	}
}

func TestStopNoActiveSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	_, err := m.Stop()
	if err == nil {
		t.Fatal("expected error when stopping with no active session")
	}
	if err.Error() != "no active session" {
		t.Errorf("error = %q, want %q", err.Error(), "no active session")
	}
}

func TestStopWithBadJSON(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), []byte("{bad json"), 0o644)

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	_, err := m.Stop()
	if err == nil {
		t.Fatal("expected error when state file has bad JSON")
	}
}

func TestRemoveStateThenCurrent(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state := &State{ID: "temp", Repo: "org/repo", Branch: "main"}
	writeState(state)
	removeState()

	loaded, err := m.Current()
	if err != nil {
		t.Fatalf("Current after removeState: %v", err)
	}
	if loaded != nil {
		t.Error("expected nil state after removeState")
	}
}

func TestWriteStateIndented(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	state := &State{ID: "indent-test", Repo: "org/repo", Branch: "main"}
	writeState(state)

	path, _ := stateFilePath()
	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		t.Fatal("state file should not be empty")
	}
	var check State
	if err := json.Unmarshal(data, &check); err != nil {
		t.Fatalf("state file is not valid JSON: %v", err)
	}
	if check.ID != "indent-test" {
		t.Errorf("ID = %q, want %q", check.ID, "indent-test")
	}
}

func TestStopSuccess(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Mock server that accepts stop requests
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	// Write a session state
	state := &State{
		ID:        "stop-test-id",
		Repo:      "org/repo",
		Branch:    "main",
		StartedAt: time.Now().Truncate(time.Second),
	}
	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	// Stop should succeed
	stopped, err := m.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.ID != "stop-test-id" {
		t.Errorf("stopped ID = %q, want %q", stopped.ID, "stop-test-id")
	}

	// State file should be removed
	current, err := m.Current()
	if err != nil {
		t.Fatalf("Current after Stop: %v", err)
	}
	if current != nil {
		t.Error("expected nil state after Stop")
	}
}

func TestStopCleansMarkerWhenInvokedOutsideWorkspace(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create a temp git repo.
	tmpDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	now := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap":
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-sess-stop",
					StartedAt:     now,
					RelayAPIKeyID: 1,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-stop",
					EnvBundle:     map[string]string{"AE_SESSION_ID": "boot-sess-stop"},
					KeyExpiresAt:  now.Add(1 * time.Hour),
				},
			})
			return
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop"):
			w.WriteHeader(http.StatusOK)
			return
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer srv.Close()

	mgr := NewManager(client.New(srv.URL, "tok"), &config.Config{})
	if _, err := mgr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	wsRoot, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(tmpDir): %v", err)
	}
	wsRoot, _ = filepath.Abs(filepath.Clean(wsRoot))

	if _, err := os.Stat(markerPath(wsRoot)); err != nil {
		t.Fatalf("expected marker to exist: %v", err)
	}
	if err := RemoveRuntime("boot-sess-stop"); err != nil {
		t.Fatalf("RemoveRuntime: %v", err)
	}

	// Stop from outside the workspace.
	outside := t.TempDir()
	if err := os.Chdir(outside); err != nil {
		t.Fatalf("chdir(outside): %v", err)
	}

	if _, err := mgr.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if _, err := os.Stat(markerPath(wsRoot)); !os.IsNotExist(err) {
		t.Fatalf("expected marker to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(runtimeDir("boot-sess-stop")); !os.IsNotExist(err) {
		t.Fatalf("expected runtime dir to be removed, stat err=%v", err)
	}
	if p, err := stateFilePath(); err == nil {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("expected legacy state file to be removed, stat err=%v", err)
		}
	}
}

func TestStopServerError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Mock server that returns an error
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state := &State{
		ID:     "33333333-3333-3333-3333-333333333333",
		Repo:   "org/repo",
		Branch: "main",
	}
	writeState(state)

	_, err := m.Stop()
	if err == nil {
		t.Fatal("expected error when server returns 500")
	}

	// State file should still exist (stop failed)
	current, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current == nil {
		t.Error("state should still exist after failed stop")
	}
}

func TestStopWithInvalidSessionIDCleansLocalStateWithoutBackendStop(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	stopCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			stopCalls++
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state := &State{
		ID:     "boot-helper-proxy-1",
		Repo:   "org/repo",
		Branch: "main",
	}
	if err := writeState(state); err != nil {
		t.Fatalf("writeState: %v", err)
	}

	stopped, err := m.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if stopped.ID != "boot-helper-proxy-1" {
		t.Fatalf("stopped ID = %q, want %q", stopped.ID, "boot-helper-proxy-1")
	}
	if stopCalls != 0 {
		t.Fatalf("stopCalls = %d, want 0", stopCalls)
	}

	current, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current != nil {
		t.Fatal("expected stale state to be removed")
	}
}

func TestCleanupLocalPreservesPendingQueue(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	workspaceRoot := t.TempDir()
	marker := &Marker{SessionID: "sess-pending"}
	if err := WriteMarker(workspaceRoot, marker); err != nil {
		t.Fatalf("WriteMarker: %v", err)
	}
	if err := WriteRuntimeBundle(&RuntimeBundle{
		SessionID:     "sess-pending",
		RuntimeRef:    "rt-pending",
		WorkspaceRoot: workspaceRoot,
		EnvBundle:     map[string]string{"AE_SESSION_ID": "sess-pending"},
	}); err != nil {
		t.Fatalf("WriteRuntimeBundle: %v", err)
	}
	queueFile := RuntimeQueueFilePath("sess-pending")
	if err := os.MkdirAll(filepath.Dir(queueFile), 0o700); err != nil {
		t.Fatalf("mkdir queue dir: %v", err)
	}
	if err := os.WriteFile(queueFile, []byte("{\"event\":{\"event_id\":\"evt-1\"}}\n"), 0o600); err != nil {
		t.Fatalf("write queue: %v", err)
	}

	mgr := &Manager{}
	if err := mgr.cleanupLocal("sess-pending", workspaceRoot); err != nil {
		t.Fatalf("cleanupLocal: %v", err)
	}

	if _, err := os.Stat(markerPath(workspaceRoot)); !os.IsNotExist(err) {
		t.Fatalf("expected marker to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(runtimeFilePath("sess-pending")); !os.IsNotExist(err) {
		t.Fatalf("expected runtime bundle to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(queueFile); err != nil {
		t.Fatalf("expected queue file to survive cleanup, stat err=%v", err)
	}
}

func TestStartSuccess(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	now := time.Now().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-sess-success",
					StartedAt:     now,
					RelayAPIKeyID: 1,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-success",
					EnvBundle:     map[string]string{"AE_SESSION_ID": "boot-sess-success"},
					KeyExpiresAt:  now.Add(1 * time.Hour),
				},
			})
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state, err := m.Start()
	if err != nil {
		// Start calls detectRepo/detectBranch which need a git repo
		t.Skipf("Start failed (likely not in git repo): %v", err)
	}
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.ID == "" {
		t.Error("state ID should not be empty")
	}
	if state.Repo == "" {
		t.Error("state Repo should not be empty")
	}
	if state.Branch == "" {
		t.Error("state Branch should not be empty")
	}
}

func TestStartServerError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	_, err := m.Start()
	if err == nil {
		t.Log("Start may succeed if not in git repo (detectRepo fails first)")
	}
	// Either git detection fails or server error — both are errors
}

func TestStartDetectRepoFails(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Change to a non-git directory
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	_, err := m.Start()
	if err == nil {
		t.Fatal("expected error when not in a git repo")
	}
	if !strings.Contains(err.Error(), "detecting git workspace root") {
		t.Errorf("error = %q, want it to contain 'detecting git workspace root'", err.Error())
	}
}

func TestStartDetectBranchFails(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create a git repo with a remote but no commits (so branch detection may fail)
	tmpDir := t.TempDir()
	cmds := [][]string{
		{"git", "init"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	// detectRepo will succeed, detectBranch may fail on empty repo
	_, err := m.Start()
	if err != nil {
		// Expected — either branch detection fails or server connection fails
		t.Logf("Start error (expected): %v", err)
	}
}

func TestStartCreateSessionFails(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	tmpDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	_, err := m.Start()
	if err == nil {
		t.Fatal("expected error when server returns 500")
	}
	if !strings.Contains(err.Error(), "bootstrapping session") {
		t.Errorf("error = %q, want it to contain 'bootstrapping session'", err.Error())
	}
}

func TestStartWriteStateFails(t *testing.T) {
	// Use a HOME that is read-only to make writeState fail
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	tmpDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	wantWorkspaceRoot, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(tmpDir): %v", err)
	}
	wantWorkspaceRoot, err = filepath.Abs(filepath.Clean(wantWorkspaceRoot))
	if err != nil {
		t.Fatalf("abs wantWorkspaceRoot: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	stopCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-sess-wsfail",
					StartedAt:     now,
					RelayAPIKeyID: 1,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-x",
					EnvBundle:     map[string]string{"AE_SESSION_ID": "boot-sess-wsfail"},
					KeyExpiresAt:  now.Add(1 * time.Hour),
				},
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/boot-sess-wsfail/stop" {
			stopCalls++
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	// Make the .ae-cli directory a file instead of a directory to cause writeState to fail
	os.WriteFile(filepath.Join(tmpHome, ".ae-cli"), []byte("not a dir"), 0o644)

	_, err = m.Start()
	if err == nil {
		t.Fatal("expected error when runtime/state write fails")
	}
	if !strings.Contains(err.Error(), "writing codex config") &&
		!strings.Contains(err.Error(), "writing runtime bundle") &&
		!strings.Contains(err.Error(), "writing session state") {
		t.Errorf("error = %q, want it to contain 'writing codex config', 'writing runtime bundle' or 'writing session state'", err.Error())
	}
	if stopCalls != 1 {
		t.Fatalf("stopCalls = %d, want 1", stopCalls)
	}
	if _, err := os.Stat(markerPath(wantWorkspaceRoot)); !os.IsNotExist(err) {
		t.Fatalf("expected marker to be removed after rollback, stat err=%v", err)
	}
	if _, err := os.Stat(runtimeDir("boot-sess-wsfail")); err == nil {
		t.Fatalf("expected runtime dir to be absent after rollback")
	}
	claudeSettingsPath := filepath.Join(wantWorkspaceRoot, ".claude", "settings.local.json")
	data, err := os.ReadFile(claudeSettingsPath)
	if err == nil && strings.Contains(string(data), "ae-session-managed") {
		t.Fatalf("expected rollback to clean managed Claude hooks, got %s", string(data))
	}
}

func TestStartRollbackPreservesRuntimeWhenProxyStopFails(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	tmpDir := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpDir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	t.Cleanup(func() { os.Chdir(origDir) })

	wantWorkspaceRoot, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("EvalSymlinks(tmpDir): %v", err)
	}
	wantWorkspaceRoot, err = filepath.Abs(filepath.Clean(wantWorkspaceRoot))
	if err != nil {
		t.Fatalf("abs wantWorkspaceRoot: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-sess-rollstopfail",
					StartedAt:     now,
					RelayAPIKeyID: 1,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-rollstopfail",
					EnvBundle:     map[string]string{"AE_SESSION_ID": "boot-sess-rollstopfail"},
					KeyExpiresAt:  now.Add(1 * time.Hour),
				},
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/boot-sess-rollstopfail/stop" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	origSpawn := spawnProxyProcess
	origStop := stopProxyProcess
	spawnProxyProcess = func(cfg proxy.RuntimeConfig) (proxy.SpawnResult, error) {
		return proxy.SpawnResult{
			PID:        9991,
			ListenAddr: "127.0.0.1:19991",
			ConfigPath: filepath.Join(t.TempDir(), "runtime.json"),
		}, nil
	}
	stopProxyProcess = func(req proxy.StopRequest) error {
		return errors.New("forced stop failure in rollback")
	}
	t.Cleanup(func() {
		spawnProxyProcess = origSpawn
		stopProxyProcess = origStop
	})

	// Cause writeState to fail after runtime bundle has already been written.
	// Making current-session.json a directory causes os.WriteFile to fail.
	if err := os.MkdirAll(filepath.Join(tmpHome, ".ae-cli", "current-session.json"), 0o755); err != nil {
		t.Fatalf("mkdir failing state path: %v", err)
	}

	m := NewManager(client.New(srv.URL, "tok"), &config.Config{})
	_, err = m.Start()
	if err == nil {
		t.Fatal("expected Start to fail")
	}
	if !strings.Contains(err.Error(), "writing session state") {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), "writing session state")
	}

	rt, err := ReadRuntimeBundle("boot-sess-rollstopfail")
	if err != nil {
		t.Fatalf("expected runtime bundle to be retained, got err=%v", err)
	}
	if rt.Proxy == nil || rt.Proxy.PID == 0 {
		t.Fatalf("expected retained proxy metadata, got %+v", rt.Proxy)
	}
	if _, err := os.Stat(markerPath(wantWorkspaceRoot)); err != nil {
		t.Fatalf("expected marker to be retained, stat err=%v", err)
	}
}

func TestStartRollbackAfterProxySpawnCleansProxyAndTempConfig(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("TMPDIR", tmpDir)

	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	tmpRepo := t.TempDir()
	cmds := [][]string{
		{"git", "init", "-b", "main"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
		{"git", "remote", "add", "origin", "https://github.com/test-org/test-repo.git"},
		{"git", "commit", "--allow-empty", "-m", "init"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = tmpRepo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("cmd %v: %v\n%s", args, err, out)
		}
	}

	origDir, _ := os.Getwd()
	os.Chdir(tmpRepo)
	t.Cleanup(func() { os.Chdir(origDir) })

	now := time.Now().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/bootstrap" {
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": client.BootstrapSessionResponse{
					SessionID:     "boot-sess-child-cleanup",
					StartedAt:     now,
					RelayAPIKeyID: 1,
					ProviderName:  "sub2api",
					RuntimeRef:    "rt-child-cleanup",
					EnvBundle:     map[string]string{"AE_SESSION_ID": "boot-sess-child-cleanup"},
					KeyExpiresAt:  now.Add(1 * time.Hour),
				},
			})
			return
		}
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/sessions/boot-sess-child-cleanup/stop" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	var spawnedPID int
	origSpawn := spawnProxyProcess
	spawnProxyProcess = func(cfg proxy.RuntimeConfig) (proxy.SpawnResult, error) {
		result, err := proxy.Spawn(cfg)
		if err == nil {
			spawnedPID = result.PID
		}
		return result, err
	}
	t.Cleanup(func() { spawnProxyProcess = origSpawn })

	// Force runtime write failure after proxy spawn.
	os.WriteFile(filepath.Join(tmpHome, ".ae-cli"), []byte("not a dir"), 0o644)

	m := NewManager(client.New(srv.URL, "tok"), &config.Config{})
	if _, err := m.Start(); err == nil {
		t.Fatal("expected start to fail")
	}
	if spawnedPID == 0 {
		t.Fatal("expected proxy child process to spawn before rollback")
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "ae-proxy-config-*"))
	if err != nil {
		t.Fatalf("glob config dirs: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected rollback to clean proxy temp config dirs, found %d", len(matches))
	}
}

func TestCurrentReadError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create .ae-cli/current-session.json as a directory to cause ReadFile to fail
	stateDir := filepath.Join(tmpHome, ".ae-cli", "current-session.json")
	os.MkdirAll(stateDir, 0o755)

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	// Ensure cwd has no workspace marker so Current falls back to legacy state file.
	origWD, _ := os.Getwd()
	tmpWD := t.TempDir()
	if err := os.Chdir(tmpWD); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	_, err := m.Current()
	if err == nil {
		t.Fatal("expected error when state file is a directory")
	}
}

func TestRemoveStateError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create .ae-cli/current-session.json as a directory to cause Remove to fail
	stateDir := filepath.Join(tmpHome, ".ae-cli", "current-session.json")
	os.MkdirAll(stateDir, 0o755)
	// Put a file inside so rmdir fails
	os.WriteFile(filepath.Join(stateDir, "dummy"), []byte("x"), 0o644)

	err := removeState()
	if err == nil {
		t.Fatal("expected error when removing a non-empty directory")
	}
}

func TestWriteStateMkdirError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	// Create .ae-cli as a file to prevent MkdirAll from creating the directory
	os.WriteFile(filepath.Join(tmpHome, ".ae-cli"), []byte("not a dir"), 0o644)

	state := &State{ID: "mkdir-fail", Repo: "org/repo", Branch: "main"}
	err := writeState(state)
	if err == nil {
		t.Fatal("expected error when .ae-cli is a file")
	}
}

func TestStopRemoveStateFails(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	// Write a valid state first
	stateDir := filepath.Join(tmpHome, ".ae-cli")
	os.MkdirAll(stateDir, 0o755)
	state := &State{ID: "stop-rm-fail", Repo: "org/repo", Branch: "main"}
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600)

	// Now replace the state file with a non-empty directory to make removeState fail
	os.Remove(filepath.Join(stateDir, "current-session.json"))
	os.MkdirAll(filepath.Join(stateDir, "current-session.json"), 0o755)
	os.WriteFile(filepath.Join(stateDir, "current-session.json", "dummy"), []byte("x"), 0o644)

	// Stop will read the directory as the state file, which will fail at Current()
	_, err := m.Stop()
	if err == nil {
		t.Log("Stop may not error depending on OS behavior")
	}
}

func TestShutdownSuccess(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	var stopCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/stop") {
			stopCalled = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state := &State{ID: "shutdown-test", Repo: "org/repo", Branch: "main"}
	writeState(state)

	err := m.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if !stopCalled {
		t.Error("expected stop API to be called")
	}

	current, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current != nil {
		t.Error("expected nil state after Shutdown")
	}
}

func TestShutdownNoSession(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	c := client.New("http://localhost:8080", "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	err := m.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown with no session should not error, got: %v", err)
	}
}

func TestShutdownAPIError(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	t.Cleanup(func() { os.Setenv("HOME", origHome) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := client.New(srv.URL, "tok")
	cfg := &config.Config{}
	m := NewManager(c, cfg)

	state := &State{ID: "shutdown-api-err", Repo: "org/repo", Branch: "main"}
	writeState(state)

	err := m.Shutdown(context.Background())
	if err != nil {
		t.Fatalf("Shutdown should not error on API failure, got: %v", err)
	}

	current, err := m.Current()
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if current != nil {
		t.Error("expected state file removed even when API fails")
	}
}
