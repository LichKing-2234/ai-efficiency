package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/hookstate"
	"github.com/ai-efficiency/ae-cli/internal/repolink"
	"github.com/spf13/cobra"
)

var enableRepoHooks = hooks.EnableRepo
var enableGlobalHooks = hooks.EnableGlobal
var initHooksMode string
var initForce bool

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize sessionless attribution for the current repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := detectAttributionContext()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(ctx.attributionRoot, 0o700); err != nil {
			return fmt.Errorf("create attribution state dir: %w", err)
		}
		repoLinkStatus := "skipped"
		configToken := ""
		if cfg != nil {
			configToken = cfg.Server.Token
		}
		if resolveToken(configToken, "") != "" {
			status, ensureResp, linkErr := repolink.EnsureWithResponse(context.Background(), apiClient, gitRemoteURLForCutover(), gitBranchForCutover())
			repoLinkStatus = status
			if linkErr != nil {
				repoLinkStatus = "failed"
			} else if ensureResp != nil {
				recordInitHookState(ctx.repoRoot, ensureResp)
			}
		}
		hookStatus := "none"
		switch strings.ToLower(strings.TrimSpace(initHooksMode)) {
		case "", "none":
			hookStatus = "none"
		case "repo":
			if err := enableRepoHooks(hooks.InstallOptions{CWD: ctx.repoRoot, Force: initForce, NonInteractive: true, GeneratorVersion: buildinfo.Version}); err != nil {
				return fmt.Errorf("enable repo hooks: %w", err)
			}
			hookStatus = "repo"
		case "global":
			if err := enableGlobalHooks(hooks.InstallOptions{CWD: ctx.repoRoot, Force: initForce, NonInteractive: true, GeneratorVersion: buildinfo.Version}); err != nil {
				return fmt.Errorf("enable global hooks: %w", err)
			}
			hookStatus = "global"
		default:
			return fmt.Errorf("invalid --hooks %q: expected none, repo, or global", initHooksMode)
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Initialized sessionless attribution.\n")
		fmt.Fprintf(out, "  Repo:          %s\n", ctx.repoRoot)
		fmt.Fprintf(out, "  Workspace ID:  %s\n", ctx.workspaceID)
		fmt.Fprintf(out, "  State Dir:     %s\n", ctx.attributionRoot)
		fmt.Fprintf(out, "  Repo Link:     %s\n", repoLinkStatus)
		fmt.Fprintf(out, "  Hooks:         %s\n", hookStatus)
		return nil
	},
}

func init() {
	initCmd.Flags().StringVar(&initHooksMode, "hooks", "none", "hook mode: none, repo, or global")
	initCmd.Flags().BoolVar(&initForce, "force", false, "overwrite existing hook path when enabling hooks")
	rootCmd.AddCommand(initCmd)
}

func recordInitHookState(repoRoot string, ensureResp *client.RepoEnsureResponse) {
	if ensureResp == nil {
		return
	}
	gitCtx, err := hooks.DetectGitContext(repoRoot)
	if err != nil {
		return
	}
	binding := currentHookBinding()
	binding.RepoKey = firstNonEmpty(binding.RepoKey, ensureResp.RepoKey, gitCtx.RepoKey)
	if !binding.Stable() {
		return
	}
	now := time.Now()
	observed, err := hookstate.LoadObservedRepos()
	if err == nil {
		observed.Observe(binding, gitCtx.RemoteURL, now)
		_ = observed.Save()
	}
	cache, err := hookstate.LoadEligibilityCache()
	if err != nil {
		return
	}
	cache.PutPositive(binding, client.RepoEligibilityResponse{
		Eligible:     true,
		RepoConfigID: ensureResp.ID,
		RepoKey:      firstNonEmpty(ensureResp.RepoKey, gitCtx.RepoKey),
		FullName:     ensureResp.FullName,
		CloneURL:     ensureResp.CloneURL,
		Status:       "active",
		BindingState: ensureResp.BindingState,
	}, now)
	_ = cache.Save()
}
