package analysis

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	scminternal "github.com/ai-efficiency/backend/internal/scm"
	"go.uber.org/zap"
)

// Cloner handles git clone and cache management for repos.
type Cloner struct {
	dataDir string
	logger  *zap.Logger
	mu      sync.Mutex
}

type CloneRequest struct {
	CloneURL     string
	RepoConfigID int
	ProviderID   int
	Auth         *scminternal.CloneAuthConfig
}

// NewCloner creates a new Cloner.
func NewCloner(dataDir string, logger *zap.Logger) *Cloner {
	return &Cloner{dataDir: dataDir, logger: logger}
}

// CloneOrUpdate clones a repo (shallow) or updates an existing clone.
// Returns the local path to the repo.
func (c *Cloner) CloneOrUpdate(cloneURL string, repoConfigID int) (string, error) {
	return c.CloneOrUpdateWithAuth(CloneRequest{
		CloneURL:     cloneURL,
		RepoConfigID: repoConfigID,
	})
}

// CloneOrUpdateWithAuth clones or updates a repo using the supplied auth configuration.
func (c *Cloner) CloneOrUpdateWithAuth(req CloneRequest) (string, error) {
	// Validate clone URL to prevent command injection
	if !strings.HasPrefix(req.CloneURL, "https://") && !strings.HasPrefix(req.CloneURL, "ssh://") && !strings.HasPrefix(req.CloneURL, "git@") {
		return "", fmt.Errorf("invalid clone URL scheme: only https://, ssh://, and git@ are allowed")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	repoDir := filepath.Join(c.dataDir, "repos", fmt.Sprintf("%d", req.RepoConfigID))

	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err == nil {
		// Existing clone — fetch + reset
		c.logger.Debug("updating existing clone", zap.String("path", repoDir))
		cmd, cleanup, err := c.gitCommand(repoDir, req, "git", "-C", repoDir, "fetch", "--depth", "1", "origin")
		if err != nil {
			return "", err
		}
		defer cleanup()
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git fetch: %s: %w", string(out), err)
		}
		cmd, cleanup, err = c.gitCommand(repoDir, req, "git", "-C", repoDir, "reset", "--hard", "FETCH_HEAD")
		if err != nil {
			return "", err
		}
		defer cleanup()
		if out, err := cmd.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git reset: %s: %w", string(out), err)
		}
		return repoDir, nil
	}

	// Fresh shallow clone
	c.logger.Debug("cloning repo", zap.String("url", req.CloneURL), zap.String("path", repoDir))
	if err := os.MkdirAll(filepath.Dir(repoDir), 0o755); err != nil {
		return "", fmt.Errorf("mkdir: %w", err)
	}
	cmd, cleanup, err := c.gitCommand(repoDir, req, "git", "clone", "--depth", "1", req.CloneURL, repoDir)
	if err != nil {
		return "", err
	}
	defer cleanup()
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git clone: %s: %w", string(out), err)
	}
	return repoDir, nil
}

// RepoPath returns the expected local path for a repo.
func (c *Cloner) RepoPath(repoConfigID int) string {
	return filepath.Join(c.dataDir, "repos", fmt.Sprintf("%d", repoConfigID))
}

func (c *Cloner) gitCommand(repoDir string, req CloneRequest, name string, args ...string) (*exec.Cmd, func(), error) {
	cmd := exec.Command(name, args...)
	env := append([]string{}, os.Environ()...)
	cleanup := func() {}

	if req.Auth == nil {
		cmd.Env = env
		return cmd, cleanup, nil
	}

	switch req.Auth.Protocol {
	case "https":
		scriptPath, err := c.writeAskPassScript(false)
		if err != nil {
			return nil, cleanup, err
		}
		cleanup = func() {
			_ = os.Remove(scriptPath)
		}
		env = append(env,
			"GIT_TERMINAL_PROMPT=0",
			"GIT_ASKPASS="+scriptPath,
			"AE_GIT_USERNAME="+req.Auth.HTTPSUsername,
			"AE_GIT_PASSWORD="+req.Auth.HTTPSPassword,
		)
	case "ssh":
		keyPath, err := c.writeTempFile("ssh-key-*", req.Auth.SSHPrivateKey, 0o600)
		if err != nil {
			return nil, cleanup, err
		}
		strictMode, knownHostsPath, err := c.ensureKnownHosts(req.ProviderID)
		if err != nil {
			_ = os.Remove(keyPath)
			return nil, cleanup, err
		}
		cleanup = func() {
			_ = os.Remove(keyPath)
		}

		sshCommand := fmt.Sprintf("ssh -i %s -o IdentitiesOnly=yes -o UserKnownHostsFile=%s -o StrictHostKeyChecking=%s", keyPath, knownHostsPath, strictMode)
		if strings.TrimSpace(req.Auth.SSHPassphrase) != "" {
			askPassPath, err := c.writeAskPassScript(true)
			if err != nil {
				_ = os.Remove(keyPath)
				return nil, cleanup, err
			}
			prevCleanup := cleanup
			cleanup = func() {
				prevCleanup()
				_ = os.Remove(askPassPath)
			}
			env = append(env,
				"SSH_ASKPASS="+askPassPath,
				"SSH_ASKPASS_REQUIRE=force",
				"DISPLAY=:0",
				"AE_SSH_PASSPHRASE="+req.Auth.SSHPassphrase,
			)
		}
		env = append(env, "GIT_SSH_COMMAND="+sshCommand)
	}

	cmd.Env = env
	return cmd, cleanup, nil
}

func (c *Cloner) writeAskPassScript(sshOnly bool) (string, error) {
	dir := filepath.Join(c.dataDir, "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir askpass dir: %w", err)
	}

	path := filepath.Join(dir, fmt.Sprintf("askpass-%d.sh", os.Getpid()))
	body := "#!/bin/sh\n"
	if sshOnly {
		body += "printf '%s\\n' \"$AE_SSH_PASSPHRASE\"\n"
	} else {
		body += "case \"$1\" in\n"
		body += "  *Username*) printf '%s\\n' \"$AE_GIT_USERNAME\" ;;\n"
		body += "  *) printf '%s\\n' \"$AE_GIT_PASSWORD\" ;;\n"
		body += "esac\n"
	}
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		return "", fmt.Errorf("write askpass script: %w", err)
	}
	return path, nil
}

func (c *Cloner) writeTempFile(pattern, content string, perm os.FileMode) (string, error) {
	dir := filepath.Join(c.dataDir, "tmp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir temp dir: %w", err)
	}
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := file.Chmod(perm); err != nil {
		return "", fmt.Errorf("chmod temp file: %w", err)
	}
	return file.Name(), nil
}

func (c *Cloner) ensureKnownHosts(providerID int) (strictMode string, knownHostsPath string, err error) {
	dir := filepath.Join(c.dataDir, "known_hosts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("mkdir known_hosts dir: %w", err)
	}

	if providerID <= 0 {
		providerID = 0
	}
	knownHostsPath = filepath.Join(dir, fmt.Sprintf("provider-%d", providerID))
	if _, err := os.Stat(knownHostsPath); err == nil {
		return "yes", knownHostsPath, nil
	}
	file, err := os.OpenFile(knownHostsPath, os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return "", "", fmt.Errorf("create known_hosts file: %w", err)
	}
	_ = file.Close()
	return "accept-new", knownHostsPath, nil
}
