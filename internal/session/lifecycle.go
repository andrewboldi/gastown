// Package session provides polecat session lifecycle management.
package session

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/runtime"
	"github.com/steveyegge/gastown/internal/telemetry"
	"github.com/steveyegge/gastown/internal/tmux"
)

// SessionConfig describes how to create and start a tmux session.
// This unifies the common startup pattern that was previously duplicated
// across polecat, mayor, boot, deacon, witness, refinery, crew, and dog
// session managers. Each of those managers previously had to coordinate
// 4+ packages (config, runtime, session, tmux) manually.
//
// Usage pattern:
//
//	result, err := session.StartSession(t, session.SessionConfig{
//	    SessionID: "gt-myrig-toast",
//	    WorkDir:   "/path/to/worktree",
//	    Role:      "polecat",
//	    TownRoot:  "/path/to/town",
//	    Beacon:    session.BeaconConfig{...},
//	})
type SessionConfig struct {
	// SessionID is the tmux session name (e.g., "gt-wyvern-Toast", "hq-mayor").
	// In window-per-rig mode, this is the rig session (e.g., "gt", "hq").
	SessionID string

	// WindowName, when non-empty, enables window-per-rig mode.
	// The agent is created as a named window within SessionID instead of
	// as a separate tmux session. E.g., WindowName="witness" creates
	// window "witness" in session "gt".
	WindowName string

	// WorkDir is the working directory for the session.
	WorkDir string

	// Role is the agent role (e.g., "polecat", "mayor", "boot", "deacon").
	Role string

	// TownRoot is the root of the Gas Town workspace (e.g., ~/gt).
	TownRoot string

	// RigPath is the rig directory path for config resolution.
	// Empty for town-level agents (mayor, deacon, boot).
	RigPath string

	// RigName is the rig name for environment variables and theming.
	// Empty for town-level agents.
	RigName string

	// AgentName is the specific agent name within a rig.
	// Used for polecats, crew, and dogs. Empty for singletons.
	AgentName string

	// Command is a pre-built startup command. If non-empty, skips command building.
	// If empty, the command is built from Beacon + config.BuildAgentStartupCommand.
	Command string

	// Beacon configures the startup beacon message for session identification.
	// Ignored if Command is non-empty.
	Beacon BeaconConfig

	// Instructions are appended after the beacon in the startup prompt.
	// Used by roles like Boot and Deacon that need explicit instructions.
	// Ignored if Command is non-empty.
	Instructions string

	// AgentOverride optionally specifies a different agent alias (e.g., "opencode").
	AgentOverride string

	// RuntimeConfigDir overrides the config directory for the runtime.
	RuntimeConfigDir string

	// ExtraEnv adds additional environment variables beyond the standard AgentEnv set.
	// These are set in the tmux session environment after the standard vars.
	ExtraEnv map[string]string

	// Theme is the tmux theme to apply. Nil means no theme is applied.
	Theme *tmux.Theme

	// Post-start behavior options.

	// WaitForAgent waits for the agent command to appear in the pane.
	WaitForAgent bool

	// WaitFatal makes WaitForAgent failure fatal — kills the session and returns error.
	// If false, WaitForAgent failure is silently ignored.
	WaitFatal bool

	// AcceptBypass accepts the bypass permissions warning dialog if it appears.
	AcceptBypass bool

	// ReadyDelay sleeps for the runtime's configured readiness delay.
	ReadyDelay bool

	// AutoRespawn sets the auto-respawn hook so the session survives crashes.
	AutoRespawn bool

	// RemainOnExit sets remain-on-exit immediately after session creation.
	RemainOnExit bool

	// TrackPID tracks the pane PID for defense-in-depth orphan cleanup.
	TrackPID bool

	// VerifySurvived checks that the session is still alive after startup.
	VerifySurvived bool
}

// tmuxTarget returns the tmux target string for this config.
// In window-per-rig mode: "session:window". Otherwise: "session".
func (c *SessionConfig) tmuxTarget() string {
	if c.WindowName != "" {
		return c.SessionID + ":" + c.WindowName
	}
	return c.SessionID
}

// isWindowMode returns true if this config uses window-per-rig mode.
func (c *SessionConfig) isWindowMode() bool {
	return c.WindowName != ""
}

// StartResult contains the results of session startup.
type StartResult struct {
	// RuntimeConfig is the resolved runtime config for the role.
	// Callers may need this for role-specific post-startup steps
	// (e.g., handling fallback nudges, legacy fallback).
	RuntimeConfig *config.RuntimeConfig
}

// StartSession creates a tmux session following the standard Gas Town lifecycle.
//
// The lifecycle handles:
//  1. Resolve runtime config for the role
//  2. Ensure settings/plugins exist for the agent
//  3. Build startup command (if not provided)
//  4. Create tmux session with command
//  5. Set environment variables (standard + extra)
//  6. Apply theme (if configured)
//  7. Optional post-start: wait for agent, accept bypass, ready delay,
//     auto-respawn, PID tracking, verify survived
//
// Role-specific concerns (issue validation, fallback nudges, pane-died hooks,
// crew cycle bindings, etc.) should be handled by the caller before/after
// calling StartSession.
func StartSession(t *tmux.Tmux, cfg SessionConfig) (_ *StartResult, retErr error) {
	defer func() { telemetry.RecordSessionStart(context.Background(), cfg.SessionID, cfg.Role, retErr) }()
	if cfg.SessionID == "" {
		return nil, fmt.Errorf("SessionID is required")
	}
	if cfg.WorkDir == "" {
		return nil, fmt.Errorf("WorkDir is required")
	}
	if cfg.Role == "" {
		return nil, fmt.Errorf("Role is required")
	}

	// 1. Resolve runtime config.
	runtimeConfig := config.ResolveRoleAgentConfig(cfg.Role, cfg.TownRoot, cfg.RigPath)

	// 2. Ensure settings/plugins exist for the agent.
	settingsDir := config.RoleSettingsDir(cfg.Role, cfg.RigPath)
	if settingsDir == "" {
		settingsDir = cfg.WorkDir
	}
	if err := runtime.EnsureSettingsForRole(settingsDir, cfg.WorkDir, cfg.Role, runtimeConfig); err != nil {
		return nil, fmt.Errorf("ensuring runtime settings: %w", err)
	}

	// 3. Build startup command if not provided.
	command := cfg.Command
	if command == "" {
		prompt := buildPrompt(cfg)
		var err error
		command, err = buildCommand(cfg, prompt)
		if err != nil {
			return nil, fmt.Errorf("building startup command: %w", err)
		}
	}

	// Prepend runtime config dir env if needed.
	if runtimeConfig.Session != nil && runtimeConfig.Session.ConfigDirEnv != "" && cfg.RuntimeConfigDir != "" {
		command = config.PrependEnv(command, map[string]string{
			runtimeConfig.Session.ConfigDirEnv: cfg.RuntimeConfigDir,
		})
	}

	// Prepend extra env vars that need to be in the command (for initial shell inheritance).
	if len(cfg.ExtraEnv) > 0 {
		command = config.PrependEnv(command, cfg.ExtraEnv)
	}

	// 4. Create tmux session/window with command.
	target := cfg.tmuxTarget()
	if cfg.isWindowMode() {
		// Window-per-rig mode: ensure rig session exists, create agent as window.
		if _, err := t.EnsureSession(cfg.SessionID, cfg.WorkDir); err != nil {
			return nil, fmt.Errorf("ensuring rig session %s: %w", cfg.SessionID, err)
		}
		if err := t.NewWindowWithCommand(cfg.SessionID, cfg.WindowName, cfg.WorkDir, command); err != nil {
			return nil, fmt.Errorf("creating window %s in %s: %w", cfg.WindowName, cfg.SessionID, err)
		}
	} else {
		// Legacy mode: one session per agent.
		if err := t.NewSessionWithCommand(cfg.SessionID, cfg.WorkDir, command); err != nil {
			return nil, fmt.Errorf("creating session: %w", err)
		}
	}

	// 5. Set remain-on-exit immediately if requested (before anything else can fail).
	if cfg.RemainOnExit {
		_ = t.SetRemainOnExit(target, true)
	}

	// 6. Set environment variables.
	envVars := config.AgentEnv(config.AgentEnvConfig{
		Role:             cfg.Role,
		Rig:              cfg.RigName,
		AgentName:        cfg.AgentName,
		TownRoot:         cfg.TownRoot,
		RuntimeConfigDir: cfg.RuntimeConfigDir,
		Agent:            cfg.AgentOverride,
	})
	envVars = mergeRuntimeLivenessEnv(envVars, runtimeConfig)
	if cfg.isWindowMode() {
		// Window mode: store agent metadata as window options for per-window queries.
		// Shared vars (GT_ROOT, GIT_CEILING_DIRECTORIES) go to session env.
		for _, k := range mapKeysSorted(envVars) {
			_ = t.SetWindowOption(target, k, envVars[k])
		}
		for _, k := range mapKeysSorted(cfg.ExtraEnv) {
			_ = t.SetWindowOption(target, k, cfg.ExtraEnv[k])
		}
		// Also set shared env vars at session level (once, idempotent).
		if cfg.TownRoot != "" {
			_ = t.SetEnvironment(cfg.SessionID, "GT_ROOT", cfg.TownRoot)
		}
	} else {
		// Legacy mode: all env vars as session environment.
		for _, k := range mapKeysSorted(envVars) {
			_ = t.SetEnvironment(cfg.SessionID, k, envVars[k])
		}
		for _, k := range mapKeysSorted(cfg.ExtraEnv) {
			_ = t.SetEnvironment(cfg.SessionID, k, cfg.ExtraEnv[k])
		}
	}

	// 7. Apply theme.
	if cfg.Theme != nil {
		_ = t.ConfigureGasTownSession(cfg.SessionID, *cfg.Theme, cfg.RigName, cfg.AgentName, cfg.Role)
	}

	// 8. Wait for agent to start.
	if cfg.WaitForAgent {
		if err := t.WaitForCommand(target, constants.SupportedShells, constants.ClaudeStartTimeout); err != nil {
			if cfg.WaitFatal {
				if cfg.isWindowMode() {
					_ = t.KillWindowWithProcesses(cfg.SessionID, cfg.WindowName)
				} else {
					_ = t.KillSessionWithProcesses(cfg.SessionID)
				}
				return nil, fmt.Errorf("waiting for %s to start: %w", cfg.Role, err)
			}
		}
	}

	// 9. Auto-respawn hook.
	if cfg.AutoRespawn {
		if err := t.SetAutoRespawnHook(target); err != nil {
			fmt.Printf("warning: failed to set auto-respawn hook for %s: %v\n", cfg.Role, err)
		}
	}

	// 10. Accept bypass permissions warning.
	if cfg.AcceptBypass {
		_ = t.AcceptBypassPermissionsWarning(target)
	}

	// 11. Ready delay: wait for agent to be fully ready at the prompt.
	// Uses prompt-based polling for agents with ReadyPromptPrefix,
	// falling back to ReadyDelayMs sleep for agents without prompt detection.
	if cfg.ReadyDelay {
		if err := t.WaitForRuntimeReady(target, runtimeConfig, constants.ClaudeStartTimeout); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: agent readiness detection timed out for %s: %v\n", cfg.SessionID, err)
		}
	}

	// 12. Verify session/window survived startup.
	if cfg.VerifySurvived {
		if cfg.isWindowMode() {
			alive, err := t.HasWindow(cfg.SessionID, cfg.WindowName)
			if err != nil {
				_ = t.KillWindowWithProcesses(cfg.SessionID, cfg.WindowName)
				return nil, fmt.Errorf("verifying window: %w", err)
			}
			if !alive {
				return nil, fmt.Errorf("window %s:%s died during startup", cfg.SessionID, cfg.WindowName)
			}
		} else {
			running, err := t.HasSession(cfg.SessionID)
			if err != nil {
				_ = t.KillSessionWithProcesses(cfg.SessionID)
				return nil, fmt.Errorf("verifying session: %w", err)
			}
			if !running {
				return nil, fmt.Errorf("session %s died during startup (agent command may have failed)", cfg.SessionID)
			}
		}
	}

	// 13. Startup fallback for runtimes without executable hooks.
	// Non-fatal by design: session startup should not fail if nudge injection fails.
	_ = runtime.RunStartupFallbackWithDelay(t, target, cfg.Role, runtimeConfig)

	// 14. Track PID for defense-in-depth orphan cleanup.
	if cfg.TrackPID && cfg.TownRoot != "" {
		_ = TrackSessionPID(cfg.TownRoot, cfg.SessionID, t)
	}

	return &StartResult{RuntimeConfig: runtimeConfig}, nil
}

// StopSession stops a tmux session with optional graceful shutdown.
//
// If graceful is true, sends Ctrl-C first and waits for the session to exit
// before force-killing. This allows the agent to clean up.
func StopSession(t *tmux.Tmux, sessionID string, graceful bool) error {
	running, err := t.HasSession(sessionID)
	if err != nil {
		return fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if graceful {
		_ = t.SendKeysRaw(sessionID, "C-c")
		WaitForSessionExit(t, sessionID, constants.GracefulShutdownTimeout)
	}

	if err := t.KillSessionWithProcesses(sessionID); err != nil {
		return fmt.Errorf("killing session: %w", err)
	}

	return nil
}

// StopWindow stops a tmux window with optional graceful shutdown.
// Like StopSession but scoped to a single window within a session.
func StopWindow(t *tmux.Tmux, session, window string, graceful bool) error {
	has, err := t.HasWindow(session, window)
	if err != nil {
		return fmt.Errorf("checking window: %w", err)
	}
	if !has {
		return fmt.Errorf("window not found: %s:%s", session, window)
	}

	target := session + ":" + window
	if graceful {
		_ = t.SendKeysRaw(target, "C-c")
		WaitForWindowExit(t, session, window, constants.GracefulShutdownTimeout)
	}

	if err := t.KillWindowWithProcesses(session, window); err != nil {
		return fmt.Errorf("killing window: %w", err)
	}

	return nil
}

// WaitForWindowExit polls for a window to disappear within the given timeout.
func WaitForWindowExit(t *tmux.Tmux, session, window string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		has, err := t.HasWindow(session, window)
		if err != nil || !has {
			return true
		}
		time.Sleep(constants.PollInterval)
	}
	return false
}

func mapKeysSorted(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// mergeRuntimeLivenessEnv ensures liveness-critical env vars are present in the
// tmux environment table, even when agent resolution came from workspace/default
// settings rather than an explicit --agent override.
func mergeRuntimeLivenessEnv(envVars map[string]string, runtimeConfig *config.RuntimeConfig) map[string]string {
	if envVars == nil {
		envVars = make(map[string]string)
	}
	if runtimeConfig == nil {
		return envVars
	}

	if _, hasGTAgent := envVars["GT_AGENT"]; !hasGTAgent && runtimeConfig.ResolvedAgent != "" {
		envVars["GT_AGENT"] = runtimeConfig.ResolvedAgent
	}

	if _, hasProcessNames := envVars["GT_PROCESS_NAMES"]; !hasProcessNames {
		agentForLookup := runtimeConfig.ResolvedAgent
		commandForLookup := runtimeConfig.Command
		if existing, ok := envVars["GT_AGENT"]; ok && existing != "" {
			agentForLookup = existing
			// When GT_AGENT was set by AgentOverride (differs from the
			// workspace-resolved agent), the runtimeConfig.Command belongs
			// to the workspace agent, not the override. Pass empty command
			// so ResolveProcessNames uses the preset's own command.
			if existing != runtimeConfig.ResolvedAgent {
				commandForLookup = ""
			}
		}
		processNames := config.ResolveProcessNames(agentForLookup, commandForLookup)
		if len(processNames) > 0 {
			envVars["GT_PROCESS_NAMES"] = strings.Join(processNames, ",")
		}
	}

	return envVars
}

// KillExistingSession kills an existing session if one is found.
// Returns true if a session was killed.
//
// If checkAlive is true, only kills zombie sessions (tmux alive but agent dead).
// If the session exists and the agent is alive, returns ErrAlreadyRunning.
// If checkAlive is false, kills any existing session unconditionally.
func KillExistingSession(t *tmux.Tmux, sessionID string, checkAlive bool) (bool, error) {
	running, err := t.HasSession(sessionID)
	if err != nil {
		return false, fmt.Errorf("checking session: %w", err)
	}
	if !running {
		return false, nil
	}

	if checkAlive && t.IsAgentAlive(sessionID) {
		return false, fmt.Errorf("session already running: %s", sessionID)
	}

	if err := t.KillSessionWithProcesses(sessionID); err != nil {
		return false, fmt.Errorf("killing session %s: %w", sessionID, err)
	}

	return true, nil
}

// KillExistingWindow kills an existing window if one is found.
// Returns true if a window was killed. The session is left intact.
func KillExistingWindow(t *tmux.Tmux, session, window string) (bool, error) {
	has, err := t.HasWindow(session, window)
	if err != nil {
		return false, fmt.Errorf("checking window: %w", err)
	}
	if !has {
		return false, nil
	}

	if err := t.KillWindowWithProcesses(session, window); err != nil {
		return false, fmt.Errorf("killing window %s:%s: %w", session, window, err)
	}

	return true, nil
}

// buildPrompt creates the startup prompt from beacon + instructions.
func buildPrompt(cfg SessionConfig) string {
	if cfg.Instructions != "" {
		return BuildStartupPrompt(cfg.Beacon, cfg.Instructions)
	}
	return FormatStartupBeacon(cfg.Beacon)
}

// buildCommand creates the startup command using the config package.
func buildCommand(cfg SessionConfig, prompt string) (string, error) {
	if cfg.AgentOverride != "" {
		return config.BuildAgentStartupCommandWithAgentOverride(
			cfg.Role, cfg.RigName, cfg.TownRoot, cfg.RigPath, prompt, cfg.AgentOverride)
	}
	return config.BuildAgentStartupCommand(
		cfg.Role, cfg.RigName, cfg.TownRoot, cfg.RigPath, prompt), nil
}

// ShutdownDelay is the standard delay after session creation.
// Some roles use this instead of the runtime's ready delay.
func ShutdownDelay() time.Duration {
	return constants.ShutdownNotifyDelay
}
