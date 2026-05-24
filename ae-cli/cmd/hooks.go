package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/auth"
	"github.com/ai-efficiency/ae-cli/internal/buildinfo"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/hookstate"
	"github.com/spf13/cobra"
)

var hooksEnableGlobal bool
var hooksEnableRepo bool
var hooksEnableForce bool
var hooksDisableGlobal bool
var hooksDisableRepo bool
var hooksStatusUploads bool
var hooksRefreshCurrent bool

var hooksCmd = &cobra.Command{
	Use:   "hooks",
	Short: "Manage AE Git hooks",
}

var hooksEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable AE Git hooks",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireOneHookScope(hooksEnableGlobal, hooksEnableRepo); err != nil {
			return err
		}
		if usableToken() == "" {
			return fmt.Errorf("not logged in; run ae-cli login")
		}
		opts := hooks.InstallOptions{Force: hooksEnableForce, NonInteractive: true, GeneratorVersion: buildinfo.Version}
		if hooksEnableGlobal {
			return hooks.EnableGlobal(opts)
		}
		return hooks.EnableRepo(opts)
	},
}

var hooksDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable AE Git hooks",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := requireOneHookScope(hooksDisableGlobal, hooksDisableRepo); err != nil {
			return err
		}
		if hooksDisableGlobal {
			return hooks.DisableGlobal()
		}
		return hooks.DisableRepo(".")
	},
}

var hooksStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show AE Git hook status",
	RunE: func(cmd *cobra.Command, args []string) error {
		status, err := hooks.StatusForRepo(hooks.StatusOptions{CWD: ".", Uploads: hooksStatusUploads})
		if err != nil {
			return err
		}
		printHookStatus(cmd.OutOrStdout(), status)
		return nil
	},
}

var hooksRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh AE Git hook eligibility state",
	RunE: func(cmd *cobra.Command, args []string) error {
		if usableToken() == "" {
			return fmt.Errorf("not logged in; run ae-cli login")
		}
		binding := currentHookBinding()
		if !hooksRefreshCurrent && strings.TrimSpace(binding.AuthSubject) == "" {
			return fmt.Errorf("auth_subject is required; run ae-cli login again")
		}
		if hooksRefreshCurrent {
			return hooks.RefreshCurrent(context.Background(), apiClient, ".", binding)
		}
		return hooks.RefreshObserved(context.Background(), apiClient, binding)
	},
}

func init() {
	hooksEnableCmd.Flags().BoolVar(&hooksEnableGlobal, "global", false, "enable global managed Git hooks")
	hooksEnableCmd.Flags().BoolVar(&hooksEnableRepo, "repo", false, "enable repo-local managed Git hooks")
	hooksEnableCmd.Flags().BoolVar(&hooksEnableForce, "force", false, "overwrite the selected hooks path")
	hooksDisableCmd.Flags().BoolVar(&hooksDisableGlobal, "global", false, "disable global managed Git hooks")
	hooksDisableCmd.Flags().BoolVar(&hooksDisableRepo, "repo", false, "disable repo-local managed Git hooks")
	hooksStatusCmd.Flags().BoolVar(&hooksStatusUploads, "uploads", false, "include upload ledger summary")
	hooksRefreshCmd.Flags().BoolVar(&hooksRefreshCurrent, "current", false, "refresh current repo only")
	hooksCmd.AddCommand(hooksEnableCmd)
	hooksCmd.AddCommand(hooksDisableCmd)
	hooksCmd.AddCommand(hooksStatusCmd)
	hooksCmd.AddCommand(hooksRefreshCmd)
	rootCmd.AddCommand(hooksCmd)
}

func requireOneHookScope(global, repo bool) error {
	if global == repo {
		return fmt.Errorf("specify exactly one of --global or --repo")
	}
	return nil
}

func usableToken() string {
	configToken := ""
	if cfg != nil {
		configToken = cfg.Server.Token
	}
	return resolveToken(configToken, "")
}

func currentHookBinding() hookstate.Context {
	serverURL := ""
	if cfg != nil {
		serverURL = cfg.Server.URL
	}
	tokenPath, _ := auth.DefaultTokenPath()
	tf := readTokenFile(tokenPath)
	authSubject := ""
	if tf != nil {
		authSubject = tf.StableAuthSubject()
		if serverURL == "" {
			serverURL = tf.ServerURL
		}
	}
	repoKey := ""
	if gitCtx, err := hooks.DetectGitContext("."); err == nil {
		repoKey = gitCtx.RepoKey
	}
	return hookstate.Context{ServerURL: serverURL, AuthSubject: authSubject, RepoKey: repoKey}
}
