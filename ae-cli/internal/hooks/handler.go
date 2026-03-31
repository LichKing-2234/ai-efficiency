package hooks

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ai-efficiency/ae-cli/internal/session"
)

type Uploader interface {
	UploadHookEvent(ctx context.Context, ev HookEvent) error
}

type Handler struct {
	uploader Uploader
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

func repoEventHint(cwd string, m *session.Marker) string {
	if m != nil && strings.TrimSpace(m.RepoFullName) != "" {
		return strings.TrimSpace(m.RepoFullName)
	}
	remote, err := gitOutput(cwd, "config", "--get", "remote.origin.url")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(remote)
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

func ensureGitInfoExcludeHas(gitDirAbs string, pattern string) error {
	gitDirAbs = strings.TrimSpace(gitDirAbs)
	pattern = strings.TrimSpace(pattern)
	if gitDirAbs == "" {
		return fmt.Errorf("git dir is empty")
	}
	if pattern == "" {
		return fmt.Errorf("pattern is empty")
	}

	excludePath := filepath.Join(gitDirAbs, "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(excludePath), 0o755); err != nil {
		return fmt.Errorf("creating exclude dir: %w", err)
	}

	var existing []byte
	mode := os.FileMode(0o644)
	if info, err := os.Stat(excludePath); err == nil {
		mode = info.Mode().Perm()
		if b, err := os.ReadFile(excludePath); err == nil {
			existing = b
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat exclude: %w", err)
	}

	lines := strings.Split(string(existing), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == pattern {
			return nil
		}
	}

	out := string(existing)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += pattern + "\n"

	if err := os.WriteFile(excludePath, []byte(out), mode); err != nil {
		return fmt.Errorf("writing exclude: %w", err)
	}
	return nil
}

func (h *Handler) PostCommit(ctx context.Context, cwd string) error {
	repoRoot, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	head, err := gitOutput(cwd, "rev-parse", "HEAD")
	if err != nil {
		return nil
	}

	m, err := session.ReadMarker(repoRoot)
	if err != nil {
		if os.IsNotExist(err) {
			m, err = h.bootstrapMarkerFromEnv(repoRoot, cwd, head)
			if err != nil {
				// Env bootstrap is best-effort; a failure here should not block commits.
				m = nil
			}
		} else {
			// Marker read errors should not block commits.
			m = nil
		}
	}

	sessionID := ""
	if m != nil {
		sessionID = strings.TrimSpace(m.SessionID)
	}
	if sessionID == "" {
		// Unbound: no marker and no env bootstrap.
		return nil
	}

	repoHint := repoEventHint(cwd, m)

	eventID, err := CheckpointEventID(repoHint, head)
	if err != nil {
		// Fail-open: do not block commits; treat as unbound.
		return nil
	}

	ev := HookEvent{
		Kind:      "post-commit",
		EventID:   eventID,
		SessionID: sessionID,
		CommitSHA: head,
	}

	if h == nil || h.uploader == nil {
		// No uploader wired; behave like upload failure (queue best-effort).
		q, err := NewLocalQueue(sessionID)
		if err == nil {
			_ = q.Enqueue(ev)
		}
		return nil
	}

	if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
		// Fail-open: queue for retry and do not fail the commit.
		q, qerr := NewLocalQueue(sessionID)
		if qerr == nil {
			_ = q.Enqueue(ev)
		}
		return nil
	}

	return nil
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
	rewriteType = strings.TrimSpace(rewriteType)

	repoRoot, err := gitOutput(cwd, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	head, _ := gitOutput(cwd, "rev-parse", "HEAD")

	m, err := session.ReadMarker(repoRoot)
	if err != nil {
		if os.IsNotExist(err) {
			m, err = h.bootstrapMarkerFromEnv(repoRoot, cwd, head)
			if err != nil {
				m = nil
			}
		} else {
			m = nil
		}
	}

	sessionID := ""
	if m != nil {
		sessionID = strings.TrimSpace(m.SessionID)
	}
	if sessionID == "" {
		return nil
	}
	repoHint := repoEventHint(cwd, m)
	if repoHint == "" || rewriteType == "" {
		// Fail-open: cannot build stable idempotent IDs without scope/type.
		return nil
	}

	pairs, err := parsePostRewritePairs(stdin)
	if err != nil {
		// Fail-open: do not block developer workflows.
		return nil
	}
	if len(pairs) == 0 {
		return nil
	}

	q, qerr := NewLocalQueue(sessionID)
	if qerr != nil {
		q = nil
	}

	for _, p := range pairs {
		oldSHA := strings.TrimSpace(p[0])
		newSHA := strings.TrimSpace(p[1])
		eid, err := RewriteEventID(repoHint, oldSHA, newSHA, rewriteType)
		if err != nil {
			continue
		}
		ev := HookEvent{
			Kind:         "post-rewrite",
			EventID:      eid,
			SessionID:    sessionID,
			RewriteType:  rewriteType,
			OldCommitSHA: oldSHA,
			NewCommitSHA: newSHA,
		}

		if h == nil || h.uploader == nil {
			if q != nil {
				_ = q.Enqueue(ev)
			}
			continue
		}
		if err := h.uploader.UploadHookEvent(ctx, ev); err != nil {
			if q != nil {
				_ = q.Enqueue(ev)
			}
		}
	}

	return nil
}

func (h *Handler) bootstrapMarkerFromEnv(repoRoot, cwd, headSHA string) (*session.Marker, error) {
	sid := strings.TrimSpace(os.Getenv("AE_SESSION_ID"))
	if sid == "" {
		return nil, os.ErrNotExist
	}

	gitDirAbs, err := gitOutput(cwd, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}
	gitCommonRel, err := gitOutput(cwd, "rev-parse", "--git-common-dir")
	if err != nil {
		return nil, err
	}
	gitCommonAbs, err := absUnder(repoRoot, gitCommonRel)
	if err != nil {
		return nil, err
	}

	// Prevent accidental commits of the marker.
	_ = ensureGitInfoExcludeHas(gitCommonAbs, "/.ae/")
	_ = ensureGitInfoExcludeHas(gitDirAbs, "/.ae/")

	var relayKeyID int64
	if v := strings.TrimSpace(os.Getenv("AE_RELAY_API_KEY_ID")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			relayKeyID = n
		}
	}

	branch := ""
	if b, err := gitOutput(cwd, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		branch = b
	}
	repoFullName := ""
	if remote, err := gitOutput(cwd, "config", "--get", "remote.origin.url"); err == nil {
		repoFullName = strings.TrimSpace(remote)
	}

	workspaceID := ""
	if id, err := session.DeriveWorkspaceID(repoRoot, repoRoot, gitDirAbs, gitCommonAbs); err == nil {
		workspaceID = id
	}

	m := &session.Marker{
		SessionID:     sid,
		WorkspaceID:   workspaceID,
		RuntimeRef:    strings.TrimSpace(os.Getenv("AE_RUNTIME_REF")),
		ProviderName:  strings.TrimSpace(os.Getenv("AE_PROVIDER_NAME")),
		RelayAPIKeyID: relayKeyID,
		RepoFullName:  repoFullName,
		Branch:        branch,
		HeadSHA:       headSHA,
	}
	if err := session.WriteMarker(repoRoot, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (h *Handler) Flush(ctx context.Context, cwd string) error {
	seen := make(map[string]struct{})
	var sessionIDs []string
	addSessionID := func(sessionID string) {
		sessionID = strings.TrimSpace(sessionID)
		if sessionID == "" {
			return
		}
		if _, ok := seen[sessionID]; ok {
			return
		}
		seen[sessionID] = struct{}{}
		sessionIDs = append(sessionIDs, sessionID)
	}

	if repoRoot, err := gitOutput(cwd, "rev-parse", "--show-toplevel"); err == nil {
		if m, err := session.ReadMarker(repoRoot); err == nil && m != nil {
			addSessionID(m.SessionID)
		}
	}

	pendingSessionIDs, err := PendingSessionIDs()
	if err != nil {
		return err
	}
	for _, sessionID := range pendingSessionIDs {
		addSessionID(sessionID)
	}

	for _, sessionID := range sessionIDs {
		if err := h.flushSession(ctx, sessionID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) flushSession(ctx context.Context, sessionID string) error {
	q, err := NewLocalQueue(sessionID)
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
		if h == nil || h.uploader == nil {
			keep = append(keep, it)
			continue
		}
		if err := h.uploader.UploadHookEvent(ctx, it.Event); err != nil {
			keep = append(keep, it)
		}
	}
	return q.rewrite(keep)
}
