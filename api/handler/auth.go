package handler

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/lib/pq"
)

type AuthHandler struct {
	db                 *sql.DB
	aihub              *service.AIHubClient
	bootstrapAdminUIDs map[int64]bool
}

func NewAuthHandler(db *sql.DB, aihub *service.AIHubClient, bootstrapAdminUIDs string) *AuthHandler {
	return &AuthHandler{db: db, aihub: aihub, bootstrapAdminUIDs: parseBootstrapAdminUIDs(bootstrapAdminUIDs)}
}

func MintAIHubCompatibleToken(user *model.User, secret string) (string, error) {
	return mintAIHubCompatibleToken(user, secret, "")
}

func MintReportRunToken(user *model.User, secret, runID string) (string, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return "", errors.New("report run id is required")
	}
	return mintAIHubCompatibleToken(user, secret, runID)
}

func mintAIHubCompatibleToken(user *model.User, secret, reportRunID string) (string, error) {
	if user == nil {
		return "", errors.New("user is required")
	}
	uid := parseUserID(user.ID)
	if uid == 0 {
		return "", errors.New("invalid user id")
	}
	if secret == "" {
		secret = "dev-jwt-secret"
	}
	now := time.Now()
	issuedAt := now.Add(-time.Hour)
	claims := jwt.MapClaims{
		"uid":      uid,
		"user_id":  user.ID,
		"username": user.Username,
		"iat":      issuedAt.Unix(),
		"exp":      now.Add(24 * time.Hour).Unix(),
	}
	if reportRunID != "" {
		claims["report_run_id"] = reportRunID
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = strings.TrimSpace(req.EmployeeID)
	}
	if username == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username and password are required"})
		return
	}
	result, err := h.aihub.Login(username, req.Password)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": err.Error()})
		return
	}
	tokenUID, _ := extractAIHubUID(result.Token, "")
	userID := tokenUID
	if userID == 0 {
		userID = result.UserID
	}
	if userID == 0 {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "aihub token missing uid"})
		return
	}

	aihubUser, err := h.getAIHubUser(userID)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	var user *model.User
	if h.isBootstrapAdmin(*aihubUser) {
		user, err = h.syncExistingAIHubUser(*aihubUser)
		if errors.Is(err, sql.ErrNoRows) {
			user, err = h.upsertAIHubUserProfile(*aihubUser, "admin", nil, true)
		}
	} else {
		user, err = h.syncExistingAIHubUser(*aihubUser)
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Aida access is not enabled; contact an administrator to add this AIHub user"})
			return
		}
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !user.LocalEnabled {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Aida access is disabled"})
		return
	}
	writeJSON(w, http.StatusOK, model.LoginResponse{Token: result.Token, User: *user})
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusGone, map[string]string{"error": "local registration is disabled; use AIHub account"})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	if _, err := h.syncExistingAIHubUserID(parseUserID(u.ID)); err == nil {
		if fresh, loadErr := loadUserByID(h.db, u.ID); loadErr == nil {
			u = fresh
		}
	}
	writeJSON(w, http.StatusOK, u)
}

func (h *AuthHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	query := userSelectSQL() + `
		WHERE u.aida_enabled = true
		ORDER BY display_name`
	users, err := queryUsers(h.db, query)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *AuthHandler) SearchAIHubUsers(w http.ResponseWriter, r *http.Request) {
	pageSize := parsePositiveInt(r.URL.Query().Get("page_size"), 20)
	pageNum := parsePositiveInt(r.URL.Query().Get("page_num"), 1)
	searchKey := strings.TrimSpace(r.URL.Query().Get("search_key"))

	page, err := h.aihub.ListUsers(pageSize, pageNum, searchKey)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	statuses, err := h.aidaUserStatuses(page.Users)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items := make([]model.AIHubUserSearchItem, 0, len(page.Users))
	for _, user := range page.Users {
		status := statuses[user.ID]
		items = append(items, model.AIHubUserSearchItem{
			ID:              user.ID,
			Username:        user.Username,
			Nickname:        user.Nickname,
			Email:           user.Email,
			Status:          user.Status,
			AidaStatus:      status.Code,
			AidaStatusLabel: status.Label,
			CurrentAppRole:  status.AppRole,
			CurrentTeamID:   status.TeamID,
			CurrentTeamName: status.TeamName,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":     items,
		"total":     page.Total,
		"page_size": page.PageSize,
		"page_num":  page.PageNum,
	})
}

func (h *AuthHandler) ListTaskAssignees(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	if u == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}

	query := userSelectSQL() + `
		WHERE u.aida_enabled = true
		  AND u.local_enabled = true`
	args := []any{}
	switch u.Role {
	case "admin", "director", "pm":
		query += ` AND u.app_role IN ('employee', 'team_leader', 'pm', 'director')`
	case "team_leader":
		if u.TeamID == nil {
			writeJSON(w, http.StatusOK, []model.User{})
			return
		}
		query += ` AND u.team_id = $1 AND u.app_role IN ('employee', 'team_leader', 'pm', 'director')`
		args = append(args, *u.TeamID)
	case "employee":
		query += " AND u.id = $1"
		args = append(args, u.ID)
	default:
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "insufficient permissions"})
		return
	}
	query += " ORDER BY display_name"
	users, err := queryUsers(h.db, query, args...)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (h *AuthHandler) ListTeams(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(`
		SELECT t.id::text, t.name, t.director_user_id::text,
			COALESCE(NULLIF(u.nickname,''), u.username, ''), t.created_at, t.updated_at
		FROM teams t
		LEFT JOIN users u ON u.id = t.director_user_id
		ORDER BY t.name`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	teams := []model.Team{}
	for rows.Next() {
		var t model.Team
		var directorID, directorName sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &directorID, &directorName, &t.CreatedAt, &t.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if directorID.Valid {
			t.DirectorUserID = &directorID.String
		}
		if directorName.Valid && directorName.String != "" {
			t.DirectorName = &directorName.String
		}
		teams = append(teams, t)
	}
	writeJSON(w, http.StatusOK, teams)
}

func (h *AuthHandler) AdminCreateTeam(w http.ResponseWriter, r *http.Request) {
	var req model.AdminCreateTeamRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "团队名称不能为空"})
		return
	}
	var team model.Team
	var directorID sql.NullString
	err := h.db.QueryRowContext(r.Context(), `
		INSERT INTO teams (name, director_user_id)
		VALUES ($1, NULLIF($2, '')::bigint)
		RETURNING id::text, name, director_user_id::text, created_at, updated_at`,
		req.Name, stringValue(req.DirectorUserID),
	).Scan(&team.ID, &team.Name, &directorID, &team.CreatedAt, &team.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "团队名称已存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if directorID.Valid {
		team.DirectorUserID = &directorID.String
	}
	writeJSON(w, http.StatusCreated, team)
}

func (h *AuthHandler) AdminUpdateTeam(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "id")
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team id is required"})
		return
	}
	var req model.AdminCreateTeamRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "团队名称不能为空"})
		return
	}
	var team model.Team
	var directorID sql.NullString
	err := h.db.QueryRowContext(r.Context(), `
		UPDATE teams
		SET name = $1, director_user_id = NULLIF($2, '')::bigint, updated_at = now()
		WHERE id = $3
		RETURNING id::text, name, director_user_id::text, created_at, updated_at`,
		req.Name, stringValue(req.DirectorUserID), teamID,
	).Scan(&team.ID, &team.Name, &directorID, &team.CreatedAt, &team.UpdatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
			return
		}
		if isUniqueViolation(err) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "团队名称已存在"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if directorID.Valid {
		team.DirectorUserID = &directorID.String
	}
	writeJSON(w, http.StatusOK, team)
}

func (h *AuthHandler) AdminDeleteTeam(w http.ResponseWriter, r *http.Request) {
	teamID := chi.URLParam(r, "id")
	if teamID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "team id is required"})
		return
	}
	var memberCount int
	if err := h.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM users WHERE team_id = $1", teamID).Scan(&memberCount); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if memberCount > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "该小组已有成员或业务数据，不能删除"})
		return
	}
	var referenceCount int
	if err := h.db.QueryRowContext(r.Context(), `
		SELECT
			(SELECT COUNT(*) FROM team_reports WHERE team_id = $1) +
			(SELECT COUNT(*) FROM team_weekly_reports WHERE team_id = $1) +
			(SELECT COUNT(*) FROM requirement_teams WHERE team_id = $1)`,
		teamID,
	).Scan(&referenceCount); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if referenceCount > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "该小组已有成员或业务数据，不能删除"})
		return
	}
	res, err := h.db.ExecContext(r.Context(), "DELETE FROM teams WHERE id = $1", teamID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "team not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "id": teamID})
}

func (h *AuthHandler) AdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	targetID := chi.URLParam(r, "id")
	if targetID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id is required"})
		return
	}
	var req model.AdminUpdateUserRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	role := req.AppRole
	if role == nil {
		role = req.Role
	}
	if role != nil && !isValidRole(*role) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid app_role"})
		return
	}
	uid := parseUserID(targetID)
	if uid == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	existing, err := loadAidaUserByID(h.db, targetID)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	appRole := existing.AppRole
	localEnabled := existing.LocalEnabled
	teamID := existing.TeamID
	if role != nil {
		appRole = *role
	}
	if req.LocalEnabled != nil {
		localEnabled = *req.LocalEnabled
	}
	if req.ClearTeam {
		teamID = nil
	} else if req.TeamID != nil {
		teamID = req.TeamID
	}
	teamID, err = h.normalizeRoleTeam(appRole, teamID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := h.protectLastAdmin(existing, appRole, localEnabled); err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	aihubUser, err := h.getAIHubUser(uid)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	user, err := h.upsertAIHubUserProfile(*aihubUser, appRole, teamID, localEnabled)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, user)
}

func (h *AuthHandler) AdminBatchAddUsers(w http.ResponseWriter, r *http.Request) {
	var req model.AdminBatchAddUsersRequest
	if err := readJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	appRole := strings.TrimSpace(req.AppRole)
	if !isValidRole(appRole) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid app_role"})
		return
	}
	teamID, err := h.normalizeRoleTeam(appRole, req.TeamID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	userIDs := uniquePositiveUserIDs(req.UserIDs)
	if len(userIDs) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_ids is required"})
		return
	}
	existingIDs, err := h.existingAidaUserIDs(userIDs)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	localEnabled := true
	if req.LocalEnabled != nil {
		localEnabled = *req.LocalEnabled
	}
	resp := model.AdminBatchAddUsersResponse{Results: []model.AdminBatchAddUserResult{}}
	for _, uid := range userIDs {
		id := strconv.FormatInt(uid, 10)
		if existingIDs[uid] {
			resp.Skipped++
			resp.SkippedExisting++
			resp.Results = append(resp.Results, model.AdminBatchAddUserResult{ID: id, Status: "skipped"})
			continue
		}
		aihubUser, err := h.getAIHubUser(uid)
		if err != nil {
			resp.Failed++
			resp.Results = append(resp.Results, model.AdminBatchAddUserResult{ID: id, Status: "failed", Error: err.Error()})
			continue
		}
		_, err = h.upsertAIHubUserProfile(*aihubUser, appRole, teamID, localEnabled)
		if err != nil {
			resp.Failed++
			resp.Results = append(resp.Results, model.AdminBatchAddUserResult{ID: id, Username: aihubUser.Username, Nickname: aihubUser.Nickname, Email: aihubUser.Email, Status: "failed", Error: err.Error()})
			continue
		}
		resp.Created++
		resp.Results = append(resp.Results, model.AdminBatchAddUserResult{ID: id, Username: aihubUser.Username, Nickname: aihubUser.Nickname, Email: aihubUser.Email, Status: "created"})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AuthHandler) getUserAndWrite(w http.ResponseWriter, id string) {
	u, err := loadUserByID(h.db, id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	writeJSON(w, http.StatusOK, u)
}

type aidaUserStatus struct {
	Code     string
	Label    string
	AppRole  *string
	TeamID   *string
	TeamName *string
}

func (h *AuthHandler) aidaUserStatuses(users []service.AIHubUser) (map[int64]aidaUserStatus, error) {
	ids := make([]int64, 0, len(users))
	for _, user := range users {
		if user.ID > 0 {
			ids = append(ids, user.ID)
		}
	}
	statuses := make(map[int64]aidaUserStatus, len(ids))
	for _, id := range ids {
		statuses[id] = aidaUserStatus{Code: "not_added", Label: "未添加"}
	}
	if len(ids) == 0 {
		return statuses, nil
	}
	rows, err := h.db.Query(`
		SELECT u.id, u.local_enabled, u.app_role, u.team_id::text, t.name
		FROM users u
		LEFT JOIN teams t ON t.id = u.team_id
		WHERE u.aida_enabled = true AND u.id = ANY($1)`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var localEnabled bool
		var appRole string
		var teamID, teamName sql.NullString
		if err := rows.Scan(&id, &localEnabled, &appRole, &teamID, &teamName); err != nil {
			return nil, err
		}
		status := aidaUserStatus{
			Code:    "disabled",
			Label:   "已关闭访问",
			AppRole: &appRole,
		}
		if localEnabled {
			status.Code = "active"
			status.Label = "已添加"
		}
		if teamID.Valid {
			status.TeamID = &teamID.String
		}
		if teamName.Valid {
			status.TeamName = &teamName.String
		}
		if localEnabled {
			statuses[id] = status
			continue
		}
		statuses[id] = status
	}
	return statuses, rows.Err()
}

func (h *AuthHandler) existingAidaUserIDs(userIDs []int64) (map[int64]bool, error) {
	existing := map[int64]bool{}
	if len(userIDs) == 0 {
		return existing, nil
	}
	rows, err := h.db.Query(`SELECT id FROM users WHERE aida_enabled = true AND id = ANY($1)`, pq.Array(userIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		existing[id] = true
	}
	return existing, rows.Err()
}

func (h *AuthHandler) teamExists(teamID string) (bool, error) {
	var exists bool
	err := h.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM teams WHERE id::text = $1)`, teamID).Scan(&exists)
	return exists, err
}

func (h *AuthHandler) teamCount() (int, error) {
	var count int
	err := h.db.QueryRow(`SELECT COUNT(*) FROM teams`).Scan(&count)
	return count, err
}

func normalizedTeamID(teamID *string) *string {
	if teamID == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*teamID)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func uniquePositiveUserIDs(values []int64) []int64 {
	seen := map[int64]bool{}
	ids := []int64{}
	for _, value := range values {
		if value <= 0 || seen[value] {
			continue
		}
		seen[value] = true
		ids = append(ids, value)
	}
	return ids
}

func roleRequiresTeam(role string) bool {
	return role == "employee" || role == "team_leader"
}

func roleForcesNoTeam(role string) bool {
	return role == "admin" || role == "director" || role == "pm"
}

func (h *AuthHandler) normalizeRoleTeam(role string, teamID *string) (*string, error) {
	if !isValidRole(role) {
		return nil, errors.New("invalid app_role")
	}
	if roleForcesNoTeam(role) {
		return nil, nil
	}
	normalized := normalizedTeamID(teamID)
	if normalized == nil {
		if count, err := h.teamCount(); err == nil && count == 0 {
			return nil, errors.New("请先创建小组")
		}
		return nil, errors.New("employee/team_leader 必须选择小组")
	}
	exists, err := h.teamExists(*normalized)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New("team not found")
	}
	return normalized, nil
}

func (h *AuthHandler) protectLastAdmin(existing *model.User, nextRole string, nextLocalEnabled bool) error {
	if existing == nil || existing.AppRole != "admin" || !existing.LocalEnabled {
		return nil
	}
	if nextRole == "admin" && nextLocalEnabled {
		return nil
	}
	count, err := h.enabledAdminCount()
	if err != nil {
		return err
	}
	if count <= 1 {
		if nextRole != "admin" {
			return errors.New("不能将最后一个启用的管理员降级")
		}
		return errors.New("不能关闭最后一个启用的管理员访问")
	}
	return nil
}

func (h *AuthHandler) enabledAdminCount() (int, error) {
	var count int
	err := h.db.QueryRow(`SELECT COUNT(*) FROM users WHERE aida_enabled = true AND local_enabled = true AND app_role = 'admin'`).Scan(&count)
	return count, err
}

func (h *AuthHandler) getAIHubUser(userID int64) (*service.AIHubUser, error) {
	if userID == 0 {
		return nil, errors.New("invalid user id")
	}
	aihubUser, err := h.aihub.GetUser(userID)
	if err != nil {
		return nil, err
	}
	if aihubUser.ID == 0 {
		aihubUser.ID = userID
	}
	return aihubUser, nil
}

func (h *AuthHandler) syncExistingAIHubUserID(userID int64) (*model.User, error) {
	aihubUser, err := h.getAIHubUser(userID)
	if err != nil {
		return nil, err
	}
	return h.syncExistingAIHubUser(*aihubUser)
}

func (h *AuthHandler) syncExistingAIHubUser(aihubUser service.AIHubUser) (*model.User, error) {
	existing, err := loadAidaUserByID(h.db, strconv.FormatInt(aihubUser.ID, 10))
	if err != nil {
		return nil, err
	}
	return h.updateAIHubUserMirror(aihubUser, existing.ID)
}

func (h *AuthHandler) updateAIHubUserMirror(aihubUser service.AIHubUser, userID string) (*model.User, error) {
	employeeID := strconv.FormatInt(aihubUser.ID, 10)
	displayName := strings.TrimSpace(aihubUser.Nickname)
	if displayName == "" {
		displayName = strings.TrimSpace(aihubUser.Username)
	}
	if displayName == "" {
		displayName = employeeID
	}
	_, err := h.db.Exec(`
		UPDATE users
		SET employee_id = $2,
			username = $3,
			nickname = $4,
			name = $5,
			email = $6,
			last_synced_at = now(),
			updated_at = now()
		WHERE id::text = $1 AND aida_enabled = true`,
		userID, employeeID, aihubUser.Username, aihubUser.Nickname, displayName, aihubUser.Email,
	)
	if err != nil {
		return nil, err
	}
	return loadAidaUserByID(h.db, userID)
}

func (h *AuthHandler) upsertAIHubUserProfile(aihubUser service.AIHubUser, appRole string, teamID *string, localEnabled bool) (*model.User, error) {
	if !isValidRole(appRole) {
		return nil, errors.New("invalid app_role")
	}
	employeeID := strconv.FormatInt(aihubUser.ID, 10)
	displayName := strings.TrimSpace(aihubUser.Nickname)
	if displayName == "" {
		displayName = strings.TrimSpace(aihubUser.Username)
	}
	if displayName == "" {
		displayName = employeeID
	}
	status := "active"
	if !localEnabled {
		status = "deactivated"
	}
	_, err := h.db.Exec(`
		INSERT INTO users (id, employee_id, username, nickname, name, email, app_role, role, team_id, local_enabled, aida_enabled, status, last_synced_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7, NULLIF($8, '')::uuid, $9, true, $10, now())
		ON CONFLICT (id) DO UPDATE SET
			employee_id = EXCLUDED.employee_id,
			username = EXCLUDED.username,
			nickname = EXCLUDED.nickname,
			name = EXCLUDED.name,
			email = EXCLUDED.email,
			app_role = EXCLUDED.app_role,
			role = EXCLUDED.role,
			team_id = EXCLUDED.team_id,
			local_enabled = EXCLUDED.local_enabled,
			aida_enabled = true,
			status = EXCLUDED.status,
			last_synced_at = now(),
			updated_at = now()`,
		aihubUser.ID, employeeID, aihubUser.Username, aihubUser.Nickname, displayName, aihubUser.Email, appRole, stringValue(teamID), localEnabled, status,
	)
	if err != nil {
		return nil, err
	}
	return loadUserByID(h.db, employeeID)
}

func (h *AuthHandler) isBootstrapAdmin(aihubUser service.AIHubUser) bool {
	return h.bootstrapAdminUIDs[aihubUser.ID] || h.bootstrapAdminUIDs[parseUserID(aihubUser.Username)]
}

func parseBootstrapAdminUIDs(raw string) map[int64]bool {
	uids := map[int64]bool{}
	for _, item := range strings.Split(raw, ",") {
		uid, err := strconv.ParseInt(strings.TrimSpace(item), 10, 64)
		if err == nil && uid > 0 {
			uids[uid] = true
		}
	}
	return uids
}

func loadUserByID(db *sql.DB, id string) (*model.User, error) {
	rows, err := db.Query(userSelectSQL()+" WHERE "+userIdentityMatchesSQL(), id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, sql.ErrNoRows
	}
	var u model.User
	if err := scanUser(rows, &u); err != nil {
		return nil, err
	}
	return &u, rows.Err()
}

func loadAidaUserByID(db *sql.DB, id string) (*model.User, error) {
	rows, err := db.Query(userSelectSQL()+" WHERE "+userIdentityMatchesSQL()+" AND u.aida_enabled = true", id)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		rows.Close()
		return nil, sql.ErrNoRows
	}
	var u model.User
	if err := scanUser(rows, &u); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if u.Role == "director" {
		var departmentID, departmentName string
		err := db.QueryRow(`
			SELECT id::text, name
			FROM departments
			WHERE director_user_id = $1
			ORDER BY id
			LIMIT 1`, u.ID).Scan(&departmentID, &departmentName)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil {
			u.DepartmentID = &departmentID
			u.DepartmentName = &departmentName
		}
	}
	return &u, nil
}

func queryUsers(db *sql.DB, query string, args ...any) ([]model.User, error) {
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := []model.User{}
	for rows.Next() {
		var u model.User
		if err := scanUser(rows, &u); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func userSelectSQL() string {
	return `
		SELECT u.id::text, u.username, u.nickname, COALESCE(u.email,''), COALESCE(NULLIF(u.nickname,''), NULLIF(u.name,''), u.username) AS display_name,
			u.employee_id, u.app_role, u.team_id::text, COALESCE(t.name, ''), u.local_enabled, u.last_synced_at, u.created_at, u.updated_at
		FROM users u
		LEFT JOIN teams t ON t.id = u.team_id`
}

func userIdentityMatchesSQL() string {
	return "(u.id::text = $1 OR u.employee_id = $1 OR u.username = $1)"
}

func scanUser(rows interface{ Scan(dest ...any) error }, u *model.User) error {
	var teamID, teamName sql.NullString
	var lastSyncedAt sql.NullTime
	if err := rows.Scan(&u.ID, &u.Username, &u.Nickname, &u.Email, &u.Name, &u.EmployeeID, &u.AppRole, &teamID, &teamName, &u.LocalEnabled, &lastSyncedAt, &u.CreatedAt, &u.UpdatedAt); err != nil {
		return err
	}
	u.Role = u.AppRole
	if u.Role == "" {
		u.Role = "employee"
	}
	u.Status = "active"
	if !u.LocalEnabled {
		u.Status = "deactivated"
	}
	if teamID.Valid && teamID.String != "" {
		u.TeamID = &teamID.String
	}
	if teamName.Valid && teamName.String != "" {
		u.TeamName = &teamName.String
	}
	if lastSyncedAt.Valid {
		u.LastSyncedAt = &lastSyncedAt.Time
	}
	return nil
}

func isValidRole(role string) bool {
	switch role {
	case "admin", "director", "pm", "team_leader", "employee":
		return true
	default:
		return false
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func parsePositiveInt(raw string, fallback int) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func parseUserID(id string) int64 {
	value, _ := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
	return value
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505"
	}
	return false
}
