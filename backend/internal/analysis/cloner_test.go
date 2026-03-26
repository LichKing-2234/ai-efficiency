package analysis

import (
	"testing"

	"go.uber.org/zap"
)

func TestClonerCloneOrUpdateInvalidScheme(t *testing.T) {
	c := NewCloner(t.TempDir(), zap.NewNop())

	tests := []struct {
		name     string
		cloneURL string
	}{
		{"http scheme", "http://github.com/org/repo.git"},
		{"ftp scheme", "ftp://github.com/org/repo.git"},
		{"no scheme", "github.com/org/repo.git"},
		{"empty string", ""},
		{"relative path", "../some/path"},
		{"file scheme", "file:///tmp/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.CloneOrUpdate(tt.cloneURL, 1)
			if err == nil {
				t.Errorf("CloneOrUpdate(%q) should return error for invalid scheme", tt.cloneURL)
			}
		})
	}
}

func TestClonerRepoPathZeroID(t *testing.T) {
	c := NewCloner("/data", zap.NewNop())
	got := c.RepoPath(0)
	if got == "" {
		t.Error("RepoPath(0) should not be empty")
	}
}
