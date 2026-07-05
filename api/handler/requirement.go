package handler

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/go-chi/chi/v5"
	"github.com/lib/pq"
)

type RequirementHandler struct {
	db *sql.DB
	ai *service.AIClient
}

func NewRequirementHandler(db *sql.DB, ai *service.AIClient) *RequirementHandler {
	return &RequirementHandler{db: db, ai: ai}
}

func (h *RequirementHandler) List(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	query := `
		SELECT r.id, r.title, r.description, r.feishu_doc_url, r.acceptance_criteria,
			r.creator_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), r.creator_role, r.status, r.priority,
			r.owner_id::text, COALESCE(NULLIF(owner.nickname,''), owner.username), owner.team_id::text, owner_team.name,
			r.progress, r.deadline, r.completed_at, r.created_at, r.updated_at, r.version
		FROM requirements r
		JOIN users u ON u.id = r.creator_id
		LEFT JOIN users owner ON owner.id = r.owner_id
		LEFT JOIN teams owner_team ON owner_team.id = owner.team_id
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
		query += fmt.Sprintf(" AND (r.creator_id = $%d OR r.owner_id = $%d OR EXISTS (SELECT 1 FROM requirement_owners ro WHERE ro.requirement_id = r.id AND ro.user_id = $%d) OR EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id = $%d))", argIdx, argIdx, argIdx, argIdx+1)
		args = append(args, u.ID, *u.TeamID)
		argIdx += 2
	} else if u.Role == "employee" && u.TeamID != nil {
		query += fmt.Sprintf(" AND (r.owner_id = $%d OR EXISTS (SELECT 1 FROM requirement_owners ro WHERE ro.requirement_id = r.id AND ro.user_id = $%d) OR r.creator_id = $%d OR EXISTS (SELECT 1 FROM requirement_teams rt WHERE rt.requirement_id = r.id AND rt.team_id = $%d))", argIdx, argIdx, argIdx, argIdx+1)
		args = append(args, u.ID, *u.TeamID)
		argIdx += 2
	} else if u.Role == "employee" {
		query += fmt.Sprintf(" AND (r.owner_id = $%d OR EXISTS (SELECT 1 FROM requirement_owners ro WHERE ro.requirement_id = r.id AND ro.user_id = $%d) OR r.creator_id = $%d)", argIdx, argIdx, argIdx)
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
		var ownerID, ownerName, ownerTeamID, ownerTeamName sql.NullString
		if err := rows.Scan(&req.ID, &req.Title, &req.Description, &feishuURL, &acStr,
			&req.CreatorID, &req.CreatorName, &req.CreatorRole, &req.Status, &req.Priority,
			&ownerID, &ownerName, &ownerTeamID, &ownerTeamName,
			&req.Progress, &deadline, &completedAt, &req.CreatedAt, &req.UpdatedAt, &req.Version); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		applyRequirementOwner(&req, ownerID, ownerName, ownerTeamID, ownerTeamName)
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
		h.loadOwners(&reqs[i])
		h.loadProjection(&reqs[i], u)
	}

	writeJSON(w, http.StatusOK, reqs)
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
	var ownerID, ownerName, ownerTeamID, ownerTeamName sql.NullString

	err := h.db.QueryRow(`
		SELECT r.id, r.title, r.description, r.feishu_doc_url, r.acceptance_criteria,
			r.creator_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), r.creator_role, r.status, r.priority,
			r.owner_id::text, COALESCE(NULLIF(owner.nickname,''), owner.username), owner.team_id::text, owner_team.name,
			r.progress, r.deadline, r.completed_at, r.created_at, r.updated_at, r.version
		FROM requirements r
		JOIN users u ON u.id = r.creator_id
		LEFT JOIN users owner ON owner.id = r.owner_id
		LEFT JOIN teams owner_team ON owner_team.id = owner.team_id
		WHERE r.id = $1`, id).Scan(
		&req.ID, &req.Title, &req.Description, &feishuURL, &acStr,
		&req.CreatorID, &req.CreatorName, &req.CreatorRole, &req.Status, &req.Priority,
		&ownerID, &ownerName, &ownerTeamID, &ownerTeamName,
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
	applyRequirementOwner(&req, ownerID, ownerName, ownerTeamID, ownerTeamName)
	h.loadTeams(&req)
	h.loadOwners(&req)
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
	ownerIDs, ownerErr := h.normalizeRequirementOwnerIDs(req.OwnerIDs, req.OwnerID)
	if ownerErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": ownerErr.Error()})
		return
	}
	if err := h.ensureRequirementOwners(ownerIDs); err != nil {
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
		INSERT INTO requirements (title, description, feishu_doc_url, acceptance_criteria, creator_id, creator_role, priority, deadline, owner_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		req.Title, req.Description, nullString(req.FeishuDocURL),
		arrayToTextArray(ac), u.ID, u.Role, req.Priority, nullString(req.Deadline), nullableUserID(firstOwnerID(ownerIDs)),
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
	if err := syncRequirementOwners(tx, reqID, ownerIDs); err != nil {
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
	var resultOwnerID, resultOwnerName, resultOwnerTeamID, resultOwnerTeamName sql.NullString
	err = h.db.QueryRow(`
		SELECT r.id, r.title, r.description, r.feishu_doc_url, r.acceptance_criteria,
			r.creator_id, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username),''), r.creator_role, r.status, r.priority,
			r.owner_id::text, COALESCE(NULLIF(owner.nickname,''), owner.username), owner.team_id::text, owner_team.name,
			r.progress, r.deadline, r.completed_at, r.created_at, r.updated_at, r.version
		FROM requirements r
		JOIN users u ON u.id = r.creator_id
		LEFT JOIN users owner ON owner.id = r.owner_id
		LEFT JOIN teams owner_team ON owner_team.id = owner.team_id
		WHERE r.id = $1`, reqID).Scan(
		&result.ID, &result.Title, &result.Description, &feishuURL, &acStr,
		&result.CreatorID, &result.CreatorName, &result.CreatorRole, &result.Status, &result.Priority,
		&resultOwnerID, &resultOwnerName, &resultOwnerTeamID, &resultOwnerTeamName,
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
	applyRequirementOwner(&result, resultOwnerID, resultOwnerName, resultOwnerTeamID, resultOwnerTeamName)
	h.loadTeams(&result)
	h.loadOwners(&result)
	h.loadProjection(&result, u)
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

	ownerTouched := req.OwnerIDs != nil || req.OwnerID != nil || req.ClearOwner
	ownerIDs := []string{}
	if ownerTouched {
		var ownerErr error
		if req.OwnerIDs != nil {
			ownerIDs, ownerErr = h.normalizeRequirementOwnerIDs(*req.OwnerIDs, nil)
		} else if req.OwnerID != nil {
			ownerIDs, ownerErr = h.normalizeRequirementOwnerIDs(nil, req.OwnerID)
			if len(ownerIDs) == 0 {
				req.ClearOwner = true
			}
		}
		if ownerErr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": ownerErr.Error()})
			return
		}
		if req.ClearOwner {
			ownerIDs = []string{}
		}
		if err := h.ensureRequirementOwners(ownerIDs); err != nil {
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
	if ownerTouched {
		if len(ownerIDs) == 0 {
			sets = append(sets, "owner_id = NULL")
		} else {
			sets = append(sets, fmt.Sprintf("owner_id = $%d", argIdx))
			args = append(args, nullableUserID(firstOwnerID(ownerIDs)))
			argIdx++
		}
	}
	if req.AcceptanceCriteria != nil {
		sets = append(sets, fmt.Sprintf("acceptance_criteria = $%d", argIdx))
		args = append(args, arrayToTextArray(*req.AcceptanceCriteria))
		argIdx++
	}

	if len(sets) == 0 && req.TeamIDs == nil && !ownerTouched {
		writeNoFieldsToUpdate(w)
		return
	}

	sets = append(sets, "version = version + 1", "updated_at = now()")
	args = append(args, id, req.BaseVersion)
	query := fmt.Sprintf("UPDATE requirements SET %s WHERE id = $%d AND version = $%d", strings.Join(sets, ", "), argIdx, argIdx+1)

	if req.TeamIDs == nil && !ownerTouched {
		res, err := h.db.Exec(query, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			writeRequirementNotFoundOrConflict(w, h.db, id)
			return
		}

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
	if ownerTouched {
		if err := syncRequirementOwners(tx, id, ownerIDs); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
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
	if _, err := tx.Exec(`DELETE FROM requirement_owners WHERE requirement_id = $1`, id); err != nil {
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
	if rows, _ := res.RowsAffected(); rows > 0 {
		if _, err := tx.Exec(`UPDATE requirements SET version = version + 1, updated_at = now() WHERE id = $1`, id); err != nil {
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
	if rows, _ := res.RowsAffected(); rows > 0 {
		if _, err := tx.Exec(`UPDATE requirements SET version = version + 1, updated_at = now() WHERE id = $1`, id); err != nil {
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
			COALESCE(t.acceptance_criteria, ARRAY[]::text[]), t.assignee_id, COALESCE(COALESCE(NULLIF(a.nickname,''), a.username), ''),
			t.creator_tl_id, t.status, t.priority, t.progress, t.due_date,
			t.completed_at, t.created_at, t.updated_at, t.version
		FROM tasks t
		JOIN requirements r ON r.id = t.requirement_id
		LEFT JOIN users a ON a.id = t.assignee_id
		WHERE t.requirement_id = $1
		ORDER BY t.created_at`, req.ID)
	tasks := []model.Task{}
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var task model.Task
			var ac pq.StringArray
			var assigneeID, assigneeName, dueDate sql.NullString
			var completedAt sql.NullTime
			if rows.Scan(
				&task.ID, &task.RequirementID, &task.RequirementTitle, &task.Title,
				&ac, &assigneeID, &assigneeName, &task.CreatorTLID,
				&task.Status, &task.Priority, &task.Progress, &dueDate,
				&completedAt, &task.CreatedAt, &task.UpdatedAt, &task.Version,
			) != nil {
				continue
			}
			task.AcceptanceCriteria = []string(ac)
			task.AssigneeID = nullStringPtr(assigneeID)
			task.AssigneeName = nullStringPtr(assigneeName)
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
	req.CanDelete = !hasAssociations
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

func applyRequirementOwner(req *model.Requirement, ownerID, ownerName, ownerTeamID, ownerTeamName sql.NullString) {
	req.OwnerID = nullStringPtr(ownerID)
	req.OwnerName = nullStringPtr(ownerName)
	req.OwnerTeamID = nullStringPtr(ownerTeamID)
	req.OwnerTeamName = nullStringPtr(ownerTeamName)
	req.OwnerIDs = []string{}
	req.Owners = []model.RequirementOwner{}
	if req.OwnerID != nil {
		req.OwnerIDs = append(req.OwnerIDs, *req.OwnerID)
		req.Owners = append(req.Owners, model.RequirementOwner{
			ID:       *req.OwnerID,
			Name:     nullStringValue(ownerName),
			TeamID:   nullStringPtr(ownerTeamID),
			TeamName: nullStringPtr(ownerTeamName),
		})
	}
}

func nullStringValue(value sql.NullString) string {
	if value.Valid {
		return value.String
	}
	return ""
}

func (h *RequirementHandler) loadOwners(req *model.Requirement) {
	rows, err := h.db.Query(`
		SELECT u.id::text,
			COALESCE(NULLIF(u.nickname,''), u.username),
			u.app_role,
			u.team_id::text,
			t.name
		FROM requirement_owners ro
		JOIN users u ON u.id = ro.user_id
		LEFT JOIN teams t ON t.id = u.team_id
		WHERE ro.requirement_id = $1
		ORDER BY ro.created_at, u.id`, req.ID)
	if err != nil {
		return
	}
	defer rows.Close()

	owners := []model.RequirementOwner{}
	ownerIDs := []string{}
	for rows.Next() {
		var owner model.RequirementOwner
		var teamID, teamName sql.NullString
		if err := rows.Scan(&owner.ID, &owner.Name, &owner.Role, &teamID, &teamName); err != nil {
			continue
		}
		owner.TeamID = nullStringPtr(teamID)
		owner.TeamName = nullStringPtr(teamName)
		owners = append(owners, owner)
		ownerIDs = append(ownerIDs, owner.ID)
	}
	if len(owners) == 0 {
		return
	}
	req.Owners = owners
	req.OwnerIDs = ownerIDs
	first := owners[0]
	req.OwnerID = &first.ID
	req.OwnerName = &first.Name
	req.OwnerTeamID = first.TeamID
	req.OwnerTeamName = first.TeamName
}

func (h *RequirementHandler) normalizeRequirementOwnerID(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	value := strings.TrimSpace(*raw)
	if value == "" {
		return nil, nil
	}
	if _, err := strconv.ParseInt(value, 10, 64); err != nil {
		return nil, fmt.Errorf("invalid owner_id")
	}
	return &value, nil
}

func (h *RequirementHandler) normalizeRequirementOwnerIDs(rawIDs []string, legacyID *string) ([]string, error) {
	values := make([]string, 0, len(rawIDs)+1)
	if len(rawIDs) == 0 && legacyID != nil {
		rawIDs = append(rawIDs, *legacyID)
	}
	seen := map[string]struct{}{}
	for _, raw := range rawIDs {
		value := strings.TrimSpace(raw)
		if value == "" {
			continue
		}
		if _, err := strconv.ParseInt(value, 10, 64); err != nil {
			return nil, fmt.Errorf("invalid owner_ids")
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values, nil
}

func (h *RequirementHandler) ensureRequirementOwner(ownerID *string) error {
	if ownerID == nil {
		return nil
	}
	var ok bool
	if err := h.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM users
			WHERE id = $1 AND local_enabled = true
		)`, *ownerID).Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("owner not found")
	}
	return nil
}

func (h *RequirementHandler) ensureRequirementOwners(ownerIDs []string) error {
	for _, ownerID := range ownerIDs {
		id := ownerID
		if err := h.ensureRequirementOwner(&id); err != nil {
			return err
		}
	}
	return nil
}

func firstOwnerID(ownerIDs []string) *string {
	if len(ownerIDs) == 0 {
		return nil
	}
	return &ownerIDs[0]
}

func syncRequirementOwners(tx *sql.Tx, requirementID string, ownerIDs []string) error {
	if _, err := tx.Exec(`DELETE FROM requirement_owners WHERE requirement_id = $1`, requirementID); err != nil {
		return err
	}
	for _, ownerID := range ownerIDs {
		if _, err := tx.Exec(`
			INSERT INTO requirement_owners (requirement_id, user_id)
			VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, requirementID, nullableUserID(&ownerID)); err != nil {
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
