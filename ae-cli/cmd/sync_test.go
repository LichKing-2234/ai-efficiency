package cmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/internal/attributionlocal"
	"github.com/ai-efficiency/ae-cli/internal/client"
	"github.com/ai-efficiency/ae-cli/internal/hooks"
	"github.com/ai-efficiency/ae-cli/internal/reporting"
	"github.com/ai-efficiency/ae-cli/internal/toolconfig"
	"github.com/google/uuid"
)

func ptrTimeValue(t time.Time) *time.Time {
	return &t
}

func TestSyncStatusCommandIsRegistered(t *testing.T) {
	var found bool
	for _, c := range syncCmd.Commands() {
		if c.Name() == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected sync status subcommand")
	}
}

func TestDoctorPrintsPendingSyncTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	task := hooks.SyncTask{
		WorkspaceID:       gitCtx.WorkspaceID,
		RepoRoot:          repo,
		ServerURL:         "https://ae.example.com",
		AuthSubject:       "user:123",
		RepoConfigID:      123,
		RepoKey:           gitCtx.RepoKey,
		Status:            hooks.SyncTaskStatusPending,
		LastRequestedAt:   time.Date(2026, 5, 26, 9, 0, 0, 0, time.UTC),
		LastError:         "spawn failed",
		LastFailureStage:  hooks.SyncTaskFailureStageRunner,
		LastFailureReason: "background sync could not start",
		AttemptCount:      3,
	}
	if err := hooks.SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}
	uploadedAt := time.Date(2026, 5, 26, 8, 55, 0, 0, time.UTC)
	if err := hooks.AppendLedger(gitCtx.WorkspaceID, hooks.LedgerRecord{
		Kind: "checkpoint", DedupeKey: "uploaded-a", ServerURL: "https://ae.example.com", AuthSubject: "user:123",
		RepoConfigID: 123, RepoKey: gitCtx.RepoKey, WorkspaceID: gitCtx.WorkspaceID, Status: "uploaded", AttemptedAt: uploadedAt, UploadedAt: &uploadedAt,
	}); err != nil {
		t.Fatalf("AppendLedger uploaded: %v", err)
	}
	if err := hooks.AppendLedger(gitCtx.WorkspaceID, hooks.LedgerRecord{
		Kind: "checkpoint", DedupeKey: "failed-a", ServerURL: "https://ae.example.com", AuthSubject: "user:123",
		RepoConfigID: 123, RepoKey: gitCtx.RepoKey, WorkspaceID: gitCtx.WorkspaceID, Status: "failed", AttemptedAt: uploadedAt.Add(time.Minute), LastError: "upload failed locally",
	}); err != nil {
		t.Fatalf("AppendLedger failed: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"Sync Task: pending", "background sync could not start", "attempt_count: 3", "last_success=2026-05-26T08:55:00Z", "last_error=upload failed locally"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("doctor output missing %q:\n%s", want, output)
		}
	}
}

func TestDoctorPrintsCompactReportingStatusWithoutCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	endpoint := "https://ae.example.com/api/v1/attribution/otel/v1/traces"
	if err := reporting.Save("", &reporting.Config{
		Version: 1, InstallationID: "installation-test", ServerURL: "https://ae.example.com",
		ReporterToken: "reporter-secret", OTLPToken: "otlp-secret", ReportingEnabled: true, OTelEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := toolconfig.ConfigureCodexOTLP(home, endpoint, "otlp-secret"); err != nil {
		t.Fatal(err)
	}
	if err := attributionlocal.SaveJSON(attributionlocal.CompactStatePath(), attributionlocal.CompactState{
		Version: 2, EnabledAt: time.Now().UTC(), SeenAtoms: map[string]bool{},
		Pending: []attributionlocal.CompactPending{{}}, Triggers: []attributionlocal.CompactTrigger{{}},
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	printCompactReportingStatus(&output)
	for _, want := range []string{
		"Compact reporting", "Installation: installation-test", "Reporter credential: available [ok]", "Legacy OTLP credential: available [warn]",
		"Buckets:      enabled [ok]", "Codex OTLP:   legacy enabled [warn]", "Pending:      buckets=1 triggers=1",
		"Codex OTLP config: legacy AE-managed [warn]", "endpoint=match", "credential=match", "prompt_logging=disabled", "trace_only=true",
		"run 'ae-cli attribution enable'",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("compact doctor output missing %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "reporter-secret") || strings.Contains(output.String(), "otlp-secret") {
		t.Fatalf("compact doctor leaked credentials: %s", output.String())
	}
}

func TestDoctorPreservesAndDoesNotWarnForUserManagedOTLP(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := reporting.Save("", &reporting.Config{
		Version: 1, InstallationID: "installation-test", ServerURL: "https://ae.example.com",
		ReporterToken: "reporter-secret", OTLPToken: "old-ae-token", ReportingEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := toolconfig.ConfigureCodexOTLP(home, "https://telemetry.example.org/v1/traces", "user-token"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	printCompactReportingStatus(&output)
	if !strings.Contains(output.String(), "Codex OTLP config: user-managed (preserved) [ok]") {
		t.Fatalf("doctor did not recognize user-managed exporter:\n%s", output.String())
	}
	if strings.Contains(output.String(), "run 'ae-cli attribution enable'") {
		t.Fatalf("doctor offered AE cleanup for user-managed exporter:\n%s", output.String())
	}
}

func TestDoctorShowsStableInstallationWhenCredentialConfigIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	config, err := reporting.LoadOrCreate("")
	if err != nil {
		t.Fatal(err)
	}
	path, err := reporting.DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove credential config: %v", err)
	}

	var output bytes.Buffer
	printCompactReportingStatus(&output)
	for _, want := range []string{
		"Compact reporting", "Installation: " + config.InstallationID,
		"Reporter credential: missing [failed]", "Legacy OTLP credential: unknown [warn]",
		"login again to recover credentials for this installation",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("compact doctor output missing %q:\n%s", want, output.String())
		}
	}
}

func TestDoctorRecoversCorruptSyncTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)
	withWorkingDir(t, repo)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	path, err := hooks.SyncTaskPath(gitCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("SyncTaskPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	buf := &bytes.Buffer{}
	doctorCmd.SetOut(buf)
	doctorCmd.SetErr(buf)
	if err := doctorCmd.RunE(doctorCmd, nil); err != nil {
		t.Fatalf("doctorCmd.RunE: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("corrupt sync task moved aside")) {
		t.Fatalf("doctor output missing corrupt recovery message:\n%s", buf.String())
	}
}

func TestSyncStatusPrintsRunningTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	now := time.Date(2026, 5, 26, 9, 30, 0, 0, time.UTC)
	task := hooks.SyncTask{
		WorkspaceID:     gitCtx.WorkspaceID,
		RepoRoot:        repo,
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:123",
		RepoConfigID:    123,
		RepoKey:         gitCtx.RepoKey,
		Status:          hooks.SyncTaskStatusRunning,
		LastRequestedAt: now.Add(-5 * time.Minute),
		LastStartedAt:   &now,
		RunnerPID:       os.Getpid(),
		LeaseExpiresAt:  ptrTimeValue(now.Add(5 * time.Minute)),
	}
	if err := hooks.SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	defer func() { _ = os.Chdir(wd) }()
	if err := os.Chdir(repo); err != nil {
		t.Fatalf("Chdir(repo): %v", err)
	}

	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("syncStatusCmd.RunE: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"Sync Task: running", "runner_pid"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("sync status output missing %q:\n%s", want, output)
		}
	}
}

func TestSyncStatusRecoversInactiveRunner(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	now := time.Now().UTC()
	task := hooks.SyncTask{
		WorkspaceID:     gitCtx.WorkspaceID,
		RepoRoot:        repo,
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:123",
		RepoConfigID:    123,
		RepoKey:         gitCtx.RepoKey,
		Status:          hooks.SyncTaskStatusRunning,
		LastRequestedAt: now.Add(-5 * time.Minute),
		LastStartedAt:   &now,
		RunnerPID:       999999,
		LeaseExpiresAt:  ptrTimeValue(now.Add(5 * time.Minute)),
	}
	if err := hooks.SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	withWorkingDir(t, repo)
	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("syncStatusCmd.RunE: %v", err)
	}
	output := buf.String()
	for _, want := range []string{"inactive runner recovered", "Sync Task: pending", "runner exited before updating sync task"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("sync status output missing %q:\n%s", want, output)
		}
	}
}

func TestSyncStatusRecoversCorruptSyncTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)
	withWorkingDir(t, repo)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	path, err := hooks.SyncTaskPath(gitCtx.WorkspaceID)
	if err != nil {
		t.Fatalf("SyncTaskPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("syncStatusCmd.RunE: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("corrupt sync task moved aside")) {
		t.Fatalf("sync status output missing corrupt recovery message:\n%s", buf.String())
	}
}

func TestPrintSyncTaskStatusShowsSafeV2FailureWithoutRawIdentifiers(t *testing.T) {
	firstFailure := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	task := &hooks.SyncTask{
		WorkspaceID: "ws-safe-status", Status: hooks.SyncTaskStatusPending, LastRequestedAt: firstFailure,
		LastError:        "backend rejected client:raw-request-value",
		LastFailureStage: "backend_delivery", LastFailureReason: "backend claim delivery failed",
		FirstFailureAt: &firstFailure, RemainingTriggerCount: 2,
	}
	var output bytes.Buffer
	printSyncTaskStatus(&output, task)
	for _, want := range []string{"failure_stage: backend_delivery", "failure_reason: backend claim delivery failed", "first_failure_at: 2026-08-12T12:00:00Z", "remaining_triggers: 2"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("status missing %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "raw-request-value") {
		t.Fatalf("status leaked a Request ID:\n%s", output.String())
	}
	output.Reset()
	printSyncTaskStatus(&output, &hooks.SyncTask{Status: hooks.SyncTaskStatusPending, LastRequestedAt: firstFailure, LastError: "legacy error client:raw-request-value"})
	if strings.Contains(output.String(), "raw-request-value") {
		t.Fatalf("legacy sync task leaked a Request ID:\n%s", output.String())
	}
}

func TestSyncStatusShowsUnresolvedAndDeadLetterCounts(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)
	withWorkingDir(t, repo)

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	if err := hooks.EnqueueUnresolvedHookEvent(hooks.UnresolvedHookEvent{
		Kind:        "post-commit",
		RemoteURL:   "https://github.com/acme/repo.git",
		RepoKey:     "github.com/acme/repo",
		WorkspaceID: gitCtx.WorkspaceID,
		CommitSHA:   "abc123",
	}); err != nil {
		t.Fatalf("EnqueueUnresolvedHookEvent: %v", err)
	}
	workspaceDir := filepath.Join(attributionlocal.AttributionRootDir(), "workspaces", gitCtx.WorkspaceID)
	if err := os.MkdirAll(workspaceDir, 0o700); err != nil {
		t.Fatalf("mkdir workspace dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspaceDir, "dead-letter-tool-usage.jsonl"), []byte(`{"version":1}`+"\n"), 0o600); err != nil {
		t.Fatalf("write dead-letter: %v", err)
	}

	buf := &bytes.Buffer{}
	syncStatusCmd.SetOut(buf)
	syncStatusCmd.SetErr(buf)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatalf("sync status RunE: %v", err)
	}
	for _, want := range []string{"Unresolved Hook Events: 1", "Tool Usage Dead Letters: 1"} {
		if !bytes.Contains(buf.Bytes(), []byte(want)) {
			t.Fatalf("sync status output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestSyncCommandReportsAlreadyRunningTask(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeTestToken(t, home, "user:123")
	writePositiveEligibility(t, home, "github.com/acme/repo", 123)
	withWorkingDir(t, repo)

	oldCfg := cfg
	oldClient := apiClient
	cfg = nil
	apiClient = nil
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})

	gitCtx, err := hooks.DetectGitContext(repo)
	if err != nil {
		t.Fatalf("DetectGitContext: %v", err)
	}
	now := time.Now().UTC()
	task := hooks.SyncTask{
		WorkspaceID:     gitCtx.WorkspaceID,
		RepoRoot:        repo,
		ServerURL:       "https://ae.example.com",
		AuthSubject:     "user:123",
		RepoConfigID:    123,
		RepoKey:         gitCtx.RepoKey,
		Status:          hooks.SyncTaskStatusRunning,
		LastRequestedAt: now,
		LastStartedAt:   &now,
		RunnerPID:       os.Getpid(),
		LeaseExpiresAt:  ptrTimeValue(now.Add(5 * time.Minute)),
	}
	if err := hooks.SaveSyncTask(task); err != nil {
		t.Fatalf("SaveSyncTask: %v", err)
	}

	buf := &bytes.Buffer{}
	syncCmd.SetOut(buf)
	syncCmd.SetErr(buf)
	if err := syncCmd.RunE(syncCmd, nil); err != nil {
		t.Fatalf("syncCmd.RunE: %v", err)
	}
	output := buf.String()
	if bytes.Contains([]byte(output), []byte("Synced local attribution data")) {
		t.Fatalf("sync output claimed completion while runner is active:\n%s", output)
	}
	if !bytes.Contains([]byte(output), []byte("Attribution sync already running")) {
		t.Fatalf("sync output missing active runner message:\n%s", output)
	}
}

func TestSyncCommandUsesPersistedFormalProtocolWithoutV1BaselineOrRequest(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDir(t, repo)

	var v1Calls, v2Calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/attribution/repos/resolve-remote":
			_, _ = w.Write([]byte(`{"code":0,"data":{"eligible":true,"repo_config_id":123,"repo_key":"github.com/acme/repo","full_name":"acme/repo","clone_url":"https://github.com/acme/repo.git","status":"active","binding_state":"bound"}}`))
		case "/api/v1/attribution/v2/claim-groups/batch":
			v2Calls++
			_, _ = w.Write([]byte(`{"code":201,"data":{"ledger_epoch":"formal_v2","v1_write_policy":"upgrade_required","minimum_cli_version":"0.2.0-preview.5","results":[{"group":{"id":"group-1","status":"persisted"},"calibration":{"status":"not_present"},"requests":[{"id":"req-1","status":"persisted"}]}]}}`))
		case "/api/v1/attribution/usage-buckets/batch":
			v1Calls++
			w.WriteHeader(http.StatusConflict)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	now := time.Now().UTC().Add(-time.Hour)
	if err := attributionlocal.SaveV2ClaimState(&attributionlocal.V2ClaimState{
		Version: 1,
		Claims: []attributionlocal.V2ClaimCandidate{{
			LocalKey: "local-1",
			Group: client.AttributionV2ClaimGroup{
				SchemaVersion: 2, GroupID: "group-1", RelayProviderID: 7, ThreadID: "thread-1", TurnID: "turn-1",
				EvidenceDigest: "evidence-1", RequestIDs: []string{"req-1"},
			},
			FirstSeenAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reporting.Save("", &reporting.Config{
		Version: 1, InstallationID: uuid.NewString(), ServerURL: server.URL, AuthSubject: "user:123",
		ReporterToken: "reporter-token", ReportingEnabled: true, RelayProviderID: 7, EnabledAt: &now,
		Protocol: client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochFormalV2, V1WritePolicy: client.AttributionV1WritePolicyUpgradeNeeded, MinimumCLIVersion: "0.2.0-preview.5"},
	}); err != nil {
		t.Fatal(err)
	}

	oldCfg := cfg
	oldClient := apiClient
	cfg = nil
	apiClient = nil
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})
	var output bytes.Buffer
	syncCmd.SetOut(&output)
	syncCmd.SetErr(&output)
	if err := syncCmd.RunE(syncCmd, nil); err != nil {
		t.Fatalf("sync formal v2 ACK: %v\n%s", err, output.String())
	}
	if v1Calls != 0 || v2Calls != 1 {
		t.Fatalf("formal delivery calls v1=%d v2=%d, want 0/1", v1Calls, v2Calls)
	}
	if _, err := attributionlocal.LoadCompactState(); !os.IsNotExist(err) {
		t.Fatalf("formal sync created or required v1 state: %v", err)
	}
	state, err := attributionlocal.LoadV2ClaimState()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Claims) != 1 || !state.Claims[0].GroupAcknowledged || len(state.Claims[0].Group.RequestIDs) != 0 {
		t.Fatalf("formal v2 ACK did not consume explicit items: %+v", state.Claims)
	}
}

func TestSyncCommandContinuesV2AfterV1UpgradeRequired(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDir(t, repo)

	var v1Calls, v2Calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/attribution/repos/resolve-remote":
			_, _ = w.Write([]byte(`{"code":0,"data":{"eligible":true,"repo_config_id":123,"repo_key":"github.com/acme/repo","full_name":"acme/repo","clone_url":"https://github.com/acme/repo.git","status":"active","binding_state":"bound"}}`))
		case "/api/v1/attribution/usage-buckets/batch":
			v1Calls++
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":409,"message":"ae-cli upgrade required","details":{"error_code":"upgrade_required","minimum_cli_version":"0.2.0-preview.5"}}`))
		case "/api/v1/attribution/v2/claim-groups/batch":
			v2Calls++
			_, _ = w.Write([]byte(`{"code":201,"data":{"ledger_epoch":"shadow_v2","v1_write_policy":"upgrade_required","minimum_cli_version":"0.2.0-preview.5","results":[{"group":{"id":"group-1","status":"persisted"},"calibration":{"status":"not_present"},"requests":[{"id":"req-1","status":"persisted"}]}]}}`))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	now := time.Now().UTC().Add(-time.Hour)
	if err := attributionlocal.SaveJSON(attributionlocal.CompactStatePath(), attributionlocal.CompactState{
		Version: 2, EnabledAt: now, SeenAtoms: map[string]bool{},
		Pending: []attributionlocal.CompactPending{{Bucket: client.AttributionBucket{BucketID: "v1-bucket-1"}, AtomIDs: []string{"atom-1"}}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := attributionlocal.SaveV2ClaimState(&attributionlocal.V2ClaimState{
		Version: 1,
		Claims: []attributionlocal.V2ClaimCandidate{{
			LocalKey: "local-1",
			Group: client.AttributionV2ClaimGroup{
				SchemaVersion: 2, GroupID: "group-1", RelayProviderID: 7, ThreadID: "thread-1", TurnID: "turn-1",
				EvidenceDigest: "evidence-1", RequestIDs: []string{"req-1"},
			},
			FirstSeenAt: now,
		}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := reporting.Save("", &reporting.Config{
		Version: 1, InstallationID: uuid.NewString(), ServerURL: server.URL, AuthSubject: "user:123",
		ReporterToken: "reporter-token", ReportingEnabled: true, RelayProviderID: 7, EnabledAt: &now,
		Protocol: client.AttributionProtocol{LedgerEpoch: client.AttributionLedgerEpochShadowV2, V1WritePolicy: client.AttributionV1WritePolicyAccept},
	}); err != nil {
		t.Fatal(err)
	}

	oldCfg := cfg
	oldClient := apiClient
	cfg = nil
	apiClient = nil
	t.Cleanup(func() {
		cfg = oldCfg
		apiClient = oldClient
	})
	var output bytes.Buffer
	syncCmd.SetOut(&output)
	syncCmd.SetErr(&output)
	if err := syncCmd.RunE(syncCmd, nil); err != nil {
		t.Fatalf("sync after v1 upgrade_required: %v\n%s", err, output.String())
	}
	if v1Calls != 1 || v2Calls != 1 {
		t.Fatalf("delivery calls v1=%d v2=%d, want 1/1", v1Calls, v2Calls)
	}
	compact, err := attributionlocal.LoadCompactState()
	if err != nil {
		t.Fatal(err)
	}
	if len(compact.Pending) != 1 || compact.SeenAtoms["atom-1"] {
		t.Fatalf("v1 rejection was treated as ACK: %+v", compact)
	}
	v2, err := attributionlocal.LoadV2ClaimState()
	if err != nil {
		t.Fatal(err)
	}
	if len(v2.Claims) != 1 || !v2.Claims[0].GroupAcknowledged || len(v2.Claims[0].Group.RequestIDs) != 0 {
		t.Fatalf("v2 claim was not delivered after v1 rejection: %+v", v2.Claims)
	}
}

func TestSyncStatusShowsV1UpgradeRequirement(t *testing.T) {
	repo := initRepoWithCommitForCmdTests(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	withWorkingDir(t, repo)

	now := time.Now().UTC().Add(-time.Hour)
	if err := attributionlocal.SaveJSON(attributionlocal.CompactStatePath(), attributionlocal.CompactState{
		Version: 2, EnabledAt: now, SeenAtoms: map[string]bool{},
		V1WritePolicy: client.AttributionV1WritePolicyUpgradeNeeded, MinimumCLIVersion: "0.2.0-preview.5",
	}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	syncStatusCmd.SetOut(&output)
	syncStatusCmd.SetErr(&output)
	if err := syncStatusCmd.RunE(syncStatusCmd, nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"upgrade_required", "minimum_cli_version=0.2.0-preview.5"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("sync status missing %q:\n%s", want, output.String())
		}
	}
}
