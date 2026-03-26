package shell

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

// --- helpers ---

func newTestShell(tools map[string]config.ToolConfig) *Shell {
	if tools == nil {
		tools = map[string]config.ToolConfig{}
	}
	cfg := &config.Config{Tools: tools}
	state := &session.State{ID: "test-id", TmuxSession: "ae-test"}
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
	if s.toolPanes == nil {
		t.Error("toolPanes should be initialized")
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

	m.sendToTool("echo-tool", "test message")
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

func TestSendToToolDeadPane(t *testing.T) {
	s := newTestShell(map[string]config.ToolConfig{
		"echo-tool": {Command: "echo", Args: []string{"hello"}},
	})
	s.state.TmuxSession = "ae-nonexistent-session-12345"
	s.toolPanes["echo-tool"] = "%999999"
	m := newModel(s)

	m.sendToTool("echo-tool", "test")
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

	tmuxName := "ae-cli-shell-test-send"
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

	m.sendToTool("echo-tool", "hello")
	if _, exists := s.toolPanes["echo-tool"]; !exists {
		t.Error("expected echo-tool pane to be registered")
	}
	m.sendToTool("echo-tool", "world")
}

func TestListPanesWithTmux(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := "ae-cli-shell-test-list"
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

	tmuxName := "ae-cli-shell-test-empty"
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

	m.sendToTool("sleep-tool", "")
	if _, exists := s.toolPanes["sleep-tool"]; !exists {
		t.Error("expected sleep-tool pane to be registered")
	}
}

func TestBroadcastWithTmux(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := "ae-cli-shell-test-bcast"
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

	m.broadcast("hello all")
}

// --- Exit with active panes tests ---

func TestExitWithActivePanesConfirmKill(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := "ae-cli-shell-test-exit-kill"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	s := newTestShell(map[string]config.ToolConfig{
		"sleep-tool": {Command: "sleep", Args: []string{"60"}},
	})
	s.state.TmuxSession = tmuxName
	m := newModel(s)
	m.lines = nil

	// Launch a tool pane
	m.sendToTool("sleep-tool", "")
	if _, exists := s.toolPanes["sleep-tool"]; !exists {
		t.Fatal("expected sleep-tool pane to be registered")
	}

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

	tmux.KillSession(tmuxName)
}

func TestExitWithActivePanesDecline(t *testing.T) {
	if !tmux.HasTmux() {
		t.Skip("tmux not installed")
	}

	tmuxName := "ae-cli-shell-test-exit-nkill"
	tmux.KillSession(tmuxName)
	if err := tmux.NewSession(tmuxName); err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer tmux.KillSession(tmuxName)

	s := newTestShell(map[string]config.ToolConfig{
		"sleep-tool": {Command: "sleep", Args: []string{"60"}},
	})
	s.state.TmuxSession = tmuxName
	m := newModel(s)

	m.sendToTool("sleep-tool", "")

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
	_ = cmd

	if !tmux.SessionExists(tmuxName) {
		t.Error("tmux session should still exist")
	}
}

// suppress unused import
var _ = fmt.Sprint
