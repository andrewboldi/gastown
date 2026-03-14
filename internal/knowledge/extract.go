package knowledge

import (
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
)

// ExtractFromCompletion builds a knowledge nugget from polecat completion data.
// This is called by the witness after processing a POLECAT_DONE event.
//
// Parameters:
//   - workBead: the issue the polecat was working on (may be nil if already closed)
//   - agentFields: the polecat's agent bead fields (completion metadata)
//   - polecatName: the polecat's display name (e.g., "Toast")
//   - rigName: the rig where work was done
//   - exitType: COMPLETED, ESCALATED, or DEFERRED
func ExtractFromCompletion(
	workBead *beads.Issue,
	agentFields *beads.AgentFields,
	polecatName string,
	rigName string,
	exitType string,
) *Nugget {
	if exitType == "" || exitType == "PHASE_COMPLETE" {
		return nil // Phase completions are intermediate, not knowledge-worthy
	}

	issueID := ""
	issueTitle := ""
	var issueLabels []string

	if workBead != nil {
		issueID = workBead.ID
		issueTitle = workBead.Title
		issueLabels = workBead.Labels
	} else if agentFields != nil {
		issueID = agentFields.HookBead
	}

	if issueID == "" {
		return nil // Can't create a nugget without an issue reference
	}

	branch := ""
	mrID := ""
	if agentFields != nil {
		branch = agentFields.Branch
		mrID = agentFields.MRID
	}

	// Infer domain from issue title and labels
	domain := InferDomain(issueTitle, issueLabels)

	// Build insight text from available data
	insight := buildInsight(issueTitle, exitType, branch, polecatName)

	// Build gotchas for escalated issues
	var gotchas []string
	if exitType == "ESCALATED" {
		gotchas = append(gotchas, fmt.Sprintf("Previous attempt by %s was escalated — may need different approach or human intervention", polecatName))
		if agentFields != nil && agentFields.MRFailed {
			gotchas = append(gotchas, "MR creation failed during the previous attempt")
		}
	} else if exitType == "DEFERRED" {
		gotchas = append(gotchas, fmt.Sprintf("Work was deferred by %s — may be blocked on external dependency", polecatName))
	}

	// Estimate duration from completion timestamp
	durationMinutes := 0
	if agentFields != nil && agentFields.CompletionTime != "" {
		if ct, err := time.Parse(time.RFC3339, agentFields.CompletionTime); err == nil {
			// Rough estimate: assume work started ~30 min before completion for
			// completed tasks, less for escalated (they fail faster)
			switch exitType {
			case "COMPLETED":
				durationMinutes = 30
			case "ESCALATED":
				durationMinutes = 15
			case "DEFERRED":
				durationMinutes = 10
			}
			_ = ct // Used for the estimate above; exact start time isn't available
		}
	}

	id := GenerateID(issueID, polecatName)

	// Base relevance score — successful completions are slightly more useful
	relevance := 0.5
	if exitType == "COMPLETED" {
		relevance = 0.7
	} else if exitType == "ESCALATED" {
		relevance = 0.8 // Failures are highly informative
	}

	return &Nugget{
		ID:              id,
		IssueID:         issueID,
		Rig:             rigName,
		Agent:           polecatName,
		Outcome:         exitType,
		Domain:          domain,
		Insight:         insight,
		FilesTouched:    nil, // Populated later by caller if git diff available
		Gotchas:         gotchas,
		IssueTitle:      issueTitle,
		IssueLabels:     issueLabels,
		Branch:          branch,
		MRID:            mrID,
		DurationMinutes: durationMinutes,
		CreatedAt:       time.Now(),
		RelevanceScore:  relevance,
	}
}

// buildInsight constructs a human-readable insight from structured data.
func buildInsight(issueTitle, exitType, branch, agent string) string {
	var parts []string

	verb := "Completed"
	switch exitType {
	case "ESCALATED":
		verb = "Escalated"
	case "DEFERRED":
		verb = "Deferred"
	}

	if issueTitle != "" {
		parts = append(parts, fmt.Sprintf("%s: '%s'", verb, issueTitle))
	} else {
		parts = append(parts, fmt.Sprintf("%s work", verb))
	}

	parts = append(parts, fmt.Sprintf("by %s", agent))

	if branch != "" {
		parts = append(parts, fmt.Sprintf("on branch %s", branch))
	}

	return strings.Join(parts, " ")
}

// EnrichWithFiles adds file paths to a nugget. Called when git diff data
// is available (e.g., from the polecat's working branch).
func EnrichWithFiles(n *Nugget, files []string) {
	if n == nil || len(files) == 0 {
		return
	}
	n.FilesTouched = files
}
