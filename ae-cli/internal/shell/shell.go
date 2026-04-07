package shell

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/ai-efficiency/ae-cli/config"
	"github.com/ai-efficiency/ae-cli/internal/router"
	"github.com/ai-efficiency/ae-cli/internal/session"
	"github.com/ai-efficiency/ae-cli/internal/tmux"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

const exitTimeout = 3 * time.Second

// exitTimeoutMsg is sent when the pending exit timer expires.
type exitTimeoutMsg struct{ id int }

type directedTarget struct {
	ToolName   string
	InstanceNo int
	Message    string
}

var (
	shellSplitWindow = tmux.SplitWindow
	shellListPanes   = tmux.ListPanes
	shellKillPane    = tmux.KillPane
	shellSendKeys    = func(paneID, msg string) error {
		return exec.Command("tmux", "send-keys", "-t", paneID, msg, "Enter").Run()
	}
)

// Shell manages the interactive agent shell session.
type Shell struct {
	config         *config.Config
	state          *session.State
	router         *router.Router
	killTmuxOnExit bool
}

func (s *Shell) ShouldKillTmux() bool { return s.killTmuxOnExit }

func New(cfg *config.Config, state *session.State) *Shell {
	var tools []string
	for name := range cfg.Tools {
		tools = append(tools, name)
	}
	var r *router.Router
	apiKey := os.Getenv(cfg.Sub2api.APIKeyEnv)
	if cfg.Sub2api.URL != "" && apiKey != "" {
		model := cfg.Sub2api.Model
		if model == "" {
			model = "claude-sonnet-4-20250514"
		}
		r = router.New(cfg.Sub2api.URL, apiKey, model, tools)
	}
	return &Shell{config: cfg, state: state, router: r}
}

// Run starts the interactive shell TUI.
func (s *Shell) Run() error {
	return s.runWithOpts()
}

func (s *Shell) runWithOpts(opts ...tea.ProgramOption) error {
	// Print banner before bubbletea takes over the terminal
	fmt.Println("\033[1m=== AE Agent Shell ===\033[0m")
	fmt.Println("Tools:", s.toolNames())
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  @tool <msg>     Launch a new instance and send message")
	fmt.Println("  @tool#N <msg>   Send to an existing instance")
	fmt.Println("  @all <msg>      Broadcast to existing tool instances")
	fmt.Println("  <msg>           Auto-route via LLM")
	fmt.Println("  !<cmd>          Execute shell command")
	fmt.Println("  ps              List running panes")
	fmt.Println("  exit            Quit shell")
	fmt.Println()

	m := newModel(s)
	p := tea.NewProgram(m, opts...)
	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	fm := finalModel.(model)
	s.killTmuxOnExit = fm.shell.killTmuxOnExit
	return nil
}

// --- bubbletea model ---

type model struct {
	shell       *Shell
	input       textinput.Model
	lines       []string // pending lines to print via tea.Println
	pendingExit bool
	exitSeq     int // incremented each time pendingExit is set; used to ignore stale timeouts
	quitting    bool
	confirmKill bool
}

func newModel(s *Shell) model {
	ti := textinput.New()
	ti.Prompt = "\033[36mae>\033[0m "
	ti.Focus()

	m := model{shell: s, input: ti}
	return m
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case exitTimeoutMsg:
		// Only reset if this timeout matches the current exit sequence
		if m.pendingExit && msg.id == m.exitSeq {
			m.pendingExit = false
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.confirmKill {
				return m, tea.Quit
			}
			if m.pendingExit {
				m.quitting = true
				m.shell.killTmuxOnExit = m.shell.hasActivePanes()
				return m, tea.Quit
			}
			m.pendingExit = true
			m.exitSeq++
			m.input.SetValue("")
			seq := m.exitSeq
			return m, tea.Tick(exitTimeout, func(time.Time) tea.Msg {
				return exitTimeoutMsg{id: seq}
			})

		case tea.KeyCtrlD:
			if m.input.Value() != "" {
				break
			}
			if m.confirmKill {
				return m, tea.Quit
			}
			if m.pendingExit {
				m.quitting = true
				m.shell.killTmuxOnExit = m.shell.hasActivePanes()
				return m, tea.Quit
			}
			m.pendingExit = true
			m.exitSeq++
			seq := m.exitSeq
			return m, tea.Tick(exitTimeout, func(time.Time) tea.Msg {
				return exitTimeoutMsg{id: seq}
			})

		case tea.KeyEnter:
			line := strings.TrimSpace(m.input.Value())
			m.input.SetValue("")
			m.pendingExit = false

			if m.confirmKill {
				m.confirmKill = false
				if a := strings.ToLower(line); a == "y" || a == "yes" {
					m.shell.killTmuxOnExit = true
					m.quitting = true
					return m, tea.Quit
				}
				return m, nil
			}

			if line == "" {
				return m, nil
			}

			m.queueLine(fmt.Sprintf("\033[36mae>\033[0m %s", line))
			m.handleCommand(line)
			flush := m.flushLines()
			if m.quitting {
				// flush 完再退出
				return m, tea.Sequence(flush, tea.Quit)
			}
			return m, flush

		default:
			if m.pendingExit && msg.Type != tea.KeyUp && msg.Type != tea.KeyDown {
				m.pendingExit = false
			}
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) View() string {
	if m.quitting {
		return ""
	}
	if m.pendingExit {
		return "\033[90m  Press Ctrl+C again to exit\033[0m\n" + m.input.View()
	}
	return m.input.View()
}

// --- line queue & flush ---

func (m *model) queueLine(s string) {
	m.lines = append(m.lines, s)
}

// flushLines returns a tea.Cmd that prints all queued lines via tea.Println,
// then clears the queue.
func (m *model) flushLines() tea.Cmd {
	if len(m.lines) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, len(m.lines))
	for i, line := range m.lines {
		cmds[i] = tea.Println(line)
	}
	m.lines = m.lines[:0]
	return tea.Sequence(cmds...)
}

// --- command handling ---

func (m *model) handleCommand(line string) {
	switch {
	case line == "exit" || line == "quit":
		if m.shell.hasActivePanes() {
			m.queueLine(fmt.Sprintf("There are %d tool pane(s) still running. Kill tmux session %s? [y/N] ",
				m.shell.activeToolPaneCount(), m.shell.state.TmuxSession))
			m.confirmKill = true
			return
		}
		m.quitting = true
		return

	case line == "ps":
		m.appendPanes()

	case line == "help":
		m.queueBanner()

	case strings.HasPrefix(line, "!"):
		m.execShell(line[1:])

	case strings.HasPrefix(line, "@all "):
		m.broadcast(strings.TrimPrefix(line, "@all "))

	case strings.HasPrefix(line, "@"):
		m.handleDirected(line)

	default:
		m.handleAutoRoute(line)
	}
}

// --- output helpers ---

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

func parseDirectedTarget(line string) (directedTarget, error) {
	parts := strings.SplitN(strings.TrimPrefix(line, "@"), " ", 2)
	head := strings.TrimSpace(parts[0])
	msg := ""
	if len(parts) == 2 {
		msg = parts[1]
	}

	target := directedTarget{Message: msg}
	if rawTool, rawIndex, ok := strings.Cut(head, "#"); ok {
		target.ToolName = strings.TrimSpace(rawTool)
		n, err := strconv.Atoi(strings.TrimSpace(rawIndex))
		if err != nil || n <= 0 || target.ToolName == "" {
			return directedTarget{}, fmt.Errorf("invalid tool instance selector %q", head)
		}
		target.InstanceNo = n
		return target, nil
	}

	target.ToolName = head
	return target, nil
}

func (m *model) handleAutoRoute(msg string) {
	if m.shell.router == nil {
		m.queueLine("\033[33mNo API key configured for auto-routing. Use @tool syntax.\033[0m")
		return
	}
	m.queueLine("\033[33mRouting...\033[0m")
	toolName, err := m.shell.router.Route(msg)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mRouting failed: %v\033[0m", err))
		return
	}
	m.queueLine(fmt.Sprintf("→ \033[32m%s\033[0m", toolName))
	m.launchToolInstance(toolName, msg)
}

func (m *model) broadcast(msg string) {
	items, err := m.liveToolPanes()
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

func (m *model) launchToolInstance(toolName, msg string) {
	toolCfg := m.shell.config.Tools[toolName]
	paneID, err := shellSplitWindow(m.shell.state.TmuxSession, toolName, toolCfg.Command, toolCfg.Args)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to launch %s: %v\033[0m", toolName, err))
		return
	}
	rec, err := session.RegisterToolPane(m.shell.state.ID, toolName, paneID, "shell")
	if err != nil {
		if killErr := shellKillPane(paneID); killErr != nil {
			m.queueLine(fmt.Sprintf("\033[31mFailed to rollback pane %s after register error: %v\033[0m", paneID, killErr))
		}
		m.queueLine(fmt.Sprintf("\033[31mFailed to register %s pane: %v\033[0m", toolName, err))
		return
	}
	label := session.FormatToolPaneLabel(*rec)
	m.queueLine(fmt.Sprintf("Launched %s in pane %s as %s", toolName, paneID, label))
	if msg != "" {
		m.sendKeys(paneID, msg, label)
	}
}

func (m *model) sendToExistingTool(toolName string, instanceNo int, msg string) {
	items, err := m.liveToolPanes()
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

func (m *model) sendKeys(paneID, msg, label string) {
	if err := shellSendKeys(paneID, msg); err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to send to %s: %v\033[0m", label, err))
	}
}

func (m *model) liveToolPanes() ([]session.ToolPaneRecord, error) {
	items, err := session.ListToolPanes(m.shell.state.ID)
	if err != nil {
		return nil, err
	}
	panes, err := shellListPanes(m.shell.state.TmuxSession)
	if err != nil {
		return nil, fmt.Errorf("listing tmux panes: %w", err)
	}
	livePaneIDs := make(map[string]struct{}, len(panes))
	for _, pane := range panes {
		livePaneIDs[pane.ID] = struct{}{}
	}
	live := make([]session.ToolPaneRecord, 0, len(items))
	for _, rec := range items {
		if _, ok := livePaneIDs[rec.PaneID]; ok {
			live = append(live, rec)
		}
	}
	return live, nil
}

func (m *model) appendPanes() {
	panes, err := shellListPanes(m.shell.state.TmuxSession)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to list panes: %v\033[0m", err))
		return
	}
	items, err := session.ListToolPanes(m.shell.state.ID)
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
		p, ok := paneByID[rec.PaneID]
		if !ok {
			continue
		}
		active := ""
		if p.Active {
			active = "*"
		}
		m.queueLine(fmt.Sprintf("%-16s %-12s %-20s %s", session.FormatToolPaneLabel(rec), rec.PaneID, p.Tool, active))
	}
}

func (m *model) execShell(cmdStr string) {
	cmd := exec.Command("sh", "-c", cmdStr)
	cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
	if err := cmd.Run(); err != nil {
		m.queueLine(fmt.Sprintf("\033[31m%v\033[0m", err))
	}
}

// --- shared helpers ---

func (s *Shell) toolNames() string {
	var names []string
	for name := range s.config.Tools {
		names = append(names, name)
	}
	return strings.Join(names, ", ")
}

func (s *Shell) paneAlive(paneID string) bool {
	return exec.Command("tmux", "has-session", "-t", paneID).Run() == nil
}

func (s *Shell) hasActivePanes() bool { return s.activeToolPaneCount() > 0 }

func (s *Shell) activeToolPaneCount() int {
	if s.state == nil {
		return 0
	}
	items, err := session.ListToolPanes(s.state.ID)
	if err != nil {
		return 0
	}
	panes, err := shellListPanes(s.state.TmuxSession)
	if err != nil {
		return 0
	}
	livePaneIDs := make(map[string]struct{}, len(panes))
	for _, pane := range panes {
		livePaneIDs[pane.ID] = struct{}{}
	}
	count := 0
	for _, rec := range items {
		if _, ok := livePaneIDs[rec.PaneID]; ok {
			count++
		}
	}
	return count
}
