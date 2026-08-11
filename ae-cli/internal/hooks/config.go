package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/clistate"
)

type ConfigScope string

const (
	ConfigScopeGlobal   ConfigScope = "global"
	ConfigScopeLocal    ConfigScope = "local"
	ConfigScopeWorktree ConfigScope = "worktree"
)

type HookMode string

const (
	HookModeNone        HookMode = "none"
	HookModeGitDefault  HookMode = "git_default"
	HookModeAEGlobal    HookMode = "ae_global"
	HookModeAERepo      HookMode = "ae_repo"
	HookModeNonAEGlobal HookMode = "non_ae_global"
	HookModeNonAERepo   HookMode = "non_ae_repo"
)

type EffectiveHookConfig struct {
	Mode                   HookMode
	Scope                  ConfigScope
	HooksPath              string
	DefaultHooksDir        string
	DefaultExecutableHooks []string
	LocalHooksPath         string
	WorktreeHooksPath      string
	GlobalHooksPath        string
}

func GlobalManagedHooksPath() (string, error) {
	return filepath.Abs(filepath.Join(clistate.RootDir(), "git-hooks"))
}

func RepoManagedHooksPath(gitCommonDir string) (string, error) {
	gitCommonDir = strings.TrimSpace(gitCommonDir)
	if gitCommonDir == "" {
		return "", fmt.Errorf("git common dir is required")
	}
	return filepath.Abs(filepath.Join(gitCommonDir, "ae-hooks"))
}

func InspectEffectiveHookConfig(cwd string, gitCtx *GitContext) (*EffectiveHookConfig, error) {
	if gitCtx == nil {
		var err error
		gitCtx, err = DetectGitContext(cwd)
		if err != nil {
			return nil, err
		}
	}
	localPath := gitConfigGet(cwd, "--local", "--get", "core.hooksPath")
	worktreePath := ""
	if strings.EqualFold(gitConfigGet(cwd, "--bool", "extensions.worktreeConfig"), "true") {
		worktreePath = gitConfigGet(cwd, "--worktree", "--get", "core.hooksPath")
	}
	globalPath := gitConfigGet(cwd, "--global", "--get", "core.hooksPath")
	effective := gitConfigGet(cwd, "--get", "core.hooksPath")
	scope := ConfigScope("")
	if effective != "" {
		switch effective {
		case worktreePath:
			scope = ConfigScopeWorktree
		case localPath:
			scope = ConfigScopeLocal
		case globalPath:
			scope = ConfigScopeGlobal
		default:
			scope = ConfigScopeLocal
		}
	}
	mode := HookModeNone
	if effective == "" {
		defaultExecutable := HasExecutableDefaultHook(gitCtx.DefaultHooksDir)
		if len(defaultExecutable) > 0 {
			mode = HookModeGitDefault
		}
		return &EffectiveHookConfig{
			Mode:                   mode,
			Scope:                  scope,
			DefaultHooksDir:        gitCtx.DefaultHooksDir,
			DefaultExecutableHooks: defaultExecutable,
			LocalHooksPath:         localPath,
			WorktreeHooksPath:      worktreePath,
			GlobalHooksPath:        globalPath,
		}, nil
	}
	if IsAEManagedPath(effective, gitCtx) {
		if scope == ConfigScopeGlobal {
			mode = HookModeAEGlobal
		} else {
			mode = HookModeAERepo
		}
	} else if scope == ConfigScopeGlobal {
		mode = HookModeNonAEGlobal
	} else {
		mode = HookModeNonAERepo
	}
	return &EffectiveHookConfig{
		Mode:                   mode,
		Scope:                  scope,
		HooksPath:              effective,
		DefaultHooksDir:        gitCtx.DefaultHooksDir,
		DefaultExecutableHooks: HasExecutableDefaultHook(gitCtx.DefaultHooksDir),
		LocalHooksPath:         localPath,
		WorktreeHooksPath:      worktreePath,
		GlobalHooksPath:        globalPath,
	}, nil
}

func SetGlobalHooksPath(path string) error {
	return runGitConfig("", "--global", "core.hooksPath", path)
}

func UnsetGlobalHooksPath() error {
	return runGitConfig("", "--global", "--unset-all", "core.hooksPath")
}

func SetRepoHooksPath(cwd string, scope ConfigScope, path string) error {
	if scope == ConfigScopeWorktree {
		return runGitConfig(cwd, "--worktree", "core.hooksPath", path)
	}
	return runGitConfig(cwd, "--local", "core.hooksPath", path)
}

func UnsetRepoHooksPath(cwd string, scope ConfigScope) error {
	if scope == ConfigScopeWorktree {
		return runGitConfig(cwd, "--worktree", "--unset-all", "core.hooksPath")
	}
	return runGitConfig(cwd, "--local", "--unset-all", "core.hooksPath")
}

func IsAEManagedPath(path string, gitCtx *GitContext) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	abs, err := absUnder(".", path)
	if gitCtx != nil {
		abs, err = absUnder(gitCtx.RepoRoot, path)
	}
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)
	if global, err := GlobalManagedHooksPath(); err == nil && filepath.Clean(global) == abs {
		return true
	}
	if gitCtx != nil {
		if repo, err := RepoManagedHooksPath(gitCtx.GitCommonDir); err == nil && filepath.Clean(repo) == abs {
			return true
		}
	}
	return false
}

func HasExecutableDefaultHook(dir string) []string {
	var out []string
	for _, name := range []string{"post-commit", "post-rewrite", "pre-push"} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
			continue
		}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func gitConfigGet(cwd string, args ...string) string {
	cmd := exec.Command("git", append([]string{"config"}, args...)...)
	if strings.TrimSpace(cwd) != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func runGitConfig(cwd string, args ...string) error {
	cmd := exec.Command("git", append([]string{"config"}, args...)...)
	if strings.TrimSpace(cwd) != "" {
		cmd.Dir = cwd
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(strings.Join(args, " "), "--unset-all") {
			return nil
		}
		return fmt.Errorf("git config %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
