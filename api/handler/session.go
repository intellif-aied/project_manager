package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/aidashboard/api/storage"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type SessionHandler struct {
	db            *sql.DB
	store         *storage.MinioStorage
	ai            *service.AIClient
	eventRecorder *service.WorkItemEventRecorder
}

func NewSessionHandler(db *sql.DB, store *storage.MinioStorage, ai *service.AIClient) *SessionHandler {
	return &SessionHandler{db: db, store: store, ai: ai}
}

func NewSessionHandlerWithRecorder(
	db *sql.DB,
	store *storage.MinioStorage,
	ai *service.AIClient,
	recorder *service.WorkItemEventRecorder,
) *SessionHandler {
	return &SessionHandler{db: db, store: store, ai: ai, eventRecorder: recorder}
}

func (h *SessionHandler) List(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	page, pageSize := parsePagination(r, 20, 100)

	where := " WHERE 1=1"
	args := []any{}
	argIdx := 1

	if date := r.URL.Query().Get("date"); date != "" {
		where += fmt.Sprintf(" AND DATE(s.uploaded_at) = $%d", argIdx)
		args = append(args, date)
		argIdx++
	}
	if startedFrom := r.URL.Query().Get("started_from"); startedFrom != "" {
		where += fmt.Sprintf(" AND s.started_at >= $%d", argIdx)
		args = append(args, startedFrom)
		argIdx++
	}
	if startedTo := r.URL.Query().Get("started_to"); startedTo != "" {
		where += fmt.Sprintf(" AND s.started_at <= $%d", argIdx)
		args = append(args, startedTo)
		argIdx++
	}

	switch u.Role {
	case "employee", "pm":
		where += fmt.Sprintf(" AND s.user_id = $%d", argIdx)
		args = append(args, u.ID)
		argIdx++
	case "team_leader":
		if u.TeamID != nil {
			where += fmt.Sprintf(" AND s.user_id IN (SELECT id FROM users WHERE team_id = $%d)", argIdx)
			args = append(args, *u.TeamID)
			argIdx++
		}
	}

	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) FROM sessions s"+where, args...).Scan(&total); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	query := `
		SELECT s.id, s.session_ref, s.user_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), s.agent_type, s.started_at, s.ended_at,
			s.duration_secs, s.model, s.summary, s.tool_calls_json, s.git_commits,
			s.task_id, COALESCE(t.title,''), s.requirement_id, s.match_confidence,
			s.raw_log_url, s.uploaded_at
		FROM sessions s
		LEFT JOIN users u ON u.id = s.user_id
		LEFT JOIN tasks t ON t.id = s.task_id` + where
	query += fmt.Sprintf(" ORDER BY s.uploaded_at DESC LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	sessions := []model.Session{}
	for rows.Next() {
		var s model.Session
		var endedAt, rawLogURL sql.NullString
		var durationSecs sql.NullInt64
		var summary sql.NullString
		var taskID, reqID sql.NullString
		var taskTitle sql.NullString
		var confidence sql.NullFloat64
		var toolCallsJSON []byte
		var gitCommits pq.StringArray

		if err := rows.Scan(&s.ID, &s.SessionRef, &s.UserID, &s.UserName, &s.AgentType, &s.StartedAt, &endedAt,
			&durationSecs, &s.Model, &summary, &toolCallsJSON, &gitCommits,
			&taskID, &taskTitle, &reqID, &confidence,
			&rawLogURL, &s.UploadedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		if endedAt.Valid {
			s.EndedAt = parseTimePtr(endedAt.String)
		}
		if durationSecs.Valid {
			ds := int(durationSecs.Int64)
			s.DurationSecs = &ds
		}
		s.Summary = nullStringPtr(summary)
		if len(toolCallsJSON) > 0 {
			json.Unmarshal(toolCallsJSON, &s.ToolCallsJSON)
		}
		if len(gitCommits) > 0 {
			s.GitCommits = []string(gitCommits)
		}
		s.TaskID = nullStringPtr(taskID)
		s.TaskTitle = nullStringPtr(taskTitle)
		s.RequirementID = nullStringPtr(reqID)
		if confidence.Valid {
			s.MatchConfidence = &confidence.Float64
		}
		s.RawLogURL = nullStringPtr(rawLogURL)
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, model.PaginatedSessions{
		Items:    sessions,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *SessionHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var s model.Session
	var endedAt, rawLogURL sql.NullString
	var durationSecs sql.NullInt64
	var summary sql.NullString
	var taskID, reqID sql.NullString
	var taskTitle sql.NullString
	var confidence sql.NullFloat64
	var toolCallsJSON []byte
	var gitCommits pq.StringArray

	err := h.db.QueryRow(`
		SELECT s.id, s.session_ref, s.user_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), s.agent_type, s.started_at, s.ended_at,
			s.duration_secs, s.model, s.summary, s.tool_calls_json, s.git_commits,
			s.task_id, COALESCE(t.title,''), s.requirement_id, s.match_confidence,
			s.raw_log_url, s.uploaded_at
		FROM sessions s
		LEFT JOIN users u ON u.id = s.user_id
		LEFT JOIN tasks t ON t.id = s.task_id
		WHERE s.id = $1`, id).Scan(
		&s.ID, &s.SessionRef, &s.UserID, &s.UserName, &s.AgentType, &s.StartedAt, &endedAt,
		&durationSecs, &s.Model, &summary, &toolCallsJSON, &gitCommits,
		&taskID, &taskTitle, &reqID, &confidence,
		&rawLogURL, &s.UploadedAt,
	)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if endedAt.Valid {
		s.EndedAt = parseTimePtr(endedAt.String)
	}
	if durationSecs.Valid {
		ds := int(durationSecs.Int64)
		s.DurationSecs = &ds
	}
	s.Summary = nullStringPtr(summary)
	if len(toolCallsJSON) > 0 {
		json.Unmarshal(toolCallsJSON, &s.ToolCallsJSON)
	}
	if len(gitCommits) > 0 {
		s.GitCommits = []string(gitCommits)
	}
	s.TaskID = nullStringPtr(taskID)
	s.TaskTitle = nullStringPtr(taskTitle)
	s.RequirementID = nullStringPtr(reqID)
	if confidence.Valid {
		s.MatchConfidence = &confidence.Float64
	}
	s.RawLogURL = nullStringPtr(rawLogURL)

	writeJSON(w, http.StatusOK, s)
}

func (h *SessionHandler) BatchUpload(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)

	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart request: " + err.Error()})
		return
	}

	metadataPart := r.FormValue("metadata")
	if metadataPart == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing metadata field"})
		return
	}

	var batch model.BatchSessionUpload
	if err := json.Unmarshal([]byte(metadataPart), &batch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid metadata JSON: " + err.Error()})
		return
	}

	type result struct {
		SessionRef string   `json:"session_ref"`
		ID         string   `json:"id"`
		Status     string   `json:"status"`
		Warnings   []string `json:"warnings,omitempty"`
	}
	var results []result

	for _, su := range batch.Sessions {
		warnings := []string{}
		if su.SessionRef == "" {
			results = append(results, result{Status: "error: missing session_ref"})
			continue
		}

		toolCallsJSON, _ := json.Marshal(su.ToolCalls)
		gitCommits := arrayToTextArray(su.GitCommits)

		agentType := su.AgentType
		if agentType == "" {
			agentType = "claude_code"
		}

		var existingID string
		err := h.db.QueryRow(
			"SELECT id FROM sessions WHERE session_ref = $1 AND user_id = $2",
			su.SessionRef, u.ID).Scan(&existingID)

		status := "created"
		sessionID := existingID

		if err == sql.ErrNoRows {
			err := h.db.QueryRow(`
				INSERT INTO sessions (session_ref, user_id, agent_type, started_at, ended_at, duration_secs, model, summary, tool_calls_json, git_commits, models)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				RETURNING id`,
				su.SessionRef, u.ID, agentType, su.StartedAt, su.EndedAt, su.DurationSecs,
				su.Model, su.Summary, toolCallsJSON, gitCommits,
				pq.Array(sessionModels(su)),
			).Scan(&sessionID)
			if err != nil {
				results = append(results, result{SessionRef: su.SessionRef, Status: "error: " + err.Error()})
				continue
			}
		} else if err == nil {
			status = "updated"
			_, err := h.db.Exec(`
				UPDATE sessions
				SET agent_type = $3, started_at = $4, ended_at = $5, duration_secs = $6, model = $7,
					summary = $8, tool_calls_json = $9, git_commits = $10, models = $11, uploaded_at = now()
				WHERE id = $1 AND user_id = $2`,
				sessionID, u.ID, agentType, su.StartedAt, su.EndedAt, su.DurationSecs,
				su.Model, su.Summary, toolCallsJSON, gitCommits,
				pq.Array(sessionModels(su)),
			)
			if err != nil {
				results = append(results, result{SessionRef: su.SessionRef, ID: sessionID, Status: "error: " + err.Error()})
				continue
			}
		} else {
			results = append(results, result{SessionRef: su.SessionRef, Status: "error: " + err.Error()})
			continue
		}

		activityWarnings, err := h.replaceActivitySlices(sessionID, u.ID, agentType, su)
		if err != nil {
			results = append(results, result{SessionRef: su.SessionRef, ID: sessionID, Status: "error: activity_slices: " + err.Error()})
			continue
		}
		warnings = append(warnings, activityWarnings...)

		// Upload raw JSONL to MinIO if file is provided and storage is available
		fileKey := "file_" + su.SessionRef
		file, header, err := r.FormFile(fileKey)
		if err == nil {
			if h.store == nil {
				warnings = append(warnings, "raw log storage is not configured")
				file.Close()
			} else {
				objectName := fmt.Sprintf("sessions/%s/%s.jsonl", u.ID, su.SessionRef)
				ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
				uploadErr := h.store.Upload(ctx, objectName, file, header.Size, "application/x-jsonlines")
				cancel()
				file.Close()
				if uploadErr != nil {
					warnings = append(warnings, "raw log upload failed: "+uploadErr.Error())
				} else if _, err = h.db.Exec("UPDATE sessions SET raw_log_url = $1 WHERE id = $2", objectName, sessionID); err != nil {
					warnings = append(warnings, "raw log url save failed")
				}
			}
		}

		if su.TokenUsage != nil {
			if err := h.replaceTokenUsage(sessionID, u.ID, su); err != nil {
				results = append(results, result{SessionRef: su.SessionRef, ID: sessionID, Status: "error: token_usage: " + err.Error()})
				continue
			}
		}

		h.matchTaskAsync(sessionID, u.ID, su.Summary)

		results = append(results, result{SessionRef: su.SessionRef, ID: sessionID, Status: status, Warnings: warnings})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":   len(batch.Sessions),
		"results": results,
	})
}

// sessionModels returns the model list to persist for a session upload:
// the daemon-reported distinct models if present, otherwise a single-element
// fallback so the column is never empty for sessions that report a model.
func sessionModels(su model.SessionUpload) []string {
	if su.TokenUsage != nil && len(su.TokenUsage.Models) > 0 {
		return su.TokenUsage.Models
	}
	if su.Model != "" {
		return []string{su.Model}
	}
	return []string{}
}

func (h *SessionHandler) replaceActivitySlices(sessionID, userID, agentType string, su model.SessionUpload) ([]string, error) {
	slices, warnings := normalizedActivitySlices(su, agentType)
	tx, err := h.db.Begin()
	if err != nil {
		return warnings, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM session_activity_slices WHERE session_id = $1", sessionID); err != nil {
		return warnings, err
	}
	for _, slice := range slices {
		toolCallsJSON, _ := json.Marshal(slice.ToolCalls)
		if slice.TokenSliceStrategy == "" {
			slice.TokenSliceStrategy = "exact"
		}
		if slice.SummaryStrategy == "" {
			slice.SummaryStrategy = "rule"
		}
		if slice.ParserVersion == "" {
			slice.ParserVersion = "v1"
		}
		if slice.SliceVersion <= 0 {
			slice.SliceVersion = 1
		}
		if slice.Timezone == "" {
			slice.Timezone = defaultActivityTimezone
		}
		if slice.AgentType == "" {
			slice.AgentType = agentType
		}
		if slice.Model == "" {
			slice.Model = su.Model
		}
		models := slice.Models
		if len(models) == 0 {
			models = sessionModels(su)
		}
		if slice.TotalTokens == 0 {
			slice.TotalTokens = slice.InputTokens + slice.OutputTokens + slice.CacheCreationTokens + slice.CacheReadTokens
		}
		if _, err := tx.Exec(`
			INSERT INTO session_activity_slices (
				session_id, user_id, activity_date, activity_start_at, activity_end_at, timezone,
				agent_type, model, models, summary, excerpt, message_count, source_event_count,
				tool_calls_json, git_commits, task_id, requirement_id,
				input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, total_tokens,
				source_has_raw_log, token_slice_strategy, summary_strategy, parser_version, slice_version, is_estimated
			)
			SELECT $1, $2, $3::date, $4, $5, $6,
				$7, $8, $9, $10, $11, $12, $13,
				$14, $15, s.task_id, s.requirement_id,
				$16, $17, $18, $19, $20,
				$21, $22, $23, $24, $25, $26
			FROM sessions s WHERE s.id = $1`,
			sessionID, userID, slice.ActivityDate, slice.ActivityStartAt, slice.ActivityEndAt, slice.Timezone,
			slice.AgentType, slice.Model, pq.Array(models), truncateForStorage(slice.Summary, 2000), truncateForStorage(slice.Excerpt, 4000),
			slice.MessageCount, slice.SourceEventCount, toolCallsJSON, arrayToTextArray(slice.GitCommits),
			slice.InputTokens, slice.OutputTokens, slice.CacheCreationTokens, slice.CacheReadTokens, slice.TotalTokens,
			slice.SourceHasRawLog, slice.TokenSliceStrategy, slice.SummaryStrategy, slice.ParserVersion, slice.SliceVersion, slice.IsEstimated,
		); err != nil {
			return warnings, err
		}
	}
	return warnings, tx.Commit()
}

const defaultActivityTimezone = "Asia/Shanghai"

func normalizedActivitySlices(su model.SessionUpload, agentType string) ([]model.SessionActivitySliceUpload, []string) {
	warnings := []string{}
	slices := append([]model.SessionActivitySliceUpload(nil), su.ActivitySlices...)
	if len(slices) == 0 {
		fallback, ok := fallbackActivitySlice(su, agentType)
		if !ok {
			return nil, []string{"activity_slices missing for cross-day or invalid session; no activity slice was stored"}
		}
		return []model.SessionActivitySliceUpload{fallback}, []string{"activity_slices missing; stored a single-day estimated slice"}
	}

	loc := activityLocation()
	seen := map[string]bool{}
	var total int64
	out := make([]model.SessionActivitySliceUpload, 0, len(slices))
	for _, slice := range slices {
		if slice.ActivityStartAt.IsZero() || slice.ActivityEndAt.IsZero() {
			warnings = append(warnings, "ignored activity slice with empty start/end")
			continue
		}
		if slice.ActivityEndAt.Before(slice.ActivityStartAt) {
			warnings = append(warnings, "ignored activity slice with end before start")
			continue
		}
		if slice.ActivityDate == "" {
			slice.ActivityDate = slice.ActivityStartAt.In(loc).Format("2006-01-02")
		}
		if slice.ActivityDate != slice.ActivityStartAt.In(loc).Format("2006-01-02") {
			warnings = append(warnings, "ignored activity slice whose activity_date does not match activity_start_at")
			continue
		}
		if seen[slice.ActivityDate] {
			warnings = append(warnings, "ignored duplicate activity slice for "+slice.ActivityDate)
			continue
		}
		if hasNegativeTokens(slice) {
			warnings = append(warnings, "ignored activity slice with negative tokens")
			continue
		}
		if !su.StartedAt.IsZero() && slice.ActivityStartAt.Before(su.StartedAt.Add(-5*time.Minute)) {
			warnings = append(warnings, "activity slice starts before session started_at")
		}
		if su.EndedAt != nil && !su.EndedAt.IsZero() && slice.ActivityEndAt.After(su.EndedAt.Add(5*time.Minute)) {
			warnings = append(warnings, "activity slice ends after session ended_at")
		}
		if slice.TotalTokens == 0 {
			slice.TotalTokens = slice.InputTokens + slice.OutputTokens + slice.CacheCreationTokens + slice.CacheReadTokens
		}
		total += slice.TotalTokens
		seen[slice.ActivityDate] = true
		out = append(out, slice)
	}
	if su.TokenUsage != nil && su.TokenUsage.TotalTokens > 0 && total > su.TokenUsage.TotalTokens {
		warnings = append(warnings, "activity slice token total exceeds session token total")
	}
	if len(out) == 0 {
		warnings = append(warnings, "no valid activity slices were stored")
	}
	return out, warnings
}

func fallbackActivitySlice(su model.SessionUpload, agentType string) (model.SessionActivitySliceUpload, bool) {
	if su.StartedAt.IsZero() {
		return model.SessionActivitySliceUpload{}, false
	}
	loc := activityLocation()
	startDate := su.StartedAt.In(loc).Format("2006-01-02")
	end := su.StartedAt
	if su.EndedAt != nil && !su.EndedAt.IsZero() {
		end = *su.EndedAt
	}
	if end.In(loc).Format("2006-01-02") != startDate {
		return model.SessionActivitySliceUpload{}, false
	}
	summary := ""
	if su.Summary != nil {
		summary = *su.Summary
	}
	slice := model.SessionActivitySliceUpload{
		ActivityDate:       startDate,
		ActivityStartAt:    su.StartedAt,
		ActivityEndAt:      end,
		Timezone:           defaultActivityTimezone,
		AgentType:          agentType,
		Model:              su.Model,
		Models:             sessionModels(su),
		Summary:            summary,
		Excerpt:            summary,
		ToolCalls:          su.ToolCalls,
		GitCommits:         su.GitCommits,
		SourceHasRawLog:    false,
		TokenSliceStrategy: "estimated",
		SummaryStrategy:    "rule",
		ParserVersion:      "fallback-v1",
		SliceVersion:       1,
		IsEstimated:        true,
	}
	if su.TokenUsage != nil {
		slice.InputTokens = su.TokenUsage.InputTokens
		slice.OutputTokens = su.TokenUsage.OutputTokens
		slice.CacheCreationTokens = su.TokenUsage.CacheCreationTokens
		slice.CacheReadTokens = su.TokenUsage.CacheReadTokens
		slice.TotalTokens = su.TokenUsage.TotalTokens
	}
	return slice, true
}

func hasNegativeTokens(slice model.SessionActivitySliceUpload) bool {
	return slice.InputTokens < 0 || slice.OutputTokens < 0 || slice.CacheCreationTokens < 0 || slice.CacheReadTokens < 0 || slice.TotalTokens < 0
}

func activityLocation() *time.Location {
	loc, err := time.LoadLocation(defaultActivityTimezone)
	if err != nil {
		return time.FixedZone(defaultActivityTimezone, 8*3600)
	}
	return loc
}

func truncateForStorage(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max])
}

func (h *SessionHandler) replaceTokenUsage(sessionID, userID string, su model.SessionUpload) error {
	tx, err := h.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec("DELETE FROM token_usage WHERE session_id = $1", sessionID); err != nil {
		return err
	}

	models := su.TokenUsage.Models
	if len(models) == 0 {
		models = []string{su.Model}
	}

	agentType := su.AgentType
	if agentType == "" {
		agentType = "claude_code"
	}

	if _, err := tx.Exec(`
		INSERT INTO token_usage (session_id, user_id, task_id, requirement_id, agent_type, model,
			input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens, total_tokens, models)
		SELECT $1, $2, s.task_id, s.requirement_id, $3, $4,
			$5, $6, $7, $8, $9, $10
		FROM sessions s
		WHERE s.id = $1`,
		sessionID, userID, agentType, su.Model,
		su.TokenUsage.InputTokens, su.TokenUsage.OutputTokens,
		su.TokenUsage.CacheCreationTokens, su.TokenUsage.CacheReadTokens,
		su.TokenUsage.TotalTokens, pq.Array(models),
	); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE sessions SET models = $2 WHERE id = $1 AND (models IS NULL OR models = '{}')`,
		sessionID, pq.Array(models)); err != nil {
		return err
	}
	return tx.Commit()
}

// matchTaskAsync runs AI task-matching inline (kept simple — claude CLI blocks the upload briefly).
// Failures are non-fatal: the session is still recorded with NULL task_id.
func (h *SessionHandler) matchTaskAsync(sessionID, userID string, summary *string) {
	if h.ai == nil {
		return
	}
	if summary == nil || *summary == "" {
		return
	}

	rows, err := h.db.Query(`
		SELECT id, title FROM tasks
		WHERE assignee_id = $1 AND status IN ('todo','in_progress')`, userID)
	if err != nil {
		log.Printf("matchTaskAsync: query tasks failed: %v", err)
		return
	}
	var tasks []service.TaskBrief
	for rows.Next() {
		var tb service.TaskBrief
		if err := rows.Scan(&tb.ID, &tb.Title); err != nil {
			rows.Close()
			return
		}
		tasks = append(tasks, tb)
	}
	rows.Close()
	if len(tasks) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	match, err := h.ai.MatchSessionToTask(ctx, *summary, tasks)
	if err != nil {
		log.Printf("AI match session->task failed (session=%s): %v", sessionID, err)
		return
	}
	if match.TaskID == "" || match.Confidence < 0.7 {
		return
	}

	var reqID *string
	h.db.QueryRow("SELECT requirement_id FROM tasks WHERE id = $1", match.TaskID).Scan(&reqID)

	_, err = h.db.Exec(`
		UPDATE sessions SET task_id = $1, requirement_id = $2, match_confidence = $3 WHERE id = $4`,
		match.TaskID, reqID, match.Confidence, sessionID)
	if err != nil {
		log.Printf("matchTaskAsync: update session failed: %v", err)
		return
	}

	if _, err := h.db.Exec(`
		UPDATE token_usage SET task_id = $1, requirement_id = $2 WHERE session_id = $3`,
		match.TaskID, reqID, sessionID); err != nil {
		log.Printf("matchTaskAsync: update token_usage failed: %v", err)
	}
}

func (h *SessionHandler) DownloadLog(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)

	var rawLogURL sql.NullString
	var ownerID string
	err := h.db.QueryRow("SELECT raw_log_url, user_id FROM sessions WHERE id = $1", id).Scan(&rawLogURL, &ownerID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if u.Role != "director" && u.Role != "admin" && u.ID != ownerID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	if !rawLogURL.Valid || rawLogURL.String == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "no raw log available"})
		return
	}

	if h.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "raw log storage not configured"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	stream, err := h.store.Download(ctx, rawLogURL.String)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "download failed: " + err.Error()})
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "application/x-jsonlines")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+id+".jsonl\"")
	io.Copy(w, stream)
}

func (h *SessionHandler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	var req model.UpdateSessionTaskRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	activityDate := ""
	if req.ActivityDate != nil {
		activityDate = strings.TrimSpace(*req.ActivityDate)
	}
	if activityDate != "" {
		if _, err := time.Parse("2006-01-02", activityDate); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid activity_date"})
			return
		}
	}

	var reqID *string
	if req.TaskID != nil && *req.TaskID != "" {
		h.db.QueryRow("SELECT requirement_id FROM tasks WHERE id = $1", *req.TaskID).Scan(&reqID)
	}
	previousTaskID, previousRequirementID := h.loadSessionTaskAssociation(id, activityDate)
	if activityDate != "" {
		res, err := h.db.Exec(`
			UPDATE session_activity_slices
			SET task_id = $1, requirement_id = $2
			WHERE session_id = $3 AND activity_date = $4::date AND (user_id = $5 OR $6 = 'admin')`,
			req.TaskID, reqID, id, activityDate, u.ID, u.Role)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session slice not found"})
			return
		}
		h.recordSessionTaskChange(r.Context(), u, id, activityDate, previousTaskID, previousRequirementID, req.TaskID, reqID)
		h.Get(w, r)
		return
	}

	res, err := h.db.Exec(`
		UPDATE sessions
		SET task_id = $1, requirement_id = $2
		WHERE id = $3 AND (user_id = $4 OR $5 = 'admin')`,
		req.TaskID, reqID, id, u.ID, u.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	_, _ = h.db.Exec("UPDATE token_usage SET task_id = $1, requirement_id = $2 WHERE session_id = $3", req.TaskID, reqID, id)
	_, _ = h.db.Exec("UPDATE session_activity_slices SET task_id = $1, requirement_id = $2 WHERE session_id = $3", req.TaskID, reqID, id)
	h.recordSessionTaskChange(r.Context(), u, id, activityDate, previousTaskID, previousRequirementID, req.TaskID, reqID)
	h.Get(w, r)
}

func (h *SessionHandler) UpdateRequirement(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	var req model.UpdateSessionRequirementRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	activityDate := ""
	if req.ActivityDate != nil {
		activityDate = strings.TrimSpace(*req.ActivityDate)
	}
	if activityDate != "" {
		if _, err := time.Parse("2006-01-02", activityDate); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid activity_date"})
			return
		}
	}
	if req.RequirementID != nil && *req.RequirementID != "" {
		var exists bool
		if err := h.db.QueryRow("SELECT EXISTS(SELECT 1 FROM requirements WHERE id = $1)", *req.RequirementID).Scan(&exists); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !exists {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "requirement not found"})
			return
		}
	}
	_, previousRequirementID := h.loadSessionTaskAssociation(id, activityDate)
	if activityDate != "" {
		res, err := h.db.Exec(`
			UPDATE session_activity_slices
			SET task_id = NULL, requirement_id = $1
			WHERE session_id = $2 AND activity_date = $3::date AND (user_id = $4 OR $5 = 'admin')`,
			req.RequirementID, id, activityDate, u.ID, u.Role)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session slice not found"})
			return
		}
		h.recordSessionRequirementChange(r.Context(), u, id, activityDate, previousRequirementID, req.RequirementID)
		h.Get(w, r)
		return
	}
	res, err := h.db.Exec(`
		UPDATE sessions
		SET task_id = NULL, requirement_id = $1
		WHERE id = $2 AND (user_id = $3 OR $4 = 'admin')`, req.RequirementID, id, u.ID, u.Role)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	_, _ = h.db.Exec("UPDATE token_usage SET task_id = NULL, requirement_id = $1 WHERE session_id = $2", req.RequirementID, id)
	_, _ = h.db.Exec("UPDATE session_activity_slices SET task_id = NULL, requirement_id = $1 WHERE session_id = $2", req.RequirementID, id)
	h.recordSessionRequirementChange(r.Context(), u, id, activityDate, previousRequirementID, req.RequirementID)
	h.Get(w, r)
}

func (h *SessionHandler) loadSessionTaskAssociation(sessionID string, activityDate string) (string, string) {
	var taskID, requirementID sql.NullString
	var err error
	if activityDate != "" {
		err = h.db.QueryRow(`
			SELECT task_id::text, requirement_id::text
			FROM session_activity_slices
			WHERE session_id = $1 AND activity_date = $2::date`, sessionID, activityDate).Scan(&taskID, &requirementID)
	} else {
		err = h.db.QueryRow(`
			SELECT task_id::text, requirement_id::text
			FROM sessions
			WHERE id = $1`, sessionID).Scan(&taskID, &requirementID)
	}
	if err != nil {
		return "", ""
	}
	return nullStringValue(taskID), nullStringValue(requirementID)
}

func (h *SessionHandler) recordSessionTaskChange(
	ctx context.Context,
	actor *model.User,
	sessionID string,
	activityDate string,
	previousTaskID string,
	previousRequirementID string,
	nextTaskID *string,
	nextRequirementID *string,
) {
	nextTask := ""
	if nextTaskID != nil {
		nextTask = strings.TrimSpace(*nextTaskID)
	}
	if previousTaskID != "" && previousTaskID != nextTask {
		h.recordSessionTaskEvent(ctx, actor, "task_session_unlinked", "取消关联 session", previousTaskID, previousRequirementID, sessionID, activityDate)
	}
	if nextTask != "" && nextTask != previousTaskID {
		h.recordSessionTaskEvent(ctx, actor, "task_session_linked", "关联了 session", nextTask, stringValue(nextRequirementID), sessionID, activityDate)
	}
}

func (h *SessionHandler) recordSessionRequirementChange(
	ctx context.Context,
	actor *model.User,
	sessionID string,
	activityDate string,
	previousRequirementID string,
	nextRequirementID *string,
) {
	nextRequirement := ""
	if nextRequirementID != nil {
		nextRequirement = strings.TrimSpace(*nextRequirementID)
	}
	if previousRequirementID != "" && previousRequirementID != nextRequirement {
		h.recordSessionRequirementEvent(ctx, actor, "requirement_session_unlinked", "取消关联 session", previousRequirementID, sessionID, activityDate)
	}
	if nextRequirement != "" && nextRequirement != previousRequirementID {
		h.recordSessionRequirementEvent(ctx, actor, "requirement_session_linked", "关联了 session", nextRequirement, sessionID, activityDate)
	}
}

func (h *SessionHandler) Withdraw(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)

	var ownerID string
	err := h.db.QueryRow("SELECT user_id FROM sessions WHERE id = $1", id).Scan(&ownerID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if u.Role != "director" && u.Role != "admin" && u.ID != ownerID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}

	res, err := h.db.Exec("DELETE FROM sessions WHERE id = $1 AND user_id = $2", id, ownerID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "withdrawn"})
}
