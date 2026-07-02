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
		status, err := h.validateDependency(u, req.RequirementID, "", depID)
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
		if _, err := h.db.Exec("INSERT INTO task_dependencies (task_id, depends_on_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", taskID, depID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	h.updateRequirementProgress(taskID)

	autoFollow(h.db, u.ID, "task", taskID)
	autoFollow(h.db, *req.AssigneeID, "task", taskID)

	writeJSON(w, http.StatusCreated, map[string]string{"id": taskID, "status": "created"})
}

func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
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

	if req.AssigneeID != nil && *req.AssigneeID != "" &&
		(!task.AssigneeID.Valid || task.AssigneeID.String != *req.AssigneeID) {
		autoFollow(h.db, *req.AssigneeID, "task", id)
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
	u := getUser(r)
	var req model.AddDependencyRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if !requireBaseVersion(w, req.BaseVersion) {
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

	status, err := h.validateDependencyTx(tx, u, task.RequirementID, taskID, req.DependsOnID)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}

	res, err := tx.Exec("INSERT INTO task_dependencies (task_id, depends_on_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", taskID, req.DependsOnID)
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
	u := getUser(r)
	baseVersion, ok := parseBaseVersionFromQuery(w, r)
	if !ok {
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

	res, err := tx.Exec("DELETE FROM task_dependencies WHERE task_id = $1 AND depends_on_id = $2", taskID, depID)
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
	rows, _ := h.db.Query(`
		SELECT td.depends_on_id, t.title, t.status
		FROM task_dependencies td
		JOIN tasks t ON t.id = td.depends_on_id
		WHERE td.task_id = $1`, t.ID)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var d model.TaskDep
			rows.Scan(&d.TaskID, &d.TaskTitle, &d.Status)
			t.Dependencies = append(t.Dependencies, d)
		}
	}

	rows, _ = h.db.Query(`
		SELECT td.task_id, t.title, t.status
		FROM task_dependencies td
		JOIN tasks t ON t.id = td.task_id
		WHERE td.depends_on_id = $1`, t.ID)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var d model.TaskDep
			rows.Scan(&d.TaskID, &d.TaskTitle, &d.Status)
			t.Blocking = append(t.Blocking, d)
		}
	}
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
		SELECT DISTINCT s.id
		FROM sessions s
		JOIN token_usage tu ON tu.session_id = s.id
		WHERE s.task_id = $1
		ORDER BY s.id`, t.ID)
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
	h.applyTaskPermissions(t, u)
}

func (h *TaskHandler) validateDependency(u *model.User, requirementID, taskID, dependsOnID string) (int, error) {
	if dependsOnID == "" {
		return http.StatusBadRequest, fmt.Errorf("depends_on_id is required")
	}
	if taskID != "" && taskID == dependsOnID {
		return http.StatusBadRequest, fmt.Errorf("task cannot depend on itself")
	}
	dependency, err := h.loadTaskAccess(dependsOnID)
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
	if dependency.RequirementID != requirementID {
		return http.StatusBadRequest, fmt.Errorf("dependencies must belong to the same requirement")
	}
	if taskID == "" {
		return http.StatusOK, nil
	}
	var createsCycle bool
	if err := h.db.QueryRow(`
			WITH RECURSIVE upstream(id) AS (
				SELECT depends_on_id FROM task_dependencies WHERE task_id = $1
				UNION
				SELECT td.depends_on_id
				FROM task_dependencies td
				JOIN upstream u ON td.task_id = u.id
			)
			SELECT EXISTS(SELECT 1 FROM upstream WHERE id = $2)`, dependsOnID, taskID).Scan(&createsCycle); err != nil {
		return http.StatusInternalServerError, err
	}
	if createsCycle {
		return http.StatusBadRequest, fmt.Errorf("dependency would create a cycle")
	}
	return http.StatusOK, nil
}

func (h *TaskHandler) validateDependencyTx(tx *sql.Tx, u *model.User, requirementID, taskID, dependsOnID string) (int, error) {
	if dependsOnID == "" {
		return http.StatusBadRequest, fmt.Errorf("depends_on_id is required")
	}
	if taskID != "" && taskID == dependsOnID {
		return http.StatusBadRequest, fmt.Errorf("task cannot depend on itself")
	}
	dependency, err := h.loadTaskAccessTx(tx, dependsOnID)
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
	if dependency.RequirementID != requirementID {
		return http.StatusBadRequest, fmt.Errorf("dependencies must belong to the same requirement")
	}
	if taskID == "" {
		return http.StatusOK, nil
	}
	var createsCycle bool
	if err := tx.QueryRow(`
			WITH RECURSIVE upstream(id) AS (
				SELECT depends_on_id FROM task_dependencies WHERE task_id = $1
				UNION
				SELECT td.depends_on_id
				FROM task_dependencies td
				JOIN upstream u ON td.task_id = u.id
			)
			SELECT EXISTS(SELECT 1 FROM upstream WHERE id = $2)`, dependsOnID, taskID).Scan(&createsCycle); err != nil {
		return http.StatusInternalServerError, err
	}
	if createsCycle {
		return http.StatusBadRequest, fmt.Errorf("dependency would create a cycle")
	}
	return http.StatusOK, nil
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
				WHERE assignee.id = $3 AND assignee.team_id = $2
			)`
	if u.Role == "team_leader" {
		query += ` OR $4 = $5`
		err := tx.QueryRow(query+`)`, task.RequirementID, *u.TeamID, task.AssigneeID, task.CreatorTLID, u.ID).Scan(&allowed)
		return allowed, err
	}
	err := tx.QueryRow(query+`)`, task.RequirementID, *u.TeamID, task.AssigneeID).Scan(&allowed)
	return allowed, err
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
