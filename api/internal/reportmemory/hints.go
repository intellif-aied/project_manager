package reportmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

const (
	maxHistoricalHints = 3
	maxHintFactRefs    = 64
)

type HintRequest struct {
	UserID     string
	RunID      string
	ReportDate string
	Facts      []FactInput
}

type HistoricalProjectHint struct {
	ProjectRef     string   `json:"project_ref"`
	CanonicalName  string   `json:"canonical_name"`
	Aliases        []string `json:"aliases,omitempty"`
	WorkstreamCues []string `json:"workstream_cues,omitempty"`
	// MatchedFactRef is the compatibility union exposed to older callers.
	MatchedFactRef   []string `json:"matched_fact_refs"`
	SemanticFactRef  []string `json:"semantic_fact_refs,omitempty"`
	WorkspaceFactRef []string `json:"workspace_fact_refs,omitempty"`
	Confidence       float64  `json:"confidence"`
	CandidateOnly    bool     `json:"candidate_only,omitempty"`
	MatchBasis       string   `json:"match_basis,omitempty"`
}

func LoadHistoricalHints(ctx context.Context, tx *sql.Tx, request HintRequest) ([]HistoricalProjectHint, error) {
	if tx == nil || strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.ReportDate) == "" {
		return nil, nil
	}
	var snapshotPayload []byte
	err := tx.QueryRowContext(ctx, `
		SELECT project_memory_json
		FROM report_project_memory_snapshots
		WHERE user_id = $1 AND report_date < $2::date
		ORDER BY report_date DESC, created_at DESC LIMIT 1`, request.UserID, request.ReportDate).
		Scan(&snapshotPayload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	acceptedRefs := snapshotProjectRefs(snapshotPayload)
	if len(acceptedRefs) == 0 {
		return nil, nil
	}
	projects, err := loadProjects(ctx, tx, request.UserID, request.ReportDate)
	if err != nil {
		return nil, err
	}
	filtered := projects[:0]
	for _, project := range projects {
		if acceptedRefs[project.ID] {
			filtered = append(filtered, project)
		}
	}
	projects = filtered
	byProject := map[string]*HistoricalProjectHint{}
	workspaceMatches, err := loadWorkspaceHintMatches(ctx, tx, request)
	if err != nil {
		return nil, err
	}
	for projectRef, match := range workspaceMatches {
		if !acceptedRefs[projectRef] {
			continue
		}
		byProject[projectRef] = &HistoricalProjectHint{
			ProjectRef: projectRef, CanonicalName: match.CanonicalName,
			MatchedFactRef: match.FactRefs, WorkspaceFactRef: match.FactRefs, Confidence: match.Confidence,
			CandidateOnly: true, MatchBasis: "workspace",
		}
	}
	for _, fact := range request.Facts {
		resolution := resolveFact(fact, projects, request.ReportDate)
		if resolution.Decision != "matched" || resolution.Confidence < highConfidenceScore || resolution.ProjectRef == "" {
			continue
		}
		item := byProject[resolution.ProjectRef]
		if item == nil {
			for _, candidate := range resolution.CandidateList {
				if candidate.ProjectRef == resolution.ProjectRef {
					item = &HistoricalProjectHint{
						ProjectRef: resolution.ProjectRef, CanonicalName: candidate.CanonicalName,
						Confidence: resolution.Confidence, MatchBasis: "semantic",
					}
					break
				}
			}
			if item == nil {
				continue
			}
			byProject[resolution.ProjectRef] = item
		}
		if item.MatchBasis == "workspace" {
			item.MatchBasis = "workspace_semantic"
		}
		item.CandidateOnly = false
		item.SemanticFactRef = appendUnique(item.SemanticFactRef, fact.FactRef)
		item.MatchedFactRef = appendUnique(item.MatchedFactRef, fact.FactRef)
		if resolution.Confidence > item.Confidence {
			item.Confidence = resolution.Confidence
		}
	}
	result := make([]HistoricalProjectHint, 0, len(byProject))
	for _, item := range byProject {
		aliases, err := loadHintAliases(ctx, tx, request.UserID, item.ProjectRef)
		if err != nil {
			return nil, err
		}
		item.Aliases = aliases
		workstreamCues, err := loadHintWorkstreamCues(ctx, tx, request.UserID, item.ProjectRef)
		if err != nil {
			return nil, err
		}
		item.WorkstreamCues = workstreamCues
		item.MatchedFactRef = limitStrings(item.MatchedFactRef, maxHintFactRefs)
		item.SemanticFactRef = limitStrings(item.SemanticFactRef, maxHintFactRefs)
		item.WorkspaceFactRef = limitStrings(item.WorkspaceFactRef, maxHintFactRefs)
		result = append(result, *item)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Confidence != result[j].Confidence {
			return result[i].Confidence > result[j].Confidence
		}
		return result[i].CanonicalName < result[j].CanonicalName
	})
	if len(result) > maxHistoricalHints {
		result = result[:maxHistoricalHints]
	}
	return result, nil
}

type workspaceHintMatch struct {
	CanonicalName string
	FactRefs      []string
	Confidence    float64
}

func loadWorkspaceHintMatches(ctx context.Context, tx *sql.Tx, request HintRequest) (map[string]workspaceHintMatch, error) {
	result := make(map[string]workspaceHintMatch)
	if strings.TrimSpace(request.RunID) == "" {
		return result, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT link.project_id::text, project.canonical_name, source.fact_ref,
		       (link.confidence * (0.7 + link.source_weight * 0.3))::float8
		FROM report_run_fact_sources source
		JOIN report_source_selections selection ON selection.attached_run_id = source.run_id
		JOIN report_source_selection_items item
		  ON item.selection_id = selection.id AND item.session_ref_snapshot = source.session_ref
		JOIN report_workspace_evidence evidence
		  ON evidence.source_session_id = item.session_id
		JOIN report_project_workspace_links link ON link.workspace_id = evidence.workspace_id
		JOIN report_projects project ON project.id = link.project_id
		WHERE source.run_id = $1 AND project.user_id = $2
		  AND project.first_seen_on < $3::date AND project.status <> 'ended'
		ORDER BY link.confidence DESC, link.last_seen_on DESC`, request.RunID, request.UserID, request.ReportDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var projectRef, name, factRef string
		var confidence float64
		if err := rows.Scan(&projectRef, &name, &factRef, &confidence); err != nil {
			return nil, err
		}
		current := result[projectRef]
		current.CanonicalName = name
		if len(current.FactRefs) < maxHintFactRefs {
			current.FactRefs = appendUnique(current.FactRefs, factRef)
		}
		if confidence > current.Confidence {
			current.Confidence = confidence
		}
		result[projectRef] = current
	}
	return result, rows.Err()
}

func snapshotProjectRefs(payload []byte) map[string]bool {
	var snapshot struct {
		Projects []struct {
			ProjectRef string `json:"project_ref"`
		} `json:"projects"`
	}
	if json.Unmarshal(payload, &snapshot) != nil {
		return nil
	}
	result := make(map[string]bool, len(snapshot.Projects))
	for _, project := range snapshot.Projects {
		if ref := strings.TrimSpace(project.ProjectRef); ref != "" {
			result[ref] = true
		}
	}
	return result
}

func loadHintAliases(ctx context.Context, tx *sql.Tx, userID, projectID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT alias FROM report_project_aliases a
		JOIN report_projects p ON p.id = a.project_id
		WHERE a.project_id = $1 AND p.user_id = $2
		ORDER BY a.source_weight DESC, a.source_report_date DESC, a.alias
		LIMIT $3`, projectID, userID, maxAliasesPerProject)
	if err != nil {
		return nil, err
	}
	aliases := make([]string, 0, maxAliasesPerProject)
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			rows.Close()
			return nil, err
		}
		if validProjectName(alias) {
			aliases = appendUnique(aliases, alias)
		}
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return aliases, nil
}

func loadHintWorkstreamCues(ctx context.Context, tx *sql.Tx, userID, projectID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT cue.value
		FROM report_project_occurrences occurrence
		JOIN report_projects project ON project.id = occurrence.project_id
		CROSS JOIN LATERAL jsonb_array_elements_text(occurrence.workstream_cues_json) cue(value)
		WHERE occurrence.project_id = $1 AND project.user_id = $2
		GROUP BY cue.value
		ORDER BY max(occurrence.source_weight) DESC, max(occurrence.report_date) DESC, cue.value
		LIMIT $3`, projectID, userID, maxWorkstreamCues)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0, maxWorkstreamCues)
	for rows.Next() {
		var cue string
		if err := rows.Scan(&cue); err != nil {
			return nil, err
		}
		if validProjectName(cue) {
			result = appendUnique(result, cue)
		}
	}
	return result, rows.Err()
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return append([]string(nil), values[:limit]...)
}
