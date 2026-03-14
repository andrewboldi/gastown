package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/knowledge"
)

var knowledgeCmd = &cobra.Command{
	Use:     "knowledge",
	Aliases: []string{"kn"},
	GroupID: GroupDiag,
	Short:   "View collective knowledge from prior agent work",
	Long: `View and manage the knowledge base built from completed polecat work.

Knowledge nuggets are extracted automatically when polecats complete tasks.
They capture what was done, what succeeded, what failed, and domain tags
for matching against future work.

The knowledge base enables institutional learning — agents get smarter
over time because every completed task enriches the context available
to future assignments.`,
}

var knowledgeListCmd = &cobra.Command{
	Use:   "list",
	Short: "List knowledge nuggets",
	Long: `List knowledge nuggets, optionally filtered by rig.

Examples:
  gt knowledge list                 # List all nuggets
  gt knowledge list --rig gastown   # List nuggets from gastown rig
  gt knowledge list --limit 20      # Show more results`,
	RunE: runKnowledgeList,
}

var knowledgeStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show knowledge base statistics",
	RunE:  runKnowledgeStats,
}

var (
	knowledgeRig   string
	knowledgeLimit int
)

func init() {
	knowledgeListCmd.Flags().StringVar(&knowledgeRig, "rig", "", "Filter by rig name")
	knowledgeListCmd.Flags().IntVar(&knowledgeLimit, "limit", 10, "Maximum number of results")

	knowledgeCmd.AddCommand(knowledgeListCmd)
	knowledgeCmd.AddCommand(knowledgeStatsCmd)
	rootCmd.AddCommand(knowledgeCmd)
}

func runKnowledgeList(cmd *cobra.Command, args []string) error {
	_, townRoot, err := resolvePrimeWorkspace()
	if err != nil {
		return err
	}
	if townRoot == "" {
		return fmt.Errorf("not in a Gas Town workspace")
	}

	// Find beads directory from workspace
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	store := knowledge.NewStore(cwd)

	var nuggets []*knowledge.Nugget
	if knowledgeRig != "" {
		nuggets, err = store.QueryByRig(knowledgeRig, knowledgeLimit)
	} else {
		nuggets, err = store.QueryAll(knowledgeLimit)
	}
	if err != nil {
		return fmt.Errorf("querying knowledge base: %w", err)
	}

	if len(nuggets) == 0 {
		fmt.Println("No knowledge nuggets found.")
		fmt.Println("Nuggets are created automatically when polecats complete work.")
		return nil
	}

	for _, n := range nuggets {
		icon := "✓"
		if n.Outcome == "ESCALATED" {
			icon = "✗"
		} else if n.Outcome == "DEFERRED" {
			icon = "⏸"
		}

		domains := ""
		if len(n.Domain) > 0 {
			domains = fmt.Sprintf(" [%s]", strings.Join(n.Domain, ", "))
		}

		fmt.Printf("%s %s  %s/%s  %s%s\n",
			icon, n.ID, n.Rig, n.Agent, n.IssueTitle, domains)

		if n.Insight != "" {
			fmt.Printf("  %s\n", n.Insight)
		}

		if len(n.FilesTouched) > 0 {
			files := n.FilesTouched
			if len(files) > 5 {
				files = append(files[:5], fmt.Sprintf("... +%d more", len(n.FilesTouched)-5))
			}
			fmt.Printf("  Files: %s\n", strings.Join(files, ", "))
		}

		if len(n.Gotchas) > 0 {
			for _, g := range n.Gotchas {
				fmt.Printf("  ⚠ %s\n", g)
			}
		}

		fmt.Println()
	}

	return nil
}

func runKnowledgeStats(cmd *cobra.Command, args []string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	store := knowledge.NewStore(cwd)
	stats, err := store.GetStats()
	if err != nil {
		return fmt.Errorf("computing knowledge stats: %w", err)
	}

	fmt.Printf("Knowledge Base Statistics\n")
	fmt.Printf("========================\n\n")
	fmt.Printf("Total nuggets: %d\n\n", stats.Total)

	if stats.Total == 0 {
		fmt.Println("No nuggets yet. They are created automatically when polecats complete work.")
		return nil
	}

	fmt.Printf("By rig:\n")
	for rig, count := range stats.ByRig {
		fmt.Printf("  %-20s %d\n", rig, count)
	}

	fmt.Printf("\nBy outcome:\n")
	for outcome, count := range stats.ByOutcome {
		fmt.Printf("  %-20s %d\n", outcome, count)
	}

	if !stats.OldestAt.IsZero() {
		fmt.Printf("\nDate range: %s to %s\n",
			stats.OldestAt.Format("2006-01-02"),
			stats.NewestAt.Format("2006-01-02"))
	}

	return nil
}
