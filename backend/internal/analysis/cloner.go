package analysis

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap"
)

// Cloner handles git clone and cache management for repos.
type Cloner struct {
	dataDir string
	logger  *zap.Logger
	mu      sync.Mutex
}

// NewCloner creates a new Cloner.
func NewCloner(dataDir string, logger *zap.Logger) *Cloner {
	return &Cloner{dataDir: dataDir, logger: logger}
}

// CloneOrUpdate clones a repo (shallow) or updates an existing clone.
// Returns the local path to the repo.
func (c *Cloner) CloneOrUpdate(cloneURL string, repoConfigID int) (string, error) {
	// Validate clone URL to prevent command injection
	if !strings.HasPrefix(cloneURL, "https://") && !strings.HasPrefix(cloneURL, "ssh://") && !strings.HasPrefix(cloneURL, "git@") {
		return "", fmt.Errorf("invalid clone URL scheme: only https://, ssh://, and git@ are allowed")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	repoDir := filepath.Join(c.dataDir, "repos", fmt.Sprintf("%d", repoConfigID))

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		// Existing clone — fetch + reset
		c.logger.Debug("updating existing clone", zap.String("path", repoDir))
		cmd := exec.Command("git", "-C", repoDir, "fetch", "--depth", "1", "origin")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git fetch: %s: %w", string(out), err)
		}
		cmd = exec.Command("git", "-C", repoDir, "reset", "--hard", "FETCH_HEAD")
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git reset: %s: %w", string(out), err)
		}
		return repoDir, nil
	}

	// Fresh shallow clone
	c.logger.Debug("cloning repo", zap.String("url", cloneURL), zap.String("path", repoDir))
	if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	cmd := exec.Command("git", "clone", "--depth", "1", cloneURL, repoDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone: %s: %w", string(out), err)
	}
	return repoDir, nil
}

// RepoPath returns the expected local path for a repo.
func (c *Cloner) RepoPath(repoConfigID int) string {
	return filepath.Join(c.dataDir, "repos", fmt.Sprintf("%d", repoConfigID))
}
