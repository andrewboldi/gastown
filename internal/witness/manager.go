package witness

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/runtime"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/style"
	"github.com/steveyegge/gastown/internal/tmux"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Common errors
var (
	ErrNotRunning     = errors.New("witness not running")
	ErrAlreadyRunning = errors.New("witness already running")
)

// Manager handles witness lifecycle and monitoring operations.
// ZFC-compliant: tmux session is the source of truth for running state.
type Manager struct {
	rig *rig.Rig
}

// NewManager creates a new witness manager for a rig.
func NewManager(r *rig.Rig) *Manager {
	return &Manager{
		rig: r,
	}
}

// IsRunning checks if the witness window is active.
// ZFC: tmux window existence is the source of truth.
func (m *Manager) IsRunning() (bool, error) {
	t := tmux.NewTmux()
	rigSess := m.rigSession()
	window := session.WitnessWindowName()
	target := m.Target().String()

	has, err := t.HasWindow(rigSess, window)
	if err != nil || !has {
		return false, err
	}
	return t.IsAgentAlive(target), nil
}

// IsHealthy checks if the witness window is running and active recently.
func (m *Manager) IsHealthy(maxInactivity time.Duration) tmux.ZombieStatus {
	t := tmux.NewTmux()
	rigSess := m.rigSession()
	window := session.WitnessWindowName()
	target := m.Target().String()

	hasWindow, err := t.HasWindow(rigSess, window)
	if err != nil || !hasWindow {
		return tmux.SessionDead
	}
	if !t.IsAgentAlive(target) {
		return tmux.AgentDead
	}
	if maxInactivity > 0 {
		if lastActivity, err := t.GetSessionActivity(rigSess); err == nil && !lastActivity.IsZero() {
			if time.Since(lastActivity) > maxInactivity {
				return tmux.AgentHung
			}
		}
	}
	return tmux.SessionHealthy
}

// SessionName returns the legacy tmux session name for this witness.
// Deprecated: Use Target() for window-per-rig mode.
func (m *Manager) SessionName() string {
	return session.WitnessSessionName(session.PrefixFor(m.rig.Name))
}

// rigSession returns the rig session name (e.g., "gt").
func (m *Manager) rigSession() string {
	return session.RigSessionName(session.PrefixFor(m.rig.Name))
}

// Target returns the tmux target for this witness (e.g., "gt:witness").
func (m *Manager) Target() session.TmuxTarget {
	return session.WitnessTarget(session.PrefixFor(m.rig.Name))
}

// Status returns information about the witness session.
// ZFC-compliant: tmux window existence is the source of truth.
func (m *Manager) Status() (*tmux.SessionInfo, error) {
	t := tmux.NewTmux()
	rigSess := m.rigSession()

	running, err := t.HasWindow(rigSess, session.WitnessWindowName())
	if err != nil {
		return nil, fmt.Errorf("checking window: %w", err)
	}
	if !running {
		return nil, ErrNotRunning
	}

	return t.GetSessionInfo(rigSess)
}

// witnessDir returns the working directory for the witness.
// Prefers witness/rig/, falls back to witness/, then rig root.
func (m *Manager) witnessDir() string {
	witnessRigDir := filepath.Join(m.rig.Path, "witness", "rig")
	if _, err := os.Stat(witnessRigDir); err == nil {
		return witnessRigDir
	}

	witnessDir := filepath.Join(m.rig.Path, "witness")
	if _, err := os.Stat(witnessDir); err == nil {
		return witnessDir
	}

	return m.rig.Path
}

// Start starts the witness.
// If foreground is true, returns an error (foreground mode deprecated).
// Otherwise, spawns a Claude agent in a tmux session.
// agentOverride optionally specifies a different agent alias to use.
// envOverrides are KEY=VALUE pairs that override all other env var sources.
// ZFC-compliant: no state file, tmux session is source of truth.
func (m *Manager) Start(foreground bool, agentOverride string, envOverrides []string) error {
	t := tmux.NewTmux()
	rigSess := m.rigSession()
	windowName := session.WitnessWindowName()
	target := m.Target().String()

	if foreground {
		// Foreground mode is deprecated - patrol logic moved to mol-witness-patrol
		return fmt.Errorf("foreground mode is deprecated; use background mode (remove --foreground flag)")
	}

	// Check if window already exists
	running, _ := t.HasWindow(rigSess, windowName)
	if running {
		// Window exists - check if Claude is actually running (healthy vs zombie)
		if t.IsAgentAlive(target) {
			// Healthy - Claude is running
			return ErrAlreadyRunning
		}
		// Zombie - tmux alive but Claude dead. Kill and recreate.
		if err := t.KillWindowWithProcesses(rigSess, windowName); err != nil {
			return fmt.Errorf("killing zombie window: %w", err)
		}
	}

	// Note: No PID check per ZFC - tmux window is the source of truth

	// Working directory
	witnessDir := m.witnessDir()

	// Ensure runtime settings exist in the shared witness parent directory.
	// Settings are passed to Claude Code via --settings flag.
	// ResolveRoleAgentConfig is internally serialized (resolveConfigMu in
	// package config) to prevent concurrent rig starts from corrupting the
	// global agent registry.
	townRoot := m.townRoot()
	runtimeConfig := config.ResolveRoleAgentConfig("witness", townRoot, m.rig.Path)
	witnessSettingsDir := config.RoleSettingsDir("witness", m.rig.Path)
	if err := runtime.EnsureSettingsForRole(witnessSettingsDir, witnessDir, "witness", runtimeConfig); err != nil {
		return fmt.Errorf("ensuring runtime settings: %w", err)
	}

	// Ensure .gitignore has required Gas Town patterns
	if err := rig.EnsureGitignorePatterns(witnessDir); err != nil {
		style.PrintWarning("could not update witness .gitignore: %v", err)
	}

	roleConfig, err := m.roleConfig()
	if err != nil {
		return err
	}

	// Build startup command first
	// NOTE: No gt prime injection needed - SessionStart hook handles it automatically
	// Export GT_ROLE and BD_ACTOR in the command since tmux SetEnvironment only affects new panes
	// Pass m.rig.Path so rig agent settings are honored (not town-level defaults)
	command, err := buildWitnessStartCommand(m.rig.Path, m.rig.Name, townRoot, target, agentOverride, roleConfig)
	if err != nil {
		return err
	}

	// Create window in rig session. EnsureSession creates the rig session if needed.
	created, err := t.EnsureSession(rigSess, witnessDir)
	if err != nil {
		return fmt.Errorf("ensuring rig session %s: %w", rigSess, err)
	}
	if err := t.NewWindowWithCommand(rigSess, windowName, witnessDir, command); err != nil {
		return fmt.Errorf("creating witness window: %w", err)
	}

	// Set agent metadata as window options (non-fatal: window works without these)
	// Use centralized AgentEnv for consistency across all role startup paths
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:     "witness",
		Rig:      m.rig.Name,
		TownRoot: townRoot,
		Agent:    agentOverride,
	})
	for k, v := range envVars {
		_ = t.SetWindowOption(target, k, v)
	}
	// Apply role config env vars as window options (non-fatal).
	for key, value := range roleConfigEnvVars(roleConfig, townRoot, m.rig.Name) {
		_ = t.SetWindowOption(target, key, value)
	}
	// Apply CLI env overrides (highest priority, non-fatal).
	for _, override := range envOverrides {
		if key, value, ok := strings.Cut(override, "="); ok {
			_ = t.SetWindowOption(target, key, value)
		}
	}
	// Shared session-level env vars (set once, idempotent).
	if townRoot != "" {
		_ = t.SetEnvironment(rigSess, "GT_ROOT", townRoot)
	}

	// Apply Gas Town theming only when rig session first created (non-fatal)
	if created {
		theme := tmux.AssignTheme(m.rig.Name)
		_ = t.ConfigureGasTownSession(rigSess, theme, m.rig.Name, "witness", "witness")
	}

	// Wait for Claude to start - fatal if Claude fails to launch
	if err := t.WaitForCommand(target, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
		// Kill the zombie window before returning error
		_ = t.KillWindowWithProcesses(rigSess, windowName)
		return fmt.Errorf("waiting for witness to start: %w", err)
	}

	// Accept bypass permissions warning dialog if it appears.
	if err := t.AcceptBypassPermissionsWarning(target); err != nil {
		log.Printf("warning: accepting bypass permissions for %s: %v", target, err)
	}

	// For non-hook runtimes (e.g. codex), inject startup fallback commands.
	// This runs gt prime so role context is available even without SessionStart hooks.
	_ = runtime.RunStartupFallback(t, target, "witness", runtimeConfig)
	// Track PID for defense-in-depth orphan cleanup (non-fatal)
	if err := session.TrackSessionPID(townRoot, target, t); err != nil {
		log.Printf("warning: tracking session PID for %s: %v", target, err)
	}

	time.Sleep(constants.ShutdownNotifyDelay)

	return nil
}

func (m *Manager) roleConfig() (*beads.RoleConfig, error) {
	// Role beads use hq- prefix and live in town-level beads, not rig beads
	townRoot := m.townRoot()
	bd := beads.NewWithBeadsDir(townRoot, beads.ResolveBeadsDir(townRoot))
	roleConfig, err := bd.GetRoleConfig(beads.RoleBeadIDTown("witness"))
	if err != nil {
		return nil, fmt.Errorf("loading witness role config: %w", err)
	}
	return roleConfig, nil
}

func (m *Manager) townRoot() string {
	townRoot, err := workspace.Find(m.rig.Path)
	if err != nil || townRoot == "" {
		return m.rig.Path
	}
	return townRoot
}

func roleConfigEnvVars(roleConfig *beads.RoleConfig, townRoot, rigName string) map[string]string {
	if roleConfig == nil || len(roleConfig.EnvVars) == 0 {
		return nil
	}
	expanded := make(map[string]string, len(roleConfig.EnvVars))
	for key, value := range roleConfig.EnvVars {
		expanded[key] = beads.ExpandRolePattern(value, townRoot, rigName, "", "witness", session.PrefixFor(rigName))
	}
	return expanded
}

func buildWitnessStartCommand(rigPath, rigName, townRoot, sessionName, agentOverride string, roleConfig *beads.RoleConfig) (string, error) {
	if agentOverride != "" {
		roleConfig = nil
	}
	if roleConfig != nil && roleConfig.StartCommand != "" {
		return beads.ExpandRolePattern(roleConfig.StartCommand, townRoot, rigName, "", "witness", session.PrefixFor(rigName)), nil
	}
	initialPrompt := session.BuildStartupPrompt(session.BeaconConfig{
		Recipient: session.BeaconRecipient("witness", "", rigName),
		Sender:    "deacon",
		Topic:     "patrol",
	}, "Run `gt prime --hook` and begin patrol.")
	command, err := config.BuildStartupCommandFromConfig(config.AgentEnvConfig{
		Role:        "witness",
		Rig:         rigName,
		TownRoot:    townRoot,
		Prompt:      initialPrompt,
		Topic:       "patrol",
		SessionName: sessionName,
	}, rigPath, initialPrompt, agentOverride)
	if err != nil {
		return "", fmt.Errorf("building startup command: %w", err)
	}
	return command, nil
}

// Stop stops the witness.
// ZFC-compliant: tmux window existence is the source of truth.
func (m *Manager) Stop() error {
	t := tmux.NewTmux()
	rigSess := m.rigSession()
	windowName := session.WitnessWindowName()

	// Check if tmux window exists
	running, _ := t.HasWindow(rigSess, windowName)
	if !running {
		return ErrNotRunning
	}

	// Kill the tmux window
	return t.KillWindowWithProcesses(rigSess, windowName)
}
