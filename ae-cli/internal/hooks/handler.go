package hooks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/collector"
)

type Uploader interface {
	UploadHookEvent(ctx context.Context, ev HookEvent) error
}

type syncCapableUploader interface {
	Uploader
	ToolUsageClient() attributionlocal.BackendClient
}

type Handler struct {
	uploader Uploader
}

type ExecutionContext struct {
	ServerURL     string
	AuthSubject   string
	RepoConfigID  int
	RepoKey       string
	RepoFullName  string
	WorkspaceID   string
	RepoRoot      string
	Branch        string
	DurableReplay bool
}

func NewHandler(u Uploader) *Handler {
	return &Handler{uploader: u}
}

// UnsupportedUploader is a placeholder until the backend exposes a commit-checkpoint API.
// The hook pipeline is fail-open and will queue events when this uploader returns an error.
type UnsupportedUploader struct{}

func (u UnsupportedUploader) UploadHookEvent(ctx context.Context, ev HookEvent) error {
	return fmt.Errorf("hook upload not implemented")
}

var runAttributionSync = func(ctx context.Context, opts attributionlocal.RunOptions, syncClient attributionlocal.BackendClient) error {
	engine := attributionlocal.NewSyncEngine(syncClient)
	return engine.Run(ctx, opts)
}

func (h *Handler) attributionSyncClient() attributionlocal.BackendClient {
	if h == nil || h.uploader == nil {
		return nil
	}
	u, ok := h.uploader.(syncCapableUploader)
	if !ok {
		return nil
	}
	return u.ToolUsageClient()
}

func (h *Handler) PostCommitResolved(ctx context.Context, execCtx ExecutionContext) error {
	repoRoot := strings.TrimSpace(execCtx.RepoRoot)
	if repoRoot == "" {
		return nil
	}
	head, err := gitOutput(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}
	workspaceID := strings.TrimSpace(execCtx.WorkspaceID)
	if workspaceID == "" {
		return nil
	}
	snapshot := collectSnapshotForHook(repoRoot)
	persistSnapshotCache(workspaceID, snapshot)
	repoHint := firstNonEmptyValue(execCtx.RepoFullName, execCtx.RepoKey)
	eventID, err := CheckpointEventID(eventIDRepoHint(execCtx), head)
	if err != nil {
		return nil
	}
	ev := HookEvent{
		Kind:           "post-commit",
		EventID:        eventID,
		WorkspaceID:    workspaceID,
		ServerURL:      execCtx.ServerURL,
		AuthSubject:    execCtx.AuthSubject,
		RepoConfigID:   execCtx.RepoConfigID,
		RepoKey:        execCtx.RepoKey,
		RepoFullName:   repoHint,
		BindingSource:  "unbound",
		AgentSnapshot:  snapshotPayload(snapshot),
		CommitSHA:      head,
		ParentSHAs:     parentSHAs(repoRoot),
		BranchSnapshot: firstNonEmptyValue(execCtx.Branch, branchSnapshot(repoRoot)),
		HeadSnapshot:   head,
		CapturedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if h == nil || h.uploader == nil {
		_ = enqueueForReplay(execCtx, ev)
	} else if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
		_ = enqueueForReplay(execCtx, ev)
	}

	task := SyncTask{
		WorkspaceID:     workspaceID,
		RepoRoot:        repoRoot,
		ServerURL:       execCtx.ServerURL,
		AuthSubject:     execCtx.AuthSubject,
		RepoConfigID:    execCtx.RepoConfigID,
		RepoKey:         execCtx.RepoKey,
		Status:          SyncTaskStatusPending,
		LastRequestedAt: time.Now().UTC(),
	}
	currentTask := &task
	syncClient := h.attributionSyncClient()
	if err := UpsertPendingSyncTask(task); err == nil {
		if loadedTask, loadErr := LoadSyncTask(workspaceID); loadErr == nil && loadedTask != nil {
			currentTask = loadedTask
		}
		if syncClient != nil {
			claimSpawn, claimedTask, claimErr := TryClaimSyncTaskSpawn(workspaceID, time.Now().UTC(), syncTaskSpawnCooldown)
			if claimedTask != nil {
				currentTask = claimedTask
			}
			if claimErr != nil {
				fmt.Fprintln(os.Stderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
			} else if claimSpawn {
				if err := spawnBackgroundSyncRunner(repoRoot); err != nil {
					_ = MarkSyncTaskFailure(currentTask, time.Now().UTC(), err)
					fmt.Fprintln(os.Stderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
				}
			} else if strings.TrimSpace(currentTask.LastError) != "" {
				fmt.Fprintln(os.Stderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
	}
	return nil
}

func (h *Handler) PostRewriteResolved(ctx context.Context, execCtx ExecutionContext, rewriteType string, stdin io.Reader) error {
	rewriteType = strings.TrimSpace(rewriteType)
	repoRoot := strings.TrimSpace(execCtx.RepoRoot)
	workspaceID := strings.TrimSpace(execCtx.WorkspaceID)
	if repoRoot == "" || workspaceID == "" || rewriteType == "" {
		return nil
	}
	pairs, err := parsePostRewritePairs(stdin)
	if err != nil || len(pairs) == 0 {
		return nil
	}
	repoHint := firstNonEmptyValue(execCtx.RepoFullName, execCtx.RepoKey)
	eventScope := eventIDRepoHint(execCtx)
	for _, p := range pairs {
		oldSHA := strings.TrimSpace(p[0])
		newSHA := strings.TrimSpace(p[1])
		eid, err := RewriteEventID(eventScope, oldSHA, newSHA, rewriteType)
		if err != nil {
			continue
		}
		ev := HookEvent{
			Kind:          "post-rewrite",
			EventID:       eid,
			WorkspaceID:   workspaceID,
			ServerURL:     execCtx.ServerURL,
			AuthSubject:   execCtx.AuthSubject,
			RepoConfigID:  execCtx.RepoConfigID,
			RepoKey:       execCtx.RepoKey,
			RepoFullName:  repoHint,
			BindingSource: "unbound",
			RewriteType:   rewriteType,
			OldCommitSHA:  oldSHA,
			NewCommitSHA:  newSHA,
			CapturedAt:    time.Now().UTC().Format(time.RFC3339),
		}
		if h == nil || h.uploader == nil {
			_ = enqueueForReplay(execCtx, ev)
			continue
		}
		if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
			_ = enqueueForReplay(execCtx, ev)
		}
	}
	return nil
}

func (h *Handler) FlushResolved(ctx context.Context, execCtx ExecutionContext) error {
	if !execCtx.hasStableReplayBinding() {
		return nil
	}
	return h.flushWorkspace(ctx, execCtx)
}

func gitOutput(dir string, args ...string) (string, error) {
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

func absUnder(root, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is empty")
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	return filepath.Abs(filepath.Join(root, p))
}

func collectSnapshotForHook(workspaceRoot string) *collector.Snapshot {
	snapshot, err := collector.BuildSnapshot(collector.DefaultPaths(workspaceRoot))
	if err != nil || snapshot == nil {
		return nil
	}
	return snapshot
}

func persistSnapshotCache(workspaceID string, snapshot *collector.Snapshot) {
	if snapshot == nil {
		return
	}
	if strings.TrimSpace(workspaceID) != "" {
		_ = collector.WriteWorkspaceCache(workspaceID, snapshot)
	}
}

func snapshotPayload(snapshot *collector.Snapshot) map[string]any {
	if snapshot == nil {
		return nil
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}
	return payload
}

func branchSnapshot(cwd string) string {
	branch, err := gitOutput(cwd, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(branch)
}

func parentSHAs(cwd string) []string {
	line, err := gitOutput(cwd, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil {
		return nil
	}
	fields := strings.Fields(line)
	if len(fields) <= 1 {
		return nil
	}
	return fields[1:]
}

func (h *Handler) PostCommit(ctx context.Context, cwd string) error {
	gitCtx, err := DetectGitContext(cwd)
	if err != nil {
		return nil
	}
	return h.PostCommitResolved(ctx, executionContextFromGit(gitCtx))
}

func parsePostRewritePairs(r io.Reader) ([][2]string, error) {
	if r == nil {
		return nil, fmt.Errorf("stdin is nil")
	}
	sc := bufio.NewScanner(r)
	var out [][2]string
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid rewrite line: %q", line)
		}
		out = append(out, [2]string{fields[0], fields[1]})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("scan stdin: %w", err)
	}
	return out, nil
}

func (h *Handler) PostRewrite(ctx context.Context, cwd string, rewriteType string, stdin io.Reader) error {
	gitCtx, err := DetectGitContext(cwd)
	if err != nil {
		return nil
	}
	return h.PostRewriteResolved(ctx, executionContextFromGit(gitCtx), rewriteType, stdin)
}

func (h *Handler) Flush(ctx context.Context, cwd string) error {
	gitCtx, err := DetectGitContext(cwd)
	if err != nil {
		return nil
	}
	return h.FlushResolved(ctx, executionContextFromGit(gitCtx))
}

func (h *Handler) flushWorkspace(ctx context.Context, execCtx ExecutionContext) error {
	q, err := NewWorkspaceQueue(execCtx.WorkspaceID)
	if err != nil {
		return err
	}
	items, err := q.List()
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	var keep []QueueItem
	for _, it := range items {
		now := time.Now().UTC()
		if !hookEventMatchesContext(it.Event, execCtx) {
			_ = AppendLedger(execCtx.WorkspaceID, LedgerRecord{
				Kind:         ledgerKind(it.Event.Kind),
				DedupeKey:    it.Event.EventID,
				ServerURL:    execCtx.ServerURL,
				AuthSubject:  execCtx.AuthSubject,
				RepoConfigID: execCtx.RepoConfigID,
				RepoKey:      execCtx.RepoKey,
				WorkspaceID:  execCtx.WorkspaceID,
				Status:       "skipped",
				AttemptCount: 1,
				AttemptedAt:  now,
				LastError:    "context mismatch",
			})
			continue
		}
		if h == nil || h.uploader == nil {
			keep = append(keep, it)
			continue
		}
		if err := h.uploader.UploadHookEvent(ctx, it.Event); err != nil {
			_ = AppendLedger(execCtx.WorkspaceID, LedgerRecord{
				Kind:         ledgerKind(it.Event.Kind),
				DedupeKey:    it.Event.EventID,
				ServerURL:    execCtx.ServerURL,
				AuthSubject:  execCtx.AuthSubject,
				RepoConfigID: execCtx.RepoConfigID,
				RepoKey:      execCtx.RepoKey,
				WorkspaceID:  execCtx.WorkspaceID,
				Status:       "failed",
				AttemptCount: 1,
				AttemptedAt:  now,
				LastError:    err.Error(),
			})
			keep = append(keep, it)
			continue
		}
		_ = AppendLedger(execCtx.WorkspaceID, LedgerRecord{
			Kind:         ledgerKind(it.Event.Kind),
			DedupeKey:    it.Event.EventID,
			ServerURL:    execCtx.ServerURL,
			AuthSubject:  execCtx.AuthSubject,
			RepoConfigID: execCtx.RepoConfigID,
			RepoKey:      execCtx.RepoKey,
			WorkspaceID:  execCtx.WorkspaceID,
			Status:       "uploaded",
			AttemptCount: 1,
			AttemptedAt:  now,
			UploadedAt:   &now,
		})
	}
	return q.rewrite(keep)
}

func enqueueForReplay(execCtx ExecutionContext, ev HookEvent) error {
	if !execCtx.hasStableReplayBinding() {
		return nil
	}
	q, err := NewWorkspaceQueue(execCtx.WorkspaceID)
	if err != nil {
		return err
	}
	return q.Enqueue(ev)
}

func (c ExecutionContext) hasStableReplayBinding() bool {
	return c.DurableReplay &&
		strings.TrimSpace(c.ServerURL) != "" &&
		strings.TrimSpace(c.AuthSubject) != "" &&
		c.RepoConfigID > 0 &&
		strings.TrimSpace(c.RepoKey) != "" &&
		strings.TrimSpace(c.WorkspaceID) != ""
}

func hookEventMatchesContext(ev HookEvent, execCtx ExecutionContext) bool {
	return strings.TrimSpace(ev.ServerURL) == strings.TrimSpace(execCtx.ServerURL) &&
		strings.TrimSpace(ev.AuthSubject) == strings.TrimSpace(execCtx.AuthSubject) &&
		ev.RepoConfigID == execCtx.RepoConfigID &&
		strings.TrimSpace(ev.RepoKey) == strings.TrimSpace(execCtx.RepoKey) &&
		strings.TrimSpace(ev.WorkspaceID) == strings.TrimSpace(execCtx.WorkspaceID)
}

func eventIDRepoHint(execCtx ExecutionContext) string {
	if execCtx.RepoConfigID > 0 {
		return fmt.Sprintf("repo_config_id:%d", execCtx.RepoConfigID)
	}
	return firstNonEmptyValue(execCtx.RepoKey, execCtx.RepoFullName)
}

func executionContextFromGit(gitCtx *GitContext) ExecutionContext {
	if gitCtx == nil {
		return ExecutionContext{}
	}
	return ExecutionContext{
		RepoKey:      gitCtx.RepoKey,
		RepoFullName: firstNonEmptyValue(gitCtx.RepoKey, gitCtx.RemoteURL),
		WorkspaceID:  gitCtx.WorkspaceID,
		RepoRoot:     gitCtx.RepoRoot,
		Branch:       gitCtx.Branch,
	}
}

func ledgerKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case "post-commit":
		return "checkpoint"
	case "post-rewrite":
		return "rewrite"
	default:
		return kind
	}
}

func firstNonEmptyValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
