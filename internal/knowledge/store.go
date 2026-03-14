package knowledge

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"
)

// Store provides CRUD operations for knowledge nuggets via bd sql.
type Store struct {
	WorkDir string // Directory where bd can find the beads database
}

// NewStore creates a Store for the given work directory.
func NewStore(workDir string) *Store {
	return &Store{WorkDir: workDir}
}

// EnsureTable creates the knowledge_nuggets table if it doesn't exist.
func (s *Store) EnsureTable() error {
	return s.bdSQL(NuggetCreateDDL)
}

// Save writes a nugget to the knowledge_nuggets table.
// Uses INSERT ... ON DUPLICATE KEY UPDATE to handle re-runs idempotently.
func (s *Store) Save(n *Nugget) error {
	if err := s.EnsureTable(); err != nil {
		return fmt.Errorf("ensuring knowledge table: %w", err)
	}

	domain := strings.Join(n.Domain, ",")
	files := strings.Join(n.FilesTouched, ",")
	gotchas := strings.Join(n.Gotchas, "\n")
	labels := strings.Join(n.IssueLabels, ",")

	// Escape single quotes for SQL
	escape := func(s string) string {
		return strings.ReplaceAll(s, "'", "''")
	}

	query := fmt.Sprintf(
		"INSERT INTO knowledge_nuggets (id, issue_id, rig, agent, outcome, domain, insight, files_touched, gotchas, issue_title, issue_labels, branch, mr_id, duration_minutes, relevance_score) "+
			"VALUES ('%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', '%s', %d, %f) "+
			"ON DUPLICATE KEY UPDATE insight = VALUES(insight), gotchas = VALUES(gotchas), relevance_score = VALUES(relevance_score)",
		escape(n.ID), escape(n.IssueID), escape(n.Rig), escape(n.Agent),
		escape(n.Outcome), escape(domain), escape(n.Insight),
		escape(files), escape(gotchas), escape(n.IssueTitle),
		escape(labels), escape(n.Branch), escape(n.MRID),
		n.DurationMinutes, n.RelevanceScore,
	)

	return s.bdSQL(query)
}

// QueryByRig returns all nuggets for a given rig, ordered by recency.
func (s *Store) QueryByRig(rig string, limit int) ([]*Nugget, error) {
	if err := s.EnsureTable(); err != nil {
		return nil, err
	}

	query := fmt.Sprintf(
		"SELECT id, issue_id, rig, agent, outcome, domain, insight, files_touched, gotchas, issue_title, issue_labels, branch, mr_id, duration_minutes, created_at, relevance_score "+
			"FROM knowledge_nuggets WHERE rig = '%s' ORDER BY created_at DESC LIMIT %d",
		strings.ReplaceAll(rig, "'", "''"), limit)

	return s.queryNuggets(query)
}

// QueryAll returns all nuggets across all rigs, ordered by recency.
func (s *Store) QueryAll(limit int) ([]*Nugget, error) {
	if err := s.EnsureTable(); err != nil {
		return nil, err
	}

	query := fmt.Sprintf(
		"SELECT id, issue_id, rig, agent, outcome, domain, insight, files_touched, gotchas, issue_title, issue_labels, branch, mr_id, duration_minutes, created_at, relevance_score "+
			"FROM knowledge_nuggets ORDER BY created_at DESC LIMIT %d", limit)

	return s.queryNuggets(query)
}

// QueryRelevant returns nuggets relevant to a given issue, ranked by match score.
func (s *Store) QueryRelevant(issueTitle string, issueLabels []string, rig string, limit int) ([]*Nugget, error) {
	// Fetch a broad set from both the target rig and cross-rig
	candidates, err := s.QueryAll(100)
	if err != nil {
		return nil, err
	}

	// Score and rank
	type scored struct {
		nugget *Nugget
		score  float64
	}
	var ranked []scored
	for _, n := range candidates {
		s := MatchScore(n, issueTitle, issueLabels, rig)
		if s > 0.1 { // Minimum relevance threshold
			ranked = append(ranked, scored{nugget: n, score: s})
		}
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	result := make([]*Nugget, 0, limit)
	for i, r := range ranked {
		if i >= limit {
			break
		}
		result = append(result, r.nugget)
	}

	return result, nil
}

// Count returns the total number of stored nuggets.
func (s *Store) Count() (int, error) {
	if err := s.EnsureTable(); err != nil {
		return 0, err
	}
	output, err := s.bdSQLCSV("SELECT COUNT(*) as cnt FROM knowledge_nuggets")
	if err != nil {
		return 0, err
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return 0, nil
	}
	var cnt int
	fmt.Sscanf(strings.TrimSpace(lines[1]), "%d", &cnt)
	return cnt, nil
}

// Stats returns aggregate statistics about the knowledge base.
type Stats struct {
	Total     int
	ByRig     map[string]int
	ByOutcome map[string]int
	OldestAt  time.Time
	NewestAt  time.Time
}

// GetStats computes aggregate statistics.
func (s *Store) GetStats() (*Stats, error) {
	nuggets, err := s.QueryAll(10000)
	if err != nil {
		return nil, err
	}

	stats := &Stats{
		Total:     len(nuggets),
		ByRig:     make(map[string]int),
		ByOutcome: make(map[string]int),
	}

	for _, n := range nuggets {
		stats.ByRig[n.Rig]++
		stats.ByOutcome[n.Outcome]++
		if stats.OldestAt.IsZero() || n.CreatedAt.Before(stats.OldestAt) {
			stats.OldestAt = n.CreatedAt
		}
		if stats.NewestAt.IsZero() || n.CreatedAt.After(stats.NewestAt) {
			stats.NewestAt = n.CreatedAt
		}
	}

	return stats, nil
}

// queryNuggets executes a SELECT query and parses results into Nuggets.
func (s *Store) queryNuggets(query string) ([]*Nugget, error) {
	output, err := s.bdSQLCSV(query)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 2 {
		return nil, nil // Header only, no data
	}

	var nuggets []*Nugget
	for _, line := range lines[1:] {
		n := parseCSVLine(line)
		if n != nil {
			nuggets = append(nuggets, n)
		}
	}

	return nuggets, nil
}

// parseCSVLine parses a single CSV row into a Nugget.
// Column order matches the SELECT in queryNuggets.
func parseCSVLine(line string) *Nugget {
	fields := splitCSV(line)
	if len(fields) < 16 {
		return nil
	}

	var durationMinutes int
	fmt.Sscanf(fields[13], "%d", &durationMinutes)

	var relevanceScore float64
	fmt.Sscanf(fields[15], "%f", &relevanceScore)

	createdAt, _ := time.Parse("2006-01-02 15:04:05", fields[14])

	return &Nugget{
		ID:              fields[0],
		IssueID:         fields[1],
		Rig:             fields[2],
		Agent:           fields[3],
		Outcome:         fields[4],
		Domain:          splitComma(fields[5]),
		Insight:         fields[6],
		FilesTouched:    splitComma(fields[7]),
		Gotchas:         splitNewline(fields[8]),
		IssueTitle:      fields[9],
		IssueLabels:     splitComma(fields[10]),
		Branch:          fields[11],
		MRID:            fields[12],
		DurationMinutes: durationMinutes,
		CreatedAt:       createdAt,
		RelevanceScore:  relevanceScore,
	}
}

// splitComma splits a comma-separated string, trimming whitespace.
func splitComma(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// splitNewline splits a newline-separated string.
func splitNewline(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// splitCSV splits a CSV line respecting quoted fields.
func splitCSV(line string) []string {
	var fields []string
	var current strings.Builder
	inQuotes := false

	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			if inQuotes && i+1 < len(line) && line[i+1] == '"' {
				current.WriteByte('"')
				i++ // Skip escaped quote
			} else {
				inQuotes = !inQuotes
			}
		case c == ',' && !inQuotes:
			fields = append(fields, current.String())
			current.Reset()
		default:
			current.WriteByte(c)
		}
	}
	fields = append(fields, current.String())

	return fields
}

// bdSQL executes a SQL query via bd sql.
func (s *Store) bdSQL(query string) error {
	cmd := exec.Command("bd", "sql", query) //nolint:gosec // G204: bd is a trusted internal tool
	cmd.Dir = s.WorkDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("bd sql: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

// bdSQLCSV executes a SQL query via bd sql --csv and returns the output.
func (s *Store) bdSQLCSV(query string) (string, error) {
	cmd := exec.Command("bd", "sql", "--csv", query) //nolint:gosec // G204: bd is a trusted internal tool
	cmd.Dir = s.WorkDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("bd sql: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return string(output), nil
}
