package reportmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lib/pq"
)

const consolidationInputSchema = "project-memory-consolidation-input/v2"

func buildConsolidationInput(ctx context.Context, database *sql.DB, job queuedJob) (ConsolidationInput, []byte, int, error) {
	input := ConsolidationInput{
		SchemaVersion: consolidationInputSchema, ResolverVersion: ResolverVersion,
		UserRef: job.UserID, ReportRef: job.ReportID, ReportDate: job.ReportDate,
		EvidenceConstraint: "历史内容仅用于项目命名和归并，不是当前日报事实；无法判断时返回 unresolved。",
		AllowedActions:     []string{"link_existing", "create_new", "unresolved", "suggest_rename", "suggest_merge"},
	}
	if database == nil {
		return input, nil, 0, sql.ErrConnDone
	}
	var report historicalReport
	var hasOutcome bool
	err := database.QueryRowContext(ctx, `
		SELECT r.id::text, r.report_date::text,
		       COALESCE(NULLIF(r.submitted_content, ''), r.content),
		       COALESCE(r.generation_mode, ''), r.edited,
		       COALESCE(s.generated_content_sha256, ''),
		       COALESCE(b.brief_payload::text, ''),
		       EXISTS (
			SELECT 1 FROM report_user_outcome_events outcome
			WHERE outcome.report_id = r.id AND outcome.action IN ('saved', 'submitted')
		   )
		FROM daily_reports r
		LEFT JOIN report_generation_snapshots s ON s.run_id = r.managed_agent_run_id
		LEFT JOIN report_run_briefs b ON b.run_id = r.managed_agent_run_id
		WHERE r.id = $1 AND r.user_id = $2 AND r.report_date = $3::date
		  AND r.status IN ('saved', 'submitted')
		  AND NULLIF(BTRIM(COALESCE(NULLIF(r.submitted_content, ''), r.content, '')), '') IS NOT NULL`,
		job.ReportID, job.UserID, job.ReportDate).Scan(
		&report.id, &report.date, &report.content, &report.generationMode, &report.edited,
		&report.generatedContentSHA256, &report.briefPayload, &hasOutcome,
	)
	if err != nil {
		return input, nil, 0, err
	}
	report.sourceType, report.sourceWeight = classifyNightlySource(report, hasOutcome)
	if stats, shadowErr := materializeWorkspaceEvidenceForReport(ctx, database, job.UserID, job.ReportID); shadowErr != nil {
		log.Printf("project memory workspace shadow materialization failed for user=%s report=%s: %v", job.UserID, job.ReportID, shadowErr)
	} else if stats.EvidenceCreated > 0 {
		log.Printf("project memory workspace shadow materialized user=%s report=%s identities=%d evidence=%d", job.UserID, job.ReportID, stats.IdentitiesObserved, stats.EvidenceCreated)
	}
	input.SourceType, input.SourceWeight = report.sourceType, report.sourceWeight
	for _, workstream := range workstreamsFromBrief(report.briefPayload) {
		input.BriefWorkstreams = append(input.BriefWorkstreams, workstream)
		if len(input.BriefWorkstreams) >= maxCurrentThemes {
			break
		}
	}
	workspaceRefs, err := loadWorkspaceRefsByFact(ctx, database, job.UserID, job.ReportID)
	if err != nil {
		return input, nil, 0, err
	}
	if report.generationMode == "managed_agent" && report.sourceType != sourceHumanEdited && len(input.BriefWorkstreams) > 0 {
		for index, workstream := range input.BriefWorkstreams {
			theme := InputTheme{ThemeRef: fmt.Sprintf("theme-%03d", index+1), Title: workstream.Subject, FactRefs: workstream.FactRefs}
			for _, factRef := range workstream.FactRefs {
				for _, workspaceRef := range workspaceRefs[factRef] {
					theme.WorkspaceRefs = appendUnique(theme.WorkspaceRefs, workspaceRef)
				}
			}
			input.CurrentThemes = append(input.CurrentThemes, theme)
		}
	} else {
		themes := consolidationThemes(report, input.BriefWorkstreams)
		if len(themes) > maxCurrentThemes {
			themes = themes[:maxCurrentThemes]
		}
		for index, theme := range themes {
			input.CurrentThemes = append(input.CurrentThemes, InputTheme{
				ThemeRef: fmt.Sprintf("theme-%03d", index+1), Title: limitRunes(theme.Title, titleRuneLimit),
			})
		}
	}
	if len(input.CurrentThemes) == 0 {
		return input, nil, 0, errors.New("final overview has no project memory themes")
	}
	projects, err := loadInputProjects(ctx, database, job.UserID, job.ReportDate, input.CurrentThemes)
	if err != nil {
		return input, nil, 0, err
	}
	input.CandidateProjects = projects
	history, err := loadHistoricalContext(ctx, database, job.UserID, job.ReportDate)
	if err != nil {
		return input, nil, 0, err
	}
	input.RecentOverviews = history.RecentOverviews
	input.HistoricalAnchors = history.ProjectAnchors

	payload, estimate, err := marshalWithinBudget(input)
	return input, payload, estimate, err
}

func consolidationThemes(report historicalReport, workstreams []InputWorkstream) []Theme {
	if report.generationMode == "managed_agent" && report.sourceType != sourceHumanEdited && len(workstreams) > 0 {
		themes := make([]Theme, 0, len(workstreams))
		for _, workstream := range workstreams {
			if subject := sanitizeTitle(workstream.Subject); subject != "" {
				themes = append(themes, Theme{Title: subject})
			}
		}
		if len(themes) > 0 {
			return themes
		}
	}
	return ExtractThemes(limitRunes(report.content, maxOverviewRunes))
}

func workstreamsFromBrief(raw string) []InputWorkstream {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var payload historicalBrief
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	result := make([]InputWorkstream, 0, minInt(len(payload.Workstreams), maxCurrentThemes))
	for _, item := range payload.Workstreams {
		subject := limitRunes(sanitizeTitle(item.Subject), titleRuneLimit)
		if subject == "" {
			continue
		}
		workstream := InputWorkstream{Subject: subject}
		for _, deliverable := range item.Deliverables {
			text := limitRunes(strings.TrimSpace(deliverable.Result), 180)
			if text != "" {
				workstream.Deliverables = appendUnique(workstream.Deliverables, text)
			}
			for _, factRef := range deliverable.FactRefs {
				if strings.TrimSpace(factRef) != "" {
					workstream.FactRefs = appendUnique(workstream.FactRefs, factRef)
				}
			}
			if len(workstream.Deliverables) >= 4 {
				break
			}
		}
		result = append(result, workstream)
		if len(result) >= maxCurrentThemes {
			break
		}
	}
	return result
}

func loadWorkspaceRefsByFact(ctx context.Context, database *sql.DB, userID, reportID string) (map[string][]string, error) {
	result := make(map[string][]string)
	rows, err := database.QueryContext(ctx, `
		SELECT DISTINCT source.fact_ref, evidence.workspace_id::text
		FROM daily_reports report
		JOIN report_run_fact_sources source ON source.run_id = report.managed_agent_run_id
		JOIN report_source_selections selection ON selection.attached_run_id = source.run_id
		JOIN report_source_selection_items item
		  ON item.selection_id = selection.id AND item.session_ref_snapshot = source.session_ref
		JOIN report_workspace_evidence evidence
		  ON evidence.source_session_id = item.session_id
		WHERE report.id = $1 AND report.user_id = $2
		ORDER BY source.fact_ref, evidence.workspace_id::text`, reportID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var factRef, workspaceRef string
		if err := rows.Scan(&factRef, &workspaceRef); err != nil {
			return nil, err
		}
		result[factRef] = appendUnique(result[factRef], workspaceRef)
	}
	return result, rows.Err()
}

func classifyNightlySource(report historicalReport, hasOutcome bool) (string, float64) {
	if strings.TrimSpace(report.generationMode) != "managed_agent" {
		return sourceManualFinal, 1
	}
	if report.generatedContentSHA256 != "" {
		if contentSHA256(report.content) != report.generatedContentSHA256 {
			return sourceHumanEdited, 0.95
		}
		if hasOutcome {
			return "explicit_saved", 0.75
		}
		return "auto_carried", 0.50
	}
	if report.edited {
		return sourceHumanEdited, 0.95
	}
	if hasOutcome {
		return "explicit_saved", 0.75
	}
	return "auto_carried", 0.50
}

func loadInputProjects(ctx context.Context, database *sql.DB, userID, reportDate string, themes []InputTheme) ([]InputProject, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT p.id::text, p.canonical_name, p.last_seen_on::text,
		       p.canonical_source_type, p.canonical_source_weight::float8,
		       COALESCE(array_agg(a.alias ORDER BY a.source_weight DESC, a.source_report_date DESC)
		           FILTER (WHERE a.alias IS NOT NULL), '{}'),
		       COALESCE((SELECT array_agg(recent_cue.cue)
		           FROM (
		               SELECT cue.value AS cue, max(occurrence.report_date) AS last_seen
		               FROM report_project_occurrences occurrence
		               CROSS JOIN LATERAL jsonb_array_elements_text(occurrence.workstream_cues_json) cue(value)
		               WHERE occurrence.project_id = p.id
		               GROUP BY cue.value
		               ORDER BY last_seen DESC, cue.value
		               LIMIT $3
		           ) recent_cue), '{}'),
		       COALESCE((SELECT array_agg(link.workspace_id::text ORDER BY link.last_seen_on DESC)
		           FROM report_project_workspace_links link WHERE link.project_id = p.id), '{}')
		FROM report_projects p
		LEFT JOIN report_project_aliases a ON a.project_id = p.id
		WHERE p.user_id = $1 AND p.first_seen_on < $2::date AND p.status <> 'ended'
		  AND EXISTS (
			SELECT 1
			FROM (
				SELECT snapshot.project_memory_json
				FROM report_project_memory_snapshots snapshot
				WHERE snapshot.user_id = $1 AND snapshot.report_date < $2::date
				ORDER BY snapshot.report_date DESC, snapshot.created_at DESC
				LIMIT $4
			) recent_snapshot
			CROSS JOIN LATERAL jsonb_array_elements(
				COALESCE(recent_snapshot.project_memory_json->'projects', '[]'::jsonb)
			) accepted
			WHERE accepted->>'project_ref' = p.id::text
		  )
		GROUP BY p.id
		ORDER BY p.last_seen_on DESC, p.id
		LIMIT 40`, userID, reportDate, maxWorkstreamCues, maxMemorySnapshotDepth)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type ranked struct {
		project InputProject
		score   float64
	}
	rankedProjects := make([]ranked, 0)
	for rows.Next() {
		var project InputProject
		var aliases, workstreamCues, workspaceRefs pq.StringArray
		if err := rows.Scan(
			&project.ProjectRef, &project.CanonicalName, &project.LastSeenOn,
			&project.SourceType, &project.SourceWeight, &aliases, &workstreamCues, &workspaceRefs,
		); err != nil {
			return nil, err
		}
		for _, workspaceRef := range workspaceRefs {
			project.WorkspaceRefs = appendUnique(project.WorkspaceRefs, workspaceRef)
		}
		if !validProjectName(project.CanonicalName) {
			continue
		}
		for _, alias := range aliases {
			if validProjectName(alias) {
				project.Aliases = appendUnique(project.Aliases, alias)
			}
			if len(project.Aliases) >= maxAliasesPerProject {
				break
			}
		}
		for _, cue := range workstreamCues {
			if validProjectName(cue) {
				project.WorkstreamCues = appendUnique(project.WorkstreamCues, cue)
			}
			if len(project.WorkstreamCues) >= maxWorkstreamCues {
				break
			}
		}
		project.MatchedThemes = matchingThemeRefs(project, themes)
		score := inputProjectSimilarity(project, themes)
		rankedProjects = append(rankedProjects, ranked{project: project, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.SliceStable(rankedProjects, func(i, j int) bool { return rankedProjects[i].score > rankedProjects[j].score })
	result := make([]InputProject, 0, maxCandidateProjects)
	for _, item := range rankedProjects {
		result = append(result, item.project)
		if len(result) >= maxCandidateProjects {
			break
		}
	}
	return result, nil
}

func inputProjectSimilarity(project InputProject, themes []InputTheme) float64 {
	best := 0.0
	workspaceMatched := false
	for _, theme := range themes {
		if stringSlicesIntersect(project.WorkspaceRefs, theme.WorkspaceRefs) {
			workspaceMatched = true
		}
		if score := projectThemeNameScore(project, theme, true); score > best {
			best = score
		}
	}
	if workspaceMatched {
		best += 1.2
	}
	if len(project.MatchedThemes) > 1 {
		best += minFloat(float64(len(project.MatchedThemes)-1)*0.1, 0.3)
	}
	return best + project.SourceWeight*0.05
}

func matchingThemeRefs(project InputProject, themes []InputTheme) []string {
	result := make([]string, 0, len(themes))
	for _, theme := range themes {
		if projectThemeNameScore(project, theme, false) >= 0.62 {
			result = append(result, theme.ThemeRef)
		}
	}
	return result
}

func projectThemeNameScore(project InputProject, theme InputTheme, includeCues bool) float64 {
	texts := append([]string{project.CanonicalName}, project.Aliases...)
	if includeCues {
		texts = append(texts, project.WorkstreamCues...)
	}
	best := 0.0
	left := normalizeName(theme.Title)
	for _, value := range texts {
		right := normalizeName(value)
		if left == "" || right == "" {
			continue
		}
		score := ngramDice(left, right)
		if strings.Contains(left, right) || strings.Contains(right, left) {
			score += 0.3
		}
		if score > best {
			best = score
		}
	}
	return best
}

func minFloat(left, right float64) float64 {
	if left < right {
		return left
	}
	return right
}

func stringSlicesIntersect(left, right []string) bool {
	seen := make(map[string]bool, len(left))
	for _, value := range left {
		seen[value] = true
	}
	for _, value := range right {
		if seen[value] {
			return true
		}
	}
	return false
}

type historicalContext struct {
	RecentOverviews []HistoricalReport
	ProjectAnchors  []HistoricalReport
}

func loadHistoricalContext(ctx context.Context, database *sql.DB, userID, reportDate string) (historicalContext, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT r.id::text, r.report_date::text,
		       COALESCE(NULLIF(r.submitted_content, ''), r.content),
		       COALESCE(r.generation_mode, ''), r.edited,
		       COALESCE(s.generated_content_sha256, ''),
		       COALESCE(b.brief_payload::text, ''),
		       EXISTS (
			SELECT 1 FROM report_user_outcome_events outcome
			WHERE outcome.report_id = r.id AND outcome.action IN ('saved', 'submitted')
		   )
		FROM daily_reports r
		LEFT JOIN report_generation_snapshots s ON s.run_id = r.managed_agent_run_id
		LEFT JOIN report_run_briefs b ON b.run_id = r.managed_agent_run_id
		WHERE r.user_id = $1 AND r.report_date < $2::date
		  AND r.status IN ('saved', 'submitted')
		  AND NULLIF(BTRIM(COALESCE(NULLIF(r.submitted_content, ''), r.content, '')), '') IS NOT NULL
		ORDER BY r.report_date DESC, r.updated_at DESC
		LIMIT $3`, userID, reportDate, maxRecentReports+maxHistoricalAnchors)
	if err != nil {
		return historicalContext{}, err
	}
	defer rows.Close()
	result := historicalContext{
		RecentOverviews: make([]HistoricalReport, 0, maxRecentReports),
		ProjectAnchors:  make([]HistoricalReport, 0, maxHistoricalAnchors),
	}
	index := 0
	for rows.Next() {
		var report historicalReport
		var hasOutcome bool
		if err := rows.Scan(
			&report.id, &report.date, &report.content, &report.generationMode, &report.edited,
			&report.generatedContentSHA256, &report.briefPayload, &hasOutcome,
		); err != nil {
			return historicalContext{}, err
		}
		report.sourceType, report.sourceWeight = classifyNightlySource(report, hasOutcome)
		item := HistoricalReport{Date: report.date, SourceType: report.sourceType, SourceWeight: report.sourceWeight}
		if index < maxRecentReports {
			item.Overview = limitRunes(strings.TrimSpace(historicalOverviewForMemory(report)), maxHistoryOverviewRune)
			result.RecentOverviews = append(result.RecentOverviews, item)
		} else {
			item.Overview = limitRunes(historicalProjectAnchor(report), maxHistoryAnchorRunes)
			if item.Overview != "" {
				result.ProjectAnchors = append(result.ProjectAnchors, item)
			}
		}
		index++
	}
	return result, rows.Err()
}

func historicalProjectAnchor(report historicalReport) string {
	themes := themesForHistoricalReport(report)
	names := make([]string, 0, minInt(len(themes), maxCurrentThemes))
	for _, theme := range themes {
		name := limitRunes(sanitizeTitle(theme.Title), titleRuneLimit)
		if validProjectName(name) {
			names = appendUnique(names, name)
		}
		if len(names) >= maxCurrentThemes {
			break
		}
	}
	return strings.Join(names, "；")
}

func historicalOverviewForMemory(report historicalReport) string {
	if report.generationMode == "managed_agent" && report.sourceType != sourceHumanEdited {
		workstreams := workstreamsFromBrief(report.briefPayload)
		if len(workstreams) > 0 {
			lines := make([]string, 0, len(workstreams))
			for index, workstream := range workstreams {
				lines = append(lines, fmt.Sprintf("%d. %s", index+1, workstream.Subject))
			}
			return strings.Join(lines, "\n")
		}
	}
	overview := overviewSection(normalizeMarkdown(report.content))
	if strings.TrimSpace(overview) == "" {
		overview = report.content
	}
	return overview
}

func marshalWithinBudget(input ConsolidationInput) ([]byte, int, error) {
	for {
		payload, err := json.Marshal(input)
		if err != nil {
			return nil, 0, err
		}
		estimate := estimateTokens(string(payload))
		if estimate <= maxInputTokens {
			return payload, estimate, nil
		}
		switch {
		case len(input.HistoricalAnchors) > 0:
			input.HistoricalAnchors = input.HistoricalAnchors[:len(input.HistoricalAnchors)-1]
		case len(input.RecentOverviews) > 0:
			input.RecentOverviews = input.RecentOverviews[:len(input.RecentOverviews)-1]
		case len(input.CandidateProjects) > 0:
			input.CandidateProjects = input.CandidateProjects[:len(input.CandidateProjects)-1]
		default:
			return nil, estimate, errors.New("project memory input exceeds token budget")
		}
	}
}

func estimateTokens(value string) int {
	ascii := 0
	tokens := 0
	for _, current := range value {
		if current <= unicode.MaxASCII {
			ascii++
			continue
		}
		tokens++
	}
	return tokens + (ascii+3)/4
}

func limitRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	return strings.TrimSpace(string([]rune(value)[:limit]))
}
