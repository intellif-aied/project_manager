package handler

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/reportbrief"
	"github.com/aidashboard/api/internal/reportcontext"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/model"
	"github.com/lib/pq"
)

// reportAIRun is the validated AI run after aiRunGuard.
type reportAIRun struct {
	ID                    string
	BusinessType          string
	AgentID               string
	ModelID               *string
	Status                string
	Stage                 string
	InputRef              map[string]any
	OutputRef             map[string]any
	ContextRepresentation string
	ReportAgentSource     string
	CreatedAt             time.Time
}

type writeReportResultArgs struct {
	RunID      string `json:"run_id"`
	Content    string `json:"content"`
	Summary    string `json:"summary,omitempty"`
	BriefHash  string `json:"brief_hash,omitempty"`
	FormatMode string `json:"format_mode,omitempty"`
}

type writeReportFailureArgs struct {
	RunID        string `json:"run_id"`
	ErrorCode    string `json:"error_code,omitempty"`
	ErrorMessage string `json:"error_message"`
}

type frozenReportSourceRefs struct {
	PersonalDailyIDs  []string
	PersonalWeeklyIDs []string
	TeamDailyIDs      []string
	TeamWeeklyIDs     []string
	TaskIDs           []string
}

func (h *ReportMCPHandler) toolWriteReportResult(r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args writeReportResultArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	runID, err := resolveReportRunID(r, args.RunID)
	if err != nil {
		return nil, err
	}
	args.RunID = runID
	run, err := h.aiRunGuard(r, runID, u.ID)
	if err != nil {
		return nil, err
	}
	reportType, date, ws, we, target, err := resolveRunReportIdentity(u, run)
	if err != nil {
		return nil, err
	}
	briefSchemaVersion := ""
	acceptedBriefHash := ""
	degradedReason := ""
	degradedContentReplaced := false
	content, normalizedSummary, err := prepareReportResultForRun(*run, reportType, args)
	if err != nil {
		return nil, err
	}
	if h.briefEnabled && reportType == reportTypePersonalDaily && reportBriefRequiredForRun(*run) {
		if h.reportBrief == nil {
			return nil, errMCPInternal
		}
		if strings.TrimSpace(args.BriefHash) == "" {
			degradedReason, err = h.reportBrief.DegradedWriteReason(r.Context(), u.ID, args.RunID)
			if err != nil {
				return nil, mapReportBriefError(err)
			}
			if !reportbrief.ReaderFacingTextSafe(normalizedSummary) || !reportbrief.ReaderFacingTextSafe(content) {
				content, normalizedSummary, err = prepareReportResultContent(
					reportType,
					run.ContextRepresentation,
					"1. 已根据本期工作记录生成日报，请检查并补充。",
					"### 工作记录\n\n本期工作内容已完成整理，请结合实际情况检查并补充。",
				)
				if err != nil {
					return nil, err
				}
				degradedContentReplaced = true
			}
		} else {
			storedBrief, briefErr := h.reportBrief.ValidateForWrite(
				r.Context(), u.ID, args.RunID, strings.TrimSpace(args.BriefHash), normalizedSummary, content,
			)
			if briefErr != nil {
				return nil, mapReportBriefError(briefErr)
			}
			briefSchemaVersion = storedBrief.Payload.SchemaVersion
			acceptedBriefHash = storedBrief.BriefHash
		}
	}
	if content == "" {
		return nil, mcpErr("INVALID_ARGUMENT", "content is required")
	}
	resultHash := reportResultHash(content)
	if idempotent, reportID, err := validateReportWriteAllowed(run, reportType, date, ws, we, target, resultHash); err != nil {
		return nil, err
	} else if idempotent {
		return mcpTextResult(map[string]any{
			"status":          "saved",
			"report_id":       reportID,
			"report_type":     reportType,
			"agent_run_id":    run.ID,
			"already_written": true,
		}), nil
	}
	sourceRefs, err := h.loadFrozenReportSourceRefs(r.Context(), u.ID, run)
	if err != nil {
		return nil, errMCPInternal
	}

	ctx := r.Context()
	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errMCPInternal
	}
	defer tx.Rollback()
	if selectionID := strings.TrimSpace(stringFromAny(run.InputRef["report_source_selection_id"])); selectionID != "" {
		if h.reportSource == nil {
			return nil, mcpErr("REPORT_SOURCE_MISMATCH", "report source selection is unavailable")
		}
		period, periodErr := reportsource.ReportPeriod(reportType, date, ws, we)
		if periodErr != nil {
			return nil, mcpErr("REPORT_SOURCE_MISMATCH", "report source period is invalid")
		}
		if err := h.reportSource.ValidateAttachedSelectionTx(
			ctx, tx, u.ID, selectionID, run.ID, reportType, period,
		); err != nil {
			switch {
			case errors.Is(err, reportsource.ErrSourceIncomplete):
				return nil, mcpErr("REPORT_SOURCE_INCOMPLETE", "the report source must be read completely in the run's required mode before writing the report")
			case errors.Is(err, reportsource.ErrSourceUnavailable):
				return nil, mcpErr("CONTENT_CLEARED", "report source content is no longer available")
			case errors.Is(err, reportsource.ErrDigestVersionMismatch):
				return nil, mcpErr("REPORT_SOURCE_DIGEST_VERSION_MISMATCH", "report source digest version does not match this run")
			case errors.Is(err, reportsource.ErrDigestCorrupt):
				return nil, mcpErr("REPORT_SOURCE_DIGEST_FAILED", "report source digest integrity check failed")
			case errors.Is(err, reportsource.ErrSelectionMismatch), errors.Is(err, reportsource.ErrSelectionNotFound):
				return nil, mcpErr("REPORT_SOURCE_MISMATCH", "report source selection does not match this run")
			default:
				return nil, errMCPInternal
			}
		}
	}
	stageResult, err := tx.ExecContext(ctx, `
		UPDATE ai_runs
		SET execution_stage = 'writing_result', stage_updated_at = now()
		WHERE id = $1 AND user_id = $2 AND status = 'running'
		  AND (execution_stage = 'agent_running' OR execution_stage IS NULL)`, run.ID, u.ID)
	if err != nil {
		return nil, errMCPInternal
	}
	if changed, countErr := stageResult.RowsAffected(); countErr != nil || changed != 1 {
		return nil, mcpErr("RUN_NOT_WRITABLE", "run is not in the report writeback stage")
	}

	existing, err := selectReportForUpdate(ctx, tx, reportType, date, ws, we, target)
	if err != nil {
		return nil, errMCPInternal
	}
	if existing != nil && existing.Edited && existing.UpdatedAt.After(run.CreatedAt) {
		msg := "报告已被用户编辑，AI 回写已取消"
		if err := markAIRunFailedTx(ctx, tx, run.ID, u.ID, reportEditConflictCode, msg); err != nil {
			return nil, errMCPInternal
		}
		if err := tx.Commit(); err != nil {
			return nil, errMCPInternal
		}
		return nil, errReportEditConflict
	}

	reportID, err := upsertReportContent(ctx, tx, reportType, date, ws, we, target, content, run, u.ID, sourceRefs)
	if err != nil {
		return nil, errMCPInternal
	}

	outputPayload := map[string]any{
		"report_type":        reportType,
		"report_id":          reportID,
		"date":               date,
		"week_start":         ws,
		"week_end":           we,
		"target":             target,
		"summary":            normalizedSummary,
		"report_result_hash": resultHash,
	}
	if acceptedBriefHash != "" {
		outputPayload["brief_schema_version"] = briefSchemaVersion
		outputPayload["brief_hash"] = acceptedBriefHash
	}
	if degradedReason != "" {
		outputPayload["report_generation_degraded"] = true
		outputPayload["report_generation_degraded_reason"] = degradedReason
		if degradedContentReplaced {
			outputPayload["report_generation_degraded_content_replaced"] = true
		}
	}
	copyReportRunMetadata(outputPayload, run.InputRef)
	outputRef, _ := json.Marshal(outputPayload)
	finalizeResult, err := tx.ExecContext(ctx, `
		UPDATE ai_runs
		SET status = 'succeeded',
		    execution_stage = 'completed',
		    stage_updated_at = now(),
		    failure_stage = NULL,
		    error_code = NULL,
		    business_id = $1,
		    output_ref_json = $2,
		    error_message = NULL,
		    finished_at = now()
		WHERE id = $3 AND user_id = $4 AND execution_stage = 'writing_result'`, reportID, outputRef, run.ID, u.ID)
	if err != nil {
		return nil, errMCPInternal
	}
	if changed, countErr := finalizeResult.RowsAffected(); countErr != nil || changed != 1 {
		return nil, errMCPInternal
	}
	if err := tx.Commit(); err != nil {
		return nil, errMCPInternal
	}

	return mcpTextResult(map[string]any{
		"status":               "saved",
		"report_id":            reportID,
		"report_type":          reportType,
		"agent_run_id":         run.ID,
		"managed_agent_run_id": run.ID,
		"product_status":       "ai_generated",
		"origin":               "ai",
		"updated_by_user":      false,
		"degraded":             degradedReason != "",
	}), nil
}

func (h *ReportMCPHandler) loadFrozenReportSourceRefs(ctx context.Context, userID string, run *reportAIRun) (frozenReportSourceRefs, error) {
	refs := frozenReportSourceRefs{
		PersonalDailyIDs:  []string{},
		PersonalWeeklyIDs: []string{},
		TeamDailyIDs:      []string{},
		TeamWeeklyIDs:     []string{},
		TaskIDs:           []string{},
	}
	if h.reportContext == nil || run == nil || strings.TrimSpace(stringFromAny(run.InputRef["report_context_hash"])) == "" {
		return refs, nil
	}
	stored, err := h.reportContext.Get(ctx, userID, run.ID)
	if err != nil {
		return refs, err
	}
	if expected := strings.TrimSpace(stringFromAny(run.InputRef["report_context_hash"])); expected != stored.Hash {
		return refs, fmt.Errorf("report context hash mismatch")
	}
	// The write path only needs immutable source identities. Decode that narrow
	// contract so Agent-facing Context representations can evolve independently.
	var payload struct {
		SourceReports []struct {
			ID         string `json:"id"`
			ReportType string `json:"report_type"`
		} `json:"source_reports"`
		Tasks []struct {
			ID string `json:"id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(stored.Payload, &payload); err != nil {
		return refs, err
	}
	for _, report := range payload.SourceReports {
		id := strings.TrimSpace(report.ID)
		if id == "" {
			continue
		}
		switch report.ReportType {
		case reportTypePersonalDaily:
			refs.PersonalDailyIDs = appendUniqueString(refs.PersonalDailyIDs, id)
		case reportTypePersonalWeekly:
			refs.PersonalWeeklyIDs = appendUniqueString(refs.PersonalWeeklyIDs, id)
		case reportTypeTeamDaily:
			refs.TeamDailyIDs = appendUniqueString(refs.TeamDailyIDs, id)
		case reportTypeTeamWeekly:
			refs.TeamWeeklyIDs = appendUniqueString(refs.TeamWeeklyIDs, id)
		}
	}
	for _, task := range payload.Tasks {
		if id := strings.TrimSpace(task.ID); id != "" {
			refs.TaskIDs = appendUniqueString(refs.TaskIDs, id)
		}
	}
	return refs, nil
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func copyReportRunMetadata(out map[string]any, input map[string]any) {
	if out == nil || input == nil {
		return
	}
	for _, key := range []string{"trigger_source", "scheduled_trigger_at", "schedule_id", "schedule_name"} {
		if value := strings.TrimSpace(stringFromAny(input[key])); value != "" {
			out[key] = value
		}
	}
}

func (h *ReportMCPHandler) toolWriteReportFailure(r *http.Request, rawArgs json.RawMessage) (any, error) {
	u, err := requireUser(r)
	if err != nil {
		return nil, err
	}
	var args writeReportFailureArgs
	if err := decodeArguments(rawArgs, &args); err != nil {
		return nil, err
	}
	boundRunID, _ := r.Context().Value(reportRunIDKey).(string)
	if strings.TrimSpace(args.RunID) == "" && strings.TrimSpace(boundRunID) == "" {
		return nil, mcpErr("INVALID_ARGUMENT", "run_id is required")
	}
	runID, err := resolveReportRunID(r, args.RunID)
	if err != nil {
		return nil, err
	}
	args.RunID = runID
	run, err := h.aiRunGuard(r, runID, u.ID)
	if err != nil {
		return nil, err
	}
	if err := validateReportFailureAllowed(run); err != nil {
		return nil, err
	}
	errorMessage := strings.TrimSpace(args.ErrorMessage)
	if errorMessage == "" {
		errorMessage = "Agent 生成失败"
	}
	errorCode := strings.TrimSpace(args.ErrorCode)
	if errorCode == "" {
		errorCode = "AGENT_REPORTED_FAILURE"
	}
	if h.briefEnabled && h.reportBrief != nil && isReportQualityRetryExhausted(errorCode, errorMessage) {
		if _, degradedErr := h.reportBrief.DegradedWriteReason(r.Context(), u.ID, args.RunID); degradedErr == nil {
			return nil, mcpErr("REPORT_DEGRADED_RESULT_REQUIRED", "report quality checks cannot fail the run; call write_report_result without brief_hash")
		}
	}
	log.Printf("report Agent failure run_id=%s user_id=%s error_code=%s detail=%q", args.RunID, u.ID, errorCode, errorMessage)
	formatted := "报告生成未完成，请重新生成"
	failureResult, err := h.db.ExecContext(r.Context(), `
		UPDATE ai_runs
		SET status = 'failed',
		    execution_stage = 'completed',
		    stage_updated_at = now(),
		    failure_stage = COALESCE(execution_stage, 'agent_running'),
		    error_code = $2,
		    error_message = $1,
		    finished_at = now()
		WHERE id::text = $3 AND user_id = $4
		  AND status = 'running'
		  AND (execution_stage = 'agent_running' OR execution_stage IS NULL)`,
		formatted, errorCode, strings.TrimSpace(args.RunID), u.ID)
	if err != nil {
		return nil, errMCPInternal
	}
	if changed, countErr := failureResult.RowsAffected(); countErr != nil || changed != 1 {
		return nil, mcpErr("RUN_NOT_WRITABLE", "run is not in the Agent execution stage")
	}
	return mcpTextResult(map[string]any{
		"run_id":    args.RunID,
		"status":    "failed",
		"retryable": true,
	}), nil
}

func isReportQualityRetryExhausted(errorCode, errorMessage string) bool {
	value := strings.ToUpper(strings.TrimSpace(errorCode) + " " + strings.TrimSpace(errorMessage))
	return strings.Contains(value, "BRIEF_RETRY_EXHAUSTED") || strings.Contains(value, "RESULT_RETRY_EXHAUSTED")
}

func resolveRunReportIdentity(u *model.User, run *reportAIRun) (string, string, string, string, reportTarget, error) {
	if run == nil {
		return "", "", "", "", reportTarget{}, mcpErr("RUN_NOT_WRITABLE", "report run is unavailable")
	}
	reportType := strings.TrimSpace(stringFromAny(run.InputRef["report_type"]))
	if err := validateReportType(reportType); err != nil {
		return "", "", "", "", reportTarget{}, mcpErr("RUN_NOT_WRITABLE", "report run type is invalid")
	}
	var period periodArgs
	periodRaw, err := json.Marshal(run.InputRef["period"])
	if err != nil || json.Unmarshal(periodRaw, &period) != nil {
		return "", "", "", "", reportTarget{}, mcpErr("RUN_NOT_WRITABLE", "report run period is invalid")
	}
	date, weekStart, weekEnd, err := resolveReportPeriod(reportType, period)
	if err != nil {
		return "", "", "", "", reportTarget{}, mcpErr("RUN_NOT_WRITABLE", "report run period is invalid")
	}
	var storedTarget reportTarget
	targetRaw, err := json.Marshal(run.InputRef["target"])
	if err != nil || json.Unmarshal(targetRaw, &storedTarget) != nil {
		return "", "", "", "", reportTarget{}, mcpErr("RUN_NOT_WRITABLE", "report run target is invalid")
	}
	target, err := resolveTarget(u, storedTarget, reportType, true)
	if err != nil {
		return "", "", "", "", reportTarget{}, err
	}
	if err := validateRunReportIdentity(run.InputRef, reportType, date, weekStart, weekEnd, target); err != nil {
		return "", "", "", "", reportTarget{}, err
	}
	return reportType, date, weekStart, weekEnd, target, nil
}

// aiRunGuard validates that runID belongs to the current user.
func (h *ReportMCPHandler) aiRunGuard(r *http.Request, runID, userID string) (*reportAIRun, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, mcpErr("INVALID_ARGUMENT", "run_id is required")
	}
	var run reportAIRun
	var modelID sql.NullString
	var createdAt sql.NullTime
	var inputRaw, executionInputRaw, outputRaw []byte
	err := h.db.QueryRowContext(r.Context(), `
		SELECT id::text, business_type, COALESCE(agent_id, ''), model_id, status,
		       COALESCE(execution_stage, ''), input_ref_json, execution_input_json,
		       output_ref_json, created_at
		FROM ai_runs
		WHERE id::text = $1 AND user_id = $2`, runID, userID).
		Scan(&run.ID, &run.BusinessType, &run.AgentID, &modelID, &run.Status, &run.Stage, &inputRaw, &executionInputRaw, &outputRaw, &createdAt)
	if err == sql.ErrNoRows {
		return nil, errRunNotFound
	}
	if err != nil {
		return nil, errMCPInternal
	}
	if modelID.Valid && modelID.String != "" {
		s := modelID.String
		run.ModelID = &s
	}
	if createdAt.Valid {
		run.CreatedAt = createdAt.Time
	}
	_ = json.Unmarshal(inputRaw, &run.InputRef)
	var executionInput map[string]any
	_ = json.Unmarshal(executionInputRaw, &executionInput)
	run.ContextRepresentation = strings.TrimSpace(stringFromAny(executionInput["report_context_representation"]))
	run.ReportAgentSource = strings.TrimSpace(stringFromAny(executionInput["report_agent_source"]))
	_ = json.Unmarshal(outputRaw, &run.OutputRef)
	if run.InputRef == nil {
		run.InputRef = map[string]any{}
	}
	if run.OutputRef == nil {
		run.OutputRef = map[string]any{}
	}
	return &run, nil
}

func reportBriefRequiredForRun(run reportAIRun) bool {
	return strings.TrimSpace(run.ReportAgentSource) != managedAgentSourcePersonal
}

func prepareReportResultForRun(run reportAIRun, reportType string, args writeReportResultArgs) (string, string, error) {
	if strings.TrimSpace(run.ReportAgentSource) != managedAgentSourcePersonal {
		formatMode := strings.TrimSpace(args.FormatMode)
		if formatMode != "" && formatMode != standardReportFormatMode {
			return "", "", mcpErr("INVALID_ARGUMENT", "format_mode must be standard for a system report run")
		}
		return prepareReportResultContent(reportType, run.ContextRepresentation, args.Summary, args.Content)
	}
	content := strings.TrimSpace(strings.ReplaceAll(args.Content, "\r\n", "\n"))
	if content == "" {
		return "", "", mcpErr("INVALID_ARGUMENT", "content is required")
	}
	summary := strings.TrimSpace(strings.ReplaceAll(args.Summary, "\r\n", "\n"))
	if strings.TrimSpace(run.ContextRepresentation) == reportcontext.RepresentationWorkEvidence && summary == "" {
		return "", "", mcpErr("REPORT_SUMMARY_REQUIRED", "summary is required for this report run")
	}
	return content, summary, nil
}

func prepareReportResultContent(reportType, representation, summary, content string) (string, string, error) {
	body := strings.TrimSpace(content)
	if body == "" {
		return "", "", mcpErr("INVALID_ARGUMENT", "content is required")
	}
	if strings.TrimSpace(representation) != reportcontext.RepresentationWorkEvidence {
		return body, summary, nil
	}
	normalizedSummary := normalizeReportSummary(summary)
	if normalizedSummary == "" {
		return "", "", mcpErr("REPORT_SUMMARY_REQUIRED", "summary is required for this report run")
	}
	if normalized, removed := stripLeadingWorkSummary(body); removed {
		body = normalized
		if body == "" {
			return "", "", mcpErr("INVALID_ARGUMENT", "content body is required after the work summary")
		}
	}
	if reportType != reportTypePersonalDaily {
		return "## 工作总结\n\n" + normalizedSummary + "\n\n" + body, normalizedSummary, nil
	}
	body = normalizeReportDetailHeadings(body)
	return "## 工作概览\n\n" + normalizedSummary + "\n\n## 工作详情\n\n" + body, normalizedSummary, nil
}

var reportSummaryItemMarkerPattern = regexp.MustCompile(`(?m)(^|[[:space:]。！？；.!?;]|\\r\\n|\\n|\\r)([1-5])\.[ \t]+`)

func normalizeReportSummary(summary string) string {
	normalized := strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(summary), "\r\n", "\n"), "\r", "\n")
	if items, ok := parseOrderedReportSummary(normalized); ok {
		return formatOrderedReportSummary(items)
	}
	lines := make([]string, 0, 5)
	for _, rawLine := range strings.Split(normalized, "\n") {
		line := strings.Join(strings.Fields(rawLine), " ")
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	items := make([]string, 0, len(lines))
	for _, line := range lines {
		dot := strings.IndexByte(line, '.')
		if dot < 1 {
			return strings.Join(strings.Fields(normalized), " ")
		}
		if _, err := strconv.Atoi(line[:dot]); err != nil {
			return strings.Join(strings.Fields(normalized), " ")
		}
		item := strings.TrimSpace(line[dot+1:])
		if item == "" {
			return strings.Join(strings.Fields(normalized), " ")
		}
		items = append(items, fmt.Sprintf("%d. %s", len(items)+1, item))
	}
	return strings.Join(items, "\n")
}

func parseOrderedReportSummary(summary string) ([]string, bool) {
	matches := reportSummaryItemMarkerPattern.FindAllStringSubmatchIndex(summary, -1)
	if len(matches) == 0 || reportSummaryMarkerStart(summary, matches[0]) != 0 {
		return nil, false
	}
	items := make([]string, 0, len(matches))
	for index, match := range matches {
		number, err := strconv.Atoi(summary[match[4]:match[5]])
		if err != nil || number != index+1 {
			return nil, false
		}
		itemEnd := len(summary)
		if index+1 < len(matches) {
			itemEnd = reportSummaryMarkerStart(summary, matches[index+1])
		}
		item := strings.Join(strings.Fields(summary[match[1]:itemEnd]), " ")
		if item == "" {
			return nil, false
		}
		items = append(items, item)
	}
	return items, true
}

func reportSummaryMarkerStart(summary string, match []int) int {
	if match[2] == match[3] {
		return match[0]
	}
	boundary := summary[match[2]:match[3]]
	if strings.TrimSpace(boundary) == "" || strings.HasPrefix(boundary, `\`) {
		return match[2]
	}
	return match[3]
}

func formatOrderedReportSummary(items []string) string {
	lines := make([]string, 0, len(items))
	for index, item := range items {
		lines = append(lines, fmt.Sprintf("%d. %s", index+1, item))
	}
	return strings.Join(lines, "\n")
}

func stripLeadingWorkSummary(content string) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(content), "\r\n", "\n"), "\n")
	first := -1
	for index, line := range lines {
		if strings.TrimSpace(line) != "" {
			first = index
			break
		}
	}
	if first < 0 {
		return strings.TrimSpace(content), false
	}
	firstHeading := strings.TrimSpace(lines[first])
	if firstHeading == "## 工作详情" {
		return strings.TrimSpace(strings.Join(lines[first+1:], "\n")), true
	}
	if firstHeading != "## 工作总结" && firstHeading != "## 工作概览" {
		return strings.TrimSpace(content), false
	}
	for index := first + 1; index < len(lines); index++ {
		line := strings.TrimSpace(lines[index])
		if line == "## 工作详情" {
			return strings.TrimSpace(strings.Join(lines[index+1:], "\n")), true
		}
		if strings.HasPrefix(line, "## ") && line != "## 工作总结" && line != "## 工作概览" {
			return strings.TrimSpace(strings.Join(lines[index:], "\n")), true
		}
	}
	return "", true
}

func normalizeReportDetailHeadings(content string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(content), "\r\n", "\n"), "\n")
	inFence := false
	for index, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "## ") {
			continue
		}
		indentLength := len(line) - len(strings.TrimLeft(line, " \t"))
		lines[index] = line[:indentLength] + "### " + strings.TrimPrefix(trimmed, "## ")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func reportResultHash(content string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(content)))
	return fmt.Sprintf("%x", sum[:])
}

func validateReportWriteAllowed(run *reportAIRun, reportType, date, ws, we string, target reportTarget, resultHash string) (bool, string, error) {
	if run.BusinessType != reportAgentRunBusinessType {
		return false, "", mcpErr("RUN_NOT_WRITABLE", "run is not a Report Agent run")
	}
	if run.Status == "failed" || run.Status == "timeout" || run.Status == "pending" {
		return false, "", mcpErr("RUN_NOT_WRITABLE", "run status does not allow report write")
	}
	if err := validateRunReportIdentity(run.InputRef, reportType, date, ws, we, target); err != nil {
		return false, "", err
	}
	if run.Status != "succeeded" {
		if run.Status == "running" {
			return false, "", nil
		}
		return false, "", mcpErr("RUN_NOT_WRITABLE", "run status does not allow report write")
	}
	if stringFromAny(run.OutputRef["report_result_hash"]) != resultHash {
		return false, "", mcpErr("REPORT_WRITE_CONFLICT", "report payload hash conflicts with existing result")
	}
	if err := validateRunReportIdentity(run.OutputRef, reportType, date, ws, we, target); err != nil {
		return false, "", err
	}
	reportID := stringFromAny(run.OutputRef["report_id"])
	return true, reportID, nil
}

func validateReportFailureAllowed(run *reportAIRun) error {
	if run.BusinessType != reportAgentRunBusinessType || run.Status == "failed" || run.Status == "timeout" || run.Status == "succeeded" {
		return mcpErr("RUN_NOT_WRITABLE", "run status does not allow failure write")
	}
	return nil
}

func validateRunReportIdentity(ref map[string]any, reportType, date, ws, we string, target reportTarget) error {
	if len(ref) == 0 {
		return nil
	}
	if existing := stringFromAny(ref["report_type"]); existing != "" && existing != reportType {
		return mcpErr("REPORT_WRITE_CONFLICT", "report_type does not match run")
	}
	if existing := stringFromAny(ref["date"]); existing != "" && date != "" && existing != date {
		return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
	}
	if existing := stringFromAny(ref["week_start"]); existing != "" && ws != "" && existing != ws {
		return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
	}
	if existing := stringFromAny(ref["week_end"]); existing != "" && we != "" && existing != we {
		return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
	}
	if periodRaw, ok := ref["period"]; ok {
		if period, ok := stringMapFromAny(periodRaw); ok {
			if existing := period["date"]; existing != "" && date != "" && existing != date {
				return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
			}
			if existing := period["week_start"]; existing != "" && ws != "" && existing != ws {
				return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
			}
			if existing := period["week_end"]; existing != "" && we != "" && existing != we {
				return mcpErr("REPORT_WRITE_CONFLICT", "period does not match run")
			}
		}
	}
	if targetRaw, ok := ref["target"]; ok {
		if existing, ok := stringMapFromAny(targetRaw); ok {
			if value := existing["type"]; value != "" && value != target.Type {
				return mcpErr("REPORT_WRITE_CONFLICT", "target does not match run")
			}
			if value := existing["user_id"]; value != "" && value != target.UserID {
				return mcpErr("REPORT_WRITE_CONFLICT", "target does not match run")
			}
			if value := existing["team_id"]; value != "" && value != target.TeamID {
				return mcpErr("REPORT_WRITE_CONFLICT", "target does not match run")
			}
			if value := existing["department_id"]; value != "" && value != target.DepartmentID {
				return mcpErr("REPORT_WRITE_CONFLICT", "target does not match run")
			}
		}
	}
	return nil
}

func stringMapFromAny(value any) (map[string]string, bool) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, false
	}
	out := map[string]string{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, false
	}
	return out, true
}

func stringFromAny(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// existingReportRow is the SELECT FOR UPDATE result used for the防覆盖 check.
type existingReportRow struct {
	ID        string
	Edited    bool
	UpdatedAt time.Time
}

func selectReportForUpdate(ctx context.Context, tx *sql.Tx, reportType, date, ws, we string, target reportTarget) (*existingReportRow, error) {
	q, args := selectForUpdateQuery(reportType, date, ws, we, target)
	row := tx.QueryRowContext(ctx, q, args...)
	var e existingReportRow
	err := row.Scan(&e.ID, &e.Edited, &e.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func selectForUpdateQuery(reportType, date, ws, we string, target reportTarget) (string, []any) {
	switch reportType {
	case reportTypePersonalDaily:
		return `SELECT id::text, edited, updated_at FROM daily_reports WHERE user_id = $1 AND report_date = $2 FOR UPDATE`, []any{target.UserID, date}
	case reportTypePersonalWeekly:
		return `SELECT id::text, edited, updated_at FROM personal_weekly_reports WHERE user_id = $1 AND week_start = $2 AND week_end = $3 FOR UPDATE`, []any{target.UserID, ws, we}
	case reportTypeTeamDaily:
		return `SELECT id::text, edited, updated_at FROM team_reports WHERE team_id = $1 AND report_date = $2 FOR UPDATE`, []any{target.TeamID, date}
	case reportTypeTeamWeekly:
		return `SELECT id::text, edited, updated_at FROM team_weekly_reports WHERE team_id = $1 AND week_start = $2 AND week_end = $3 FOR UPDATE`, []any{target.TeamID, ws, we}
	case reportTypeDepartmentDaily:
		return `SELECT id::text, edited, updated_at FROM department_reports WHERE department_id=$1 AND report_date=$2 FOR UPDATE`, []any{target.DepartmentID, date}
	case reportTypeDepartmentWeekly:
		return `SELECT id::text, edited, updated_at FROM department_weekly_reports WHERE department_id=$1 AND week_start=$2 AND week_end=$3 FOR UPDATE`, []any{target.DepartmentID, ws, we}
	}
	return "", nil
}

// upsertReportContent writes content + agent metadata into the target table and returns the report ID.
// leaderID is the current user's ID, used for team_reports / team_weekly_reports leader_id column.
func upsertReportContent(ctx context.Context, tx *sql.Tx, reportType, date, ws, we string, target reportTarget, content string, run *reportAIRun, leaderID string, sourceRefs frozenReportSourceRefs) (string, error) {
	switch reportType {
	case reportTypePersonalDaily:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO daily_reports (user_id, report_date, content, edited, generation_mode, managed_agent_run_id, agent_id, model_id, status, saved_at)
			VALUES ($1, $2, $3, false, 'managed_agent', $4, $5, $6, 'saved', now())
			ON CONFLICT (user_id, report_date) DO UPDATE
			SET content = EXCLUDED.content,
			    edited = false,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    status = 'saved',
			    saved_at = now(),
			    updated_at = now()
			RETURNING id::text`, target.UserID, date, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID)).Scan(&reportID)
		return reportID, err
	case reportTypePersonalWeekly:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO personal_weekly_reports (user_id, week_start, week_end, content, status, generation_mode, managed_agent_run_id, agent_id, model_id, edited, saved_at, source_daily_report_ids, source_task_ids)
			VALUES ($1, $2, $3, $4, 'saved', 'managed_agent', $5, $6, $7, false, now(), $8, $9)
			ON CONFLICT (user_id, week_start) DO UPDATE
			SET content = EXCLUDED.content,
			    week_end = EXCLUDED.week_end,
			    status = 'saved',
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    saved_at = now(),
			    source_daily_report_ids = EXCLUDED.source_daily_report_ids,
			    source_task_ids = EXCLUDED.source_task_ids,
			    updated_at = now()
			RETURNING id::text`, target.UserID, ws, we, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID), pq.Array(sourceRefs.PersonalDailyIDs), pq.Array(sourceRefs.TaskIDs)).Scan(&reportID)
		return reportID, err
	case reportTypeTeamDaily:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO team_reports (team_id, leader_id, report_date, content, generation_mode, managed_agent_run_id, agent_id, model_id, edited, status, saved_at, member_report_ids, source_daily_report_ids)
			VALUES ($1, $2, $3, $4, 'managed_agent', $5, $6, $7, false, 'saved', now(), $8, $8)
			ON CONFLICT (team_id, report_date) DO UPDATE
			SET content = EXCLUDED.content,
			    leader_id = EXCLUDED.leader_id,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    status = 'saved',
			    saved_at = now(),
			    member_report_ids = EXCLUDED.member_report_ids,
			    source_daily_report_ids = EXCLUDED.source_daily_report_ids,
			    updated_at = now()
			RETURNING id::text`, target.TeamID, leaderID, date, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID), pq.Array(sourceRefs.PersonalDailyIDs)).Scan(&reportID)
		return reportID, err
	case reportTypeTeamWeekly:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO team_weekly_reports (team_id, leader_id, week_start, week_end, content, generation_mode, managed_agent_run_id, agent_id, model_id, edited, source_personal_weekly_report_ids, source_task_ids)
			VALUES ($1, $2, $3, $4, $5, 'managed_agent', $6, $7, $8, false, $9, $10)
			ON CONFLICT (team_id, week_start) DO UPDATE
			SET content = EXCLUDED.content,
			    leader_id = EXCLUDED.leader_id,
			    week_end = EXCLUDED.week_end,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    source_personal_weekly_report_ids = EXCLUDED.source_personal_weekly_report_ids,
			    source_task_ids = EXCLUDED.source_task_ids,
			    updated_at = now()
			RETURNING id::text`, target.TeamID, leaderID, ws, we, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID), pq.Array(sourceRefs.PersonalWeeklyIDs), pq.Array(sourceRefs.TaskIDs)).Scan(&reportID)
		return reportID, err
	case reportTypeDepartmentDaily:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO department_reports (department_id, report_date, content, generation_mode, managed_agent_run_id, agent_id, model_id, edited, status, saved_at, source_team_report_ids)
			VALUES ($1, $2, $3, 'managed_agent', $4, $5, $6, false, 'saved', now(), $7)
			ON CONFLICT (department_id, report_date) WHERE department_id IS NOT NULL DO UPDATE
			SET content = EXCLUDED.content,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    status = 'saved',
			    saved_at = now(),
			    source_team_report_ids = EXCLUDED.source_team_report_ids,
			    updated_at = now()
			RETURNING id::text`, target.DepartmentID, date, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID), pq.Array(sourceRefs.TeamDailyIDs)).Scan(&reportID)
		return reportID, err
	case reportTypeDepartmentWeekly:
		var reportID string
		err := tx.QueryRowContext(ctx, `
			INSERT INTO department_weekly_reports (department_id, week_start, week_end, content, generation_mode, managed_agent_run_id, agent_id, model_id, edited, source_team_weekly_report_ids)
			VALUES ($1, $2, $3, $4, 'managed_agent', $5, $6, $7, false, $8)
			ON CONFLICT (department_id, week_start) WHERE department_id IS NOT NULL DO UPDATE
			SET content = EXCLUDED.content,
			    week_end = EXCLUDED.week_end,
			    generation_mode = 'managed_agent',
			    managed_agent_run_id = EXCLUDED.managed_agent_run_id,
			    agent_id = EXCLUDED.agent_id,
			    model_id = EXCLUDED.model_id,
			    edited = false,
			    source_team_weekly_report_ids = EXCLUDED.source_team_weekly_report_ids,
			    updated_at = now()
			RETURNING id::text`, target.DepartmentID, ws, we, content, run.ID, nullableValue(run.AgentID), nullablePtrValue(run.ModelID), pq.Array(sourceRefs.TeamWeeklyIDs)).Scan(&reportID)
		return reportID, err
	}
	return "", fmt.Errorf("unsupported report_type: %s", reportType)
}

func markAIRunFailedTx(ctx context.Context, tx *sql.Tx, runID, userID, code, message string) error {
	formatted := message
	if code != "" {
		formatted = code + ": " + message
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE ai_runs
		SET status = 'failed',
		    execution_stage = 'completed',
		    stage_updated_at = now(),
		    failure_stage = COALESCE(execution_stage, 'writing_result'),
		    error_code = NULLIF($2, ''),
		    error_message = $1,
		    finished_at = now()
		WHERE id::text = $3 AND user_id = $4`, formatted, code, runID, userID)
	return err
}

func nullableValue(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullablePtrValue(p *string) any {
	if p == nil || *p == "" {
		return nil
	}
	return *p
}
