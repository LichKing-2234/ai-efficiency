package hooks

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hookstate"
)

type InstallOptions struct {
	CWD              string
	Force            bool
	NonInteractive   bool
	GeneratorVersion string
}

type StatusOptions struct {
	CWD     string
	Uploads bool
}

type Status struct {
	GlobalEnabled          bool
	RepoEnabled            bool
	EffectiveMode          HookMode
	EffectiveScope         ConfigScope
	HooksPath              string
	TemplateVersion        int
	TemplateStale          bool
	BinaryPath             string
	BinaryOverride         bool
	DefaultExecutableHooks []string
	EligibilityCache       string
	ObservedRepo           string
}

type repoResolver interface {
	ResolveRepoFromRemote(ctx context.Context, req client.ResolveRepoRequest) (*client.RepoEligibilityResponse, error)
}

type batchResolver interface {
	BatchHookEligible(ctx context.Context, repos []client.HookEligibleRepoRequest) (*client.BatchHookEligibleResponse, error)
}

func EnableGlobal(opts InstallOptions) error {
	path, err := GlobalManagedHooksPath()
	if err != nil {
		return err
	}
	if !opts.Force && opts.NonInteractive {
		if existing := gitConfigGet(opts.CWD, "--global", "--get", "core.hooksPath"); existing != "" && existing != path {
			return fmt.Errorf("global core.hooksPath already set to %s; use --force to overwrite", existing)
		}
	}
	if err := WriteManagedScripts(path, opts.GeneratorVersion); err != nil {
		return err
	}
	if err := SetGlobalHooksPath(path); err != nil {
		return err
	}
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		return err
	}
	now := time.Now()
	registry.Upsert(hookstate.InstallationRecord{
		Mode:            "global",
		HooksPath:       path,
		Enabled:         true,
		TemplateVersion: hookstate.CurrentHookTemplateVersion,
		UpdatedAt:       now,
	})
	return registry.Save()
}

func EnableRepo(opts InstallOptions) error {
	gitCtx, err := DetectGitContext(opts.CWD)
	if err != nil {
		return err
	}
	managed, err := RepoManagedHooksPath(gitCtx.GitCommonDir)
	if err != nil {
		return err
	}
	current, err := InspectEffectiveHookConfig(opts.CWD, gitCtx)
	if err != nil {
		return err
	}
	if !opts.Force && opts.NonInteractive {
		if current.HooksPath != "" && !IsAEManagedPath(current.HooksPath, gitCtx) {
			return fmt.Errorf("core.hooksPath already set to %s; use --force to overwrite", current.HooksPath)
		}
		if len(current.DefaultExecutableHooks) > 0 {
			return fmt.Errorf("executable default hooks exist (%s); use --force to overwrite", strings.Join(current.DefaultExecutableHooks, ", "))
		}
	}
	if err := WriteManagedScripts(managed, opts.GeneratorVersion); err != nil {
		return err
	}
	scope := ConfigScopeLocal
	if strings.EqualFold(gitConfigGet(opts.CWD, "--bool", "extensions.worktreeConfig"), "true") {
		scope = ConfigScopeWorktree
	}
	if err := SetRepoHooksPath(opts.CWD, scope, managed); err != nil {
		return err
	}
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		return err
	}
	now := time.Now()
	registry.Upsert(hookstate.InstallationRecord{
		Mode:            string(scopeToMode(scope)),
		RepoKey:         gitCtx.RepoKey,
		GitDir:          gitCtx.GitDir,
		GitCommonDir:    gitCtx.GitCommonDir,
		ConfigScope:     string(scope),
		HooksPath:       managed,
		Enabled:         true,
		TemplateVersion: hookstate.CurrentHookTemplateVersion,
		UpdatedAt:       now,
	})
	return registry.Save()
}

func DisableGlobal() error {
	path, _ := GlobalManagedHooksPath()
	if current := gitConfigGet("", "--global", "--get", "core.hooksPath"); current != "" && filepath.Clean(current) != filepath.Clean(path) {
		return nil
	}
	if err := UnsetGlobalHooksPath(); err != nil {
		return err
	}
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		return err
	}
	registry.Disable(hookstate.InstallationRecord{Mode: "global"}, time.Now())
	return registry.Save()
}

func DisableRepo(cwd string) error {
	gitCtx, err := DetectGitContext(cwd)
	if err != nil {
		return err
	}
	status, err := InspectEffectiveHookConfig(cwd, gitCtx)
	if err != nil {
		return err
	}
	if status.Mode == HookModeAERepo {
		if err := UnsetRepoHooksPath(cwd, status.Scope); err != nil {
			return err
		}
	}
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		return err
	}
	now := time.Now()
	registry.Disable(hookstate.InstallationRecord{Mode: string(scopeToMode(status.Scope)), GitDir: gitCtx.GitDir, GitCommonDir: gitCtx.GitCommonDir, ConfigScope: string(status.Scope), HooksPath: status.HooksPath}, now)
	return registry.Save()
}

func StatusForRepo(opts StatusOptions) (*Status, error) {
	gitCtx, err := DetectGitContext(opts.CWD)
	if err != nil {
		return nil, err
	}
	cfg, err := InspectEffectiveHookConfig(opts.CWD, gitCtx)
	if err != nil {
		return nil, err
	}
	status := &Status{
		GlobalEnabled:          IsAEManagedPath(cfg.GlobalHooksPath, gitCtx),
		RepoEnabled:            cfg.Mode == HookModeAERepo,
		EffectiveMode:          cfg.Mode,
		EffectiveScope:         cfg.Scope,
		HooksPath:              cfg.HooksPath,
		DefaultExecutableHooks: cfg.DefaultExecutableHooks,
		BinaryPath:             bestBinaryPath(),
		BinaryOverride:         strings.TrimSpace(os.Getenv("AE_CLI_BIN")) != "",
		EligibilityCache:       "missing",
		ObservedRepo:           "missing",
	}
	if cfg.HooksPath != "" {
		data, err := os.ReadFile(filepath.Join(cfg.HooksPath, "post-commit"))
		if err == nil {
			if v, ok := ParseTemplateVersion(data); ok {
				status.TemplateVersion = v
				status.TemplateStale = v != hookstate.CurrentHookTemplateVersion
			} else {
				status.TemplateStale = true
			}
		} else {
			status.TemplateStale = true
		}
	}
	return status, nil
}

func RefreshCurrent(ctx context.Context, c repoResolver, cwd string, binding hookstate.Context) error {
	if c == nil {
		return nil
	}
	gitCtx, err := DetectGitContext(cwd)
	if err != nil {
		return err
	}
	resp, err := c.ResolveRepoFromRemote(ctx, client.ResolveRepoRequest{
		RemoteURL:          gitCtx.RemoteURL,
		Branch:             gitCtx.Branch,
		ClientCacheVersion: client.RepoEligibilityVersion,
	})
	if err != nil {
		return err
	}
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		return err
	}
	now := time.Now()
	binding.RepoKey = firstNonEmpty(binding.RepoKey, gitCtx.RepoKey)
	if resp != nil && resp.Eligible {
		cache.PutPositive(binding, *resp, now)
	} else {
		reason := "not_found"
		if resp != nil && strings.TrimSpace(resp.Reason) != "" {
			reason = resp.Reason
		}
		cache.PutNegative(binding, gitCtx.RemoteURL, reason, now)
	}
	return cache.Save()
}

func RefreshObserved(ctx context.Context, c batchResolver, binding hookstate.Context) error {
	_ = ctx
	_ = c
	_ = binding
	return nil
}

func RefreshManagedInstallations(generatorVersion string, out io.Writer) error {
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		return err
	}
	records := append([]hookstate.InstallationRecord(nil), registry.Records...)
	if globalPath := gitConfigGet("", "--global", "--get", "core.hooksPath"); strings.TrimSpace(globalPath) != "" {
		if managed, err := GlobalManagedHooksPath(); err == nil && filepath.Clean(globalPath) == filepath.Clean(managed) {
			records = append(records, hookstate.InstallationRecord{
				Mode:            "global",
				HooksPath:       managed,
				Enabled:         true,
				TemplateVersion: hookstate.CurrentHookTemplateVersion,
				UpdatedAt:       time.Now(),
			})
		}
	}
	seen := map[string]bool{}
	for _, rec := range records {
		if !rec.Enabled || strings.TrimSpace(rec.HooksPath) == "" {
			continue
		}
		key := filepath.Clean(rec.HooksPath)
		if seen[key] {
			continue
		}
		seen[key] = true
		if err := WriteManagedScripts(rec.HooksPath, generatorVersion); err != nil {
			return err
		}
		rec.TemplateVersion = hookstate.CurrentHookTemplateVersion
		rec.UpdatedAt = time.Now()
		registry.Upsert(rec)
		if out != nil {
			fmt.Fprintf(out, "refreshed %s\n", rec.HooksPath)
		}
	}
	return registry.Save()
}

// InstallSharedHooks is retained for existing init call sites. It now uses the
// managed repo hook contract and does not preserve or invoke previous hooks.
func InstallSharedHooks(cwd string, selfPath string) error {
	_ = selfPath
	return EnableRepo(InstallOptions{CWD: cwd, Force: true, NonInteractive: true})
}

func scopeToMode(scope ConfigScope) HookMode {
	if scope == ConfigScopeWorktree {
		return HookMode("worktree")
	}
	return HookMode("local")
}

func bestBinaryPath() string {
	if v := strings.TrimSpace(os.Getenv("AE_CLI_BIN")); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", "ae-cli")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "ae-cli"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
