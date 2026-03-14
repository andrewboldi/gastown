package knowledge

import (
	"strings"
	"testing"
	"time"
)

func TestInferDomain(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		title          string
		labels         []string
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:         "go concurrency issue",
			title:        "fix goroutine race condition in auth handler",
			labels:       nil,
			wantContains: []string{"go", "concurrency", "auth", "bugfix"},
		},
		{
			name:         "python test",
			title:        "add pytest fixtures for API endpoints",
			labels:       nil,
			wantContains: []string{"python", "testing", "api"},
		},
		{
			name:         "typescript react feature",
			title:        "implement React component for dashboard",
			labels:       nil,
			wantContains: []string{"typescript", "feature"},
		},
		{
			name:           "labels pass through",
			title:          "update something",
			labels:         []string{"frontend", "gt:keep"},
			wantContains:   []string{"frontend"},
			wantNotContain: []string{"gt:keep"},
		},
		{
			name:         "empty input",
			title:        "",
			labels:       nil,
			wantContains: nil,
		},
		{
			name:         "git refactor",
			title:        "refactor git merge logic for worktree handling",
			labels:       nil,
			wantContains: []string{"git", "refactor"},
		},
		{
			name:         "performance optimization",
			title:        "optimize cache latency for database queries",
			labels:       nil,
			wantContains: []string{"performance", "sql"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			domains := InferDomain(tt.title, tt.labels)
			domainSet := make(map[string]bool)
			for _, d := range domains {
				domainSet[d] = true
			}

			for _, want := range tt.wantContains {
				if !domainSet[want] {
					t.Errorf("InferDomain(%q, %v) missing domain %q, got %v",
						tt.title, tt.labels, want, domains)
				}
			}

			for _, notWant := range tt.wantNotContain {
				if domainSet[notWant] {
					t.Errorf("InferDomain(%q, %v) should not contain %q, got %v",
						tt.title, tt.labels, notWant, domains)
				}
			}
		})
	}
}

func TestMatchScore(t *testing.T) {
	t.Parallel()

	now := time.Now()

	nugget := &Nugget{
		IssueTitle: "fix auth race condition in token handler",
		Domain:     []string{"go", "concurrency", "auth"},
		Rig:        "gastown",
		Outcome:    "COMPLETED",
		CreatedAt:  now.Add(-2 * time.Hour),
	}

	tests := []struct {
		name        string
		issueTitle  string
		issueLabels []string
		rig         string
		wantMin     float64
		wantMax     float64
	}{
		{
			name:       "exact domain match same rig",
			issueTitle: "fix another auth concurrency bug in Go handler",
			rig:        "gastown",
			wantMin:    0.5,
			wantMax:    1.0,
		},
		{
			name:       "partial domain match different rig",
			issueTitle: "update auth flow",
			rig:        "otherrig",
			wantMin:    0.1,
			wantMax:    0.7,
		},
		{
			name:       "no domain overlap",
			issueTitle: "add CSS styling to footer",
			rig:        "otherrig",
			wantMin:    0.0,
			wantMax:    0.4,
		},
		{
			name:       "same rig different domain",
			issueTitle: "add new REST endpoint",
			rig:        "gastown",
			wantMin:    0.1,
			wantMax:    0.6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			score := MatchScore(nugget, tt.issueTitle, tt.issueLabels, tt.rig)
			if score < tt.wantMin || score > tt.wantMax {
				t.Errorf("MatchScore() = %f, want between %f and %f",
					score, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestMatchScore_RecencyDecay(t *testing.T) {
	t.Parallel()

	now := time.Now()

	recentNugget := &Nugget{
		IssueTitle: "fix auth bug",
		Domain:     []string{"auth"},
		Rig:        "gastown",
		Outcome:    "COMPLETED",
		CreatedAt:  now.Add(-1 * time.Hour),
	}

	oldNugget := &Nugget{
		IssueTitle: "fix auth bug",
		Domain:     []string{"auth"},
		Rig:        "gastown",
		Outcome:    "COMPLETED",
		CreatedAt:  now.Add(-120 * 24 * time.Hour), // 120 days old
	}

	recentScore := MatchScore(recentNugget, "fix auth issue", nil, "gastown")
	oldScore := MatchScore(oldNugget, "fix auth issue", nil, "gastown")

	if recentScore <= oldScore {
		t.Errorf("recent nugget score (%f) should be higher than old nugget score (%f)",
			recentScore, oldScore)
	}
}

func TestMatchScore_EscalatedBoost(t *testing.T) {
	t.Parallel()

	now := time.Now()

	completed := &Nugget{
		IssueTitle: "fix auth bug",
		Domain:     []string{"auth"},
		Rig:        "gastown",
		Outcome:    "COMPLETED",
		CreatedAt:  now,
	}

	escalated := &Nugget{
		IssueTitle: "fix auth bug",
		Domain:     []string{"auth"},
		Rig:        "gastown",
		Outcome:    "ESCALATED",
		CreatedAt:  now,
	}

	completedScore := MatchScore(completed, "fix auth issue", nil, "gastown")
	escalatedScore := MatchScore(escalated, "fix auth issue", nil, "gastown")

	if escalatedScore <= completedScore {
		t.Errorf("escalated nugget score (%f) should be higher than completed (%f)",
			escalatedScore, completedScore)
	}
}

func TestFormatForInjection(t *testing.T) {
	t.Parallel()

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()
		output := FormatForInjection(nil)
		if output != "" {
			t.Errorf("FormatForInjection(nil) = %q, want empty", output)
		}
	})

	t.Run("single completed nugget", func(t *testing.T) {
		t.Parallel()
		nuggets := []*Nugget{{
			IssueTitle:   "Fix auth race condition",
			Outcome:      "COMPLETED",
			Insight:      "Mutex needed on tokenCache, not HTTP client",
			FilesTouched: []string{"internal/auth/token.go"},
		}}
		output := FormatForInjection(nuggets)
		if !strings.Contains(output, "Prior Knowledge") {
			t.Error("missing header")
		}
		if !strings.Contains(output, "[+] Fix auth race condition") {
			t.Error("missing completed icon")
		}
		if !strings.Contains(output, "Mutex needed") {
			t.Error("missing insight")
		}
		if !strings.Contains(output, "internal/auth/token.go") {
			t.Error("missing files")
		}
	})

	t.Run("escalated nugget", func(t *testing.T) {
		t.Parallel()
		nuggets := []*Nugget{{
			IssueTitle: "Implement OAuth2 flow",
			Outcome:    "ESCALATED",
			Gotchas:    []string{"Token refresh endpoint returns 403 intermittently"},
		}}
		output := FormatForInjection(nuggets)
		if !strings.Contains(output, "[!]") {
			t.Error("missing escalated icon")
		}
		if !strings.Contains(output, "previous agent couldn't complete") {
			t.Error("missing escalation warning")
		}
		if !strings.Contains(output, "403 intermittently") {
			t.Error("missing gotcha")
		}
	})

	t.Run("caps at 5 nuggets", func(t *testing.T) {
		t.Parallel()
		nuggets := make([]*Nugget, 10)
		for i := range nuggets {
			nuggets[i] = &Nugget{
				IssueTitle: "Issue " + string(rune('A'+i)),
				Outcome:    "COMPLETED",
			}
		}
		output := FormatForInjection(nuggets)
		// Should only have 5 entries
		count := strings.Count(output, "###")
		if count != 5 {
			t.Errorf("expected 5 entries, got %d", count)
		}
	})
}

func TestGenerateID(t *testing.T) {
	t.Parallel()

	id := GenerateID("gt-auth-fix", "Toast")
	if id != "kn-gt-auth-fix-Toast" {
		t.Errorf("GenerateID() = %q, want %q", id, "kn-gt-auth-fix-Toast")
	}
}

func TestTokenize(t *testing.T) {
	t.Parallel()

	tokens := tokenize("Fix the auth race condition in handler")
	expected := map[string]bool{"fix": true, "auth": true, "race": true, "condition": true, "handler": true}

	for _, tok := range tokens {
		if !expected[tok] {
			// Shouldn't include stop words
			if tok == "the" || tok == "in" {
				t.Errorf("tokenize should filter stop word %q", tok)
			}
		}
	}
}

func TestSplitCSV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  int
	}{
		{"a,b,c", 3},
		{`"hello, world",b,c`, 3},
		{`a,"b""c",d`, 3},
		{"", 1},
		{`"quoted"`, 1},
	}

	for _, tt := range tests {
		fields := splitCSV(tt.input)
		if len(fields) != tt.want {
			t.Errorf("splitCSV(%q) got %d fields, want %d: %v", tt.input, len(fields), tt.want, fields)
		}
	}
}
