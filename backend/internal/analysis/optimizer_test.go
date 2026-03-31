package analysis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ai-efficiency/backend/ent"
	"github.com/ai-efficiency/backend/internal/analysis/llm"
	"github.com/ai-efficiency/backend/internal/analysis/rules"
	"github.com/ai-efficiency/backend/internal/config"
	"github.com/ai-efficiency/backend/internal/relay"
	"github.com/ai-efficiency/backend/internal/scm"
	"go.uber.org/zap"
)

// mockSCMProvider implements scm.SCMProvider for testing.
type mockSCMProvider struct {
	fileContents   map[string][]byte
	getFileErr     error
	branchSHA      string
	branchSHAErr   error
	createBranchErr error
	commitFilesErr  error
	commitSHA       string
	createPRResult  *scm.PR
	createPRErr     error
}

func (m *mockSCMProvider) GetRepo(ctx context.Context, fullName string) (*scm.Repo, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListRepos(ctx context.Context, opts scm.ListOpts) ([]*scm.Repo, error) {
	return nil, nil
}
func (m *mockSCMProvider) CreatePR(ctx context.Context, req scm.CreatePRRequest) (*scm.PR, error) {
	return m.createPRResult, m.createPRErr
}
func (m *mockSCMProvider) GetPR(ctx context.Context, repoFullName string, prID int) (*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListPRs(ctx context.Context, repoFullName string, opts scm.PRListOpts) ([]*scm.PR, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPRChangedFiles(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	return nil, nil
}
func (m *mockSCMProvider) ListPRCommits(ctx context.Context, repoFullName string, prID int) ([]string, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetPRApprovals(ctx context.Context, repoFullName string, prID int) (int, error) {
	return 0, nil
}
func (m *mockSCMProvider) AddLabels(ctx context.Context, repoFullName string, prID int, labels []string) error {
	return nil
}
func (m *mockSCMProvider) SetPRStatus(ctx context.Context, req scm.SetStatusRequest) error {
	return nil
}
func (m *mockSCMProvider) MergePR(ctx context.Context, repoFullName string, prID int, opts scm.MergeOpts) error {
	return nil
}
func (m *mockSCMProvider) RegisterWebhook(ctx context.Context, repoFullName string, events []string, secret string) (string, error) {
	return "", nil
}
func (m *mockSCMProvider) DeleteWebhook(ctx context.Context, repoFullName string, webhookID string) error {
	return nil
}
func (m *mockSCMProvider) ParseWebhookPayload(r *http.Request, secret string) (*scm.WebhookEvent, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetFileContent(ctx context.Context, repoFullName, path, ref string) ([]byte, error) {
	if m.getFileErr != nil {
		return nil, m.getFileErr
	}
	if content, ok := m.fileContents[path]; ok {
		return content, nil
	}
	return nil, fmt.Errorf("file not found: %s", path)
}
func (m *mockSCMProvider) GetTree(ctx context.Context, repoFullName, ref string) ([]*scm.TreeEntry, error) {
	return nil, nil
}
func (m *mockSCMProvider) GetBranchSHA(ctx context.Context, repoFullName, branch string) (string, error) {
	return m.branchSHA, m.branchSHAErr
}
func (m *mockSCMProvider) CreateBranch(ctx context.Context, repoFullName, branchName, baseSHA string) error {
	return m.createBranchErr
}
func (m *mockSCMProvider) CommitFiles(ctx context.Context, req scm.CommitFilesRequest) (string, error) {
	return m.commitSHA, m.commitFilesErr
}

func newTestOptimizer() *Optimizer {
	return NewOptimizer(nil, zap.NewNop())
}

func TestNewOptimizer(t *testing.T) {
	o := newTestOptimizer()
	if o == nil {
		t.Fatal("NewOptimizer returned nil")
	}
}

func TestPreviewOptimizationNoSuggestions(t *testing.T) {
	o := newTestOptimizer()
	scan := &ent.AiScanResult{
		Score:       80,
		Suggestions: nil,
	}
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{}

	preview, err := o.PreviewOptimization(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview != nil {
		t.Error("expected nil preview when no suggestions")
	}
}

func TestPreviewOptimizationNoAutoFixSuggestions(t *testing.T) {
	o := newTestOptimizer()
	scan := &ent.AiScanResult{
		Score: 50,
		Suggestions: []map[string]interface{}{
			{
				"category": "docs",
				"message":  "Add README",
				"priority": "high",
				"auto_fix": false,
			},
		},
	}
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{}

	preview, err := o.PreviewOptimization(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview != nil {
		t.Error("expected nil preview when no auto-fix suggestions")
	}
}

func TestPreviewOptimizationAgentsMD(t *testing.T) {
	o := newTestOptimizer()
	scan := &ent.AiScanResult{
		Score: 30,
		Suggestions: []map[string]interface{}{
			{
				"category": "ai_files",
				"message":  "Add AGENTS.md or CLAUDE.md to help AI assistants understand your project",
				"priority": "high",
				"auto_fix": true,
			},
		},
	}
	rc := &ent.RepoConfig{
		FullName:      "org/test-repo",
		DefaultBranch: "main",
	}
	// File doesn't exist on remote
	provider := &mockSCMProvider{
		getFileErr: fmt.Errorf("not found"),
	}

	preview, err := o.PreviewOptimization(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview == nil {
		t.Fatal("expected non-nil preview")
	}
	if len(preview.Files) == 0 {
		t.Fatal("expected at least one file in preview")
	}

	found := false
	for _, f := range preview.Files {
		if f.Path == "AGENTS.md" {
			found = true
			if !f.IsNew {
				t.Error("AGENTS.md should be marked as new")
			}
			if f.NewContent == "" {
				t.Error("AGENTS.md should have content")
			}
			if !strings.Contains(f.NewContent, "org/test-repo") {
				t.Error("AGENTS.md should contain repo name")
			}
		}
	}
	if !found {
		t.Error("expected AGENTS.md in preview files")
	}
}

func TestPreviewOptimizationEditorConfig(t *testing.T) {
	o := newTestOptimizer()
	scan := &ent.AiScanResult{
		Score: 30,
		Suggestions: []map[string]interface{}{
			{
				"category": "ai_files",
				"message":  "Add .editorconfig and .prettierrc for consistent formatting",
				"priority": "low",
				"auto_fix": true,
			},
		},
	}
	rc := &ent.RepoConfig{
		FullName:      "org/test-repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{
		getFileErr: fmt.Errorf("not found"),
	}

	preview, err := o.PreviewOptimization(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview == nil {
		t.Fatal("expected non-nil preview")
	}

	found := false
	for _, f := range preview.Files {
		if f.Path == ".editorconfig" {
			found = true
			if !f.IsNew {
				t.Error(".editorconfig should be marked as new")
			}
			if !strings.Contains(f.NewContent, "root = true") {
				t.Error(".editorconfig should contain 'root = true'")
			}
		}
	}
	if !found {
		t.Error("expected .editorconfig in preview files")
	}
}

func TestPreviewOptimizationSkipsIdenticalContent(t *testing.T) {
	o := newTestOptimizer()
	editorConfigContent := generateEditorConfig()

	scan := &ent.AiScanResult{
		Score: 30,
		Suggestions: []map[string]interface{}{
			{
				"category": "ai_files",
				"message":  "Add .editorconfig and .prettierrc for consistent formatting",
				"priority": "low",
				"auto_fix": true,
			},
		},
	}
	rc := &ent.RepoConfig{
		FullName:      "org/test-repo",
		DefaultBranch: "main",
	}
	// Remote already has identical content
	provider := &mockSCMProvider{
		fileContents: map[string][]byte{
			".editorconfig": []byte(editorConfigContent),
		},
	}

	preview, err := o.PreviewOptimization(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should be nil because the only file is identical
	if preview != nil {
		t.Error("expected nil preview when file content is identical")
	}
}

func TestPreviewOptimizationExistingDifferentContent(t *testing.T) {
	o := newTestOptimizer()

	scan := &ent.AiScanResult{
		Score: 30,
		Suggestions: []map[string]interface{}{
			{
				"category": "ai_files",
				"message":  "Add .editorconfig and .prettierrc for consistent formatting",
				"priority": "low",
				"auto_fix": true,
			},
		},
	}
	rc := &ent.RepoConfig{
		FullName:      "org/test-repo",
		DefaultBranch: "main",
	}
	// Remote has different content
	provider := &mockSCMProvider{
		fileContents: map[string][]byte{
			".editorconfig": []byte("[*]\nindent_style = tab\n"),
		},
	}

	preview, err := o.PreviewOptimization(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview == nil {
		t.Fatal("expected non-nil preview for different content")
	}

	for _, f := range preview.Files {
		if f.Path == ".editorconfig" {
			if f.IsNew {
				t.Error(".editorconfig should NOT be marked as new (exists with different content)")
			}
			if f.OldContent == "" {
				t.Error("OldContent should be populated")
			}
		}
	}
}

func TestConfirmOptimizationEmptyFiles(t *testing.T) {
	o := newTestOptimizer()
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{}

	_, err := o.ConfirmOptimization(context.Background(), provider, rc, map[string]string{}, 50)
	if err == nil {
		t.Fatal("expected error for empty files")
	}
	if !strings.Contains(err.Error(), "no files to commit") {
		t.Errorf("error = %q, want 'no files to commit'", err.Error())
	}
}

func TestConfirmOptimizationGetBranchSHAError(t *testing.T) {
	o := newTestOptimizer()
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{
		branchSHAErr: fmt.Errorf("branch not found"),
	}

	_, err := o.ConfirmOptimization(context.Background(), provider, rc, map[string]string{"f.txt": "content"}, 50)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "get branch SHA") {
		t.Errorf("error = %q, want 'get branch SHA'", err.Error())
	}
}

func TestConfirmOptimizationCreateBranchError(t *testing.T) {
	o := newTestOptimizer()
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{
		branchSHA:       "abc123",
		createBranchErr: fmt.Errorf("permission denied"),
	}

	_, err := o.ConfirmOptimization(context.Background(), provider, rc, map[string]string{"f.txt": "content"}, 50)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "create branch") {
		t.Errorf("error = %q, want 'create branch'", err.Error())
	}
}

func TestConfirmOptimizationCommitFilesError(t *testing.T) {
	o := newTestOptimizer()
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{
		branchSHA:      "abc123",
		commitFilesErr: fmt.Errorf("commit failed"),
	}

	_, err := o.ConfirmOptimization(context.Background(), provider, rc, map[string]string{"f.txt": "content"}, 50)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "commit files") {
		t.Errorf("error = %q, want 'commit files'", err.Error())
	}
}

func TestConfirmOptimizationCreatePRError(t *testing.T) {
	o := newTestOptimizer()
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{
		branchSHA:  "abc123",
		commitSHA:  "def456",
		createPRErr: fmt.Errorf("PR creation failed"),
	}

	_, err := o.ConfirmOptimization(context.Background(), provider, rc, map[string]string{"AGENTS.md": "# Agents"}, 50)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "create PR") {
		t.Errorf("error = %q, want 'create PR'", err.Error())
	}
}

func TestConfirmOptimizationSuccess(t *testing.T) {
	o := newTestOptimizer()
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{
		branchSHA: "abc123",
		commitSHA: "def456",
		createPRResult: &scm.PR{
			ID:  42,
			URL: "https://github.com/org/repo/pull/42",
		},
	}

	files := map[string]string{
		"AGENTS.md":     "# Agents",
		".editorconfig": "[*]\nindent_style = space",
	}
	result, err := o.ConfirmOptimization(context.Background(), provider, rc, files, 75)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.PRID != 42 {
		t.Errorf("PRID = %d, want 42", result.PRID)
	}
	if result.PRURL != "https://github.com/org/repo/pull/42" {
		t.Errorf("PRURL = %q, want expected URL", result.PRURL)
	}
	if result.FilesAdded != 2 {
		t.Errorf("FilesAdded = %d, want 2", result.FilesAdded)
	}
	if !strings.HasPrefix(result.BranchName, "ai-efficiency/optimize-") {
		t.Errorf("BranchName = %q, want prefix 'ai-efficiency/optimize-'", result.BranchName)
	}
}

func TestCreateOptimizationPRNilPreview(t *testing.T) {
	o := newTestOptimizer()
	scan := &ent.AiScanResult{
		Score:       80,
		Suggestions: nil,
	}
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{}

	result, err := o.CreateOptimizationPR(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Error("expected nil result when no optimization needed")
	}
}

func TestCreateOptimizationPRSuccess(t *testing.T) {
	o := newTestOptimizer()
	scan := &ent.AiScanResult{
		Score: 30,
		Suggestions: []map[string]interface{}{
			{
				"category": "ai_files",
				"message":  "Add .editorconfig and .prettierrc for consistent formatting",
				"priority": "low",
				"auto_fix": true,
			},
		},
	}
	rc := &ent.RepoConfig{
		FullName:      "org/repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{
		getFileErr: fmt.Errorf("not found"),
		branchSHA:  "abc123",
		commitSHA:  "def456",
		createPRResult: &scm.PR{
			ID:  10,
			URL: "https://github.com/org/repo/pull/10",
		},
	}

	result, err := o.CreateOptimizationPR(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.PRID != 10 {
		t.Errorf("PRID = %d, want 10", result.PRID)
	}
}

func TestBuildPRDescriptionUnknownFile(t *testing.T) {
	files := map[string]string{
		"custom-file.txt": "content",
	}
	content := buildPRDescription(files, nil, 55)
	if !strings.Contains(content, "custom-file.txt") {
		t.Error("buildPRDescription() missing custom file name")
	}
	if !strings.Contains(content, "55/100") {
		t.Error("buildPRDescription() missing score")
	}
}

func TestBuildPRDescriptionEmptyDims(t *testing.T) {
	files := map[string]string{"AGENTS.md": "# Agents"}
	content := buildPRDescription(files, []rules.DimensionScore{}, 42)
	if !strings.Contains(content, "42/100") {
		t.Error("buildPRDescription() missing score")
	}
	if !strings.Contains(content, "Dimension") {
		t.Error("buildPRDescription() missing table header")
	}
}

func TestExtractDimensionsInvalidType(t *testing.T) {
	scan := &ent.AiScanResult{
		Dimensions: map[string]interface{}{
			"bad_dim": "not a map",
		},
	}
	dims := extractDimensions(scan)
	if len(dims) != 0 {
		t.Errorf("expected 0 dimensions for invalid type, got %d", len(dims))
	}
}

func TestExtractDimensionsMultiple(t *testing.T) {
	scan := &ent.AiScanResult{
		Dimensions: map[string]interface{}{
			"ai_files": map[string]interface{}{
				"score":     float64(15),
				"max_score": float64(20),
				"details":   "has AGENTS.md",
			},
			"structure": map[string]interface{}{
				"score":     float64(10),
				"max_score": float64(15),
				"details":   "good structure",
			},
		},
	}
	dims := extractDimensions(scan)
	if len(dims) != 2 {
		t.Errorf("expected 2 dimensions, got %d", len(dims))
	}
}

func TestGenerateAgentsMDTemplateNoDims(t *testing.T) {
	content := generateAgentsMDTemplate("org/repo", nil)
	if !strings.Contains(content, "org/repo") {
		t.Error("template should contain repo name")
	}
	if !strings.Contains(content, "## Project Overview") {
		t.Error("template should contain Project Overview section")
	}
}

func TestGenerateSuggestionsHighRatio(t *testing.T) {
	// Dimensions with ratio >= 0.8 should not generate suggestions
	dims := []rules.DimensionScore{
		{Name: "ai_files", Score: 18, MaxScore: 20, Details: "good"},
		{Name: "structure", Score: 14, MaxScore: 15, Details: "good"},
		{Name: "docs", Score: 14, MaxScore: 15, Details: "good"},
		{Name: "testing", Score: 9, MaxScore: 10, Details: "good"},
	}
	suggestions := generateSuggestions(dims)
	if len(suggestions) != 0 {
		t.Errorf("expected 0 suggestions for high-scoring dims, got %d", len(suggestions))
	}
}

func TestGenerateSuggestionsLowRatio(t *testing.T) {
	dims := []rules.DimensionScore{
		{Name: "ai_files", Score: 0, MaxScore: 20, Details: "no AI files"},
		{Name: "docs", Score: 2, MaxScore: 15, Details: "minimal docs"},
		{Name: "testing", Score: 1, MaxScore: 10, Details: "no tests"},
		{Name: "structure", Score: 3, MaxScore: 15, Details: "poor structure"},
	}
	suggestions := generateSuggestions(dims)
	if len(suggestions) == 0 {
		t.Error("expected suggestions for low-scoring dims")
	}

	// Check that ai_files with ratio=0 generates high priority + auto_fix
	hasHighAI := false
	hasAutoFix := false
	for _, s := range suggestions {
		if s.Category == "ai_files" && s.Priority == "high" {
			hasHighAI = true
		}
		if s.Category == "ai_files" && s.AutoFix {
			hasAutoFix = true
		}
	}
	if !hasHighAI {
		t.Error("expected high-priority ai_files suggestion")
	}
	if !hasAutoFix {
		t.Error("expected auto-fix ai_files suggestion")
	}
}

func TestGenerateSuggestionsMediumRatio(t *testing.T) {
	dims := []rules.DimensionScore{
		{Name: "ai_files", Score: 10, MaxScore: 20, Details: "partial"},
		{Name: "docs", Score: 10, MaxScore: 15, Details: "ok docs"},
		{Name: "testing", Score: 5, MaxScore: 10, Details: "some tests"},
	}
	suggestions := generateSuggestions(dims)

	for _, s := range suggestions {
		if s.Category == "ai_files" && s.Priority == "high" {
			t.Error("ai_files with ratio 0.5 should not be high priority")
		}
	}
}

func TestStaticScannerScoreCap(t *testing.T) {
	// Verify score is capped at 60
	scanner := NewStaticScanner()
	result, err := scanner.Scan(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if result.Score > 60 {
		t.Errorf("score = %d, should be capped at 60", result.Score)
	}
}

// ---------------------------------------------------------------------------
// generateAgentsMD tests
// ---------------------------------------------------------------------------

func TestGenerateAgentsMDNoLLM(t *testing.T) {
	// When llmAnalyzer is nil, should fall back to template
	o := NewOptimizer(nil, zap.NewNop())
	rc := &ent.RepoConfig{FullName: "org/repo"}
	dims := []rules.DimensionScore{
		{Name: "ai_files", Score: 5, MaxScore: 20, Details: "partial"},
	}
	content, err := o.generateAgentsMD(context.Background(), rc, dims, 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "org/repo") {
		t.Error("template should contain repo name")
	}
	if !strings.Contains(content, "## Project Overview") {
		t.Error("template should contain Project Overview")
	}
}

func TestGenerateAgentsMDWithLLMDisabled(t *testing.T) {
	// LLM analyzer exists but is not enabled
	analyzer := llm.NewAnalyzer(config.LLMConfig{}, nil, zap.NewNop())
	o := NewOptimizer(analyzer, zap.NewNop())
	rc := &ent.RepoConfig{FullName: "org/repo"}

	content, err := o.generateAgentsMD(context.Background(), rc, nil, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "org/repo") {
		t.Error("should fall back to template when LLM disabled")
	}
}

func TestGenerateAgentsMDWithLLMSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "# Generated AGENTS.md\n\nThis is AI-generated content."}},
			},
			"usage": map[string]int{"total_tokens": 200},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	rp := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "gpt-4", zap.NewNop())
	analyzer := llm.NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())
	o := NewOptimizer(analyzer, zap.NewNop())
	rc := &ent.RepoConfig{FullName: "org/repo"}
	dims := []rules.DimensionScore{
		{Name: "ai_files", Score: 5, MaxScore: 20, Details: "partial"},
	}

	content, err := o.generateAgentsMD(context.Background(), rc, dims, 40)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(content, "Generated AGENTS.md") {
		t.Error("should use LLM-generated content")
	}
}

func TestGenerateAgentsMDWithLLMFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer server.Close()

	rp := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "gpt-4", zap.NewNop())
	analyzer := llm.NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())
	o := NewOptimizer(analyzer, zap.NewNop())
	rc := &ent.RepoConfig{FullName: "org/repo"}

	// Should return error (caller in PreviewOptimization catches this and falls back)
	_, err := o.generateAgentsMD(context.Background(), rc, nil, 50)
	if err == nil {
		t.Fatal("expected error when LLM fails")
	}
}

func TestPreviewOptimizationAgentsMDWithLLMFallback(t *testing.T) {
	// When LLM fails, PreviewOptimization should fall back to template
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer server.Close()

	rp := relay.NewSub2apiProvider(server.Client(), server.URL+"/v1", server.URL, "sk-test", "gpt-4", zap.NewNop())
	analyzer := llm.NewAnalyzer(config.LLMConfig{
	}, rp, zap.NewNop())
	o := NewOptimizer(analyzer, zap.NewNop())

	scan := &ent.AiScanResult{
		Score: 30,
		Suggestions: []map[string]interface{}{
			{
				"category": "ai_files",
				"message":  "Add AGENTS.md or CLAUDE.md to help AI assistants understand your project",
				"priority": "high",
				"auto_fix": true,
			},
		},
	}
	rc := &ent.RepoConfig{
		FullName:      "org/test-repo",
		DefaultBranch: "main",
	}
	provider := &mockSCMProvider{
		getFileErr: fmt.Errorf("not found"),
	}

	preview, err := o.PreviewOptimization(context.Background(), provider, rc, scan)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview == nil {
		t.Fatal("expected non-nil preview (should fall back to template)")
	}

	found := false
	for _, f := range preview.Files {
		if f.Path == "AGENTS.md" {
			found = true
			if !strings.Contains(f.NewContent, "## Project Overview") {
				t.Error("should contain template content after LLM fallback")
			}
		}
	}
	if !found {
		t.Error("expected AGENTS.md in preview")
	}
}
