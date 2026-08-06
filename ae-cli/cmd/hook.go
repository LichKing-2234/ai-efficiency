package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/hookstate"
	"github.com/spf13/cobra"
)

var hookCmd = &cobra.Command{
	Use:    "hook",
	Short:  "Internal git hook entrypoint (hidden)",
	Hidden: true,
}

var hookCommandTimeout = 10 * time.Second
var hookEligibilityResolveTimeout = 2 * time.Second
var runBackgroundSyncTask = hooks.RunPendingSyncTask

var newHookUploader = func() hooks.Uploader {
	if reportingConfig, ok := loadEnabledReportingConfig(); ok {
		return hooks.NewCompactBackendUploader(client.New(reportingConfig.ServerURL, reportingConfig.ReporterToken), reportingConfig.InstallationID)
	}
	if apiClient == nil {
		return hooks.UnsupportedUploader{}
	}
	return hooks.NewBackendUploader(apiClient)
}

func newHookCommandContext() (context.Context, context.CancelFunc) {
	if hookCommandTimeout <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), hookCommandTimeout)
}

var hookPostCommitCmd = &cobra.Command{
	Use:    "post-commit",
	Short:  "Handle git post-commit hook (hidden)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		ctx, cancel := newHookCommandContext()
		defer cancel()
		gitCtx, err := hooks.DetectGitContext(cwd)
		if err != nil {
			return nil
		}
		execCtx, ok := resolveHookExecutionContext(ctx, gitCtx)
		if !ok {
			queueUnresolvedPostCommit(gitCtx)
			return nil
		}
		return hooks.NewHandler(newHookUploader()).PostCommitResolved(ctx, execCtx)
	},
}

var hookPostRewriteCmd = &cobra.Command{
	Use:    "post-rewrite <rewrite_type>",
	Short:  "Handle git post-rewrite hook (hidden)",
	Hidden: true,
	Args:   cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		ctx, cancel := newHookCommandContext()
		defer cancel()
		gitCtx, err := hooks.DetectGitContext(cwd)
		if err != nil {
			return nil
		}
		execCtx, ok := resolveHookExecutionContext(ctx, gitCtx)
		if !ok {
			queueUnresolvedPostRewrite(gitCtx, args[0], os.Stdin)
			return nil
		}
		return hooks.NewHandler(newHookUploader()).PostRewriteResolved(ctx, execCtx, args[0], os.Stdin)
	},
}

var hookAttributionSyncCmd = &cobra.Command{
	Use:    "attribution-sync",
	Short:  "Run local attribution sync (hidden)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		ctx, cancel := newHookCommandContext()
		defer cancel()
		gitCtx, err := hooks.DetectGitContext(cwd)
		if err != nil {
			return nil
		}
		execCtx, ok := resolveHookExecutionContext(ctx, gitCtx)
		if !ok {
			return nil
		}
		return runBackgroundSyncTask(ctx, execCtx, newHookUploader())
	},
}

var hookBackgroundSyncCmd = &cobra.Command{
	Use:    "background-sync",
	Short:  "Run async attribution sync outside hook timeout (hidden)",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		cwd, _ := os.Getwd()
		gitCtx, err := hooks.DetectGitContext(cwd)
		if err != nil {
			return nil
		}
		execCtx, ok := resolveHookExecutionContext(context.Background(), gitCtx)
		if !ok {
			return nil
		}
		return runBackgroundSyncTask(context.Background(), execCtx, newHookUploader())
	},
}

func resolveHookExecutionContext(ctx context.Context, gitCtx *hooks.GitContext) (hooks.ExecutionContext, bool) {
	if gitCtx == nil {
		return hooks.ExecutionContext{}, false
	}
	tokenPath, _ := auth.DefaultTokenPath()
	tf := readTokenFile(tokenPath)
	reportingConfig, compactEnabled := loadEnabledReportingConfig()
	if !compactEnabled && (tf == nil || !tf.IsValid()) {
		observeHookRepo(gitCtx, hookstate.Context{RepoKey: gitCtx.RepoKey})
		return hooks.ExecutionContext{}, false
	}
	serverURL := ""
	authSubject := ""
	if compactEnabled {
		serverURL = reportingConfig.ServerURL
		authSubject = reportingConfig.AuthSubject
	} else {
		serverURL = tf.ServerURL
		authSubject = tf.StableAuthSubject()
		if cfg != nil && cfg.Server.URL != "" {
			serverURL = cfg.Server.URL
		}
	}
	binding := hookstate.Context{ServerURL: serverURL, AuthSubject: authSubject, RepoKey: gitCtx.RepoKey}
	observeHookRepo(gitCtx, binding)
	now := time.Now()
	stable := binding.Stable()
	var stalePositive hookstate.EligibilityRecord
	var hasStalePositive bool
	if stable {
		cache, err := hookstate.LoadEligibilityCache()
		if err != nil {
			return hooks.ExecutionContext{}, false
		}
		record, ok := cache.Lookup(binding, now, true)
		if ok {
			if !record.Eligible || record.RepoConfigID == 0 {
				return hooks.ExecutionContext{}, false
			}
			return executionContextFromEligibility(gitCtx, binding, record, true), true
		}
		stalePositive, hasStalePositive = cache.LookupStalePositive(binding, true)
	}

	var resolver hookRepoResolver
	if compactEnabled {
		resolver = attributionHookRepoResolverFor(serverURL, reportingConfig.ReporterToken)
	} else {
		resolverToken := ""
		if tf != nil && tf.IsValid() {
			resolverToken = tf.AccessToken
		}
		resolver = hookRepoResolverFor(serverURL, resolverToken)
	}
	if resolver == nil {
		if hasStalePositive {
			return executionContextFromEligibility(gitCtx, binding, stalePositive, true), true
		}
		return hooks.ExecutionContext{}, false
	}
	resolveCtx, cancel := context.WithTimeout(ctx, hookEligibilityResolveTimeout)
	defer cancel()
	resp, err := resolver.ResolveRepoFromRemote(resolveCtx, client.ResolveRepoRequest{
		RemoteURL:          gitCtx.RemoteURL,
		Branch:             gitCtx.Branch,
		ClientCacheVersion: client.RepoEligibilityVersion,
	})
	if err != nil || resp == nil {
		if hasStalePositive {
			return executionContextFromEligibility(gitCtx, binding, stalePositive, true), true
		}
		return hooks.ExecutionContext{}, false
	}
	if stable {
		cache, err := hookstate.LoadEligibilityCache()
		if err == nil {
			if resp.Eligible && resp.RepoConfigID > 0 {
				cache.PutPositive(binding, *resp, now)
			} else {
				reason := strings.TrimSpace(resp.Reason)
				if reason == "" {
					reason = "not_found"
				}
				cache.PutNegative(binding, gitCtx.RemoteURL, reason, now)
			}
			_ = cache.Save()
		}
	}
	if !resp.Eligible || resp.RepoConfigID == 0 {
		return hooks.ExecutionContext{}, false
	}
	record := hookstate.EligibilityRecord{
		Eligible:       true,
		ServerURL:      binding.Normalized().ServerURL,
		AuthSubject:    binding.Normalized().AuthSubject,
		RepoConfigID:   resp.RepoConfigID,
		RepoKey:        firstNonEmpty(resp.RepoKey, gitCtx.RepoKey),
		FullName:       strings.TrimSpace(resp.FullName),
		CloneURL:       strings.TrimSpace(resp.CloneURL),
		Status:         strings.TrimSpace(resp.Status),
		BindingState:   strings.TrimSpace(resp.BindingState),
		Reason:         strings.TrimSpace(resp.Reason),
		LastResolvedAt: now,
		LastObservedAt: now,
	}
	return executionContextFromEligibility(gitCtx, binding, record, stable), true
}

func queueUnresolvedPostCommit(gitCtx *hooks.GitContext) {
	if gitCtx == nil {
		return
	}
	repoRoot := strings.TrimSpace(gitCtx.RepoRoot)
	if repoRoot == "" {
		return
	}
	head, err := hooks.GitOutputForHook(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return
	}
	serverURL, authSubject := unresolvedHookBinding()
	if shouldSkipUnresolvedHookQueue(serverURL, authSubject, gitCtx.RepoKey) {
		return
	}
	ev := hooks.UnresolvedHookEvent{
		Kind:           "post-commit",
		RemoteURL:      gitCtx.RemoteURL,
		RepoKey:        gitCtx.RepoKey,
		WorkspaceID:    gitCtx.WorkspaceID,
		ServerURL:      serverURL,
		AuthSubject:    authSubject,
		CommitSHA:      head,
		ParentSHAs:     hooks.ParentSHAsForHook(repoRoot),
		BranchSnapshot: firstNonEmpty(gitCtx.Branch, hooks.BranchSnapshotForHook(repoRoot)),
		HeadSnapshot:   head,
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := hooks.EnqueueUnresolvedHookEvent(ev); err != nil {
		fmt.Fprintf(os.Stderr, "ae-cli: failed to queue unresolved checkpoint event: %v\n", err)
	}
}

func queueUnresolvedPostRewrite(gitCtx *hooks.GitContext, rewriteType string, stdin io.Reader) {
	if gitCtx == nil {
		return
	}
	rewriteType = strings.TrimSpace(rewriteType)
	if rewriteType == "" {
		return
	}
	pairs, err := hooks.ParsePostRewritePairs(stdin)
	if err != nil || len(pairs) == 0 {
		return
	}
	serverURL, authSubject := unresolvedHookBinding()
	if shouldSkipUnresolvedHookQueue(serverURL, authSubject, gitCtx.RepoKey) {
		return
	}
	capturedAt := time.Now().UTC().Format(time.RFC3339)
	for _, p := range pairs {
		ev := hooks.UnresolvedHookEvent{
			Kind:         "post-rewrite",
			RemoteURL:    gitCtx.RemoteURL,
			RepoKey:      gitCtx.RepoKey,
			WorkspaceID:  gitCtx.WorkspaceID,
			ServerURL:    serverURL,
			AuthSubject:  authSubject,
			RewriteType:  rewriteType,
			OldCommitSHA: strings.TrimSpace(p[0]),
			NewCommitSHA: strings.TrimSpace(p[1]),
			CapturedAt:   capturedAt,
		}
		if err := hooks.EnqueueUnresolvedHookEvent(ev); err != nil {
			fmt.Fprintf(os.Stderr, "ae-cli: failed to queue unresolved rewrite event: %v\n", err)
		}
	}
}

func unresolvedHookBinding() (string, string) {
	if reportingConfig, ok := loadEnabledReportingConfig(); ok {
		return reportingConfig.ServerURL, reportingConfig.AuthSubject
	}
	tokenPath, _ := auth.DefaultTokenPath()
	tf := readTokenFile(tokenPath)
	if tf == nil {
		return "", ""
	}
	serverURL := tf.ServerURL
	if cfg != nil && cfg.Server.URL != "" {
		serverURL = cfg.Server.URL
	}
	return serverURL, tf.StableAuthSubject()
}

func shouldSkipUnresolvedHookQueue(serverURL, authSubject, repoKey string) bool {
	binding := hookstate.Context{ServerURL: serverURL, AuthSubject: authSubject, RepoKey: repoKey}
	if !binding.Stable() {
		return false
	}
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		return false
	}
	record, ok := cache.Lookup(binding, time.Now(), true)
	return ok && !record.Eligible
}

type hookRepoResolver interface {
	ResolveRepoFromRemote(ctx context.Context, req client.ResolveRepoRequest) (*client.RepoEligibilityResponse, error)
}

type attributionHookRepoResolver struct {
	client *client.Client
}

func (r attributionHookRepoResolver) ResolveRepoFromRemote(ctx context.Context, req client.ResolveRepoRequest) (*client.RepoEligibilityResponse, error) {
	return r.client.ResolveAttributionRepoFromRemote(ctx, req)
}

func attributionHookRepoResolverFor(serverURL, token string) hookRepoResolver {
	if strings.TrimSpace(serverURL) == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	return attributionHookRepoResolver{client: client.New(serverURL, token)}
}

func hookRepoResolverFor(serverURL, token string) hookRepoResolver {
	if apiClient != nil && strings.TrimSpace(apiClient.BaseURL()) != "" {
		return apiClient
	}
	if strings.TrimSpace(serverURL) == "" || strings.TrimSpace(token) == "" {
		return nil
	}
	return client.New(serverURL, token)
}

func executionContextFromEligibility(gitCtx *hooks.GitContext, binding hookstate.Context, record hookstate.EligibilityRecord, durable bool) hooks.ExecutionContext {
	n := binding.Normalized()
	return hooks.ExecutionContext{
		ServerURL:     n.ServerURL,
		AuthSubject:   n.AuthSubject,
		RepoConfigID:  record.RepoConfigID,
		RepoKey:       record.RepoKey,
		RepoFullName:  record.FullName,
		WorkspaceID:   gitCtx.WorkspaceID,
		RepoRoot:      gitCtx.RepoRoot,
		Branch:        gitCtx.Branch,
		DurableReplay: durable,
	}
}

func observeHookRepo(gitCtx *hooks.GitContext, binding hookstate.Context) {
	if gitCtx == nil {
		return
	}
	observed, err := hookstate.LoadObservedRepos()
	if err != nil {
		return
	}
	if binding.RepoKey == "" {
		binding.RepoKey = gitCtx.RepoKey
	}
	observed.Observe(binding, gitCtx.RemoteURL, time.Now())
	_ = observed.Save()
}

func init() {
	hookCmd.AddCommand(hookPostCommitCmd)
	hookCmd.AddCommand(hookPostRewriteCmd)
	hookCmd.AddCommand(hookAttributionSyncCmd)
	hookCmd.AddCommand(hookBackgroundSyncCmd)
	rootCmd.AddCommand(hookCmd)
}
