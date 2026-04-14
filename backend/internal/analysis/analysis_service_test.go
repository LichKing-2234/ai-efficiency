package analysis

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

	"github.com/ai-efficiency/backend/ent"
	entcredential "github.com/ai-efficiency/backend/ent/credential"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/analysis/rules"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/pkg"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/testdb"
	"go.uber.org/zap"
)

const testEncryptionKey = "0000000000000000000000000000000000000000000000000000000000000000"

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func setupEntClient(t *testing.T) *ent.Client {
	t.Helper()
	return testdb.Open(t)
}

func createTestSCMProvider(t *testing.T, client *ent.Client) *ent.ScmProvider {
	t.Helper()
	encrypted, err := pkg.Encrypt(`{"text":"ghp_test"}`, testEncryptionKey)
	if err != nil {
		t.Fatalf("encrypt credential payload: %v", err)
	}
	cred, err := client.Credential.Create().
		SetName("test-github-pat").
		SetDescription("test api credential").
		SetKind(entcredential.KindSecretText).
		SetPayload(encrypted).
		Save(context.Background())
	if err != nil {
		t.Fatalf("create credential: %v", err)
	}
	p, err := client.ScmProvider.Create().
		SetName("test-github").
		SetType("github").
		SetBaseURL("https://api.github.com").
		SetAPICredentialID(cred.ID).
		SetCloneProtocol("https").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create scm provider: %v", err)
	}
	return p
}

func createTestRepoConfig(t *testing.T, client *ent.Client, providerID int) *ent.RepoConfig {
	t.Helper()
	rc, err := client.RepoConfig.Create().
		SetScmProviderID(providerID).
		SetName("test-repo").
		SetFullName("org/test-repo").
		SetCloneURL("https://github.com/org/test-repo.git").
		SetDefaultBranch("main").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo config: %v", err)
	}
	return rc
}

// ---------------------------------------------------------------------------
// Cloner tests
// ---------------------------------------------------------------------------

func TestNewCloner(t *testing.T) {
	c := NewCloner("/tmp/data", zap.NewNop())
	if c == nil {
		t.Fatal("NewCloner returned nil")
	}
}

func TestClonerRepoPath(t *testing.T) {
	c := NewCloner("/tmp/data", zap.NewNop())
	got := c.RepoPath(42)
	want := filepath.Join("/tmp/data", "repos", "42")
	if got != want {
		t.Errorf("RepoPath(42) = %q, want %q", got, want)
	}
}

func TestClonerRepoPathDifferentIDs(t *testing.T) {
	c := NewCloner("/tmp/data", zap.NewNop())
	p1 := c.RepoPath(1)
	p2 := c.RepoPath(2)
	if p1 == p2 {
		t.Errorf("RepoPath(1) == RepoPath(2) == %q, want different paths", p1)
	}
	if !strings.HasSuffix(p1, "/1") {
		t.Errorf("RepoPath(1) = %q, want suffix /1", p1)
	}
	if !strings.HasSuffix(p2, "/2") {
		t.Errorf("RepoPath(2) = %q, want suffix /2", p2)
	}
}

// ---------------------------------------------------------------------------
// Service tests
// ---------------------------------------------------------------------------

func TestNewService(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)
	if svc == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestNewServicePanicsWithoutEncryptionKey(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected NewService to panic without encryption key")
		}
	}()

	_ = NewService(client, c, nil, zap.NewNop(), "")
}

func TestGetLatestScanNoResults(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)

	p := createTestSCMProvider(t, client)
	rc := createTestRepoConfig(t, client, p.ID)

	_, err := svc.GetLatestScan(context.Background(), rc.ID)
	if err == nil {
		t.Fatal("GetLatestScan should return error when no scans exist")
	}
}

func TestGetLatestScanWithResults(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)
	ctx := context.Background()

	p := createTestSCMProvider(t, client)
	rc := createTestRepoConfig(t, client, p.ID)

	// Create older scan
	_, err := client.AiScanResult.Create().
		SetRepoConfigID(rc.ID).
		SetScore(50).
		SetCreatedAt(time.Now().Add(-time.Hour)).
		Save(ctx)
	if err != nil {
		t.Fatalf("create scan 1: %v", err)
	}

	// Create newer scan
	newer, err := client.AiScanResult.Create().
		SetRepoConfigID(rc.ID).
		SetScore(80).
		SetCreatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		t.Fatalf("create scan 2: %v", err)
	}

	got, err := svc.GetLatestScan(ctx, rc.ID)
	if err != nil {
		t.Fatalf("GetLatestScan error: %v", err)
	}
	if got.ID != newer.ID {
		t.Errorf("GetLatestScan returned ID %d, want %d (latest)", got.ID, newer.ID)
	}
	if got.Score != 80 {
		t.Errorf("GetLatestScan score = %d, want 80", got.Score)
	}
}

func TestListScansDefaultLimit(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)
	ctx := context.Background()

	p := createTestSCMProvider(t, client)
	rc := createTestRepoConfig(t, client, p.ID)

	// Create 25 scans
	for i := 0; i < 25; i++ {
		_, err := client.AiScanResult.Create().
			SetRepoConfigID(rc.ID).
			SetScore(i).
			Save(ctx)
		if err != nil {
			t.Fatalf("create scan %d: %v", i, err)
		}
	}

	// limit <= 0 should default to 20
	scans, err := svc.ListScans(ctx, rc.ID, 0)
	if err != nil {
		t.Fatalf("ListScans error: %v", err)
	}
	if len(scans) != 20 {
		t.Errorf("ListScans(limit=0) returned %d results, want 20", len(scans))
	}
}

func TestListScansCustomLimit(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)
	ctx := context.Background()

	p := createTestSCMProvider(t, client)
	rc := createTestRepoConfig(t, client, p.ID)

	for i := 0; i < 10; i++ {
		_, err := client.AiScanResult.Create().
			SetRepoConfigID(rc.ID).
			SetScore(i).
			Save(ctx)
		if err != nil {
			t.Fatalf("create scan %d: %v", i, err)
		}
	}

	scans, err := svc.ListScans(ctx, rc.ID, 5)
	if err != nil {
		t.Fatalf("ListScans error: %v", err)
	}
	if len(scans) != 5 {
		t.Errorf("ListScans(limit=5) returned %d results, want 5", len(scans))
	}
}

func TestListScansOrdering(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)
	ctx := context.Background()

	p := createTestSCMProvider(t, client)
	rc := createTestRepoConfig(t, client, p.ID)

	// Create scans with explicit timestamps to guarantee ordering
	base := time.Now()
	for i := 0; i < 5; i++ {
		_, err := client.AiScanResult.Create().
			SetRepoConfigID(rc.ID).
			SetScore(i * 10).
			SetCreatedAt(base.Add(time.Duration(i) * time.Minute)).
			Save(ctx)
		if err != nil {
			t.Fatalf("create scan %d: %v", i, err)
		}
	}

	scans, err := svc.ListScans(ctx, rc.ID, 5)
	if err != nil {
		t.Fatalf("ListScans error: %v", err)
	}

	// Should be descending by created_at, so newest first (score=40, 30, 20, 10, 0)
	for i := 1; i < len(scans); i++ {
		if scans[i].CreatedAt.After(scans[i-1].CreatedAt) {
			t.Errorf("scan[%d].CreatedAt (%v) is after scan[%d].CreatedAt (%v), want descending order",
				i, scans[i].CreatedAt, i-1, scans[i-1].CreatedAt)
		}
	}
}

// ---------------------------------------------------------------------------
// helpers for RunScan tests
// ---------------------------------------------------------------------------

// initTestGitRepo creates a git repo at repoDir with an "origin" remote pointing
// to itself so that `git fetch origin` succeeds.
func initTestGitRepo(t *testing.T, repoDir string) {
	t.Helper()
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
	)
	for _, args := range [][]string{
		{"init", repoDir},
		{"-C", repoDir, "commit", "--allow-empty", "-m", "init"},
		{"-C", repoDir, "remote", "add", "origin", repoDir},
	} {
		cmd := exec.Command("git", args...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s: %v", args, string(out), err)
		}
	}
}

// ---------------------------------------------------------------------------
// RunScan tests
// ---------------------------------------------------------------------------

func TestRunScanRepoConfigNotFound(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)

	_, err := svc.RunScan(context.Background(), 99999)
	if err == nil {
		t.Fatal("expected error for non-existent repo config")
	}
	if !strings.Contains(err.Error(), "get repo config") {
		t.Errorf("error = %q, want 'get repo config'", err.Error())
	}
}

func TestRunScanCloneError(t *testing.T) {
	client := setupEntClient(t)
	c := NewCloner(t.TempDir(), zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)

	p := createTestSCMProvider(t, client)
	rc, err := client.RepoConfig.Create().
		SetScmProviderID(p.ID).
		SetName("bad-repo").
		SetFullName("org/bad-repo").
		SetCloneURL("ftp://invalid-scheme.com/repo.git").
		SetDefaultBranch("main").
		Save(context.Background())
	if err != nil {
		t.Fatalf("create repo config: %v", err)
	}

	_, err = svc.RunScan(context.Background(), rc.ID)
	if err == nil {
		t.Fatal("expected error for invalid clone URL")
	}
	if !strings.Contains(err.Error(), "clone repo") {
		t.Errorf("error = %q, want 'clone repo'", err.Error())
	}
}

func TestRunScanStaticOnly(t *testing.T) {
	client := setupEntClient(t)
	dataDir := t.TempDir()
	c := NewCloner(dataDir, zap.NewNop())
	svc := NewService(client, c, nil, zap.NewNop(), testEncryptionKey)
	ctx := context.Background()

	p := createTestSCMProvider(t, client)
	rc := createTestRepoConfig(t, client, p.ID)

	repoDir := c.RepoPath(rc.ID)
	initTestGitRepo(t, repoDir)

	os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# Test\nSome content here\n"), 0o644)
	os.WriteFile(filepath.Join(repoDir, "AGENTS.md"), []byte("# Agents\nProject info\n"), 0o644)

	// Set clone URL to https:// so it passes validation; the existing .git + origin=self handles fetch
	client.RepoConfig.UpdateOneID(rc.ID).SetCloneURL("https://github.com/org/test-repo.git").ExecX(ctx)

	result, err := svc.RunScan(ctx, rc.ID)
	if err != nil {
		t.Fatalf("RunScan error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil scan result")
	}
	if result.Score < 0 {
		t.Errorf("score = %d, should be >= 0", result.Score)
	}
	if result.ScanType != "static" {
		t.Errorf("scan_type = %q, want 'static' (no LLM configured)", result.ScanType)
	}

	latest, err := svc.GetLatestScan(ctx, rc.ID)
	if err != nil {
		t.Fatalf("GetLatestScan error: %v", err)
	}
	if latest.ID != result.ID {
		t.Errorf("latest scan ID = %d, want %d", latest.ID, result.ID)
	}

	updatedRC, err := client.RepoConfig.Get(ctx, rc.ID)
	if err != nil {
		t.Fatalf("get repo config: %v", err)
	}
	if updatedRC.AiScore != result.Score {
		t.Errorf("repo ai_score = %d, want %d", updatedRC.AiScore, result.Score)
	}
}

func TestRunScanWithLLMEnabled(t *testing.T) {
	client := setupEntClient(t)
	dataDir := t.TempDir()
	c := NewCloner(dataDir, zap.NewNop())
	ctx := context.Background()

	mockResp := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message": map[string]string{
					"content": `{"dimensions":[{"name":"code_readability","score":8,"details":"good"}],"suggestions":[]}`,
				},
			},
		},
		"usage": map[string]int{"total_tokens": 100},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(mockResp)
	}))
	defer server.Close()

	rp := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "gpt-4", zap.NewNop())
	llmAnalyzer := llm.NewAnalyzer(config.LLMConfig{}, rp, zap.NewNop())

	svc := NewService(client, c, llmAnalyzer, zap.NewNop(), testEncryptionKey)

	p := createTestSCMProvider(t, client)
	rc := createTestRepoConfig(t, client, p.ID)

	repoDir := c.RepoPath(rc.ID)
	initTestGitRepo(t, repoDir)

	client.RepoConfig.UpdateOneID(rc.ID).SetCloneURL("https://github.com/org/test-repo.git").ExecX(ctx)

	result, err := svc.RunScan(ctx, rc.ID)
	if err != nil {
		t.Fatalf("RunScan error: %v", err)
	}
	if result.ScanType != "full" {
		t.Errorf("scan_type = %q, want 'full' (LLM enabled)", result.ScanType)
	}
	if result.Score <= 0 {
		t.Errorf("score = %d, should be > 0 with LLM", result.Score)
	}
}

func TestRunScanWithLLMFailure(t *testing.T) {
	client := setupEntClient(t)
	dataDir := t.TempDir()
	c := NewCloner(dataDir, zap.NewNop())
	ctx := context.Background()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("LLM error"))
	}))
	defer server.Close()

	rp := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "gpt-4", zap.NewNop())
	llmAnalyzer := llm.NewAnalyzer(config.LLMConfig{}, rp, zap.NewNop())

	svc := NewService(client, c, llmAnalyzer, zap.NewNop(), testEncryptionKey)

	p := createTestSCMProvider(t, client)
	rc := createTestRepoConfig(t, client, p.ID)

	repoDir := c.RepoPath(rc.ID)
	initTestGitRepo(t, repoDir)

	client.RepoConfig.UpdateOneID(rc.ID).SetCloneURL("https://github.com/org/test-repo.git").ExecX(ctx)

	result, err := svc.RunScan(ctx, rc.ID)
	if err != nil {
		t.Fatalf("RunScan error: %v (should fallback to static)", err)
	}
	if result.ScanType != "static" {
		t.Errorf("scan_type = %q, want 'static' (LLM failed, fallback)", result.ScanType)
	}
}

func TestRunScanWithPromptOverride(t *testing.T) {
	client := setupEntClient(t)
	dataDir := t.TempDir()
	c := NewCloner(dataDir, zap.NewNop())
	ctx := context.Background()

	var capturedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": `{"dimensions":[],"suggestions":[]}`}},
			},
			"usage": map[string]int{"total_tokens": 10},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	rp := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "gpt-4", zap.NewNop())
	llmAnalyzer := llm.NewAnalyzer(config.LLMConfig{}, rp, zap.NewNop())

	svc := NewService(client, c, llmAnalyzer, zap.NewNop(), testEncryptionKey)

	p := createTestSCMProvider(t, client)
	rc, err := client.RepoConfig.Create().
		SetScmProviderID(p.ID).
		SetName("override-repo").
		SetFullName("org/override-repo").
		SetCloneURL("https://github.com/org/override-repo.git").
		SetDefaultBranch("main").
		SetScanPromptOverride(map[string]string{
			"system_prompt": "Custom system prompt for this repo",
		}).
		Save(ctx)
	if err != nil {
		t.Fatalf("create repo config: %v", err)
	}

	repoDir := c.RepoPath(rc.ID)
	initTestGitRepo(t, repoDir)

	client.RepoConfig.UpdateOneID(rc.ID).SetCloneURL("https://github.com/org/override-repo.git").ExecX(ctx)

	result, err := svc.RunScan(ctx, rc.ID)
	if err != nil {
		t.Fatalf("RunScan error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}

// ---------------------------------------------------------------------------
// Optimizer helper tests
// ---------------------------------------------------------------------------

func TestGenerateEditorConfig(t *testing.T) {
	content := generateEditorConfig()

	sections := []string{"[*]", "[*.go]", "[*.md]", "[Makefile]"}
	for _, s := range sections {
		if !strings.Contains(content, s) {
			t.Errorf("generateEditorConfig() missing section %q", s)
		}
	}

	if !strings.Contains(content, "root = true") {
		t.Error("generateEditorConfig() missing 'root = true'")
	}
	if !strings.Contains(content, "indent_style = tab") {
		t.Error("generateEditorConfig() missing 'indent_style = tab' for Go/Makefile")
	}
}

func TestGenerateAgentsMDTemplate(t *testing.T) {
	dims := []rules.DimensionScore{
		{Name: "ai_files", Score: 10, MaxScore: 20, Details: "some details"},
	}
	content := generateAgentsMDTemplate("org/my-repo", dims)

	checks := map[string]string{
		"repo name":        "org/my-repo",
		"Project Overview": "## Project Overview",
		"Code Style":       "## Code Style",
		"Testing":          "## Testing",
		"AI Scan Summary":  "## AI Scan Summary",
		"dimension":        "ai_files",
	}
	for label, want := range checks {
		if !strings.Contains(content, want) {
			t.Errorf("generateAgentsMDTemplate() missing %s (%q)", label, want)
		}
	}
}

func TestBuildPRDescription(t *testing.T) {
	files := map[string]string{
		"AGENTS.md":     "# Agents",
		".editorconfig": "[*]",
	}
	dims := []rules.DimensionScore{
		{Name: "ai_files", Score: 15, MaxScore: 20, Details: "good"},
	}
	content := buildPRDescription(files, dims, 42)

	if !strings.Contains(content, "42/100") {
		t.Error("buildPRDescription() missing score '42/100'")
	}
	if !strings.Contains(content, "AGENTS.md") {
		t.Error("buildPRDescription() missing AGENTS.md description")
	}
	if !strings.Contains(content, "AI coding assistants") {
		t.Error("buildPRDescription() missing AGENTS.md explanation")
	}
	if !strings.Contains(content, ".editorconfig") {
		t.Error("buildPRDescription() missing .editorconfig description")
	}
	if !strings.Contains(content, "consistent coding style") {
		t.Error("buildPRDescription() missing .editorconfig explanation")
	}
	if !strings.Contains(content, "ai_files") {
		t.Error("buildPRDescription() missing dimension name in scores table")
	}
}

func TestExtractSuggestions(t *testing.T) {
	scan := &ent.AiScanResult{
		Suggestions: []map[string]interface{}{
			{
				"category": "ai_files",
				"message":  "Add AGENTS.md",
				"priority": "high",
				"auto_fix": true,
			},
			{
				"category": "docs",
				"message":  "Add README",
				"priority": "medium",
				"auto_fix": false,
			},
		},
	}

	suggestions := extractSuggestions(scan)
	if len(suggestions) != 2 {
		t.Fatalf("extractSuggestions() returned %d, want 2", len(suggestions))
	}

	s := suggestions[0]
	if s.Category != "ai_files" {
		t.Errorf("suggestion[0].Category = %q, want %q", s.Category, "ai_files")
	}
	if s.Message != "Add AGENTS.md" {
		t.Errorf("suggestion[0].Message = %q, want %q", s.Message, "Add AGENTS.md")
	}
	if s.Priority != "high" {
		t.Errorf("suggestion[0].Priority = %q, want %q", s.Priority, "high")
	}
	if !s.AutoFix {
		t.Error("suggestion[0].AutoFix = false, want true")
	}

	s2 := suggestions[1]
	if s2.AutoFix {
		t.Error("suggestion[1].AutoFix = true, want false")
	}
}

func TestExtractSuggestionsNil(t *testing.T) {
	scan := &ent.AiScanResult{Suggestions: nil}
	suggestions := extractSuggestions(scan)
	if suggestions != nil {
		t.Errorf("extractSuggestions(nil) = %v, want nil", suggestions)
	}
}

func TestExtractDimensions(t *testing.T) {
	scan := &ent.AiScanResult{
		Dimensions: map[string]interface{}{
			"ai_files": map[string]interface{}{
				"score":     float64(15),
				"max_score": float64(20),
				"details":   "has AGENTS.md",
			},
		},
	}

	dims := extractDimensions(scan)
	if len(dims) != 1 {
		t.Fatalf("extractDimensions() returned %d, want 1", len(dims))
	}

	d := dims[0]
	if d.Name != "ai_files" {
		t.Errorf("dim.Name = %q, want %q", d.Name, "ai_files")
	}
	if d.Score != 15 {
		t.Errorf("dim.Score = %v, want 15", d.Score)
	}
	if d.MaxScore != 20 {
		t.Errorf("dim.MaxScore = %v, want 20", d.MaxScore)
	}
	if d.Details != "has AGENTS.md" {
		t.Errorf("dim.Details = %q, want %q", d.Details, "has AGENTS.md")
	}
}

func TestExtractDimensionsNil(t *testing.T) {
	scan := &ent.AiScanResult{Dimensions: nil}
	dims := extractDimensions(scan)
	if dims != nil {
		t.Errorf("extractDimensions(nil) = %v, want nil", dims)
	}
}

func TestStringVal(t *testing.T) {
	m := map[string]interface{}{
		"name":   "hello",
		"number": 42,
	}

	// Existing string key
	if got := stringVal(m, "name"); got != "hello" {
		t.Errorf("stringVal(name) = %q, want %q", got, "hello")
	}

	// Missing key
	if got := stringVal(m, "missing"); got != "" {
		t.Errorf("stringVal(missing) = %q, want empty", got)
	}

	// Non-string value
	if got := stringVal(m, "number"); got != "" {
		t.Errorf("stringVal(number) = %q, want empty (non-string)", got)
	}
}

func TestBoolVal(t *testing.T) {
	m := map[string]interface{}{
		"enabled":  true,
		"disabled": false,
		"text":     "yes",
	}

	if got := boolVal(m, "enabled"); !got {
		t.Error("boolVal(enabled) = false, want true")
	}

	if got := boolVal(m, "disabled"); got {
		t.Error("boolVal(disabled) = true, want false")
	}

	// Missing key
	if got := boolVal(m, "missing"); got {
		t.Error("boolVal(missing) = true, want false")
	}

	// Non-bool value
	if got := boolVal(m, "text"); got {
		t.Error("boolVal(text) = true, want false (non-bool)")
	}
}

func TestFloatVal(t *testing.T) {
	m := map[string]interface{}{
		"score": float64(95.5),
		"count": int(10),
		"label": "not-a-number",
	}

	// float64 value
	if got := floatVal(m, "score"); got != 95.5 {
		t.Errorf("floatVal(score) = %v, want 95.5", got)
	}

	// int value (should convert)
	if got := floatVal(m, "count"); got != 10.0 {
		t.Errorf("floatVal(count) = %v, want 10.0", got)
	}

	// Missing key
	if got := floatVal(m, "missing"); got != 0 {
		t.Errorf("floatVal(missing) = %v, want 0", got)
	}

	// Non-numeric value
	if got := floatVal(m, "label"); got != 0 {
		t.Errorf("floatVal(label) = %v, want 0 (non-numeric)", got)
	}
}
