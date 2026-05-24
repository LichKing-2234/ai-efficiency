package cmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/hookstate"
)

func TestRootCommandHasSessionlessPrimaryCommands(t *testing.T) {
	expected := map[string]bool{
		"discover": false,
		"init":     false,
		"sync":     false,
		"doctor":   false,
		"hooks":    false,
	}

	for _, cmd := range rootCmd.Commands() {
		if _, ok := expected[cmd.Name()]; ok {
			expected[cmd.Name()] = true
		}
	}

	for name, found := range expected {
		if !found {
			t.Fatalf("expected subcommand %q not found", name)
		}
	}
}

func TestRootCommandDoesNotExposeLegacyWorkflowCommands(t *testing.T) {
	legacy := map[string]struct{}{
		"start":  {},
		"stop":   {},
		"run":    {},
		"attach": {},
		"ps":     {},
		"kill":   {},
		"shell":  {},
		"flush":  {},
	}

	for _, cmd := range rootCmd.Commands() {
		if _, ok := legacy[cmd.Name()]; ok {
			t.Fatalf("unexpected legacy command %q still registered", cmd.Name())
		}
	}
}

func TestInitCommandCreatesAttributionStateDir(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/ensure-remote" {
			t.Fatalf("path = %s, want /api/v1/repos/ensure-remote", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":1,"repo_key":"github.com/acme/repo","full_name":"github.com/acme/repo","clone_url":"https://github.com/acme/repo.git","default_branch":"main","binding_state":"unbound"}}`))
	}))
	defer srv.Close()

	origInstallSharedHooks := installSharedHooks
	called := false
	installSharedHooks = func(cwd, selfPath string) error {
		called = true
		return nil
	}
	origCfg := cfg
	origClient := apiClient
	cfg = &config.Config{Server: config.ServerConfig{Token: "tok"}}
	apiClient = client.New(srv.URL, "tok")
	t.Cleanup(func() {
		installSharedHooks = origInstallSharedHooks
		cfg = origCfg
		apiClient = origClient
	})

	buf := new(bytes.Buffer)
	initCmd.SetOut(buf)
	initCmd.SetErr(buf)

	if err := initCmd.RunE(initCmd, nil); err != nil {
		t.Fatalf("initCmd.RunE: %v", err)
	}

	if _, err := os.Stat(attributionlocal.AttributionRootDir()); err != nil {
		t.Fatalf("expected attribution root dir to exist, stat err=%v", err)
	}
	if !called {
		t.Fatal("expected installSharedHooks to be called")
	}
	if !strings.Contains(buf.String(), "Initialized sessionless attribution.") {
		t.Fatalf("output = %q, want init success summary", buf.String())
	}
	if !strings.Contains(buf.String(), "Repo Link:     linked") {
		t.Fatalf("output = %q, want repo link status", buf.String())
	}
}

func TestDoctorCommandPrintsWorkspaceIdentity(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/ensure-remote" {
			t.Fatalf("path = %s, want /api/v1/repos/ensure-remote", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"id":1,"repo_key":"github.com/acme/repo","full_name":"github.com/acme/repo","clone_url":"https://github.com/acme/repo.git","default_branch":"main","binding_state":"unbound"}}`))
	}))
	defer srv.Close()

	origCfg := cfg
	origClient := apiClient
	cfg = &config.Config{Server: config.ServerConfig{Token: "tok"}}
	apiClient = client.New(srv.URL, "tok")
	t.Cleanup(func() {
		cfg = origCfg
		apiClient = origClient
	})

	buf := new(bytes.Buffer)
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)

	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"Sessionless attribution doctor", "Workspace ID:", "State Dir:", "Hook status"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output = %q, want substring %q", output, want)
		}
	}
}

func TestSyncCommandRequiresLogin(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	oldCfg := cfg
	cfg = nil
	t.Cleanup(func() { cfg = oldCfg })

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	err = syncCmd.RunE(syncCmd, nil)
	if err == nil {
		t.Fatal("expected login requirement error")
	}
	if !strings.Contains(err.Error(), "ae-cli login") {
		t.Fatalf("err = %q, want login guidance", err.Error())
	}
}

func TestSyncCommandFlushesPendingHookQueueBeforeAttributionSync(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	workspaceID := gitCtx.WorkspaceID

	q, err := hooks.NewWorkspaceQueue(workspaceID)
	if err != nil {
		t.Fatalf("NewWorkspaceQueue: %v", err)
	}
	eventID, err := hooks.CheckpointEventID("repo_config_id:123", "queued-sha")
	if err != nil {
		t.Fatalf("CheckpointEventID: %v", err)
	}
	if err := q.Enqueue(hooks.HookEvent{
		Kind:           "post-commit",
		EventID:        eventID,
		WorkspaceID:    workspaceID,
		ServerURL:      "https://ae.example.com",
		AuthSubject:    "user:123",
		RepoConfigID:   123,
		RepoKey:        "github.com/acme/repo",
		RepoFullName:   "acme/repo",
		CommitSHA:      "queued-sha",
		BindingSource:  "unbound",
		BranchSnapshot: "main",
	}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if err := auth.WriteToken(filepath.Join(home, ".ae-cli", "token.json"), &auth.TokenFile{
		AccessToken:  "test-token",
		RefreshToken: "refresh-token",
		ExpiresAt:    time.Now().Add(time.Hour),
		ServerURL:    "https://ae.example.com",
		AuthSubject:  "user:123",
	}); err != nil {
		t.Fatalf("WriteToken: %v", err)
	}
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		t.Fatalf("LoadEligibilityCache: %v", err)
	}
	cache.PutPositive(hookstate.Context{ServerURL: "https://ae.example.com", AuthSubject: "user:123", RepoKey: "github.com/acme/repo"}, client.RepoEligibilityResponse{
		Eligible:     true,
		RepoConfigID: 123,
		RepoKey:      "github.com/acme/repo",
		FullName:     "acme/repo",
		CloneURL:     "https://github.com/acme/repo.git",
		Status:       "active",
	}, time.Now())
	if err := cache.Save(); err != nil {
		t.Fatalf("Save cache: %v", err)
	}

	var checkpointCalls, syncCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/checkpoints/commit":
			checkpointCalls++
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"code":0,"data":{"event_id":"ok"}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	oldCfg := cfg
	oldClient := apiClient
	oldRun := runSyncEngine
	cfg = &config.Config{Server: config.ServerConfig{URL: "https://ae.example.com", Token: "tok"}}
	apiClient = client.New(srv.URL, "tok")
	runSyncEngine = func(engine *attributionlocal.SyncEngine, ctx context.Context, opts attributionlocal.RunOptions) error {
		syncCalls++
		if opts.RepoConfigID != 123 || opts.RepoKey != "github.com/acme/repo" {
			t.Fatalf("sync opts = %+v, want repo_config_id 123", opts)
		}
		return nil
	}
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
		runSyncEngine = oldRun
	})

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	if err := syncCmd.RunE(syncCmd, nil); err != nil {
		t.Fatalf("syncCmd.RunE: %v", err)
	}

	if checkpointCalls != 1 {
		t.Fatalf("checkpoint calls = %d, want 1", checkpointCalls)
	}
	if syncCalls != 1 {
		t.Fatalf("sync engine calls = %d, want 1", syncCalls)
	}

	items, err := q.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("queued items after sync = %d, want 0", len(items))
	}
}
