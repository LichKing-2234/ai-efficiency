package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Pane represents a tmux pane running a tool.
type Pane struct {
	ID     string
	Tool   string
	Active bool
}

// HasTmux checks if tmux is installed.
func HasTmux() bool {
	_, err := exec.LookPath("tmux")
	return err == nil
}

// isInsideSession is the default implementation that checks $TMUX and queries tmux.
func isInsideSession(name string) bool {
	if os.Getenv("TMUX") == "" {
		return false
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == name
}

// IsInsideSessionFunc can be overridden in tests to control IsInsideSession behavior.
var IsInsideSessionFunc = isInsideSession

// IsInsideSession checks if the current process is running inside the given tmux session.
func IsInsideSession(name string) bool {
	return IsInsideSessionFunc(name)
}

// SessionExists checks if a tmux session exists.
func SessionExists(name string) bool {
	err := exec.Command("tmux", "has-session", "-t", name).Run()
	return err == nil
}

// NewSession creates a new tmux session (detached) with aggressive resize enabled.
func NewSession(name string) error {
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "200", "-y", "50").Run(); err != nil {
		return err
	}
	_ = exec.Command("tmux", "set-option", "-t", name, "-g", "aggressive-resize", "on").Run()
	return nil
}

// NewSessionWithCommand creates a new tmux session that runs a command directly.
// When the command exits, the tmux pane (and session if it's the only pane) also exits.
func NewSessionWithCommand(name string, command string) error {
	if err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "200", "-y", "50", command).Run(); err != nil {
		return err
	}
	_ = exec.Command("tmux", "set-option", "-t", name, "-g", "aggressive-resize", "on").Run()
	return nil
}

// SplitWindow creates a new pane in the session running the given command.
func SplitWindow(sessionName string, toolName string, command string, args []string) (string, error) {
	// Build command args safely — pass command and args separately to tmux
	// using "--" to prevent argument injection
	cmdArgs := []string{
		"split-window", "-t", sessionName,
		"-P", "-F", "#{pane_id}",
		"--", command,
	}
	cmdArgs = append(cmdArgs, args...)
	out, err := exec.Command("tmux", cmdArgs...).Output()
	if err != nil {
		return "", fmt.Errorf("split-window: %w", err)
	}

	paneID := strings.TrimSpace(string(out))

	// Auto-layout: tiled
	_ = exec.Command("tmux", "select-layout", "-t", sessionName, "tiled").Run()

	return paneID, nil
}

// ListPanes returns all panes in the session with their commands.
func ListPanes(sessionName string) ([]Pane, error) {
	out, err := exec.Command("tmux", "list-panes", "-t", sessionName,
		"-F", "#{pane_id}\t#{pane_current_command}\t#{pane_active}").Output()
	if err != nil {
		return nil, fmt.Errorf("list-panes: %w", err)
	}

	var panes []Pane
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) < 3 {
			continue
		}
		panes = append(panes, Pane{
			ID:     parts[0],
			Tool:   parts[1],
			Active: parts[2] == "1",
		})
	}
	return panes, nil
}

// KillPane kills a specific pane.
func KillPane(paneID string) error {
	return exec.Command("tmux", "kill-pane", "-t", paneID).Run()
}

// KillSession kills the entire tmux session.
func KillSession(name string) error {
	return exec.Command("tmux", "kill-session", "-t", name).Run()
}

// SetEnvironment sets environment variables for a tmux session (best-effort).
// This affects future panes started in that tmux session.
func SetEnvironment(sessionName string, env map[string]string) error {
	if sessionName == "" || len(env) == 0 {
		return nil
	}
	for k, v := range env {
		if k == "" {
			continue
		}
		// tmux stores environment variables per-session; values are passed as args (no shell eval).
		if err := exec.Command("tmux", "set-environment", "-t", sessionName, k, v).Run(); err != nil {
			return err
		}
	}
	return nil
}

// UnsetEnvironment removes environment variables from a tmux session.
// This affects future panes started in that tmux session.
func UnsetEnvironment(sessionName string, keys []string) error {
	if sessionName == "" || len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		if strings.TrimSpace(k) == "" {
			continue
		}
		if err := exec.Command("tmux", "set-environment", "-u", "-t", sessionName, k).Run(); err != nil {
			return err
		}
	}
	return nil
}
