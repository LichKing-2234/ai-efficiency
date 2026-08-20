package hooks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
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

type v2ClaimCapableUploader interface {
	Uploader
	V2ClaimClient() attributionlocal.V2ClaimBackendClient
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

var hookStderr io.Writer = os.Stderr

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

func (h *Handler) v2ClaimClient() attributionlocal.V2ClaimBackendClient {
	if h == nil || h.uploader == nil {
		return nil
	}
	u, ok := h.uploader.(v2ClaimCapableUploader)
	if !ok {
		return nil
	}
	return u.V2ClaimClient()
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
	var snapshot *collector.Snapshot
	if h.v2ClaimClient() == nil {
		snapshot = collectSnapshotForHook(repoRoot)
		persistSnapshotCache(workspaceID, snapshot)
	}
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
		CapturedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
	lineageKind, sourceCommitSHA := commitLineageEvidence(repoRoot, head)
	if lineageKind == "cherry-pick" {
		ev.LineageKind = "cherry_pick"
		ev.SourceCommitSHA = sourceCommitSHA
		ev.CommitPatchID = attributionlocal.StableCommitPatchID(ctx, repoRoot, head)
		ev.SourcePatchID = attributionlocal.StableCommitPatchID(ctx, repoRoot, sourceCommitSHA)
		if ev.CommitPatchID == "" || ev.CommitPatchID != ev.SourcePatchID {
			ev.LineageKind, ev.SourceCommitSHA, ev.CommitPatchID, ev.SourcePatchID = "", "", "", ""
			lineageKind, sourceCommitSHA = "", ""
		}
	}
	if h == nil || h.uploader == nil {
		queueForReplayOrWarn(execCtx, ev)
	} else if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
		queueForReplayOrWarn(execCtx, ev)
	}

	h.schedulePendingSync(execCtx, &ev)
	return nil
}

func (h *Handler) schedulePendingSync(execCtx ExecutionContext, trigger *HookEvent) {
	workspaceID := strings.TrimSpace(execCtx.WorkspaceID)
	repoRoot := strings.TrimSpace(execCtx.RepoRoot)
	if workspaceID == "" || repoRoot == "" {
		return
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
	if trigger != nil {
		task.TriggerKind = strings.TrimSpace(trigger.Kind)
		task.TriggerEventID = strings.TrimSpace(trigger.EventID)
		task.TriggerCommitSHA = strings.TrimSpace(trigger.CommitSHA)
		task.TriggerBranch = strings.TrimSpace(trigger.BranchSnapshot)
		if capturedAt, err := time.Parse(time.RFC3339, trigger.CapturedAt); err == nil {
			task.LastRequestedAt = capturedAt.UTC()
		}
		v2Trigger := v2SyncTriggerFromHookEvent(*trigger)
		v2Trigger.RelayProviderID = h.relayProviderID()
		task.V2Triggers = []V2SyncTrigger{v2Trigger}
	}
	currentTask := &task
	canRunSync := h.attributionSyncClient() != nil || h.v2ClaimClient() != nil
	if err := UpsertPendingSyncTask(task); err == nil {
		if loadedTask, loadErr := LoadSyncTask(workspaceID); loadErr == nil && loadedTask != nil {
			currentTask = loadedTask
		}
		if canRunSync {
			claimSpawn, claimedTask, claimErr := TryClaimSyncTaskSpawn(workspaceID, time.Now().UTC(), syncTaskSpawnCooldown)
			if claimedTask != nil {
				currentTask = claimedTask
			}
			if claimErr != nil {
				fmt.Fprintln(hookStderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
			} else if claimSpawn {
				if err := spawnBackgroundSyncRunner(repoRoot); err != nil {
					_ = MarkSyncTaskFailure(currentTask, time.Now().UTC(), err)
					fmt.Fprintln(hookStderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
				}
			} else if strings.TrimSpace(currentTask.LastError) != "" {
				fmt.Fprintln(hookStderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
			}
		}
	} else {
		fmt.Fprintln(hookStderr, "ae-cli: attribution sync pending for this repo; run 'ae-cli doctor' for details")
	}
}

func (h *Handler) relayProviderID() int {
	if h == nil || h.uploader == nil {
		return 0
	}
	provider, ok := h.uploader.(interface{ RelayProviderID() int })
	if !ok {
		return 0
	}
	return provider.RelayProviderID()
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
			CapturedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		}
		if h == nil || h.uploader == nil {
			queueForReplayOrWarn(execCtx, ev)
			continue
		}
		if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
			queueForReplayOrWarn(execCtx, ev)
		}
	}
	h.schedulePendingSync(execCtx, nil)
	return nil
}

// PrePushResolved only wakes durable delivery and always leaves Git push
// semantics to the caller's fail-open hook script.
func (h *Handler) PrePushResolved(execCtx ExecutionContext) error {
	h.schedulePendingSync(execCtx, nil)
	return nil
}

func parseTriggerTime(raw string) time.Time {
	parsed, _ := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	return parsed.UTC()
}

func commitLineageEvidence(repoRoot, head string) (string, string) {
	reflog, err := gitOutput(repoRoot, "reflog", "-1", "--format=%gs", "HEAD")
	if err != nil || !strings.HasPrefix(strings.ToLower(strings.TrimSpace(reflog)), "cherry-pick:") {
		return "", ""
	}
	sourcePath, err := gitOutput(repoRoot, "rev-parse", "--git-path", "CHERRY_PICK_HEAD")
	if err != nil {
		return "", ""
	}
	if !filepath.IsAbs(sourcePath) {
		sourcePath = filepath.Join(repoRoot, sourcePath)
	}
	payload, err := os.ReadFile(sourcePath)
	if err != nil {
		return "", ""
	}
	source := strings.TrimSpace(string(payload))
	if source == "" || source == strings.TrimSpace(head) {
		return "", ""
	}
	verified, err := gitOutput(repoRoot, "rev-parse", "-q", "--verify", source+"^{commit}")
	if err != nil || strings.TrimSpace(verified) != source {
		return "", ""
	}
	return "cherry-pick", source
}

func (h *Handler) FlushResolved(ctx context.Context, execCtx ExecutionContext) error {
	if !execCtx.hasStableReplayBinding() {
		return nil
	}
	return h.flushWorkspace(ctx, execCtx)
}

func (h *Handler) FlushUnresolvedResolved(ctx context.Context, execCtx ExecutionContext) error {
	if !execCtx.hasStableReplayBinding() {
		return nil
	}
	var items []UnresolvedHookEvent
	if err := withUnresolvedQueueLock(func() error {
		var err error
		items, err = listUnresolvedHookEventsUnlocked()
		return err
	}); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	uploaded := map[string]bool{}
	for _, item := range items {
		if !unresolvedHookEventMatchesContext(item, execCtx) {
			continue
		}

		var ev HookEvent
		var eventID string
		switch strings.TrimSpace(item.Kind) {
		case "post-commit":
			id, err := CheckpointEventID(eventIDRepoHint(execCtx), item.CommitSHA)
			if err != nil {
				continue
			}
			eventID = id
			ev = HookEvent{
				Kind:           "post-commit",
				EventID:        eventID,
				WorkspaceID:    execCtx.WorkspaceID,
				ServerURL:      execCtx.ServerURL,
				AuthSubject:    execCtx.AuthSubject,
				RepoConfigID:   execCtx.RepoConfigID,
				RepoKey:        execCtx.RepoKey,
				RepoFullName:   firstNonEmptyValue(execCtx.RepoFullName, execCtx.RepoKey),
				BindingSource:  "unbound",
				AgentSnapshot:  item.AgentSnapshot,
				CommitSHA:      item.CommitSHA,
				ParentSHAs:     item.ParentSHAs,
				BranchSnapshot: item.BranchSnapshot,
				HeadSnapshot:   item.HeadSnapshot,
				CapturedAt:     item.CapturedAt,
			}
		case "post-rewrite":
			id, err := RewriteEventID(eventIDRepoHint(execCtx), item.OldCommitSHA, item.NewCommitSHA, item.RewriteType)
			if err != nil {
				continue
			}
			eventID = id
			ev = HookEvent{
				Kind:          "post-rewrite",
				EventID:       eventID,
				WorkspaceID:   execCtx.WorkspaceID,
				ServerURL:     execCtx.ServerURL,
				AuthSubject:   execCtx.AuthSubject,
				RepoConfigID:  execCtx.RepoConfigID,
				RepoKey:       execCtx.RepoKey,
				RepoFullName:  firstNonEmptyValue(execCtx.RepoFullName, execCtx.RepoKey),
				BindingSource: "unbound",
				RewriteType:   item.RewriteType,
				OldCommitSHA:  item.OldCommitSHA,
				NewCommitSHA:  item.NewCommitSHA,
				CapturedAt:    item.CapturedAt,
			}
		default:
			continue
		}
		if h == nil || h.uploader == nil {
			continue
		}
		if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
			continue
		}
		if h.v2ClaimClient() != nil {
			if err := AppendV2SyncTrigger(execCtx.WorkspaceID, v2SyncTriggerFromHookEvent(ev)); err != nil {
				continue
			}
		}
		uploaded[eventID] = true
	}
	if len(uploaded) == 0 {
		return nil
	}
	return withUnresolvedQueueLock(func() error {
		current, err := listUnresolvedHookEventsUnlocked()
		if err != nil {
			return err
		}
		var keep []UnresolvedHookEvent
		for _, item := range current {
			if !unresolvedHookEventMatchesContext(item, execCtx) || !uploaded[unresolvedHookEventID(item, execCtx)] {
				keep = append(keep, item)
			}
		}
		return saveUnresolvedHookEventsUnlocked(keep)
	})
}

func v2SyncTriggerFromHookEvent(ev HookEvent) V2SyncTrigger {
	return V2SyncTrigger{
		Kind: strings.TrimSpace(ev.Kind), EventID: strings.TrimSpace(ev.EventID), CommitSHA: strings.TrimSpace(ev.CommitSHA),
		Branch: strings.TrimSpace(ev.BranchSnapshot), RewriteType: strings.TrimSpace(ev.RewriteType),
		OldCommitSHA: strings.TrimSpace(ev.OldCommitSHA), NewCommitSHA: strings.TrimSpace(ev.NewCommitSHA),
		CapturedAt: parseTriggerTime(ev.CapturedAt),
	}
}

func unresolvedHookEventMatchesContext(item UnresolvedHookEvent, execCtx ExecutionContext) bool {
	if strings.TrimSpace(item.WorkspaceID) != strings.TrimSpace(execCtx.WorkspaceID) {
		return false
	}
	itemServerURL := normalizeHookServerURL(item.ServerURL)
	execServerURL := normalizeHookServerURL(execCtx.ServerURL)
	if itemServerURL == "" || itemServerURL != execServerURL {
		return false
	}
	if strings.TrimSpace(item.AuthSubject) == "" || strings.TrimSpace(item.AuthSubject) != strings.TrimSpace(execCtx.AuthSubject) {
		return false
	}
	if strings.TrimSpace(item.RepoKey) == "" || strings.TrimSpace(item.RepoKey) != strings.TrimSpace(execCtx.RepoKey) {
		return false
	}
	return true
}

func unresolvedHookEventID(item UnresolvedHookEvent, execCtx ExecutionContext) string {
	var eventID string
	var err error
	switch strings.TrimSpace(item.Kind) {
	case "post-commit":
		eventID, err = CheckpointEventID(eventIDRepoHint(execCtx), item.CommitSHA)
	case "post-rewrite":
		eventID, err = RewriteEventID(eventIDRepoHint(execCtx), item.OldCommitSHA, item.NewCommitSHA, item.RewriteType)
	default:
		return ""
	}
	if err != nil {
		return ""
	}
	return eventID
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

func GitOutputForHook(dir string, args ...string) (string, error) {
	return gitOutput(dir, args...)
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

func BranchSnapshotForHook(cwd string) string {
	return branchSnapshot(cwd)
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

func ParentSHAsForHook(cwd string) []string {
	return parentSHAs(cwd)
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

func ParsePostRewritePairs(r io.Reader) ([][2]string, error) {
	return parsePostRewritePairs(r)
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
	var items []QueueItem
	if err := q.withLock(func() error {
		var err error
		items, err = q.listUnlocked()
		return err
	}); err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}

	uploaded := map[string]bool{}
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
				Status:       "deferred",
				AttemptCount: 1,
				AttemptedAt:  now,
				LastError:    "context mismatch",
			})
			continue
		}
		if h == nil || h.uploader == nil {
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
		uploaded[it.Event.EventID] = true
	}
	if len(uploaded) == 0 {
		return nil
	}
	return q.withLock(func() error {
		current, err := q.listUnlocked()
		if err != nil {
			return err
		}
		var keep []QueueItem
		for _, it := range current {
			if !hookEventMatchesContext(it.Event, execCtx) || !uploaded[strings.TrimSpace(it.Event.EventID)] {
				keep = append(keep, it)
			}
		}
		return q.rewriteUnlocked(keep)
	})
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

func queueForReplayOrWarn(execCtx ExecutionContext, ev HookEvent) {
	if err := enqueueForReplay(execCtx, ev); err != nil {
		fmt.Fprintf(hookStderr, "ae-cli: failed to queue %s event for replay: %v\n", ledgerKind(ev.Kind), err)
	}
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
	return normalizeHookServerURL(ev.ServerURL) == normalizeHookServerURL(execCtx.ServerURL) &&
		strings.TrimSpace(ev.AuthSubject) == strings.TrimSpace(execCtx.AuthSubject) &&
		ev.RepoConfigID == execCtx.RepoConfigID &&
		strings.TrimSpace(ev.RepoKey) == strings.TrimSpace(execCtx.RepoKey) &&
		strings.TrimSpace(ev.WorkspaceID) == strings.TrimSpace(execCtx.WorkspaceID)
}

func normalizeHookServerURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return strings.TrimRight(raw, "/")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

func eventIDRepoHint(execCtx ExecutionContext) string {
	scope := strings.TrimSpace(execCtx.AuthSubject) + "\x1f" + strings.TrimSpace(execCtx.WorkspaceID)
	if execCtx.RepoConfigID > 0 {
		return fmt.Sprintf("repo_config_id:%d\x1f%s", execCtx.RepoConfigID, scope)
	}
	return firstNonEmptyValue(execCtx.RepoKey, execCtx.RepoFullName) + "\x1f" + scope
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
