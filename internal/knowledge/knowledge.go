// Package knowledge provides collective learning across agent sessions.
//
// When a polecat completes work, the witness extracts a "knowledge nugget" —
// structured metadata about what was done, what succeeded, and what to watch
// out for. These nuggets are stored in a Dolt table and queried during gt prime
// to inject relevant prior experience into new agent sessions.
//
// This enables institutional learning: agents get smarter over time because
// every completed task enriches the knowledge base for future assignments.
package knowledge

import (
	"fmt"
	"strings"
	"time"
)

// Nugget represents a distilled lesson from a completed polecat task.
type Nugget struct {
	ID              string    `json:"id"`
	IssueID         string    `json:"issue_id"`
	Rig             string    `json:"rig"`
	Agent           string    `json:"agent"`
	Outcome         string    `json:"outcome"` // COMPLETED, ESCALATED, DEFERRED
	Domain          []string  `json:"domain"`  // e.g. ["go", "concurrency", "auth"]
	Insight         string    `json:"insight"`
	FilesTouched    []string  `json:"files_touched"`
	Gotchas         []string  `json:"gotchas"`
	IssueTitle      string    `json:"issue_title"`
	IssueLabels     []string  `json:"issue_labels"`
	Branch          string    `json:"branch"`
	MRID            string    `json:"mr_id"`
	DurationMinutes int       `json:"duration_minutes"`
	CreatedAt       time.Time `json:"created_at"`
	RelevanceScore  float64   `json:"relevance_score"`
}

// NuggetCreateDDL is the CREATE TABLE statement for the knowledge_nuggets table.
// Stored alongside wisps (ephemeral, dolt_ignored) to avoid polluting
// git-versioned issue history.
var NuggetCreateDDL = `CREATE TABLE IF NOT EXISTS knowledge_nuggets (
  id varchar(255) NOT NULL,
  issue_id varchar(255) NOT NULL,
  rig varchar(255) NOT NULL,
  agent varchar(255) NOT NULL,
  outcome varchar(32) NOT NULL DEFAULT 'COMPLETED',
  domain text NOT NULL DEFAULT '',
  insight text NOT NULL DEFAULT '',
  files_touched text NOT NULL DEFAULT '',
  gotchas text NOT NULL DEFAULT '',
  issue_title varchar(500) NOT NULL DEFAULT '',
  issue_labels text NOT NULL DEFAULT '',
  branch varchar(255) DEFAULT '',
  mr_id varchar(255) DEFAULT '',
  duration_minutes int DEFAULT 0,
  created_at datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  relevance_score double DEFAULT 1.0,
  PRIMARY KEY (id),
  KEY idx_kn_rig (rig),
  KEY idx_kn_outcome (outcome),
  KEY idx_kn_created_at (created_at)
)`

// GenerateID creates a nugget ID from the issue ID and agent name.
// Format: kn-<issueID>-<agent> (e.g., kn-gt-auth-fix-Toast).
func GenerateID(issueID, agent string) string {
	return fmt.Sprintf("kn-%s-%s", issueID, agent)
}

// FormatForInjection renders nuggets as markdown for prime context injection.
// Returns empty string if no nuggets are provided.
func FormatForInjection(nuggets []*Nugget) string {
	if len(nuggets) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Prior Knowledge\n\n")
	sb.WriteString("Relevant lessons from previous work on similar issues:\n\n")

	for i, n := range nuggets {
		if i >= 5 {
			break // Cap at 5 nuggets to avoid context bloat
		}

		icon := "+"
		if n.Outcome == "ESCALATED" {
			icon = "!"
		} else if n.Outcome == "DEFERRED" {
			icon = "~"
		}

		sb.WriteString(fmt.Sprintf("### [%s] %s\n", icon, n.IssueTitle))

		if n.Insight != "" {
			sb.WriteString(fmt.Sprintf("  %s\n", n.Insight))
		}

		if len(n.FilesTouched) > 0 {
			sb.WriteString(fmt.Sprintf("  Files: %s\n", strings.Join(n.FilesTouched, ", ")))
		}

		if len(n.Gotchas) > 0 {
			sb.WriteString("  Watch out for:\n")
			for _, g := range n.Gotchas {
				sb.WriteString(fmt.Sprintf("  - %s\n", g))
			}
		}

		if n.Outcome == "ESCALATED" {
			sb.WriteString("  *This issue was escalated — a previous agent couldn't complete it.*\n")
		}

		sb.WriteString("\n")
	}

	return sb.String()
}

// InferDomain extracts domain tags from an issue title and labels.
// Uses simple keyword matching — no LLM calls.
func InferDomain(title string, labels []string) []string {
	domains := make(map[string]bool)

	lower := strings.ToLower(title)

	// Language detection from title keywords
	langKeywords := map[string][]string{
		"go":         {"golang", ".go", "goroutine", "chan ", "mutex", "sync."},
		"python":     {"python", ".py", "pip ", "pytest"},
		"typescript": {"typescript", ".ts", ".tsx", "react", "next.js", "nextjs"},
		"javascript": {"javascript", ".js", ".jsx", "node", "npm"},
		"rust":       {"rust", "cargo", ".rs"},
		"sql":        {"sql", "query", "database", "dolt", "mysql", "postgres"},
		"shell":      {"bash", "shell", "script", ".sh", "zsh"},
	}
	for domain, keywords := range langKeywords {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				domains[domain] = true
				break
			}
		}
	}

	// Concept detection from title keywords
	conceptKeywords := map[string][]string{
		"concurrency": {"race", "deadlock", "mutex", "goroutine", "concurrent", "parallel", "channel", "lock"},
		"auth":        {"auth", "login", "token", "oauth", "jwt", "session", "credential"},
		"api":         {"api", "endpoint", "rest", "grpc", "handler", "route"},
		"testing":     {"test", "spec", "assert", "mock", "fixture"},
		"ci":          {"ci", "pipeline", "github action", "workflow", "deploy"},
		"config":      {"config", "env", "setting", "yaml", "toml", "json"},
		"git":         {"git", "branch", "merge", "rebase", "commit", "worktree"},
		"refactor":    {"refactor", "cleanup", "rename", "reorganize", "simplify"},
		"bugfix":      {"fix", "bug", "crash", "error", "panic", "nil pointer", "segfault"},
		"feature":     {"feat", "add", "implement", "introduce", "new"},
		"performance": {"perf", "slow", "optimize", "cache", "latency", "timeout"},
	}
	for domain, keywords := range conceptKeywords {
		for _, kw := range keywords {
			if strings.Contains(lower, kw) {
				domains[domain] = true
				break
			}
		}
	}

	// Direct labels pass through
	for _, label := range labels {
		l := strings.ToLower(label)
		// Skip gastown-internal labels
		if strings.HasPrefix(l, "gt:") {
			continue
		}
		domains[l] = true
	}

	result := make([]string, 0, len(domains))
	for d := range domains {
		result = append(result, d)
	}
	return result
}

// MatchScore computes how relevant a nugget is to a given issue.
// Higher score = more relevant. Range: 0.0 to 1.0.
func MatchScore(nugget *Nugget, issueTitle string, issueLabels []string, rig string) float64 {
	score := 0.0
	maxScore := 0.0

	issueDomains := InferDomain(issueTitle, issueLabels)
	issueDomainSet := make(map[string]bool)
	for _, d := range issueDomains {
		issueDomainSet[d] = true
	}

	// Domain overlap (strongest signal)
	maxScore += 0.4
	if len(issueDomains) > 0 && len(nugget.Domain) > 0 {
		overlap := 0
		for _, d := range nugget.Domain {
			if issueDomainSet[d] {
				overlap++
			}
		}
		if overlap > 0 {
			score += 0.4 * float64(overlap) / float64(len(issueDomains))
		}
	}

	// Same rig boost (likely same codebase)
	maxScore += 0.2
	if nugget.Rig == rig {
		score += 0.2
	}

	// Recency boost (recent nuggets more relevant)
	maxScore += 0.2
	age := time.Since(nugget.CreatedAt)
	switch {
	case age < 24*time.Hour:
		score += 0.2
	case age < 7*24*time.Hour:
		score += 0.15
	case age < 30*24*time.Hour:
		score += 0.1
	case age < 90*24*time.Hour:
		score += 0.05
	}

	// Outcome weighting: escalated issues are more informative (what didn't work)
	maxScore += 0.1
	if nugget.Outcome == "ESCALATED" {
		score += 0.1 // Failures are highly instructive
	} else if nugget.Outcome == "COMPLETED" {
		score += 0.05
	}

	// Title similarity (keyword overlap)
	maxScore += 0.1
	nuggetWords := tokenize(nugget.IssueTitle)
	issueWords := tokenize(issueTitle)
	if len(nuggetWords) > 0 && len(issueWords) > 0 {
		wordOverlap := 0
		issueWordSet := make(map[string]bool)
		for _, w := range issueWords {
			issueWordSet[w] = true
		}
		for _, w := range nuggetWords {
			if issueWordSet[w] {
				wordOverlap++
			}
		}
		if wordOverlap > 0 {
			score += 0.1 * float64(wordOverlap) / float64(len(issueWords))
		}
	}

	if maxScore == 0 {
		return 0
	}
	return score / maxScore
}

// tokenize splits text into lowercase tokens, filtering common stop words.
func tokenize(text string) []string {
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "in": true,
		"to": true, "for": true, "of": true, "and": true, "or": true,
		"on": true, "at": true, "by": true, "with": true, "from": true,
		"it": true, "as": true, "be": true, "was": true, "are": true,
	}

	words := strings.Fields(strings.ToLower(text))
	result := make([]string, 0, len(words))
	for _, w := range words {
		w = strings.Trim(w, ".,;:!?()[]{}\"'`")
		if len(w) > 1 && !stopWords[w] {
			result = append(result, w)
		}
	}
	return result
}
