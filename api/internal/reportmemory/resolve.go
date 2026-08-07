package reportmemory

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	maxCandidates       = 3
	highConfidenceScore = 0.86
	sourceManualFinal   = "manual_final"
	sourceHumanEdited   = "human_edited"
	sourceExplicitSaved = "explicit_saved"
)

type storedProject struct {
	ID            string
	CanonicalName string
	LastSeenOn    string
	Aliases       []storedAlias
}

type storedAlias struct {
	Text         string
	Normalized   string
	Type         string
	SourceType   string
	SourceWeight float64
}

type historicalReport struct {
	id, date, content      string
	generationMode         string
	edited                 bool
	generatedContentSHA256 string
	briefPayload           string
	sourceType             string
	sourceWeight           float64
}

type historicalBrief struct {
	Workstreams []struct {
		Subject      string `json:"subject"`
		Deliverables []struct {
			Result   string   `json:"result"`
			FactRefs []string `json:"fact_refs"`
		} `json:"deliverables"`
	} `json:"workstreams"`
}

func resolveShadow(ctx context.Context, tx *sql.Tx, request ResolveRequest) (ResolutionSnapshot, error) {
	snapshot := ResolutionSnapshot{AlgorithmVersion: AlgorithmVersion, Mode: "shadow", Facts: []FactResolution{}}
	if tx == nil || strings.TrimSpace(request.RunID) == "" || strings.TrimSpace(request.UserID) == "" || strings.TrimSpace(request.ReportDate) == "" {
		return snapshot, errors.New("invalid project memory request")
	}
	if err := syncHistoricalReports(ctx, tx, request.UserID, request.ReportDate); err != nil {
		return snapshot, err
	}
	projects, err := loadProjects(ctx, tx, request.UserID, request.ReportDate)
	if err != nil {
		return snapshot, err
	}
	for _, fact := range request.Facts {
		resolution := resolveFact(fact, projects, request.ReportDate)
		snapshot.Facts = append(snapshot.Facts, resolution)
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return snapshot, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO report_project_resolution_snapshots (
			run_id, user_id, report_date, algorithm_version, snapshot_json
		) VALUES ($1, $2, $3::date, $4, $5::jsonb)
		ON CONFLICT (run_id) DO NOTHING`, request.RunID, request.UserID, request.ReportDate, AlgorithmVersion, string(payload))
	return snapshot, err
}

func syncHistoricalReports(ctx context.Context, tx *sql.Tx, userID, reportDate string) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT r.id::text, r.report_date::text,
		       COALESCE(NULLIF(r.submitted_content, ''), r.content),
		       COALESCE(r.generation_mode, ''), r.edited,
		       COALESCE(snapshot.generated_content_sha256, ''),
		       COALESCE(brief.brief_payload::text, '')
		FROM daily_reports r
		LEFT JOIN report_generation_snapshots snapshot ON snapshot.run_id = r.managed_agent_run_id
		LEFT JOIN report_run_briefs brief ON brief.run_id = r.managed_agent_run_id
		WHERE r.user_id = $1
		  AND r.report_date < $2::date
		  AND r.status IN ('saved', 'submitted')
		  AND NULLIF(BTRIM(COALESCE(NULLIF(r.submitted_content, ''), r.content, '')), '') IS NOT NULL
		  AND (
			r.edited = true OR r.status = 'submitted' OR EXISTS (
				SELECT 1 FROM report_user_outcome_events outcome
				WHERE outcome.report_id = r.id AND outcome.action IN ('saved', 'submitted')
			)
		  )
		ORDER BY r.report_date, r.id`, userID, reportDate)
	if err != nil {
		return err
	}
	defer rows.Close()
	reports := make([]historicalReport, 0)
	for rows.Next() {
		var current historicalReport
		if err := rows.Scan(
			&current.id, &current.date, &current.content,
			&current.generationMode, &current.edited,
			&current.generatedContentSHA256, &current.briefPayload,
		); err != nil {
			return err
		}
		current.sourceType, current.sourceWeight = classifyHistoricalSource(current)
		reports = append(reports, current)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fingerprint := reportFingerprint(reports)
	var storedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT source_fingerprint
		FROM report_project_memory_states
		WHERE user_id = $1`, userID).Scan(&storedFingerprint)
	if err == nil && storedFingerprint == fingerprint {
		return nil
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM report_projects WHERE user_id = $1`, userID); err != nil {
		return err
	}
	for _, current := range reports {
		if err := syncReport(ctx, tx, userID, current); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO report_project_memory_states (user_id, source_fingerprint, synced_at)
		VALUES ($1, $2, now())
		ON CONFLICT (user_id) DO UPDATE SET
			source_fingerprint = EXCLUDED.source_fingerprint,
			synced_at = now()`, userID, fingerprint)
	return err
}

func syncReport(ctx context.Context, tx *sql.Tx, userID string, report historicalReport) error {
	for _, theme := range themesForHistoricalReport(report) {
		if !validProjectName(theme.Title) {
			continue
		}
		normalized := normalizeName(theme.Title)
		deterministicID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(userID+"\x00"+normalized)).String()
		var projectID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO report_projects (
				id, user_id, canonical_name, normalized_name,
				canonical_source_type, canonical_source_weight,
				first_seen_on, last_seen_on
			) VALUES ($1, $2, $3, $4, $5, $6, $7::date, $7::date)
			ON CONFLICT (user_id, normalized_name) DO UPDATE SET
				canonical_name = CASE
					WHEN EXCLUDED.canonical_source_weight >= report_projects.canonical_source_weight THEN EXCLUDED.canonical_name
					ELSE report_projects.canonical_name
				END,
				canonical_source_type = CASE
					WHEN EXCLUDED.canonical_source_weight >= report_projects.canonical_source_weight THEN EXCLUDED.canonical_source_type
					ELSE report_projects.canonical_source_type
				END,
				canonical_source_weight = GREATEST(report_projects.canonical_source_weight, EXCLUDED.canonical_source_weight),
				first_seen_on = LEAST(report_projects.first_seen_on, EXCLUDED.first_seen_on),
				last_seen_on = GREATEST(report_projects.last_seen_on, EXCLUDED.last_seen_on),
				updated_at = now()
			RETURNING id::text`, deterministicID, userID, theme.Title, normalized,
			report.sourceType, report.sourceWeight, report.date).Scan(&projectID)
		if err != nil {
			return err
		}
		if err := upsertAlias(ctx, tx, projectID, report, theme.Title, "canonical", 1); err != nil {
			return err
		}
		for _, child := range theme.Children {
			if validProjectName(child) {
				if err := upsertAlias(ctx, tx, projectID, report, child, "child_topic", 0.95); err != nil {
					return err
				}
			}
		}
		childrenValue := theme.Children
		if childrenValue == nil {
			childrenValue = []string{}
		}
		children, err := json.Marshal(childrenValue)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO report_project_occurrences (
				project_id, report_id, report_date, observed_title, child_topics_json,
				source_type, source_weight
			) VALUES ($1, $2, $3::date, $4, $5::jsonb, $6, $7)
			ON CONFLICT (project_id, report_id) DO UPDATE SET
				report_date = EXCLUDED.report_date,
				observed_title = EXCLUDED.observed_title,
				child_topics_json = EXCLUDED.child_topics_json,
				source_type = EXCLUDED.source_type,
				source_weight = EXCLUDED.source_weight`, projectID, report.id, report.date, theme.Title,
			string(children), report.sourceType, report.sourceWeight); err != nil {
			return err
		}
	}
	return nil
}

func reportFingerprint(reports []historicalReport) string {
	hash := sha256.New()
	hash.Write([]byte(AlgorithmVersion))
	hash.Write([]byte{0})
	for _, report := range reports {
		hash.Write([]byte(report.id))
		hash.Write([]byte{0})
		hash.Write([]byte(report.date))
		hash.Write([]byte{0})
		hash.Write([]byte(report.content))
		hash.Write([]byte{0})
		hash.Write([]byte(report.sourceType))
		hash.Write([]byte{0})
		hash.Write([]byte(report.briefPayload))
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func upsertAlias(ctx context.Context, tx *sql.Tx, projectID string, report historicalReport, alias, aliasType string, confidence float64) error {
	if !validProjectName(alias) {
		return nil
	}
	normalized := normalizeName(alias)
	_, err := tx.ExecContext(ctx, `
		INSERT INTO report_project_aliases (
			project_id, alias, normalized_alias, alias_type,
			source_report_id, source_report_date, confidence,
			source_type, source_weight
		) VALUES ($1, $2, $3, $4, $5, $6::date, $7, $8, $9)
		ON CONFLICT (project_id, normalized_alias) DO UPDATE SET
			alias = EXCLUDED.alias,
			alias_type = CASE
				WHEN report_project_aliases.alias_type = 'canonical' THEN 'canonical'
				ELSE EXCLUDED.alias_type
			END,
			source_report_id = CASE
				WHEN EXCLUDED.source_report_date >= report_project_aliases.source_report_date THEN EXCLUDED.source_report_id
				ELSE report_project_aliases.source_report_id
			END,
			source_report_date = GREATEST(report_project_aliases.source_report_date, EXCLUDED.source_report_date),
			confidence = GREATEST(report_project_aliases.confidence, EXCLUDED.confidence),
			source_type = CASE
				WHEN EXCLUDED.source_weight >= report_project_aliases.source_weight THEN EXCLUDED.source_type
				ELSE report_project_aliases.source_type
			END,
			source_weight = GREATEST(report_project_aliases.source_weight, EXCLUDED.source_weight),
			updated_at = now()`, projectID, alias, normalized, aliasType,
		report.id, report.date, confidence, report.sourceType, report.sourceWeight)
	return err
}

func classifyHistoricalSource(report historicalReport) (string, float64) {
	if strings.TrimSpace(report.generationMode) != "managed_agent" {
		return sourceManualFinal, 1
	}
	if report.generatedContentSHA256 != "" {
		if contentSHA256(report.content) != report.generatedContentSHA256 {
			return sourceHumanEdited, 0.95
		}
		return sourceExplicitSaved, confirmedAIWeight(report)
	}
	if report.edited {
		return sourceHumanEdited, 0.95
	}
	return sourceExplicitSaved, confirmedAIWeight(report)
}

func confirmedAIWeight(report historicalReport) float64 {
	if len(themesFromBrief(report.briefPayload)) > 0 {
		return 0.75
	}
	return 0.55
}

func themesForHistoricalReport(report historicalReport) []Theme {
	if report.sourceType == sourceExplicitSaved {
		if themes := themesFromBrief(report.briefPayload); len(themes) > 0 {
			return themes
		}
	}
	return ExtractThemes(report.content)
}

func themesFromBrief(raw string) []Theme {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var payload historicalBrief
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	themes := make([]Theme, 0, len(payload.Workstreams))
	for _, workstream := range payload.Workstreams {
		subject := sanitizeTitle(workstream.Subject)
		if subject == "" || findTheme(themes, subject) >= 0 {
			continue
		}
		themes = append(themes, Theme{Title: subject})
		if len(themes) >= themeLimit {
			break
		}
	}
	return themes
}

func contentSHA256(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return hex.EncodeToString(sum[:])
}

func loadProjects(ctx context.Context, tx *sql.Tx, userID, reportDate string) ([]storedProject, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT source.project_id, source.canonical_name, source.last_seen_on,
		       source.term, source.normalized_term, source.term_type,
		       source.source_type, source.source_weight
		FROM (
			SELECT p.id::text AS project_id, p.canonical_name, p.last_seen_on::text,
			       a.alias AS term, a.normalized_alias AS normalized_term, a.alias_type AS term_type,
			       a.source_type, a.source_weight::float8
			FROM report_projects p
			JOIN report_project_aliases a ON a.project_id = p.id
			WHERE p.user_id = $1 AND p.first_seen_on < $2::date AND p.status <> 'ended'
			UNION ALL
			SELECT p.id::text, p.canonical_name, p.last_seen_on::text,
			       cue.value, cue.value, 'workstream_cue',
			       occurrence.source_type, occurrence.source_weight::float8
			FROM report_projects p
			JOIN report_project_occurrences occurrence ON occurrence.project_id = p.id
			CROSS JOIN LATERAL jsonb_array_elements_text(occurrence.workstream_cues_json) cue(value)
			WHERE p.user_id = $1 AND p.first_seen_on < $2::date AND p.status <> 'ended'
		) source
		ORDER BY source.last_seen_on DESC, source.project_id, source.term_type, source.normalized_term`, userID, reportDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := make([]storedProject, 0)
	indexes := map[string]int{}
	for rows.Next() {
		var id, name, lastSeen, alias, normalizedAlias, aliasType, sourceType string
		var sourceWeight float64
		if err := rows.Scan(
			&id, &name, &lastSeen, &alias, &normalizedAlias, &aliasType,
			&sourceType, &sourceWeight,
		); err != nil {
			return nil, err
		}
		if !validProjectName(name) || !validProjectName(alias) {
			continue
		}
		if aliasType == "workstream_cue" {
			normalizedAlias = normalizeName(alias)
		}
		index, exists := indexes[id]
		if !exists {
			projects = append(projects, storedProject{ID: id, CanonicalName: name, LastSeenOn: lastSeen})
			index = len(projects) - 1
			indexes[id] = index
		}
		duplicate := false
		for _, existing := range projects[index].Aliases {
			if existing.Normalized == normalizedAlias && existing.Type == aliasType {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		projects[index].Aliases = append(projects[index].Aliases, storedAlias{
			Text: alias, Normalized: normalizedAlias, Type: aliasType,
			SourceType: sourceType, SourceWeight: sourceWeight,
		})
	}
	return projects, rows.Err()
}

func resolveFact(fact FactInput, projects []storedProject, reportDate string) FactResolution {
	resolution := FactResolution{FactRef: fact.FactRef, Decision: "unmatched", CandidateList: []Candidate{}}
	searchText := normalizeName(strings.Join(append([]string{fact.Text}, fact.ThreadGoals...), " "))
	if searchText == "" {
		return resolution
	}
	for _, project := range projects {
		candidate, ok := scoreProject(searchText, project, reportDate)
		if ok {
			resolution.CandidateList = append(resolution.CandidateList, candidate)
		}
	}
	sort.SliceStable(resolution.CandidateList, func(i, j int) bool {
		if resolution.CandidateList[i].Score == resolution.CandidateList[j].Score {
			return resolution.CandidateList[i].ProjectRef < resolution.CandidateList[j].ProjectRef
		}
		return resolution.CandidateList[i].Score > resolution.CandidateList[j].Score
	})
	if len(resolution.CandidateList) > maxCandidates {
		resolution.CandidateList = resolution.CandidateList[:maxCandidates]
	}
	if len(resolution.CandidateList) > 0 {
		resolution.Confidence = resolution.CandidateList[0].Score
		ambiguousExactAlias := false
		if containsSignal(resolution.CandidateList[0].Signals, "exact_alias") {
			for _, candidate := range resolution.CandidateList[1:] {
				if containsSignal(candidate.Signals, "exact_alias") {
					ambiguousExactAlias = true
					break
				}
			}
		}
		if resolution.Confidence >= highConfidenceScore &&
			containsSignal(resolution.CandidateList[0].Signals, "exact_alias") && !ambiguousExactAlias {
			resolution.Decision = "matched"
			resolution.ProjectRef = resolution.CandidateList[0].ProjectRef
		}
	}
	return resolution
}

func scoreProject(searchText string, project storedProject, reportDate string) (Candidate, bool) {
	bestSimilarity := 0.0
	bestCoverage := 0.0
	bestSimilarityWeight := 0.0
	bestCoverageWeight := 0.0
	bestExactWeight := 0.0
	bestSimilaritySource := ""
	bestCoverageSource := ""
	bestExactSource := ""
	exactCount := 0
	signals := make([]string, 0, 3)
	for _, alias := range project.Aliases {
		if utf8.RuneCountInString(alias.Normalized) < 3 {
			continue
		}
		if strings.Contains(searchText, alias.Normalized) {
			exactCount++
			if alias.SourceWeight > bestExactWeight {
				bestExactWeight = alias.SourceWeight
				bestExactSource = alias.SourceType
			}
			continue
		}
		if similarity := ngramDice(searchText, alias.Normalized); similarity > bestSimilarity {
			bestSimilarity = similarity
			bestSimilarityWeight = alias.SourceWeight
			bestSimilaritySource = alias.SourceType
		}
		if coverage := ngramCoverage(searchText, alias.Normalized); coverage > bestCoverage {
			bestCoverage = coverage
			bestCoverageWeight = alias.SourceWeight
			bestCoverageSource = alias.SourceType
		}
	}
	base := 0.0
	selectedSourceType := ""
	if exactCount > 0 {
		base = 0.76 + 0.12*bestExactWeight
		selectedSourceType = bestExactSource
		signals = append(signals, "exact_alias")
		if exactCount > 1 {
			base += math.Min(0.04, float64(exactCount-1)*0.02)
			signals = append(signals, "multiple_aliases")
		}
	} else if bestSimilarity >= 0.48 {
		base = bestSimilarity * 0.72 * sourceWeightMultiplier(bestSimilarityWeight)
		selectedSourceType = bestSimilaritySource
		signals = append(signals, "ngram_similarity")
	} else if bestCoverage >= 0.40 {
		base = bestCoverage * 0.68 * sourceWeightMultiplier(bestCoverageWeight)
		selectedSourceType = bestCoverageSource
		signals = append(signals, "alias_coverage")
	} else {
		return Candidate{}, false
	}
	recency := recencyBoost(project.LastSeenOn, reportDate)
	if recency > 0 {
		base += recency
		signals = append(signals, "recent_occurrence")
	}
	if selectedSourceType != "" {
		signals = append(signals, selectedSourceType+"_source")
	}
	return Candidate{
		ProjectRef: project.ID, CanonicalName: project.CanonicalName,
		Score: roundScore(math.Min(base, 0.99)), Signals: signals, LastSeenOn: project.LastSeenOn,
	}, true
}

func sourceWeightMultiplier(weight float64) float64 {
	if weight < 0 {
		weight = 0
	}
	if weight > 1 {
		weight = 1
	}
	return 0.65 + 0.35*weight
}

func recencyBoost(lastSeen, reportDate string) float64 {
	last, firstErr := time.Parse("2006-01-02", lastSeen)
	current, secondErr := time.Parse("2006-01-02", reportDate)
	if firstErr != nil || secondErr != nil || !last.Before(current) {
		return 0
	}
	days := current.Sub(last).Hours() / 24
	return 0.08 * math.Exp(-days/30)
}

func ngramDice(left, right string) float64 {
	leftSet, rightSet := runeBigrams(left), runeBigrams(right)
	if len(leftSet) == 0 || len(rightSet) == 0 {
		return 0
	}
	intersection := 0
	for value := range leftSet {
		if _, exists := rightSet[value]; exists {
			intersection++
		}
	}
	return 2 * float64(intersection) / float64(len(leftSet)+len(rightSet))
}

func ngramCoverage(searchText, alias string) float64 {
	searchSet, aliasSet := runeBigrams(searchText), runeBigrams(alias)
	if len(searchSet) == 0 || len(aliasSet) == 0 {
		return 0
	}
	intersection := 0
	for value := range aliasSet {
		if _, exists := searchSet[value]; exists {
			intersection++
		}
	}
	return float64(intersection) / float64(len(aliasSet))
}

func runeBigrams(value string) map[string]struct{} {
	runes := []rune(value)
	result := make(map[string]struct{})
	if len(runes) == 1 {
		result[string(runes)] = struct{}{}
		return result
	}
	for index := 0; index+1 < len(runes); index++ {
		result[string(runes[index:index+2])] = struct{}{}
	}
	return result
}

func containsSignal(signals []string, expected string) bool {
	for _, signal := range signals {
		if signal == expected {
			return true
		}
	}
	return false
}

func roundScore(value float64) float64 { return math.Round(value*1000) / 1000 }
