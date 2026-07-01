package hooks

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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
	Binding hookstate.Context
}

type Status struct {
	GlobalEnabled           bool
	RepoEnabled             bool
	EffectiveMode           HookMode
	EffectiveScope          ConfigScope
	HooksPath               string
	TemplateVersion         int
	CurrentTemplateVersion  int
	TemplateStale           bool
	BinaryPath              string
	BinaryOverride          bool
	DefaultExecutableHooks  []string
	DefaultHooksDisposition string
	EligibilityCache        string
	ObservedRepo            string
	ContextFingerprint      string
	UploadGroups            []UploadGroup
}

type UploadGroup struct {
	ServerURL            string
	AuthSubject          string
	RepoConfigID         int
	RepoKey              string
	WorkspaceID          string
	PendingCount         int
	UploadedCount        int
	FailedCount          int
	DeferredCount        int
	SkippedCount         int
	LastSuccessfulUpload *time.Time
	LastError            string
}

type DisableRepoResult struct {
	DisabledScopes []ConfigScope
	Reconciled     bool
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
		if current.HooksPath == "" && len(current.DefaultExecutableHooks) > 0 {
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
	_, err := DisableRepoWithResult(cwd)
	return err
}

func DisableRepoWithResult(cwd string) (DisableRepoResult, error) {
	var result DisableRepoResult
	gitCtx, err := DetectGitContext(cwd)
	if err != nil {
		return result, err
	}
	registry, err := hookstate.LoadInstallations()
	if err != nil {
		return result, err
	}
	now := time.Now()
	for {
		status, err := InspectEffectiveHookConfig(cwd, gitCtx)
		if err != nil {
			return result, err
		}
		if status.Mode != HookModeAERepo || (status.Scope != ConfigScopeLocal && status.Scope != ConfigScopeWorktree) {
			break
		}
		if err := UnsetRepoHooksPath(cwd, status.Scope); err != nil {
			return result, err
		}
		result.DisabledScopes = append(result.DisabledScopes, status.Scope)
		registry.Disable(repoInstallationMatch(gitCtx, status.Scope, status.HooksPath), now)
	}
	result.Reconciled = reconcileInactiveRepoInstallations(registry, gitCtx, cwd, now)
	return result, registry.Save()
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
		GlobalEnabled:           IsAEManagedPath(cfg.GlobalHooksPath, gitCtx),
		RepoEnabled:             cfg.Mode == HookModeAERepo,
		EffectiveMode:           cfg.Mode,
		EffectiveScope:          cfg.Scope,
		HooksPath:               cfg.HooksPath,
		CurrentTemplateVersion:  hookstate.CurrentHookTemplateVersion,
		DefaultExecutableHooks:  cfg.DefaultExecutableHooks,
		DefaultHooksDisposition: defaultHooksDisposition(cfg),
		BinaryPath:              bestBinaryPath(),
		BinaryOverride:          strings.TrimSpace(os.Getenv("AE_CLI_BIN")) != "",
		EligibilityCache:        "missing",
		ObservedRepo:            "missing",
		ContextFingerprint:      contextFingerprint(opts.Binding),
	}
	status.applyCurrentHookState(opts.Binding, gitCtx)
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
	if opts.Uploads {
		status.UploadGroups = summarizeUploads(gitCtx.WorkspaceID)
	}
	return status, nil
}

func (s *Status) applyCurrentHookState(binding hookstate.Context, gitCtx *GitContext) {
	if s == nil || gitCtx == nil {
		return
	}
	binding.RepoKey = firstNonEmpty(binding.RepoKey, gitCtx.RepoKey)
	now := time.Now()
	if binding.Stable() {
		if cache, err := hookstate.LoadEligibilityCache(); err == nil {
			if rec, ok := cache.Lookup(binding, now, true); ok {
				if rec.Eligible && rec.RepoConfigID > 0 {
					s.EligibilityCache = fmt.Sprintf("eligible repo_config_id=%d", rec.RepoConfigID)
				} else {
					reason := strings.TrimSpace(rec.Reason)
					if reason == "" {
						reason = "ineligible"
					}
					s.EligibilityCache = reason
				}
			}
		}
	}
	if observed, err := hookstate.LoadObservedRepos(); err == nil {
		for _, rec := range observed.Matching(binding) {
			if strings.TrimSpace(rec.ServerURL) != "" && strings.TrimSpace(rec.AuthSubject) != "" {
				s.ObservedRepo = "bound"
			} else {
				s.ObservedRepo = "unbound"
			}
			break
		}
	}
}

func summarizeUploads(workspaceID string) []UploadGroup {
	groups := map[string]*UploadGroup{}
	for _, item := range pendingQueueItems(workspaceID) {
		ev := item.Event
		g := uploadGroupFor(groups, Binding{
			ServerURL:    ev.ServerURL,
			AuthSubject:  ev.AuthSubject,
			RepoConfigID: ev.RepoConfigID,
			RepoKey:      ev.RepoKey,
			WorkspaceID:  ev.WorkspaceID,
		})
		g.PendingCount++
	}
	records, err := ReadLedger(workspaceID)
	if err == nil {
		for _, rec := range records {
			g := uploadGroupFor(groups, Binding{
				ServerURL:    rec.ServerURL,
				AuthSubject:  rec.AuthSubject,
				RepoConfigID: rec.RepoConfigID,
				RepoKey:      rec.RepoKey,
				WorkspaceID:  rec.WorkspaceID,
			})
			switch strings.TrimSpace(rec.Status) {
			case "uploaded":
				g.UploadedCount++
				if rec.UploadedAt != nil && (g.LastSuccessfulUpload == nil || rec.UploadedAt.After(*g.LastSuccessfulUpload)) {
					t := *rec.UploadedAt
					g.LastSuccessfulUpload = &t
				}
			case "failed":
				g.FailedCount++
			case "deferred":
				g.DeferredCount++
			case "skipped":
				g.SkippedCount++
			case "pending":
				g.PendingCount++
			}
			if strings.TrimSpace(rec.LastError) != "" {
				g.LastError = strings.TrimSpace(rec.LastError)
			}
		}
	}
	out := make([]UploadGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		return uploadGroupKey(out[i]) < uploadGroupKey(out[j])
	})
	return out
}

func pendingQueueItems(workspaceID string) []QueueItem {
	p, err := workspaceQueuePath(workspaceID)
	if err != nil {
		return nil
	}
	q := &Queue{path: p}
	items, err := q.List()
	if err != nil {
		return nil
	}
	return items
}

func uploadGroupFor(groups map[string]*UploadGroup, b Binding) *UploadGroup {
	g := UploadGroup{
		ServerURL:    strings.TrimSpace(b.ServerURL),
		AuthSubject:  strings.TrimSpace(b.AuthSubject),
		RepoConfigID: b.RepoConfigID,
		RepoKey:      strings.TrimSpace(b.RepoKey),
		WorkspaceID:  strings.TrimSpace(b.WorkspaceID),
	}
	key := uploadGroupKey(g)
	if existing := groups[key]; existing != nil {
		return existing
	}
	groups[key] = &g
	return &g
}

func uploadGroupKey(g UploadGroup) string {
	return strings.Join([]string{
		g.ServerURL,
		g.AuthSubject,
		fmt.Sprintf("%d", g.RepoConfigID),
		g.RepoKey,
		g.WorkspaceID,
	}, "\x1f")
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
	observed, err := hookstate.LoadObservedRepos()
	if err != nil {
		return err
	}
	observed.Observe(binding, gitCtx.RemoteURL, now)
	if err := observed.Save(); err != nil {
		return err
	}
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
	if c == nil {
		return nil
	}
	n := binding.Normalized()
	if strings.TrimSpace(n.ServerURL) == "" || strings.TrimSpace(n.AuthSubject) == "" {
		return fmt.Errorf("server_url and auth_subject are required")
	}
	observed, err := hookstate.LoadObservedRepos()
	if err != nil {
		return err
	}
	var repos []client.HookEligibleRepoRequest
	for _, rec := range observed.Repos {
		if hookstate.NormalizeServerURL(rec.ServerURL) != n.ServerURL || strings.TrimSpace(rec.AuthSubject) != n.AuthSubject {
			continue
		}
		repoKey := strings.TrimSpace(rec.RepoKey)
		if repoKey == "" {
			continue
		}
		repos = append(repos, client.HookEligibleRepoRequest{
			RepoKey:   repoKey,
			RemoteURL: strings.TrimSpace(rec.RemoteURL),
		})
	}
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].RepoKey < repos[j].RepoKey
	})
	if len(repos) == 0 {
		return nil
	}
	resp, err := c.BatchHookEligible(ctx, repos)
	if err != nil {
		return err
	}
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		return err
	}
	now := time.Now()
	remoteByRepo := map[string]string{}
	for _, repo := range repos {
		remoteByRepo[repo.RepoKey] = repo.RemoteURL
	}
	if resp != nil {
		for _, item := range resp.Repos {
			repoKey := strings.TrimSpace(item.RepoKey)
			if repoKey == "" {
				continue
			}
			cache.PutPositive(hookstate.Context{ServerURL: n.ServerURL, AuthSubject: n.AuthSubject, RepoKey: repoKey}, item, now)
		}
		for _, item := range resp.Ineligible {
			repoKey := strings.TrimSpace(item.RepoKey)
			if repoKey == "" {
				continue
			}
			reason := strings.TrimSpace(item.Reason)
			if reason == "" {
				reason = "not_found"
			}
			cache.PutNegative(hookstate.Context{ServerURL: n.ServerURL, AuthSubject: n.AuthSubject, RepoKey: repoKey}, remoteByRepo[repoKey], reason, now)
		}
	}
	return cache.Save()
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
		if isInaccessibleRepoLocalInstallation(rec) {
			if out != nil {
				fmt.Fprintf(out, "skipped %s: repository location unavailable\n", rec.HooksPath)
			}
			continue
		}
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

func repoInstallationMatch(gitCtx *GitContext, scope ConfigScope, hooksPath string) hookstate.InstallationRecord {
	return hookstate.InstallationRecord{
		Mode:         string(scopeToMode(scope)),
		GitDir:       gitCtx.GitDir,
		GitCommonDir: gitCtx.GitCommonDir,
		ConfigScope:  string(scope),
		HooksPath:    hooksPath,
	}
}

func reconcileInactiveRepoInstallations(registry *hookstate.Installations, gitCtx *GitContext, cwd string, now time.Time) bool {
	if registry == nil || gitCtx == nil {
		return false
	}
	changed := false
	localPath := gitConfigGet(cwd, "--local", "--get", "core.hooksPath")
	worktreePath := ""
	if strings.EqualFold(gitConfigGet(cwd, "--bool", "extensions.worktreeConfig"), "true") {
		worktreePath = gitConfigGet(cwd, "--worktree", "--get", "core.hooksPath")
	}
	for _, rec := range registry.Records {
		if !rec.Enabled || rec.ConfigScope == "" || rec.HooksPath == "" {
			continue
		}
		switch rec.ConfigScope {
		case string(ConfigScopeLocal):
			if filepath.Clean(rec.GitCommonDir) != filepath.Clean(gitCtx.GitCommonDir) {
				continue
			}
			if filepath.Clean(strings.TrimSpace(localPath)) == filepath.Clean(rec.HooksPath) {
				continue
			}
			if registry.Disable(rec, now) {
				changed = true
			}
		case string(ConfigScopeWorktree):
			if filepath.Clean(rec.GitDir) != filepath.Clean(gitCtx.GitDir) {
				continue
			}
			if filepath.Clean(strings.TrimSpace(worktreePath)) == filepath.Clean(rec.HooksPath) {
				continue
			}
			if registry.Disable(rec, now) {
				changed = true
			}
		}
	}
	return changed
}

func isInaccessibleRepoLocalInstallation(rec hookstate.InstallationRecord) bool {
	switch strings.TrimSpace(rec.Mode) {
	case "global":
		return false
	case "local", "worktree":
	default:
		return false
	}
	if strings.TrimSpace(rec.GitCommonDir) == "" {
		return true
	}
	if _, err := os.Stat(rec.GitCommonDir); err != nil {
		return true
	}
	if strings.TrimSpace(rec.ConfigScope) == string(ConfigScopeWorktree) {
		if strings.TrimSpace(rec.GitDir) == "" {
			return true
		}
		if _, err := os.Stat(rec.GitDir); err != nil {
			return true
		}
	}
	return false
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

func defaultHooksDisposition(cfg *EffectiveHookConfig) string {
	if cfg == nil || len(cfg.DefaultExecutableHooks) == 0 {
		return "none"
	}
	if cfg.Mode == HookModeGitDefault {
		return "effective"
	}
	return "bypassed"
}

func contextFingerprint(ctx hookstate.Context) string {
	n := ctx.Normalized()
	if !n.Stable() {
		return "unstable"
	}
	key := n.CacheKey()
	if len(key) > 12 {
		return key[:12]
	}
	return key
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
