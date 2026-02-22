package tmux

import "testing"

func TestLegacySessionWindow(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantSession string
		wantWindow  string
		wantOK      bool
	}{
		{name: "hq mayor", in: "hq-mayor", wantSession: "hq", wantWindow: "mayor", wantOK: true},
		{name: "hq deacon", in: "hq-deacon", wantSession: "hq", wantWindow: "deacon", wantOK: true},
		{name: "hq boot", in: "hq-boot", wantSession: "hq", wantWindow: "boot", wantOK: true},
		{name: "hq dog", in: "hq-dog-rufus", wantSession: "hq", wantWindow: "dog-rufus", wantOK: true},
		{name: "rig witness", in: "gt-witness", wantSession: "gt", wantWindow: "witness", wantOK: true},
		{name: "rig refinery", in: "gt-refinery", wantSession: "gt", wantWindow: "refinery", wantOK: true},
		{name: "rig crew", in: "gt-crew-max", wantSession: "gt", wantWindow: "crew-max", wantOK: true},
		{name: "rig polecat", in: "gt-Toast", wantSession: "gt", wantWindow: "Toast", wantOK: true},
		{name: "hq unknown", in: "hq-unknown", wantOK: false},
		{name: "no dash", in: "gt", wantOK: false},
		{name: "target already", in: "gt:witness", wantOK: false},
		{name: "pane id", in: "%1", wantOK: false},
		{name: "empty", in: "", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSession, gotWindow, gotOK := legacySessionWindow(tt.in)
			if gotOK != tt.wantOK {
				t.Fatalf("legacySessionWindow(%q) ok = %v, want %v", tt.in, gotOK, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if gotSession != tt.wantSession || gotWindow != tt.wantWindow {
				t.Fatalf(
					"legacySessionWindow(%q) = (%q, %q), want (%q, %q)",
					tt.in,
					gotSession,
					gotWindow,
					tt.wantSession,
					tt.wantWindow,
				)
			}
		})
	}
}

func TestLegacyAliasForWindow(t *testing.T) {
	tests := []struct {
		name    string
		session string
		window  string
		role    string
		want    string
		wantOK  bool
	}{
		{name: "hq mayor by window", session: "hq", window: "mayor", want: "hq-mayor", wantOK: true},
		{name: "hq dog by role", session: "hq", window: "dog-rufus", role: "dog", want: "hq-dog-rufus", wantOK: true},
		{name: "rig witness by role", session: "gt", window: "witness", role: "witness", want: "gt-witness", wantOK: true},
		{name: "rig crew by window", session: "gt", window: "crew-max", want: "gt-crew-max", wantOK: true},
		{name: "rig polecat by role", session: "gt", window: "Toast", role: "polecat", want: "gt-Toast", wantOK: true},
		{name: "skip feed window", session: "gt", window: "feed", wantOK: false},
		{name: "skip unknown hq", session: "hq", window: "misc", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := legacyAliasForWindow(tt.session, tt.window, tt.role)
			if ok != tt.wantOK {
				t.Fatalf("legacyAliasForWindow(%q,%q,%q) ok=%v, want %v", tt.session, tt.window, tt.role, ok, tt.wantOK)
			}
			if !tt.wantOK {
				return
			}
			if got != tt.want {
				t.Fatalf("legacyAliasForWindow(%q,%q,%q) = %q, want %q", tt.session, tt.window, tt.role, got, tt.want)
			}
		})
	}
}
