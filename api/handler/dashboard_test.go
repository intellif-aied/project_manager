package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/model"
	"github.com/lib/pq"
)

func TestLoadDashboardMyItemRefsDefinesPersonalReportItemScope(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("SELECT DISTINCT r.id").
		WithArgs("307").
		WillReturnRows(sqlmock.NewRows([]string{"id", "followed_by_me", "created_by_me", "assigned_to_me"}).
			AddRow("req-created", false, true, false).
			AddRow("req-followed", true, false, false))
	mock.ExpectQuery("SELECT DISTINCT t.id").
		WithArgs("307").
		WillReturnRows(sqlmock.NewRows([]string{"id", "followed_by_me", "created_by_me", "assigned_to_me"}).
			AddRow("task-assigned", false, false, true))

	refs, err := loadDashboardMyItemRefs(context.Background(), db, "307")
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 {
		t.Fatalf("refs = %#v", refs)
	}
	if refs[0].TargetType != "requirement" || refs[0].TargetID != "req-created" || !refs[0].CreatedByMe {
		t.Fatalf("created requirement ref = %#v", refs[0])
	}
	if refs[1].TargetID != "req-followed" || !refs[1].FollowedByMe {
		t.Fatalf("followed requirement ref = %#v", refs[1])
	}
	if refs[2].TargetType != "task" || refs[2].TargetID != "task-assigned" || !refs[2].AssignedToMe {
		t.Fatalf("assigned task ref = %#v", refs[2])
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDashboardRisksUsesMyRiskScopeForAllRolesAndMergesTaskRisks(t *testing.T) {
	tests := []struct {
		name string
		role string
	}{
		{name: "director", role: "director"},
		{name: "pm", role: "pm"},
		{name: "employee", role: "employee"},
		{name: "team_leader", role: "team_leader"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.MatchExpectationsInOrder(false)

			userID := test.role + "-user"
			taskID := test.role + "-task"
			reqID := test.role + "-req"

			expectDashboardRequirementRiskQuery(mock, userID).
				WillReturnRows(sqlmock.NewRows(dashboardRequirementRiskColumns()))
			expectDashboardTaskRiskCandidateQuery(mock, userID).
				WillReturnRows(sqlmock.NewRows(dashboardRiskTaskColumns()).
					AddRow(taskID, reqID, "需求", "双风险任务", "{}", userID, "创建者", "in_progress", "high", 40, "2000-01-01", nil, testTime(), testTime(), int64(1)))
			expectTaskDependencies(mock, taskID, 2)
			expectAttentionScore(mock, "requirement", reqID, 0)
			expectAttentionScore(mock, "task", taskID, 0)

			h := NewDashboardHandler(db)
			req := httptest.NewRequest(http.MethodGet, "/dashboard/risks", nil)
			req = requestWithUser(req, &model.User{ID: userID, Role: test.role, Name: "当前用户"})
			rec := httptest.NewRecorder()

			h.Risks(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
			}
			var got []model.DashboardRiskGroup
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 {
				t.Fatalf("risk group count = %d, want 1; groups=%#v", len(got), got)
			}
			group := got[0]
			if group.DisplayType != "single_task" {
				t.Fatalf("displayType = %q, want single_task", group.DisplayType)
			}
			if group.RequirementID != reqID || group.RequirementTitle != "需求" {
				t.Fatalf("requirement = %s/%s, want %s/需求", group.RequirementID, group.RequirementTitle, reqID)
			}
			if group.DeadlineTaskCount != 1 || group.DependencyBlockerCount != 1 {
				t.Fatalf("counts deadline=%d blocker=%d, want 1/1", group.DeadlineTaskCount, group.DependencyBlockerCount)
			}
			if group.RepresentativeTask == nil {
				t.Fatal("representativeTask is nil")
			}
			if group.RepresentativeTask.TaskID != taskID {
				t.Fatalf("representative task id = %q, want %q", group.RepresentativeTask.TaskID, taskID)
			}
			assertRiskTypes(t, group.RepresentativeTask.RiskTypes, []string{"deadline", "dependency_blocker"})
			if group.RepresentativeTask.UnfinishedDependencyCount != 2 {
				t.Fatalf("unfinishedDependencyCount = %d, want 2", group.RepresentativeTask.UnfinishedDependencyCount)
			}
			if group.TargetURL != "/requirements?requirementId="+reqID+"&taskId="+taskID {
				t.Fatalf("targetUrl = %q", group.TargetURL)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("sql expectations: %v", err)
			}
		})
	}
}

func TestDashboardRisksIncludesRequirementOverdueForCreatedOrFollowedRequirement(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	const userID = "director-user"
	const reqID = "req-overdue"
	expectDashboardRequirementRiskQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRequirementRiskColumns()).
			AddRow(reqID, "超期需求", "2000-01-01", testTime()))
	expectDashboardTaskRiskCandidateQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRiskTaskColumns()))
	expectAttentionScore(mock, "requirement", reqID, 100)

	h := NewDashboardHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/risks", nil)
	req = requestWithUser(req, &model.User{ID: userID, Role: "director", Name: "总监"})
	rec := httptest.NewRecorder()

	h.Risks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []model.DashboardRiskGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("risk group count = %d, want 1; groups=%#v", len(got), got)
	}
	group := got[0]
	if group.DisplayType != "requirement_group" {
		t.Fatalf("displayType = %q, want requirement_group", group.DisplayType)
	}
	if !group.RequirementOverdue {
		t.Fatal("RequirementOverdue = false, want true")
	}
	assertRiskTypes(t, group.RiskTypes, []string{"requirement_overdue"})
	if group.RepresentativeTask != nil {
		t.Fatalf("representativeTask = %#v, want nil", group.RepresentativeTask)
	}
	if group.TargetURL != "/requirements?requirementId="+reqID {
		t.Fatalf("targetUrl = %q", group.TargetURL)
	}
	if group.AttentionScore != 100 || group.AttentionLevel != "important" {
		t.Fatalf("attention = %d/%s, want 100/important", group.AttentionScore, group.AttentionLevel)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDashboardRisksAggregatesOnlyVisibleTaskFacts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	const userID = "employee-user"
	const reqID = "req-shared"
	const visibleTaskID = "task-visible"
	expectDashboardRequirementRiskQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRequirementRiskColumns()))
	expectDashboardTaskRiskCandidateQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRiskTaskColumns()).
			AddRow(visibleTaskID, reqID, "共享需求", "我负责的超期任务", "{}", "creator", "创建者", "in_progress", "high", 20, "2000-01-02", nil, testTime(), testTime(), int64(1)))
	expectTaskDependencies(mock, visibleTaskID, 0)
	expectAttentionScore(mock, "requirement", reqID, 0)
	expectAttentionScore(mock, "task", visibleTaskID, 10)

	h := NewDashboardHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/risks", nil)
	req = requestWithUser(req, &model.User{ID: userID, Role: "employee", Name: "员工"})
	rec := httptest.NewRecorder()

	h.Risks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []model.DashboardRiskGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("risk group count = %d, want 1; groups=%#v", len(got), got)
	}
	group := got[0]
	if group.DisplayType != "single_task" {
		t.Fatalf("displayType = %q, want single_task", group.DisplayType)
	}
	if group.DeadlineTaskCount != 1 || group.DependencyBlockerCount != 0 {
		t.Fatalf("counts deadline=%d blocker=%d, want 1/0", group.DeadlineTaskCount, group.DependencyBlockerCount)
	}
	if group.RepresentativeTask == nil || group.RepresentativeTask.TaskID != visibleTaskID {
		t.Fatalf("representativeTask = %#v, want visible task", group.RepresentativeTask)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDashboardRisksMultipleVisibleTasksUseRequirementGroup(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	const userID = "pm-user"
	const reqID = "req-multiple"
	expectDashboardRequirementRiskQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRequirementRiskColumns()))
	expectDashboardTaskRiskCandidateQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRiskTaskColumns()).
			AddRow("task-one", reqID, "多任务需求", "超期任务一", "{}", userID, "创建者", "in_progress", "high", 20, "2000-01-01", nil, testTime(), testTime(), int64(1)).
			AddRow("task-two", reqID, "多任务需求", "超期任务二", "{}", userID, "创建者", "in_progress", "high", 20, "2000-01-02", nil, testTime(), testTime(), int64(1)))
	expectTaskDependencies(mock, "task-one", 0)
	expectTaskDependencies(mock, "task-two", 0)
	expectAttentionScore(mock, "requirement", reqID, 0)
	expectAttentionScore(mock, "task", "task-one", 0)
	expectAttentionScore(mock, "task", "task-two", 0)

	h := NewDashboardHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/risks", nil)
	req = requestWithUser(req, &model.User{ID: userID, Role: "pm", Name: "PM"})
	rec := httptest.NewRecorder()

	h.Risks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []model.DashboardRiskGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("risk group count = %d, want 1; groups=%#v", len(got), got)
	}
	group := got[0]
	if group.DisplayType != "requirement_group" {
		t.Fatalf("displayType = %q, want requirement_group", group.DisplayType)
	}
	if group.DeadlineTaskCount != 2 {
		t.Fatalf("deadlineTaskCount = %d, want 2", group.DeadlineTaskCount)
	}
	if group.TargetURL != "/requirements?requirementId="+reqID {
		t.Fatalf("targetUrl = %q, want requirement target", group.TargetURL)
	}
	if group.Navigation.TaskID != nil {
		t.Fatalf("navigation.taskId = %v, want nil", *group.Navigation.TaskID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDashboardRisksSortsUsingOnlyVisibleFacts(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	const userID = "employee-user"
	expectDashboardRequirementRiskQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRequirementRiskColumns()))
	expectDashboardTaskRiskCandidateQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRiskTaskColumns()).
			AddRow("task-late", "req-late", "较晚需求", "较晚可见任务", "{}", "creator", "创建者", "in_progress", "high", 20, "2000-01-03", nil, testTime(), testTime(), int64(1)).
			AddRow("task-early", "req-early", "较早需求", "较早可见任务", "{}", "creator", "创建者", "in_progress", "high", 20, "2000-01-02", nil, testTime(), testTime(), int64(1)))
	expectTaskDependencies(mock, "task-late", 0)
	expectTaskDependencies(mock, "task-early", 0)
	expectAttentionScore(mock, "requirement", "req-late", 0)
	expectAttentionScore(mock, "task", "task-late", 0)
	expectAttentionScore(mock, "requirement", "req-early", 0)
	expectAttentionScore(mock, "task", "task-early", 0)

	h := NewDashboardHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/risks", nil)
	req = requestWithUser(req, &model.User{ID: userID, Role: "employee", Name: "员工"})
	rec := httptest.NewRecorder()

	h.Risks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []model.DashboardRiskGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("risk group count = %d, want 2; groups=%#v", len(got), got)
	}
	if got[0].RequirementID != "req-early" {
		t.Fatalf("first requirement = %q, want req-early", got[0].RequirementID)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDashboardRisksExcludesUnrelatedTasksThroughScopedCandidateQuery(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.MatchExpectationsInOrder(false)

	const userID = "director-user"
	expectDashboardRequirementRiskQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRequirementRiskColumns()))
	expectDashboardTaskRiskCandidateQuery(mock, userID).
		WillReturnRows(sqlmock.NewRows(dashboardRiskTaskColumns()))

	h := NewDashboardHandler(db)
	req := httptest.NewRequest(http.MethodGet, "/dashboard/risks", nil)
	req = requestWithUser(req, &model.User{ID: userID, Role: "director", Name: "总监"})
	rec := httptest.NewRecorder()

	h.Risks(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var got []model.DashboardRiskGroup
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("risk group count = %d, want 0; groups=%#v", len(got), got)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestDashboardTaskRiskEvidenceIncludesBlockingSourcesWhenOverdueAndBlocked(t *testing.T) {
	dueDate := "2000-01-01"
	blockingTasks := []model.TaskDep{{
		ItemType:         "task",
		ItemID:           "upstream-task",
		Title:            "上游阻塞任务",
		TaskID:           "upstream-task",
		TaskTitle:        "上游阻塞任务",
		RequirementID:    "upstream-req",
		RequirementTitle: "上游需求",
		Status:           "in_progress",
		ResponsibleNames: []string{"测试06"},
	}}
	task := model.Task{
		ID:      "blocked-task",
		Title:   "逾期且依赖未完成的任务",
		DueDate: &dueDate,
	}
	riskTypes := []string{dashboardRiskTypeDeadline, dashboardRiskTypeDependencyBlocker}

	evidence := dashboardTaskRiskEvidence(task, riskTypes, blockingTasks)

	if evidence == nil {
		t.Fatal("risk evidence is nil")
	}
	if evidence.PrimaryRisk != dashboardRiskTypeDependencyBlocker {
		t.Fatalf("primaryRisk = %q, want %q", evidence.PrimaryRisk, dashboardRiskTypeDependencyBlocker)
	}
	if evidence.AffectedTaskCount != 1 || evidence.TotalRiskCount != 2 {
		t.Fatalf("counts affected/total = %d/%d, want 1/2", evidence.AffectedTaskCount, evidence.TotalRiskCount)
	}
	if len(evidence.Samples) != 1 {
		t.Fatalf("sample count = %d, want 1", len(evidence.Samples))
	}
	sample := evidence.Samples[0]
	if sample.TaskID != task.ID || sample.TaskTitle != task.Title {
		t.Fatalf("sample task = %s/%s, want %s/%s", sample.TaskID, sample.TaskTitle, task.ID, task.Title)
	}
	assertRiskTypes(t, sample.RiskTypes, riskTypes)
	if len(sample.BlockingSources) != 1 {
		t.Fatalf("blocking source count = %d, want 1", len(sample.BlockingSources))
	}
	if sample.BlockingSources[0].TaskID != "upstream-task" {
		t.Fatalf("blocking source task = %q, want upstream-task", sample.BlockingSources[0].TaskID)
	}
}

func TestDashboardTaskRiskEvidenceReturnsNilWithoutRisks(t *testing.T) {
	evidence := dashboardTaskRiskEvidence(model.Task{ID: "task"}, nil, nil)
	if evidence != nil {
		t.Fatalf("risk evidence = %#v, want nil", evidence)
	}
}

func expectDashboardRequirementRiskQuery(mock sqlmock.Sqlmock, userID string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`(?s)SELECT r\.id, r\.title, r\.deadline, r\.updated_at.*FROM requirements r.*r\.status NOT IN \('completed', 'cancelled'\).*r\.deadline IS NOT NULL.*r\.deadline < \$2.*r\.creator_id = \$1.*f\.user_id = \$1.*f\.target_type = 'requirement'`).
		WithArgs(userID, sqlmock.AnyArg())
}

func expectDashboardTaskRiskCandidateQuery(mock sqlmock.Sqlmock, userID string) *sqlmock.ExpectedQuery {
	return mock.ExpectQuery(`(?s)SELECT t\.id, t\.requirement_id.*FROM tasks t.*JOIN users c ON c\.id = t\.creator_id.*WHERE t\.status <> 'done'.*r\.status NOT IN \('completed', 'cancelled'\).*task_responsibles tr.*t\.creator_id = \$1.*r\.creator_id = \$1.*requirement_responsibles rr.*f\.user_id = \$1.*f\.target_type = 'task'.*f\.user_id = \$1.*f\.target_type = 'requirement'`).
		WithArgs(userID)
}

func expectTaskDependencies(mock sqlmock.Sqlmock, taskID string, unfinishedCount int) {
	depRows := sqlmock.NewRows([]string{
		"item_type", "item_id", "title", "task_id", "task_title",
		"requirement_id", "requirement_title", "status", "due_date",
		"responsible_user_ids", "responsible_names",
	})
	for i := 0; i < unfinishedCount; i++ {
		depRows.AddRow(
			"task",
			"dependency-"+taskID,
			"上游任务",
			"dependency-"+taskID,
			"上游任务",
			"req-upstream",
			"上游需求",
			"todo",
			nil,
			pq.StringArray{},
			pq.StringArray{},
		)
	}
	mock.ExpectQuery(`(?s)SELECT rel\.target_type.*FROM work_item_relations rel.*WHERE rel\.source_type = \$1 AND rel\.source_id = \$2`).
		WithArgs("task", taskID).
		WillReturnRows(depRows)
	mock.ExpectQuery(`(?s)SELECT rel\.source_type.*FROM work_item_relations rel.*WHERE rel\.target_type = \$1 AND rel\.target_id = \$2`).
		WithArgs("task", taskID).
		WillReturnRows(sqlmock.NewRows([]string{
			"item_type", "item_id", "title", "task_id", "task_title",
			"requirement_id", "requirement_title", "status", "due_date",
			"responsible_user_ids", "responsible_names",
		}))
}

func expectAttentionScore(mock sqlmock.Sqlmock, targetType, targetID string, score int) {
	mock.ExpectQuery(`SELECT\s+COALESCE\(SUM\(CASE u\.app_role`).
		WithArgs(targetType, targetID).
		WillReturnRows(sqlmock.NewRows([]string{"score", "count"}).AddRow(score, 1))
}

func assertRiskTypes(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("risk types = %#v, want %#v", got, want)
	}
	for _, item := range want {
		found := false
		for _, gotItem := range got {
			if gotItem == item {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("risk types = %#v, missing %q", got, item)
		}
	}
}

func dashboardRequirementRiskColumns() []string {
	return []string{"id", "title", "deadline", "updated_at"}
}

func dashboardRiskTaskColumns() []string {
	return []string{
		"id", "requirement_id", "requirement_title", "title",
		"acceptance_criteria", "creator_id", "creator_name", "status", "priority", "progress", "due_date",
		"completed_at", "created_at", "updated_at", "version",
	}
}

func testTime() time.Time {
	return time.Date(2026, 6, 24, 9, 0, 0, 0, time.UTC)
}
