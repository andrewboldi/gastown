// Package session provides polecat session lifecycle management.
package session

import (
	"fmt"
)

// DefaultPrefix is the default beads prefix used when no rig-specific prefix is known.
const DefaultPrefix = "gt"

// HQPrefix is the prefix for town-level services (Mayor, Deacon).
const HQPrefix = "hq-"

// MayorSessionName returns the session name for the Mayor agent.
// One mayor per machine - multi-town requires containers/VMs for isolation.
func MayorSessionName() string {
	return HQPrefix + "mayor"
}

// DeaconSessionName returns the session name for the Deacon agent.
// One deacon per machine - multi-town requires containers/VMs for isolation.
func DeaconSessionName() string {
	return HQPrefix + "deacon"
}

// WitnessSessionName returns the session name for a rig's Witness agent.
// rigPrefix is the rig's beads prefix (e.g., "gt" for gastown, "bd" for beads).
func WitnessSessionName(rigPrefix string) string {
	return fmt.Sprintf("%s-witness", rigPrefix)
}

// RefinerySessionName returns the session name for a rig's Refinery agent.
// rigPrefix is the rig's beads prefix (e.g., "gt" for gastown, "bd" for beads).
func RefinerySessionName(rigPrefix string) string {
	return fmt.Sprintf("%s-refinery", rigPrefix)
}

// CrewSessionName returns the session name for a crew worker in a rig.
// rigPrefix is the rig's beads prefix (e.g., "gt" for gastown, "bd" for beads).
func CrewSessionName(rigPrefix, name string) string {
	return fmt.Sprintf("%s-crew-%s", rigPrefix, name)
}

// PolecatSessionName returns the session name for a polecat in a rig.
// rigPrefix is the rig's beads prefix (e.g., "gt" for gastown, "bd" for beads).
func PolecatSessionName(rigPrefix, name string) string {
	return fmt.Sprintf("%s-%s", rigPrefix, name)
}

// OverseerSessionName returns the session name for the human operator.
// The overseer is the human who controls Gas Town, not an AI agent.
func OverseerSessionName() string {
	return HQPrefix + "overseer"
}

// BootSessionName returns the session name for the Boot watchdog.
// Boot is town-level (launched by deacon), so it uses the hq- prefix.
// "hq-boot" avoids tmux prefix-matching collisions with "hq-deacon".
func BootSessionName() string {
	return HQPrefix + "boot"
}

// --- Window-per-rig naming ---
// In the window-per-rig model, each rig has one tmux session (e.g., "gt")
// and each agent is a named window within it (e.g., "witness", "crew-max").

// HQSession is the session name for town-level agents.
const HQSession = "hq"

// RigSessionName returns the session name for a rig.
// In the window-per-rig model this is just the rig prefix (e.g., "gt", "bd").
func RigSessionName(rigPrefix string) string {
	return rigPrefix
}

// HQSessionName returns the session name for town-level agents.
func HQSessionName() string {
	return HQSession
}

// MayorWindowName returns the window name for the Mayor agent.
func MayorWindowName() string { return "mayor" }

// DeaconWindowName returns the window name for the Deacon agent.
func DeaconWindowName() string { return "deacon" }

// BootWindowName returns the window name for the Boot watchdog.
func BootWindowName() string { return "boot" }

// OverseerWindowName returns the window name for the human operator.
func OverseerWindowName() string { return "overseer" }

// WitnessWindowName returns the window name for a rig's Witness agent.
func WitnessWindowName() string { return "witness" }

// RefineryWindowName returns the window name for a rig's Refinery agent.
func RefineryWindowName() string { return "refinery" }

// CrewWindowName returns the window name for a crew worker.
func CrewWindowName(name string) string {
	return "crew-" + name
}

// PolecatWindowName returns the window name for a polecat.
func PolecatWindowName(name string) string {
	return name
}

// MayorTarget returns the tmux target for the Mayor agent.
func MayorTarget() TmuxTarget {
	return NewTarget(HQSession, MayorWindowName())
}

// DeaconTarget returns the tmux target for the Deacon agent.
func DeaconTarget() TmuxTarget {
	return NewTarget(HQSession, DeaconWindowName())
}

// BootTarget returns the tmux target for the Boot watchdog.
func BootTarget() TmuxTarget {
	return NewTarget(HQSession, BootWindowName())
}

// WitnessTarget returns the tmux target for a rig's Witness agent.
func WitnessTarget(rigPrefix string) TmuxTarget {
	return NewTarget(RigSessionName(rigPrefix), WitnessWindowName())
}

// RefineryTarget returns the tmux target for a rig's Refinery agent.
func RefineryTarget(rigPrefix string) TmuxTarget {
	return NewTarget(RigSessionName(rigPrefix), RefineryWindowName())
}

// CrewTarget returns the tmux target for a crew worker in a rig.
func CrewTarget(rigPrefix, name string) TmuxTarget {
	return NewTarget(RigSessionName(rigPrefix), CrewWindowName(name))
}

// PolecatTarget returns the tmux target for a polecat in a rig.
func PolecatTarget(rigPrefix, name string) TmuxTarget {
	return NewTarget(RigSessionName(rigPrefix), PolecatWindowName(name))
}
