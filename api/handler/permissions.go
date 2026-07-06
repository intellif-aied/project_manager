package handler

import "github.com/aidashboard/api/model"

func isGlobalRequirementManager(role string) bool {
	return role == "admin" || role == "director" || role == "pm"
}

func isGlobalTaskManager(role string) bool {
	return role == "admin" || role == "director" || role == "pm"
}

func hasTeam(user *model.User) bool {
	return user != nil && user.TeamID != nil && *user.TeamID != ""
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func (h *RequirementHandler) canViewRequirement(u *model.User, requirementID string) (bool, error) {
	if u == nil {
		return false, nil
	}
	if isGlobalRequirementManager(u.Role) {
		return h.requirementExists(requirementID)
	}
	var allowed bool
	query := `
		SELECT EXISTS(
			SELECT 1 FROM requirements r
			WHERE r.id = $1
			  AND (r.creator_id = CAST($2 AS bigint) OR EXISTS (
				SELECT 1 FROM requirement_responsibles rr
				WHERE rr.requirement_id = r.id AND rr.user_id = CAST($2 AS bigint)
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
	return allowed, h.db.QueryRow(query, args...).Scan(&allowed)
}

func (h *RequirementHandler) canCreateRequirement(u *model.User, teamIDs []string) bool {
	if u == nil {
		return false
	}
	if isGlobalRequirementManager(u.Role) {
		return true
	}
	return u.Role == "team_leader"
}

func (h *RequirementHandler) canManageRequirement(u *model.User, requirementID string) (bool, error) {
	if u == nil {
		return false, nil
	}
	if isGlobalRequirementManager(u.Role) {
		return h.requirementExists(requirementID)
	}
	var allowed bool
	query := `
		SELECT EXISTS(
			SELECT 1 FROM requirements r
			WHERE r.id = $1
			  AND (r.creator_id = CAST($2 AS bigint) OR EXISTS (
				SELECT 1 FROM requirement_responsibles rr
				WHERE rr.requirement_id = r.id AND rr.user_id = CAST($2 AS bigint)
			  )`
	args := []any{requirementID, u.ID}
	if u.Role == "team_leader" && hasTeam(u) {
		query += ` OR EXISTS (
				SELECT 1 FROM requirement_teams rt
				WHERE rt.requirement_id = r.id AND rt.team_id = $3
			  )`
		args = append(args, *u.TeamID)
	}
	query += `))`
	return allowed, h.db.QueryRow(query, args...).Scan(&allowed)
}

func (h *RequirementHandler) requirementExists(requirementID string) (bool, error) {
	var exists bool
	err := h.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM requirements WHERE id = $1)`, requirementID).Scan(&exists)
	return exists, err
}

type taskAccessRecord struct {
	ID            string
	RequirementID string
	CreatorID     string
}

func (h *TaskHandler) loadTaskAccess(taskID string) (taskAccessRecord, error) {
	var task taskAccessRecord
	err := h.db.QueryRow(`
		SELECT id, requirement_id, creator_id::text
		FROM tasks
		WHERE id = $1`, taskID).Scan(&task.ID, &task.RequirementID, &task.CreatorID)
	return task, err
}

func (h *TaskHandler) canViewTask(u *model.User, taskID string) (bool, error) {
	task, err := h.loadTaskAccess(taskID)
	if err != nil {
		return false, err
	}
	return h.canViewTaskRecord(u, task)
}

func (h *TaskHandler) canViewTaskRecord(u *model.User, task taskAccessRecord) (bool, error) {
	if u == nil {
		return false, nil
	}
	if isGlobalTaskManager(u.Role) || task.CreatorID == u.ID || h.taskHasResponsible(task.ID, u.ID) {
		return true, nil
	}
	if hasTeam(u) && h.taskHasResponsibleInTeam(task.ID, *u.TeamID) {
		return true, nil
	}
	return NewRequirementHandler(h.db, nil).canViewRequirement(u, task.RequirementID)
}

func (h *TaskHandler) canCreateTask(u *model.User, requirementID string, responsibleUserIDs []string) (bool, string, error) {
	if u == nil {
		return false, "insufficient permissions to create tasks", nil
	}
	if requirementID == "" {
		return false, "requirement_id and title required", nil
	}
	var status string
	if err := h.db.QueryRow(`SELECT status FROM requirements WHERE id = $1`, requirementID).Scan(&status); err != nil {
		return false, "requirement not found", err
	}
	if status == "cancelled" {
		return false, "requirement is cancelled", nil
	}
	switch u.Role {
	case "admin", "director", "pm":
		ok, err := h.validateTaskResponsibleUsers(responsibleUserIDs, nil)
		if err != nil || !ok {
			return ok, "responsible users must be enabled task responsible users in scope", err
		}
		return true, "", nil
	case "team_leader":
		if !hasTeam(u) {
			return false, "team leader must belong to a team", nil
		}
		scoped, err := NewRequirementHandler(h.db, nil).canManageRequirement(u, requirementID)
		if err != nil || !scoped {
			return scoped, "requirement is not assigned to your team", err
		}
		ok, err := h.validateTaskResponsibleUsers(responsibleUserIDs, u.TeamID)
		if err != nil || !ok {
			return ok, "responsible users must be enabled task responsible users in your team", err
		}
		return true, "", nil
	case "employee":
		if len(responsibleUserIDs) != 1 || responsibleUserIDs[0] != u.ID {
			return false, "employee can only create tasks assigned to self", nil
		}
		visible, err := NewRequirementHandler(h.db, nil).canViewRequirement(u, requirementID)
		if err != nil || !visible {
			return visible, "requirement is not assigned to your team", err
		}
		return true, "", nil
	default:
		return false, "insufficient permissions to create tasks", nil
	}
}

func (h *TaskHandler) canManageTask(u *model.User, task taskAccessRecord) (bool, error) {
	if u == nil {
		return false, nil
	}
	if isGlobalTaskManager(u.Role) || task.CreatorID == u.ID || h.taskHasResponsible(task.ID, u.ID) {
		return true, nil
	}
	if u.Role == "employee" {
		return NewRequirementHandler(h.db, nil).canManageRequirement(u, task.RequirementID)
	}
	if u.Role != "team_leader" {
		return false, nil
	}
	if hasTeam(u) && h.taskHasResponsibleInTeam(task.ID, *u.TeamID) {
		return true, nil
	}
	return NewRequirementHandler(h.db, nil).canManageRequirement(u, task.RequirementID)
}

func (h *TaskHandler) canReassignTask(u *model.User, responsibleUserIDs []string) (bool, string, error) {
	if u == nil {
		return false, "insufficient permissions to reassign task", nil
	}
	if u.Role == "employee" {
		return false, "employee cannot reassign tasks", nil
	}
	if isGlobalTaskManager(u.Role) {
		ok, err := h.validateTaskResponsibleUsers(responsibleUserIDs, nil)
		if err != nil || !ok {
			return ok, "responsible users must be enabled task responsible users in scope", err
		}
		return true, "", nil
	}
	if u.Role == "team_leader" && hasTeam(u) {
		ok, err := h.validateTaskResponsibleUsers(responsibleUserIDs, u.TeamID)
		if err != nil || !ok {
			return ok, "responsible users must be enabled task responsible users in your team", err
		}
		return true, "", nil
	}
	return false, "insufficient permissions to reassign task", nil
}

func (h *TaskHandler) validateTaskResponsibleUsers(responsibleUserIDs []string, teamID *string) (bool, error) {
	for _, responsibleUserID := range responsibleUserIDs {
		var ok bool
		query := `
			SELECT EXISTS(
				SELECT 1 FROM users
				WHERE id = CAST($1 AS bigint)
				  AND local_enabled = true
				  AND app_role IN ('employee', 'team_leader', 'pm', 'director')`
		args := []any{responsibleUserID}
		if teamID != nil {
			query += ` AND team_id = $2`
			args = append(args, *teamID)
		}
		query += `)`
		if err := h.db.QueryRow(query, args...).Scan(&ok); err != nil || !ok {
			return ok, err
		}
	}
	return true, nil
}

func (h *TaskHandler) taskHasResponsible(taskID, userID string) bool {
	var ok bool
	_ = h.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM task_responsibles
			WHERE task_id = $1 AND user_id = CAST($2 AS bigint)
		)`, taskID, userID).Scan(&ok)
	return ok
}

func (h *TaskHandler) taskHasResponsibleInTeam(taskID, teamID string) bool {
	var ok bool
	_ = h.db.QueryRow(`
		SELECT EXISTS(
			SELECT 1 FROM task_responsibles tr
			JOIN users u ON u.id = tr.user_id
			WHERE tr.task_id = $1 AND u.team_id = $2
		)`, taskID, teamID).Scan(&ok)
	return ok
}

func (h *RequirementHandler) applyRequirementPermissions(req *model.Requirement, u *model.User) {
	if u == nil {
		return
	}
	manageable, _ := h.canManageRequirement(u, req.ID)
	canCreate := false
	if req.Status != "cancelled" {
		isResponsible := containsString(req.ResponsibleUserIDs, u.ID)
		isCreator := req.CreatorID == u.ID
		if isGlobalTaskManager(u.Role) {
			canCreate = true
		} else if isResponsible || isCreator {
			canCreate = true
		} else if hasTeam(u) && containsString(req.TeamIDs, *u.TeamID) {
			canCreate = true
		}
	}
	req.CanUpdate = manageable
	req.CanChangeStatus = manageable
	req.CanCancel = manageable && req.Status != "cancelled"
	req.CanRestore = manageable && req.Status == "cancelled"
	req.CanManageAC = manageable
	req.CanCreateTask = canCreate
}

func (h *TaskHandler) applyTaskPermissions(t *model.Task, u *model.User) {
	if u == nil {
		return
	}
	record := taskAccessRecord{ID: t.ID, RequirementID: t.RequirementID, CreatorID: t.CreatorID}
	manageable, _ := h.canManageTask(u, record)
	reassignable := false
	if manageable && u.Role != "employee" {
		reassignable = true
	}
	t.CanUpdateMeta = manageable
	t.CanReassign = reassignable
	t.CanUpdateStatus = manageable
	t.CanUpdateProgress = manageable
	t.CanManageDependencies = manageable
	t.CanDelete = manageable
}
