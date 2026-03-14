package knowledge

import (
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

func TestExtractFromCompletion_Completed(t *testing.T) {
	t.Parallel()

	workBead := &beads.Issue{
		ID:     "gt-auth-fix",
		Title:  "Fix auth race condition in token handler",
		Labels: []string{"bugfix", "auth"},
	}

	agentFields := &beads.AgentFields{
		HookBead:       "gt-auth-fix",
		ExitType:       "COMPLETED",
		MRID:           "gt-mr-123",
		Branch:         "fix/gt-auth-fix",
		CompletionTime: "2026-03-14T10:00:00Z",
	}

	nugget := ExtractFromCompletion(workBead, agentFields, "Toast", "gastown", "COMPLETED")

	if nugget == nil {
		t.Fatal("expected nugget, got nil")
	}

	if nugget.ID != "kn-gt-auth-fix-Toast" {
		t.Errorf("ID = %q, want %q", nugget.ID, "kn-gt-auth-fix-Toast")
	}
	if nugget.IssueID != "gt-auth-fix" {
		t.Errorf("IssueID = %q, want %q", nugget.IssueID, "gt-auth-fix")
	}
	if nugget.Rig != "gastown" {
		t.Errorf("Rig = %q, want %q", nugget.Rig, "gastown")
	}
	if nugget.Outcome != "COMPLETED" {
		t.Errorf("Outcome = %q, want %q", nugget.Outcome, "COMPLETED")
	}
	if nugget.Branch != "fix/gt-auth-fix" {
		t.Errorf("Branch = %q, want %q", nugget.Branch, "fix/gt-auth-fix")
	}
	if nugget.MRID != "gt-mr-123" {
		t.Errorf("MRID = %q, want %q", nugget.MRID, "gt-mr-123")
	}

	// Should infer domains from title
	domainSet := make(map[string]bool)
	for _, d := range nugget.Domain {
		domainSet[d] = true
	}
	if !domainSet["auth"] {
		t.Error("missing domain 'auth'")
	}
	if !domainSet["concurrency"] {
		t.Error("missing domain 'concurrency'")
	}

	// Completed tasks should have higher relevance than baseline
	if nugget.RelevanceScore < 0.5 {
		t.Errorf("RelevanceScore = %f, want >= 0.5", nugget.RelevanceScore)
	}
}

func TestExtractFromCompletion_Escalated(t *testing.T) {
	t.Parallel()

	workBead := &beads.Issue{
		ID:    "gt-oauth-impl",
		Title: "Implement OAuth2 flow",
	}

	agentFields := &beads.AgentFields{
		HookBead:       "gt-oauth-impl",
		ExitType:       "ESCALATED",
		MRFailed:       true,
		CompletionTime: "2026-03-14T10:00:00Z",
	}

	nugget := ExtractFromCompletion(workBead, agentFields, "Amber", "gastown", "ESCALATED")

	if nugget == nil {
		t.Fatal("expected nugget, got nil")
	}

	if nugget.Outcome != "ESCALATED" {
		t.Errorf("Outcome = %q, want %q", nugget.Outcome, "ESCALATED")
	}

	// Escalated should have gotchas
	if len(nugget.Gotchas) == 0 {
		t.Error("escalated nugget should have gotchas")
	}

	// Should mention MR failure in gotchas
	found := false
	for _, g := range nugget.Gotchas {
		if contains(g, "MR creation failed") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected gotcha about MR creation failure")
	}

	// Escalated should have high relevance (failures are instructive)
	if nugget.RelevanceScore < 0.7 {
		t.Errorf("RelevanceScore = %f, want >= 0.7 for escalated", nugget.RelevanceScore)
	}
}

func TestExtractFromCompletion_PhaseComplete(t *testing.T) {
	t.Parallel()

	nugget := ExtractFromCompletion(nil, nil, "Toast", "gastown", "PHASE_COMPLETE")
	if nugget != nil {
		t.Error("PHASE_COMPLETE should not produce a nugget")
	}
}

func TestExtractFromCompletion_NoIssueID(t *testing.T) {
	t.Parallel()

	nugget := ExtractFromCompletion(nil, &beads.AgentFields{}, "Toast", "gastown", "COMPLETED")
	if nugget != nil {
		t.Error("missing issue ID should not produce a nugget")
	}
}

func TestExtractFromCompletion_NilWorkBead(t *testing.T) {
	t.Parallel()

	agentFields := &beads.AgentFields{
		HookBead: "gt-closed-issue",
		ExitType: "COMPLETED",
	}

	nugget := ExtractFromCompletion(nil, agentFields, "Toast", "gastown", "COMPLETED")

	if nugget == nil {
		t.Fatal("should produce nugget from agent fields alone")
	}
	if nugget.IssueID != "gt-closed-issue" {
		t.Errorf("IssueID = %q, want %q", nugget.IssueID, "gt-closed-issue")
	}
}

func TestEnrichWithFiles(t *testing.T) {
	t.Parallel()

	nugget := &Nugget{}
	EnrichWithFiles(nugget, []string{"internal/auth/token.go", "internal/auth/cache.go"})

	if len(nugget.FilesTouched) != 2 {
		t.Errorf("expected 2 files, got %d", len(nugget.FilesTouched))
	}
}

func TestEnrichWithFiles_Nil(t *testing.T) {
	t.Parallel()
	EnrichWithFiles(nil, []string{"a.go"}) // Should not panic
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
