package analysis

import (
	"context"
	"math"

	"github.com/ai-efficiency/backend/internal/analysis/rules"
)

// StaticScanner performs static analysis on a repo directory.
type StaticScanner struct{}

// NewStaticScanner creates a new StaticScanner.
func NewStaticScanner() *StaticScanner {
	return &StaticScanner{}
}

// Scan runs all static rules against the repo path and returns a ScanResult.
func (s *StaticScanner) Scan(_ context.Context, repoPath string) (*ScanResult, error) {
	dims := []rules.DimensionScore{
		rules.AIFiles(repoPath),
		rules.Structure(repoPath),
		rules.Docs(repoPath),
		rules.Testing(repoPath),
	}

	var totalScore float64
	for _, d := range dims {
		totalScore += d.Score
	}

	// Static rules max = 60, normalize to 0-60 range
	score := int(math.Round(totalScore))
	if score > 60 {
		score = 60
	}

	suggestions := generateSuggestions(dims)

	return &ScanResult{
		Score:       score,
		Dimensions:  dims,
		Suggestions: suggestions,
		ScanType:    "static",
	}, nil
}

func generateSuggestions(dims []rules.DimensionScore) []rules.Suggestion {
	var suggestions []rules.Suggestion
	for _, d := range dims {
		ratio := d.Score / d.MaxScore
		if ratio >= 0.8 {
			continue
		}
		priority := "medium"
		if ratio < 0.3 {
			priority = "high"
		}
		switch d.Name {
		case "ai_files":
			if ratio == 0 {
				suggestions = append(suggestions, rules.Suggestion{
					Category: "ai_files",
					Message:  "Add AGENTS.md or CLAUDE.md to help AI assistants understand your project",
					Priority: priority,
					AutoFix:  true,
				})
			}
			suggestions = append(suggestions, rules.Suggestion{
				Category: "ai_files",
				Message:  "Add .editorconfig and .prettierrc for consistent formatting",
				Priority: "low",
				AutoFix:  true,
			})
		case "structure":
			suggestions = append(suggestions, rules.Suggestion{
				Category: "structure",
				Message:  d.Details,
				Priority: priority,
			})
		case "docs":
			if ratio < 0.5 {
				suggestions = append(suggestions, rules.Suggestion{
					Category: "docs",
					Message:  "Add or improve README.md with project overview, setup instructions, and usage examples",
					Priority: priority,
				})
			}
		case "testing":
			suggestions = append(suggestions, rules.Suggestion{
				Category: "testing",
				Message:  "Add test files and CI configuration to improve code quality",
				Priority: priority,
			})
		}
	}
	return suggestions
}
