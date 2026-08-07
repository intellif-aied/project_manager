package reportmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	proposalSchema       = "project-memory-proposal/v1"
	projectNameRuneLimit = 48
)

var activityOnlyName = regexp.MustCompile(`^(修复|测试|部署|调研|优化|开发|验证|走读|讨论|排查|发布|上线)$`)
var literalProjectKey = regexp.MustCompile(`[A-Za-z][A-Za-z0-9._-]{2,23}`)

func parseAndValidateProposal(raw string, input ConsolidationInput) (MemoryProposal, []byte, int, error) {
	clean := strings.TrimSpace(raw)
	if strings.HasPrefix(clean, "```") {
		lines := strings.Split(clean, "\n")
		if len(lines) >= 3 {
			clean = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}
	start, end := strings.Index(clean, "{"), strings.LastIndex(clean, "}")
	if start < 0 || end < start {
		return MemoryProposal{}, nil, estimateTokens(clean), errors.New("memory proposal is not a JSON object")
	}
	clean = clean[start : end+1]
	estimate := estimateTokens(clean)
	if estimate > maxOutputTokens {
		return MemoryProposal{}, nil, estimate, errors.New("memory proposal exceeds output budget")
	}
	// Agents commonly echo immutable request metadata beside the proposal. Keep
	// the top-level envelope forward-compatible, while decoding the decisions
	// themselves strictly so an invented decision field cannot silently pass.
	var envelope struct {
		SchemaVersion string          `json:"schema_version"`
		Decisions     json.RawMessage `json:"decisions"`
	}
	if err := json.Unmarshal([]byte(clean), &envelope); err != nil {
		return MemoryProposal{}, nil, estimate, fmt.Errorf("decode memory proposal: %w", err)
	}
	proposal := MemoryProposal{SchemaVersion: envelope.SchemaVersion}
	decoder := json.NewDecoder(strings.NewReader(string(envelope.Decisions)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal.Decisions); err != nil {
		return MemoryProposal{}, nil, estimate, fmt.Errorf("decode memory decisions: %w", err)
	}
	if proposal.SchemaVersion != proposalSchema {
		return MemoryProposal{}, nil, estimate, errors.New("memory proposal schema is invalid")
	}
	inputThemes := make(map[string]InputTheme, len(input.CurrentThemes))
	for _, theme := range input.CurrentThemes {
		inputThemes[theme.ThemeRef] = theme
	}
	candidates := make(map[string]InputProject, len(input.CandidateProjects))
	for _, project := range input.CandidateProjects {
		candidates[project.ProjectRef] = project
	}
	seen := make(map[string]bool, len(proposal.Decisions))
	for index := range proposal.Decisions {
		decision := &proposal.Decisions[index]
		decision.ThemeRef = strings.TrimSpace(decision.ThemeRef)
		decision.Action = strings.TrimSpace(decision.Action)
		decision.ProjectRef = strings.TrimSpace(decision.ProjectRef)
		decision.CanonicalName = limitRunes(decision.CanonicalName, titleRuneLimit)
		decision.Reason = limitRunes(decision.Reason, 240)
		if _, ok := inputThemes[decision.ThemeRef]; !ok || seen[decision.ThemeRef] {
			return MemoryProposal{}, nil, estimate, fmt.Errorf("decision %d has unknown or duplicate theme_ref", index)
		}
		seen[decision.ThemeRef] = true
		if decision.Confidence < 0 || decision.Confidence > 1 {
			return MemoryProposal{}, nil, estimate, fmt.Errorf("decision %s confidence is invalid", decision.ThemeRef)
		}
		switch decision.Action {
		case "link_existing":
			if _, ok := candidates[decision.ProjectRef]; !ok {
				return MemoryProposal{}, nil, estimate, fmt.Errorf("decision %s references an unavailable project", decision.ThemeRef)
			}
			if decision.Confidence < highConfidenceScore {
				decision.Action, decision.ProjectRef = "unresolved", ""
			}
		case "create_new":
			if !validProjectName(decision.CanonicalName) {
				decision.Action, decision.ProjectRef, decision.CanonicalName, decision.Aliases = "unresolved", "", "", nil
			}
		case "unresolved":
			decision.ProjectRef, decision.CanonicalName, decision.Aliases, decision.WorkstreamCues = "", "", nil, nil
		case "suggest_rename", "suggest_merge":
			// Suggestions remain observable but are not applied in v1.
		default:
			return MemoryProposal{}, nil, estimate, fmt.Errorf("decision %s action is invalid", decision.ThemeRef)
		}
		decision.Aliases = normalizeProposalAliases(decision.Aliases)
		decision.WorkstreamCues = normalizeWorkstreamCues(decision.WorkstreamCues)
	}
	if len(seen) != len(inputThemes) {
		return MemoryProposal{}, nil, estimate, errors.New("memory proposal does not account for every current theme")
	}
	applyStrongParentScope(&proposal, input)
	sort.SliceStable(proposal.Decisions, func(i, j int) bool { return proposal.Decisions[i].ThemeRef < proposal.Decisions[j].ThemeRef })
	payload, err := json.Marshal(proposal)
	return proposal, payload, estimate, err
}

func applyStrongParentScope(proposal *MemoryProposal, input ConsolidationInput) {
	if proposal == nil {
		return
	}
	strongParents := make([]InputProject, 0)
	parentByTheme := make(map[string][]string)
	for _, candidate := range input.CandidateProjects {
		if candidate.SourceWeight < 0.95 ||
			(candidate.SourceType != sourceManualFinal && candidate.SourceType != sourceHumanEdited) ||
			len(candidate.MatchedThemes) < 2 {
			continue
		}
		strongParents = append(strongParents, candidate)
		for _, themeRef := range candidate.MatchedThemes {
			parentByTheme[themeRef] = appendUnique(parentByTheme[themeRef], candidate.ProjectRef)
		}
	}
	if len(strongParents) == 0 {
		return
	}
	candidates := make(map[string]InputProject, len(input.CandidateProjects))
	for _, candidate := range input.CandidateProjects {
		candidates[candidate.ProjectRef] = candidate
	}
	for _, parent := range strongParents {
		covered := make([]int, 0, len(parent.MatchedThemes))
		for index := range proposal.Decisions {
			decision := &proposal.Decisions[index]
			if len(parentByTheme[decision.ThemeRef]) != 1 || parentByTheme[decision.ThemeRef][0] != parent.ProjectRef {
				continue
			}
			if decision.Action != "link_existing" && decision.Action != "create_new" {
				continue
			}
			covered = append(covered, index)
		}
		if len(covered) < 2 {
			continue
		}
		for _, index := range covered {
			decision := &proposal.Decisions[index]
			if decision.Action == "link_existing" && decision.ProjectRef == parent.ProjectRef {
				continue
			}
			if chosen, ok := candidates[decision.ProjectRef]; ok && chosen.SourceWeight >= parent.SourceWeight {
				continue
			}
			decision.Action = "link_existing"
			decision.ProjectRef = parent.ProjectRef
			decision.CanonicalName = ""
			decision.Confidence = maxFloat(decision.Confidence, highConfidenceScore)
			decision.Reason = "服务端依据人工确认父项目覆盖多个当天主题归并"
		}
	}
}

func maxFloat(left, right float64) float64 {
	if left > right {
		return left
	}
	return right
}

// ValidateProposal is the Project Memory MCP write seam. It applies the same
// bounded schema and semantic validation used by the nightly worker.
func ValidateProposal(raw []byte, input ConsolidationInput) (MemoryProposal, []byte, int, error) {
	return parseAndValidateProposal(string(raw), input)
}

func validProjectName(value string) bool {
	value = strings.TrimSpace(value)
	count := utf8.RuneCountInString(value)
	if count < 2 || count > projectNameRuneLimit || activityOnlyName.MatchString(value) {
		return false
	}
	return !strings.ContainsAny(value, "\r\n。；！？") &&
		!strings.HasPrefix(value, "#") && !strings.HasPrefix(value, "- ")
}

func normalizeProposalAliases(values []string) []string {
	result := make([]string, 0, minInt(len(values), maxAliasesPerProject))
	for _, value := range values {
		value = limitRunes(value, titleRuneLimit)
		if validProjectName(value) {
			result = appendUnique(result, value)
		}
		if len(result) >= maxAliasesPerProject {
			break
		}
	}
	return result
}

func normalizeWorkstreamCues(values []string) []string {
	result := make([]string, 0, minInt(len(values), maxDecisionCues))
	for _, value := range values {
		value = limitRunes(value, titleRuneLimit)
		if validProjectName(value) {
			result = appendUnique(result, value)
		}
		if len(result) >= maxDecisionCues {
			break
		}
	}
	return result
}

func applyProposal(
	ctx context.Context, database *sql.DB, job queuedJob, input ConsolidationInput,
	proposal MemoryProposal, inputPayload, proposalPayload []byte, inputTokens, outputTokens int,
	modelID, taskID string, startedAt, finishedAt time.Time,
) (string, error) {
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var desired, claimed, status string
	if err := tx.QueryRowContext(ctx, `
		SELECT desired_source_fingerprint, COALESCE(claimed_source_fingerprint, ''), status
		FROM report_project_memory_jobs
		WHERE user_id = $1 AND report_date = $2::date
		FOR UPDATE`, job.UserID, job.ReportDate).Scan(&desired, &claimed, &status); err != nil {
		return "", err
	}
	if status != "running" || claimed == "" || claimed != job.ClaimedSourceFingerprint {
		return "", errors.New("project memory job changed before proposal apply")
	}
	if desired != claimed {
		_, err := tx.ExecContext(ctx, `
			UPDATE report_project_memory_jobs SET
				status = 'pending', due_at = $3, claimed_source_fingerprint = NULL,
				external_task_id = NULL, lease_owner = NULL, lease_until = NULL,
				last_error = NULL, finished_at = $4, updated_at = now()
			WHERE user_id = $1 AND report_date = $2::date`, job.UserID, job.ReportDate,
			nextNightlyWindow(finishedAt), finishedAt)
		if err != nil {
			return "", err
		}
		return "", tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM report_project_occurrences occurrence
		USING report_projects project
		WHERE occurrence.project_id = project.id
		  AND occurrence.report_id = $1 AND project.user_id = $2`, job.ReportID, job.UserID); err != nil {
		return "", err
	}
	themes := make(map[string]InputTheme, len(input.CurrentThemes))
	for _, theme := range input.CurrentThemes {
		themes[theme.ThemeRef] = theme
	}
	type appliedProject struct {
		ID, CanonicalName string
		Titles            []string
		Aliases           []string
		WorkstreamCues    []string
	}
	applied := map[string]*appliedProject{}
	for _, decision := range proposal.Decisions {
		if decision.Action != "link_existing" && decision.Action != "create_new" {
			continue
		}
		projectID := decision.ProjectRef
		canonicalName := decision.CanonicalName
		if decision.Action == "create_new" {
			normalized := normalizeName(canonicalName)
			projectID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(job.UserID+"\x00"+normalized)).String()
			err := tx.QueryRowContext(ctx, `
				INSERT INTO report_projects (
					id, user_id, canonical_name, normalized_name,
					canonical_source_type, canonical_source_weight, first_seen_on, last_seen_on
				) VALUES ($1, $2, $3, $4, $5, $6, $7::date, $7::date)
				ON CONFLICT (user_id, normalized_name) DO UPDATE SET
					last_seen_on = GREATEST(report_projects.last_seen_on, EXCLUDED.last_seen_on),
					updated_at = now()
				RETURNING id::text, canonical_name`, projectID, job.UserID, canonicalName, normalized,
				input.SourceType, input.SourceWeight, job.ReportDate).Scan(&projectID, &canonicalName)
			if err != nil {
				return "", err
			}
		} else {
			if err := tx.QueryRowContext(ctx, `
				UPDATE report_projects SET last_seen_on = GREATEST(last_seen_on, $3::date), updated_at = now()
				WHERE id = $1 AND user_id = $2 RETURNING canonical_name`, projectID, job.UserID, job.ReportDate).
				Scan(&canonicalName); err != nil {
				return "", err
			}
		}
		item := applied[projectID]
		if item == nil {
			item = &appliedProject{ID: projectID, CanonicalName: canonicalName}
			applied[projectID] = item
		}
		themeTitle := themes[decision.ThemeRef].Title
		item.Titles = appendUnique(item.Titles, themeTitle)
		for _, alias := range decision.Aliases {
			item.Aliases = appendUnique(item.Aliases, alias)
		}
		for _, cue := range decision.WorkstreamCues {
			item.WorkstreamCues = appendUnique(item.WorkstreamCues, cue)
		}
		for _, workspaceRef := range themes[decision.ThemeRef].WorkspaceRefs {
			if err := upsertProjectWorkspaceLink(
				ctx, tx, job, input, projectID, workspaceRef, decision.ThemeRef, decision.Confidence,
			); err != nil {
				return "", err
			}
		}
	}
	for _, item := range applied {
		report := historicalReport{id: job.ReportID, date: job.ReportDate, sourceType: input.SourceType, sourceWeight: input.SourceWeight}
		if err := upsertAlias(ctx, tx, item.ID, report, item.CanonicalName, "canonical", 1); err != nil {
			return "", err
		}
		for _, projectKey := range derivedProjectKeys(item.CanonicalName) {
			if err := upsertAlias(ctx, tx, item.ID, report, projectKey, "child_topic", 0.98); err != nil {
				return "", err
			}
		}
		for _, alias := range item.Aliases {
			if err := upsertAlias(ctx, tx, item.ID, report, alias, "child_topic", 0.9); err != nil {
				return "", err
			}
		}
		children, _ := json.Marshal(item.Titles)
		workstreamCues := marshalStringArray(item.WorkstreamCues)
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO report_project_occurrences (
				project_id, report_id, report_date, observed_title, child_topics_json, workstream_cues_json,
				source_type, source_weight
			) VALUES ($1, $2, $3::date, $4, $5::jsonb, $6::jsonb, $7, $8)
			ON CONFLICT (project_id, report_id) DO UPDATE SET
				report_date = EXCLUDED.report_date,
				observed_title = EXCLUDED.observed_title,
				child_topics_json = EXCLUDED.child_topics_json,
				workstream_cues_json = EXCLUDED.workstream_cues_json,
				source_type = EXCLUDED.source_type,
				source_weight = EXCLUDED.source_weight`, item.ID, job.ReportID, job.ReportDate,
			item.CanonicalName, string(children), string(workstreamCues), input.SourceType, input.SourceWeight); err != nil {
			return "", err
		}
	}
	memoryPayload, err := currentMemoryPayload(ctx, tx, job.UserID, job.ReportID)
	if err != nil {
		return "", err
	}
	duration := finishedAt.Sub(startedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	var snapshotID string
	err = tx.QueryRowContext(ctx, `
		INSERT INTO report_project_memory_snapshots (
			user_id, report_id, report_date, source_fingerprint, resolver_version,
			model_id, input_json, proposal_json, project_memory_json,
			input_token_estimate, output_token_estimate, external_task_id, duration_ms
		) VALUES ($1, $2, $3::date, $4, $5, NULLIF($6, ''), $7::jsonb, $8::jsonb, $9::jsonb, $10, $11, NULLIF($12, ''), $13)
		ON CONFLICT (user_id, source_fingerprint, resolver_version) DO UPDATE SET
			proposal_json = EXCLUDED.proposal_json,
			project_memory_json = EXCLUDED.project_memory_json
		RETURNING id::text`, job.UserID, job.ReportID, job.ReportDate, claimed, ResolverVersion,
		modelID, string(inputPayload), string(proposalPayload), string(memoryPayload),
		inputTokens, outputTokens, taskID, duration).Scan(&snapshotID)
	if err != nil {
		return "", err
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET
			status = CASE WHEN desired_source_fingerprint = claimed_source_fingerprint THEN 'succeeded' ELSE 'pending' END,
			due_at = CASE WHEN desired_source_fingerprint = claimed_source_fingerprint THEN due_at ELSE $4 END,
			proposal_json = $3::jsonb, snapshot_id = $5,
			output_token_estimate = $6, model_id = NULLIF($7, ''), resolver_version = $8,
			claimed_source_fingerprint = NULL, external_task_id = NULL,
			lease_owner = NULL, lease_until = NULL, last_error = NULL,
			finished_at = $9, updated_at = now()
		WHERE user_id = $1 AND report_date = $2::date`, job.UserID, job.ReportDate,
		string(proposalPayload), nextNightlyWindow(finishedAt), snapshotID, outputTokens,
		modelID, ResolverVersion, finishedAt); err != nil {
		return "", err
	}
	return snapshotID, tx.Commit()
}

func derivedProjectKeys(value string) []string {
	result := make([]string, 0, 2)
	for _, match := range literalProjectKey.FindAllString(value, -1) {
		match = strings.Trim(match, "._-")
		hasLetter, hasDigit := false, false
		for _, current := range match {
			hasLetter = hasLetter || (current >= 'A' && current <= 'Z') || (current >= 'a' && current <= 'z')
			hasDigit = hasDigit || (current >= '0' && current <= '9')
		}
		if hasLetter && hasDigit && validProjectName(match) {
			result = appendUnique(result, match)
		}
	}
	return result
}

func marshalStringArray(values []string) []byte {
	if len(values) == 0 {
		return []byte("[]")
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return []byte("[]")
	}
	return payload
}

func upsertProjectWorkspaceLink(
	ctx context.Context, tx *sql.Tx, job queuedJob, input ConsolidationInput,
	projectID, workspaceID, themeRef string, confidence float64,
) error {
	if strings.TrimSpace(workspaceID) == "" || confidence < 0.6 {
		return nil
	}
	var ownerMatches bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM report_workspaces WHERE id = $1 AND user_id = $2)`,
		workspaceID, job.UserID).Scan(&ownerMatches); err != nil || !ownerMatches {
		return err
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO report_project_workspace_link_evidence (
			project_id, workspace_id, report_id, theme_ref, report_date, confidence, source_weight
		) VALUES ($1, $2, $3, $4, $5::date, $6, $7)
		ON CONFLICT (project_id, workspace_id, report_id, theme_ref) DO NOTHING`,
		projectID, workspaceID, job.ReportID, themeRef, job.ReportDate, confidence, input.SourceWeight)
	if err != nil {
		return err
	}
	delta := int64(0)
	if affected, affectedErr := result.RowsAffected(); affectedErr == nil {
		delta = affected
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO report_project_workspace_links (
			project_id, workspace_id, confidence, source_weight, evidence_count,
			first_seen_on, last_seen_on, resolver_version
		) VALUES ($1, $2, $3, $4, GREATEST($5, 1), $6::date, $6::date, $7)
		ON CONFLICT (project_id, workspace_id) DO UPDATE SET
			confidence = GREATEST(report_project_workspace_links.confidence, EXCLUDED.confidence),
			source_weight = GREATEST(report_project_workspace_links.source_weight, EXCLUDED.source_weight),
			evidence_count = report_project_workspace_links.evidence_count + $5,
			first_seen_on = LEAST(report_project_workspace_links.first_seen_on, EXCLUDED.first_seen_on),
			last_seen_on = GREATEST(report_project_workspace_links.last_seen_on, EXCLUDED.last_seen_on),
			resolver_version = EXCLUDED.resolver_version, updated_at = now()`,
		projectID, workspaceID, confidence, input.SourceWeight, delta, job.ReportDate, ResolverVersion)
	return err
}

func currentMemoryPayload(ctx context.Context, tx *sql.Tx, userID, currentReportID string) ([]byte, error) {
	rows, err := tx.QueryContext(ctx, `
	SELECT p.id::text, p.canonical_name, p.status, p.first_seen_on::text, p.last_seen_on::text,
		       COALESCE(array_agg(a.alias ORDER BY a.source_weight DESC, a.source_report_date DESC)
		           FILTER (WHERE a.alias IS NOT NULL), '{}'),
		       COALESCE((
		           SELECT array_agg(recent_cue.cue)
		           FROM (
		               SELECT cue.value AS cue, max(occurrence.report_date) AS last_seen
		               FROM report_project_occurrences occurrence
		               CROSS JOIN LATERAL jsonb_array_elements_text(occurrence.workstream_cues_json) cue(value)
		               WHERE occurrence.project_id = p.id
		               GROUP BY cue.value
		               ORDER BY last_seen DESC, cue.value
		               LIMIT $3
		           ) recent_cue
		       ), '{}')
		FROM report_projects p
		LEFT JOIN report_project_aliases a ON a.project_id = p.id
		WHERE p.user_id = $1
		  AND EXISTS (
			SELECT 1 FROM report_project_occurrences accepted
			WHERE accepted.project_id = p.id
			  AND (
				accepted.report_id = $2
				OR accepted.report_id IN (
					SELECT snapshot.report_id
					FROM report_project_memory_snapshots snapshot
					WHERE snapshot.user_id = $1
				)
			  )
		  )
		GROUP BY p.id ORDER BY p.last_seen_on DESC, p.id`, userID, currentReportID, maxWorkstreamCues)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type project struct {
		ProjectRef     string   `json:"project_ref"`
		CanonicalName  string   `json:"canonical_name"`
		Status         string   `json:"status"`
		FirstSeenOn    string   `json:"first_seen_on"`
		LastSeenOn     string   `json:"last_seen_on"`
		Aliases        []string `json:"aliases"`
		WorkstreamCues []string `json:"workstream_cues,omitempty"`
	}
	projects := make([]project, 0)
	for rows.Next() {
		var item project
		var aliases, workstreamCues pq.StringArray
		if err := rows.Scan(&item.ProjectRef, &item.CanonicalName, &item.Status, &item.FirstSeenOn, &item.LastSeenOn, &aliases, &workstreamCues); err != nil {
			return nil, err
		}
		item.Aliases = append([]string(nil), aliases...)
		item.WorkstreamCues = append([]string(nil), workstreamCues...)
		projects = append(projects, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{"schema_version": "project-memory-snapshot/v1", "projects": projects})
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
