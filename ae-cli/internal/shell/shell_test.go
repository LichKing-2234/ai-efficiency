package shell

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// --- helpers ---

var testShellSeq atomic.Int64

func newTestShell(tools map[string]config.ToolConfig) *Shell {
	if tools == nil {
		tools = map[string]config.ToolConfig{}
	}
	cfg := &config.Config{Tools: tools}
	state := &session.State{
		ID:          fmt.Sprintf("test-id-%d", testShellSeq.Add(1)),
		TmuxSession: "ae-test",
	}
	return New(cfg, state)
}

func newTestModel(tools map[string]config.ToolConfig) model {
	return newModel(newTestShell(tools))
}

func sendKey(m model, keyType tea.KeyType) (model, tea.Cmd) {
	updated, cmd := m.Update(tea.KeyMsg{Type: keyType})
	return updated.(model), cmd
}

func sendRunes(m model, s string) (model, tea.Cmd) {
	var cmd tea.Cmd
	for _, r := range s {
		var c tea.Cmd
		updated, c := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = updated.(model)
		if c != nil {
			cmd = c
		}
	}
	return m, cmd
}

func sendLine(m model, s string) (model, tea.Cmd) {
	m, _ = sendRunes(m, s)
	return sendKey(m, tea.KeyEnter)
}

// --- Shell constructor tests ---

func TestNew(t *testing.T) {
	cfg := &config.Config{
		Tools: map[string]config.ToolConfig{
			"claude": {Command: "claude", Args: []string{"-p"}},
			"codex":  {Command: "codex", Args: []string{}},
		},
	}
	state := &session.State{
		ID: "test-id", Repo: "org/repo", Branch: "main", TmuxSession: "ae-test",
	}
	s := New(cfg, state)
	if s == nil {
		t.Fatal("expected non-nil shell")
	}
	if s.config != cfg {
		t.Error("shell config mismatch")
	}
	if s.state != state {
		t.Error("shell state mismatch")
	}
	if s.router != nil {
		t.Error("router should be nil without sub2api config")
	}
}

func TestNewWithoutTools(t *testing.T) {
	s := New(&config.Config{Tools: map[string]config.ToolConfig{}}, &session.State{ID: "test-id"})
	if s == nil {
		t.Fatal("expected non-nil shell")
	}
}

func TestNewWithNilTools(t *testing.T) {
	s := New(&config.Config{}, &session.State{ID: "test-id"})
	if s == nil {
		t.Fatal("expected non-nil shell")
	}
}

func TestNewWithRouter(t *testing.T) {
	os.Setenv("TEST_SHELL_API_KEY", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_SHELL_API_KEY") })

	cfg := &config.Config{
		Sub2api: config.Sub2apiConfig{URL: "http://localhost:9999", APIKeyEnv: "TEST_SHELL_API_KEY", Model: "test-model"},
		Tools:   map[string]config.ToolConfig{"claude": {Command: "claude"}},
	}
	s := New(cfg, &session.State{ID: "test-id"})
	if s.router == nil {
		t.Error("router should be non-nil")
	}
}

func TestNewWithRouterDefaultModel(t *testing.T) {
	os.Setenv("TEST_SHELL_API_KEY2", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_SHELL_API_KEY2") })

	cfg := &config.Config{
		Sub2api: config.Sub2apiConfig{URL: "http://localhost:9999", APIKeyEnv: "TEST_SHELL_API_KEY2"},
		Tools:   map[string]config.ToolConfig{"claude": {Command: "claude"}},
	}
	s := New(cfg, &session.State{ID: "test-id"})
	if s.router == nil {
		t.Error("router should be non-nil with default model")
	}
}

func TestNewWithRouterNoURL(t *testing.T) {
	os.Setenv("TEST_SHELL_API_KEY3", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_SHELL_API_KEY3") })

	cfg := &config.Config{
		Sub2api: config.Sub2apiConfig{APIKeyEnv: "TEST_SHELL_API_KEY3"},
		Tools:   map[string]config.ToolConfig{"claude": {Command: "claude"}},
	}
	s := New(cfg, &session.State{ID: "test-id"})
	if s.router != nil {
		t.Error("router should be nil when URL is empty")
	}
}

func TestNewWithRouterNoAPIKey(t *testing.T) {
	cfg := &config.Config{
		Sub2api: config.Sub2apiConfig{URL: "http://localhost:9999", APIKeyEnv: "TEST_SHELL_NONEXISTENT_KEY"},
		Tools:   map[string]config.ToolConfig{"claude": {Command: "claude"}},
	}
	s := New(cfg, &session.State{ID: "test-id"})
	if s.router != nil {
		t.Error("router should be nil when API key env var is not set")
	}
}

// --- Shared helper tests ---

func TestToolNames(t *testing.T) {
	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "claude"}, "codex": {Command: "codex"},
	})
	names := s.toolNames()
	if !strings.Contains(names, "claude") || !strings.Contains(names, "codex") {
		t.Errorf("toolNames = %q, should contain claude and codex", names)
	}
}

func TestToolNamesEmpty(t *testing.T) {
	s := newTestShell(nil)
	if names := s.toolNames(); names != "" {
		t.Errorf("toolNames = %q, want empty", names)
	}
}

func TestToolNamesSingle(t *testing.T) {
	s := newTestShell(map[string]config.ToolConfig{"claude": {Command: "claude"}})
	if names := s.toolNames(); names != "claude" {
		t.Errorf("toolNames = %q, want %q", names, "claude")
	}
}

func TestPaneAliveNonExistent(t *testing.T) {
	s := newTestShell(nil)
	if s.paneAlive("%999999") {
		t.Error("non-existent pane should not be alive")
	}
}

// --- Model banner test ---

func TestBannerQueuedByHelp(t *testing.T) {
	m := newTestModel(map[string]config.ToolConfig{"claude": {Command: "claude"}})
	// newModel no longer queues banner (printed before bubbletea starts)
	// but "help" command should queue it
	m.handleCommand("help")
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "AE Agent Shell") {
			found = true
			break
		}
	}
	if !found {
		t.Error("help command should queue banner lines")
	}
}

// --- Double Ctrl+C tests ---

func TestDoubleCtrlCExits(t *testing.T) {
	m := newTestModel(nil)
	m.lines = nil // clear banner

	// First Ctrl+C — should NOT quit
	m, cmd := sendKey(m, tea.KeyCtrlC)
	if cmd == nil {
		t.Fatal("first Ctrl+C should return a Println cmd")
	}
	if !m.pendingExit {
		t.Error("pendingExit should be true after first Ctrl+C")
	}

	// Second Ctrl+C — should quit
	m, cmd = sendKey(m, tea.KeyCtrlC)
	if cmd == nil {
		t.Fatal("second Ctrl+C should return tea.Quit")
	}
	if !m.quitting {
		t.Error("should be quitting after second Ctrl+C")
	}
}

func TestCtrlCThenInputResetsExit(t *testing.T) {
	m := newTestModel(nil)
	m.lines = nil

	// First Ctrl+C
	m, _ = sendKey(m, tea.KeyCtrlC)
	if !m.pendingExit {
		t.Fatal("pendingExit should be true")
	}

	// Type something — should reset pendingExit
	m, _ = sendRunes(m, "a")
	if m.pendingExit {
		t.Error("pendingExit should be reset after typing")
	}
}

func TestSingleCtrlCDoesNotExit(t *testing.T) {
	m := newTestModel(nil)
	m.lines = nil

	// Single Ctrl+C
	m, _ = sendKey(m, tea.KeyCtrlC)
	if m.quitting {
		t.Error("single Ctrl+C should not quit")
	}

	// Then "exit" command
	m, cmd := sendLine(m, "exit")
	if cmd == nil {
		t.Error("exit command should return a cmd")
	}
	if !m.quitting {
		t.Error("exit should set quitting")
	}
}

// --- Double Ctrl+D tests ---

func TestDoubleCtrlDExits(t *testing.T) {
	m := newTestModel(nil)
	m.lines = nil

	// First Ctrl+D on empty input
	m, cmd := sendKey(m, tea.KeyCtrlD)
	if cmd == nil {
		t.Fatal("first Ctrl+D should return a Println cmd")
	}
	if !m.pendingExit {
		t.Error("pendingExit should be true after first Ctrl+D")
	}

	// Second Ctrl+D
	m, cmd = sendKey(m, tea.KeyCtrlD)
	if cmd == nil {
		t.Fatal("second Ctrl+D should return tea.Quit")
	}
	if !m.quitting {
		t.Error("should be quitting after second Ctrl+D")
	}
}

func TestCtrlDIgnoredWithInput(t *testing.T) {
	m := newTestModel(nil)
	m.lines = nil

	// Type something first
	m, _ = sendRunes(m, "hello")

	// Ctrl+D with text — should NOT trigger exit
	m, _ = sendKey(m, tea.KeyCtrlD)
	if m.pendingExit {
		t.Error("pendingExit should not be set when input has text")
	}
}

// --- Command tests ---

func TestExitCommand(t *testing.T) {
	m := newTestModel(nil)
	m.lines = nil

	m, cmd := sendLine(m, "exit")
	if cmd == nil {
		t.Error("exit should return a cmd")
	}
	if !m.quitting {
		t.Error("exit should set quitting")
	}
}

func TestQuitCommand(t *testing.T) {
	m := newTestModel(nil)
	m.lines = nil

	m, cmd := sendLine(m, "quit")
	if cmd == nil {
		t.Error("quit should return a cmd")
	}
	if !m.quitting {
		t.Error("quit should set quitting")
	}
}

func TestHelpCommand(t *testing.T) {
	m := newTestModel(map[string]config.ToolConfig{"claude": {Command: "claude"}})
	m.lines = nil

	// Call handleCommand directly to check queued lines before flush
	m.handleCommand("help")
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "AE Agent Shell") {
			found = true
			break
		}
	}
	if !found {
		t.Error("help should queue banner lines")
	}
}

func TestUnknownToolDirected(t *testing.T) {
	m := newTestModel(map[string]config.ToolConfig{"claude": {Command: "claude"}})
	m.lines = nil

	m.handleCommand("@unknown-tool hello")
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "Unknown tool") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected unknown tool error in queued lines")
	}
}

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
	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{}, nil
	}
	t.Cleanup(func() { shellListPanes = origList })
	m.broadcast("hello")

	found := false
	expected := "No running tool instances. Use @tool <msg> to start one."
	for _, line := range m.lines {
		if strings.Contains(line, expected) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected guidance containing %q, got %q", expected, m.lines)
	}
}

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

	items, err := session.ListToolPanes(s.state.ID)
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
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%201", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%202", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	var gotTarget string
	origSend := shellSendKeys
	shellSendKeys = func(paneID, msg string) error {
		gotTarget = paneID
		return nil
	}
	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{{ID: "%201", Tool: "claude"}, {ID: "%202", Tool: "claude"}}, nil
	}
	t.Cleanup(func() {
		shellSendKeys = origSend
		shellListPanes = origList
	})

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
	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{}, nil
	}
	t.Cleanup(func() { shellListPanes = origList })

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
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%301", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%302", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	var targets []string
	origSend := shellSendKeys
	shellSendKeys = func(paneID, msg string) error {
		targets = append(targets, paneID)
		return nil
	}
	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{{ID: "%301", Tool: "claude"}, {ID: "%302", Tool: "claude"}}, nil
	}
	t.Cleanup(func() {
		shellSendKeys = origSend
		shellListPanes = origList
	})

	m := newModel(s)
	m.broadcast("hello all")

	if len(targets) != 2 {
		t.Fatalf("broadcast targets = %v, want 2 existing instances", targets)
	}
}

func TestBroadcastDoesNotFallbackWhenPaneListingFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-broadcast-fail"
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%381", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	var targets []string
	origSend := shellSendKeys
	shellSendKeys = func(paneID, msg string) error {
		targets = append(targets, paneID)
		return nil
	}
	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return nil, fmt.Errorf("tmux list failed")
	}
	t.Cleanup(func() {
		shellSendKeys = origSend
		shellListPanes = origList
	})

	m := newModel(s)
	m.lines = nil
	m.broadcast("hello all")

	if len(targets) != 0 {
		t.Fatalf("broadcast should not send when pane listing fails, got targets %v", targets)
	}
	foundErr := false
	for _, line := range m.lines {
		if strings.Contains(line, "Failed to load tool panes") {
			foundErr = true
		}
	}
	if !foundErr {
		t.Fatal("expected broadcast to show pane loading error when pane listing fails")
	}
}

func TestAppendPanesShowsToolLabels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-ps"
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%401", "shell"); err != nil {
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

func TestAppendPanesSkipsStaleRegistryEntries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-ps-live-only"
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%501", "shell"); err != nil {
		t.Fatalf("RegisterToolPane #1: %v", err)
	}
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%502", "shell"); err != nil {
		t.Fatalf("RegisterToolPane #2: %v", err)
	}

	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{{ID: "%501", Tool: "claude", Active: true}}, nil
	}
	t.Cleanup(func() { shellListPanes = origList })

	m := newModel(s)
	m.lines = nil
	m.appendPanes()

	hasLive := false
	hasStale := false
	for _, line := range m.lines {
		if strings.Contains(line, "claude#1") {
			hasLive = true
		}
		if strings.Contains(line, "claude#2") {
			hasStale = true
		}
	}
	if !hasLive {
		t.Fatal("expected appendPanes output to include claude#1")
	}
	if hasStale {
		t.Fatal("appendPanes should not include stale registry-only pane claude#2")
	}
}

func TestActiveToolPaneCountDoesNotFallbackWhenPaneListingFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-count-fail"
	if _, err := session.RegisterToolPane(s.state.ID, "claude", "%601", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return nil, fmt.Errorf("tmux list failed")
	}
	t.Cleanup(func() { shellListPanes = origList })

	if got := s.activeToolPaneCount(); got != 0 {
		t.Fatalf("activeToolPaneCount = %d, want 0 when pane listing fails", got)
	}
}

func TestAutoRouteNoRouter(t *testing.T) {
	m := newTestModel(map[string]config.ToolConfig{"claude": {Command: "claude"}})
	m.lines = nil

	m.handleCommand("some random message")
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "No API key") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected no API key warning in queued lines")
	}
}

func TestEmptyLineIgnored(t *testing.T) {
	m := newTestModel(nil)
	m.lines = nil

	m, cmd := sendKey(m, tea.KeyEnter)
	if cmd != nil {
		t.Error("empty line should not produce a cmd")
	}
	if len(m.lines) > 0 {
		t.Error("empty line should not queue output")
	}
}

// --- Router integration tests ---

func TestAutoRouteWithRouterError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer srv.Close()

	os.Setenv("TEST_SHELL_ROUTE_KEY", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_SHELL_ROUTE_KEY") })

	cfg := &config.Config{
		Sub2api: config.Sub2apiConfig{URL: srv.URL, APIKeyEnv: "TEST_SHELL_ROUTE_KEY"},
		Tools:   map[string]config.ToolConfig{"claude": {Command: "claude"}},
	}
	state := &session.State{ID: "test-id", TmuxSession: "ae-test"}
	s := New(cfg, state)
	m := newModel(s)
	m.lines = nil

	m.handleCommand("test message")
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "Routing") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected routing output in queued lines")
	}
}

func TestAutoRouteWithRouterSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"content": "claude"}},
			},
		})
	}))
	defer srv.Close()

	os.Setenv("TEST_SHELL_ROUTE_KEY2", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_SHELL_ROUTE_KEY2") })

	cfg := &config.Config{
		Sub2api: config.Sub2apiConfig{URL: srv.URL, APIKeyEnv: "TEST_SHELL_ROUTE_KEY2"},
		Tools:   map[string]config.ToolConfig{"claude": {Command: "claude"}},
	}
	state := &session.State{ID: "test-id", TmuxSession: "ae-nonexistent-12345"}
	s := New(cfg, state)
	m := newModel(s)
	m.lines = nil

	m.handleCommand("help me debug")
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "claude") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected routed to claude in queued lines")
	}
}

// --- Tmux integration tests ---

func TestSendToToolNoTmux(t *testing.T) {
	s := newTestShell(map[string]config.ToolConfig{
		"echo-tool": {Command: "echo", Args: []string{"hello"}},
	})
	s.state.TmuxSession = "ae-nonexistent-session-12345"
	m := newModel(s)
	m.lines = nil

	m.launchToolInstance("echo-tool", "test message")
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "Failed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected failure message for non-existent tmux session")
	}
}

func TestLaunchToolInstanceRollsBackPaneOnRegisterFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"claude": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = "ae-shell-rollback"
	s.state.ID = ""
	m := newModel(s)
	m.lines = nil

	origSplit := shellSplitWindow
	shellSplitWindow = func(sessionName, toolName, command string, args []string) (string, error) {
		return "%777", nil
	}
	origKill := shellKillPane
	var killedPaneID string
	shellKillPane = func(paneID string) error {
		killedPaneID = paneID
		return nil
	}
	t.Cleanup(func() {
		shellSplitWindow = origSplit
		shellKillPane = origKill
	})

	m.launchToolInstance("claude", "hello")

	if killedPaneID != "%777" {
		t.Fatalf("rollback killed pane = %q, want %q", killedPaneID, "%777")
	}

	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "Failed to register claude pane") {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected register failure message")
	}
}

func TestSendToToolDeadPane(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"echo-tool": {Command: "echo", Args: []string{"hello"}},
	})
	s.state.TmuxSession = "ae-nonexistent-session-12345"
	if _, err := session.RegisterToolPane(s.state.ID, "echo-tool", "%999999", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}
	m := newModel(s)
	m.lines = nil

	m.sendToExistingTool("echo-tool", 1, "test")
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "Failed") || strings.Contains(line, "not found") {
			found = true
		}
	}
	if !found {
		t.Error("expected send failure or not found message for dead pane")
	}
}

func TestListPanesNoTmux(t *testing.T) {
	s := newTestShell(nil)
	s.state.TmuxSession = "ae-nonexistent-session-12345"
	m := newModel(s)
	m.lines = nil

	m.appendPanes()
	found := false
	for _, line := range m.lines {
		if strings.Contains(line, "Failed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected error for non-existent tmux session")
	}
}

func TestSendToToolWithTmux(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}
	t.Setenv("HOME", t.TempDir())

	tmuxName := fmt.Sprintf("ae-cli-shell-test-send-%d", time.Now().UnixNano())
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	s := newTestShell(map[string]config.ToolConfig{
		"echo-tool": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = tmuxName
	m := newModel(s)

	m.launchToolInstance("echo-tool", "")
	items, err := session.ListToolPanes(s.state.ID)
	if err != nil {
		t.Fatalf("ListToolPanes after first launch: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) after first launch = %d, want 1", len(items))
	}
	m.launchToolInstance("echo-tool", "")
	items, err = session.ListToolPanes(s.state.ID)
	if err != nil {
		t.Fatalf("ListToolPanes after second launch: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) after second launch = %d, want 2", len(items))
	}
	if got := session.FormatToolPaneLabel(items[1]); got != "echo-tool#2" {
		t.Fatalf("second label = %q, want %q", got, "echo-tool#2")
	}

	wantPaneID := items[1].PaneID
	origSend := shellSendKeys
	var gotTarget string
	shellSendKeys = func(paneID, msg string) error {
		gotTarget = paneID
		return nil
	}
	t.Cleanup(func() { shellSendKeys = origSend })

	m.sendToExistingTool("echo-tool", 2, "world")
	if gotTarget != wantPaneID {
		t.Fatalf("send target = %q, want %q (echo-tool#2)", gotTarget, wantPaneID)
	}
}

func TestListPanesWithTmux(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := fmt.Sprintf("ae-cli-shell-test-list-%d", time.Now().UnixNano())
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	s := newTestShell(nil)
	s.state.TmuxSession = tmuxName
	m := newModel(s)
	m.lines = nil

	m.appendPanes()
	for _, line := range m.lines {
		if strings.Contains(line, "Failed") {
			t.Error("expected successful pane listing")
		}
	}
}

func TestSendToToolWithTmuxEmptyMessage(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}
	t.Setenv("HOME", t.TempDir())

	tmuxName := fmt.Sprintf("ae-cli-shell-test-empty-%d", time.Now().UnixNano())
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	s := newTestShell(map[string]config.ToolConfig{
		"sleep-tool": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = tmuxName
	m := newModel(s)

	m.launchToolInstance("sleep-tool", "")
	items, err := session.ListToolPanes(s.state.ID)
	if err != nil {
		t.Fatalf("ListToolPanes: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
}

func TestBroadcastWithTmux(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}
	t.Setenv("HOME", t.TempDir())

	tmuxName := fmt.Sprintf("ae-cli-shell-test-bcast-%d", time.Now().UnixNano())
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	s := newTestShell(map[string]config.ToolConfig{
		"tool-a": {Command: "sleep", Args: []string{"10"}},
	})
	s.state.TmuxSession = tmuxName
	m := newModel(s)
	m.lines = nil

	m.launchToolInstance("tool-a", "")
	m.launchToolInstance("tool-a", "")
	items, err := session.ListToolPanes(s.state.ID)
	if err != nil {
		t.Fatalf("ListToolPanes: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2 live instances", len(items))
	}
	if _, err := session.RegisterToolPane(s.state.ID, "tool-a", "%999999", "shell"); err != nil {
		t.Fatalf("RegisterToolPane stale: %v", err)
	}

	origSend := shellSendKeys
	var targets []string
	shellSendKeys = func(paneID, msg string) error {
		targets = append(targets, paneID)
		return nil
	}
	t.Cleanup(func() { shellSendKeys = origSend })

	m.broadcast("hello all")

	if len(targets) != 2 {
		t.Fatalf("broadcast targets = %v, want 2 live targets", targets)
	}
	live := map[string]bool{items[0].PaneID: true, items[1].PaneID: true}
	for _, paneID := range targets {
		if !live[paneID] {
			t.Fatalf("broadcast sent to non-live pane %q (targets=%v)", paneID, targets)
		}
	}
}

// --- Exit with active panes tests ---

func TestExitWithActivePanesConfirmKill(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"sleep-tool": {Command: "sleep", Args: []string{"60"}},
	})
	s.state.TmuxSession = "ae-cli-shell-test-exit-kill"
	m := newModel(s)
	m.lines = nil

	if _, err := session.RegisterToolPane(s.state.ID, "sleep-tool", "%901", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{{ID: "%901", Tool: "sleep", Active: true}}, nil
	}
	t.Cleanup(func() { shellListPanes = origList })

	// Type "exit" — should prompt for confirmation
	m, cmd := sendLine(m, "exit")
	if !m.confirmKill {
		t.Fatal("should be in confirmKill state")
	}
	_ = cmd

	// Confirm with "y"
	m, cmd = sendLine(m, "y")
	if cmd == nil {
		t.Error("confirming kill should quit")
	}
	if !s.killTmuxOnExit {
		t.Error("expected killTmuxOnExit to be true")
	}
}

func TestExitWithActivePanesDecline(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	s := newTestShell(map[string]config.ToolConfig{
		"sleep-tool": {Command: "sleep", Args: []string{"60"}},
	})
	s.state.TmuxSession = "ae-cli-shell-test-exit-nkill"
	m := newModel(s)

	if _, err := session.RegisterToolPane(s.state.ID, "sleep-tool", "%902", "shell"); err != nil {
		t.Fatalf("RegisterToolPane: %v", err)
	}

	origList := shellListPanes
	shellListPanes = func(string) ([]tmux.Pane, error) {
		return []tmux.Pane{{ID: "%902", Tool: "sleep", Active: true}}, nil
	}
	t.Cleanup(func() { shellListPanes = origList })

	// Type "exit"
	m, _ = sendLine(m, "exit")
	if !m.confirmKill {
		t.Fatal("should be in confirmKill state")
	}

	// Decline with "n"
	m, cmd := sendLine(m, "n")
	if m.confirmKill {
		t.Error("confirmKill should be reset after declining")
	}
	if cmd != nil {
		t.Error("declining should not quit")
	}
	if s.killTmuxOnExit {
		t.Error("killTmuxOnExit should remain false after declining")
	}
}

// suppress unused import
var _ = fmt.Sprint
