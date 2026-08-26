package cmd

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	_ "github.com/glebarez/go-sqlite"
)

func TestAttributionRecoveryInstalledBinaryE2E(t *testing.T) {
	moduleRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "ae-cli")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = moduleRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ae-cli: %v\n%s", err, output)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	repo := initRepoWithCommitForCmdTests(t)
	featurePath := filepath.Join(repo, "feature.go")
	if err := os.WriteFile(featurePath, []byte("package feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "feature.go")
	runGit(t, repo, "commit", "-m", "add feature")
	reachableCommit := runGitOutputForRecovery(t, repo, "rev-parse", "HEAD")
	recoveryTree := runGitOutputForRecovery(t, repo, "rev-parse", "HEAD^{tree}")
	recoveryCommit := runGitOutputForRecovery(t, repo, "commit-tree", recoveryTree, "-m", "retained recovery")
	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatal(err)
	}

	claimStarted := make(chan struct{})
	releaseClaim := make(chan struct{})
	var claimOnce sync.Once
	var claimCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/attribution/checkpoints/commit" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":0}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/attribution/v2/claim-groups/batch" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		var request client.AttributionV2ClaimBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(request.Groups) != 1 {
			http.Error(w, fmt.Sprintf("unexpected groups: %+v", request.Groups), http.StatusBadRequest)
			return
		}
		nextCall := claimCalls.Load() + 1
		hasReachable, hasRecovery := false, false
		for _, allocation := range request.Groups[0].CommitAllocations {
			hasReachable = hasReachable || allocation.CommitSHA == reachableCommit
			hasRecovery = hasRecovery || allocation.CommitSHA == recoveryCommit
		}
		if !hasReachable || (nextCall == 1 && hasRecovery) || (nextCall == 2 && !hasRecovery) {
			http.Error(w, fmt.Sprintf("unexpected call %d allocations: %+v", nextCall, request.Groups[0].CommitAllocations), http.StatusBadRequest)
			return
		}
		claimCalls.Add(1)
		claimOnce.Do(func() { close(claimStarted) })
		<-releaseClaim
		group := request.Groups[0]
		requests := make([]client.AttributionV2ItemStatus, 0, len(group.RequestIDs))
		for _, requestID := range group.RequestIDs {
			requests = append(requests, client.AttributionV2ItemStatus{ID: requestID, Status: "persisted"})
		}
		response := map[string]any{"code": 0, "data": client.AttributionV2ClaimBatchResult{
			LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept,
			Results: []client.AttributionV2ClaimResult{{Group: client.AttributionV2ItemStatus{ID: group.GroupID, Status: "persisted"}, Requests: requests}},
		}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	t.Cleanup(server.Close)

	now := time.Now().UTC()
	protocol := client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept}
	if err := reporting.Save("", &reporting.Config{
		Version: 1, InstallationID: "11111111-1111-4111-8111-111111111111", ServerURL: server.URL,
		AuthSubject: "user:1", RelayProviderID: 17, ReporterToken: "reporter-token", ReportingEnabled: true,
		EnabledAt: &now, Protocol: protocol,
	}); err != nil {
		t.Fatal(err)
	}
	writePositiveEligibilityForServerAt(t, home, server.URL, "user:1", gitCtx.RepoKey, 23, now)

	hint := fmt.Sprintf("repo_config_id:23\x1fuser:1\x1f%s", gitCtx.WorkspaceID)
	reachableEvent, err := hooks.CheckpointEventID(hint, reachableCommit)
	if err != nil {
		t.Fatal(err)
	}
	unreachableEvent, err := hooks.CheckpointEventID(hint, recoveryCommit)
	if err != nil {
		t.Fatal(err)
	}
	if err := hooks.SaveSyncTask(hooks.SyncTask{
		WorkspaceID: gitCtx.WorkspaceID, RepoRoot: repo, ServerURL: server.URL, AuthSubject: "user:1",
		RepoConfigID: 23, RepoKey: gitCtx.RepoKey, Status: hooks.SyncTaskStatusPending, LastRequestedAt: now,
		V2Triggers: []hooks.V2SyncTrigger{
			{Kind: "post-commit", EventID: reachableEvent, CommitSHA: reachableCommit, CapturedAt: now, RelayProviderID: 17},
			{Kind: "post-commit", EventID: unreachableEvent, CommitSHA: recoveryCommit, CapturedAt: now, RelayProviderID: 17},
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := hooks.SaveSyncTask(hooks.SyncTask{
		WorkspaceID: "synthetic-workspace", RepoRoot: filepath.Join(home, "deleted-TestFixture", "001"),
		ServerURL: server.URL, AuthSubject: "user:1", RepoConfigID: 99, RepoKey: "repo-host.example.com/org/repo",
		Status: hooks.SyncTaskStatusPending, LastRequestedAt: now, LastError: "repository unavailable",
	}); err != nil {
		t.Fatal(err)
	}
	legitimateDeletedWorkspace := "legitimate-deleted-worktree"
	if err := hooks.SaveSyncTask(hooks.SyncTask{
		WorkspaceID: legitimateDeletedWorkspace, RepoRoot: filepath.Join(home, "deleted-real-worktree"),
		ServerURL: server.URL, AuthSubject: "user:1", RepoConfigID: 24, RepoKey: "github.com/acme/other",
		Status: hooks.SyncTaskStatusPending, LastRequestedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := hooks.EnqueueUnresolvedHookEvent(hooks.UnresolvedHookEvent{
		Kind: "post-commit", RemoteURL: "https://repo-host.example.com/org/repo.git", RepoKey: "repo-host.example.com/org/repo",
		WorkspaceID: "synthetic-workspace", ServerURL: server.URL, AuthSubject: "user:1", CommitSHA: "abc123", CapturedAt: now.Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	writeRecoveryCodexEvidence(t, home)

	childEnv := []string{
		"HOME=" + home,
		"GIT_CONFIG_GLOBAL=" + filepath.Join(home, ".gitconfig"),
		"GIT_CONFIG_NOSYSTEM=1",
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + t.TempDir(),
	}
	managedHook := filepath.Join(t.TempDir(), "post-commit")
	if err := os.WriteFile(managedHook, []byte(hooks.RenderManagedHookScript("post-commit", "recovery-e2e")), 0o755); err != nil {
		t.Fatal(err)
	}
	managedEnv := append(append([]string{}, childEnv...), "AE_CLI_BIN="+bin)
	managedCommands := make([]*exec.Cmd, 20)
	managedOutputs := make([]bytes.Buffer, len(managedCommands))
	for index := range managedCommands {
		command := exec.Command(managedHook)
		command.Dir = repo
		command.Env = managedEnv
		command.Stdout = &managedOutputs[index]
		command.Stderr = &managedOutputs[index]
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		managedCommands[index] = command
	}
	for index, command := range managedCommands {
		if err := command.Wait(); err != nil {
			t.Fatalf("managed hook wakeup %d: %v\n%s", index, err, managedOutputs[index].String())
		}
	}
	commands := make([]*exec.Cmd, 20)
	outputs := make([]bytes.Buffer, len(commands))
	for index := range commands {
		command := exec.Command(bin, "hook", "background-sync")
		command.Dir = repo
		command.Env = childEnv
		command.Stdout = &outputs[index]
		command.Stderr = &outputs[index]
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands[index] = command
	}
	select {
	case <-claimStarted:
	case <-time.After(10 * time.Second):
		t.Fatal("installed binary did not reach controlled claim backend")
	}
	close(releaseClaim)
	waitCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for index, command := range commands {
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()
		select {
		case err := <-done:
			var exitErr *exec.ExitError
			if err != nil && (!errors.As(err, &exitErr) || !strings.Contains(outputs[index].String(), "trigger commit is not reachable")) {
				t.Fatalf("background process %d: %v\n%s", index, err, outputs[index].String())
			}
		case <-waitCtx.Done():
			t.Fatalf("background process %d did not exit: %v", index, waitCtx.Err())
		}
	}
	waitForRecoveryBackgroundProcesses(t, bin, home)
	if got := claimCalls.Load(); got != 1 {
		t.Fatalf("claim calls = %d, want exactly one", got)
	}

	status := exec.Command(bin, "attribution", "status")
	status.Dir = repo
	status.Env = childEnv
	statusOutput, err := status.CombinedOutput()
	if err != nil {
		t.Fatalf("attribution status: %v\n%s", err, statusOutput)
	}
	for _, want := range []string{
		"Sync Task: pending [failed]", "remaining_triggers: 1", "failure_reason: commit unavailable in recovery checkout",
		"Synthetic Fixture Quarantine: workspaces=1 unresolved=1", "V2 Claim Delivery: pending=0 conflict=0 upgrade_required=0",
	} {
		if !bytes.Contains(statusOutput, []byte(want)) {
			t.Fatalf("installed status missing %q:\n%s", want, statusOutput)
		}
	}
	if bytes.Contains(statusOutput, []byte("repo-host.example.com")) || bytes.Contains(statusOutput, []byte(home)) {
		t.Fatalf("installed status leaked local fixture detail:\n%s", statusOutput)
	}
	if task, err := hooks.LoadSyncTask(legitimateDeletedWorkspace); err != nil || task == nil {
		t.Fatalf("legitimate deleted-worktree task = %+v, %v, want retained", task, err)
	}

	runGit(t, repo, "update-ref", "refs/heads/recovered-attribution-e2e", recoveryCommit)
	restore := exec.Command(bin, "hook", "background-sync")
	restore.Dir = repo
	restore.Env = childEnv
	restoreOutput, restoreErr := restore.CombinedOutput()
	if restoreErr != nil && !bytes.Contains(restoreOutput, []byte("no such file or directory")) {
		t.Fatalf("restored background sync: %v\n%s", restoreErr, restoreOutput)
	}
	if got := claimCalls.Load(); got != 2 {
		t.Fatalf("claim calls after ref restoration = %d, want two", got)
	}
	waitForRecoveryBackgroundProcesses(t, bin, home)
	finalStatus := exec.Command(bin, "attribution", "status")
	finalStatus.Dir = repo
	finalStatus.Env = childEnv
	finalOutput, err := finalStatus.CombinedOutput()
	if err != nil {
		t.Fatalf("final attribution status: %v\n%s", err, finalOutput)
	}
	for _, want := range []string{
		"Sync Task: none [ok]", "Machine Sync Tasks: queued=0 running=0 yielded=0 recoverable=1 terminal=0 expiring=0",
		"Synthetic Fixture Quarantine: workspaces=1 unresolved=1", "V2 Claim Delivery: pending=0 conflict=0 upgrade_required=0",
	} {
		if !bytes.Contains(finalOutput, []byte(want)) {
			t.Fatalf("final installed status missing %q:\n%s", want, finalOutput)
		}
	}
}

func waitForRecoveryBackgroundProcesses(t *testing.T, bin, home string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		processes := 0
		if runtime.GOOS != "windows" {
			command := exec.Command("ps", "-Ao", "command=")
			output, err := command.Output()
			if err != nil {
				t.Fatalf("list recovery processes: %v", err)
			}
			for _, line := range strings.Split(string(output), "\n") {
				if strings.Contains(line, bin+" hook background-sync") {
					processes++
				}
			}
		}
		_, lockErr := os.Stat(filepath.Join(home, ".ae-cli", "state", "attribution", "machine-sync.run.lock"))
		wakeFiles, err := filepath.Glob(filepath.Join(home, ".ae-cli", "state", "attribution", "machine-sync-wakes", "*.json"))
		if err != nil {
			t.Fatal(err)
		}
		if processes == 0 && os.IsNotExist(lockErr) && len(wakeFiles) == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("recovery processes did not converge: processes=%d lock_present=%t wakes=%d", processes, lockErr == nil, len(wakeFiles))
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func runGitOutputForRecovery(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func writeRecoveryCodexEvidence(t *testing.T, home string) {
	t.Helper()
	sessions := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		t.Fatal(err)
	}
	rows := []map[string]any{
		{"timestamp": time.Now().UTC().Format(time.RFC3339Nano), "type": "session_meta", "payload": map[string]any{"id": "thread-recovery-e2e"}},
		{"timestamp": time.Now().UTC().Add(time.Second).Format(time.RFC3339Nano), "type": "turn_context", "payload": map[string]any{"turn_id": "turn-recovery-e2e"}},
		{"timestamp": time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano), "type": "response_item", "payload": map[string]any{
			"type": "custom_tool_call", "name": "exec",
			"input": "const patch = \"*** Begin Patch\\n*** Add File: feature.go\\n+package feature\\n*** End Patch\";\ntext(await tools.apply_patch(patch));",
		}},
	}
	var body bytes.Buffer
	for _, row := range rows {
		payload, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(payload)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(sessions, "recovery-e2e.jsonl"), body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, ".codex", "logs_2.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE logs (id INTEGER PRIMARY KEY, ts INTEGER, ts_nanos INTEGER, thread_id TEXT, target TEXT, feedback_log_body TEXT)`); err != nil {
		t.Fatal(err)
	}
	logBody := `turn{thread.id=thread-recovery-e2e turn.id=turn-recovery-e2e}: Request completed method=POST api.path="responses" status=200 OK headers={"x-client-request-id":"request-recovery-e2e"}`
	if _, err := db.Exec(`INSERT INTO logs(id, ts, ts_nanos, thread_id, target, feedback_log_body) VALUES(1, ?, 0, ?, ?, ?)`,
		time.Now().UTC().Unix(), "thread-recovery-e2e", "codex_http_client::client", logBody); err != nil {
		t.Fatal(err)
	}
}
