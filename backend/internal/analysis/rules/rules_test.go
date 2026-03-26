package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- AIFiles tests ---

func TestAIFilesEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	result := AIFiles(dir)
	if result.Name != "ai_files" {
		t.Errorf("name = %s, want ai_files", result.Name)
	}
	if result.MaxScore != 20 {
		t.Errorf("max_score = %v, want 20", result.MaxScore)
	}
	if result.Score != 0 {
		t.Errorf("score = %v, want 0", result.Score)
	}
}

func TestAIFilesAllPresent(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents"), 0o644)
	os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("# Claude"), 0o644)
	os.WriteFile(filepath.Join(dir, ".cursorrules"), []byte("rules"), 0o644)
	os.WriteFile(filepath.Join(dir, ".editorconfig"), []byte("[*]"), 0o644)
	os.WriteFile(filepath.Join(dir, ".prettierrc"), []byte("{}"), 0o644)

	result := AIFiles(dir)
	if result.Score != 20 {
		t.Errorf("score = %v, want 20", result.Score)
	}
}

func TestAIFilesPartial(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# Agents"), 0o644)
	os.WriteFile(filepath.Join(dir, ".editorconfig"), []byte("[*]"), 0o644)

	result := AIFiles(dir)
	// AGENTS.md=5 + .editorconfig=3 = 8
	if result.Score != 8 {
		t.Errorf("score = %v, want 8", result.Score)
	}
}

func TestAIFilesCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "agents.md"), []byte("# Agents"), 0o644)

	result := AIFiles(dir)
	if result.Score != 5 {
		t.Errorf("score = %v, want 5 (lowercase agents.md)", result.Score)
	}
}

func TestAIFilesCursorAlternate(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".cursor"), 0o755)
	os.WriteFile(filepath.Join(dir, ".cursor", "rules"), []byte("rules"), 0o644)

	result := AIFiles(dir)
	if result.Score != 4 {
		t.Errorf("score = %v, want 4 (.cursor/rules)", result.Score)
	}
}

func TestAIFilesPrettierrcVariants(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".prettierrc.json"), []byte("{}"), 0o644)

	result := AIFiles(dir)
	if result.Score != 3 {
		t.Errorf("score = %v, want 3 (.prettierrc.json)", result.Score)
	}
}

// --- Docs tests ---

func TestDocsEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	result := Docs(dir)
	if result.Name != "docs" {
		t.Errorf("name = %s, want docs", result.Name)
	}
	if result.MaxScore != 15 {
		t.Errorf("max_score = %v, want 15", result.MaxScore)
	}
	if result.Score != 0 {
		t.Errorf("score = %v, want 0", result.Score)
	}
}

func TestDocsShortReadme(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("# Hi\n"), 0o644)

	result := Docs(dir)
	// <5 lines = 2 points
	if result.Score != 2 {
		t.Errorf("score = %v, want 2 (minimal readme)", result.Score)
	}
}

func TestDocsMediumReadme(t *testing.T) {
	dir := t.TempDir()
	content := "# Project\n"
	for i := 0; i < 15; i++ {
		content += "Line.\n"
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(content), 0o644)

	result := Docs(dir)
	// >5 lines, <=30 = 4 points
	if result.Score != 4 {
		t.Errorf("score = %v, want 4 (basic readme)", result.Score)
	}
}

func TestDocsDetailedReadme(t *testing.T) {
	dir := t.TempDir()
	content := "# Project\n"
	for i := 0; i < 40; i++ {
		content += "Documentation line.\n"
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(content), 0o644)

	result := Docs(dir)
	// >30 lines = 6 points
	if result.Score != 6 {
		t.Errorf("score = %v, want 6 (detailed readme)", result.Score)
	}
}

func TestDocsFullScore(t *testing.T) {
	dir := t.TempDir()
	content := "# Project\n"
	for i := 0; i < 40; i++ {
		content += "Documentation line.\n"
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte(content), 0o644)
	os.MkdirAll(filepath.Join(dir, "docs", "api"), 0o755)
	os.WriteFile(filepath.Join(dir, "docs", "api", "index.md"), []byte("API"), 0o644)
	os.WriteFile(filepath.Join(dir, "CONTRIBUTING.md"), []byte("# Contributing"), 0o644)
	os.WriteFile(filepath.Join(dir, "CHANGELOG.md"), []byte("# Changelog"), 0o644)

	result := Docs(dir)
	// README=6 + API=5 + CONTRIBUTING=2 + CHANGELOG=2 = 15
	if result.Score != 15 {
		t.Errorf("score = %v, want 15", result.Score)
	}
}

func TestDocsSwaggerFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "swagger.json"), []byte("{}"), 0o644)

	result := Docs(dir)
	if result.Score != 5 {
		t.Errorf("score = %v, want 5 (swagger.json)", result.Score)
	}
}

func TestDocsFindFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("# Hi"), 0o644)

	path := findFile(dir, []string{"README.md", "readme.md"})
	if path == "" {
		t.Error("findFile should find readme.md")
	}
}

func TestDocsFindFileNotFound(t *testing.T) {
	dir := t.TempDir()
	path := findFile(dir, []string{"README.md"})
	if path != "" {
		t.Errorf("findFile should return empty, got %s", path)
	}
}

// --- Structure tests ---

func TestStructureEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	result := Structure(dir)
	if result.Name != "structure" {
		t.Errorf("name = %s, want structure", result.Name)
	}
	if result.MaxScore != 15 {
		t.Errorf("max_score = %v, want 15", result.MaxScore)
	}
	// Empty dir: depth=0 (5pts), no large files (5pts), consistent naming (5pts) = 15
	if result.Score != 15 {
		t.Errorf("score = %v, want 15", result.Score)
	}
}

func TestStructureDeepNesting(t *testing.T) {
	dir := t.TempDir()
	// Create 9-level deep nesting
	nested := dir
	for i := 0; i < 9; i++ {
		nested = filepath.Join(nested, "level")
		os.MkdirAll(nested, 0o755)
	}

	result := Structure(dir)
	// depth=9 > 8, so 0 points for depth
	// no large files = 5, consistent naming = 5 → total = 10
	if result.Score != 10 {
		t.Errorf("score = %v, want 10 (deep nesting)", result.Score)
	}
}

func TestStructureLargeFiles(t *testing.T) {
	dir := t.TempDir()
	// Create a large .go file (>500 lines)
	content := ""
	for i := 0; i < 600; i++ {
		content += "// line\n"
	}
	os.WriteFile(filepath.Join(dir, "big.go"), []byte(content), 0o644)

	result := Structure(dir)
	// depth=0 (5pts), 1 large file (3pts), consistent naming (5pts) = 13
	if result.Score != 13 {
		t.Errorf("score = %v, want 13 (1 large file)", result.Score)
	}
}

func TestStructureManyLargeFiles(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("// line\n", 600)
	for i := 0; i < 5; i++ {
		os.WriteFile(filepath.Join(dir, "big"+string(rune('a'+i))+".go"), []byte(content), 0o644)
	}

	result := Structure(dir)
	// depth=0 (5pts), 5 large files (1pt), consistent naming (5pts) = 11
	if result.Score != 11 {
		t.Errorf("score = %v, want 11 (many large files)", result.Score)
	}
}

func TestStructureInconsistentNaming(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "my_module"), 0o755)   // snake_case
	os.MkdirAll(filepath.Join(dir, "my-component"), 0o755) // kebab-case

	result := Structure(dir)
	// depth <=6 (5pts), no large files (5pts), inconsistent naming (2pts) = 12
	if result.Score != 12 {
		t.Errorf("score = %v, want 12 (inconsistent naming)", result.Score)
	}
}

func TestMeasureMaxDepthSkipsHidden(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".git", "deep", "deep", "deep"), 0o755)
	os.MkdirAll(filepath.Join(dir, "src"), 0o755)

	depth := measureMaxDepth(dir, 0, 10)
	// Should skip .git, only count src (depth=1)
	if depth != 1 {
		t.Errorf("depth = %d, want 1 (should skip .git)", depth)
	}
}

func TestCountLargeFilesSkipsNonCode(t *testing.T) {
	dir := t.TempDir()
	content := strings.Repeat("line\n", 600)
	os.WriteFile(filepath.Join(dir, "data.txt"), []byte(content), 0o644)
	os.WriteFile(filepath.Join(dir, "image.png"), []byte(content), 0o644)

	count := countLargeFiles(dir, 500)
	if count != 0 {
		t.Errorf("count = %d, want 0 (non-code files)", count)
	}
}

func TestHasConsistentNamingEmpty(t *testing.T) {
	dir := t.TempDir()
	if !hasConsistentNaming(dir) {
		t.Error("empty dir should have consistent naming")
	}
}

func TestHasConsistentNamingSingleStyle(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "my_module"), 0o755)
	os.MkdirAll(filepath.Join(dir, "my_other"), 0o755)

	if !hasConsistentNaming(dir) {
		t.Error("single style (snake_case) should be consistent")
	}
}

// --- Testing tests ---

func TestTestingEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	result := Testing(dir)
	if result.Name != "testing" {
		t.Errorf("name = %s, want testing", result.Name)
	}
	if result.MaxScore != 10 {
		t.Errorf("max_score = %v, want 10", result.MaxScore)
	}
	if result.Score != 0 {
		t.Errorf("score = %v, want 0", result.Score)
	}
}

func TestTestingWithTestFiles(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "main_test.go"), []byte("package pkg"), 0o644)

	result := Testing(dir)
	if result.Score != 4 {
		t.Errorf("score = %v, want 4 (test files only)", result.Score)
	}
}

func TestTestingWithCIConfig(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755)
	os.WriteFile(filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte("name: CI"), 0o644)

	result := Testing(dir)
	if result.Score != 3 {
		t.Errorf("score = %v, want 3 (CI config only)", result.Score)
	}
}

func TestTestingWithFrameworkConfig(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "vitest.config.ts"), []byte("export default {}"), 0o644)

	result := Testing(dir)
	if result.Score != 3 {
		t.Errorf("score = %v, want 3 (framework config only)", result.Score)
	}
}

func TestTestingFullScore(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "pkg", "main_test.go"), []byte("package pkg"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".github", "workflows"), 0o755)
	os.WriteFile(filepath.Join(dir, ".github", "workflows", "ci.yml"), []byte("name: CI"), 0o644)
	os.WriteFile(filepath.Join(dir, "jest.config.js"), []byte("module.exports = {}"), 0o644)

	result := Testing(dir)
	// test files=4 + CI=3 + framework=3 = 10
	if result.Score != 10 {
		t.Errorf("score = %v, want 10", result.Score)
	}
}

func TestTestingGitlabCI(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitlab-ci.yml"), []byte("stages:"), 0o644)

	result := Testing(dir)
	if result.Score != 3 {
		t.Errorf("score = %v, want 3 (.gitlab-ci.yml)", result.Score)
	}
}

func TestHasTestFilesJSSpec(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.spec.ts"), []byte("test"), 0o644)

	if !hasTestFiles(dir) {
		t.Error("should detect .spec.ts files")
	}
}

func TestHasTestFilesPython(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test_main.py"), []byte("test"), 0o644)

	if !hasTestFiles(dir) {
		t.Error("should detect test_*.py files")
	}
}

func TestHasTestFilesSkipsNodeModules(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "node_modules", "pkg"), 0o755)
	os.WriteFile(filepath.Join(dir, "node_modules", "pkg", "index.test.js"), []byte("test"), 0o644)

	if hasTestFiles(dir) {
		t.Error("should skip node_modules")
	}
}

// --- fileExists tests ---

func TestFileExistsTrue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	os.WriteFile(path, []byte("hi"), 0o644)

	if !fileExists(path) {
		t.Error("fileExists should return true for existing file")
	}
}

func TestFileExistsFalse(t *testing.T) {
	if fileExists("/nonexistent/path/file.txt") {
		t.Error("fileExists should return false for non-existing file")
	}
}

func TestFileExistsDirectory(t *testing.T) {
	dir := t.TempDir()
	if !fileExists(dir) {
		t.Error("fileExists should return true for directories")
	}
}
