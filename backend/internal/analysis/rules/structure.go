package rules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Structure checks project structure quality.
// Max score: 15
func Structure(repoPath string) DimensionScore {
	dim := DimensionScore{
		Name:     "structure",
		MaxScore: 15,
	}

	var issues []string

	// Check max directory depth (penalty if > 8 levels)
	maxDepth := measureMaxDepth(repoPath, 0, 10)
	if maxDepth <= 6 {
		dim.Score += 5
	} else if maxDepth <= 8 {
		dim.Score += 3
	} else {
		issues = append(issues, fmt.Sprintf("deep nesting (%d levels)", maxDepth))
	}

	// Check for oversized files (> 500 lines)
	largeFiles := countLargeFiles(repoPath, 500)
	if largeFiles == 0 {
		dim.Score += 5
	} else if largeFiles <= 3 {
		dim.Score += 3
		issues = append(issues, fmt.Sprintf("%d large files (>500 lines)", largeFiles))
	} else {
		dim.Score += 1
		issues = append(issues, fmt.Sprintf("%d large files (>500 lines)", largeFiles))
	}

	// Check for consistent naming (no mixed case styles in top-level dirs)
	if hasConsistentNaming(repoPath) {
		dim.Score += 5
	} else {
		dim.Score += 2
		issues = append(issues, "inconsistent naming in top-level directories")
	}

	if len(issues) > 0 {
		dim.Details = "Issues: " + strings.Join(issues, "; ")
	} else {
		dim.Details = "Good project structure"
	}
	return dim
}

func measureMaxDepth(dir string, current, limit int) int {
	if current >= limit {
		return current
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return current
	}
	max := current
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" || e.Name() == "vendor" || e.Name() == "__pycache__" {
			continue
		}
		d := measureMaxDepth(filepath.Join(dir, e.Name()), current+1, limit)
		if d > max {
			max = d
		}
	}
	return max
}

func countLargeFiles(dir string, threshold int) int {
	count := 0
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		codeExts := map[string]bool{".go": true, ".js": true, ".ts": true, ".py": true, ".java": true, ".vue": true, ".tsx": true, ".jsx": true, ".rb": true, ".rs": true}
		if !codeExts[ext] {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := len(strings.Split(string(data), "\n"))
		if lines > threshold {
			count++
		}
		return nil
	})
	return count
}

func hasConsistentNaming(repoPath string) bool {
	entries, err := os.ReadDir(repoPath)
	if err != nil {
		return true
	}
	hasCamel, hasSnake, hasKebab := false, false, false
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		name := e.Name()
		if strings.Contains(name, "_") {
			hasSnake = true
		} else if strings.Contains(name, "-") {
			hasKebab = true
		} else if name != strings.ToLower(name) {
			hasCamel = true
		}
	}
	styles := 0
	if hasCamel {
		styles++
	}
	if hasSnake {
		styles++
	}
	if hasKebab {
		styles++
	}
	return styles <= 1
}
