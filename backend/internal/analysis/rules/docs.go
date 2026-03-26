package rules

import (
	"os"
	"path/filepath"
	"strings"
)

// Docs checks documentation completeness.
// Max score: 15
func Docs(repoPath string) DimensionScore {
	dim := DimensionScore{
		Name:     "docs",
		MaxScore: 15,
	}

	var found []string

	// README.md (6 points)
	readmePath := findFile(repoPath, []string{"README.md", "readme.md", "Readme.md"})
	if readmePath != "" {
		data, err := os.ReadFile(readmePath)
		if err == nil {
			content := string(data)
			lines := len(strings.Split(content, "\n"))
			if lines > 30 {
				dim.Score += 6
				found = append(found, "README.md (detailed)")
			} else if lines > 5 {
				dim.Score += 4
				found = append(found, "README.md (basic)")
			} else {
				dim.Score += 2
				found = append(found, "README.md (minimal)")
			}
		}
	}

	// API docs (5 points)
	apiDocPatterns := []string{
		"docs/api", "doc/api", "api/docs",
		"swagger.json", "swagger.yaml", "openapi.json", "openapi.yaml",
		"docs/swagger", "docs/openapi",
	}
	for _, p := range apiDocPatterns {
		if fileExists(filepath.Join(repoPath, p)) {
			dim.Score += 5
			found = append(found, "API docs")
			break
		}
	}

	// Contributing / changelog (4 points)
	extras := []struct {
		patterns []string
		score    float64
		label    string
	}{
		{[]string{"CONTRIBUTING.md", "contributing.md"}, 2, "CONTRIBUTING.md"},
		{[]string{"CHANGELOG.md", "changelog.md", "CHANGES.md"}, 2, "CHANGELOG.md"},
	}
	for _, e := range extras {
		for _, p := range e.patterns {
			if fileExists(filepath.Join(repoPath, p)) {
				dim.Score += e.score
				found = append(found, e.label)
				break
			}
		}
	}

	if len(found) > 0 {
		dim.Details = "Found: " + strings.Join(found, ", ")
	} else {
		dim.Details = "No documentation found"
	}
	return dim
}

func findFile(dir string, names []string) string {
	for _, n := range names {
		p := filepath.Join(dir, n)
		if fileExists(p) {
			return p
		}
	}
	return ""
}
