# ae-cli Same-Tool Multi-Instance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Support launching multiple instances of the same tool in `ae-cli`, make `@tool <msg>` always open a fresh instance, and allow targeting existing instances via `@tool#N <msg>`.

**Architecture:** Add a per-session tool-pane registry under the ae-cli runtime directory so `shell`, `run`, `ps`, and `kill` share one source of truth for logical labels like `claude#2`. Keep `docs/architecture.md` as the current-state document, but implement the runtime behavior entirely inside `ae-cli` by extending session runtime persistence, tmux launch bookkeeping, and shell command parsing. Use TDD for the registry layer first, then wire existing command surfaces to it.

**Tech Stack:** Go, Cobra, Bubble Tea, tmux, JSON runtime files, Go testing

**Status:** ✅ 已完成（2026-04-12）

**Replay Status:** 历史完成记录。不要直接按本文逐 task 重跑；如需再次执行或扩展，请基于当前代码和最新 spec 重写执行计划。

**Source Of Truth:** 已实现行为以当前代码、`docs/architecture.md` 和相关最新 spec 为准。本文保留实施切片与验收轨迹。

> **Updated:** 2026-04-12 — 基于代码审查回填状态与 checkbox。

---

## File Structure

- Create: `ae-cli/internal/session/tool_panes.go`
  Responsibility: persist and mutate the per-session tool-pane registry stored under `~/.ae-cli/runtime/<session-id>/`.
- Create: `ae-cli/internal/session/tool_panes_test.go`
  Responsibility: cover registry persistence, monotonic numbering, lookup, removal, and pruning.
- Modify: `ae-cli/internal/dispatcher/dispatcher.go`
  Responsibility: register each tmux-launched tool pane in the shared registry.
- Modify: `ae-cli/internal/dispatcher/dispatcher_test.go`
  Responsibility: assert tmux launches register labeled instances.
- Modify: `ae-cli/internal/shell/shell.go`
  Responsibility: parse `@tool` vs `@tool#N`, always spawn on plain `@tool`, target existing labeled instances, broadcast only to existing live instances, and expose test injection points for pane launch / send-keys.
- Modify: `ae-cli/internal/shell/shell_test.go`
  Responsibility: cover multi-instance shell behavior and user-facing errors.
- Modify: `ae-cli/cmd/ps.go`
  Responsibility: render registry-backed labels (`claude#1`, `claude#2`) instead of raw pane-only listings, with testable output and tmux-list injection.
- Modify: `ae-cli/cmd/kill.go`
  Responsibility: best-effort prune a killed pane from the registry, with a testable tmux-kill injection.
- Modify: `ae-cli/cmd/version_test.go`
  Responsibility: update command-output tests for the new `ps` behavior and keep command usage expectations aligned.

### Task 1: Add the Session Tool-Pane Registry

**Files:**
- Create: `ae-cli/internal/session/tool_panes.go`
- Test: `ae-cli/internal/session/tool_panes_test.go`

- [x] **Step 1: Write the failing registry tests**

```go
package session

import (
	"os"
	"testing"
)

func TestRegisterToolPaneAssignsMonotonicInstanceNumbers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	first, err := RegisterToolPane("sess-1", "claude", "%11", "shell")
	if err != nil {
		t.Fatalf("RegisterToolPane first: %v", err)
	}
	second, err := RegisterToolPane("sess-1", "claude", "%12", "shell")
	if err != nil {
		t.Fatalf("RegisterToolPane second: %v", err)
	}
	if first.InstanceNo != 1 || second.InstanceNo != 2 {
		t.Fatalf("instance numbers = %d, %d, want 1, 2", first.InstanceNo, second.InstanceNo)
	}
	if got := FormatToolPaneLabel(*second); got != "claude#2" {
		t.Fatalf("label = %q, want %q", got, "claude#2")
	}
}

func TestFindToolPaneReturnsRequestedInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := RegisterToolPane("sess-1", "codex", "%21", "run"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := RegisterToolPane("sess-1", "codex", "%22", "run"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	inst, err := FindToolPane("sess-1", "codex", 2)
	if err != nil {
		t.Fatalf("FindToolPane: %v", err)
	}
	if inst.PaneID != "%22" {
		t.Fatalf("PaneID = %q, want %q", inst.PaneID, "%22")
	}
}

func TestRemoveToolPaneByPaneIDDeletesOnlyMatchingPane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := RegisterToolPane("sess-1", "claude", "%31", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := RegisterToolPane("sess-1", "claude", "%32", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	if err := RemoveToolPaneByPaneID("sess-1", "%31"); err != nil {
		t.Fatalf("RemoveToolPaneByPaneID: %v", err)
	}

	items, err := ListToolPanes("sess-1")
	if err != nil {
		t.Fatalf("ListToolPanes: %v", err)
	}
	if len(items) != 1 || items[0].PaneID != "%32" {
		t.Fatalf("remaining panes = %#v, want only %%32", items)
	}
}

func TestPruneToolPanesRemovesDeadEntriesWithoutReusingNumbers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if _, err := RegisterToolPane("sess-1", "claude", "%41", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := RegisterToolPane("sess-1", "claude", "%42", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	items, err := PruneToolPanes("sess-1", func(paneID string) bool {
		return paneID == "%42"
	})
	if err != nil {
		t.Fatalf("PruneToolPanes: %v", err)
	}
	if len(items) != 1 || items[0].PaneID != "%42" {
		t.Fatalf("items after prune = %#v, want only %%42", items)
	}

	next, err := RegisterToolPane("sess-1", "claude", "%43", "shell")
	if err != nil {
		t.Fatalf("RegisterToolPane after prune: %v", err)
	}
	if next.InstanceNo != 3 {
		t.Fatalf("InstanceNo = %d, want 3", next.InstanceNo)
	}
}

func TestReadToolPaneRegistryMissingFileReturnsEmptyRegistry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	reg, err := ReadToolPaneRegistry("sess-missing")
	if err != nil {
		t.Fatalf("ReadToolPaneRegistry: %v", err)
	}
	if len(reg.Instances) != 0 {
		t.Fatalf("Instances len = %d, want 0", len(reg.Instances))
	}
	if _, err := os.Stat(toolPaneRegistryPath("sess-missing")); !os.IsNotExist(err) {
		t.Fatalf("expected no registry file yet, stat err=%v", err)
	}
}
```

- [x] **Step 2: Run the session package tests to verify they fail**

Run: `go test ./internal/session -run 'TestRegisterToolPaneAssignsMonotonicInstanceNumbers|TestFindToolPaneReturnsRequestedInstance|TestRemoveToolPaneByPaneIDDeletesOnlyMatchingPane|TestPruneToolPanesRemovesDeadEntriesWithoutReusingNumbers|TestReadToolPaneRegistryMissingFileReturnsEmptyRegistry'`

Expected: FAIL with `undefined: RegisterToolPane`, `undefined: FindToolPane`, and missing registry helpers.

- [x] **Step 3: Write the minimal registry implementation**

```go
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ToolPaneRecord struct {
	ToolName     string    `json:"tool_name"`
	InstanceNo   int       `json:"instance_no"`
	PaneID       string    `json:"pane_id"`
	LaunchSource string    `json:"launch_source,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type ToolPaneRegistry struct {
	NextInstanceByTool map[string]int  `json:"next_instance_by_tool"`
	Instances          []ToolPaneRecord `json:"instances"`
}

func toolPaneRegistryPath(sessionID string) string {
	return filepath.Join(runtimeDir(sessionID), "tool-panes.json")
}

func ReadToolPaneRegistry(sessionID string) (*ToolPaneRegistry, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, fmt.Errorf("session_id is required")
	}
	data, err := os.ReadFile(toolPaneRegistryPath(sessionID))
	if err != nil {
		if os.IsNotExist(err) {
			return &ToolPaneRegistry{
				NextInstanceByTool: map[string]int{},
				Instances:          []ToolPaneRecord{},
			}, nil
		}
		return nil, fmt.Errorf("reading tool pane registry: %w", err)
	}
	var reg ToolPaneRegistry
	if err := json.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parsing tool pane registry: %w", err)
	}
	if reg.NextInstanceByTool == nil {
		reg.NextInstanceByTool = map[string]int{}
	}
	if reg.Instances == nil {
		reg.Instances = []ToolPaneRecord{}
	}
	return &reg, nil
}

func WriteToolPaneRegistry(sessionID string, reg *ToolPaneRegistry) error {
	if reg == nil {
		return fmt.Errorf("tool pane registry is nil")
	}
	if err := os.MkdirAll(runtimeDir(sessionID), 0o700); err != nil {
		return fmt.Errorf("creating runtime dir: %w", err)
	}
	data, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling tool pane registry: %w", err)
	}
	if err := os.WriteFile(toolPaneRegistryPath(sessionID), data, 0o600); err != nil {
		return fmt.Errorf("writing tool pane registry: %w", err)
	}
	return nil
}

func FormatToolPaneLabel(rec ToolPaneRecord) string {
	return fmt.Sprintf("%s#%d", rec.ToolName, rec.InstanceNo)
}

func RegisterToolPane(sessionID, toolName, paneID, source string) (*ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	toolName = strings.TrimSpace(toolName)
	next := reg.NextInstanceByTool[toolName] + 1
	rec := ToolPaneRecord{
		ToolName:     toolName,
		InstanceNo:   next,
		PaneID:       strings.TrimSpace(paneID),
		LaunchSource: strings.TrimSpace(source),
		CreatedAt:    time.Now().UTC(),
	}
	reg.NextInstanceByTool[toolName] = next
	reg.Instances = append(reg.Instances, rec)
	if err := WriteToolPaneRegistry(sessionID, reg); err != nil {
		return nil, err
	}
	return &rec, nil
}

func FindToolPane(sessionID, toolName string, instanceNo int) (*ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	for _, rec := range reg.Instances {
		if rec.ToolName == toolName && rec.InstanceNo == instanceNo {
			copy := rec
			return &copy, nil
		}
	}
	return nil, fmt.Errorf("tool instance %s#%d not found", toolName, instanceNo)
}

func ListToolPanes(sessionID string) ([]ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	return append([]ToolPaneRecord(nil), reg.Instances...), nil
}

func RemoveToolPaneByPaneID(sessionID, paneID string) error {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return err
	}
	keep := reg.Instances[:0]
	for _, rec := range reg.Instances {
		if rec.PaneID == strings.TrimSpace(paneID) {
			continue
		}
		keep = append(keep, rec)
	}
	reg.Instances = append([]ToolPaneRecord(nil), keep...)
	return WriteToolPaneRegistry(sessionID, reg)
}

func PruneToolPanes(sessionID string, alive func(string) bool) ([]ToolPaneRecord, error) {
	reg, err := ReadToolPaneRegistry(sessionID)
	if err != nil {
		return nil, err
	}
	keep := reg.Instances[:0]
	for _, rec := range reg.Instances {
		if alive != nil && !alive(rec.PaneID) {
			continue
		}
		keep = append(keep, rec)
	}
	reg.Instances = append([]ToolPaneRecord(nil), keep...)
	if err := WriteToolPaneRegistry(sessionID, reg); err != nil {
		return nil, err
	}
	return append([]ToolPaneRecord(nil), reg.Instances...), nil
}
```

- [x] **Step 4: Run the session package tests to verify they pass**

Run: `go test ./internal/session -run 'TestRegisterToolPaneAssignsMonotonicInstanceNumbers|TestFindToolPaneReturnsRequestedInstance|TestRemoveToolPaneByPaneIDDeletesOnlyMatchingPane|TestPruneToolPanesRemovesDeadEntriesWithoutReusingNumbers|TestReadToolPaneRegistryMissingFileReturnsEmptyRegistry'`

Expected: PASS with all 5 tests green.

- [x] **Step 5: Commit the registry layer**

```bash
git add ae-cli/internal/session/tool_panes.go ae-cli/internal/session/tool_panes_test.go
git commit -m "feat(ae-cli): add session tool pane registry"
```

### Task 2: Register tmux-Launched Tool Instances from `ae-cli run`

**Files:**
- Modify: `ae-cli/internal/dispatcher/dispatcher.go`
- Modify: `ae-cli/internal/dispatcher/dispatcher_test.go`
- Test: `ae-cli/internal/session/tool_panes_test.go`

- [x] **Step 1: Write the failing dispatcher test for registry registration**

```go
func TestRunWithTmuxRegistersToolPane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"echo-tool": {Command: "echo", Args: []string{"hello"}},
		},
	}

	var recordedSession string
	var recordedTool string
	origSplit := tmuxSplitWindow
	tmuxSplitWindow = func(sessionName string, toolName string, command string, args []string, env map[string]string, unsetKeys []string) (string, error) {
		recordedSession = sessionName
		recordedTool = toolName
		return "%91", nil
	}
	t.Cleanup(func() { tmuxSplitWindow = origSplit })

	d := New(cfg, newTestClient(t))
	if err := d.Run("sess-run", "echo-tool", nil, "tmux-session-1"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	items, err := session.ListToolPanes("sess-run")
	if err != nil {
		t.Fatalf("ListToolPanes: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].PaneID != "%91" || items[0].ToolName != "echo-tool" || items[0].LaunchSource != "run" {
		t.Fatalf("unexpected registry item: %#v", items[0])
	}
	if recordedSession != "tmux-session-1" || recordedTool != "echo-tool" {
		t.Fatalf("split called with (%q, %q), want (%q, %q)", recordedSession, recordedTool, "tmux-session-1", "echo-tool")
	}
}
```

- [x] **Step 2: Run the dispatcher test to verify it fails**

Run: `go test ./internal/dispatcher -run TestRunWithTmuxRegistersToolPane`

Expected: FAIL because `Dispatcher.Run` currently discards the returned pane id and never writes the registry.

- [x] **Step 3: Register panes after tmux launch**

```go
if tmuxSession != "" {
	unsetKeys := []string{}
	if proxyClaudeEnvActive(runtimeEnv) {
		unsetKeys = append(unsetKeys, "ANTHROPIC_API_KEY")
	}
	if proxyCodexEnvActive(runtimeEnv) {
		unsetKeys = append(unsetKeys, "OPENAI_API_KEY", "OPENAI_BASE_URL")
	}
	if len(runtimeEnv) > 0 {
		_ = tmuxSetEnvironment(tmuxSession, runtimeEnv)
		if len(unsetKeys) > 0 {
			_ = tmuxUnsetEnvironment(tmuxSession, unsetKeys)
		}
	}

	paneID, err := tmuxSplitWindow(tmuxSession, toolName, toolCfg.Command, args, runtimeEnv, unsetKeys)
	if err != nil {
		return fmt.Errorf("splitting tmux pane: %w", err)
	}
	if _, err := session.RegisterToolPane(sessionID, toolName, paneID, "run"); err != nil {
		return fmt.Errorf("registering tool pane: %w", err)
	}

	fmt.Printf("Tool %q launched in tmux session %q\n", toolName, tmuxSession)
	fmt.Printf("Run 'ae-cli attach' to view all panes.\n")
	// existing invocation recording stays unchanged
}
```

- [x] **Step 4: Run the dispatcher tests to verify they pass**

Run: `go test ./internal/dispatcher -run 'TestRunWithTmuxRegistersToolPane|TestRunWithTmux|TestRunWithTmuxUnsets'`

Expected: PASS with the new registration test and existing tmux-launch tests all green.

- [x] **Step 5: Commit the `run` integration**

```bash
git add ae-cli/internal/dispatcher/dispatcher.go ae-cli/internal/dispatcher/dispatcher_test.go
git commit -m "feat(ae-cli): register tmux tool panes for run"
```

### Task 3: Make `shell` Spawn New Instances by Default and Target `@tool#N`

**Files:**
- Modify: `ae-cli/internal/shell/shell.go`
- Modify: `ae-cli/internal/shell/shell_test.go`

- [x] **Step 1: Write the failing shell tests for spawn-vs-target semantics**

```go
func TestHandleDirectedLaunchesNewInstanceForPlainTool(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-new"
	m := newModel(s)

	origSplit := shellSplitWindow
	var paneSeq int
	shellSplitWindow = func(sessionName, toolName, command string, args []string) (string, error) {
		paneSeq++
		return fmt.Sprintf("%%%d", 100+paneSeq), nil
	}
	t.Cleanup(func() { shellSplitWindow = origSplit })

	m.handleDirected("@claude hello")
	m.handleDirected("@claude again")

	items, err := session.ListToolPanes("test-id")
	if err != nil {
		t.Fatalf("ListToolPanes: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if session.FormatToolPaneLabel(items[1]) != "claude#2" {
		t.Fatalf("label = %q, want %q", session.FormatToolPaneLabel(items[1]), "claude#2")
	}
}

func TestHandleDirectedTargetsIndexedInstance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-target"
	if _, err := session.RegisterToolPane("test-id", "claude", "%201", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := session.RegisterToolPane("test-id", "claude", "%202", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	var gotTarget string
	origSend := shellSendKeys
	shellSendKeys = func(paneID, msg string) error {
		gotTarget = paneID
		return nil
	}
	t.Cleanup(func() { shellSendKeys = origSend })

	m := newModel(s)
	m.handleDirected("@claude#2 hello")

	if gotTarget != "%202" {
		t.Fatalf("target pane = %q, want %q", gotTarget, "%202")
	}
}

func TestHandleDirectedMissingIndexedInstanceShowsHelpfulError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-missing"
	m := newModel(s)
	m.lines = nil

	m.handleDirected("@claude#2 hello")

	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "claude#2") && strings.Contains(line, "not found") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected missing instance error mentioning claude#2")
	}
}

func TestBroadcastSendsToExistingInstancesOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
		"codex":  {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-broadcast"
	if _, err := session.RegisterToolPane("test-id", "claude", "%301", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := session.RegisterToolPane("test-id", "claude", "%302", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	var targets []string
	origSend := shellSendKeys
	shellSendKeys = func(paneID, msg string) error {
		targets = append(targets, paneID)
		return nil
	}
	t.Cleanup(func() { shellSendKeys = origSend })

	m := newModel(s)
	m.broadcast("hello all")

	if len(targets) != 2 {
		t.Fatalf("broadcast targets = %v, want 2 existing instances", targets)
	}
}

func TestAppendPanesShowsToolLabels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-ps"
	if _, err := session.RegisterToolPane("test-id", "claude", "%401", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{{ID: "%401", Tool: "claude", Active: true}}, nil
	}
	t.Cleanup(func() { shellListPanes = origList })

	m := newModel(s)
	m.lines = nil
	m.appendPanes()

	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "claude#1") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected appendPanes output to include claude#1")
	}
}
```

- [x] **Step 2: Run the shell tests to verify they fail**

Run: `go test ./internal/shell -run 'TestHandleDirectedLaunchesNewInstanceForPlainTool|TestHandleDirectedTargetsIndexedInstance|TestHandleDirectedMissingIndexedInstanceShowsHelpfulError|TestBroadcastSendsToExistingInstancesOnly'`

Expected: FAIL because the shell still uses a single `toolPanes` map keyed only by tool name and has no `#N` parser.

- [x] **Step 3: Replace single-pane shell state with registry-backed routing**

```go
type directedTarget struct {
	ToolName   string
	InstanceNo int
	Message    string
}

var (
	shellSplitWindow = tmux.SplitWindow
	shellListPanes   = tmux.ListPanes
	shellSendKeys    = func(paneID, msg string) error {
		return exec.Command("tmux", "send-keys", "-t", paneID, msg, "Enter").Run()
	}
)

func parseDirectedTarget(line string) (directedTarget, error) {
	parts := strings.SplitN(strings.TrimPrefix(line, "@"), " ", 2)
	head := parts[0]
	msg := ""
	if len(parts) == 2 {
		msg = parts[1]
	}

	target := directedTarget{Message: msg}
	if rawTool, rawIndex, ok := strings.Cut(head, "#"); ok {
		target.ToolName = rawTool
		n, err := strconv.Atoi(rawIndex)
		if err != nil || n <= 0 {
			return directedTarget{}, fmt.Errorf("invalid tool instance selector %q", head)
		}
		target.InstanceNo = n
		return target, nil
	}

	target.ToolName = head
	return target, nil
}

func (m *model) handleDirected(line string) {
	target, err := parseDirectedTarget(line)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31m%v\033[0m", err))
		return
	}
	if _, ok := m.shell.config.Tools[target.ToolName]; !ok {
		m.queueLine(fmt.Sprintf("\033[31mUnknown tool: %s\033[0m", target.ToolName))
		m.queueLine("Available: " + m.shell.toolNames())
		return
	}
	if target.InstanceNo == 0 {
		m.launchToolInstance(target.ToolName, target.Message)
		return
	}
	m.sendToExistingTool(target.ToolName, target.InstanceNo, target.Message)
}

func (m *model) launchToolInstance(toolName, msg string) {
	toolCfg := m.shell.config.Tools[toolName]
	paneID, err := shellSplitWindow(m.shell.state.TmuxSession, toolName, toolCfg.Command, toolCfg.Args)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to launch %s: %v\033[0m", toolName, err))
		return
	}
	rec, err := session.RegisterToolPane(m.shell.state.ID, toolName, paneID, "shell")
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to register %s pane: %v\033[0m", toolName, err))
		return
	}
	m.queueLine(fmt.Sprintf("Launched %s in pane %s as %s", toolName, paneID, session.FormatToolPaneLabel(*rec)))
	if msg != "" {
		m.sendKeys(paneID, msg, session.FormatToolPaneLabel(*rec))
	}
}

func (m *model) sendToExistingTool(toolName string, instanceNo int, msg string) {
	items, err := session.PruneToolPanes(m.shell.state.ID, m.shell.paneAlive)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to load tool panes: %v\033[0m", err))
		return
	}
	for _, rec := range items {
		if rec.ToolName == toolName && rec.InstanceNo == instanceNo {
			if msg != "" {
				m.sendKeys(rec.PaneID, msg, session.FormatToolPaneLabel(rec))
			}
			return
		}
	}
	m.queueLine(fmt.Sprintf("\033[31mTool instance %s#%d not found.\033[0m", toolName, instanceNo))
}

func (m *model) broadcast(msg string) {
	items, err := session.PruneToolPanes(m.shell.state.ID, m.shell.paneAlive)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to load tool panes: %v\033[0m", err))
		return
	}
	if len(items) == 0 {
		m.queueLine("\033[33mNo running tool instances. Use @tool <msg> to start one.\033[0m")
		return
	}
	for _, rec := range items {
		label := session.FormatToolPaneLabel(rec)
		m.queueLine(fmt.Sprintf("→ \033[32m%s\033[0m", label))
		m.sendKeys(rec.PaneID, msg, label)
	}
}

func (m *model) sendKeys(paneID, msg, label string) {
	if err := shellSendKeys(paneID, msg); err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to send to %s: %v\033[0m", label, err))
	}
}

func (m *model) appendPanes() {
	panes, err := shellListPanes(m.shell.state.TmuxSession)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to list panes: %v\033[0m", err))
		return
	}
	items, err := session.PruneToolPanes(m.shell.state.ID, m.shell.paneAlive)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to load tool panes: %v\033[0m", err))
		return
	}
	paneByID := map[string]tmux.Pane{}
	for _, pane := range panes {
		paneByID[pane.ID] = pane
	}
	m.queueLine(fmt.Sprintf("%-16s %-12s %-20s %s", "LABEL", "PANE ID", "COMMAND", "ACTIVE"))
	for _, rec := range items {
		pane := paneByID[rec.PaneID]
		active := ""
		if pane.Active {
			active = "*"
		}
		m.queueLine(fmt.Sprintf("%-16s %-12s %-20s %s", session.FormatToolPaneLabel(rec), rec.PaneID, pane.Tool, active))
	}
}

func (s *Shell) activeToolPaneCount() int {
	if s.state == nil {
		return 0
	}
	items, err := session.PruneToolPanes(s.state.ID, s.paneAlive)
	if err != nil {
		return 0
	}
	return len(items)
}
```

- [x] **Step 4: Run the shell package tests to verify they pass**

Run: `go test ./internal/shell -run 'TestHandleDirectedLaunchesNewInstanceForPlainTool|TestHandleDirectedTargetsIndexedInstance|TestHandleDirectedMissingIndexedInstanceShowsHelpfulError|TestBroadcastSendsToExistingInstancesOnly|TestAppendPanesShowsToolLabels|TestSendToToolWithTmux|TestBroadcastWithTmux'`

Expected: PASS with the new multi-instance tests and the updated tmux tests.

- [x] **Step 5: Commit the shell behavior change**

```bash
git add ae-cli/internal/shell/shell.go ae-cli/internal/shell/shell_test.go
git commit -m "feat(ae-cli): support multi-instance shell tool panes"
```

### Task 4: Label `ps` Output and Prune the Registry on `kill`

**Files:**
- Modify: `ae-cli/cmd/ps.go`
- Modify: `ae-cli/cmd/kill.go`
- Modify: `ae-cli/cmd/version_test.go`

- [x] **Step 1: Write the failing command tests for labeled `ps` and registry-pruning `kill`**

```go
func TestPsCommandShowsToolLabelsFromRegistry(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cleanup := setupTestGlobals(t, srv)
	defer cleanup()

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	state := session.State{ID: "sess-ps-label", Repo: "org/repo", Branch: "main", TmuxSession: "tmux-ps-label"}
	data, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := session.RegisterToolPane("sess-ps-label", "claude", "%401", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	origListPanes := listPanes
	listPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{{ID: "%401", Tool: "claude", Active: true}}, nil
	}
	t.Cleanup(func() { listPanes = origListPanes })

	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	psCmd.SetOut(buf)
	psCmd.SetErr(buf)

	if err := psCmd.RunE(psCmd, nil); err != nil {
		t.Fatalf("psCmd.RunE: %v", err)
	}

	if !strings.Contains(buf.String(), "claude#1") {
		t.Fatalf("ps output = %q, want label claude#1", buf.String())
	}
}

func TestKillCommandPrunesToolPaneRegistry(t *testing.T) {
	origHome := os.Getenv("HOME")
	tmpHome := t.TempDir()
	os.Setenv("HOME", tmpHome)
	defer os.Setenv("HOME", origHome)

	if _, err := session.RegisterToolPane("sess-kill", "claude", "%501", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	stateDir := filepath.Join(tmpHome, ".ae-cli")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	state := session.State{ID: "sess-kill", Repo: "org/repo", Branch: "main"}
	data, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(filepath.Join(stateDir, "current-session.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	origKillPane := killPane
	killPane = func(paneID string) error { return nil }
	t.Cleanup(func() { killPane = origKillPane })

	if err := killCmd.RunE(killCmd, []string{"%501"}); err != nil {
		t.Fatalf("killCmd.RunE: %v", err)
	}

	items, err := session.ListToolPanes("sess-kill")
	if err != nil {
		t.Fatalf("ListToolPanes: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("registry items = %#v, want empty after kill", items)
	}
}
```

- [x] **Step 2: Run the command tests to verify they fail**

Run: `go test ./cmd -run 'TestPsCommandShowsToolLabelsFromRegistry|TestKillCommandPrunesToolPaneRegistry'`

Expected: FAIL because `ps` still prints only raw pane data and `kill` does not touch the registry.

- [x] **Step 3: Render registry labels in `ps` and prune the registry on `kill`**

```go
// ae-cli/cmd/ps.go
var listPanes = tmux.ListPanes

panes, err := listPanes(state.TmuxSession)
if err != nil {
	return fmt.Errorf("listing panes: %w", err)
}

items, err := session.PruneToolPanes(state.ID, func(paneID string) bool {
	for _, pane := range panes {
		if pane.ID == paneID {
			return true
		}
	}
	return false
})
if err != nil {
	return fmt.Errorf("loading tool panes: %w", err)
}

paneByID := map[string]tmux.Pane{}
for _, pane := range panes {
	paneByID[pane.ID] = pane
}

out := cmd.OutOrStdout()
fmt.Fprintf(out, "%-16s %-12s %-20s %s\n", "LABEL", "PANE ID", "COMMAND", "ACTIVE")
for _, rec := range items {
	pane := paneByID[rec.PaneID]
	active := ""
	if pane.Active {
		active = "*"
	}
	fmt.Fprintf(out, "%-16s %-12s %-20s %s\n", session.FormatToolPaneLabel(rec), rec.PaneID, pane.Tool, active)
}

// ae-cli/cmd/kill.go
var killPane = tmux.KillPane

if err := killPane(paneID); err != nil {
	return fmt.Errorf("killing pane %s: %w", paneID, err)
}
mgr := session.NewManager(apiClient, cfg)
if state, err := mgr.Current(); err == nil && state != nil {
	_ = session.RemoveToolPaneByPaneID(state.ID, paneID)
}
fmt.Fprintf(cmd.OutOrStdout(), "Pane %s killed.\n", paneID)
```

- [x] **Step 4: Run the command tests to verify they pass**

Run: `go test ./cmd -run 'TestPsCommandShowsToolLabelsFromRegistry|TestKillCommandPrunesToolPaneRegistry|TestPsCommandNoSession|TestPsCommandNoTmux'`

Expected: PASS with labeled output and registry pruning covered.

- [x] **Step 5: Commit the CLI surface changes**

```bash
git add ae-cli/cmd/ps.go ae-cli/cmd/kill.go ae-cli/cmd/version_test.go
git commit -m "feat(ae-cli): label and prune tool panes"
```

### Task 5: Final Verification and Command Help Polish

**Files:**
- Modify: `ae-cli/internal/shell/shell.go`
- Modify: `ae-cli/internal/shell/shell_test.go`
- Modify: `ae-cli/cmd/version_test.go`

- [x] **Step 1: Write the failing tests for help text and no-instance broadcast**

```go
func TestHandleDirectedInvalidSelectorShowsError(t *testing.T) {
	m := newTestModel(map[string]config.ToolConfig{"claude": {Command: "claude"}})
	m.lines = nil

	m.handleDirected("@claude#x hello")

	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "invalid tool instance selector") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected invalid selector error")
	}
}

func TestBroadcastWithNoRunningInstancesShowsGuidance(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	m := newTestModel(map[string]config.ToolConfig{"claude": {Command: "claude"}})
	m.lines = nil
	m.broadcast("hello")

	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "No running tool instances") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected no-running-instances guidance")
	}
}
```

- [x] **Step 2: Run the shell tests to verify they fail**

Run: `go test ./internal/shell -run 'TestHandleDirectedInvalidSelectorShowsError|TestBroadcastWithNoRunningInstancesShowsGuidance'`

Expected: FAIL before help/error-path polish is complete.

- [x] **Step 3: Update banner/help text and finalize error handling**

```go
func (m *model) queueBanner() {
	m.queueLine("\033[1m=== AE Agent Shell ===\033[0m")
	m.queueLine("Tools: " + m.shell.toolNames())
	m.queueLine("")
	m.queueLine("Usage:")
	m.queueLine("  @claude <msg>    Launch a new claude instance")
	m.queueLine("  @claude#2 <msg> Send to an existing claude instance")
	m.queueLine("  @all <msg>       Broadcast to all running tool instances")
	m.queueLine("  ps               List running labeled panes")
	m.queueLine("  exit             Quit shell")
	m.queueLine("")
}
```

- [x] **Step 4: Run the focused tests and the full ae-cli suite**

Run: `go test ./internal/shell -run 'TestHandleDirectedInvalidSelectorShowsError|TestBroadcastWithNoRunningInstancesShowsGuidance|TestHelpCommand'`
Expected: PASS

Run: `go test ./...`
Expected: PASS across the full `ae-cli` module.

- [x] **Step 5: Commit the polish and verification**

```bash
git add ae-cli/internal/shell/shell.go ae-cli/internal/shell/shell_test.go ae-cli/cmd/version_test.go
git commit -m "test(ae-cli): verify multi-instance tool shell flow"
```

## Self-Review

- Spec coverage: The plan covers default multi-open behavior (`@tool`), existing-instance targeting (`@tool#N`), shared runtime registry, `run` integration, `ps` labeling, `kill` pruning, broadcast semantics, and focused/full verification.
- Placeholder scan: No `TODO`/`TBD` markers remain; every code-changing step contains concrete code snippets and exact commands.
- Type consistency: The plan consistently uses `ToolPaneRecord`, `ToolPaneRegistry`, `RegisterToolPane`, `FindToolPane`, `ListToolPanes`, `RemoveToolPaneByPaneID`, `PruneToolPanes`, and `FormatToolPaneLabel` across all tasks.
