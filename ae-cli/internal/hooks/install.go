package hooks

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func shellQuote(s string) string {
	// Minimal single-quote escaping suitable for sh.
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func canonicalPath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("abs %q: %w", p, err)
	}
	abs = filepath.Clean(abs)
	real, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Best-effort; missing paths can be fine during install.
		return abs, nil
	}
	real = filepath.Clean(real)
	real, err = filepath.Abs(real)
	if err != nil {
		return "", fmt.Errorf("abs(real) %q: %w", real, err)
	}
	return real, nil
}

func isUnderDir(parent, child string) bool {
	parent = filepath.Clean(parent)
	child = filepath.Clean(child)
	if parent == child {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func gitOutputInstall(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w (stderr=%s)", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func copyFile(dst, src string, mode os.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}

// InstallSharedHooks installs AE shared git hooks under $(git rev-parse --git-common-dir)/ae-hooks
// and points core.hooksPath to that directory.
//
// It preserves any existing hook logic by copying the legacy hook script and chaining it after the
// AE runner.
func InstallSharedHooks(cwd string, selfPath string) error {
	if strings.TrimSpace(cwd) == "" {
		return fmt.Errorf("cwd is required")
	}
	if strings.TrimSpace(selfPath) == "" {
		return fmt.Errorf("selfPath is required")
	}

	repoRoot, err := gitOutputInstall(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	gitDirAbs, err := gitOutputInstall(cwd, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	gitCommonRel, err := gitOutputInstall(cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return err
	}
	gitCommonAbs, err := absUnder(repoRoot, gitCommonRel)
	if err != nil {
		return err
	}

	sharedDir := filepath.Join(gitCommonAbs, "ae-hooks")
	sharedCanon, _ := canonicalPath(sharedDir)

	// Detect legacy hooks path.
	legacyHooksDir := ""
	hooksPath, err := gitOutputInstall(cwd, "config", "--local", "--get", "core.hooksPath")
	if err != nil {
		// Not set: fall back to default.
		legacyHooksDir = filepath.Join(gitDirAbs, "hooks")
	} else if strings.TrimSpace(hooksPath) != "" {
		abs, err := absUnder(repoRoot, hooksPath)
		if err != nil {
			return fmt.Errorf("abs legacy hooksPath: %w", err)
		}
		legacyHooksDir = abs
	}

	if legacyHooksDir != "" {
		legacyCanon, _ := canonicalPath(legacyHooksDir)
		if legacyCanon == sharedCanon {
			// Already installed.
			legacyHooksDir = ""
		} else if isUnderDir(sharedCanon, legacyCanon) {
			// Refuse to chain a legacy path that lives under our shared hook dir
			// (would self-reference or recursively take over).
			return fmt.Errorf("legacy hooks path %q is under shared hooks dir %q (recursive)", legacyHooksDir, sharedDir)
		}
	}

	if err := os.MkdirAll(sharedDir, 0o755); err != nil {
		return fmt.Errorf("creating shared hooks dir: %w", err)
	}

	for _, hookName := range []string{"post-commit", "post-rewrite"} {
		legacyCopy := filepath.Join(sharedDir, "legacy", hookName)
		legacyExists := fileExists(legacyCopy)

		// If we haven't captured a legacy hook yet, try to copy it now.
		if !legacyExists && legacyHooksDir != "" {
			legacyHook := filepath.Join(legacyHooksDir, hookName)
			if fileExists(legacyHook) {
				info, err := os.Stat(legacyHook)
				if err == nil && info.Mode().IsRegular() {
					mode := info.Mode().Perm()
					if mode == 0 {
						mode = 0o755
					}
					if err := copyFile(legacyCopy, legacyHook, mode); err != nil {
						return fmt.Errorf("copy legacy hook %s: %w", hookName, err)
					}
					_ = os.Chmod(legacyCopy, 0o755)
					legacyExists = true
				}
			}
		}

		runnerPath := filepath.Join(sharedDir, hookName)
		var b strings.Builder
		b.WriteString("#!/bin/sh\n")
		// The runner itself must not break commits. Any upload failures are handled in ae-cli.
		b.WriteString(shellQuote(selfPath) + " hook " + hookName + " \"$@\" || true\n")
		if legacyExists {
			b.WriteString("legacy=" + shellQuote(legacyCopy) + "\n")
			b.WriteString("if [ -f \"$legacy\" ]; then\n")
			b.WriteString("  if [ -x \"$legacy\" ]; then\n")
			b.WriteString("    \"$legacy\" \"$@\"\n")
			b.WriteString("  else\n")
			b.WriteString("    sh \"$legacy\" \"$@\"\n")
			b.WriteString("  fi\n")
			b.WriteString("fi\n")
		}

		if err := os.WriteFile(runnerPath, []byte(b.String()), 0o755); err != nil {
			return fmt.Errorf("writing hook %s: %w", hookName, err)
		}
	}

	// Activate shared hooks.
	cmd := exec.Command("git", "config", "core.hooksPath", sharedDir)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git config core.hooksPath: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
