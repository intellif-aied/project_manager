package handler

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/model"
	"github.com/aidashboard/api/service"
	"github.com/lib/pq"
)

type DashboardHandler struct {
	db *sql.DB
}

func NewDashboardHandler(db *sql.DB) *DashboardHandler {
	return &DashboardHandler{db: db}
}

func (h *DashboardHandler) Follows(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	rows, err := h.db.Query(`
		SELECT target_type, target_id
		FROM user_follows
		WHERE user_id = $1
		ORDER BY created_at DESC`, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer rows.Close()
	type target struct{ targetType, targetID string }
	targets := []target{}
	for rows.Next() {
		var item target
		if err := rows.Scan(&item.targetType, &item.targetID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		targets = append(targets, item)
	}

	items := []model.DashboardFollowItem{}
	for _, target := range targets {
		if target.targetType == "requirement" {
			item, ok := h.requirementFollowItem(target.targetID, u.ID)
			if ok {
				item.FollowedByMe = true
				items = append(items, item)
			}
			continue
		}
		item, ok := h.taskFollowItem(target.targetID, u.ID)
		if ok {
			item.FollowedByMe = true
			items = append(items, item)
		}
	}
	sortDashboardFollowItems(items)
	if shouldUseDashboardPagedResponse(r) {
		page, pageSize := parsePagination(r, 20, 100)
		writeJSON(w, http.StatusOK, paginateDashboardFollowItems(items, page, pageSize))
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *DashboardHandler) MyItems(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	itemsByKey := map[string]model.DashboardFollowItem{}

	requirementRows, err := h.db.Query(`
		SELECT DISTINCT r.id,
			EXISTS (
				SELECT 1 FROM user_follows f
				WHERE f.user_id = $1 AND f.target_type = 'requirement' AND f.target_id = r.id
			) AS followed_by_me,
			(r.creator_id = $1) AS created_by_me,
			EXISTS (
				SELECT 1 FROM requirement_responsibles rr
				WHERE rr.requirement_id = r.id AND rr.user_id = $1
			) AS assigned_to_me
		FROM requirements r
		WHERE r.status NOT IN ('completed', 'cancelled')
			AND (
				r.creator_id = $1
				OR EXISTS (
					SELECT 1 FROM requirement_responsibles rr
					WHERE rr.requirement_id = r.id AND rr.user_id = $1
				)
				OR EXISTS (
					SELECT 1 FROM user_follows f
					WHERE f.user_id = $1 AND f.target_type = 'requirement' AND f.target_id = r.id
				)
			)`, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer requirementRows.Close()

	for requirementRows.Next() {
		var id string
		var followedByMe, createdByMe, assignedToMe bool
		if err := requirementRows.Scan(&id, &followedByMe, &createdByMe, &assignedToMe); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item, ok := h.requirementFollowItem(id, u.ID)
		if !ok {
			continue
		}
		item.FollowedByMe = followedByMe
		item.CreatedByMe = createdByMe
		item.AssignedToMe = assignedToMe
		itemsByKey[item.Key] = item
	}
	if err := requirementRows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	taskRows, err := h.db.Query(`
		SELECT DISTINCT t.id,
			EXISTS (
				SELECT 1 FROM user_follows f
				WHERE f.user_id = $1 AND f.target_type = 'task' AND f.target_id = t.id
			) AS followed_by_me,
			(t.creator_id = $1) AS created_by_me,
			EXISTS (
				SELECT 1 FROM task_responsibles tr
				WHERE tr.task_id = t.id AND tr.user_id = $1
			) AS assigned_to_me
		FROM tasks t
		JOIN requirements r ON r.id = t.requirement_id
		WHERE t.status <> 'done'
			AND r.status NOT IN ('completed', 'cancelled')
			AND (
				t.creator_id = $1
				OR EXISTS (
					SELECT 1 FROM task_responsibles tr
					WHERE tr.task_id = t.id AND tr.user_id = $1
				)
				OR EXISTS (
					SELECT 1 FROM user_follows f
					WHERE f.user_id = $1 AND f.target_type = 'task' AND f.target_id = t.id
				)
			)`, u.ID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	defer taskRows.Close()

	for taskRows.Next() {
		var id string
		var followedByMe, createdByMe, assignedToMe bool
		if err := taskRows.Scan(&id, &followedByMe, &createdByMe, &assignedToMe); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		item, ok := h.taskFollowItem(id, u.ID)
		if !ok {
			continue
		}
		item.FollowedByMe = followedByMe
		item.CreatedByMe = createdByMe
		item.AssignedToMe = assignedToMe
		itemsByKey[item.Key] = item
	}
	if err := taskRows.Err(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	items := make([]model.DashboardFollowItem, 0, len(itemsByKey))
	for _, item := range itemsByKey {
		if item.FollowedByMe || item.CreatedByMe || item.AssignedToMe {
			items = append(items, item)
		}
	}
	sortDashboardFollowItems(items)
	if shouldUseDashboardPagedResponse(r) {
		page, pageSize := parsePagination(r, 20, 100)
		writeJSON(w, http.StatusOK, paginateDashboardFollowItems(items, page, pageSize))
		return
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *DashboardHandler) Risks(w http.ResponseWriter, r *http.Request) {
	u := getUser(r)
	now := biztime.Now()
	requirementFacts, err := h.loadRequirementRiskFacts(u.ID, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	taskFacts, err := h.loadTaskRiskFacts(u, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	groups := h.dashboardRiskGroups(requirementFacts, taskFacts)
	sortDashboardRiskGroups(groups)
	if shouldUseDashboardPagedResponse(r) {
		page, pageSize := parsePagination(r, 20, 100)
		writeJSON(w, http.StatusOK, paginateDashboardRiskGroups(groups, page, pageSize))
		return
	}
	writeJSON(w, http.StatusOK, groups)
}

func shouldUseDashboardPagedResponse(r *http.Request) bool {
	q := r.URL.Query()
	return q.Get("page") != "" || q.Get("page_size") != ""
}

func paginateDashboardFollowItems(items []model.DashboardFollowItem, page, pageSize int) model.PaginatedDashboardFollowItems {
	total := len(items)
	start, end := pageBounds(total, page, pageSize)
	return model.PaginatedDashboardFollowItems{
		Items:    items[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
}

func paginateDashboardRiskGroups(items []model.DashboardRiskGroup, page, pageSize int) model.PaginatedDashboardRiskGroups {
	total := len(items)
	start, end := pageBounds(total, page, pageSize)
	return model.PaginatedDashboardRiskGroups{
		Items:    items[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}
}

func pageBounds(total, page, pageSize int) (int, int) {
	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return start, end
}

func (h *DashboardHandler) requirementFollowItem(id, userID string) (model.DashboardFollowItem, bool) {
	var req model.Requirement
	var deadline sql.NullString
	var updatedAt time.Time
	err := h.db.QueryRow(`
		SELECT r.id, r.title, r.status, r.deadline, COALESCE(COALESCE(NULLIF(u.nickname,''), u.username), ''), r.updated_at
		FROM requirements r
		JOIN users u ON u.id = r.creator_id
		WHERE r.id = $1`, id).Scan(&req.ID, &req.Title, &req.Status, &deadline, &req.CreatorName, &updatedAt)
	if err != nil || req.Status == "completed" || req.Status == "cancelled" {
		return model.DashboardFollowItem{}, false
	}
	req.Deadline = nullStringPtr(deadline)
	NewRequirementHandler(h.db, nil).loadProjection(&req, &model.User{ID: userID})
	attention := h.followAttention("requirement", req.ID)
	url := fmt.Sprintf("/requirements?requirementId=%s", req.ID)
	return model.DashboardFollowItem{
		Key:                         "requirement:" + req.ID,
		Type:                        "需求",
		Title:                       req.Title,
		RequirementID:               req.ID,
		CreatorID:                   req.CreatorID,
		CreatorName:                 req.CreatorName,
		RequirementResponsibleIDs:   req.ResponsibleUserIDs,
		RequirementResponsibleNames: responsibleUserNames(req.ResponsibleUsers),
		Status:                      requirementStatusLabel(req.Status),
		Deadline:                    displayDate(req.Deadline),
		Risk:                        requirementRiskLabel(req.RiskSummary),
		Activity:                    recentUpdateLabel(updatedAt),
		AttentionScore:              attention.score,
		AttentionLevel:              attentionLevel(attention.score),
		FollowerCount:               attention.count,
		RiskPriority:                requirementRiskPriority(req.RiskSummary),
		SortDueDate:                 req.Deadline,
		SortUpdatedAt:               updatedAt,
		Navigation: model.DashboardNavigationTarget{
			RequirementID: req.ID,
			URL:           url,
		},
	}, true
}

func (h *DashboardHandler) taskFollowItem(id, userID string) (model.DashboardFollowItem, bool) {
	task, err := h.loadTask(id, userID)
	if err != nil || task.Status == "done" {
		return model.DashboardFollowItem{}, false
	}
	var parentStatus string
	if err := h.db.QueryRow(`SELECT status FROM requirements WHERE id = $1`, task.RequirementID).Scan(&parentStatus); err != nil {
		return model.DashboardFollowItem{}, false
	}
	if parentStatus == "cancelled" || parentStatus == "completed" {
		return model.DashboardFollowItem{}, false
	}
	url := fmt.Sprintf("/requirements?requirementId=%s&taskId=%s", task.RequirementID, task.ID)
	dependency := ""
	blockingTasks := []model.TaskDep{}
	if task.DisplayStatus == "blocked" {
		blockingTasks = unfinishedDependencies(task)
		dependency = unfinishedDependencyNames(blockingTasks)
	}
	attention := h.followAttention("task", task.ID)
	return model.DashboardFollowItem{
		Key:                  "task:" + task.ID,
		Type:                 "任务",
		Title:                task.Title,
		Requirement:          task.RequirementTitle,
		RequirementID:        task.RequirementID,
		TaskID:               &task.ID,
		CreatorID:            task.CreatorID,
		CreatorName:          task.CreatorName,
		TaskResponsibleIDs:   task.ResponsibleUserIDs,
		TaskResponsibleNames: responsibleUserNames(task.ResponsibleUsers),
		Status:               taskStatusLabel(task.DisplayStatus),
		Deadline:             displayDate(task.DueDate),
		Risk:                 taskRiskLabel(task.RiskTypes),
		Dependency:           dependency,
		BlockingTasks:        blockingTasks,
		Activity:             recentUpdateLabel(task.UpdatedAt),
		AttentionScore:       attention.score,
		AttentionLevel:       attentionLevel(attention.score),
		FollowerCount:        attention.count,
		RiskPriority:         taskRiskPriority(task.RiskTypes),
		SortDueDate:          task.DueDate,
		SortUpdatedAt:        task.UpdatedAt,
		Navigation: model.DashboardNavigationTarget{
			RequirementID: task.RequirementID,
			TaskID:        &task.ID,
			URL:           url,
		},
	}, true
}

func (h *DashboardHandler) loadTask(id, userID string) (model.Task, error) {
	row := h.db.QueryRow(`
		SELECT t.id, t.requirement_id, r.title, t.title,
			COALESCE(t.acceptance_criteria, ARRAY[]::text[]),
			t.creator_id::text, COALESCE(COALESCE(NULLIF(c.nickname,''), c.username), ''), t.status, t.priority, t.progress, t.due_date,
			t.completed_at, t.created_at, t.updated_at, t.version
		FROM tasks t
		JOIN requirements r ON r.id = t.requirement_id
		JOIN users c ON c.id = t.creator_id
		WHERE t.id = $1`, id)
	task, err := scanProjectionTask(row)
	if err != nil {
		return model.Task{}, err
	}
	NewTaskHandler(h.db).enrichTask(&task, &model.User{ID: userID})
	return task, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProjectionTask(row rowScanner) (model.Task, error) {
	var task model.Task
	var ac pq.StringArray
	var dueDate sql.NullString
	var completedAt sql.NullTime
	err := row.Scan(
		&task.ID, &task.RequirementID, &task.RequirementTitle, &task.Title,
		&ac, &task.CreatorID, &task.CreatorName,
		&task.Status, &task.Priority, &task.Progress, &dueDate,
		&completedAt, &task.CreatedAt, &task.UpdatedAt, &task.Version,
	)
	if err != nil {
		return model.Task{}, err
	}
	task.AcceptanceCriteria = []string(ac)
	task.DueDate = nullStringPtr(dueDate)
	task.CompletedAt = nullTimePtr(completedAt)
	return task, nil
}

const (
	dashboardRiskTypeRequirementOverdue = "requirement_overdue"
	dashboardRiskTypeDeadline           = "deadline"
	dashboardRiskTypeDependencyBlocker  = "dependency_blocker"
	dashboardRiskTypeDependencyConflict = "dependency_conflict"

	dashboardRiskDisplayRequirementGroup = "requirement_group"
	dashboardRiskDisplaySingleTask       = "single_task"
)

type dashboardRequirementRiskFact struct {
	requirementID    string
	requirementTitle string
	deadline         *string
	updatedAt        time.Time
}

type dashboardTaskRiskFact struct {
	task                      model.Task
	riskTypes                 []string
	unfinishedDependencyCount int
}

type dashboardRiskGroupBuilder struct {
	group model.DashboardRiskGroup
	tasks map[string]*model.DashboardRiskTaskSummary
}

func (h *DashboardHandler) loadRequirementRiskFacts(userID string, now time.Time) ([]dashboardRequirementRiskFact, error) {
	today := dashboardDateString(now)
	rows, err := h.db.Query(`
		SELECT r.id, r.title, r.deadline, r.updated_at
		FROM requirements r
		WHERE r.status NOT IN ('completed', 'cancelled')
			AND r.deadline IS NOT NULL
			AND r.deadline < $2
			AND (
				r.creator_id = $1
				OR EXISTS (
					SELECT 1 FROM requirement_responsibles rr
					WHERE rr.requirement_id = r.id AND rr.user_id = $1
				)
				OR EXISTS (
					SELECT 1 FROM user_follows f
					WHERE f.user_id = $1
						AND f.target_type = 'requirement'
						AND f.target_id = r.id
				)
			)`, userID, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	facts := []dashboardRequirementRiskFact{}
	for rows.Next() {
		var fact dashboardRequirementRiskFact
		var deadline sql.NullString
		if err := rows.Scan(&fact.requirementID, &fact.requirementTitle, &deadline, &fact.updatedAt); err != nil {
			return nil, err
		}
		fact.deadline = normalizeDashboardDate(nullStringPtr(deadline))
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}

func (h *DashboardHandler) loadTaskRiskFacts(u *model.User, now time.Time) ([]dashboardTaskRiskFact, error) {
	rows, err := h.db.Query(`
		SELECT t.id, t.requirement_id, r.title, t.title,
			COALESCE(t.acceptance_criteria, ARRAY[]::text[]),
			t.creator_id::text, COALESCE(COALESCE(NULLIF(c.nickname,''), c.username), ''), t.status, t.priority, t.progress, t.due_date,
			t.completed_at, t.created_at, t.updated_at, t.version
		FROM tasks t
		JOIN requirements r ON r.id = t.requirement_id
		JOIN users c ON c.id = t.creator_id
		WHERE t.status <> 'done' AND r.status NOT IN ('completed', 'cancelled')
		AND (
			EXISTS (
				SELECT 1 FROM task_responsibles tr
				WHERE tr.task_id = t.id AND tr.user_id = $1
			)
			OR t.creator_id = $1
			OR r.creator_id = $1
			OR EXISTS (
				SELECT 1 FROM requirement_responsibles rr
				WHERE rr.requirement_id = r.id AND rr.user_id = $1
			)
			OR EXISTS (
				SELECT 1 FROM user_follows f
				WHERE f.user_id = $1
					AND f.target_type = 'task'
					AND f.target_id = t.id
			)
			OR EXISTS (
				SELECT 1 FROM user_follows f
				WHERE f.user_id = $1
					AND f.target_type = 'requirement'
					AND f.target_id = r.id
			)
		)`, u.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	taskHandler := NewTaskHandler(h.db)
	facts := []dashboardTaskRiskFact{}
	for rows.Next() {
		task, err := scanProjectionTask(rows)
		if err != nil {
			return nil, err
		}
		taskHandler.loadDeps(&task)
		task.RiskTypes = service.DeriveTaskRisks(task, now)
		riskTypes := dashboardTaskRiskTypes(task.RiskTypes)
		if len(riskTypes) == 0 {
			continue
		}
		facts = append(facts, dashboardTaskRiskFact{
			task:                      task,
			riskTypes:                 riskTypes,
			unfinishedDependencyCount: unfinishedDependencyCount(task),
		})
	}
	return facts, rows.Err()
}

func (h *DashboardHandler) dashboardRiskGroups(requirementFacts []dashboardRequirementRiskFact, taskFacts []dashboardTaskRiskFact) []model.DashboardRiskGroup {
	builders := map[string]*dashboardRiskGroupBuilder{}
	for _, fact := range requirementFacts {
		builder := dashboardRiskGroupBuilderFor(builders, fact.requirementID, fact.requirementTitle)
		builder.group.RequirementOverdue = true
		builder.group.RiskTypes = appendRiskType(builder.group.RiskTypes, dashboardRiskTypeRequirementOverdue)
		builder.group.SortHasOverdue = true
		builder.group.SortEarliestOverdueDate = earliestOptionalDate(builder.group.SortEarliestOverdueDate, fact.deadline)
		builder.group.SortUpdatedAt = latestTime(builder.group.SortUpdatedAt, fact.updatedAt)
	}

	for _, fact := range taskFacts {
		builder := dashboardRiskGroupBuilderFor(builders, fact.task.RequirementID, fact.task.RequirementTitle)
		taskSummary := dashboardRiskTaskSummary(fact)
		builder.tasks[fact.task.ID] = taskSummary
		builder.group.SortUpdatedAt = latestTime(builder.group.SortUpdatedAt, fact.task.UpdatedAt)
		for _, riskType := range fact.riskTypes {
			builder.group.RiskTypes = appendRiskType(builder.group.RiskTypes, riskType)
			switch riskType {
			case dashboardRiskTypeDeadline:
				builder.group.DeadlineTaskCount++
				builder.group.SortHasOverdue = true
				builder.group.SortEarliestOverdueDate = earliestOptionalDate(builder.group.SortEarliestOverdueDate, taskSummary.SortDueDate)
			case dashboardRiskTypeDependencyBlocker:
				builder.group.DependencyBlockerCount++
			case dashboardRiskTypeDependencyConflict:
				builder.group.DependencyConflictCount++
			}
		}
		if betterRepresentativeTask(taskSummary, builder.group.RepresentativeTask) {
			builder.group.RepresentativeTask = taskSummary
		}
	}

	groups := []model.DashboardRiskGroup{}
	for _, builder := range builders {
		group := builder.group
		if !group.RequirementOverdue && len(builder.tasks) == 0 {
			continue
		}
		group.AttentionScore = h.dashboardRiskGroupAttentionScore(group.RequirementID, builder.tasks)
		group.AttentionLevel = attentionLevel(group.AttentionScore)
		group.DisplayType = dashboardRiskDisplayRequirementGroup
		if !group.RequirementOverdue && len(builder.tasks) == 1 {
			group.DisplayType = dashboardRiskDisplaySingleTask
		}
		group.Summary = dashboardRiskGroupSummary(group)
		group.Level, group.Tone = dashboardRiskGroupSeverity(group)
		group.Deadline = dashboardRiskGroupDeadline(group)
		group.TargetURL = fmt.Sprintf("/requirements?requirementId=%s", group.RequirementID)
		group.ActionText = "查看需求"
		group.Navigation = model.DashboardNavigationTarget{
			RequirementID: group.RequirementID,
			URL:           group.TargetURL,
		}
		if group.DisplayType == dashboardRiskDisplaySingleTask && group.RepresentativeTask != nil {
			taskID := group.RepresentativeTask.TaskID
			group.TargetURL = fmt.Sprintf("/requirements?requirementId=%s&taskId=%s", group.RequirementID, taskID)
			group.ActionText = "查看任务"
			if containsRiskType(group.RepresentativeTask.RiskTypes, dashboardRiskTypeDependencyBlocker) &&
				!containsRiskType(group.RepresentativeTask.RiskTypes, dashboardRiskTypeDeadline) {
				group.ActionText = "处理依赖"
			}
			if containsRiskType(group.RepresentativeTask.RiskTypes, dashboardRiskTypeDependencyConflict) &&
				!containsRiskType(group.RepresentativeTask.RiskTypes, dashboardRiskTypeDependencyBlocker) &&
				!containsRiskType(group.RepresentativeTask.RiskTypes, dashboardRiskTypeDeadline) {
				group.ActionText = "检查排期"
			}
			group.Navigation = model.DashboardNavigationTarget{
				RequirementID: group.RequirementID,
				TaskID:        &taskID,
				URL:           group.TargetURL,
			}
		}
		groups = append(groups, group)
	}
	return groups
}

func dashboardRiskGroupBuilderFor(builders map[string]*dashboardRiskGroupBuilder, requirementID, requirementTitle string) *dashboardRiskGroupBuilder {
	if builder, ok := builders[requirementID]; ok {
		if builder.group.RequirementTitle == "" {
			builder.group.RequirementTitle = requirementTitle
		}
		return builder
	}
	builder := &dashboardRiskGroupBuilder{
		group: model.DashboardRiskGroup{
			Key:              "requirement:" + requirementID,
			RequirementID:    requirementID,
			RequirementTitle: requirementTitle,
			RiskTypes:        []string{},
		},
		tasks: map[string]*model.DashboardRiskTaskSummary{},
	}
	builders[requirementID] = builder
	return builder
}

func dashboardRiskTaskSummary(fact dashboardTaskRiskFact) *model.DashboardRiskTaskSummary {
	blockingDependencies := []model.TaskDep{}
	if containsRiskType(fact.riskTypes, dashboardRiskTypeDependencyBlocker) {
		blockingDependencies = unfinishedDependencies(fact.task)
	}
	summary := &model.DashboardRiskTaskSummary{
		TaskID:                    fact.task.ID,
		Title:                     fact.task.Title,
		Deadline:                  displayDate(fact.task.DueDate),
		RiskTypes:                 append([]string{}, fact.riskTypes...),
		BlockingDependencies:      blockingDependencies,
		UnfinishedDependencyCount: len(blockingDependencies),
		SortUpdatedAt:             fact.task.UpdatedAt,
	}
	if containsRiskType(fact.riskTypes, dashboardRiskTypeDeadline) {
		summary.SortDueDate = normalizeDashboardDate(fact.task.DueDate)
	}
	if fact.unfinishedDependencyCount == 0 {
		summary.UnfinishedDependencyCount = 0
	}
	return summary
}

func (h *DashboardHandler) dashboardRiskGroupAttentionScore(requirementID string, tasks map[string]*model.DashboardRiskTaskSummary) int {
	score := h.attentionScore("requirement", requirementID)
	for taskID := range tasks {
		if taskScore := h.attentionScore("task", taskID); taskScore > score {
			score = taskScore
		}
	}
	return score
}

func dashboardTaskRiskTypes(risks []string) []string {
	result := []string{}
	for _, risk := range risks {
		if risk == service.TaskRiskOverdue {
			result = appendRiskType(result, dashboardRiskTypeDeadline)
		}
	}
	for _, risk := range risks {
		if risk == service.TaskRiskBlocked {
			result = appendRiskType(result, dashboardRiskTypeDependencyBlocker)
		}
	}
	for _, risk := range risks {
		if risk == service.TaskRiskDependencyConflict {
			result = appendRiskType(result, dashboardRiskTypeDependencyConflict)
		}
	}
	return result
}

func appendRiskType(items []string, riskType string) []string {
	if containsRiskType(items, riskType) {
		return items
	}
	return append(items, riskType)
}

func containsRiskType(items []string, riskType string) bool {
	for _, item := range items {
		if item == riskType {
			return true
		}
	}
	return false
}

func unfinishedDependencyCount(task model.Task) int {
	return len(unfinishedDependencies(task))
}

func dashboardDateString(value time.Time) string {
	return biztime.Date(value)
}

func normalizeDashboardDate(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	normalized := strings.TrimSpace(*value)
	if len(normalized) >= 10 {
		normalized = normalized[:10]
	}
	return &normalized
}

func earliestOptionalDate(left, right *string) *string {
	if left == nil || *left == "" {
		return right
	}
	if right == nil || *right == "" {
		return left
	}
	if *right < *left {
		return right
	}
	return left
}

func latestTime(left, right time.Time) time.Time {
	if right.After(left) {
		return right
	}
	return left
}

func betterRepresentativeTask(candidate, current *model.DashboardRiskTaskSummary) bool {
	if candidate == nil {
		return false
	}
	if current == nil {
		return true
	}
	leftPriority := representativeTaskPriority(candidate.RiskTypes)
	rightPriority := representativeTaskPriority(current.RiskTypes)
	if leftPriority != rightPriority {
		return leftPriority > rightPriority
	}
	if !sameOptionalDate(candidate.SortDueDate, current.SortDueDate) {
		return optionalDateBefore(candidate.SortDueDate, current.SortDueDate)
	}
	return candidate.SortUpdatedAt.After(current.SortUpdatedAt)
}

func representativeTaskPriority(riskTypes []string) int {
	hasDeadline := containsRiskType(riskTypes, dashboardRiskTypeDeadline)
	hasBlocker := containsRiskType(riskTypes, dashboardRiskTypeDependencyBlocker)
	hasConflict := containsRiskType(riskTypes, dashboardRiskTypeDependencyConflict)
	if hasDeadline && hasBlocker {
		return 3
	}
	if hasDeadline {
		return 2
	}
	if hasBlocker {
		return 1
	}
	if hasConflict {
		return 1
	}
	return 0
}

func dashboardRiskGroupSummary(group model.DashboardRiskGroup) string {
	parts := []string{}
	if group.RequirementOverdue {
		parts = append(parts, "需求逾期")
	}
	if group.DeadlineTaskCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个任务逾期", group.DeadlineTaskCount))
	}
	if group.DependencyBlockerCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个依赖阻塞", group.DependencyBlockerCount))
	}
	if group.DependencyConflictCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个依赖冲突", group.DependencyConflictCount))
	}
	if len(parts) == 0 {
		return "暂无风险"
	}
	summary := strings.Join(parts, " · ")
	if group.RepresentativeTask != nil && group.DisplayType == dashboardRiskDisplayRequirementGroup {
		summary += "；重点任务：" + group.RepresentativeTask.Title
	}
	return summary
}

func dashboardRiskGroupSeverity(group model.DashboardRiskGroup) (string, string) {
	if group.RequirementOverdue || group.DeadlineTaskCount > 0 || group.DependencyBlockerCount > 0 {
		return "高", "red"
	}
	if group.DependencyConflictCount > 0 {
		return "中", "orange"
	}
	return "低", "blue"
}

func dashboardRiskGroupDeadline(group model.DashboardRiskGroup) string {
	if group.SortEarliestOverdueDate != nil && *group.SortEarliestOverdueDate != "" {
		return displayDate(group.SortEarliestOverdueDate)
	}
	if group.RepresentativeTask != nil {
		return group.RepresentativeTask.Deadline
	}
	return "未设置"
}

func sortDashboardRiskGroups(items []model.DashboardRiskGroup) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.SortHasOverdue != right.SortHasOverdue {
			return left.SortHasOverdue
		}
		if !sameOptionalDate(left.SortEarliestOverdueDate, right.SortEarliestOverdueDate) {
			return optionalDateBefore(left.SortEarliestOverdueDate, right.SortEarliestOverdueDate)
		}
		if left.AttentionScore != right.AttentionScore {
			return left.AttentionScore > right.AttentionScore
		}
		return left.SortUpdatedAt.After(right.SortUpdatedAt)
	})
}

type followAttention struct {
	score int
	count int
}

func (h *DashboardHandler) followAttention(targetType, targetID string) followAttention {
	return loadFollowAttention(h.db, targetType, targetID)
}

func loadFollowAttention(db *sql.DB, targetType, targetID string) followAttention {
	var attention followAttention
	if err := db.QueryRow(`
		SELECT
			COALESCE(SUM(CASE u.app_role
				WHEN 'director' THEN 100
				WHEN 'team_leader' THEN 50
				WHEN 'pm' THEN 40
				WHEN 'employee' THEN 10
				ELSE 0
			END), 0),
			COUNT(*)
		FROM user_follows f
		JOIN users u ON u.id = f.user_id
		WHERE f.target_type = $1 AND f.target_id = $2`, targetType, targetID).Scan(&attention.score, &attention.count); err != nil {
		return followAttention{}
	}
	return attention
}

func (h *DashboardHandler) attentionScore(targetType, targetID string) int {
	return loadFollowAttention(h.db, targetType, targetID).score
}

func attentionLevel(score int) string {
	if score >= 150 {
		return "high"
	}
	if score >= 80 {
		return "important"
	}
	if score >= 40 {
		return "notable"
	}
	return "normal"
}

func requirementRiskPriority(risk model.RequirementRiskSummary) int {
	if risk.RequirementOverdue > 0 {
		return 110
	}
	if risk.Overdue > 0 {
		return 100
	}
	if risk.Blocked > 0 {
		return 90
	}
	if risk.DependencyConflict > 0 {
		return 70
	}
	return 0
}

func taskRiskPriority(risks []string) int {
	for _, risk := range risks {
		if risk == service.TaskRiskOverdue {
			return 100
		}
	}
	for _, risk := range risks {
		if risk == service.TaskRiskBlocked {
			return 90
		}
	}
	for _, risk := range risks {
		if risk == service.TaskRiskDependencyConflict {
			return 70
		}
	}
	return 0
}

func sortDashboardFollowItems(items []model.DashboardFollowItem) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if left.RiskPriority != right.RiskPriority {
			return left.RiskPriority > right.RiskPriority
		}
		if left.AttentionScore != right.AttentionScore {
			return left.AttentionScore > right.AttentionScore
		}
		if !sameOptionalDate(left.SortDueDate, right.SortDueDate) {
			return optionalDateBefore(left.SortDueDate, right.SortDueDate)
		}
		return left.SortUpdatedAt.After(right.SortUpdatedAt)
	})
}

func sameOptionalDate(left, right *string) bool {
	leftEmpty := left == nil || *left == ""
	rightEmpty := right == nil || *right == ""
	if leftEmpty || rightEmpty {
		return leftEmpty == rightEmpty
	}
	return *left == *right
}

func optionalDateBefore(left, right *string) bool {
	leftEmpty := left == nil || *left == ""
	rightEmpty := right == nil || *right == ""
	if leftEmpty || rightEmpty {
		return !leftEmpty && rightEmpty
	}
	return *left < *right
}

func unfinishedDependencies(task model.Task) []model.TaskDep {
	items := []model.TaskDep{}
	for _, dependency := range task.Dependencies {
		if isUnfinishedDependency(dependency) {
			items = append(items, dependency)
		}
	}
	return items
}

func isUnfinishedDependency(dependency model.TaskDep) bool {
	if dependency.ItemType == "requirement" {
		return dependency.Status != "completed" && dependency.Status != "cancelled"
	}
	return dependency.Status != "done"
}

func unfinishedDependencyNames(dependencies []model.TaskDep) string {
	names := []string{}
	for _, dependency := range dependencies {
		title := dependency.TaskTitle
		if title == "" {
			title = dependency.Title
		}
		if len(dependency.ResponsibleNames) > 0 {
			names = append(names, fmt.Sprintf("%s（负责人 %s）", title, strings.Join(dependency.ResponsibleNames, "、")))
			continue
		}
		names = append(names, title)
	}
	if len(names) == 0 {
		return "未完成上游任务"
	}
	return strings.Join(names, "、")
}

func requirementRiskLabel(risk model.RequirementRiskSummary) string {
	if risk.RequirementOverdue > 0 {
		return "需求已逾期"
	}
	if risk.Overdue > 0 {
		return fmt.Sprintf("%d 个任务已逾期", risk.Overdue)
	}
	if risk.Blocked > 0 {
		return fmt.Sprintf("%d 个依赖阻塞", risk.Blocked)
	}
	if risk.DependencyConflict > 0 {
		return fmt.Sprintf("%d 个依赖冲突", risk.DependencyConflict)
	}
	return "正常推进"
}

func taskRiskLabel(risks []string) string {
	for _, risk := range risks {
		if risk == service.TaskRiskOverdue {
			return "已逾期"
		}
	}
	for _, risk := range risks {
		if risk == service.TaskRiskBlocked {
			return "依赖阻塞"
		}
	}
	for _, risk := range risks {
		if risk == service.TaskRiskDependencyConflict {
			return "依赖冲突"
		}
	}
	return "正常推进"
}

func responsibleUserNames(users []model.ResponsibleUser) []string {
	names := make([]string, 0, len(users))
	for _, user := range users {
		if strings.TrimSpace(user.Name) == "" {
			continue
		}
		names = append(names, user.Name)
	}
	return names
}

func firstNonEmpty(values []string, fallbackValue string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return fallbackValue
}

func requirementStatusLabel(status string) string {
	labels := map[string]string{"todo": "待开始", "review": "评审", "active": "进行中", "completed": "已完成", "cancelled": "已取消"}
	return fallback(labels[status], status)
}

func taskStatusLabel(status string) string {
	labels := map[string]string{"todo": "待办", "in_progress": "进行中", "blocked": "阻塞", "done": "已完成"}
	return fallback(labels[status], status)
}

func displayDate(value *string) string {
	if value == nil || *value == "" {
		return "未设置"
	}
	if len(*value) >= 10 {
		return (*value)[:10]
	}
	return *value
}

func recentUpdateLabel(value time.Time) string {
	today := biztime.StartOfDay(time.Now())
	updatedDay := biztime.StartOfDay(value)
	days := int(today.Sub(updatedDay).Hours() / 24)
	if days <= 0 {
		return "今天更新"
	}
	if days == 1 {
		return "昨天更新"
	}
	return fmt.Sprintf("%d 天前更新", days)
}

func pointerFallback(value *string, fallbackValue string) string {
	if value == nil {
		return fallbackValue
	}
	return fallback(*value, fallbackValue)
}

func fallback(value, fallbackValue string) string {
	if strings.TrimSpace(value) == "" {
		return fallbackValue
	}
	return value
}
