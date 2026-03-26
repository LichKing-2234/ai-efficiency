package rules

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// Testing checks test coverage indicators.
// Max score: 10
func Testing(repoPath string) DimensionScore {
	dim := DimensionScore{
		Name:     "testing",
		MaxScore: 10,
	}

	var found []string

	// Test files exist (4 points)
	hasTests := hasTestFiles(repoPath)
	if hasTests {
		dim.Score += 4
		found = append(found, "test files")
	}

	// CI config (3 points)
	ciPatterns := []string{
		".github/workflows",
		".gitlab-ci.yml",
		"Jenkinsfile",
		".circleci/config.yml",
		".travis.yml",
		"azure-pipelines.yml",
	}
	for _, p := range ciPatterns {
		if fileExists(filepath.Join(repoPath, p)) {
			dim.Score += 3
			found = append(found, "CI config")
			break
		}
	}

	// Test framework config (3 points)
	frameworkPatterns := []string{
		"jest.config.js", "jest.config.ts",
		"vitest.config.ts", "vitest.config.js",
		"pytest.ini", "pyproject.toml",
		"phpunit.xml",
		".rspec",
	}
	for _, p := range frameworkPatterns {
		if fileExists(filepath.Join(repoPath, p)) {
			dim.Score += 3
			found = append(found, "test framework config")
			break
		}
	}

	if len(found) > 0 {
		dim.Details = "Found: " + strings.Join(found, ", ")
	} else {
		dim.Details = "No test infrastructure found"
	}
	return dim
}

func hasTestFiles(repoPath string) bool {
	testPatterns := []string{
		"*_test.go",
		"*.test.js", "*.test.ts", "*.test.tsx",
		"*.spec.js", "*.spec.ts", "*.spec.tsx",
		"test_*.py", "*_test.py",
	}
	found := false
	_ = filepath.WalkDir(repoPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		for _, p := range testPatterns {
			matched, _ := filepath.Match(p, d.Name())
			if matched {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}
