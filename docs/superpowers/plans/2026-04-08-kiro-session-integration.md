# Kiro Session Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend the existing local session proxy runtime so `kiro-cli` launches inside an `ae-cli` session with managed hooks, inherited local-proxy credentials, and backend session-event attribution that matches the current `Codex + Claude` behavior.

**Architecture:** Reuse the existing local-session runtime instead of inventing a third integration path. `ae-cli start` will materialize a managed Kiro agent config under the workspace, shell/dispatcher launch Kiro with `--agent <managed-name>`, and Kiro hook events will forward into the same local proxy `/api/v1/session-events` ingress already used by Codex and Claude.

**Tech Stack:** Go (`cobra`, `tmux`, existing `toolconfig` package), Kiro CLI managed agent JSON, local proxy event ingress, existing backend session/session_event tables.

---

## File Map

- Create: [ae-cli/internal/toolconfig/kiro.go](/Users/admin/ai-efficiency/ae-cli/internal/toolconfig/kiro.go)  
  Responsibility: write/cleanup the managed Kiro agent JSON and any helper constants used by session startup.
- Modify: [ae-cli/internal/toolconfig/toolconfig_test.go](/Users/admin/ai-efficiency/ae-cli/internal/toolconfig/toolconfig_test.go)  
  Responsibility: unit tests for Kiro managed agent generation and cleanup.
- Modify: [ae-cli/internal/session/manager.go](/Users/admin/ai-efficiency/ae-cli/internal/session/manager.go)  
  Responsibility: write Kiro config during `ae-cli start`, remove it during stop/rollback, and expose any Kiro runtime metadata needed by launchers.
- Modify: [ae-cli/internal/session/session_test.go](/Users/admin/ai-efficiency/ae-cli/internal/session/session_test.go)  
  Responsibility: session-start/session-stop tests proving Kiro config is written and cleaned up alongside Codex/Claude.
- Modify: [ae-cli/internal/shell/shell.go](/Users/admin/ai-efficiency/ae-cli/internal/shell/shell.go)  
  Responsibility: launch `kiro-cli` panes with the managed agent flag and runtime env.
- Modify: [ae-cli/internal/shell/shell_test.go](/Users/admin/ai-efficiency/ae-cli/internal/shell/shell_test.go)  
  Responsibility: prove `@kiro` launches with `--agent ae-session-managed`.
- Modify: [ae-cli/internal/dispatcher/dispatcher.go](/Users/admin/ai-efficiency/ae-cli/internal/dispatcher/dispatcher.go)  
  Responsibility: make non-shell tool launches add the managed Kiro agent flag and carry runtime env consistently.
- Modify: [ae-cli/internal/proxy/hookforward_test.go](/Users/admin/ai-efficiency/ae-cli/internal/proxy/hookforward_test.go)  
  Responsibility: verify Kiro hook payloads normalize into backend session events.
- Modify: [ae-cli/internal/proxy/server_test.go](/Users/admin/ai-efficiency/ae-cli/internal/proxy/server_test.go)  
  Responsibility: confirm local proxy ingress stores Kiro events exactly like Codex/Claude.
- Modify: [docs/superpowers/specs/2026-04-02-local-session-proxy-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-02-local-session-proxy-design.md)  
  Responsibility: record that Kiro is now in-scope for the local session proxy rollout.

## Task 1: Add Managed Kiro Tool Config Writer

**Files:**
- Create: [ae-cli/internal/toolconfig/kiro.go](/Users/admin/ai-efficiency/ae-cli/internal/toolconfig/kiro.go)
- Modify: [ae-cli/internal/toolconfig/toolconfig_test.go](/Users/admin/ai-efficiency/ae-cli/internal/toolconfig/toolconfig_test.go)

- [ ] **Step 1: Write the failing Kiro config test**

```go
func TestWriteKiroSessionConfig(t *testing.T) {
	workspaceRoot := t.TempDir()
	err := WriteKiroSessionConfig(workspaceRoot, KiroConfig{
		AgentName: "ae-session-managed",
		SelfPath:  "/tmp/ae-cli",
	})
	if err != nil {
		t.Fatalf("WriteKiroSessionConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(workspaceRoot, ".kiro", "agents", "ae-session-managed.json"))
	if err != nil {
		t.Fatalf("ReadFile agent config: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal agent config: %v", err)
	}

	hooks, ok := decoded["hooks"].(map[string]any)
	if !ok {
		t.Fatalf("hooks missing: %+v", decoded)
	}
	for _, eventName := range []string{"AgentSpawn", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop"} {
		if _, ok := hooks[eventName]; !ok {
			t.Fatalf("missing %q hook: %+v", eventName, hooks)
		}
	}
}
```

- [ ] **Step 2: Run the test to confirm it fails**

Run:

```bash
cd ae-cli
go test ./internal/toolconfig -run 'TestWriteKiroSessionConfig$' -count=1
```

Expected: FAIL because `WriteKiroSessionConfig` does not exist yet.

- [ ] **Step 3: Implement the minimal Kiro config writer**

Add [ae-cli/internal/toolconfig/kiro.go](/Users/admin/ai-efficiency/ae-cli/internal/toolconfig/kiro.go):

```go
package toolconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const ManagedKiroAgentName = "ae-session-managed"

type KiroConfig struct {
	AgentName string
	SelfPath  string
}

func WriteKiroSessionConfig(workspaceRoot string, cfg KiroConfig) error {
	agentName := strings.TrimSpace(cfg.AgentName)
	if agentName == "" {
		agentName = ManagedKiroAgentName
	}
	command := hookCommand(strings.TrimSpace(cfg.SelfPath), "kiro")
	doc := map[string]any{
		"name": agentName,
		"hooks": map[string]any{
			"AgentSpawn": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}},
			"UserPromptSubmit": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}},
			"PreToolUse": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}},
			"PostToolUse": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}},
			"Stop": []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command}}}},
		},
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(workspaceRoot, ".kiro", "agents", agentName+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func CleanupKiroSessionConfig(workspaceRoot, agentName string) error {
	if strings.TrimSpace(agentName) == "" {
		agentName = ManagedKiroAgentName
	}
	path := filepath.Join(workspaceRoot, ".kiro", "agents", agentName+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
```

- [ ] **Step 4: Run the toolconfig tests**

Run:

```bash
cd ae-cli
go test ./internal/toolconfig -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/toolconfig/kiro.go ae-cli/internal/toolconfig/toolconfig_test.go
git commit -m "feat(ae-cli): add managed kiro hook config"
```

## Task 2: Write and Clean Up Kiro Config During Session Lifecycle

**Files:**
- Modify: [ae-cli/internal/session/manager.go](/Users/admin/ai-efficiency/ae-cli/internal/session/manager.go)
- Modify: [ae-cli/internal/session/session_test.go](/Users/admin/ai-efficiency/ae-cli/internal/session/session_test.go)

- [ ] **Step 1: Add a failing session lifecycle test**

```go
func TestManagerStartWritesKiroManagedAgentConfig(t *testing.T) {
	state, _, _ := startSessionWithFakeBootstrap(t)
	data, err := os.ReadFile(filepath.Join(state.WorkspaceRoot, ".kiro", "agents", "ae-session-managed.json"))
	if err != nil {
		t.Fatalf("read kiro agent config: %v", err)
	}
	if !strings.Contains(string(data), "hook session-event --tool kiro") {
		t.Fatalf("missing kiro hook command: %s", string(data))
	}
}
```

- [ ] **Step 2: Run the session test to confirm it fails**

Run:

```bash
cd ae-cli
go test ./internal/session -run 'TestManagerStartWritesKiroManagedAgentConfig$' -count=1
```

Expected: FAIL because session start does not write Kiro config yet.

- [ ] **Step 3: Implement session start/stop integration**

In [ae-cli/internal/session/manager.go](/Users/admin/ai-efficiency/ae-cli/internal/session/manager.go), add Kiro alongside Codex/Claude:

```go
kiroAgentName := toolconfig.ManagedKiroAgentName
if err := toolconfig.WriteKiroSessionConfig(gc.workspaceRoot, toolconfig.KiroConfig{
	AgentName: kiroAgentName,
	SelfPath:  selfPath,
}); err != nil {
	return nil, rollback(fmt.Errorf("writing kiro config: %w", err))
}
rt.EnvBundle["AE_KIRO_AGENT_NAME"] = kiroAgentName
```

And in rollback / cleanup paths:

```go
_ = toolconfig.CleanupKiroSessionConfig(gc.workspaceRoot, toolconfig.ManagedKiroAgentName)
```

- [ ] **Step 4: Run the targeted session tests**

Run:

```bash
cd ae-cli
go test ./internal/session -run 'TestManagerStartWritesKiroManagedAgentConfig|TestManagerStopRemovesProxyRuntime' -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/session/manager.go ae-cli/internal/session/session_test.go
git commit -m "feat(ae-cli): manage kiro config with session lifecycle"
```

## Task 3: Launch Kiro With the Managed Agent

**Files:**
- Modify: [ae-cli/internal/shell/shell.go](/Users/admin/ai-efficiency/ae-cli/internal/shell/shell.go)
- Modify: [ae-cli/internal/shell/shell_test.go](/Users/admin/ai-efficiency/ae-cli/internal/shell/shell_test.go)
- Modify: [ae-cli/internal/dispatcher/dispatcher.go](/Users/admin/ai-efficiency/ae-cli/internal/dispatcher/dispatcher.go)

- [ ] **Step 1: Add a failing launcher test**

```go
func TestLaunchToolInstanceStartsKiroWithManagedAgent(t *testing.T) {
	// assert the split-window command receives:
	// kiro-cli --agent ae-session-managed
}
```

- [ ] **Step 2: Run the test to confirm it fails**

Run:

```bash
cd ae-cli
go test ./internal/shell -run 'TestLaunchToolInstanceStartsKiroWithManagedAgent$' -count=1
```

Expected: FAIL because Kiro currently launches with its raw configured args only.

- [ ] **Step 3: Implement Kiro argument injection**

In [ae-cli/internal/shell/shell.go](/Users/admin/ai-efficiency/ae-cli/internal/shell/shell.go):

```go
func augmentToolArgs(toolName string, args []string) []string {
	if toolName != "kiro" {
		return args
	}
	return append([]string{"--agent", toolconfig.ManagedKiroAgentName}, args...)
}
```

Use this helper before calling `shellSplitWindow`, and mirror the same behavior in [ae-cli/internal/dispatcher/dispatcher.go](/Users/admin/ai-efficiency/ae-cli/internal/dispatcher/dispatcher.go) so `ae-cli run kiro ...` and shell-launched panes behave the same way.

- [ ] **Step 4: Run shell/dispatcher tests**

Run:

```bash
cd ae-cli
go test ./internal/shell ./internal/dispatcher -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/shell/shell.go ae-cli/internal/shell/shell_test.go ae-cli/internal/dispatcher/dispatcher.go
git commit -m "feat(ae-cli): launch kiro with managed session agent"
```

## Task 4: Normalize Kiro Hook Events Into Backend Session Events

**Files:**
- Modify: [ae-cli/internal/proxy/hookforward.go](/Users/admin/ai-efficiency/ae-cli/internal/proxy/hookforward.go)
- Modify: [ae-cli/internal/proxy/hookforward_test.go](/Users/admin/ai-efficiency/ae-cli/internal/proxy/hookforward_test.go)
- Modify: [ae-cli/internal/proxy/server_test.go](/Users/admin/ai-efficiency/ae-cli/internal/proxy/server_test.go)

- [ ] **Step 1: Add a failing Kiro normalization test**

```go
func TestNormalizeHookEventKiroUserPromptSubmit(t *testing.T) {
	event, err := NormalizeHookEvent("kiro", map[string]any{
		"hook_event_name": "UserPromptSubmit",
		"prompt":          "review this patch",
	}, HookForwardOptions{
		WorkspaceID: "ws-kiro-1",
		CapturedAt:  time.Date(2026, 4, 8, 2, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NormalizeHookEvent: %v", err)
	}
	if event.EventType != "user_prompt_submit" {
		t.Fatalf("event_type = %q", event.EventType)
	}
}
```

- [ ] **Step 2: Run the proxy test to confirm it fails if mapping is missing**

Run:

```bash
cd ae-cli
go test ./internal/proxy -run 'TestNormalizeHookEventKiroUserPromptSubmit$' -count=1
```

Expected: FAIL if Kiro-specific payload handling is missing or malformed.

- [ ] **Step 3: Implement the minimal Kiro normalization support**

Update [ae-cli/internal/proxy/hookforward.go](/Users/admin/ai-efficiency/ae-cli/internal/proxy/hookforward.go) only as needed so Kiro payloads get:

```go
payload["source"] = "kiro_hook"
payload["workspace_id"] = strings.TrimSpace(opts.WorkspaceID)
payload["captured_at"] = capturedAt.Format(time.RFC3339)
```

If Kiro-specific event names differ, add the smallest normalization branch needed instead of introducing a new event pipeline.

- [ ] **Step 4: Run proxy tests**

Run:

```bash
cd ae-cli
go test ./internal/proxy -count=1
```

Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add ae-cli/internal/proxy/hookforward.go ae-cli/internal/proxy/hookforward_test.go ae-cli/internal/proxy/server_test.go
git commit -m "feat(ae-cli): forward kiro hook events"
```

## Task 5: Verify End-to-End Kiro Session Flow

**Files:**
- Modify: [docs/superpowers/specs/2026-04-02-local-session-proxy-design.md](/Users/admin/ai-efficiency/docs/superpowers/specs/2026-04-02-local-session-proxy-design.md)

- [ ] **Step 1: Update the existing design doc to reflect Kiro support**

Add a short note under the `非目标` / scope section:

```md
- Kiro is now supported for managed session hooks through a generated `.kiro/agents/ae-session-managed.json` config and `kiro-cli --agent ae-session-managed`.
```

- [ ] **Step 2: Run the full automated verification set**

Run:

```bash
cd backend && go test ./...
cd ../ae-cli && go test ./...
cd ../frontend && ./node_modules/.bin/vitest --run && ./node_modules/.bin/vue-tsc -b && ./node_modules/.bin/vite build
```

Expected: all PASS

- [ ] **Step 3: Run a manual Kiro smoke test**

Run:

```bash
ae-cli stop
ae-cli start
```

Then in the `ae` shell:

```text
@kiro
@kiro#1 hi
```

Verify:

```bash
sqlite3 backend/ai_efficiency.db "select id, event_type, source from session_events order by id desc limit 10;"
```

Expected:
- new rows with `source = 'kiro_hook'`
- session detail API includes those rows under `edges.session_events`

- [ ] **Step 4: Commit docs/test alignment**

```bash
git add docs/superpowers/specs/2026-04-02-local-session-proxy-design.md
git commit -m "docs(specs): note kiro session integration support"
```

## Spec Coverage Check

- Existing local-session-proxy spec already defines the shared session runtime, local proxy ingress, and hook normalization path. This plan extends that runtime to Kiro instead of inventing a parallel system.
- The one deliberate scope cut is provider routing nuance inside Kiro itself: the plan assumes Kiro requests continue to flow through the same runtime environment/session shell path already used for other tools, and limits new work to managed-agent launch + hook/event integration.

## Self-Review Notes

- No placeholders remain for file paths, commands, or commit boundaries.
- Tasks are independent and ordered: config writer → lifecycle wiring → launcher args → hook forwarding → verification/docs.
- Type names are consistent across tasks (`KiroConfig`, `ManagedKiroAgentName`, `WriteKiroSessionConfig`, `CleanupKiroSessionConfig`).
