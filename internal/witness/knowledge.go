package witness

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/knowledge"
	"github.com/steveyegge/gastown/internal/util"
)

// extractAndStoreKnowledge creates a knowledge nugget from a polecat completion
// and stores it in the knowledge base. This runs as a best-effort side effect
// of completion processing — errors are logged but never block the completion flow.
//
// Called from HandlePolecatDone and HandlePolecatDoneFromBead after the primary
// completion handling (MR submission, idle transition) succeeds.
func extractAndStoreKnowledge(bd *BdCli, workDir, rigName string, payload *PolecatDonePayload) {
	// Fetch the work bead for context (title, labels, description)
	var workBead *beads.Issue
	if payload.IssueID != "" {
		output, err := bd.Exec(workDir, "show", payload.IssueID, "--json")
		if err == nil && output != "" {
			workBead = parseBdShowJSON(output)
		}
	}

	// Build agent fields from payload for the extraction
	agentFields := &beads.AgentFields{
		HookBead: payload.IssueID,
		ExitType: payload.Exit,
		MRID:     payload.MRID,
		Branch:   payload.Branch,
		MRFailed: payload.MRFailed,
	}

	nugget := knowledge.ExtractFromCompletion(workBead, agentFields, payload.PolecatName, rigName, payload.Exit)
	if nugget == nil {
		return
	}

	// Try to enrich with changed files from the branch
	if payload.Branch != "" {
		files := getChangedFiles(workDir, payload.Branch)
		knowledge.EnrichWithFiles(nugget, files)
	}

	// Store the nugget
	townRoot := workDirToTownRoot(workDir)
	storeDir := resolveKnowledgeStoreDir(townRoot, rigName)

	store := knowledge.NewStore(storeDir)
	if err := store.Save(nugget); err != nil {
		fmt.Fprintf(os.Stderr, "knowledge: failed to store nugget for %s/%s: %v\n",
			rigName, payload.PolecatName, err)
		return
	}

	fmt.Fprintf(os.Stderr, "knowledge: stored nugget %s (outcome=%s, domains=%s)\n",
		nugget.ID, nugget.Outcome, strings.Join(nugget.Domain, ","))
}

// getChangedFiles returns the list of files changed on a branch relative to main.
func getChangedFiles(workDir, branch string) []string {
	output, err := util.ExecWithOutput(workDir, "git", "diff", "--name-only", "main..."+branch)
	if err != nil || output == "" {
		// Try origin/main as fallback
		output, err = util.ExecWithOutput(workDir, "git", "diff", "--name-only", "origin/main..."+branch)
		if err != nil || output == "" {
			return nil
		}
	}

	var files []string
	for _, f := range strings.Split(strings.TrimSpace(output), "\n") {
		f = strings.TrimSpace(f)
		if f != "" {
			files = append(files, f)
		}
	}

	// Cap at 20 files to avoid bloating the nugget
	if len(files) > 20 {
		files = files[:20]
	}

	return files
}

// resolveKnowledgeStoreDir determines the beads directory for storing knowledge.
// Uses the rig's mayor clone (authoritative beads location).
func resolveKnowledgeStoreDir(townRoot, rigName string) string {
	if townRoot == "" {
		return ""
	}
	// Try rig's mayor clone first (authoritative beads location)
	rigMayor := fmt.Sprintf("%s/%s/mayor/rig", townRoot, rigName)
	if _, err := os.Stat(rigMayor); err == nil {
		return rigMayor
	}
	// Fall back to rig root
	rigRoot := fmt.Sprintf("%s/%s", townRoot, rigName)
	if _, err := os.Stat(rigRoot); err == nil {
		return rigRoot
	}
	return townRoot
}

// parseBdShowJSON parses bd show --json output into a beads.Issue.
// bd show --json returns an array with one element.
func parseBdShowJSON(output string) *beads.Issue {
	var issues []*beads.Issue
	if err := json.Unmarshal([]byte(output), &issues); err != nil {
		// Try as single object
		var issue beads.Issue
		if err := json.Unmarshal([]byte(output), &issue); err != nil {
			return nil
		}
		return &issue
	}
	if len(issues) == 0 {
		return nil
	}
	return issues[0]
}
