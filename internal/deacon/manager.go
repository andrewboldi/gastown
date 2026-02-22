package deacon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/runtime"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

// Common errors
var (
	ErrNotRunning     = errors.New("deacon not running")
	ErrAlreadyRunning = errors.New("deacon already running")
)

// tmuxOps abstracts tmux operations for testing.
type tmuxOps interface {
	HasSession(name string) (bool, error)
	IsAgentAlive(session string) bool
	KillSessionWithProcesses(name string) error
	NewSessionWithCommand(name, workDir, command string) error
	SetRemainOnExit(pane string, on bool) error
	SetEnvironment(session, key, value string) error
	ConfigureGasTownSession(session string, theme tmux.Theme, rig, worker, role string) error
	WaitForCommand(session string, excludeCommands []string, timeout time.Duration) error
	SetAutoRespawnHook(session string) error
	AcceptBypassPermissionsWarning(session string) error
	SendKeysRaw(session, keys string) error
	GetSessionInfo(name string) (*tmux.SessionInfo, error)

	// Window-per-rig methods
	HasWindow(session, windowName string) (bool, error)
	KillWindowWithProcesses(session, windowName string) error
	EnsureSession(name, workDir string) (bool, error)
	NewWindowWithCommand(session, windowName, workDir, command string) error
	SetWindowOption(target, key, value string) error
}

// Manager handles deacon lifecycle operations.
type Manager struct {
	townRoot string
	tmux     tmuxOps
}

// NewManager creates a new deacon manager for a town.
func NewManager(townRoot string) *Manager {
	return &Manager{
		townRoot: townRoot,
		tmux:     tmux.NewTmux(),
	}
}

// SessionName returns the legacy tmux session name for the deacon.
// Deprecated: Use Target() for window-per-rig mode.
func SessionName() string {
	return session.DeaconSessionName()
}

// SessionName returns the legacy tmux session name for the deacon.
func (m *Manager) SessionName() string {
	return SessionName()
}

// Target returns the tmux target for the deacon (e.g., "hq:deacon").
func (m *Manager) Target() session.TmuxTarget {
	return session.DeaconTarget()
}

// deaconDir returns the working directory for the deacon.
func (m *Manager) deaconDir() string {
	return filepath.Join(m.townRoot, "deacon")
}

// Start starts the deacon window in the shared hq session.
// agentOverride allows specifying an alternate agent alias (e.g., for testing).
// Restarts are handled by daemon via ensureDeaconRunning on each heartbeat.
func (m *Manager) Start(agentOverride string) error {
	t := m.tmux
	rigSess := session.HQSessionName()
	windowName := session.DeaconWindowName()
	target := m.Target().String()

	// Check if window already exists
	running, _ := t.HasWindow(rigSess, windowName)
	if running {
		// Window exists - check if agent is actually running (healthy vs zombie)
		if t.IsAgentAlive(target) {
			return ErrAlreadyRunning
		}
		// Zombie - tmux alive but agent dead. Kill and recreate.
		if err := t.KillWindowWithProcesses(rigSess, windowName); err != nil {
			return fmt.Errorf("killing zombie window: %w", err)
		}
	}

	// Ensure deacon directory exists
	deaconDir := m.deaconDir()
	if err := os.MkdirAll(deaconDir, 0755); err != nil {
		return fmt.Errorf("creating deacon directory: %w", err)
	}

	// Ensure runtime settings exist in deaconDir where session runs.
	runtimeConfig := config.ResolveRoleAgentConfig("deacon", m.townRoot, deaconDir)
	if err := runtime.EnsureSettingsForRole(deaconDir, deaconDir, "deacon", runtimeConfig); err != nil {
		return fmt.Errorf("ensuring runtime settings: %w", err)
	}

	initialPrompt := session.BuildStartupPrompt(session.BeaconConfig{
		Recipient: "deacon",
		Sender:    "daemon",
		Topic:     "patrol",
	}, "I am Deacon. Start patrol: run gt deacon heartbeat, then check gt hook. If no hook, create mol-deacon-patrol wisp and execute it.")
	startupCmd, err := config.BuildStartupCommandFromConfig(config.AgentEnvConfig{
		Role:        "deacon",
		TownRoot:    m.townRoot,
		Prompt:      initialPrompt,
		Topic:       "patrol",
		SessionName: target,
	}, "", initialPrompt, agentOverride)
	if err != nil {
		return fmt.Errorf("building startup command: %w", err)
	}

	// Create window in hq session. EnsureSession creates hq if needed.
	created, err := t.EnsureSession(rigSess, deaconDir)
	if err != nil {
		return fmt.Errorf("ensuring hq session %s: %w", rigSess, err)
	}
	if err := t.NewWindowWithCommand(rigSess, windowName, deaconDir, startupCmd); err != nil {
		return fmt.Errorf("creating deacon window: %w", err)
	}

	// PATCH-010: Set remain-on-exit IMMEDIATELY after session creation.
	// This ensures the pane stays if Claude exits before hooks are fully set.
	// The pane will show "[Exited]" status but remain available for respawn.
	_ = t.SetRemainOnExit(target, true)

	// Set environment variables (non-fatal: window works without these)
	// Use centralized AgentEnv for consistency across all role startup paths
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:     "deacon",
		TownRoot: m.townRoot,
		Agent:    agentOverride,
	})
	for k, v := range envVars {
		_ = t.SetWindowOption(target, k, v)
	}
	if m.townRoot != "" {
		_ = t.SetEnvironment(rigSess, "GT_ROOT", m.townRoot)
	}

	// Apply Deacon theming (non-fatal: theming failure doesn't affect operation).
	// Only apply when hq session is first created to avoid clobbering user customizations.
	if created {
		theme := tmux.DeaconTheme()
		_ = t.ConfigureGasTownSession(rigSess, theme, "", "Deacon", "health-check")
	}

	// Wait for Claude to start - fatal if Claude fails to launch
	if err := t.WaitForCommand(target, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		// Kill the zombie window before returning error
		_ = t.KillWindowWithProcesses(rigSess, windowName)
		return fmt.Errorf("waiting for deacon to start: %w", err)
	}

	// Track PID for defense-in-depth orphan cleanup (non-fatal)
	if realTmux, ok := t.(*tmux.Tmux); ok {
		_ = session.TrackSessionPID(m.townRoot, target, realTmux)
	}

	// PATCH-010: Set auto-respawn hook for Deacon resilience.
	// When Claude exits (for any reason), tmux will automatically respawn it.
	// This prevents the crash loop where daemon repeatedly restarts Deacon.
	// Note: SetAutoRespawnHook calls SetRemainOnExit again (harmless, already set above).
	if err := t.SetAutoRespawnHook(target); err != nil {
		// Non-fatal: Deacon still works, just won't auto-respawn on crash
		// Daemon will still restart it, but with a delay
		fmt.Printf("warning: failed to set auto-respawn hook for deacon: %v\n", err)
	}

	// Accept bypass permissions warning dialog if it appears.
	_ = t.AcceptBypassPermissionsWarning(target)

	time.Sleep(constants.ShutdownNotifyDelay)

	return nil
}

// Stop stops the deacon window.
func (m *Manager) Stop() error {
	t := m.tmux
	rigSess := session.HQSessionName()
	windowName := session.DeaconWindowName()
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

	// Kill the window.
	// Use KillWindowWithProcesses to ensure all descendant processes are killed.
	// This prevents orphan bash processes from Claude's Bash tool surviving session termination.
	if err := t.KillWindowWithProcesses(rigSess, windowName); err != nil {
		return fmt.Errorf("killing window: %w", err)
	}

	return nil
}

// IsRunning checks if the deacon window is active.
func (m *Manager) IsRunning() (bool, error) {
	rigSess := session.HQSessionName()
	windowName := session.DeaconWindowName()
	target := m.Target().String()

	hasWindow, err := m.tmux.HasWindow(rigSess, windowName)
	if err != nil || !hasWindow {
		return false, err
	}
	return m.tmux.IsAgentAlive(target), nil
}

// Status returns information about the hq session when the deacon window is active.
func (m *Manager) Status() (*tmux.SessionInfo, error) {
	t := m.tmux
	rigSess := session.HQSessionName()

	running, err := t.HasWindow(rigSess, session.DeaconWindowName())
	if err != nil {
		return nil, fmt.Errorf("checking window: %w", err)
	}
	if !running {
		return nil, ErrNotRunning
	}

	return t.GetSessionInfo(rigSess)
}
