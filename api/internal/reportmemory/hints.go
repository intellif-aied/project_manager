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
		WHERE user_id = $1 AND evidence_cutoff_date < $2::date
		  AND resolver_version = $3
		  AND NOT EXISTS (
			SELECT 1 FROM report_project_memory_jobs job
			WHERE job.user_id = $1 AND job.dirty_from_date < $2::date
			  AND job.status <> 'succeeded'
		  )
		ORDER BY evidence_cutoff_date DESC, created_at DESC LIMIT 1`, request.UserID, request.ReportDate, ResolverVersion).
		Scan(&snapshotPayload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	frozenProjects := snapshotProjects(snapshotPayload)
	if len(frozenProjects) == 0 {
		return nil, nil
	}
	projects := make([]storedProject, 0, len(frozenProjects))
	for _, frozen := range frozenProjects {
		projects = append(projects, frozen.Stored)
	}
	byProject := map[string]*HistoricalProjectHint{}
	workspaceMatches, err := loadWorkspaceHintMatches(ctx, tx, request, frozenProjects)
	if err != nil {
		return nil, err
	}
	for projectRef, match := range workspaceMatches {
		frozen := frozenProjects[projectRef]
		byProject[projectRef] = &HistoricalProjectHint{
			ProjectRef: projectRef, CanonicalName: frozen.Stored.CanonicalName,
			Aliases: frozen.Aliases, WorkstreamCues: frozen.WorkstreamCues,
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
					frozen := frozenProjects[resolution.ProjectRef]
					item = &HistoricalProjectHint{
						ProjectRef: resolution.ProjectRef, CanonicalName: candidate.CanonicalName,
						Aliases: frozen.Aliases, WorkstreamCues: frozen.WorkstreamCues,
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
	FactRefs   []string
	Confidence float64
}

func loadWorkspaceHintMatches(ctx context.Context, tx *sql.Tx, request HintRequest, projects map[string]frozenSnapshotProject) (map[string]workspaceHintMatch, error) {
	result := make(map[string]workspaceHintMatch)
	if strings.TrimSpace(request.RunID) == "" {
		return result, nil
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT DISTINCT evidence.workspace_id::text, source.fact_ref
		FROM report_run_fact_sources source
		JOIN report_source_selections selection ON selection.attached_run_id = source.run_id
		JOIN report_source_selection_items item
		  ON item.selection_id = selection.id AND item.session_ref_snapshot = source.session_ref
		JOIN report_workspace_evidence evidence
		  ON evidence.source_session_id = item.session_id
		WHERE source.run_id = $1 AND selection.user_id = $2
		ORDER BY evidence.workspace_id::text, source.fact_ref`, request.RunID, request.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var workspaceRef, factRef string
		if err := rows.Scan(&workspaceRef, &factRef); err != nil {
			return nil, err
		}
		for projectRef, project := range projects {
			if !containsString(project.WorkspaceRefs, workspaceRef) {
				continue
			}
			current := result[projectRef]
			current.FactRefs = appendUnique(current.FactRefs, factRef)
			current.Confidence = 0.72
			result[projectRef] = current
		}
	}
	return result, rows.Err()
}

type frozenSnapshotProject struct {
	Stored         storedProject
	Aliases        []string
	WorkstreamCues []string
	WorkspaceRefs  []string
}

func snapshotProjects(payload []byte) map[string]frozenSnapshotProject {
	var snapshot struct {
		Projects []struct {
			ProjectRef     string   `json:"project_ref"`
			CanonicalName  string   `json:"canonical_name"`
			LastSeenOn     string   `json:"last_seen_on"`
			Aliases        []string `json:"aliases"`
			WorkstreamCues []string `json:"workstream_cues"`
			WorkspaceRefs  []string `json:"workspace_refs"`
		} `json:"projects"`
	}
	if json.Unmarshal(payload, &snapshot) != nil {
		return nil
	}
	result := make(map[string]frozenSnapshotProject, len(snapshot.Projects))
	for _, project := range snapshot.Projects {
		ref := strings.TrimSpace(project.ProjectRef)
		if ref == "" || !validProjectName(project.CanonicalName) {
			continue
		}
		frozen := frozenSnapshotProject{
			Stored:         storedProject{ID: ref, CanonicalName: project.CanonicalName, LastSeenOn: project.LastSeenOn},
			Aliases:        limitStrings(project.Aliases, maxAliasesPerProject),
			WorkstreamCues: limitStrings(project.WorkstreamCues, maxWorkstreamCues),
			WorkspaceRefs:  limitStrings(project.WorkspaceRefs, maxHintFactRefs),
		}
		frozen.Stored.Aliases = append(frozen.Stored.Aliases, storedAlias{
			Text: project.CanonicalName, Normalized: normalizeName(project.CanonicalName), Type: "canonical", SourceType: "snapshot", SourceWeight: 1,
		})
		for _, alias := range frozen.Aliases {
			frozen.Stored.Aliases = append(frozen.Stored.Aliases, storedAlias{Text: alias, Normalized: normalizeName(alias), Type: "alias", SourceType: "snapshot", SourceWeight: 0.9})
		}
		for _, cue := range frozen.WorkstreamCues {
			frozen.Stored.Aliases = append(frozen.Stored.Aliases, storedAlias{Text: cue, Normalized: normalizeName(cue), Type: "workstream_cue", SourceType: "snapshot", SourceWeight: 0.8})
		}
		result[ref] = frozen
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func limitStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}
	return append([]string(nil), values[:limit]...)
}
