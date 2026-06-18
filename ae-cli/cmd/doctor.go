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

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
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

var doctorProbeTools bool

// doctorRecentFailures controls how many recent failed Codex requests doctor
// prints. It defaults to a small, copy-pasteable summary and can be raised (or
// set to 0 to hide) via the --recent-failures flag.
var doctorRecentFailures int = 3

var recentCodexFailureSummary = attributionlocal.RecentCodexFailureSummary

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
		style := doctorOutputStyleFor(out)
		fmt.Fprintf(out, "Sessionless attribution doctor\n")
		fmt.Fprintf(out, "  Repo:          %s\n", ctx.repoRoot)
		fmt.Fprintf(out, "  Workspace ID:  %s\n", ctx.workspaceID)
		fmt.Fprintf(out, "  Git Dir:       %s\n", ctx.gitDir)
		fmt.Fprintf(out, "  Git Common:    %s\n", ctx.gitCommonDir)
		fmt.Fprintf(out, "  State Dir:     %s\n", ctx.attributionRoot)
		fmt.Fprintf(out, "  Logged In:     %s\n", formatDoctorBool(style, token != "", doctorcheck.StatusOK, "warn"))
		if _, err := os.Stat(ctx.attributionRoot); err == nil {
			fmt.Fprintf(out, "  State Exists:  %s\n", formatDoctorBool(style, true, doctorcheck.StatusOK, "warn"))
		} else if os.IsNotExist(err) {
			fmt.Fprintf(out, "  State Exists:  %s\n", formatDoctorBool(style, false, doctorcheck.StatusOK, "warn"))
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
			fmt.Fprintf(out, "Sync Task: corrupt sync task moved aside %s\n", style.badge("warn"))
		}
		if task != nil {
			var runnerRecovered bool
			task, runnerRecovered, err = hooks.RecoverInactiveSyncTaskRunner(ctx.workspaceID, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("recover inactive sync runner: %w", err)
			}
			if runnerRecovered {
				fmt.Fprintf(out, "Sync Task: inactive runner recovered %s\n", style.badge("warn"))
			}
		}
		printSyncTaskStatus(out, task)
		printToolDiagnostics(out)
		printRepoEligibilityDiagnostic(out)
		printRecentFailures(out, doctorRecentFailures)
		return nil
	},
}

// printRecentFailures renders the most recent non-2xx Codex Responses requests
// recovered from the local ~/.codex log database, including the upstream request
// identifiers (x-request-id / x-client-request-id / x-kong-request-id). The goal
// is that any user — including non-developers with no Python or other tooling —
// can copy these IDs straight out of `ae-cli doctor` when reporting a problem.
func printRecentFailures(out io.Writer, limit int) {
	style := doctorOutputStyleFor(out)
	if limit <= 0 {
		return
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(out, "Recent Codex Failures: unavailable %s (%v)\n", style.badge("warn"), err)
		return
	}
	summary, err := recentCodexFailureSummary(homeDir, limit)
	if err != nil {
		fmt.Fprintf(out, "Recent Codex Failures: unavailable %s (%v)\n", style.badge("warn"), err)
		return
	}
	if len(summary.Recent) == 0 {
		fmt.Fprintf(out, "Recent Codex Failures: none %s (no failed Codex requests in local logs)\n", style.badge(doctorcheck.StatusOK))
		return
	}
	printCodexFailureList(out, style, "Recent Codex Failures", summary.Recent, "most recent Codex request errors")
	if codexFailuresNeedRequestIDFallback(summary.Recent) {
		if len(summary.RecentWithRequestID) == 0 {
			fmt.Fprintf(out, "Recent Codex Failures With Request IDs: none %s (no failed Codex requests with upstream IDs found)\n", style.badge("warn"))
			return
		}
		printCodexFailureList(out, style, "Recent Codex Failures With Request IDs", summary.RecentWithRequestID, "most recent Codex request errors with upstream IDs")
	}
}

func printCodexFailureList(out io.Writer, style doctorOutputStyle, title string, failures []attributionlocal.CodexFailedRequest, description string) {
	fmt.Fprintf(out, "%s: %d %s (%s)\n", title, len(failures), style.badge("warn"), description)
	for i := range failures {
		f := failures[i]
		when := f.Timestamp.Local().Format("2006-01-02 15:04:05")
		fmt.Fprintf(out, "  - %s status=%d %s\n", when, f.StatusCode, strings.TrimSpace(f.StatusText))
		fmt.Fprintf(out, "      url=%s\n", f.URL)
		fmt.Fprintf(out, "      x-request-id=%s\n", failureID(f.XRequestID))
		fmt.Fprintf(out, "      x-client-request-id=%s\n", failureID(f.XClientRequestID))
		fmt.Fprintf(out, "      x-kong-request-id=%s\n", failureID(f.XKongRequestID))
	}
}

func codexFailuresNeedRequestIDFallback(failures []attributionlocal.CodexFailedRequest) bool {
	for i := range failures {
		if !failures[i].HasRequestID() {
			return true
		}
	}
	return false
}

func failureID(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(none)"
	}
	return value
}

func printToolDiagnostics(out io.Writer) {
	style := doctorOutputStyleFor(out)
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
		fmt.Fprintf(out, "  %s\n", formatDoctorConfigResult(style, &report.Results[i]))
	}
	if !doctorProbeTools {
		fmt.Fprintf(out, "Tool probe: skipped %s (use --probe-tools to run local CLI probes)\n", style.badge("warn"))
		return
	}
	fmt.Fprintf(out, "Tool probe %s\n", style.badge("running"))
	printedResults := map[string]bool{}
	probeResults := probeToolsForDoctor(context.Background(), doctorcheck.ProbeOptions{
		Timeout: time.Minute,
		Configs: report.Results,
		OnStart: func(cfg doctorcheck.ConfigResult, timeout time.Duration) {
			fmt.Fprintf(out, "  %s\n", formatDoctorProbeStart(style, cfg.Name, timeout))
		},
		OnResult: func(result doctorcheck.ProbeResult) {
			printedResults[result.Name] = true
			fmt.Fprintf(out, "  %s\n", formatDoctorProbeResult(style, result))
		},
	})
	for _, result := range probeResults {
		if !printedResults[result.Name] {
			fmt.Fprintf(out, "  %s\n", formatDoctorProbeResult(style, result))
		}
	}
}

type doctorOutputStyle struct {
	color bool
}

func doctorOutputStyleFor(out io.Writer) doctorOutputStyle {
	return doctorOutputStyle{color: doctorColorEnabledForOutput(out)}
}

func doctorColorEnabledForOutput(out io.Writer) bool {
	if strings.TrimSpace(os.Getenv("NO_COLOR")) != "" {
		return false
	}
	if colorForce := strings.TrimSpace(os.Getenv("CLICOLOR_FORCE")); colorForce != "" && colorForce != "0" {
		return true
	}
	if strings.TrimSpace(os.Getenv("CLICOLOR")) == "0" {
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func formatDoctorBool(style doctorOutputStyle, value bool, trueStatus string, falseStatus string) string {
	status := falseStatus
	if value {
		status = trueStatus
	}
	return fmt.Sprintf("%t %s", value, style.badge(status))
}

func formatDoctorConfigResult(style doctorOutputStyle, result *doctorcheck.ConfigResult) string {
	status := doctorConfigDisplayStatus(result)
	line := doctorcheck.FormatConfigResult(result)
	if result != nil && result.Status != "" && status != result.Status {
		oldPrefix := result.Name + ": " + result.Status
		if strings.HasPrefix(line, oldPrefix) {
			line = result.Name + ": " + status + strings.TrimPrefix(line, oldPrefix)
		}
	}
	return fmt.Sprintf("%s %s", style.badge(status), line)
}

func formatDoctorProbeStart(style doctorOutputStyle, name string, timeout time.Duration) string {
	return doctorcheck.RedactSecrets(fmt.Sprintf("%s %s: running timeout=%s", style.badge("running"), name, timeout))
}

func formatDoctorProbeResult(style doctorOutputStyle, result doctorcheck.ProbeResult) string {
	return fmt.Sprintf("%s %s", style.badge(result.Status), doctorcheck.FormatProbeResult(result))
}

func doctorConfigDisplayStatus(result *doctorcheck.ConfigResult) string {
	if result == nil {
		return doctorcheck.StatusSkipped
	}
	switch result.Status {
	case doctorcheck.StatusFailed, doctorcheck.StatusSkipped:
		return result.Status
	}
	if result.BaseURLStatus != "" && result.BaseURLStatus != doctorcheck.Match {
		return "warn"
	}
	if result.CredentialStatus != "" && result.CredentialStatus != doctorcheck.CredentialMatch {
		return "warn"
	}
	if result.ModelContract == doctorcheck.Mismatch {
		return "warn"
	}
	if result.SkipReason != "" || len(result.Details) > 0 {
		return "warn"
	}
	return doctorcheck.StatusOK
}

func (s doctorOutputStyle) badge(status string) string {
	label := "[" + status + "]"
	if !s.color {
		return label
	}
	switch status {
	case doctorcheck.StatusOK:
		return "\x1b[32m" + label + "\x1b[0m"
	case "warn", doctorcheck.StatusSkipped:
		return "\x1b[33m" + label + "\x1b[0m"
	case doctorcheck.StatusFailed:
		return "\x1b[31m" + label + "\x1b[0m"
	case "running":
		return "\x1b[36m" + label + "\x1b[0m"
	default:
		return label
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
	style := doctorOutputStyleFor(out)
	gitCtx, err := hooks.DetectGitContext(".")
	if err != nil {
		fmt.Fprintf(out, "Repo Eligibility: unavailable %s (%v)\n", style.badge(doctorcheck.StatusFailed), err)
		return
	}
	if apiClient == nil || strings.TrimSpace(apiClient.AuthToken()) == "" {
		fmt.Fprintf(out, "Repo Eligibility: skipped %s (not logged in)\n", style.badge("warn"))
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
			fmt.Fprintf(out, "Repo Eligibility: unavailable %s (timeout after %s)\n", style.badge(doctorcheck.StatusFailed), doctorRepoEligibilityTimeout)
			return
		}
		fmt.Fprintf(out, "Repo Eligibility: unavailable %s (%v, duration=%s)\n", style.badge(doctorcheck.StatusFailed), err, duration)
		return
	}
	if resp != nil && resp.Eligible && resp.RepoConfigID > 0 {
		fmt.Fprintf(out, "Repo Eligibility: eligible %s (repo_config_id=%d, duration=%s)\n", style.badge(doctorcheck.StatusOK), resp.RepoConfigID, duration)
		return
	}
	reason := "not_found"
	if resp != nil && strings.TrimSpace(resp.Reason) != "" {
		reason = strings.TrimSpace(resp.Reason)
	}
	fmt.Fprintf(out, "Repo Eligibility: ineligible %s (%s, duration=%s)\n", style.badge(doctorcheck.StatusFailed), reason, duration)
}

func printHookStatus(out io.Writer, status *hooks.Status) {
	if status == nil {
		return
	}
	style := doctorOutputStyleFor(out)
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
	fmt.Fprintf(out, "  Global:        %s %s\n", global, style.badge(enabledStatus(global == "enabled")))
	fmt.Fprintf(out, "  Repo-local:    %s %s\n", repo, style.badge(enabledStatus(repo == "enabled")))
	fmt.Fprintf(out, "  Effective:     %s %s\n", status.EffectiveMode, style.badge(effectiveModeStatus(string(status.EffectiveMode))))
	fmt.Fprintf(out, "  Scope:         %s\n", status.EffectiveScope)
	fmt.Fprintf(out, "  Binary:        %s\n", status.BinaryPath)
	fmt.Fprintf(out, "  AE_CLI_BIN:    %s %s\n", override, style.badge(doctorcheck.StatusOK))
	fmt.Fprintf(out, "  Template:      %s %s\n", template, style.badge(templateStatus(status.TemplateVersion, status.TemplateStale)))
	fmt.Fprintf(out, "  Context:       %s %s\n", status.ContextFingerprint, style.badge(contextStatus(status.ContextFingerprint)))
	fmt.Fprintf(out, "  Observed Repo: %s %s\n", status.ObservedRepo, style.badge(observedRepoStatus(status.ObservedRepo)))
	fmt.Fprintf(out, "  Default Hooks: %s %s\n", defaultHooks, style.badge(defaultHooksStatus(status.DefaultHooksDisposition)))
	fmt.Fprintf(out, "  Eligibility:   %s %s\n", status.EligibilityCache, style.badge(eligibilityCacheStatus(status.EligibilityCache)))
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
			fmt.Fprintf(out, "  repo_config_id=%d repo=%s workspace=%s server=%s account=%s pending=%d uploaded=%d failed=%d deferred=%d skipped=%d last_success=%s last_error=%s\n",
				group.RepoConfigID,
				group.RepoKey,
				group.WorkspaceID,
				group.ServerURL,
				group.AuthSubject,
				group.PendingCount,
				group.UploadedCount,
				group.FailedCount,
				group.DeferredCount,
				group.SkippedCount,
				lastSuccess,
				lastError,
			)
		}
	}
}

func printSyncTaskStatus(out io.Writer, task *hooks.SyncTask) {
	style := doctorOutputStyleFor(out)
	if task == nil {
		fmt.Fprintf(out, "Sync Task: none %s\n", style.badge(doctorcheck.StatusOK))
		return
	}
	fmt.Fprintf(out, "Sync Task: %s %s\n", task.Status, style.badge(syncTaskStatus(task)))
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

func enabledStatus(enabled bool) string {
	if enabled {
		return doctorcheck.StatusOK
	}
	return "warn"
}

func effectiveModeStatus(mode string) string {
	switch strings.TrimSpace(mode) {
	case "ae_global", "ae_repo":
		return doctorcheck.StatusOK
	case "":
		return "warn"
	default:
		return "warn"
	}
}

func templateStatus(version int, stale bool) string {
	if version <= 0 || stale {
		return "warn"
	}
	return doctorcheck.StatusOK
}

func contextStatus(value string) string {
	switch strings.TrimSpace(value) {
	case "", "unstable":
		return "warn"
	default:
		return doctorcheck.StatusOK
	}
}

func observedRepoStatus(value string) string {
	switch strings.TrimSpace(value) {
	case "bound":
		return doctorcheck.StatusOK
	case "":
		return "warn"
	default:
		return "warn"
	}
}

func defaultHooksStatus(disposition string) string {
	switch strings.TrimSpace(disposition) {
	case "", "none", "bypassed":
		return doctorcheck.StatusOK
	default:
		return "warn"
	}
}

func eligibilityCacheStatus(value string) string {
	trimmed := strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(trimmed, "eligible"):
		return doctorcheck.StatusOK
	case trimmed == "", trimmed == "missing":
		return "warn"
	default:
		return "warn"
	}
}

func syncTaskStatus(task *hooks.SyncTask) string {
	if task == nil {
		return doctorcheck.StatusOK
	}
	if strings.TrimSpace(task.LastError) != "" {
		return doctorcheck.StatusFailed
	}
	switch task.Status {
	case hooks.SyncTaskStatusRunning, hooks.SyncTaskStatusPending:
		return "warn"
	default:
		return "warn"
	}
}

func init() {
	doctorCmd.Flags().BoolVar(&doctorProbeTools, "probe-tools", false, "run local Codex, Claude, and Gemini CLI probes")
	doctorCmd.Flags().IntVar(&doctorRecentFailures, "recent-failures", doctorRecentFailures, "number of recent failed Codex requests to show (0 hides the section)")
	rootCmd.AddCommand(doctorCmd)
}
