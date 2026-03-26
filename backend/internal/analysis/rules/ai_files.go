package rules

import (
	"os"
	"path/filepath"
	"strings"
)

// AIFiles checks for AI-assistant configuration files.
// Max score: 20
func AIFiles(repoPath string) DimensionScore {
	dim := DimensionScore{
		Name:     "ai_files",
		MaxScore: 20,
	}

	checks := []struct {
		patterns []string
		score    float64
		label    string
	}{
		{[]string{"AGENTS.md", "agents.md"}, 5, "AGENTS.md"},
		{[]string{"CLAUDE.md", "claude.md"}, 5, "CLAUDE.md"},
		{[]string{".cursorrules", ".cursor/rules"}, 4, ".cursorrules"},
		{[]string{".editorconfig"}, 3, ".editorconfig"},
		{[]string{".prettierrc", ".prettierrc.json", ".prettierrc.yaml", ".prettierrc.yml", ".prettierrc.js"}, 3, ".prettierrc"},
	}

	var found []string
	for _, c := range checks {
		for _, p := range c.patterns {
			if fileExists(filepath.Join(repoPath, p)) {
				dim.Score += c.score
				found = append(found, c.label)
				break
			}
		}
	}

	if len(found) > 0 {
		dim.Details = "Found: " + strings.Join(found, ", ")
	} else {
		dim.Details = "No AI assistant config files found"
	}
	return dim
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
