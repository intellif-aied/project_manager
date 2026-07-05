package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type TaskHandler struct {
	db *sql.DB
}

func NewTaskHandler(db *sql.DB) *TaskHandler {
	return &TaskHandler{db: db}
}

func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	query := `
		SELECT t.id, t.requirement_id, r.title as req_title, t.title,
			COALESCE(t.acceptance_criteria, ARRAY[]::text[]), t.assignee_id, COALESCE(COALESCE(NULLIF(a.nickname,''), a.username),''),
			t.creator_tl_id, t.status, t.priority, t.progress, t.due_date, t.completed_at,
			t.created_at, t.updated_at, t.version
		FROM tasks t
		JOIN requirements r ON r.id = t.requirement_id
		LEFT JOIN users a ON a.id = t.assignee_id
		WHERE 1=1`
	args := []any{}
	argIdx := 1

	if reqID := r.URL.Query().Get("requirement_id"); reqID != "" {
		query += fmt.Sprintf(" AND t.requirement_id = $%d", argIdx)
		args = append(args, reqID)
		argIdx++
	}

	if assignee := r.URL.Query().Get("assignee_id"); assignee != "" {
		query += fmt.Sprintf(" AND t.assignee_id = $%d", argIdx)
		args = append(args, assignee)
		argIdx++
	}

	if status := r.URL.Query().Get("status"); status != "" {
		query += fmt.Sprintf(" AND t.status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}

	switch u.Role {
	case "team_leader":
		if u.TeamID != nil {
			query += fmt.Sprintf(` AND (
				t.creator_tl_id = $%d
				OR t.assignee_id IN (SELECT id FROM users WHERE team_id = $%d)
				OR EXISTS (
					SELECT 1 FROM requirement_teams rt
					WHERE rt.requirement_id = t.requirement_id AND rt.team_id = $%d
				)
			)`, argIdx, argIdx+1, argIdx+2)
			args = append(args, u.ID, *u.TeamID, *u.TeamID)
			argIdx += 3
		} else {
			query += " AND 1=0"
		}
	case "employee":
		if u.TeamID != nil {
			query += fmt.Sprintf(` AND (
				t.assignee_id IN (SELECT id FROM users WHERE team_id = $%d)
				OR EXISTS (
					SELECT 1 FROM requirement_teams rt
					WHERE rt.requirement_id = t.requirement_id AND rt.team_id = $%d
				)
			)`, argIdx, argIdx+1)
			args = append(args, *u.TeamID, *u.TeamID)
			argIdx += 2
		} else {
			query += " AND 1=0"
		}
	}

	query += " ORDER BY t.created_at DESC"

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	tasks := []model.Task{}
	for rows.Next() {
		var t model.Task
		var ac pq.StringArray
		var dueDate sql.NullString
		var assigneeID sql.NullString
		var assigneeName sql.NullString
		var completedAt sql.NullTime
		if err := rows.Scan(&t.ID, &t.RequirementID, &t.RequirementTitle, &t.Title,
			&ac, &assigneeID, &assigneeName,
			&t.CreatorTLID, &t.Status, &t.Priority, &t.Progress, &dueDate, &completedAt,
			&t.CreatedAt, &t.UpdatedAt, &t.Version); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		t.AcceptanceCriteria = []string(ac)
		t.AssigneeID = nullStringPtr(assigneeID)
		t.AssigneeName = nullStringPtr(assigneeName)
		t.DueDate = nullStringPtr(dueDate)
		t.CompletedAt = nullTimePtr(completedAt)
		h.enrichTask(&t, u)
		tasks = append(tasks, t)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, tasks)
}

func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "task_id") {
		return
	}
	u := getUser(r)
	var t model.Task
	var ac pq.StringArray
	var dueDate sql.NullString
	var assigneeID sql.NullString
	var assigneeName sql.NullString
	var completedAt sql.NullTime

	err := h.db.QueryRow(`
		SELECT t.id, t.requirement_id, r.title, t.title,
			COALESCE(t.acceptance_criteria, ARRAY[]::text[]), t.assignee_id, COALESCE(COALESCE(NULLIF(a.nickname,''), a.username),''),
			t.creator_tl_id, t.status, t.priority, t.progress, t.due_date, t.completed_at,
			t.created_at, t.updated_at, t.version
		FROM tasks t
		JOIN requirements r ON r.id = t.requirement_id
		LEFT JOIN users a ON a.id = t.assignee_id
		WHERE t.id = $1`, id).Scan(
		&t.ID, &t.RequirementID, &t.RequirementTitle, &t.Title,
		&ac, &assigneeID, &assigneeName,
		&t.CreatorTLID, &t.Status, &t.Priority, &t.Progress, &dueDate, &completedAt,
		&t.CreatedAt, &t.UpdatedAt, &t.Version,
	)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allowed, permErr := h.canViewTask(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	t.AcceptanceCriteria = []string(ac)
	t.AssigneeID = nullStringPtr(assigneeID)
	t.AssigneeName = nullStringPtr(assigneeName)
	t.DueDate = nullStringPtr(dueDate)
	t.CompletedAt = nullTimePtr(completedAt)
	h.enrichTask(&t, u)
	writeJSON(w, http.StatusOK, t)
}

func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	var req model.CreateTaskRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if req.RequirementID == "" || req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "requirement_id and title required"})
		return
	}
	if !validateUUIDParam(w, req.RequirementID, "requirement_id") {
		return
	}
	if (req.AssigneeID == nil || *req.AssigneeID == "") && u != nil && u.Role == "employee" {
		req.AssigneeID = &u.ID
	}
	if req.AssigneeID == nil || *req.AssigneeID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "assignee_id is required"})
		return
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}

	allowed, permissionMessage, err := h.canCreateTask(u, req.RequirementID, req.AssigneeID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !allowed {
		status := http.StatusForbidden
		if permissionMessage == "requirement is cancelled" {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]string{"error": permissionMessage})
		return
	}
	for _, depID := range req.DependsOnIDs {
		status, err := h.validateWorkItemDependency(u, "task", "", "task", depID)
		if err != nil {
			writeJSON(w, status, map[string]string{"error": err.Error()})
			return
		}
	}

	var taskID string
	err = h.db.QueryRow(`
		INSERT INTO tasks (requirement_id, title, acceptance_criteria, assignee_id, creator_tl_id, priority, due_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		req.RequirementID, req.Title, pq.Array(req.AcceptanceCriteria),
		nullString(req.AssigneeID), u.ID, req.Priority, nullString(req.DueDate),
	).Scan(&taskID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for _, depID := range req.DependsOnIDs {
		if _, err := h.db.Exec(`
			INSERT INTO work_item_relations (source_type, source_id, target_type, target_id, relation_type, created_by)
			VALUES ('task', $1, 'task', $2, 'depends_on', CAST($3 AS bigint))
			ON CONFLICT DO NOTHING`, taskID, depID, u.ID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	h.updateRequirementProgress(taskID)

	writeJSON(w, http.StatusCreated, map[string]string{"id": taskID, "status": "created"})
}

func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "task_id") {
		return
	}
	u := getUser(r)
	var req model.UpdateTaskRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Status != nil && !isStoredTaskStatus(*req.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status; blocked is derived from dependencies"})
		return
	}
	if req.Progress != nil && (*req.Progress < 0 || *req.Progress > 100) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "progress must be between 0 and 100"})
		return
	}
	if !requireBaseVersion(w, req.BaseVersion) {
		return
	}
	task, err := h.loadTaskAccess(id)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allowed, permErr := h.canManageTask(u, task)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		visible, viewErr := h.canViewTaskRecord(u, task)
		if viewErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": viewErr.Error()})
			return
		}
		if !visible {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions to update task"})
		return
	}
	if req.AssigneeID != nil && taskAssigneeChanged(task.AssigneeID, req.AssigneeID) {
		reassignAllowed, message, err := h.canReassignTask(u, req.AssigneeID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !reassignAllowed {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": message})
			return
		}
	}

	sets := []string{}
	args := []any{}
	argIdx := 1

	if req.Title != nil {
		sets = append(sets, fmt.Sprintf("title = $%d", argIdx))
		args = append(args, *req.Title)
		argIdx++
	}
	if req.AssigneeID != nil {
		sets = append(sets, fmt.Sprintf("assignee_id = $%d", argIdx))
		args = append(args, nullString(req.AssigneeID))
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
		if *req.Status == "done" {
			sets = append(sets, "progress = 100", "completed_at = now()")
		} else {
			sets = append(sets, "completed_at = NULL")
		}
	}
	if req.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *req.Priority)
		argIdx++
	}
	if req.DueDate != nil {
		sets = append(sets, fmt.Sprintf("due_date = $%d", argIdx))
		args = append(args, nullString(req.DueDate))
		argIdx++
	}
	if req.AcceptanceCriteria != nil {
		sets = append(sets, fmt.Sprintf("acceptance_criteria = $%d", argIdx))
		args = append(args, pq.Array(*req.AcceptanceCriteria))
		argIdx++
	}
	if req.Progress != nil && (req.Status == nil || *req.Status != "done") {
		sets = append(sets, fmt.Sprintf("progress = $%d", argIdx))
		args = append(args, *req.Progress)
		argIdx++
	}

	if len(sets) == 0 {
		writeNoFieldsToUpdate(w)
		return
	}

	sets = append(sets, "version = version + 1", "updated_at = now()")
	args = append(args, id, req.BaseVersion)
	query := fmt.Sprintf("UPDATE tasks SET %s WHERE id = $%d AND version = $%d", strings.Join(sets, ", "), argIdx, argIdx+1)

	res, err := h.db.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if rows, _ := res.RowsAffected(); rows == 0 {
		writeTaskNotFoundOrConflict(w, h.db, id)
		return
	}

	h.updateRequirementProgress(id)

	h.Get(w, r)
}

func taskAssigneeChanged(current sql.NullString, next *string) bool {
	if next == nil {
		return false
	}
	if !current.Valid {
		return *next != ""
	}
	return current.String != *next
}

func (h *TaskHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "task_id") {
		return
	}
	u := getUser(r)
	var req model.UpdateTaskStatusRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if !isStoredTaskStatus(req.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid status; blocked is derived from dependencies"})
		return
	}
	if !requireBaseVersion(w, req.BaseVersion) {
		return
	}
	task, err := h.loadTaskAccess(id)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allowed, permErr := h.canManageTask(u, task)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		visible, viewErr := h.canViewTaskRecord(u, task)
		if viewErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": viewErr.Error()})
			return
		}
		if !visible {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions to update task status"})
		return
	}

	res, err := h.db.Exec(`
		UPDATE tasks
		SET status = $1,
			progress = CASE WHEN $1 = 'done' THEN 100 ELSE progress END,
			completed_at = CASE WHEN $1 = 'done' THEN now() ELSE NULL END,
			version = version + 1,
			updated_at = now()
		WHERE id = $2 AND version = $3`, req.Status, id, req.BaseVersion)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeTaskNotFoundOrConflict(w, h.db, id)
		return
	}

	h.updateRequirementProgress(id)

	h.Get(w, r)
}

func (h *TaskHandler) UpdateProgress(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "task_id") {
		return
	}
	u := getUser(r)
	var req model.UpdateTaskProgressRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Progress < 0 || req.Progress > 100 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "progress must be between 0 and 100"})
		return
	}
	if !requireBaseVersion(w, req.BaseVersion) {
		return
	}
	task, err := h.loadTaskAccess(id)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allowed, permErr := h.canManageTask(u, task)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		visible, viewErr := h.canViewTaskRecord(u, task)
		if viewErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": viewErr.Error()})
			return
		}
		if !visible {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions to update task progress"})
		return
	}
	res, err := h.db.Exec("UPDATE tasks SET progress = $1, version = version + 1, updated_at = now() WHERE id = $2 AND version = $3", req.Progress, id, req.BaseVersion)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeTaskNotFoundOrConflict(w, h.db, id)
		return
	}
	h.updateRequirementProgress(id)
	h.Get(w, r)
}

func (h *TaskHandler) AddDependency(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	if !validateUUIDParam(w, taskID, "task_id") {
		return
	}
	u := getUser(r)
	var req model.AddDependencyRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if !requireBaseVersion(w, req.BaseVersion) {
		return
	}
	targetType, typeErr := normalizeWorkItemType(req.DependsOnType)
	if typeErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": typeErr.Error()})
		return
	}

	task, err := h.loadTaskAccess(taskID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allowed, permErr := h.canManageTask(u, task)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		visible, viewErr := h.canViewTaskRecord(u, task)
		if viewErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": viewErr.Error()})
			return
		}
		if !visible {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions to update task dependencies"})
		return
	}
	tx, err := h.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var currentVersion int64
	if err := tx.QueryRow(`SELECT version FROM tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&currentVersion); err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if currentVersion != req.BaseVersion {
		writeEditConflict(w, currentVersion)
		return
	}

	status, err := h.validateWorkItemDependencyTx(tx, u, "task", taskID, targetType, req.DependsOnID)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	res, err := tx.Exec(`
		INSERT INTO work_item_relations (source_type, source_id, target_type, target_id, relation_type, created_by)
		VALUES ('task', $1, $2, $3, 'depends_on', CAST($4 AS bigint))
		ON CONFLICT DO NOTHING`, taskID, targetType, req.DependsOnID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		if _, err := tx.Exec("UPDATE tasks SET version = version + 1, updated_at = now() WHERE id = $1", taskID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Get(w, r)
}

func (h *TaskHandler) RemoveDependency(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "id")
	depID := chi.URLParam(r, "dep_id")
	if !validateUUIDParam(w, taskID, "task_id") || !validateUUIDParam(w, depID, "dependency_id") {
		return
	}
	u := getUser(r)
	baseVersion, ok := parseBaseVersionFromQuery(w, r)
	if !ok {
		return
	}
	targetType, typeErr := normalizeWorkItemType(r.URL.Query().Get("depends_on_type"))
	if typeErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": typeErr.Error()})
		return
	}
	task, err := h.loadTaskAccess(taskID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allowed, permErr := h.canManageTask(u, task)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		visible, viewErr := h.canViewTaskRecord(u, task)
		if viewErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": viewErr.Error()})
			return
		}
		if !visible {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions to update task dependencies"})
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var currentVersion int64
	if err := tx.QueryRow(`SELECT version FROM tasks WHERE id = $1 FOR UPDATE`, taskID).Scan(&currentVersion); err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if currentVersion != baseVersion {
		writeEditConflict(w, currentVersion)
		return
	}

	res, err := tx.Exec(`
		DELETE FROM work_item_relations
		WHERE source_type = 'task'
		  AND source_id = $1
		  AND target_type = $2
		  AND target_id = $3
		  AND relation_type = 'depends_on'`, taskID, targetType, depID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		if _, err := tx.Exec("UPDATE tasks SET version = version + 1, updated_at = now() WHERE id = $1", taskID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.Get(w, r)
}

func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "task_id") {
		return
	}
	u := getUser(r)
	baseVersion, ok := parseBaseVersionFromQuery(w, r)
	if !ok {
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var requirementID string
	var creatorTL string
	var assigneeID sql.NullString
	var currentVersion int64
	err = tx.QueryRow(`SELECT requirement_id, creator_tl_id, assignee_id, version FROM tasks WHERE id = $1 FOR UPDATE`, id).
		Scan(&requirementID, &creatorTL, &assigneeID, &currentVersion)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	allowed, permErr := h.canManageTask(u, taskAccessRecord{
		ID:            id,
		RequirementID: requirementID,
		AssigneeID:    assigneeID,
		CreatorTLID:   creatorTL,
	})
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		visible, viewErr := h.canViewTaskRecord(u, taskAccessRecord{
			ID:            id,
			RequirementID: requirementID,
			AssigneeID:    assigneeID,
			CreatorTLID:   creatorTL,
		})
		if viewErr != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": viewErr.Error()})
			return
		}
		if !visible {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions to delete tasks"})
		return
	}
	if currentVersion != baseVersion {
		writeEditConflict(w, currentVersion)
		return
	}

	if _, err := tx.Exec(`SELECT id FROM requirements WHERE id = $1 FOR UPDATE`, requirementID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if _, err := tx.Exec(`UPDATE sessions SET task_id = NULL WHERE task_id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`UPDATE token_usage SET task_id = NULL WHERE task_id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`UPDATE documents SET task_id = NULL WHERE task_id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM user_follows WHERE target_type = 'task' AND target_id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`
		DELETE FROM work_item_relations
		WHERE (source_type = 'task' AND source_id = $1)
		   OR (target_type = 'task' AND target_id = $1)`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM tasks WHERE id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`
		UPDATE requirements
		SET progress = COALESCE((
			SELECT FLOOR(AVG(progress))::int FROM tasks WHERE requirement_id = $1
		), 0), updated_at = now()
		WHERE id = $1`, requirementID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

func (h *TaskHandler) loadDeps(t *model.Task) {
	t.Dependencies, t.Blocking = h.loadWorkItemRelations("task", t.ID)
}

func (h *TaskHandler) enrichTask(t *model.Task, u *model.User) {
	h.loadDeps(t)
	t.RiskTypes = service.DeriveTaskRisks(*t, time.Now())
	t.DisplayStatus = service.DisplayTaskStatus(*t)
	if t.RiskTypes == nil {
		t.RiskTypes = []string{}
	}
	userID := ""
	if u != nil {
		userID = u.ID
	}
	rows, err := h.db.Query(`
		SELECT source_id
		FROM (
			SELECT DISTINCT sas.session_id::text || ':' || sas.activity_date::text AS source_id
			FROM session_activity_slices sas
			WHERE sas.task_id = $1
			UNION
			SELECT DISTINCT s.id::text AS source_id
			FROM sessions s
			JOIN token_usage tu ON tu.session_id = s.id
			WHERE s.task_id = $1
			  AND NOT EXISTS (SELECT 1 FROM session_activity_slices sas WHERE sas.session_id = s.id)
		) sources
		ORDER BY source_id`, t.ID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil {
				t.TokenSourceIDs = append(t.TokenSourceIDs, id)
			}
		}
	}
	_ = h.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM user_follows
			WHERE user_id = $1 AND target_type = 'task' AND target_id = $2
		)`, userID, t.ID).Scan(&t.IsFollowed)
	attention := loadFollowAttention(h.db, "task", t.ID)
	t.FollowSummary = model.RequirementFollowSummary{
		Count: attention.count,
		Score: attention.score,
		Level: attentionLevel(attention.score),
	}
	h.applyTaskPermissions(t, u)
}

func (h *TaskHandler) loadWorkItemRelations(itemType, itemID string) ([]model.TaskDep, []model.TaskDep) {
	dependencies := h.loadRelationSide(`
		SELECT rel.target_type,
			rel.target_id::text,
			COALESCE(target_task.title, target_req.title, '已删除工作项') AS title,
			target_task.id::text,
			target_task.title,
			COALESCE(target_task.requirement_id::text, target_req.id::text, '') AS requirement_id,
			COALESCE(target_task_req.title, target_req.title, '') AS requirement_title,
			COALESCE(target_task.status, target_req.status, '') AS status,
			CASE
				WHEN rel.target_type = 'task' THEN target_task.due_date::text
				ELSE target_req.deadline::text
			END AS due_date
		FROM work_item_relations rel
		LEFT JOIN tasks target_task ON rel.target_type = 'task' AND target_task.id = rel.target_id
		LEFT JOIN requirements target_task_req ON target_task_req.id = target_task.requirement_id
		LEFT JOIN requirements target_req ON rel.target_type = 'requirement' AND target_req.id = rel.target_id
		WHERE rel.source_type = $1 AND rel.source_id = $2 AND rel.relation_type = 'depends_on'
		ORDER BY rel.created_at`, itemType, itemID)
	blocking := h.loadRelationSide(`
		SELECT rel.source_type,
			rel.source_id::text,
			COALESCE(source_task.title, source_req.title, '已删除工作项') AS title,
			source_task.id::text,
			source_task.title,
			COALESCE(source_task.requirement_id::text, source_req.id::text, '') AS requirement_id,
			COALESCE(source_task_req.title, source_req.title, '') AS requirement_title,
			COALESCE(source_task.status, source_req.status, '') AS status,
			CASE
				WHEN rel.source_type = 'task' THEN source_task.due_date::text
				ELSE source_req.deadline::text
			END AS due_date
		FROM work_item_relations rel
		LEFT JOIN tasks source_task ON rel.source_type = 'task' AND source_task.id = rel.source_id
		LEFT JOIN requirements source_task_req ON source_task_req.id = source_task.requirement_id
		LEFT JOIN requirements source_req ON rel.source_type = 'requirement' AND source_req.id = rel.source_id
		WHERE rel.target_type = $1 AND rel.target_id = $2 AND rel.relation_type = 'depends_on'
		ORDER BY rel.created_at`, itemType, itemID)
	return dependencies, blocking
}

func (h *TaskHandler) loadRelationSide(query, itemType, itemID string) []model.TaskDep {
	rows, err := h.db.Query(query, itemType, itemID)
	if err != nil {
		return []model.TaskDep{}
	}
	defer rows.Close()

	items := []model.TaskDep{}
	for rows.Next() {
		var d model.TaskDep
		var dueDate sql.NullString
		var taskID, taskTitle, requirementID, requirementTitle sql.NullString
		if err := rows.Scan(
			&d.ItemType,
			&d.ItemID,
			&d.Title,
			&taskID,
			&taskTitle,
			&requirementID,
			&requirementTitle,
			&d.Status,
			&dueDate,
		); err != nil {
			continue
		}
		d.TaskID = nullStringValue(taskID)
		d.TaskTitle = nullStringValue(taskTitle)
		d.RequirementID = nullStringValue(requirementID)
		d.RequirementTitle = nullStringValue(requirementTitle)
		d.DueDate = nullStringPtr(dueDate)
		if d.ItemType == "task" {
			d.TaskID = d.ItemID
			if d.TaskTitle == "" {
				d.TaskTitle = d.Title
			}
		}
		items = append(items, d)
	}
	return items
}

func (h *TaskHandler) validateWorkItemDependency(u *model.User, sourceType, sourceID, targetType, targetID string) (int, error) {
	if targetID == "" {
		return http.StatusBadRequest, fmt.Errorf("depends_on_id is required")
	}
	if !isValidUUID(targetID) {
		return http.StatusBadRequest, fmt.Errorf("invalid depends_on_id")
	}
	if sourceID != "" && sourceType == targetType && sourceID == targetID {
		return http.StatusBadRequest, fmt.Errorf("work item cannot depend on itself")
	}
	if status, err := h.validateWorkItemVisibility(u, targetType, targetID); err != nil {
		return status, err
	}
	if sourceID == "" {
		return http.StatusOK, nil
	}
	var createsCycle bool
	if err := h.db.QueryRow(`
			WITH RECURSIVE upstream(source_type, source_id, target_type, target_id) AS (
				SELECT source_type, source_id, target_type, target_id
				FROM work_item_relations
				WHERE source_type = $1 AND source_id = $2 AND relation_type = 'depends_on'
				UNION
				SELECT wir.source_type, wir.source_id, wir.target_type, wir.target_id
				FROM work_item_relations wir
				JOIN upstream u ON wir.source_type = u.target_type AND wir.source_id = u.target_id
				WHERE wir.relation_type = 'depends_on'
			)
			SELECT EXISTS(
				SELECT 1 FROM upstream WHERE target_type = $3 AND target_id = $4
			)`, targetType, targetID, sourceType, sourceID).Scan(&createsCycle); err != nil {
		return http.StatusInternalServerError, err
	}
	if createsCycle {
		return http.StatusBadRequest, fmt.Errorf("dependency would create a cycle")
	}
	return http.StatusOK, nil
}

func (h *TaskHandler) validateWorkItemVisibility(u *model.User, targetType, targetID string) (int, error) {
	switch targetType {
	case "task":
		dependency, err := h.loadTaskAccess(targetID)
		if err == sql.ErrNoRows {
			return http.StatusNotFound, fmt.Errorf("dependency task not found")
		}
		if err != nil {
			return http.StatusInternalServerError, err
		}
		visible, err := h.canViewTaskRecord(u, dependency)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if !visible {
			return http.StatusNotFound, fmt.Errorf("dependency task not found")
		}
		return http.StatusOK, nil
	case "requirement":
		visible, err := NewRequirementHandler(h.db, nil).canViewRequirement(u, targetID)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if !visible {
			return http.StatusNotFound, fmt.Errorf("dependency requirement not found")
		}
		return http.StatusOK, nil
	default:
		return http.StatusBadRequest, fmt.Errorf("invalid depends_on_type")
	}
}

func (h *TaskHandler) validateWorkItemDependencyTx(tx *sql.Tx, u *model.User, sourceType, sourceID, targetType, targetID string) (int, error) {
	if targetID == "" {
		return http.StatusBadRequest, fmt.Errorf("depends_on_id is required")
	}
	if !isValidUUID(targetID) {
		return http.StatusBadRequest, fmt.Errorf("invalid depends_on_id")
	}
	if sourceID != "" && sourceType == targetType && sourceID == targetID {
		return http.StatusBadRequest, fmt.Errorf("work item cannot depend on itself")
	}
	if status, err := h.validateWorkItemVisibilityTx(tx, u, targetType, targetID); err != nil {
		return status, err
	}
	if sourceID == "" {
		return http.StatusOK, nil
	}
	var createsCycle bool
	if err := tx.QueryRow(`
			WITH RECURSIVE upstream(source_type, source_id, target_type, target_id) AS (
				SELECT source_type, source_id, target_type, target_id
				FROM work_item_relations
				WHERE source_type = $1 AND source_id = $2 AND relation_type = 'depends_on'
				UNION
				SELECT wir.source_type, wir.source_id, wir.target_type, wir.target_id
				FROM work_item_relations wir
				JOIN upstream u ON wir.source_type = u.target_type AND wir.source_id = u.target_id
				WHERE wir.relation_type = 'depends_on'
			)
			SELECT EXISTS(
				SELECT 1 FROM upstream WHERE target_type = $3 AND target_id = $4
			)`, targetType, targetID, sourceType, sourceID).Scan(&createsCycle); err != nil {
		return http.StatusInternalServerError, err
	}
	if createsCycle {
		return http.StatusBadRequest, fmt.Errorf("dependency would create a cycle")
	}
	return http.StatusOK, nil
}

func (h *TaskHandler) validateWorkItemVisibilityTx(tx *sql.Tx, u *model.User, targetType, targetID string) (int, error) {
	switch targetType {
	case "task":
		dependency, err := h.loadTaskAccessTx(tx, targetID)
		if err == sql.ErrNoRows {
			return http.StatusNotFound, fmt.Errorf("dependency task not found")
		}
		if err != nil {
			return http.StatusInternalServerError, err
		}
		visible, err := h.canViewTaskRecordTx(tx, u, dependency)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if !visible {
			return http.StatusNotFound, fmt.Errorf("dependency task not found")
		}
		return http.StatusOK, nil
	case "requirement":
		visible, err := h.canViewRequirementTx(tx, u, targetID)
		if err != nil {
			return http.StatusInternalServerError, err
		}
		if !visible {
			return http.StatusNotFound, fmt.Errorf("dependency requirement not found")
		}
		return http.StatusOK, nil
	default:
		return http.StatusBadRequest, fmt.Errorf("invalid depends_on_type")
	}
}

func normalizeWorkItemType(value string) (string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "task", nil
	}
	if trimmed != "task" && trimmed != "requirement" {
		return "", fmt.Errorf("invalid depends_on_type")
	}
	return trimmed, nil
}

func (h *TaskHandler) loadTaskAccessTx(tx *sql.Tx, taskID string) (taskAccessRecord, error) {
	var task taskAccessRecord
	err := tx.QueryRow(`
		SELECT id, requirement_id, assignee_id, creator_tl_id
		FROM tasks
		WHERE id = $1`, taskID).Scan(&task.ID, &task.RequirementID, &task.AssigneeID, &task.CreatorTLID)
	return task, err
}

func (h *TaskHandler) canViewTaskRecordTx(tx *sql.Tx, u *model.User, task taskAccessRecord) (bool, error) {
	if u == nil {
		return false, nil
	}
	if isGlobalTaskManager(u.Role) {
		return true, nil
	}
	if !hasTeam(u) {
		return false, nil
	}
	var allowed bool
	query := `
		SELECT EXISTS(
			SELECT 1
			WHERE EXISTS (
				SELECT 1 FROM requirement_teams rt
				WHERE rt.requirement_id = $1 AND rt.team_id = $2
			)
		OR EXISTS (
			SELECT 1 FROM users assignee
			WHERE assignee.id = CAST($3 AS bigint) AND assignee.team_id = $2
		)`
	if u.Role == "team_leader" {
		query += ` OR CAST($4 AS bigint) = CAST($5 AS bigint)`
		err := tx.QueryRow(query+`)`, task.RequirementID, *u.TeamID, task.AssigneeID, task.CreatorTLID, u.ID).Scan(&allowed)
		return allowed, err
	}
	err := tx.QueryRow(query+`)`, task.RequirementID, *u.TeamID, task.AssigneeID).Scan(&allowed)
	return allowed, err
}

func (h *TaskHandler) canViewRequirementTx(tx *sql.Tx, u *model.User, requirementID string) (bool, error) {
	if u == nil {
		return false, nil
	}
	if isGlobalRequirementManager(u.Role) {
		var exists bool
		err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM requirements WHERE id = $1)`, requirementID).Scan(&exists)
		return exists, err
	}
	var allowed bool
	query := `
		SELECT EXISTS(
			SELECT 1 FROM requirements r
			WHERE r.id = $1
			  AND (r.creator_id = CAST($2 AS bigint) OR r.owner_id = CAST($2 AS bigint) OR EXISTS (
				SELECT 1 FROM requirement_owners ro
				WHERE ro.requirement_id = r.id AND ro.user_id = CAST($2 AS bigint)
			  )`
	args := []any{requirementID, u.ID}
	if hasTeam(u) {
		query += ` OR EXISTS (
				SELECT 1 FROM requirement_teams rt
				WHERE rt.requirement_id = r.id AND rt.team_id = $3
			  )`
		args = append(args, *u.TeamID)
	}
	query += `))`
	return allowed, tx.QueryRow(query, args...).Scan(&allowed)
}

func (h *TaskHandler) updateRequirementProgress(taskID string) {
	var reqID string
	_ = h.db.QueryRow("SELECT requirement_id FROM tasks WHERE id = $1", taskID).Scan(&reqID)
	if reqID == "" {
		return
	}
	_, _ = h.db.Exec(`
		UPDATE requirements
		SET progress = COALESCE((
			SELECT FLOOR(AVG(progress))::int FROM tasks WHERE requirement_id = $1
		), 0), updated_at = now()
	WHERE id = $1`, reqID)
}

func isStoredTaskStatus(status string) bool {
	return status == "todo" || status == "in_progress" || status == "done"
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
