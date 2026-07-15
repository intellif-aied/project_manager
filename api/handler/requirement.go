package handler

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type RequirementHandler struct {
	db            *sql.DB
	ai            *service.AIClient
	eventRecorder *service.WorkItemEventRecorder
}

func NewRequirementHandler(db *sql.DB, ai *service.AIClient) *RequirementHandler {
	return &RequirementHandler{db: db, ai: ai}
}

func NewRequirementHandlerWithRecorder(db *sql.DB, ai *service.AIClient, recorder *service.WorkItemEventRecorder) *RequirementHandler {
	return &RequirementHandler{db: db, ai: ai, eventRecorder: recorder}
}

func (h *RequirementHandler) List(w http.ResponseWriter, r *http.Request) {
	if shouldUseRequirementPagedResponse(r) {
		h.listRequirementsPaged(w, r)
		return
	}

	u := getUser(r)
	query := `
		SELECT r.id, r.title, r.description, r.feishu_doc_url, r.acceptance_criteria,
			r.creator_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), r.creator_role, r.status, r.priority,
			r.progress, r.deadline, r.completed_at, r.created_at, r.updated_at, r.version
		FROM requirements r
		JOIN users u ON u.id = r.creator_id
		WHERE 1=1`
	args := []any{}
	argIdx := 1

	if status := r.URL.Query().Get("status"); status != "" {
		query += fmt.Sprintf(" AND r.status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}

	if teamID := r.URL.Query().Get("team_id"); teamID != "" {
		query += fmt.Sprintf(" AND EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id = $%d)", argIdx)
		args = append(args, teamID)
		argIdx++
	}

	if u.Role == "team_leader" && u.TeamID != nil {
		query += fmt.Sprintf(" AND (r.creator_id = $%d OR EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id = $%d) OR EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id = $%d))", argIdx, argIdx, argIdx+1)
		args = append(args, u.ID, *u.TeamID)
		argIdx += 2
	} else if u.Role == "employee" && u.TeamID != nil {
		query += fmt.Sprintf(" AND (EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id = $%d) OR r.creator_id = $%d OR EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id = $%d))", argIdx, argIdx, argIdx+1)
		args = append(args, u.ID, *u.TeamID)
		argIdx += 2
	} else if u.Role == "employee" {
		query += fmt.Sprintf(" AND (EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id = $%d) OR r.creator_id = $%d)", argIdx, argIdx)
		args = append(args, u.ID)
		argIdx++
	}

	query += " ORDER BY r.created_at DESC"

	rows, err := h.db.Query(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()

	reqs := []model.Requirement{}
	for rows.Next() {
		var req model.Requirement
		var acStr string
		var deadline sql.NullString
		var feishuURL sql.NullString
		var completedAt sql.NullTime
		if err := rows.Scan(&req.ID, &req.Title, &req.Description, &feishuURL, &acStr,
			&req.CreatorID, &req.CreatorName, &req.CreatorRole, &req.Status, &req.Priority,
			&req.Progress, &deadline, &completedAt, &req.CreatedAt, &req.UpdatedAt, &req.Version); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		req.FeishuDocURL = nullStringPtr(feishuURL)
		req.Deadline = nullStringPtr(deadline)
		req.AcceptanceCriteria = parseTextArray(acStr)
		req.CompletedAt = nullTimePtr(completedAt)
		reqs = append(reqs, req)
	}
	if err := rows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for i := range reqs {
		h.loadTeams(&reqs[i])
		h.loadResponsibles(&reqs[i])
		h.loadProjection(&reqs[i], u)
	}

	writeJSON(w, http.StatusOK, reqs)
}

func shouldUseRequirementPagedResponse(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("view") != "" || q.Get("page") != "" || q.Get("page_size") != "" || q.Get("scope") != "" || q.Get("keyword") != "" || q.Get("priority") != "" || q.Get("risk") != ""
}

func (h *RequirementHandler) listRequirementsPaged(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("view") == "board" {
		h.listRequirementBoard(w, r)
		return
	}
	page, pageSize := parsePagination(r, 20, 100)
	items, total, err := h.listRequirementPage(r, getUser(r), page, pageSize, "")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.PaginatedRequirements{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

func (h *RequirementHandler) listRequirementBoard(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	columnPageSize := parsePositiveInt(r.URL.Query().Get("column_page_size"), 20)
	if columnPageSize <= 0 {
		columnPageSize = 20
	}
	if columnPageSize > 100 {
		columnPageSize = 100
	}
	statuses := []string{"todo", "review", "active", "completed"}
	if status := r.URL.Query().Get("status"); status != "" {
		statuses = []string{status}
	}
	columns := make([]model.RequirementBoardColumn, 0, len(statuses))
	total := 0
	for _, status := range statuses {
		items, columnTotal, err := h.listRequirementPage(r, u, 1, columnPageSize, status)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		total += columnTotal
		columns = append(columns, model.RequirementBoardColumn{
			Status:   status,
			Items:    items,
			Total:    columnTotal,
			Page:     1,
			PageSize: columnPageSize,
			HasMore:  columnTotal > len(items),
		})
	}
	writeJSON(w, http.StatusOK, model.RequirementBoardResponse{
		Columns:        columns,
		Total:          total,
		ColumnPageSize: columnPageSize,
	})
}

func (h *RequirementHandler) listRequirementPage(r *http.Request, u *model.User, page, pageSize int, statusOverride string) ([]model.Requirement, int, error) {
	where, args := h.requirementListWhere(r, u, statusOverride)
	from := `
		FROM requirements r
		JOIN users u ON u.id = r.creator_id`
	whereSQL := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := h.db.QueryRow("SELECT COUNT(*) "+from+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT r.id, r.title, r.description, r.feishu_doc_url, r.acceptance_criteria,
			r.creator_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), r.creator_role, r.status, r.priority,
			r.progress, r.deadline, r.completed_at, r.created_at, r.updated_at, r.version` + from + whereSQL +
		fmt.Sprintf(" ORDER BY r.updated_at DESC, r.created_at DESC, r.id DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	queryArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := h.db.Query(query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	reqs, scanErr := scanRequirementRows(rows)
	if scanErr != nil {
		return nil, 0, scanErr
	}
	for i := range reqs {
		h.loadTeams(&reqs[i])
		h.loadResponsibles(&reqs[i])
		h.loadProjection(&reqs[i], u)
	}
	return reqs, total, nil
}

func scanRequirementRows(rows *sql.Rows) ([]model.Requirement, error) {
	reqs := []model.Requirement{}
	for rows.Next() {
		var req model.Requirement
		var acStr string
		var deadline sql.NullString
		var feishuURL sql.NullString
		var completedAt sql.NullTime
		if err := rows.Scan(&req.ID, &req.Title, &req.Description, &feishuURL, &acStr,
			&req.CreatorID, &req.CreatorName, &req.CreatorRole, &req.Status, &req.Priority,
			&req.Progress, &deadline, &completedAt, &req.CreatedAt, &req.UpdatedAt, &req.Version); err != nil {
			return nil, err
		}
		req.FeishuDocURL = nullStringPtr(feishuURL)
		req.Deadline = nullStringPtr(deadline)
		req.AcceptanceCriteria = parseTextArray(acStr)
		req.CompletedAt = nullTimePtr(completedAt)
		reqs = append(reqs, req)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reqs, nil
}

func (h *RequirementHandler) requirementListWhere(r *http.Request, u *model.User, statusOverride string) ([]string, []any) {
	q := r.URL.Query()
	where := []string{"1=1"}
	args := []any{}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	status := statusOverride
	if status == "" {
		status = q.Get("status")
	}
	if status != "" {
		where = append(where, "r.status = "+addArg(status))
	} else {
		where = append(where, "r.status <> 'cancelled'")
	}

	if teamID := q.Get("team_id"); teamID != "" {
		where = append(where, fmt.Sprintf("EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id = %s)", addArg(teamID)))
	}
	if priority := q.Get("priority"); priority != "" {
		where = append(where, "r.priority = "+addArg(priority))
	}
	if keyword := strings.TrimSpace(q.Get("keyword")); keyword != "" {
		pattern := "%" + keyword + "%"
		placeholder := addArg(pattern)
		where = append(where, fmt.Sprintf(`(
			r.title ILIKE %[1]s
			OR r.description ILIKE %[1]s
			OR r.acceptance_criteria::text ILIKE %[1]s
			OR COALESCE(NULLIF(u.nickname,''), u.username) ILIKE %[1]s
			OR EXISTS (
				SELECT 1 FROM requirement_responsibles rr
				JOIN users ru ON ru.id = rr.user_id
				WHERE rr.requirement_id = r.id AND COALESCE(NULLIF(ru.nickname,''), ru.username) ILIKE %[1]s
			)
			OR EXISTS (
				SELECT 1 FROM requirement_teams rt
				JOIN teams t ON t.id = rt.team_id
				WHERE rt.requirement_id = r.id AND t.name ILIKE %[1]s
			)
			OR EXISTS (
				SELECT 1 FROM tasks task
				LEFT JOIN task_responsibles tr ON tr.task_id = task.id
				LEFT JOIN users tu ON tu.id = tr.user_id
				WHERE task.requirement_id = r.id
				  AND (task.title ILIKE %[1]s OR COALESCE(NULLIF(tu.nickname,''), tu.username) ILIKE %[1]s)
			)
		)`, placeholder))
	}

	h.appendRequirementRoleFilter(&where, &args, u)
	h.appendRequirementScopeFilter(&where, &args, u, q.Get("scope"))
	h.appendRequirementRiskFilter(&where, &args, q.Get("risk"))
	return where, args
}

func (h *RequirementHandler) appendRequirementRoleFilter(where *[]string, args *[]any, u *model.User) {
	addArg := func(value any) string {
		*args = append(*args, value)
		return fmt.Sprintf("$%d", len(*args))
	}
	switch {
	case u.Role == "team_leader" && u.TeamID != nil:
		userID := addArg(u.ID)
		teamID := addArg(*u.TeamID)
		*where = append(*where, fmt.Sprintf(`(
			r.creator_id = %s
			OR EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id = %s)
			OR EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id = %s)
		)`, userID, userID, teamID))
	case u.Role == "employee" && u.TeamID != nil:
		userID := addArg(u.ID)
		teamID := addArg(*u.TeamID)
		*where = append(*where, fmt.Sprintf(`(
			EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id = %s)
			OR r.creator_id = %s
			OR EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id = %s)
		)`, userID, userID, teamID))
	case u.Role == "employee":
		userID := addArg(u.ID)
		*where = append(*where, fmt.Sprintf(`(
			EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id = %s)
			OR r.creator_id = %s
		)`, userID, userID))
	}
}

func (h *RequirementHandler) appendRequirementScopeFilter(where *[]string, args *[]any, u *model.User, scope string) {
	if scope == "" || scope == "all" {
		return
	}
	addArg := func(value any) string {
		*args = append(*args, value)
		return fmt.Sprintf("$%d", len(*args))
	}
	userID := addArg(u.ID)
	reqFollow := fmt.Sprintf("EXISTS (SELECT 1 FROM user_follows f WHERE f.user_id = %s AND f.target_type = 'requirement' AND f.target_id = r.id)", userID)
	taskFollow := fmt.Sprintf("EXISTS (SELECT 1 FROM tasks task JOIN user_follows f ON f.target_type = 'task' AND f.target_id = task.id WHERE task.requirement_id = r.id AND f.user_id = %s)", userID)
	reqAssigned := fmt.Sprintf("EXISTS (SELECT 1 FROM requirement_responsibles rr WHERE rr.requirement_id = r.id AND rr.user_id = %s)", userID)
	taskAssigned := fmt.Sprintf("EXISTS (SELECT 1 FROM tasks task JOIN task_responsibles tr ON tr.task_id = task.id WHERE task.requirement_id = r.id AND tr.user_id = %s)", userID)
	reqCreated := fmt.Sprintf("r.creator_id = %s", userID)
	taskCreated := fmt.Sprintf("EXISTS (SELECT 1 FROM tasks task WHERE task.requirement_id = r.id AND task.creator_id = %s)", userID)
	switch scope {
	case "mine":
		*where = append(*where, fmt.Sprintf("(%s OR %s OR %s OR %s OR %s OR %s)", reqFollow, taskFollow, reqAssigned, taskAssigned, reqCreated, taskCreated))
	case "followed":
		*where = append(*where, fmt.Sprintf("(%s OR %s)", reqFollow, taskFollow))
	case "assigned":
		*where = append(*where, fmt.Sprintf("(%s OR %s)", reqAssigned, taskAssigned))
	case "created":
		*where = append(*where, fmt.Sprintf("(%s OR %s)", reqCreated, taskCreated))
	}
}

func (h *RequirementHandler) appendRequirementRiskFilter(where *[]string, args *[]any, risk string) {
	if risk == "" {
		return
	}
	addArg := func(value any) string {
		*args = append(*args, value)
		return fmt.Sprintf("$%d", len(*args))
	}
	switch risk {
	case "requirement_overdue":
		today := addArg(biztime.Date(biztime.Now()))
		*where = append(*where, fmt.Sprintf("(r.status NOT IN ('completed', 'cancelled') AND r.deadline IS NOT NULL AND r.deadline < %s)", today))
	case "task_overdue":
		today := addArg(biztime.Date(biztime.Now()))
		*where = append(*where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM tasks task
			WHERE task.requirement_id = r.id
			  AND task.status <> 'done'
			  AND task.due_date IS NOT NULL
			  AND task.due_date < %s
		)`, today))
	case "blocked":
		today := addArg(biztime.Date(biztime.Now()))
		*where = append(*where, requirementBlockedRiskSQL(today))
	case "dependency_conflict":
		*where = append(*where, requirementDependencyConflictRiskSQL())
	}
}

func requirementBlockedRiskSQL(today string) string {
	return fmt.Sprintf(`EXISTS (
		SELECT 1
		FROM tasks task
		JOIN work_item_relations rel ON rel.source_type = 'task' AND rel.source_id = task.id AND rel.relation_type = 'depends_on'
		LEFT JOIN tasks dep_task ON rel.target_type = 'task' AND dep_task.id = rel.target_id
		LEFT JOIN requirements dep_req ON rel.target_type = 'requirement' AND dep_req.id = rel.target_id
		WHERE task.requirement_id = r.id
		  AND task.status <> 'done'
		  AND COALESCE(dep_task.status, dep_req.status, '') NOT IN ('done', 'completed')
		  AND (
			task.status = 'in_progress'
			OR (task.due_date IS NOT NULL AND task.due_date <= %[1]s)
			OR (CASE WHEN rel.target_type = 'task' THEN dep_task.due_date ELSE dep_req.deadline END) IS NOT NULL
			   AND (CASE WHEN rel.target_type = 'task' THEN dep_task.due_date ELSE dep_req.deadline END) < %[1]s
		  )
	)`, today)
}

func requirementDependencyConflictRiskSQL() string {
	return `EXISTS (
		SELECT 1
		FROM tasks task
		JOIN work_item_relations rel ON rel.source_type = 'task' AND rel.source_id = task.id AND rel.relation_type = 'depends_on'
		LEFT JOIN tasks dep_task ON rel.target_type = 'task' AND dep_task.id = rel.target_id
		LEFT JOIN requirements dep_req ON rel.target_type = 'requirement' AND dep_req.id = rel.target_id
		WHERE task.requirement_id = r.id
		  AND task.status <> 'done'
		  AND task.due_date IS NOT NULL
		  AND COALESCE(dep_task.status, dep_req.status, '') NOT IN ('done', 'completed')
		  AND (CASE WHEN rel.target_type = 'task' THEN dep_task.due_date ELSE dep_req.deadline END) IS NOT NULL
		  AND (CASE WHEN rel.target_type = 'task' THEN dep_task.due_date ELSE dep_req.deadline END) >= task.due_date
	)`
}

func (h *RequirementHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "requirement_id") {
		return
	}
	u := getUser(r)
	var req model.Requirement
	var acStr string
	var deadline sql.NullString
	var feishuURL sql.NullString
	var completedAt sql.NullTime
	err := h.db.QueryRow(`
		SELECT r.id, r.title, r.description, r.feishu_doc_url, r.acceptance_criteria,
			r.creator_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), r.creator_role, r.status, r.priority,
			r.progress, r.deadline, r.completed_at, r.created_at, r.updated_at, r.version
		FROM requirements r
		JOIN users u ON u.id = r.creator_id
		WHERE r.id = $1`, id).Scan(
		&req.ID, &req.Title, &req.Description, &feishuURL, &acStr,
		&req.CreatorID, &req.CreatorName, &req.CreatorRole, &req.Status, &req.Priority,
		&req.Progress, &deadline, &completedAt, &req.CreatedAt, &req.UpdatedAt, &req.Version,
	)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	allowed, permErr := h.canViewRequirement(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	req.FeishuDocURL = nullStringPtr(feishuURL)
	req.Deadline = nullStringPtr(deadline)
	req.AcceptanceCriteria = parseTextArray(acStr)
	req.CompletedAt = nullTimePtr(completedAt)
	h.loadTeams(&req)
	h.loadResponsibles(&req)
	h.loadProjection(&req, u)
	writeJSON(w, http.StatusOK, req)
}

func (h *RequirementHandler) Create(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	var req model.CreateRequirementRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}

	if req.Title == "" || req.Description == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title and description required"})
		return
	}
	responsibleUserIDs, ownerErr := h.normalizeRequirementResponsibleUserIDs(req.ResponsibleUserIDs)
	if ownerErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ownerErr.Error()})
		return
	}
	if err := h.ensureRequirementResponsibleUsers(responsibleUserIDs); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if !h.canCreateRequirement(u, req.TeamIDs) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions to create requirements for these teams"})
		return
	}
	if req.Priority == "" {
		req.Priority = "medium"
	}

	ac := req.AcceptanceCriteria

	tx, err := h.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var reqID string
	err = tx.QueryRow(`
		INSERT INTO requirements (title, description, feishu_doc_url, acceptance_criteria, creator_id, creator_role, priority, deadline)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		req.Title, req.Description, nullString(req.FeishuDocURL),
		arrayToTextArray(ac), u.ID, u.Role, req.Priority, nullString(req.Deadline),
	).Scan(&reqID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	for _, tid := range req.TeamIDs {
		if _, err := tx.Exec("INSERT INTO requirement_teams (requirement_id, team_id) VALUES ($1, $2)", reqID, tid); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid team_id"})
			return
		}
	}
	if err := syncRequirementResponsibles(tx, reqID, responsibleUserIDs); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var result model.Requirement
	var acStr string
	var deadline sql.NullString
	var feishuURL sql.NullString
	var completedAt sql.NullTime
	err = h.db.QueryRow(`
		SELECT r.id, r.title, r.description, r.feishu_doc_url, r.acceptance_criteria,
			r.creator_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), r.creator_role, r.status, r.priority,
			r.progress, r.deadline, r.completed_at, r.created_at, r.updated_at, r.version
		FROM requirements r
		JOIN users u ON u.id = r.creator_id
		WHERE r.id = $1`, reqID).Scan(
		&result.ID, &result.Title, &result.Description, &feishuURL, &acStr,
		&result.CreatorID, &result.CreatorName, &result.CreatorRole, &result.Status, &result.Priority,
		&result.Progress, &deadline, &completedAt, &result.CreatedAt, &result.UpdatedAt, &result.Version,
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	result.FeishuDocURL = nullStringPtr(feishuURL)
	result.Deadline = nullStringPtr(deadline)
	result.AcceptanceCriteria = parseTextArray(acStr)
	result.CompletedAt = nullTimePtr(completedAt)
	h.loadTeams(&result)
	h.loadResponsibles(&result)
	h.loadProjection(&result, u)
	h.recordRequirementEvent(r.Context(), u, result.ID, "requirement_created", "创建了需求", nil, requirementEventData(result), nil)
	writeJSON(w, http.StatusCreated, result)
}

func (h *RequirementHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "requirement_id") {
		return
	}
	u := getUser(r)
	var req model.UpdateRequirementRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if req.Status != nil && !isRequirementStatus(*req.Status) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid requirement status"})
		return
	}
	if !requireBaseVersion(w, req.BaseVersion) {
		return
	}
	allowed, permErr := h.canManageRequirement(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeRequirementForbiddenOrNotFound(w, h.db, id, "insufficient permissions to update requirement")
		return
	}
	beforeState, beforeStateErr := loadRequirementEventState(h.db, id)
	if beforeStateErr != nil {
		log.Printf("load requirement before state failed: requirement_id=%s error=%v", id, beforeStateErr)
	}

	responsibleTouched := req.ResponsibleUserIDs != nil
	responsibleUserIDs := []string{}
	if responsibleTouched {
		var ownerErr error
		responsibleUserIDs, ownerErr = h.normalizeRequirementResponsibleUserIDs(*req.ResponsibleUserIDs)
		if ownerErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": ownerErr.Error()})
			return
		}
		if err := h.ensureRequirementResponsibleUsers(responsibleUserIDs); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
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
	if req.Description != nil {
		sets = append(sets, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *req.Description)
		argIdx++
	}
	if req.Status != nil {
		sets = append(sets, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, *req.Status)
		argIdx++
		if *req.Status == "completed" {
			sets = append(sets, "completed_at = COALESCE(completed_at, now())")
		} else {
			sets = append(sets, "completed_at = NULL")
		}
	}
	if req.Priority != nil {
		sets = append(sets, fmt.Sprintf("priority = $%d", argIdx))
		args = append(args, *req.Priority)
		argIdx++
	}
	if req.Deadline != nil {
		sets = append(sets, fmt.Sprintf("deadline = $%d", argIdx))
		args = append(args, *req.Deadline)
		argIdx++
	}
	if req.FeishuDocURL != nil {
		sets = append(sets, fmt.Sprintf("feishu_doc_url = $%d", argIdx))
		args = append(args, *req.FeishuDocURL)
		argIdx++
	}
	if req.AcceptanceCriteria != nil {
		sets = append(sets, fmt.Sprintf("acceptance_criteria = $%d", argIdx))
		args = append(args, arrayToTextArray(*req.AcceptanceCriteria))
		argIdx++
	}

	if len(sets) == 0 && req.TeamIDs == nil && !responsibleTouched {
		writeNoFieldsToUpdate(w)
		return
	}

	sets = append(sets, "version = version + 1", "updated_at = now()")
	args = append(args, id, req.BaseVersion)
	query := fmt.Sprintf("UPDATE requirements SET %s WHERE id = $%d AND version = $%d", strings.Join(sets, ", "), argIdx, argIdx+1)

	if req.TeamIDs == nil && !responsibleTouched {
		res, err := h.db.Exec(query, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			writeRequirementNotFoundOrConflict(w, h.db, id)
			return
		}

		h.recordRequirementChange(r.Context(), u, id, beforeState)
		h.Get(w, r)
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	res, err := tx.Exec(query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeRequirementNotFoundOrConflict(w, h.db, id)
		return
	}
	if req.TeamIDs != nil {
		if _, err := tx.Exec(`DELETE FROM requirement_teams WHERE requirement_id = $1`, id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		for _, tid := range *req.TeamIDs {
			if _, err := tx.Exec(`INSERT INTO requirement_teams (requirement_id, team_id) VALUES ($1, $2)`, id, tid); err != nil {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid team_id"})
				return
			}
		}
	}
	if responsibleTouched {
		if err := syncRequirementResponsibles(tx, id, responsibleUserIDs); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.recordRequirementChange(r.Context(), u, id, beforeState)
	h.Get(w, r)
}

func (h *RequirementHandler) Restore(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "requirement_id") {
		return
	}
	u := getUser(r)
	var req model.RequirementVersionRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if !requireBaseVersion(w, req.BaseVersion) {
		return
	}
	allowed, permErr := h.canManageRequirement(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeRequirementForbiddenOrNotFound(w, h.db, id, "insufficient permissions to restore requirements")
		return
	}
	beforeState, beforeStateErr := loadRequirementEventState(h.db, id)
	if beforeStateErr != nil {
		log.Printf("load requirement before restore failed: requirement_id=%s error=%v", id, beforeStateErr)
	}

	res, err := h.db.Exec(`
		UPDATE requirements
		SET status = 'todo', completed_at = NULL, version = version + 1, updated_at = now()
		WHERE id = $1 AND status = 'cancelled' AND version = $2`, id, req.BaseVersion)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		var currentVersion int64
		var status string
		err := h.db.QueryRow(`SELECT version, status FROM requirements WHERE id = $1`, id).Scan(&currentVersion, &status)
		if err == sql.ErrNoRows {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if currentVersion != req.BaseVersion {
			writeEditConflict(w, currentVersion)
			return
		}
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "requirement is not cancelled",
			"code":  "not_cancelled",
		})
		return
	}

	if beforeState != nil {
		h.recordRequirementChange(r.Context(), u, id, beforeState)
	} else {
		h.recordRequirementEvent(r.Context(), u, id, "requirement_restored", "恢复了需求", nil, nil, nil)
	}
	h.Get(w, r)
}

func (h *RequirementHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	u := getUser(r)
	if !validateUUIDParam(w, id, "requirement_id") {
		return
	}
	baseVersion, ok := parseBaseVersionFromQuery(w, r)
	if !ok {
		return
	}
	allowed, permErr := h.canManageRequirement(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeRequirementForbiddenOrNotFound(w, h.db, id, "insufficient permissions to delete requirements")
		return
	}
	beforeState, beforeStateErr := loadRequirementEventState(h.db, id)
	if beforeStateErr != nil {
		log.Printf("load requirement before delete failed: requirement_id=%s error=%v", id, beforeStateErr)
	}

	tx, err := h.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var currentVersion int64
	if err := tx.QueryRow("SELECT version FROM requirements WHERE id = $1 FOR UPDATE", id).Scan(&currentVersion); err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if currentVersion != baseVersion {
		writeEditConflict(w, currentVersion)
		return
	}

	var hasAssociations bool
	if err := tx.QueryRow(`
			SELECT EXISTS(SELECT 1 FROM tasks WHERE requirement_id = $1)
			    OR EXISTS(SELECT 1 FROM sessions WHERE requirement_id = $1)
			    OR EXISTS(SELECT 1 FROM token_usage WHERE requirement_id = $1)
			    OR EXISTS(SELECT 1 FROM documents WHERE requirement_id = $1)
			    OR EXISTS(
					SELECT 1 FROM work_item_relations
					WHERE (source_type = 'requirement' AND source_id = $1)
					   OR (target_type = 'requirement' AND target_id = $1)
				)`, id).Scan(&hasAssociations); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if hasAssociations {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "requirement has associated tasks/sessions/tokens/documents — cancel instead of delete",
			"code":  "has_associations",
		})
		return
	}

	if _, err := tx.Exec(`DELETE FROM user_follows WHERE target_type = 'requirement' AND target_id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM requirement_teams WHERE requirement_id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM requirement_responsibles WHERE requirement_id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if _, err := tx.Exec(`DELETE FROM requirements WHERE id = $1`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	h.recordRequirementEvent(r.Context(), u, id, "requirement_deleted", "删除了需求", beforeState, nil, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": id})
}

func (h *RequirementHandler) AddDependency(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "requirement_id") {
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
	allowed, permErr := h.canManageRequirement(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeRequirementForbiddenOrNotFound(w, h.db, id, "insufficient permissions to update requirement dependencies")
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var currentVersion int64
	if err := tx.QueryRow(`SELECT version FROM requirements WHERE id = $1 FOR UPDATE`, id).Scan(&currentVersion); err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	} else if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if currentVersion != req.BaseVersion {
		writeEditConflict(w, currentVersion)
		return
	}
	taskHandler := NewTaskHandler(h.db)
	status, err := taskHandler.validateWorkItemDependencyTx(tx, u, "requirement", id, targetType, req.DependsOnID)
	if err != nil {
		writeJSON(w, status, map[string]string{"error": err.Error()})
		return
	}
	res, err := tx.Exec(`
		INSERT INTO work_item_relations (source_type, source_id, target_type, target_id, relation_type, created_by)
		VALUES ('requirement', $1, $2, $3, 'depends_on', CAST($4 AS bigint))
		ON CONFLICT DO NOTHING`, id, targetType, req.DependsOnID, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dependencyAdded := false
	if rows, _ := res.RowsAffected(); rows > 0 {
		dependencyAdded = true
		if _, err := tx.Exec(`UPDATE requirements SET version = version + 1, updated_at = now() WHERE id = $1`, id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if dependencyAdded {
		metadata := loadWorkItemEventMetadata(h.db, targetType, req.DependsOnID)
		h.recordRequirementEvent(r.Context(), u, id, "requirement_dependency_added", "新增了需求上游依赖", nil, nil, metadata)
	}
	h.Get(w, r)
}

func (h *RequirementHandler) RemoveDependency(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	depID := chi.URLParam(r, "dep_id")
	if !validateUUIDParam(w, id, "requirement_id") || !validateUUIDParam(w, depID, "dependency_id") {
		return
	}
	u := getUser(r)
	baseVersion, ok := parseBaseVersionFromQuery(w, r)
	if !ok {
		return
	}
	targetType, typeErr := normalizeWorkItemType(chi.URLParam(r, "target_type"))
	if typeErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": typeErr.Error()})
		return
	}
	allowed, permErr := h.canManageRequirement(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeRequirementForbiddenOrNotFound(w, h.db, id, "insufficient permissions to update requirement dependencies")
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer tx.Rollback()

	var currentVersion int64
	if err := tx.QueryRow(`SELECT version FROM requirements WHERE id = $1 FOR UPDATE`, id).Scan(&currentVersion); err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
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
		WHERE source_type = 'requirement'
		  AND source_id = $1
		  AND target_type = $2
		  AND target_id = $3
		  AND relation_type = 'depends_on'`, id, targetType, depID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	dependencyRemoved := false
	if rows, _ := res.RowsAffected(); rows > 0 {
		dependencyRemoved = true
		if _, err := tx.Exec(`UPDATE requirements SET version = version + 1, updated_at = now() WHERE id = $1`, id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if dependencyRemoved {
		metadata := loadWorkItemEventMetadata(h.db, targetType, depID)
		h.recordRequirementEvent(r.Context(), u, id, "requirement_dependency_removed", "移除了需求上游依赖", metadata, nil, metadata)
	}
	h.Get(w, r)
}

func (h *RequirementHandler) GetAC(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "requirement_id") {
		return
	}
	u := getUser(r)
	allowed, permErr := h.canViewRequirement(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	var acStr string
	err := h.db.QueryRow("SELECT acceptance_criteria FROM requirements WHERE id = $1", id).Scan(&acStr)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	acItems := parseTextArray(acStr)
	statuses := []model.ACStatus{}

	for i, text := range acItems {
		statuses = append(statuses, model.ACStatus{
			Index:       i,
			Text:        text,
			Completed:   false,
			LinkedTasks: []string{},
		})
	}

	writeJSON(w, http.StatusOK, statuses)
}

func (h *RequirementHandler) RegenerateAC(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !validateUUIDParam(w, id, "requirement_id") {
		return
	}
	u := getUser(r)
	var req model.RegenerateACRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
		return
	}
	if !requireBaseVersion(w, req.BaseVersion) {
		return
	}
	allowed, permErr := h.canManageRequirement(u, id)
	if permErr != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": permErr.Error()})
		return
	}
	if !allowed {
		writeRequirementForbiddenOrNotFound(w, h.db, id, "only director/pm/tl can regenerate AC")
		return
	}
	beforeState, beforeStateErr := loadRequirementEventState(h.db, id)
	if beforeStateErr != nil {
		log.Printf("load requirement before regenerate ac failed: requirement_id=%s error=%v", id, beforeStateErr)
	}

	var title, description string
	var currentVersion int64
	err := h.db.QueryRow("SELECT title, description, version FROM requirements WHERE id = $1", id).Scan(&title, &description, &currentVersion)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if currentVersion != req.BaseVersion {
		writeEditConflict(w, currentVersion)
		return
	}

	if h.ai == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "AI service not configured"})
		return
	}

	ac, err := h.ai.GenerateAcceptanceCriteria(r.Context(), title, description)
	if err != nil {
		log.Printf("AI regenerate AC failed: %v", err)
	}
	if len(ac) == 0 {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "AI returned no acceptance criteria"})
		return
	}

	res, err := h.db.Exec("UPDATE requirements SET acceptance_criteria = $1, version = version + 1, updated_at = now() WHERE id = $2 AND version = $3",
		arrayToTextArray(ac), id, req.BaseVersion)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		writeRequirementNotFoundOrConflict(w, h.db, id)
		return
	}

	if beforeState != nil {
		h.recordRequirementChange(r.Context(), u, id, beforeState)
	} else {
		h.recordRequirementEvent(r.Context(), u, id, "requirement_ac_updated", "重新生成了验收标准", nil, map[string]any{"acceptance_criteria": ac}, nil)
	}
	h.Get(w, r)
}

func (h *RequirementHandler) loadTeams(req *model.Requirement) {
	req.TeamIDs = []string{}
	req.TeamNames = []string{}
	rows, err := h.db.Query(`
		SELECT t.id, t.name FROM teams t
		JOIN requirement_teams rt ON rt.team_id = t.id
		WHERE rt.requirement_id = $1`, req.ID)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var id, name string
		rows.Scan(&id, &name)
		req.TeamIDs = append(req.TeamIDs, id)
		req.TeamNames = append(req.TeamNames, name)
	}
}

func (h *RequirementHandler) loadProjection(req *model.Requirement, u *model.User) {
	taskHandler := NewTaskHandler(h.db)
	rows, err := h.db.Query(`
		SELECT t.id, t.requirement_id, r.title, t.title,
			COALESCE(t.acceptance_criteria, ARRAY[]::text[]),
			t.creator_id::text, COALESCE(COALESCE(NULLIF(c.nickname,''), c.username), ''), t.status, t.priority, t.progress, t.due_date,
			t.completed_at, t.created_at, t.updated_at, t.version
		FROM tasks t
		JOIN requirements r ON r.id = t.requirement_id
		JOIN users c ON c.id = t.creator_id
		WHERE t.requirement_id = $1
		ORDER BY t.created_at`, req.ID)
	tasks := []model.Task{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var task model.Task
			var ac pq.StringArray
			var dueDate sql.NullString
			var completedAt sql.NullTime
			if rows.Scan(
				&task.ID, &task.RequirementID, &task.RequirementTitle, &task.Title,
				&ac, &task.CreatorID, &task.CreatorName,
				&task.Status, &task.Priority, &task.Progress, &dueDate,
				&completedAt, &task.CreatedAt, &task.UpdatedAt, &task.Version,
			) != nil {
				continue
			}
			task.AcceptanceCriteria = []string(ac)
			task.DueDate = nullStringPtr(dueDate)
			task.CompletedAt = nullTimePtr(completedAt)
			taskHandler.enrichTask(&task, u)
			tasks = append(tasks, task)
		}
	}
	req.Dependencies, req.Blocking = taskHandler.loadWorkItemRelations("requirement", req.ID)
	req.Progress = service.AggregateRequirementProgress(tasks)
	req.TaskSummary, req.RiskSummary = service.SummarizeRequirement(*req, tasks, time.Now())
	userID := ""
	if u != nil {
		userID = u.ID
	}
	_ = h.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM user_follows
			WHERE user_id = $1 AND target_type = 'requirement' AND target_id = $2
		)`, userID, req.ID).Scan(&req.IsFollowed)
	attention := loadFollowAttention(h.db, "requirement", req.ID)
	req.FollowSummary = model.RequirementFollowSummary{
		Count: attention.count,
		Score: attention.score,
		Level: attentionLevel(attention.score),
	}

	var hasAssociations bool
	_ = h.db.QueryRow(`
			SELECT EXISTS(SELECT 1 FROM tasks WHERE requirement_id = $1)
			    OR EXISTS(SELECT 1 FROM sessions WHERE requirement_id = $1)
			    OR EXISTS(SELECT 1 FROM token_usage WHERE requirement_id = $1)
			    OR EXISTS(SELECT 1 FROM documents WHERE requirement_id = $1)
			    OR EXISTS(
					SELECT 1 FROM work_item_relations
					WHERE (source_type = 'requirement' AND source_id = $1)
					   OR (target_type = 'requirement' AND target_id = $1)
				)`, req.ID).Scan(&hasAssociations)
	h.applyRequirementPermissions(req, u)
	req.CanDelete = req.CanDelete && !hasAssociations

	tokenRows, tokenErr := h.db.Query(`
		SELECT source_id
		FROM (
			SELECT DISTINCT sas.session_id::text || ':' || sas.activity_date::text AS source_id
			FROM session_activity_slices sas
			WHERE sas.requirement_id = $1 AND sas.task_id IS NULL
			UNION
			SELECT DISTINCT s.id::text AS source_id
			FROM sessions s
			JOIN token_usage tu ON tu.session_id = s.id
			WHERE s.requirement_id = $1 AND s.task_id IS NULL
			  AND NOT EXISTS (SELECT 1 FROM session_activity_slices sas WHERE sas.session_id = s.id)
		) sources
		ORDER BY source_id`, req.ID)
	if tokenErr == nil {
		defer tokenRows.Close()
		for tokenRows.Next() {
			var id string
			if tokenRows.Scan(&id) == nil {
				req.TokenSourceIDs = append(req.TokenSourceIDs, id)
			}
		}
	}
}

func nullStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func (h *RequirementHandler) loadResponsibles(req *model.Requirement) {
	rows, err := h.db.Query(`
		SELECT u.id::text,
			COALESCE(NULLIF(u.nickname,''), u.username),
			u.app_role,
			u.team_id::text,
			t.name
		FROM requirement_responsibles rr
		JOIN users u ON u.id = rr.user_id
		LEFT JOIN teams t ON t.id = u.team_id
		WHERE rr.requirement_id = $1
		ORDER BY rr.created_at, u.id`, req.ID)
	if err != nil {
		return
	}
	defer rows.Close()

	responsibleUsers := []model.ResponsibleUser{}
	responsibleUserIDs := []string{}
	for rows.Next() {
		var responsible model.ResponsibleUser
		var teamID, teamName sql.NullString
		if err := rows.Scan(&responsible.ID, &responsible.Name, &responsible.Role, &teamID, &teamName); err != nil {
			continue
		}
		responsible.TeamID = nullStringPtr(teamID)
		responsible.TeamName = nullStringPtr(teamName)
		responsibleUsers = append(responsibleUsers, responsible)
		responsibleUserIDs = append(responsibleUserIDs, responsible.ID)
	}
	if len(responsibleUsers) == 0 {
		return
	}
	req.ResponsibleUsers = responsibleUsers
	req.ResponsibleUserIDs = responsibleUserIDs
}

func (h *RequirementHandler) normalizeRequirementResponsibleUserIDs(rawIDs []string) ([]string, error) {
	values := make([]string, 0, len(rawIDs))
	seen := map[string]struct{}{}
	for _, raw := range rawIDs {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return nil, fmt.Errorf("invalid responsible_user_ids")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("responsible_user_ids is required")
	}
	return values, nil
}

func (h *RequirementHandler) ensureRequirementResponsibleUser(responsibleUserID string) error {
	var ok bool
	if err := h.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM users
			WHERE id = $1 AND local_enabled = true
		)`, responsibleUserID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("responsible user not found")
	}
	return nil
}

func (h *RequirementHandler) ensureRequirementResponsibleUsers(responsibleUserIDs []string) error {
	for _, responsibleUserID := range responsibleUserIDs {
		id := responsibleUserID
		if err := h.ensureRequirementResponsibleUser(id); err != nil {
			return err
		}
	}
	return nil
}

func syncRequirementResponsibles(tx *sql.Tx, requirementID string, responsibleUserIDs []string) error {
	if _, err := tx.Exec(`DELETE FROM requirement_responsibles WHERE requirement_id = $1`, requirementID); err != nil {
		return err
	}
	for _, responsibleUserID := range responsibleUserIDs {
		if _, err := tx.Exec(`
			INSERT INTO requirement_responsibles (requirement_id, user_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, requirementID, nullableUserID(&responsibleUserID)); err != nil {
			return err
		}
	}
	return nil
}

func nullableUserID(id *string) sql.NullInt64 {
	if id == nil || strings.TrimSpace(*id) == "" {
		return sql.NullInt64{}
	}
	value, err := strconv.ParseInt(strings.TrimSpace(*id), 10, 64)
	if err != nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: value, Valid: true}
}

func isRequirementStatus(status string) bool {
	return status == "todo" || status == "review" || status == "active" || status == "completed" || status == "cancelled"
}

func parseTextArray(pgArray string) []string {
	if pgArray == "" || pgArray == "{}" || pgArray == "{NULL}" {
		return []string{}
	}
	s := strings.Trim(pgArray, "{}")
	if s == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), "\"")
		if p != "" && p != "NULL" {
			result = append(result, p)
		}
	}
	return result
}

func arrayToTextArray(items []string) string {
	if len(items) == 0 {
		return "{}"
	}
	escaped := make([]string, len(items))
	for i, s := range items {
		s = strings.ReplaceAll(s, `"`, `\"`)
		escaped[i] = `"` + s + `"`
	}
	return "{" + strings.Join(escaped, ",") + "}"
}
