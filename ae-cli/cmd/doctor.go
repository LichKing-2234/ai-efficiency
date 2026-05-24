package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Inspect sessionless attribution readiness for the current repo",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, err := detectAttributionContext()
		if err != nil {
			return err
		}
		configToken := ""
		if cfg != nil {
			configToken = cfg.Server.Token
		}
		token := resolveToken(configToken, "")
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "Sessionless attribution doctor\n")
		fmt.Fprintf(out, "  Repo:          %s\n", ctx.repoRoot)
		fmt.Fprintf(out, "  Workspace ID:  %s\n", ctx.workspaceID)
		fmt.Fprintf(out, "  Git Dir:       %s\n", ctx.gitDir)
		fmt.Fprintf(out, "  Git Common:    %s\n", ctx.gitCommonDir)
		fmt.Fprintf(out, "  State Dir:     %s\n", ctx.attributionRoot)
		fmt.Fprintf(out, "  Logged In:     %t\n", token != "")
		if _, err := os.Stat(ctx.attributionRoot); err == nil {
			fmt.Fprintf(out, "  State Exists:  true\n")
		} else if os.IsNotExist(err) {
			fmt.Fprintf(out, "  State Exists:  false\n")
		} else {
			return fmt.Errorf("stat attribution state dir: %w", err)
		}
		if status, err := hooks.StatusForRepo(hooks.StatusOptions{CWD: ctx.repoRoot}); err == nil {
			printHookStatus(out, status)
		}
		printRepoEligibilityDiagnostic(out)
		return nil
	},
}

func printRepoEligibilityDiagnostic(out io.Writer) {
	gitCtx, err := hooks.DetectGitContext(".")
	if err != nil {
		fmt.Fprintf(out, "Repo Eligibility: unavailable (%v)\n", err)
		return
	}
	if apiClient == nil || strings.TrimSpace(apiClient.AuthToken()) == "" {
		fmt.Fprintf(out, "Repo Eligibility: skipped (not logged in)\n")
		return
	}
	resolveCtx, cancel := context.WithTimeout(context.Background(), hookEligibilityResolveTimeout)
	defer cancel()
	resp, err := apiClient.ResolveRepoFromRemote(resolveCtx, client.ResolveRepoRequest{
		RemoteURL:          gitCtx.RemoteURL,
		Branch:             gitCtx.Branch,
		ClientCacheVersion: client.RepoEligibilityVersion,
	})
	if err != nil {
		fmt.Fprintf(out, "Repo Eligibility: unavailable (%v)\n", err)
		return
	}
	if resp != nil && resp.Eligible && resp.RepoConfigID > 0 {
		fmt.Fprintf(out, "Repo Eligibility: eligible (repo_config_id=%d)\n", resp.RepoConfigID)
		return
	}
	reason := "not_found"
	if resp != nil && strings.TrimSpace(resp.Reason) != "" {
		reason = strings.TrimSpace(resp.Reason)
	}
	fmt.Fprintf(out, "Repo Eligibility: ineligible (%s)\n", reason)
}

func printHookStatus(out io.Writer, status *hooks.Status) {
	if status == nil {
		return
	}
	global := "disabled"
	if status.GlobalEnabled {
		global = "enabled"
	}
	repo := "disabled"
	if status.RepoEnabled {
		repo = "enabled"
	}
	template := "missing"
	if status.TemplateVersion > 0 {
		template = "current"
		if status.TemplateStale {
			template = "stale"
		}
	}
	override := "unset"
	if status.BinaryOverride {
		override = "set"
	}
	fmt.Fprintf(out, "Hook status\n")
	fmt.Fprintf(out, "  Global:        %s\n", global)
	fmt.Fprintf(out, "  Repo-local:    %s\n", repo)
	fmt.Fprintf(out, "  Effective:     %s\n", status.EffectiveMode)
	fmt.Fprintf(out, "  Binary:        %s\n", status.BinaryPath)
	fmt.Fprintf(out, "  AE_CLI_BIN:    %s\n", override)
	fmt.Fprintf(out, "  Template:      %s\n", template)
	fmt.Fprintf(out, "  Eligibility:   %s\n", status.EligibilityCache)
	if len(status.UploadGroups) > 0 {
		fmt.Fprintf(out, "Uploads:\n")
		for _, group := range status.UploadGroups {
			lastSuccess := "never"
			if group.LastSuccessfulUpload != nil {
				lastSuccess = group.LastSuccessfulUpload.UTC().Format(time.RFC3339)
			}
			lastError := group.LastError
			if strings.TrimSpace(lastError) == "" {
				lastError = "none"
			}
			fmt.Fprintf(out, "  repo_config_id=%d repo=%s workspace=%s server=%s account=%s pending=%d uploaded=%d failed=%d skipped=%d last_success=%s last_error=%s\n",
				group.RepoConfigID,
				group.RepoKey,
				group.WorkspaceID,
				group.ServerURL,
				group.AuthSubject,
				group.PendingCount,
				group.UploadedCount,
				group.FailedCount,
				group.SkippedCount,
				lastSuccess,
				lastError,
			)
		}
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
