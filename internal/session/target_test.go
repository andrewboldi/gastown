package session

import (
	"testing"
)

func TestTmuxTarget_Components(t *testing.T) {
	tests := []struct {
		name       string
		target     TmuxTarget
		wantSess   string
		wantWindow string
		wantStr    string
	}{
		{"rig agent", NewTarget("gt", "witness"), "gt", "witness", "gt:witness"},
		{"hq agent", NewTarget("hq", "mayor"), "hq", "mayor", "hq:mayor"},
		{"crew", NewTarget("gt", "crew-max"), "gt", "crew-max", "gt:crew-max"},
		{"polecat", NewTarget("gt", "Toast"), "gt", "Toast", "gt:Toast"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.target.Session(); got != tt.wantSess {
				t.Errorf("Session() = %q, want %q", got, tt.wantSess)
			}
			if got := tt.target.Window(); got != tt.wantWindow {
				t.Errorf("Window() = %q, want %q", got, tt.wantWindow)
			}
			if got := tt.target.String(); got != tt.wantStr {
				t.Errorf("String() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestTmuxTarget_WithPane(t *testing.T) {
	target := NewTarget("gt", "witness")
	got := target.WithPane(0)
	want := "gt:witness.0"
	if got != want {
		t.Errorf("WithPane(0) = %q, want %q", got, want)
	}
}

func TestTmuxTarget_SessionOnly(t *testing.T) {
	// A TmuxTarget with no colon returns the whole string as session
	target := TmuxTarget("gt")
	if got := target.Session(); got != "gt" {
		t.Errorf("Session() = %q, want %q", got, "gt")
	}
	if got := target.Window(); got != "" {
		t.Errorf("Window() = %q, want empty", got)
	}
}

func TestRigSessionName(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{"gt", "gt"},
		{"bd", "bd"},
		{"hop", "hop"},
	}
	for _, tt := range tests {
		if got := RigSessionName(tt.prefix); got != tt.want {
			t.Errorf("RigSessionName(%q) = %q, want %q", tt.prefix, got, tt.want)
		}
	}
}

func TestHQSessionName(t *testing.T) {
	if got := HQSessionName(); got != "hq" {
		t.Errorf("HQSessionName() = %q, want %q", got, "hq")
	}
}

func TestWindowNames(t *testing.T) {
	if got := MayorWindowName(); got != "mayor" {
		t.Errorf("MayorWindowName() = %q", got)
	}
	if got := DeaconWindowName(); got != "deacon" {
		t.Errorf("DeaconWindowName() = %q", got)
	}
	if got := BootWindowName(); got != "boot" {
		t.Errorf("BootWindowName() = %q", got)
	}
	if got := WitnessWindowName(); got != "witness" {
		t.Errorf("WitnessWindowName() = %q", got)
	}
	if got := RefineryWindowName(); got != "refinery" {
		t.Errorf("RefineryWindowName() = %q", got)
	}
	if got := CrewWindowName("max"); got != "crew-max" {
		t.Errorf("CrewWindowName(max) = %q", got)
	}
	if got := PolecatWindowName("Toast"); got != "Toast" {
		t.Errorf("PolecatWindowName(Toast) = %q", got)
	}
}

func TestTargetConstructors(t *testing.T) {
	tests := []struct {
		name string
		got  TmuxTarget
		want string
	}{
		{"mayor", MayorTarget(), "hq:mayor"},
		{"deacon", DeaconTarget(), "hq:deacon"},
		{"boot", BootTarget(), "hq:boot"},
		{"witness-gt", WitnessTarget("gt"), "gt:witness"},
		{"witness-bd", WitnessTarget("bd"), "bd:witness"},
		{"refinery", RefineryTarget("gt"), "gt:refinery"},
		{"crew", CrewTarget("gt", "max"), "gt:crew-max"},
		{"polecat", PolecatTarget("gt", "Toast"), "gt:Toast"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.got.String(); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentIdentity_Target(t *testing.T) {
	tests := []struct {
		name     string
		identity AgentIdentity
		want     string
	}{
		{"mayor", AgentIdentity{Role: RoleMayor}, "hq:mayor"},
		{"deacon", AgentIdentity{Role: RoleDeacon}, "hq:deacon"},
		{"boot", AgentIdentity{Role: RoleDeacon, Name: "boot"}, "hq:boot"},
		{"witness", AgentIdentity{Role: RoleWitness, Prefix: "gt"}, "gt:witness"},
		{"refinery", AgentIdentity{Role: RoleRefinery, Prefix: "bd"}, "bd:refinery"},
		{"crew", AgentIdentity{Role: RoleCrew, Prefix: "gt", Name: "max"}, "gt:crew-max"},
		{"polecat", AgentIdentity{Role: RolePolecat, Prefix: "gt", Name: "Toast"}, "gt:Toast"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.identity.Target()
			if got.String() != tt.want {
				t.Errorf("Target() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAgentIdentity_RigSession(t *testing.T) {
	tests := []struct {
		name     string
		identity AgentIdentity
		want     string
	}{
		{"mayor", AgentIdentity{Role: RoleMayor}, "hq"},
		{"deacon", AgentIdentity{Role: RoleDeacon}, "hq"},
		{"witness-gt", AgentIdentity{Role: RoleWitness, Prefix: "gt"}, "gt"},
		{"crew-bd", AgentIdentity{Role: RoleCrew, Prefix: "bd", Name: "max"}, "bd"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.identity.RigSession(); got != tt.want {
				t.Errorf("RigSession() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTarget(t *testing.T) {
	// Set up a registry for testing
	reg := NewPrefixRegistry()
	reg.Register("gt", "gastown")
	reg.Register("bd", "beads")
	oldReg := defaultRegistry
	SetDefaultRegistry(reg)
	defer SetDefaultRegistry(oldReg)

	tests := []struct {
		name     string
		target   TmuxTarget
		wantRole Role
		wantRig  string
		wantName string
		wantErr  bool
	}{
		{"mayor", TmuxTarget("hq:mayor"), RoleMayor, "", "", false},
		{"deacon", TmuxTarget("hq:deacon"), RoleDeacon, "", "", false},
		{"boot", TmuxTarget("hq:boot"), RoleDeacon, "", "boot", false},
		{"overseer", TmuxTarget("hq:overseer"), RoleOverseer, "", "", false},
		{"witness", TmuxTarget("gt:witness"), RoleWitness, "gastown", "", false},
		{"refinery", TmuxTarget("bd:refinery"), RoleRefinery, "beads", "", false},
		{"crew", TmuxTarget("gt:crew-max"), RoleCrew, "gastown", "max", false},
		{"polecat", TmuxTarget("gt:Toast"), RolePolecat, "gastown", "Toast", false},
		{"invalid hq window", TmuxTarget("hq:unknown"), "", "", "", true},
		{"no window", TmuxTarget("gt"), "", "", "", true},
		{"empty", TmuxTarget(""), "", "", "", true},
		{"empty crew name", TmuxTarget("gt:crew-"), "", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := ParseTarget(tt.target)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseTarget(%q) error = %v, wantErr %v", tt.target, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if id.Role != tt.wantRole {
				t.Errorf("Role = %q, want %q", id.Role, tt.wantRole)
			}
			if id.Rig != tt.wantRig {
				t.Errorf("Rig = %q, want %q", id.Rig, tt.wantRig)
			}
			if id.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", id.Name, tt.wantName)
			}
		})
	}
}

func TestParseTarget_RoundTrip(t *testing.T) {
	// Verify that Target() → ParseTarget() produces the same identity.
	reg := NewPrefixRegistry()
	reg.Register("gt", "gastown")
	oldReg := defaultRegistry
	SetDefaultRegistry(reg)
	defer SetDefaultRegistry(oldReg)

	identities := []AgentIdentity{
		{Role: RoleMayor},
		{Role: RoleDeacon},
		{Role: RoleDeacon, Name: "boot"},
		{Role: RoleWitness, Rig: "gastown", Prefix: "gt"},
		{Role: RoleRefinery, Rig: "gastown", Prefix: "gt"},
		{Role: RoleCrew, Rig: "gastown", Name: "max", Prefix: "gt"},
		{Role: RolePolecat, Rig: "gastown", Name: "Toast", Prefix: "gt"},
	}
	for _, orig := range identities {
		target := orig.Target()
		parsed, err := ParseTarget(target)
		if err != nil {
			t.Errorf("ParseTarget(%q) for %+v: %v", target, orig, err)
			continue
		}
		if parsed.Role != orig.Role {
			t.Errorf("round-trip Role: got %q, want %q (target %q)", parsed.Role, orig.Role, target)
		}
		if parsed.Name != orig.Name {
			t.Errorf("round-trip Name: got %q, want %q (target %q)", parsed.Name, orig.Name, target)
		}
	}
}
