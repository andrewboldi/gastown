// Package session provides polecat session lifecycle management.
package session

import (
	"fmt"
	"strings"
)

// TmuxTarget represents a tmux target in "session:window" format.
// This is the canonical way to reference an agent in the window-per-rig model.
type TmuxTarget string

// NewTarget creates a TmuxTarget from session and window names.
func NewTarget(session, window string) TmuxTarget {
	return TmuxTarget(session + ":" + window)
}

// Session returns the session component of the target.
func (t TmuxTarget) Session() string {
	if i := strings.IndexByte(string(t), ':'); i >= 0 {
		return string(t)[:i]
	}
	return string(t)
}

// Window returns the window component of the target.
// Returns empty string if no window is specified.
func (t TmuxTarget) Window() string {
	if i := strings.IndexByte(string(t), ':'); i >= 0 {
		return string(t)[i+1:]
	}
	return ""
}

// WithPane appends a pane index to the target (e.g., "gt:witness.0").
func (t TmuxTarget) WithPane(pane int) string {
	return fmt.Sprintf("%s.%d", t, pane)
}

// String returns the target string.
func (t TmuxTarget) String() string {
	return string(t)
}
