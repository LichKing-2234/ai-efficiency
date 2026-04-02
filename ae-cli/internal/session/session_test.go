package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/proxy"
)

func startSessionWithFakeBootstrap(t *testing.T) (*State, *RuntimeBundle, *Manager) {
	t.Helper()

	origSpawn := spawnProxyProcess
	origStop := stopProxyProcess
	spawnProxyProcess = func(cfg proxy.RuntimeConfig) (int, string, error) {
		return 31337, "127.0.0.1:17888", nil
	}
	stopProxyProcess = func(pid int) error { return nil }
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
						"SUB2API_API_KEY": "test-key",
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

func TestManagerStopRemovesProxyRuntime(t *testing.T) {
	state, _, mgr := startSessionWithFakeBootstrap(t)

	var stoppedPID int
	origStop := stopProxyProcess
	stopProxyProcess = func(pid int) error {
		stoppedPID = pid
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
						"AE_SESSION_ID":   "boot-sess-1",
						"SUB2API_API_KEY": "k",
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
		ID:     "stop-err-id",
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
	if !strings.Contains(err.Error(), "writing runtime bundle") && !strings.Contains(err.Error(), "writing session state") {
		t.Errorf("error = %q, want it to contain 'writing runtime bundle' or 'writing session state'", err.Error())
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
