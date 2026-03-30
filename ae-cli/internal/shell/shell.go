package shell

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
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

// Shell manages the interactive agent shell session.
type Shell struct {
	config         *config.Config
	state          *session.State
	router         *router.Router
	toolPanes      map[string]string
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
	return &Shell{config: cfg, state: state, router: r, toolPanes: make(map[string]string)}
}

const envShellForceStdin = "AE_CLI_SHELL_FORCE_STDIN"

// Run starts the interactive shell TUI.
func (s *Shell) Run() error {
	// Bubble Tea's default stdin behavior is to open /dev/tty when stdin isn't a
	// terminal, so interactive apps still work when stdin is piped/redirected.
	// In some test/CI environments /dev/tty is unavailable; for deterministic
	// tests we allow bypassing the TUI entirely and exiting based on stdin.
	if os.Getenv(envShellForceStdin) == "1" {
		sc := bufio.NewScanner(os.Stdin)
		for sc.Scan() {
			switch strings.TrimSpace(sc.Text()) {
			case "exit", "quit":
				return nil
			}
		}
		if err := sc.Err(); err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
		return nil
	}
	return s.runWithOpts()
}

func (s *Shell) runWithOpts(opts ...tea.ProgramOption) error {
	// Print banner before bubbletea takes over the terminal
	fmt.Println("\033[1m=== AE Agent Shell ===\033[0m")
	fmt.Println("Tools:", s.toolNames())
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  @claude <msg>   Send to specific tool")
	fmt.Println("  @all <msg>      Broadcast to all tools")
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
	m.queueLine("  @claude <msg>   Send to specific tool")
	m.queueLine("  @all <msg>      Broadcast to all tools")
	m.queueLine("  <msg>           Auto-route via LLM")
	m.queueLine("  !<cmd>          Execute shell command")
	m.queueLine("  ps              List running panes")
	m.queueLine("  exit            Quit shell")
	m.queueLine("")
}

func (m *model) handleDirected(line string) {
	parts := strings.SplitN(line[1:], " ", 2)
	toolName := parts[0]
	msg := ""
	if len(parts) > 1 {
		msg = parts[1]
	}
	if _, ok := m.shell.config.Tools[toolName]; !ok {
		m.queueLine(fmt.Sprintf("\033[31mUnknown tool: %s\033[0m", toolName))
		m.queueLine("Available: " + m.shell.toolNames())
		return
	}
	m.sendToTool(toolName, msg)
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
	m.sendToTool(toolName, msg)
}

func (m *model) broadcast(msg string) {
	for name := range m.shell.config.Tools {
		m.queueLine(fmt.Sprintf("→ \033[32m%s\033[0m", name))
		m.sendToTool(name, msg)
	}
}

func (m *model) sendToTool(toolName, msg string) {
	paneID, exists := m.shell.toolPanes[toolName]
	if exists && !m.shell.paneAlive(paneID) {
		delete(m.shell.toolPanes, toolName)
		exists = false
	}
	if !exists {
		toolCfg := m.shell.config.Tools[toolName]
		id, err := tmux.SplitWindow(m.shell.state.TmuxSession, toolName, toolCfg.Command, toolCfg.Args)
		if err != nil {
			m.queueLine(fmt.Sprintf("\033[31mFailed to launch %s: %v\033[0m", toolName, err))
			return
		}
		m.shell.toolPanes[toolName] = id
		paneID = id
		m.queueLine(fmt.Sprintf("Launched %s in pane %s", toolName, id))
	}
	if msg != "" {
		if err := exec.Command("tmux", "send-keys", "-t", paneID, msg, "Enter").Run(); err != nil {
			m.queueLine(fmt.Sprintf("\033[31mFailed to send to %s: %v\033[0m", toolName, err))
		}
	}
}

func (m *model) appendPanes() {
	panes, err := tmux.ListPanes(m.shell.state.TmuxSession)
	if err != nil {
		m.queueLine(fmt.Sprintf("\033[31mFailed to list panes: %v\033[0m", err))
		return
	}
	m.queueLine(fmt.Sprintf("%-12s %-20s %s", "PANE ID", "COMMAND", "ACTIVE"))
	for _, p := range panes {
		active := ""
		if p.Active {
			active = "*"
		}
		m.queueLine(fmt.Sprintf("%-12s %-20s %s", p.ID, p.Tool, active))
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
	count := 0
	for _, paneID := range s.toolPanes {
		if s.paneAlive(paneID) {
			count++
		}
	}
	return count
}
