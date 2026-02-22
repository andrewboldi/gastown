// Package dog provides dog session management for Deacon's helper workers.
package dog

import (
	"errors"
	"fmt"
	"github.com/steveyegge/gastown/internal/cli"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

// Session errors
var (
	ErrSessionRunning  = errors.New("session already running")
	ErrSessionNotFound = errors.New("session not found")
)

// SessionManager handles dog session lifecycle.
type SessionManager struct {
	tmux     *tmux.Tmux
	mgr      *Manager
	townRoot string
}

// NewSessionManager creates a new dog session manager.
// The Manager parameter is used to sync persistent dog state (idle/working)
// when sessions start and stop.
func NewSessionManager(t *tmux.Tmux, townRoot string, mgr *Manager) *SessionManager {
	return &SessionManager{
		tmux:     t,
		mgr:      mgr,
		townRoot: townRoot,
	}
}

// SessionStartOptions configures dog session startup.
type SessionStartOptions struct {
	// WorkDesc is the work description (formula or bead ID) for the startup prompt.
	WorkDesc string

	// AgentOverride specifies an alternate agent (e.g., "gemini", "claude-haiku").
	AgentOverride string
}

// SessionInfo contains information about a running dog session.
type SessionInfo struct {
	// DogName is the dog name.
	DogName string `json:"dog_name"`

	// SessionID is the tmux session identifier.
	SessionID string `json:"session_id"`

	// Running indicates if the session is currently active.
	Running bool `json:"running"`

	// Attached indicates if someone is attached to the session.
	Attached bool `json:"attached,omitempty"`

	// Created is when the session was created.
	Created time.Time `json:"created,omitempty"`
}

// SessionName generates the tmux session name for a dog.
// Pattern: hq-dog-{name}
// Dogs are town-level (managed by deacon), so they use the hq- prefix.
// We use "hq-dog-" instead of "hq-deacon-" to avoid tmux prefix-matching
// collisions with the "hq-deacon" session.
// Deprecated: Use Target() for window-per-rig mode.
func (m *SessionManager) SessionName(dogName string) string {
	return fmt.Sprintf("hq-dog-%s", dogName)
}

// rigSession returns the shared tmux session for town-level agents.
func (m *SessionManager) rigSession() string {
	return session.HQSessionName()
}

// windowName returns the tmux window name for a dog.
func (m *SessionManager) windowName(dogName string) string {
	return "dog-" + dogName
}

// Target returns the tmux target for a dog window (e.g., "hq:dog-rufus").
func (m *SessionManager) Target(dogName string) session.TmuxTarget {
	return session.NewTarget(m.rigSession(), m.windowName(dogName))
}

// kennelPath returns the path to the dog's kennel directory.
func (m *SessionManager) kennelPath(dogName string) string {
	return filepath.Join(m.townRoot, "deacon", "dogs", dogName)
}

// Start creates and starts a new window for a dog.
// Dogs run agent sessions that check mail for work and execute formulas.
func (m *SessionManager) Start(dogName string, opts SessionStartOptions) error {
	kennelDir := m.kennelPath(dogName)
	if _, err := os.Stat(kennelDir); os.IsNotExist(err) {
		return fmt.Errorf("%w: %s", ErrDogNotFound, dogName)
	}

	rigSess := m.rigSession()
	windowName := m.windowName(dogName)
	target := m.Target(dogName).String()

	// Check if window already exists.
	running, err := m.tmux.HasWindow(rigSess, windowName)
	if err != nil {
		return fmt.Errorf("checking window: %w", err)
	}
	if running {
		if m.tmux.IsAgentAlive(target) {
			return fmt.Errorf("%w: %s", ErrSessionRunning, target)
		}
		// Zombie window - kill and recreate.
		if err := m.tmux.KillWindowWithProcesses(rigSess, windowName); err != nil {
			return fmt.Errorf("killing zombie window: %w", err)
		}
	}

	// Build instructions for the dog
	workInfo := ""
	if opts.WorkDesc != "" {
		workInfo = fmt.Sprintf(" Work assigned: %s.", opts.WorkDesc)
	}
	instructions := fmt.Sprintf("I am Dog %s.%s Check mail for work: `"+cli.Name()+" mail inbox`. Execute assigned formula/bead. When done, send DOG_DONE mail to deacon/, run `"+cli.Name()+" dog done`, then exit the session. Do NOT idle at the prompt after completing work.", dogName, workInfo)

	// Use unified session lifecycle.
	theme := tmux.DogTheme()
	_, err = session.StartSession(m.tmux, session.SessionConfig{
		SessionID:  rigSess,
		WindowName: windowName,
		WorkDir:    kennelDir,
		Role:       "dog",
		TownRoot:   m.townRoot,
		AgentName:  dogName,
		Beacon: session.BeaconConfig{
			Recipient: session.BeaconRecipient("dog", dogName, ""),
			Sender:    "deacon",
			Topic:     "assigned",
		},
		Instructions:   instructions,
		AgentOverride:  opts.AgentOverride,
		Theme:          &theme,
		WaitForAgent:   true,
		WaitFatal:      true,
		AcceptBypass:   true,
		ReadyDelay:     true,
		VerifySurvived: true,
		TrackPID:       true,
	})
	if err != nil {
		return err
	}

	// Update persistent state to working
	if m.mgr != nil {
		if err := m.mgr.SetState(dogName, StateWorking); err != nil {
			// Log but don't fail - session is running, state sync is best-effort
			fmt.Fprintf(os.Stderr, "warning: failed to set dog %s state to working: %v\n", dogName, err)
		}
	}

	return nil
}

// Stop terminates a dog session.
func (m *SessionManager) Stop(dogName string, force bool) error {
	rigSess := m.rigSession()
	windowName := m.windowName(dogName)
	target := m.Target(dogName).String()

	running, err := m.tmux.HasWindow(rigSess, windowName)
	if err != nil {
		return fmt.Errorf("checking window: %w", err)
	}
	if !running {
		return ErrSessionNotFound
	}

	// Try graceful shutdown first
	if !force {
		_ = m.tmux.SendKeysRaw(target, "C-c")
		session.WaitForWindowExit(m.tmux, rigSess, windowName, constants.GracefulShutdownTimeout)
	}

	if err := m.tmux.KillWindowWithProcesses(rigSess, windowName); err != nil {
		return fmt.Errorf("killing window: %w", err)
	}

	// Update persistent state to idle so dog is available for reassignment
	if m.mgr != nil {
		if err := m.mgr.SetState(dogName, StateIdle); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to set dog %s state to idle: %v\n", dogName, err)
		}
	}

	return nil
}

// IsRunning checks if a dog session is active.
func (m *SessionManager) IsRunning(dogName string) (bool, error) {
	rigSess := m.rigSession()
	windowName := m.windowName(dogName)
	target := m.Target(dogName).String()

	hasWindow, err := m.tmux.HasWindow(rigSess, windowName)
	if err != nil || !hasWindow {
		return false, err
	}
	return m.tmux.IsAgentAlive(target), nil
}

// Status returns detailed status for a dog session.
func (m *SessionManager) Status(dogName string) (*SessionInfo, error) {
	rigSess := m.rigSession()
	windowName := m.windowName(dogName)
	target := m.Target(dogName).String()

	running, err := m.tmux.HasWindow(rigSess, windowName)
	if err != nil {
		return nil, fmt.Errorf("checking window: %w", err)
	}

	info := &SessionInfo{
		DogName:   dogName,
		SessionID: target,
		Running:   running,
	}

	if !running {
		return info, nil
	}

	tmuxInfo, err := m.tmux.GetSessionInfo(rigSess)
	if err != nil {
		return info, nil
	}

	info.Attached = tmuxInfo.Attached

	return info, nil
}

// GetPane returns the pane ID for a dog session.
func (m *SessionManager) GetPane(dogName string) (string, error) {
	rigSess := m.rigSession()
	windowName := m.windowName(dogName)
	target := m.Target(dogName).String()

	running, err := m.tmux.HasWindow(rigSess, windowName)
	if err != nil {
		return "", fmt.Errorf("checking window: %w", err)
	}
	if !running {
		return "", ErrSessionNotFound
	}

	// Get pane ID from target window
	pane, err := m.tmux.GetPaneID(target)
	if err != nil {
		return "", fmt.Errorf("getting pane: %w", err)
	}

	return pane, nil
}

// EnsureRunning ensures a dog session is running, starting it if needed.
// Returns the pane ID.
func (m *SessionManager) EnsureRunning(dogName string, opts SessionStartOptions) (string, error) {
	running, err := m.IsRunning(dogName)
	if err != nil {
		return "", err
	}

	if !running {
		if err := m.Start(dogName, opts); err != nil {
			return "", err
		}
	}

	return m.GetPane(dogName)
}
