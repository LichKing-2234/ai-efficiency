package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/doctorcheck"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/spf13/cobra"
)

var doctorToolNames = []string{"codex", "claude", "gemini"}

var listProvidersForDoctor = func(ctx context.Context) ([]client.ProviderInfo, string, error) {
	if apiClient == nil {
		return nil, "", fmt.Errorf("API client is not configured")
	}
	providers, err := apiClient.ListProviders(ctx)
	return providers, "user/providers", err
}

var detectToolsForDoctor = detectDoctorTools

var probeToolsForDoctor = doctorcheck.ProbeTools

var doctorRepoEligibilityTimeout = 10 * time.Second

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
		if status, err := hooks.StatusForRepo(hooks.StatusOptions{CWD: ctx.repoRoot, Binding: currentHookBinding()}); err == nil {
			printHookStatus(out, status)
		}
		task, recovered, err := hooks.LoadSyncTaskRecovering(ctx.workspaceID)
		if err != nil {
			return fmt.Errorf("load sync task: %w", err)
		}
		if recovered {
			fmt.Fprintln(out, "Sync Task: corrupt sync task moved aside")
		}
		if task != nil {
			var runnerRecovered bool
			task, runnerRecovered, err = hooks.RecoverInactiveSyncTaskRunner(ctx.workspaceID, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("recover inactive sync runner: %w", err)
			}
			if runnerRecovered {
				fmt.Fprintln(out, "Sync Task: inactive runner recovered")
			}
		}
		printSyncTaskStatus(out, task)
		printToolDiagnostics(out)
		printRepoEligibilityDiagnostic(out)
		return nil
	},
}

func printToolDiagnostics(out io.Writer) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(out, "Tool configuration\n  provider: unavailable (%v)\n", err)
		return
	}
	providers, providerSource, providerErr := listProvidersForDoctor(context.Background())
	providerAvailable := providerErr == nil && len(providers) > 0
	var selected toolconfig.Provider
	if providerAvailable {
		selected, err = toolconfig.SelectProvider(mapProviders(providers), "")
		if err != nil {
			providerAvailable = false
			providerErr = err
		}
	}
	tools, err := detectToolsForDoctor(doctorToolNames)
	if err != nil {
		fmt.Fprintf(out, "Tool configuration\n  provider: unavailable (%v)\n", err)
		return
	}
	report := doctorcheck.ValidateTools(doctorcheck.ValidateOptions{
		HomeDir:           homeDir,
		ShellPath:         os.Getenv("SHELL"),
		Provider:          selected,
		ProviderAvailable: providerAvailable,
		ProviderSource:    providerSource,
		Tools:             tools,
	})
	fmt.Fprintln(out, "Tool configuration")
	if providerAvailable {
		fmt.Fprintf(out, "  provider: %s source=%s\n", report.ProviderName, report.ProviderSource)
	} else {
		fmt.Fprintf(out, "  provider: unavailable (%v)\n", providerErr)
	}
	for i := range report.Results {
		fmt.Fprintf(out, "  %s\n", doctorcheck.FormatConfigResult(&report.Results[i]))
	}
	fmt.Fprintln(out, "Tool probe")
	probeResults := probeToolsForDoctor(context.Background(), doctorcheck.ProbeOptions{
		Timeout: time.Minute,
		Configs: report.Results,
	})
	for _, result := range probeResults {
		fmt.Fprintf(out, "  %s\n", doctorcheck.FormatProbeResult(result))
	}
}

func detectDoctorTools(toolNames []string) ([]doctorcheck.ToolState, error) {
	installed, err := toolconfig.DetectInstalledTools(toolNames)
	if err != nil {
		return nil, err
	}
	byName := map[string]toolconfig.InstalledTool{}
	for _, item := range installed {
		byName[item.Name] = item
	}
	out := make([]doctorcheck.ToolState, 0, len(toolNames))
	for _, name := range toolNames {
		item, ok := byName[name]
		if !ok {
			out = append(out, doctorcheck.ToolState{Name: name, Missing: true})
			continue
		}
		probeable := true
		if strings.HasSuffix(item.Path, ".app") {
			probeable = false
		}
		out = append(out, doctorcheck.ToolState{
			Name:           name,
			ExecutablePath: item.Path,
			Version:        doctorToolVersion(item.Path),
			Probeable:      probeable,
		})
	}
	return out, nil
}

func doctorToolVersion(path string) string {
	if strings.TrimSpace(path) == "" || strings.HasSuffix(path, ".app") {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
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
	started := time.Now()
	resolveCtx, cancel := context.WithTimeout(context.Background(), doctorRepoEligibilityTimeout)
	defer cancel()
	resp, err := apiClient.ResolveRepoFromRemote(resolveCtx, client.ResolveRepoRequest{
		RemoteURL:          gitCtx.RemoteURL,
		Branch:             gitCtx.Branch,
		ClientCacheVersion: client.RepoEligibilityVersion,
	})
	duration := time.Since(started).Round(time.Millisecond)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			fmt.Fprintf(out, "Repo Eligibility: unavailable (timeout after %s)\n", doctorRepoEligibilityTimeout)
			return
		}
		fmt.Fprintf(out, "Repo Eligibility: unavailable (%v, duration=%s)\n", err, duration)
		return
	}
	if resp != nil && resp.Eligible && resp.RepoConfigID > 0 {
		fmt.Fprintf(out, "Repo Eligibility: eligible (repo_config_id=%d, duration=%s)\n", resp.RepoConfigID, duration)
		return
	}
	reason := "not_found"
	if resp != nil && strings.TrimSpace(resp.Reason) != "" {
		reason = strings.TrimSpace(resp.Reason)
	}
	fmt.Fprintf(out, "Repo Eligibility: ineligible (%s, duration=%s)\n", reason, duration)
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
		template = fmt.Sprintf("%s (installed=%d current=%d)", template, status.TemplateVersion, status.CurrentTemplateVersion)
	}
	override := "unset"
	if status.BinaryOverride {
		override = "set"
	}
	defaultHooks := "none"
	if len(status.DefaultExecutableHooks) > 0 {
		defaultHooks = fmt.Sprintf("%s (%s)", status.DefaultHooksDisposition, strings.Join(status.DefaultExecutableHooks, ", "))
	}
	fmt.Fprintf(out, "Hook status\n")
	fmt.Fprintf(out, "  Global:        %s\n", global)
	fmt.Fprintf(out, "  Repo-local:    %s\n", repo)
	fmt.Fprintf(out, "  Effective:     %s\n", status.EffectiveMode)
	fmt.Fprintf(out, "  Scope:         %s\n", status.EffectiveScope)
	fmt.Fprintf(out, "  Binary:        %s\n", status.BinaryPath)
	fmt.Fprintf(out, "  AE_CLI_BIN:    %s\n", override)
	fmt.Fprintf(out, "  Template:      %s\n", template)
	fmt.Fprintf(out, "  Context:       %s\n", status.ContextFingerprint)
	fmt.Fprintf(out, "  Observed Repo: %s\n", status.ObservedRepo)
	fmt.Fprintf(out, "  Default Hooks: %s\n", defaultHooks)
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

func printSyncTaskStatus(out io.Writer, task *hooks.SyncTask) {
	if task == nil {
		fmt.Fprintln(out, "Sync Task: none")
		return
	}
	fmt.Fprintf(out, "Sync Task: %s\n", task.Status)
	fmt.Fprintf(out, "  last_requested_at: %s\n", task.LastRequestedAt.UTC().Format(time.RFC3339))
	if task.LastStartedAt != nil {
		fmt.Fprintf(out, "  last_started_at: %s\n", task.LastStartedAt.UTC().Format(time.RFC3339))
	}
	if task.LastCompletedAt != nil {
		fmt.Fprintf(out, "  last_completed_at: %s\n", task.LastCompletedAt.UTC().Format(time.RFC3339))
	}
	fmt.Fprintf(out, "  attempt_count: %d\n", task.AttemptCount)
	if task.RunnerPID != 0 {
		fmt.Fprintf(out, "  runner_pid: %d\n", task.RunnerPID)
	}
	if strings.TrimSpace(task.LastError) != "" {
		fmt.Fprintf(out, "  last_error: %s\n", task.LastError)
	}
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}
