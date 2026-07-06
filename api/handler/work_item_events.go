package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"reflect"
	"strings"

	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

func (h *RequirementHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "requirement_id") {
		return
	}
	u := getUser(r)
	allowed, err := h.canViewRequirement(u, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	page, pageSize := parsePagination(r, 20, 100)
	result, err := listWorkItemEvents(r.Context(), h.db, "requirement_id = $1", []any{id}, page, pageSize)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *TaskHandler) ListEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "task_id") {
		return
	}
	u := getUser(r)
	task, err := h.loadTaskAccess(id)
	if err == sql.ErrNoRows {
		requirementID, lookupErr := h.loadTaskEventRequirementID(id)
		if lookupErr == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if lookupErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": lookupErr.Error()})
			return
		}
		allowed, viewErr := NewRequirementHandler(h.db, nil).canViewRequirement(u, requirementID)
		if viewErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": viewErr.Error()})
			return
		}
		if !allowed {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeTaskEvents(w, r, h.db, id)
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allowed, err := h.canViewTaskRecord(u, task)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeTaskEvents(w, r, h.db, id)
}

func (h *TaskHandler) loadTaskEventRequirementID(taskID string) (string, error) {
	var requirementID string
	err := h.db.QueryRow(`
		SELECT requirement_id::text
		FROM work_item_events
		WHERE task_id = $1 AND requirement_id IS NOT NULL
		ORDER BY created_at DESC, id DESC
		LIMIT 1`, taskID).Scan(&requirementID)
	return requirementID, err
}

func writeTaskEvents(w http.ResponseWriter, r *http.Request, db *sql.DB, taskID string) {
	page, pageSize := parsePagination(r, 20, 100)
	result, err := listWorkItemEvents(r.Context(), db, "task_id = $1", []any{taskID}, page, pageSize)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func listWorkItemEvents(
	ctx context.Context,
	db *sql.DB,
	where string,
	args []any,
	page int,
	pageSize int,
) (model.PaginatedWorkItemEvents, error) {
	result := model.PaginatedWorkItemEvents{
		Items:    []model.WorkItemEvent{},
		Page:     page,
		PageSize: pageSize,
	}
	countQuery := "SELECT COUNT(*) FROM work_item_events WHERE " + where
	if err := db.QueryRowContext(ctx, countQuery, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, (page-1)*pageSize)
	query := fmt.Sprintf(`
		SELECT id::text, target_type, target_id::text,
			requirement_id::text, task_id::text, actor_id::text,
			actor_name, actor_role, event_type, event_title,
			before_data, after_data, metadata, created_at
		FROM work_item_events
		WHERE %s
		ORDER BY created_at DESC, id DESC
		LIMIT $%d OFFSET $%d`, where, len(queryArgs)-1, len(queryArgs))
	rows, err := db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return result, err
	}
	defer rows.Close()

	for rows.Next() {
		var item model.WorkItemEvent
		var requirementID, taskID, actorID sql.NullString
		var beforeJSON, afterJSON, metadataJSON []byte
		if err := rows.Scan(
			&item.ID,
			&item.TargetType,
			&item.TargetID,
			&requirementID,
			&taskID,
			&actorID,
			&item.ActorName,
			&item.ActorRole,
			&item.EventType,
			&item.EventTitle,
			&beforeJSON,
			&afterJSON,
			&metadataJSON,
			&item.CreatedAt,
		); err != nil {
			return result, err
		}
		item.RequirementID = nullStringPtr(requirementID)
		item.TaskID = nullStringPtr(taskID)
		item.ActorID = nullStringPtr(actorID)
		item.BeforeData = decodeEventJSON(beforeJSON)
		item.AfterData = decodeEventJSON(afterJSON)
		item.Metadata = decodeEventJSON(metadataJSON)
		normalizeWorkItemEventTitle(&item)
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func normalizeWorkItemEventTitle(item *model.WorkItemEvent) {
	if item == nil || item.EventType != "task_deleted" {
		return
	}
	taskTitle, _ := item.BeforeData["title"].(string)
	taskTitle = strings.TrimSpace(taskTitle)
	if taskTitle == "" || strings.Contains(item.EventTitle, taskTitle) {
		return
	}
	baseTitle := strings.TrimSpace(item.EventTitle)
	if baseTitle == "" {
		baseTitle = "删除了任务"
	}
	item.EventTitle = baseTitle + "：" + taskTitle
}

func decodeEventJSON(raw []byte) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return map[string]any{}
	}
	if value == nil {
		return map[string]any{}
	}
	return value
}

func (h *RequirementHandler) recordRequirementEvent(
	ctx context.Context,
	actor *model.User,
	requirementID string,
	eventType string,
	eventTitle string,
	beforeData map[string]any,
	afterData map[string]any,
	metadata map[string]any,
) {
	if h == nil || h.eventRecorder == nil {
		return
	}
	if err := h.eventRecorder.Record(ctx, service.WorkItemEventInput{
		TargetType:    "requirement",
		TargetID:      requirementID,
		RequirementID: requirementID,
		Actor:         actor,
		EventType:     eventType,
		EventTitle:    eventTitle,
		BeforeData:    beforeData,
		AfterData:     afterData,
		Metadata:      metadata,
	}); err != nil {
		log.Printf("record requirement event failed: requirement_id=%s event_type=%s error=%v", requirementID, eventType, err)
	}
}

func (h *TaskHandler) recordTaskEvent(
	ctx context.Context,
	actor *model.User,
	taskID string,
	requirementID string,
	eventType string,
	eventTitle string,
	beforeData map[string]any,
	afterData map[string]any,
	metadata map[string]any,
) {
	if h == nil || h.eventRecorder == nil {
		return
	}
	if err := h.eventRecorder.Record(ctx, service.WorkItemEventInput{
		TargetType:    "task",
		TargetID:      taskID,
		RequirementID: requirementID,
		TaskID:        taskID,
		Actor:         actor,
		EventType:     eventType,
		EventTitle:    eventTitle,
		BeforeData:    beforeData,
		AfterData:     afterData,
		Metadata:      metadata,
	}); err != nil {
		log.Printf("record task event failed: task_id=%s event_type=%s error=%v", taskID, eventType, err)
	}
}

func (h *SessionHandler) recordSessionTaskEvent(
	ctx context.Context,
	actor *model.User,
	eventType string,
	eventTitle string,
	taskID string,
	requirementID string,
	sessionID string,
	activityDate string,
) {
	if h == nil || h.eventRecorder == nil || taskID == "" {
		return
	}
	if err := h.eventRecorder.Record(ctx, service.WorkItemEventInput{
		TargetType:    "task",
		TargetID:      taskID,
		RequirementID: requirementID,
		TaskID:        taskID,
		Actor:         actor,
		EventType:     eventType,
		EventTitle:    eventTitle,
		Metadata: map[string]any{
			"session_id":    sessionID,
			"activity_date": activityDate,
		},
	}); err != nil {
		log.Printf("record task session event failed: task_id=%s event_type=%s error=%v", taskID, eventType, err)
	}
}

func (h *SessionHandler) recordSessionRequirementEvent(
	ctx context.Context,
	actor *model.User,
	eventType string,
	eventTitle string,
	requirementID string,
	sessionID string,
	activityDate string,
) {
	if h == nil || h.eventRecorder == nil || requirementID == "" {
		return
	}
	if err := h.eventRecorder.Record(ctx, service.WorkItemEventInput{
		TargetType:    "requirement",
		TargetID:      requirementID,
		RequirementID: requirementID,
		Actor:         actor,
		EventType:     eventType,
		EventTitle:    eventTitle,
		Metadata: map[string]any{
			"session_id":    sessionID,
			"activity_date": activityDate,
		},
	}); err != nil {
		log.Printf("record requirement session event failed: requirement_id=%s event_type=%s error=%v", requirementID, eventType, err)
	}
}

func (h *RequirementHandler) recordRequirementChange(ctx context.Context, actor *model.User, requirementID string, beforeState map[string]any) {
	if h == nil || h.eventRecorder == nil || beforeState == nil {
		return
	}
	afterState, err := loadRequirementEventState(h.db, requirementID)
	if err != nil {
		log.Printf("load requirement event state failed: requirement_id=%s error=%v", requirementID, err)
		return
	}
	changed := changedEventFields(beforeState, afterState, requirementEventLabels)
	if len(changed) == 0 {
		return
	}
	eventType, eventTitle := requirementChangeEvent(changed, beforeState, afterState)
	h.recordRequirementEvent(ctx, actor, requirementID, eventType, eventTitle, beforeState, afterState, map[string]any{
		"changed_fields": changed,
	})
}

func (h *TaskHandler) recordTaskChange(ctx context.Context, actor *model.User, taskID string, beforeState map[string]any) {
	if h == nil || h.eventRecorder == nil || beforeState == nil {
		return
	}
	afterState, err := loadTaskEventState(h.db, taskID)
	if err != nil {
		log.Printf("load task event state failed: task_id=%s error=%v", taskID, err)
		return
	}
	changed := changedEventFields(beforeState, afterState, taskEventLabels)
	if len(changed) == 0 {
		return
	}
	requirementID, _ := afterState["requirement_id"].(string)
	eventType, eventTitle := taskChangeEvent(changed)
	h.recordTaskEvent(ctx, actor, taskID, requirementID, eventType, eventTitle, beforeState, afterState, map[string]any{
		"changed_fields": changed,
	})
}

var requirementEventLabels = map[string]string{
	"title":                "标题",
	"description":          "描述",
	"feishu_doc_url":       "飞书文档",
	"priority":             "优先级",
	"status":               "阶段",
	"deadline":             "截止日期",
	"responsible_user_ids": "负责人",
	"team_ids":             "参与团队",
	"acceptance_criteria":  "验收标准",
}

var taskEventLabels = map[string]string{
	"title":                "标题",
	"priority":             "优先级",
	"status":               "状态",
	"progress":             "进度",
	"due_date":             "截止日期",
	"responsible_user_ids": "负责人",
	"acceptance_criteria":  "验收标准",
}

func loadRequirementEventState(db *sql.DB, requirementID string) (map[string]any, error) {
	var title, description, priority, status string
	var feishuURL, deadline sql.NullString
	var acStr string
	if err := db.QueryRow(`
		SELECT title, description, feishu_doc_url, priority, status, deadline::text, acceptance_criteria
		FROM requirements
		WHERE id = $1`, requirementID).Scan(&title, &description, &feishuURL, &priority, &status, &deadline, &acStr); err != nil {
		return nil, err
	}
	return map[string]any{
		"title":                title,
		"description":          description,
		"feishu_doc_url":       nullStringValue(feishuURL),
		"priority":             priority,
		"status":               status,
		"deadline":             nullStringValue(deadline),
		"responsible_user_ids": loadRequirementResponsibleUserIDs(db, requirementID),
		"team_ids":             loadRequirementTeamIDs(db, requirementID),
		"acceptance_criteria":  parseTextArray(acStr),
	}, nil
}

func loadTaskEventState(db *sql.DB, taskID string) (map[string]any, error) {
	var requirementID, title, status, priority string
	var ac pq.StringArray
	var dueDate sql.NullString
	var progress int
	if err := db.QueryRow(`
		SELECT requirement_id::text, title, COALESCE(acceptance_criteria, ARRAY[]::text[]),
			status, priority, progress, due_date::text
		FROM tasks
		WHERE id = $1`, taskID).Scan(&requirementID, &title, &ac, &status, &priority, &progress, &dueDate); err != nil {
		return nil, err
	}
	return map[string]any{
		"requirement_id":       requirementID,
		"title":                title,
		"acceptance_criteria":  []string(ac),
		"responsible_user_ids": loadTaskResponsibleUserIDs(db, taskID),
		"status":               status,
		"priority":             priority,
		"progress":             progress,
		"due_date":             nullStringValue(dueDate),
	}, nil
}

func loadRequirementResponsibleUserIDs(db *sql.DB, requirementID string) []string {
	return loadStringColumn(db, `
		SELECT user_id::text
		FROM requirement_responsibles
		WHERE requirement_id = $1
		ORDER BY created_at, user_id`, requirementID)
}

func loadTaskResponsibleUserIDs(db *sql.DB, taskID string) []string {
	return loadStringColumn(db, `
		SELECT user_id::text
		FROM task_responsibles
		WHERE task_id = $1
		ORDER BY created_at, user_id`, taskID)
}

func loadRequirementTeamIDs(db *sql.DB, requirementID string) []string {
	return loadStringColumn(db, `
		SELECT team_id::text
		FROM requirement_teams
		WHERE requirement_id = $1
		ORDER BY team_id`, requirementID)
}

func loadStringColumn(db *sql.DB, query string, arg any) []string {
	rows, err := db.Query(query, arg)
	if err != nil {
		return []string{}
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var item string
		if rows.Scan(&item) == nil {
			items = append(items, item)
		}
	}
	return items
}

func changedEventFields(beforeState map[string]any, afterState map[string]any, labels map[string]string) []string {
	changed := []string{}
	for key := range labels {
		if !reflect.DeepEqual(beforeState[key], afterState[key]) {
			changed = append(changed, key)
		}
	}
	return changed
}

func changeEventTitle(prefix string, changed []string) string {
	labels := requirementEventLabels
	if strings.Contains(prefix, "任务") {
		labels = taskEventLabels
	}
	parts := make([]string, 0, len(changed))
	for _, key := range changed {
		if label, ok := labels[key]; ok {
			parts = append(parts, label)
		}
	}
	if len(parts) == 0 {
		return prefix
	}
	return prefix + "：" + strings.Join(parts, "、")
}

func requirementChangeEventType(changed []string) string {
	return singleFieldEventType(changed, map[string]string{
		"status":               "requirement_status_changed",
		"responsible_user_ids": "requirement_responsibles_changed",
		"team_ids":             "requirement_team_changed",
		"deadline":             "requirement_deadline_changed",
		"acceptance_criteria":  "requirement_ac_updated",
	}, "requirement_updated")
}

func requirementChangeEvent(changed []string, beforeState map[string]any, afterState map[string]any) (string, string) {
	if containsEventField(changed, "status") {
		beforeStatus, _ := beforeState["status"].(string)
		afterStatus, _ := afterState["status"].(string)
		if afterStatus == "cancelled" {
			return "requirement_cancelled", "取消了需求"
		}
		if beforeStatus == "cancelled" && afterStatus != "cancelled" {
			return "requirement_restored", "恢复了需求"
		}
	}
	return requirementChangeEventType(changed), changeEventTitle("更新了需求", changed)
}

func taskChangeEventType(changed []string) string {
	return singleFieldEventType(changed, map[string]string{
		"status":               "task_status_changed",
		"progress":             "task_progress_changed",
		"responsible_user_ids": "task_responsibles_changed",
		"due_date":             "task_deadline_changed",
		"acceptance_criteria":  "task_ac_updated",
	}, "task_updated")
}

func taskChangeEvent(changed []string) (string, string) {
	if containsEventField(changed, "status") {
		return "task_status_changed", "更新了任务：状态"
	}
	return taskChangeEventType(changed), changeEventTitle("更新了任务", changed)
}

func containsEventField(fields []string, target string) bool {
	for _, field := range fields {
		if field == target {
			return true
		}
	}
	return false
}

func singleFieldEventType(changed []string, types map[string]string, fallback string) string {
	if len(changed) != 1 {
		return fallback
	}
	if value, ok := types[changed[0]]; ok {
		return value
	}
	return fallback
}

func loadWorkItemEventMetadata(db *sql.DB, itemType string, itemID string) map[string]any {
	metadata := map[string]any{
		"target_type": itemType,
		"target_id":   itemID,
	}
	switch itemType {
	case "task":
		var title, requirementID, requirementTitle string
		if err := db.QueryRow(`
			SELECT t.title, t.requirement_id::text, r.title
			FROM tasks t
			JOIN requirements r ON r.id = t.requirement_id
			WHERE t.id = $1`, itemID).Scan(&title, &requirementID, &requirementTitle); err == nil {
			metadata["target_title"] = title
			metadata["target_requirement_id"] = requirementID
			metadata["target_requirement_title"] = requirementTitle
		}
	case "requirement":
		var title string
		if err := db.QueryRow(`SELECT title FROM requirements WHERE id = $1`, itemID).Scan(&title); err == nil {
			metadata["target_title"] = title
		}
	}
	return metadata
}

func requirementEventData(req model.Requirement) map[string]any {
	return map[string]any{
		"title":                req.Title,
		"description":          req.Description,
		"feishu_doc_url":       stringValue(req.FeishuDocURL),
		"priority":             req.Priority,
		"status":               req.Status,
		"deadline":             stringValue(req.Deadline),
		"responsible_user_ids": req.ResponsibleUserIDs,
		"team_ids":             req.TeamIDs,
		"acceptance_criteria":  req.AcceptanceCriteria,
	}
}

func taskEventData(task model.Task) map[string]any {
	return map[string]any{
		"requirement_id":       task.RequirementID,
		"title":                task.Title,
		"acceptance_criteria":  task.AcceptanceCriteria,
		"responsible_user_ids": task.ResponsibleUserIDs,
		"status":               task.Status,
		"priority":             task.Priority,
		"progress":             task.Progress,
		"due_date":             stringValue(task.DueDate),
	}
}
