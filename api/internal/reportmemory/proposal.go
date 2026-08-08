package reportmemory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

const (
	proposalSchema        = "project-memory-maintenance/v2"
	projectNameRuneLimit  = 48
	maxProposalOperations = 64
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
	// Keep the envelope forward-compatible while decoding operations strictly.
	var envelope struct {
		SchemaVersion string              `json:"schema_version"`
		Operations    json.RawMessage     `json:"operations"`
		Rejected      []RejectedOperation `json:"rejected_operations,omitempty"`
	}
	if err := json.Unmarshal([]byte(clean), &envelope); err != nil {
		return MemoryProposal{}, nil, estimate, fmt.Errorf("decode memory proposal: %w", err)
	}
	proposal := MemoryProposal{SchemaVersion: envelope.SchemaVersion, Operations: []MemoryOperation{}, Rejected: envelope.Rejected}
	if len(envelope.Operations) == 0 || string(envelope.Operations) == "null" {
		envelope.Operations = json.RawMessage("[]")
	}
	decoder := json.NewDecoder(strings.NewReader(string(envelope.Operations)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&proposal.Operations); err != nil {
		return MemoryProposal{}, nil, estimate, fmt.Errorf("decode memory operations: %w", err)
	}
	if proposal.SchemaVersion != proposalSchema {
		return MemoryProposal{}, nil, estimate, errors.New("memory proposal schema is invalid")
	}
	if len(proposal.Operations) > maxProposalOperations {
		return MemoryProposal{}, nil, estimate, errors.New("memory proposal has too many operations")
	}
	inputThemes := make(map[string]InputTheme, len(input.CurrentThemes))
	inputEvidence := make(map[string]InputTheme, len(input.CurrentThemes))
	for _, theme := range input.CurrentThemes {
		inputThemes[theme.ThemeRef] = theme
		inputEvidence[theme.EvidenceRef] = theme
	}
	candidates := make(map[string]InputProject, len(input.CandidateProjects))
	for _, project := range input.CandidateProjects {
		candidates[project.ProjectRef] = project
	}
	seenIDs := make(map[string]bool, len(proposal.Operations))
	knownTempRefs := make(map[string]string)
	accepted := make([]MemoryOperation, 0, len(proposal.Operations))
	for index := range proposal.Operations {
		operation := proposal.Operations[index]
		normalizeOperation(&operation)
		reason := validateOperation(operation, inputThemes, inputEvidence, candidates, seenIDs, knownTempRefs)
		if reason != "" {
			proposal.Rejected = append(proposal.Rejected, RejectedOperation{OperationID: operation.OperationID, Reason: reason})
			continue
		}
		seenIDs[operation.OperationID] = true
		if operation.TempRef != "" {
			knownTempRefs[operation.TempRef] = operation.OperationID
		}
		accepted = append(accepted, operation)
	}
	proposal.Operations = accepted
	payload, err := json.Marshal(proposal)
	return proposal, payload, estimate, err
}

func normalizeOperation(operation *MemoryOperation) {
	operation.OperationID = strings.TrimSpace(operation.OperationID)
	operation.Operation = strings.TrimSpace(operation.Operation)
	operation.ThemeRef = strings.TrimSpace(operation.ThemeRef)
	operation.ProjectRef = strings.TrimSpace(operation.ProjectRef)
	operation.TempRef = strings.TrimSpace(operation.TempRef)
	operation.CanonicalName = limitRunes(operation.CanonicalName, titleRuneLimit)
	operation.SignalType = strings.TrimSpace(operation.SignalType)
	operation.Value = limitRunes(operation.Value, titleRuneLimit)
	operation.WorkspaceRef = strings.TrimSpace(operation.WorkspaceRef)
	operation.Reason = limitRunes(operation.Reason, 240)
	normalizedEvidence := make([]string, 0, len(operation.EvidenceRefs))
	for _, ref := range operation.EvidenceRefs {
		if ref = strings.TrimSpace(ref); ref != "" {
			normalizedEvidence = appendUnique(normalizedEvidence, ref)
		}
	}
	operation.EvidenceRefs = normalizedEvidence
}

func validateOperation(operation MemoryOperation, themes, evidence map[string]InputTheme, candidates map[string]InputProject, seenIDs map[string]bool, tempRefs map[string]string) string {
	if operation.OperationID == "" || seenIDs[operation.OperationID] {
		return "operation_id is missing or duplicated"
	}
	if operation.Confidence < 0 || operation.Confidence > 1 {
		return "confidence is invalid"
	}
	for _, dependency := range operation.DependsOn {
		if !seenIDs[strings.TrimSpace(dependency)] {
			return "depends_on references an unavailable operation"
		}
	}
	if operation.ThemeRef != "" {
		if _, ok := themes[operation.ThemeRef]; !ok {
			return "theme_ref is unavailable"
		}
	}
	if len(operation.EvidenceRefs) == 0 {
		return "evidence_refs are required"
	}
	for _, ref := range operation.EvidenceRefs {
		if _, ok := evidence[ref]; !ok {
			return "evidence_ref is unavailable"
		}
	}
	if operation.ThemeRef != "" && !containsString(operation.EvidenceRefs, themes[operation.ThemeRef].EvidenceRef) {
		return "theme evidence_ref is required"
	}
	projectAvailable := func() bool {
		if _, ok := candidates[operation.ProjectRef]; ok {
			return true
		}
		_, ok := tempRefs[operation.ProjectRef]
		return ok
	}
	if creatorID, usesTempRef := tempRefs[operation.ProjectRef]; usesTempRef {
		declared := false
		for _, dependency := range operation.DependsOn {
			declared = declared || strings.TrimSpace(dependency) == creatorID
		}
		if !declared {
			return "operation using temp_ref must depend on its creator"
		}
	}
	switch operation.Operation {
	case "create_project":
		if operation.ThemeRef == "" || operation.TempRef == "" || !validProjectName(operation.CanonicalName) {
			return "create_project requires theme_ref, temp_ref, and a valid canonical_name"
		}
		if _, exists := tempRefs[operation.TempRef]; exists {
			return "temp_ref is duplicated"
		}
	case "link_existing":
		if operation.ThemeRef == "" || !projectAvailable() || operation.Confidence < highConfidenceScore {
			return "link_existing requires a theme, available project, and high confidence"
		}
	case "upsert_signal", "retire_signal":
		if operation.ThemeRef == "" || !projectAvailable() || (operation.SignalType != "alias" && operation.SignalType != "workstream_cue") || !validProjectName(operation.Value) {
			return "signal operation is invalid"
		}
	case "link_workspace", "unlink_workspace":
		if !projectAvailable() || operation.WorkspaceRef == "" || operation.ThemeRef == "" {
			return "workspace operation is invalid"
		}
		if !containsString(themes[operation.ThemeRef].WorkspaceRefs, operation.WorkspaceRef) {
			return "workspace_ref is not present in the referenced theme evidence"
		}
	case "archive_project":
		if !projectAvailable() {
			return "archive_project requires an available project"
		}
	case "noop", "unresolved":
		if operation.ThemeRef == "" {
			return "noop/unresolved requires theme_ref"
		}
	default:
		return "operation is unsupported"
	}
	return ""
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
	var desiredWatermark, claimedWatermark int64
	if err := tx.QueryRowContext(ctx, `
		SELECT desired_source_fingerprint, COALESCE(claimed_source_fingerprint, ''), status,
		       desired_evidence_watermark, COALESCE(claimed_evidence_watermark, 0)
		FROM report_project_memory_jobs
		WHERE user_id = $1
		FOR UPDATE`, job.UserID).Scan(&desired, &claimed, &status, &desiredWatermark, &claimedWatermark); err != nil {
		return "", err
	}
	if status != "running" || claimed == "" || claimed != job.ClaimedSourceFingerprint {
		return "", errors.New("project memory job changed before proposal apply")
	}
	if desired != claimed || desiredWatermark != claimedWatermark {
		_, err := tx.ExecContext(ctx, `
			UPDATE report_project_memory_jobs SET
				status = 'pending', due_at = $2, claimed_source_fingerprint = NULL,
				claimed_evidence_watermark = NULL,
				external_task_id = NULL, lease_owner = NULL, lease_until = NULL,
				last_error = NULL, finished_at = $3, updated_at = now()
			WHERE user_id = $1`, job.UserID, nextNightlyWindow(finishedAt), finishedAt)
		if err != nil {
			return "", err
		}
		return "", tx.Commit()
	}
	themes := make(map[string]InputTheme, len(input.CurrentThemes))
	for _, theme := range input.CurrentThemes {
		themes[theme.ThemeRef] = theme
	}
	type appliedProject struct {
		ID, CanonicalName    string
		ReportID, ReportDate string
		SourceType           string
		SourceWeight         float64
		Titles               []string
		Aliases              []string
		WorkstreamCues       []string
	}
	applied := map[string]*appliedProject{}
	resolvedRefs := make(map[string]string)
	for _, operation := range proposal.Operations {
		projectID := operation.ProjectRef
		if resolvedRefs[projectID] != "" {
			projectID = resolvedRefs[projectID]
		}
		canonicalName := operation.CanonicalName
		theme := themes[operation.ThemeRef]
		switch operation.Operation {
		case "create_project":
			normalized := normalizeName(canonicalName)
			projectID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(job.UserID+"\x00project-memory/v2\x00"+normalized)).String()
			err := tx.QueryRowContext(ctx, `
				INSERT INTO report_projects (
					id, user_id, canonical_name, normalized_name,
					canonical_source_type, canonical_source_weight, first_seen_on, last_seen_on,
					memory_schema_version
				) VALUES ($1, $2, $3, $4, $5, $6, $7::date, $7::date, 'project-memory/v2')
				ON CONFLICT (user_id, normalized_name, memory_schema_version) DO UPDATE SET
					last_seen_on = GREATEST(report_projects.last_seen_on, EXCLUDED.last_seen_on),
					updated_at = now()
				RETURNING id::text, canonical_name`, projectID, job.UserID, canonicalName, normalized,
				theme.SourceType, theme.SourceWeight, theme.ReportDate).Scan(&projectID, &canonicalName)
			if err != nil {
				return "", err
			}
			resolvedRefs[operation.TempRef] = projectID
		case "link_existing":
			if err := tx.QueryRowContext(ctx, `
				UPDATE report_projects SET last_seen_on = GREATEST(last_seen_on, $3::date), updated_at = now()
				WHERE id = $1 AND user_id = $2 RETURNING canonical_name`, projectID, job.UserID, theme.ReportDate).
				Scan(&canonicalName); err != nil {
				return "", err
			}
		case "upsert_signal":
			if err := upsertProjectSignal(ctx, tx, job, input, projectID, operation); err != nil {
				return "", err
			}
			continue
		case "retire_signal":
			if err := retireProjectSignal(ctx, tx, job.UserID, projectID, operation); err != nil {
				return "", err
			}
			continue
		case "link_workspace":
			if err := upsertProjectWorkspaceLink(ctx, tx, job.UserID, theme, projectID, operation.WorkspaceRef, operation.Confidence); err != nil {
				return "", err
			}
			continue
		case "unlink_workspace":
			if err := retireProjectWorkspaceLink(ctx, tx, job.UserID, projectID, operation.WorkspaceRef); err != nil {
				return "", err
			}
			continue
		case "archive_project":
			if _, err := tx.ExecContext(ctx, `
				UPDATE report_projects SET status = 'ended', updated_at = now()
				WHERE id = $1 AND user_id = $2 AND memory_schema_version = 'project-memory/v2'
				  AND canonical_source_weight < 0.95`, projectID, job.UserID); err != nil {
				return "", err
			}
			continue
		default:
			continue
		}
		appliedKey := projectID + "\x00" + theme.ReportRef
		item := applied[appliedKey]
		if item == nil {
			item = &appliedProject{
				ID: projectID, CanonicalName: canonicalName,
				ReportID: theme.ReportRef, ReportDate: theme.ReportDate,
				SourceType: theme.SourceType, SourceWeight: theme.SourceWeight,
			}
			applied[appliedKey] = item
		}
		themeTitle := themes[operation.ThemeRef].Title
		item.Titles = appendUnique(item.Titles, themeTitle)
	}
	for _, item := range applied {
		report := historicalReport{id: item.ReportID, date: item.ReportDate, sourceType: item.SourceType, sourceWeight: item.SourceWeight}
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
		for _, cue := range item.WorkstreamCues {
			if err := upsertProjectSignal(ctx, tx, job, input, item.ID, MemoryOperation{
				OperationID: "derived-cue", Operation: "upsert_signal", SignalType: "workstream_cue",
				Value: cue, Confidence: 0.9,
			}); err != nil {
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
				source_weight = EXCLUDED.source_weight`, item.ID, item.ReportID, item.ReportDate,
			item.CanonicalName, string(children), string(workstreamCues), item.SourceType, item.SourceWeight); err != nil {
			return "", err
		}
	}
	memoryPayload, err := currentMemoryPayload(ctx, tx, job.UserID, input.ReportDate)
	if err != nil {
		return "", err
	}
	duration := finishedAt.Sub(startedAt).Milliseconds()
	if duration < 0 {
		duration = 0
	}
	var snapshotID string
	batchFingerprint := memorySourceFingerprint(claimed, input.ReportRef, input.ReportDate)
	err = tx.QueryRowContext(ctx, `
		INSERT INTO report_project_memory_snapshots (
			user_id, report_id, report_date, source_fingerprint, resolver_version,
			model_id, input_json, proposal_json, project_memory_json,
			input_token_estimate, output_token_estimate, external_task_id, duration_ms,
			evidence_cutoff_date, evidence_watermark
		) VALUES ($1, $2, $3::date, $4, $5, NULLIF($6, ''), $7::jsonb, $8::jsonb, $9::jsonb, $10, $11, NULLIF($12, ''), $13, $3::date, $14)
		ON CONFLICT (user_id, source_fingerprint, resolver_version) DO UPDATE SET
			report_id = EXCLUDED.report_id,
			report_date = EXCLUDED.report_date,
			model_id = EXCLUDED.model_id,
			input_json = EXCLUDED.input_json,
			proposal_json = EXCLUDED.proposal_json,
			project_memory_json = EXCLUDED.project_memory_json,
			input_token_estimate = EXCLUDED.input_token_estimate,
			output_token_estimate = EXCLUDED.output_token_estimate,
			external_task_id = EXCLUDED.external_task_id,
			duration_ms = EXCLUDED.duration_ms,
			evidence_cutoff_date = EXCLUDED.evidence_cutoff_date,
			evidence_watermark = EXCLUDED.evidence_watermark,
			created_at = now()
		RETURNING id::text`, job.UserID, input.ReportRef, input.ReportDate, batchFingerprint, ResolverVersion,
		modelID, string(inputPayload), string(proposalPayload), string(memoryPayload),
		inputTokens, outputTokens, taskID, duration, claimedWatermark).Scan(&snapshotID)
	if err != nil {
		return "", err
	}
	nextStatus := "succeeded"
	nextDirtyFrom := job.ReportDate
	var nextReportDate sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT min(report_date)::text
		FROM daily_reports
		WHERE user_id = $1 AND report_date > $2::date AND report_date <= $3::date
		  AND status IN ('saved', 'submitted')
		  AND NULLIF(BTRIM(COALESCE(NULLIF(submitted_content, ''), content, '')), '') IS NOT NULL`,
		job.UserID, input.ReportDate, job.ReportDate).Scan(&nextReportDate); err != nil {
		return "", err
	}
	if nextReportDate.Valid && strings.TrimSpace(nextReportDate.String) != "" {
		nextStatus = "pending"
		nextDirtyFrom = nextReportDate.String
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE report_project_memory_jobs SET
			status = CASE WHEN desired_source_fingerprint = claimed_source_fingerprint THEN $9 ELSE 'pending' END,
			due_at = CASE WHEN desired_source_fingerprint = claimed_source_fingerprint AND $9 = 'succeeded' THEN due_at ELSE $3 END,
			proposal_json = $2::jsonb, snapshot_id = $4,
			output_token_estimate = $5, model_id = NULLIF($6, ''), resolver_version = $7,
			claimed_source_fingerprint = NULL, external_task_id = NULL,
			claimed_evidence_watermark = NULL,
			lease_owner = NULL, lease_until = NULL, last_error = NULL,
			finished_at = $8, updated_at = now(),
			attempts = CASE WHEN desired_source_fingerprint = claimed_source_fingerprint AND $9 = 'pending' THEN 0 ELSE attempts END,
			dirty_from_date = CASE WHEN desired_source_fingerprint = claimed_source_fingerprint THEN $10::date ELSE dirty_from_date END
		WHERE user_id = $1`, job.UserID, string(proposalPayload), finishedAt, snapshotID, outputTokens,
		modelID, ResolverVersion, finishedAt, nextStatus, nextDirtyFrom); err != nil {
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
	ctx context.Context, tx *sql.Tx, userID string, theme InputTheme,
	projectID, workspaceID string, confidence float64,
) error {
	if strings.TrimSpace(workspaceID) == "" || confidence < 0.6 {
		return nil
	}
	var ownerMatches bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM report_workspaces WHERE id = $1 AND user_id = $2)`,
		workspaceID, userID).Scan(&ownerMatches); err != nil || !ownerMatches {
		if err != nil {
			return err
		}
		return errors.New("workspace owner does not match")
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO report_project_workspace_link_evidence (
			project_id, workspace_id, report_id, theme_ref, report_date, confidence, source_weight
		) VALUES ($1, $2, $3, $4, $5::date, $6, $7)
		ON CONFLICT (project_id, workspace_id, report_id, theme_ref) DO NOTHING`,
		projectID, workspaceID, theme.ReportRef, theme.ThemeRef, theme.ReportDate, confidence, theme.SourceWeight)
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
			status = 'active',
			confidence = GREATEST(report_project_workspace_links.confidence, EXCLUDED.confidence),
			source_weight = GREATEST(report_project_workspace_links.source_weight, EXCLUDED.source_weight),
			evidence_count = report_project_workspace_links.evidence_count + $5,
			first_seen_on = LEAST(report_project_workspace_links.first_seen_on, EXCLUDED.first_seen_on),
			last_seen_on = GREATEST(report_project_workspace_links.last_seen_on, EXCLUDED.last_seen_on),
			resolver_version = EXCLUDED.resolver_version, updated_at = now()`,
		projectID, workspaceID, confidence, theme.SourceWeight, delta, theme.ReportDate, ResolverVersion)
	return err
}

func signalAuthority(sourceType string) string {
	switch sourceType {
	case sourceHumanEdited:
		return "human_edited"
	case sourceManualFinal:
		return "manual_report"
	case "explicit_saved":
		return "explicit_saved"
	default:
		return "ai_inferred"
	}
}

type operationEvidenceMetadata struct {
	SourceType   string
	SourceWeight float64
	FirstDate    string
	LastDate     string
}

func evidenceMetadata(input ConsolidationInput, operation MemoryOperation) (operationEvidenceMetadata, error) {
	byRef := make(map[string]InputTheme, len(input.CurrentThemes))
	for _, theme := range input.CurrentThemes {
		byRef[theme.EvidenceRef] = theme
	}
	var metadata operationEvidenceMetadata
	if operation.ThemeRef != "" {
		for _, theme := range input.CurrentThemes {
			if theme.ThemeRef == operation.ThemeRef {
				metadata = operationEvidenceMetadata{
					SourceType: theme.SourceType, SourceWeight: theme.SourceWeight,
					FirstDate: theme.ReportDate, LastDate: theme.ReportDate,
				}
				break
			}
		}
	}
	for _, ref := range operation.EvidenceRefs {
		theme, ok := byRef[ref]
		if !ok {
			return operationEvidenceMetadata{}, errors.New("operation evidence is unavailable")
		}
		if metadata.FirstDate == "" || theme.ReportDate < metadata.FirstDate {
			metadata.FirstDate = theme.ReportDate
		}
		if theme.ReportDate > metadata.LastDate {
			metadata.LastDate = theme.ReportDate
		}
		if operation.ThemeRef == "" && theme.SourceWeight > metadata.SourceWeight {
			metadata.SourceType, metadata.SourceWeight = theme.SourceType, theme.SourceWeight
		}
	}
	if metadata.FirstDate == "" {
		return operationEvidenceMetadata{}, errors.New("operation evidence is required")
	}
	return metadata, nil
}

func upsertProjectSignal(ctx context.Context, tx *sql.Tx, job queuedJob, input ConsolidationInput, projectID string, operation MemoryOperation) error {
	if tx == nil || !validProjectName(operation.Value) || (operation.SignalType != "alias" && operation.SignalType != "workstream_cue") {
		return nil
	}
	var ownerMatches bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM report_projects WHERE id = $1 AND user_id = $2)`, projectID, job.UserID).Scan(&ownerMatches); err != nil || !ownerMatches {
		if err != nil {
			return err
		}
		return errors.New("project signal owner does not match")
	}
	metadata, err := evidenceMetadata(input, operation)
	if err != nil {
		return err
	}
	authority := signalAuthority(metadata.SourceType)
	_, err = tx.ExecContext(ctx, `
		INSERT INTO report_project_signals (
			project_id, signal_type, normalized_value, display_value, authority,
			confidence, evidence_count, first_seen_on, last_seen_on, status, last_agent_run_id
		) VALUES ($1, $2, $3, $4, $5, $6, 1, $7::date, $8::date, 'active', NULLIF($9, ''))
		ON CONFLICT (project_id, signal_type, normalized_value) DO UPDATE SET
			display_value = CASE
				WHEN report_project_signals.authority IN ('human_edited', 'manual_report')
				 AND EXCLUDED.authority NOT IN ('human_edited', 'manual_report')
				THEN report_project_signals.display_value ELSE EXCLUDED.display_value END,
			authority = CASE
				WHEN report_project_signals.authority IN ('human_edited', 'manual_report')
				 AND EXCLUDED.authority NOT IN ('human_edited', 'manual_report')
				THEN report_project_signals.authority ELSE EXCLUDED.authority END,
			confidence = GREATEST(report_project_signals.confidence, EXCLUDED.confidence),
			evidence_count = report_project_signals.evidence_count + 1,
			last_seen_on = GREATEST(report_project_signals.last_seen_on, EXCLUDED.last_seen_on),
			status = 'active', last_agent_run_id = EXCLUDED.last_agent_run_id, updated_at = now()`,
		projectID, operation.SignalType, normalizeName(operation.Value), operation.Value, authority,
		operation.Confidence, metadata.FirstDate, metadata.LastDate, job.ExternalTaskID)
	return err
}

func retireProjectSignal(ctx context.Context, tx *sql.Tx, userID, projectID string, operation MemoryOperation) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE report_project_signals signal SET status = 'retired', updated_at = now()
		FROM report_projects project
		WHERE signal.project_id = project.id AND project.id = $1 AND project.user_id = $2
		  AND signal.signal_type = $3 AND signal.normalized_value = $4
		  AND signal.authority IN ('ai_inferred', 'machine')`,
		projectID, userID, operation.SignalType, normalizeName(operation.Value))
	return err
}

func retireProjectWorkspaceLink(ctx context.Context, tx *sql.Tx, userID, projectID, workspaceID string) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE report_project_workspace_links link SET status = 'retired', updated_at = now()
		FROM report_projects project
		WHERE link.project_id = project.id AND project.id = $1 AND project.user_id = $2
		  AND link.workspace_id = $3 AND link.source_weight < 0.95`, projectID, userID, workspaceID)
	return err
}

func currentMemoryPayload(ctx context.Context, tx *sql.Tx, userID, evidenceCutoffDate string) ([]byte, error) {
	rows, err := tx.QueryContext(ctx, `
	SELECT p.id::text, p.canonical_name, p.status, p.first_seen_on::text, p.last_seen_on::text,
		       COALESCE((SELECT array_agg(alias.display_value) FROM (
		           SELECT signal.display_value FROM report_project_signals signal
		           WHERE signal.project_id = p.id AND signal.signal_type = 'alias' AND signal.status = 'active'
		           ORDER BY signal.confidence DESC, signal.last_seen_on DESC LIMIT 5) alias), '{}'),
		       COALESCE((SELECT array_agg(cue.display_value) FROM (
		           SELECT signal.display_value FROM report_project_signals signal
		           WHERE signal.project_id = p.id AND signal.signal_type = 'workstream_cue' AND signal.status = 'active'
		           ORDER BY signal.confidence DESC, signal.last_seen_on DESC LIMIT 8) cue), '{}'),
		       COALESCE((SELECT array_agg(link.workspace_id::text ORDER BY link.confidence DESC, link.last_seen_on DESC)
		           FROM report_project_workspace_links link
		           WHERE link.project_id = p.id AND link.status = 'active'), '{}')
		FROM report_projects p
		WHERE p.user_id = $1 AND p.memory_schema_version = 'project-memory/v2' AND p.status <> 'ended'
		  AND EXISTS (
			SELECT 1 FROM report_project_occurrences accepted
			WHERE accepted.project_id = p.id
			  AND accepted.report_date <= $2::date
		  )
		ORDER BY p.last_seen_on DESC, p.id`, userID, evidenceCutoffDate)
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
		WorkspaceRefs  []string `json:"workspace_refs,omitempty"`
	}
	projects := make([]project, 0)
	for rows.Next() {
		var item project
		var aliases, workstreamCues, workspaceRefs pq.StringArray
		if err := rows.Scan(&item.ProjectRef, &item.CanonicalName, &item.Status, &item.FirstSeenOn, &item.LastSeenOn, &aliases, &workstreamCues, &workspaceRefs); err != nil {
			return nil, err
		}
		item.Aliases = append([]string(nil), aliases...)
		item.WorkstreamCues = append([]string(nil), workstreamCues...)
		item.WorkspaceRefs = append([]string(nil), workspaceRefs...)
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
