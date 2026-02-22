package mayor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

// Common errors
var (
	ErrNotRunning     = errors.New("mayor not running")
	ErrAlreadyRunning = errors.New("mayor already running")
)

// Manager handles mayor lifecycle operations.
type Manager struct {
	townRoot string
}

// NewManager creates a new mayor manager for a town.
func NewManager(townRoot string) *Manager {
	return &Manager{
		townRoot: townRoot,
	}
}

// SessionName returns the tmux session name for the mayor.
// This is a package-level function for convenience.
// Deprecated: Use Target() for window-per-rig mode.
func SessionName() string {
	return session.MayorSessionName()
}

// SessionName returns the tmux session name for the mayor.
// Deprecated: Use Target() for window-per-rig mode.
func (m *Manager) SessionName() string {
	return SessionName()
}

// Target returns the tmux target for the mayor (e.g., "hq:mayor").
func (m *Manager) Target() session.TmuxTarget {
	return session.MayorTarget()
}

// mayorDir returns the working directory for the mayor.
func (m *Manager) mayorDir() string {
	return filepath.Join(m.townRoot, "mayor")
}

// Start starts the mayor window in the shared hq session.
// agentOverride optionally specifies a different agent alias to use.
func (m *Manager) Start(agentOverride string) error {
	t := tmux.NewTmux()
	rigSess := session.HQSessionName()
	windowName := session.MayorWindowName()
	target := m.Target().String()

	// Check if window already exists
	running, _ := t.HasWindow(rigSess, windowName)
	if running {
		// Window exists - check if agent is actually running (healthy vs zombie)
		if t.IsAgentAlive(target) {
			return ErrAlreadyRunning
		}
		// Zombie - tmux window alive but agent dead. Kill and recreate.
		if err := t.KillWindowWithProcesses(rigSess, windowName); err != nil {
			return fmt.Errorf("killing zombie window: %w", err)
		}
	}

	// Ensure mayor directory exists (for Claude settings)
	mayorDir := m.mayorDir()
	if err := os.MkdirAll(mayorDir, 0755); err != nil {
		return fmt.Errorf("creating mayor directory: %w", err)
	}

	// Use unified session lifecycle for config → settings → command → create → env → theme → wait.
	theme := tmux.MayorTheme()
	_, err := session.StartSession(t, session.SessionConfig{
		SessionID:  rigSess,
		WindowName: windowName,
		WorkDir:    mayorDir,
		Role:       "mayor",
		TownRoot:   m.townRoot,
		AgentName:  "Mayor",
		Beacon: session.BeaconConfig{
			Recipient: "mayor",
			Sender:    "human",
			Topic:     "cold-start",
		},
		AgentOverride: agentOverride,
		Theme:         &theme,
		WaitForAgent:  true,
		WaitFatal:     true,
		AutoRespawn:   true,
		AcceptBypass:  true,
	})
	if err != nil {
		return err
	}

	time.Sleep(session.ShutdownDelay())

	return nil
}

// Stop stops the mayor window.
func (m *Manager) Stop() error {
	t := tmux.NewTmux()
	rigSess := session.HQSessionName()
	windowName := session.MayorWindowName()
	target := m.Target().String()

	// Check if window exists
	running, err := t.HasWindow(rigSess, windowName)
	if err != nil {
		return fmt.Errorf("checking window: %w", err)
	}
	if !running {
		return ErrNotRunning
	}

	// Try graceful shutdown first (best-effort interrupt)
	_ = t.SendKeysRaw(target, "C-c")
	time.Sleep(100 * time.Millisecond)

	// Kill the window and all its processes
	if err := t.KillWindowWithProcesses(rigSess, windowName); err != nil {
		return fmt.Errorf("killing window: %w", err)
	}

	return nil
}

// IsRunning checks if the mayor window is active.
func (m *Manager) IsRunning() (bool, error) {
	t := tmux.NewTmux()
	rigSess := session.HQSessionName()
	windowName := session.MayorWindowName()
	target := m.Target().String()

	hasWindow, err := t.HasWindow(rigSess, windowName)
	if err != nil || !hasWindow {
		return false, err
	}
	return t.IsAgentAlive(target), nil
}

// Status returns information about the mayor session.
func (m *Manager) Status() (*tmux.SessionInfo, error) {
	t := tmux.NewTmux()
	rigSess := session.HQSessionName()

	running, err := t.HasWindow(rigSess, session.MayorWindowName())
	if err != nil {
		return nil, fmt.Errorf("checking window: %w", err)
	}
	if !running {
		return nil, ErrNotRunning
	}

	return t.GetSessionInfo(rigSess)
}
