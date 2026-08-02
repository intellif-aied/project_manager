package reportmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

const maxHistoricalHints = 5

type HintRequest struct {
	UserID     string
	ReportDate string
	Facts      []FactInput
}

type HistoricalProjectHint struct {
	ProjectRef     string              `json:"project_ref"`
	CanonicalName  string              `json:"canonical_name"`
	Aliases        []string            `json:"aliases,omitempty"`
	MatchedFactRef []string            `json:"matched_fact_refs"`
	RecentContext  []HintRecentContext `json:"recent_context,omitempty"`
	Confidence     float64             `json:"confidence"`
}

type HintRecentContext struct {
	Date        string   `json:"date"`
	Overview    string   `json:"overview"`
	ChildTopics []string `json:"child_topics,omitempty"`
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
						Confidence: resolution.Confidence,
					}
					break
				}
			}
			if item == nil {
				continue
			}
			byProject[resolution.ProjectRef] = item
		}
		item.MatchedFactRef = appendUnique(item.MatchedFactRef, fact.FactRef)
		if resolution.Confidence > item.Confidence {
			item.Confidence = resolution.Confidence
		}
	}
	result := make([]HistoricalProjectHint, 0, len(byProject))
	for _, item := range byProject {
		aliases, recent, err := loadHintDetails(ctx, tx, request.UserID, item.ProjectRef, request.ReportDate)
		if err != nil {
			return nil, err
		}
		item.Aliases, item.RecentContext = aliases, recent
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

func loadHintDetails(ctx context.Context, tx *sql.Tx, userID, projectID, reportDate string) ([]string, []HintRecentContext, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT alias FROM report_project_aliases a
		JOIN report_projects p ON p.id = a.project_id
		WHERE a.project_id = $1 AND p.user_id = $2
		ORDER BY a.source_weight DESC, a.source_report_date DESC, a.alias
		LIMIT $3`, projectID, userID, maxAliasesPerProject)
	if err != nil {
		return nil, nil, err
	}
	aliases := make([]string, 0, maxAliasesPerProject)
	for rows.Next() {
		var alias string
		if err := rows.Scan(&alias); err != nil {
			rows.Close()
			return nil, nil, err
		}
		aliases = appendUnique(aliases, alias)
	}
	if err := rows.Close(); err != nil {
		return nil, nil, err
	}
	rows, err = tx.QueryContext(ctx, `
		SELECT o.report_date::text, o.observed_title, o.child_topics_json
		FROM report_project_occurrences o
		JOIN report_projects p ON p.id = o.project_id
		WHERE o.project_id = $1 AND p.user_id = $2 AND o.report_date < $3::date
		ORDER BY o.report_date DESC, o.id DESC
		LIMIT $4`, projectID, userID, reportDate, maxHistoricalExcerpt)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	recent := make([]HintRecentContext, 0, maxHistoricalExcerpt)
	for rows.Next() {
		var item HintRecentContext
		var children []byte
		if err := rows.Scan(&item.Date, &item.Overview, &children); err != nil {
			return nil, nil, err
		}
		_ = json.Unmarshal(children, &item.ChildTopics)
		item.Overview = limitRunes(item.Overview, maxHistoryOverviewRune)
		recent = append(recent, item)
	}
	return aliases, recent, rows.Err()
}
