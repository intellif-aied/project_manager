package reportmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lib/pq"
)

const consolidationInputSchema = "project-memory-consolidation-input/v1"

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
	input.SourceType, input.SourceWeight = report.sourceType, report.sourceWeight
	themes := ExtractThemes(limitRunes(report.content, maxOverviewRunes))
	if len(themes) > maxCurrentThemes {
		themes = themes[:maxCurrentThemes]
	}
	for index, theme := range themes {
		input.CurrentThemes = append(input.CurrentThemes, InputTheme{
			ThemeRef: fmt.Sprintf("theme-%03d", index+1), Title: limitRunes(theme.Title, titleRuneLimit),
		})
	}
	if len(input.CurrentThemes) == 0 {
		return input, nil, 0, errors.New("final overview has no project memory themes")
	}
	for _, workstream := range workstreamsFromBrief(report.briefPayload) {
		input.BriefWorkstreams = append(input.BriefWorkstreams, workstream)
		if len(input.BriefWorkstreams) >= maxCurrentThemes {
			break
		}
	}
	projects, err := loadInputProjects(ctx, database, job.UserID, job.ReportDate, input.CurrentThemes)
	if err != nil {
		return input, nil, 0, err
	}
	input.CandidateProjects = projects
	history, err := loadRecentOverviews(ctx, database, job.UserID, job.ReportDate)
	if err != nil {
		return input, nil, 0, err
	}
	input.RecentOverviews = history

	payload, estimate, err := marshalWithinBudget(input)
	return input, payload, estimate, err
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
		           FILTER (WHERE a.alias IS NOT NULL), '{}')
		FROM report_projects p
		LEFT JOIN report_project_aliases a ON a.project_id = p.id
		WHERE p.user_id = $1 AND p.first_seen_on < $2::date AND p.status <> 'ended'
		  AND EXISTS (
			SELECT 1
			FROM report_project_memory_snapshots snapshot
			CROSS JOIN LATERAL jsonb_array_elements(
				COALESCE(snapshot.project_memory_json->'projects', '[]'::jsonb)
			) accepted
			WHERE snapshot.id = (
				SELECT latest.id FROM report_project_memory_snapshots latest
				WHERE latest.user_id = $1 AND latest.report_date < $2::date
				ORDER BY latest.report_date DESC, latest.created_at DESC LIMIT 1
			)
			  AND accepted->>'project_ref' = p.id::text
		  )
		GROUP BY p.id
		ORDER BY p.last_seen_on DESC, p.id
		LIMIT 40`, userID, reportDate)
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
		var aliases pq.StringArray
		if err := rows.Scan(
			&project.ProjectRef, &project.CanonicalName, &project.LastSeenOn,
			&project.SourceType, &project.SourceWeight, &aliases,
		); err != nil {
			return nil, err
		}
		for _, alias := range aliases {
			project.Aliases = appendUnique(project.Aliases, limitRunes(alias, titleRuneLimit))
			if len(project.Aliases) >= maxAliasesPerProject {
				break
			}
		}
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
	texts := append([]string{project.CanonicalName}, project.Aliases...)
	for _, theme := range themes {
		for _, value := range texts {
			left, right := normalizeName(theme.Title), normalizeName(value)
			score := ngramDice(left, right)
			if strings.Contains(left, right) || strings.Contains(right, left) {
				score += 0.3
			}
			if score > best {
				best = score
			}
		}
	}
	return best + project.SourceWeight*0.05
}

func loadRecentOverviews(ctx context.Context, database *sql.DB, userID, reportDate string) ([]HistoricalReport, error) {
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
		LIMIT $3`, userID, reportDate, maxRecentReports)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]HistoricalReport, 0, maxRecentReports)
	for rows.Next() {
		var report historicalReport
		var hasOutcome bool
		if err := rows.Scan(
			&report.id, &report.date, &report.content, &report.generationMode, &report.edited,
			&report.generatedContentSHA256, &report.briefPayload, &hasOutcome,
		); err != nil {
			return nil, err
		}
		report.sourceType, report.sourceWeight = classifyNightlySource(report, hasOutcome)
		overview := overviewSection(normalizeMarkdown(report.content))
		if strings.TrimSpace(overview) == "" {
			overview = report.content
		}
		result = append(result, HistoricalReport{
			Date: report.date, Overview: limitRunes(strings.TrimSpace(overview), maxHistoryOverviewRune),
			SourceType: report.sourceType, SourceWeight: report.sourceWeight,
		})
	}
	return result, rows.Err()
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
