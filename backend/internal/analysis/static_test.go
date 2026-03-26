package analysis

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStaticScannerEmptyRepo(t *testing.T) {
	dir := t.TempDir()

	scanner := NewStaticScanner()
	result, err := scanner.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	// Empty dir gets structure points (no deep nesting, no large files, consistent naming)
	// structure = 15 (5+5+5), all others = 0
	if result.Score != 15 {
		t.Errorf("empty repo score = %d, want 15", result.Score)
	}
	if result.ScanType != "static" {
		t.Errorf("scan_type = %s, want static", result.ScanType)
	}
	if len(result.Dimensions) != 4 {
		t.Errorf("dimensions count = %d, want 4", len(result.Dimensions))
	}
}

func TestStaticScannerWithAIFiles(t *testing.T) {
	dir := t.TempDir()

	// Create AI assistant files
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents\nSome content"), 0o644)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude\nSome content"), 0o644)
	os.WriteFile(filepath.Join(dir, ".editorconfig"), []byte("[*]\nindent_style = space"), 0o644)

	scanner := NewStaticScanner()
	result, err := scanner.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	// AGENTS.md=5 + CLAUDE.md=5 + .editorconfig=3 = 13 from ai_files
	var aiScore float64
	for _, d := range result.Dimensions {
		if d.Name == "ai_files" {
			aiScore = d.Score
			break
		}
	}
	if aiScore != 13 {
		t.Errorf("ai_files score = %v, want 13", aiScore)
	}
}

func TestStaticScannerWithDocs(t *testing.T) {
	dir := t.TempDir()

	// Create a detailed README
	readme := "# Project\n\n" + "This is a detailed readme.\n"
	for i := 0; i < 40; i++ {
		readme += "Line of documentation content.\n"
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644)
	os.WriteFile(filepath.Join(dir, "CONTRIBUTING.md"), []byte("# Contributing\nHow to contribute"), 0o644)

	scanner := NewStaticScanner()
	result, err := scanner.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	var docsScore float64
	for _, d := range result.Dimensions {
		if d.Name == "docs" {
			docsScore = d.Score
			break
		}
	}
	// README (detailed, >30 lines) = 6 + CONTRIBUTING = 2 = 8
	if docsScore != 8 {
		t.Errorf("docs score = %v, want 8", docsScore)
	}
}

func TestStaticScannerWithTests(t *testing.T) {
	dir := t.TempDir()

	// Create test file and CI config
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "main_test.go"), []byte("package pkg\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755)
	os.WriteFile(filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte("name: CI"), 0o644)

	scanner := NewStaticScanner()
	result, err := scanner.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	var testScore float64
	for _, d := range result.Dimensions {
		if d.Name == "testing" {
			testScore = d.Score
			break
		}
	}
	// test files = 4 + CI config = 3 = 7
	if testScore != 7 {
		t.Errorf("testing score = %v, want 7", testScore)
	}
}

func TestStaticScannerSuggestions(t *testing.T) {
	dir := t.TempDir()

	scanner := NewStaticScanner()
	result, err := scanner.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	if len(result.Suggestions) == 0 {
		t.Error("expected suggestions for empty repo, got none")
	}

	// Should have high-priority suggestion for AI files
	hasAIFileSuggestion := false
	for _, s := range result.Suggestions {
		if s.Category == "ai_files" && s.Priority == "high" {
			hasAIFileSuggestion = true
			break
		}
	}
	if !hasAIFileSuggestion {
		t.Error("expected high-priority ai_files suggestion")
	}
}

func TestStaticScannerFullScore(t *testing.T) {
	dir := t.TempDir()

	// Create all AI files
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents"), 0o644)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0o644)
	os.WriteFile(filepath.Join(dir, ".cursorrules"), []byte("rules"), 0o644)
	os.WriteFile(filepath.Join(dir, ".editorconfig"), []byte("[*]"), 0o644)
	os.WriteFile(filepath.Join(dir, ".prettierrc"), []byte("{}"), 0o644)

	// Create docs
	readme := "# Project\n"
	for i := 0; i < 40; i++ {
		readme += "Documentation line.\n"
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644)
	os.MkdirAll(filepath.Join(dir, "docs", "api"), 0o755)
	os.WriteFile(filepath.Join(dir, "docs", "api", "index.md"), []byte("API"), 0o644)
	os.WriteFile(filepath.Join(dir, "CONTRIBUTING.md"), []byte("# Contributing"), 0o644)
	os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog"), 0o644)

	// Create tests
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "main_test.go"), []byte("package pkg"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755)
	os.WriteFile(filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte("name: CI"), 0o644)
	os.WriteFile(filepath.Join(dir, "vitest.config.ts"), []byte("export default {}"), 0o644)

	scanner := NewStaticScanner()
	result, err := scanner.Scan(context.Background(), dir)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}

	// ai_files=20 + structure=15 + docs=15 + testing=10 = 60
	if result.Score != 60 {
		t.Errorf("full score = %d, want 60", result.Score)
		for _, d := range result.Dimensions {
			t.Logf("  %s: %.0f/%.0f (%s)", d.Name, d.Score, d.MaxScore, d.Details)
		}
	}
}
