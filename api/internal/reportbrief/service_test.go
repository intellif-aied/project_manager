package reportbrief

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeDraftAcceptsCompleteEvidenceMap(t *testing.T) {
	available := map[string]struct{}{
		"fact-001": {},
		"fact-002": {},
		"fact-003": {},
	}
	draft := Draft{
		Workstreams: []Workstream{{
			Title:     "日报生成质量优化",
			Objective: "提高日报中工作结果与实际证据的一致性",
			Deliverables: []Deliverable{{
				Result:      "完成 Report Brief 两阶段生成方案并通过测试服验证",
				State:       "validated",
				Environment: "test",
				Validation:  "自动化测试通过，测试服真实流程返回日报内容",
				NextAction:  "待生产发布",
				FactRefs:    []string{"fact-002", "fact-001", "fact-001"},
			}},
		}},
		ExcludedFacts: []ExcludedFact{{FactRef: "fact-003", Reason: "discussion"}},
	}

	payload, err := normalizeDraft(draft, personalDaily, Period{Start: "2026-07-27", End: "2026-07-27"}, available)
	if err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != SchemaVersion || payload.ReportType != personalDaily {
		t.Fatalf("unexpected payload identity: %#v", payload)
	}
	refs := payload.Workstreams[0].Deliverables[0].FactRefs
	if len(refs) != 2 || refs[0] != "fact-001" || refs[1] != "fact-002" {
		t.Fatalf("fact refs = %#v, want sorted unique refs", refs)
	}
}

func TestNormalizeDraftRejectsInvalidEvidenceMaps(t *testing.T) {
	available := map[string]struct{}{"fact-001": {}, "fact-002": {}}
	base := Draft{Workstreams: []Workstream{{
		Title: "日报优化", Objective: "提高内容质量",
		Deliverables: []Deliverable{{
			Result: "完成结构化摘要", State: "validated", Environment: "test",
			Validation: "测试服验证通过", NextAction: "待生产发布", FactRefs: []string{"fact-001"},
		}},
	}}}

	tests := []struct {
		name  string
		draft Draft
	}{
		{name: "unaccounted fact", draft: base},
		{name: "included and excluded", draft: Draft{
			Workstreams:   base.Workstreams,
			ExcludedFacts: []ExcludedFact{{FactRef: "fact-001", Reason: "discussion"}, {FactRef: "fact-002", Reason: "trace"}},
		}},
		{name: "released outside production", draft: Draft{Workstreams: []Workstream{{
			Title: "日报优化", Objective: "提高内容质量", Deliverables: []Deliverable{{
				Result: "完成发布", State: "released", Environment: "test",
				Validation: "测试通过", NextAction: "持续观察", FactRefs: []string{"fact-001", "fact-002"},
			}},
		}}}},
		{name: "forbidden translation", draft: Draft{Workstreams: []Workstream{{
			Title: "通知优化", Objective: "提高内容质量", Deliverables: []Deliverable{{
				Result: "完成通知深链", State: "validated", Environment: "test",
				Validation: "测试通过", NextAction: "待生产发布", FactRefs: []string{"fact-001", "fact-002"},
			}},
		}}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeDraft(test.draft, personalDaily, Period{Start: "2026-07-27", End: "2026-07-27"}, available)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestNormalizeDraftAcceptsNoReportableWorkWhenEveryFactIsExcluded(t *testing.T) {
	payload, err := normalizeDraft(Draft{
		NoReportableWork: true,
		ExcludedFacts:    []ExcludedFact{{FactRef: "fact-001", Reason: "preparation"}},
	}, personalDaily, Period{Start: "2026-07-27", End: "2026-07-27"}, map[string]struct{}{"fact-001": {}})
	if err != nil {
		t.Fatal(err)
	}
	if !payload.NoReportableWork || len(payload.Workstreams) != 0 {
		t.Fatalf("unexpected no-work payload: %#v", payload)
	}
}

func TestNormalizeDraftMergesSameWorkstream(t *testing.T) {
	payload, err := normalizeDraft(Draft{Workstreams: []Workstream{
		{Title: "报告体验优化", Objective: "改善报告使用体验", Deliverables: []Deliverable{{
			Result: "完成交互优化", State: "validated", Environment: "test",
			Validation: "测试通过", NextAction: "待生产发布", FactRefs: []string{"fact-001"},
		}}},
		{Title: "报告体验优化", Objective: "改善报告使用体验", Deliverables: []Deliverable{{
			Result: "完成日期优化", State: "completed", Environment: "development",
			Validation: "代码检查通过", NextAction: "进入测试", FactRefs: []string{"fact-002"},
		}}},
	}}, personalDaily, Period{Start: "2026-07-27", End: "2026-07-27"}, map[string]struct{}{
		"fact-001": {}, "fact-002": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Workstreams) != 1 || len(payload.Workstreams[0].Deliverables) != 2 {
		t.Fatalf("workstreams = %#v, want one merged workstream", payload.Workstreams)
	}
}

func TestValidateTextIssuesRejectsInternalDetails(t *testing.T) {
	for _, text := range []string{
		"后端返回 REPORT_SOURCE_UNAVAILABLE",
		"镜像标签 20260727-a66ffdb-report-actions",
		"兼容路由 /reports/daily",
		"开放 UDP 123 并运行 systemd --user 服务",
		"通知跳转到 /agent 页面",
	} {
		if len(validateTextIssues("content", text, 0, MaxPayloadBytes)) == 0 {
			t.Fatalf("text %q should be rejected", text)
		}
	}
	if issues := validateTextIssues("content", "完成错误提示、通知跳转和操作布局优化，并在生产环境验证", 0, MaxPayloadBytes); len(issues) != 0 {
		t.Fatalf("reader-facing content rejected: %v", issues)
	}
}

func TestNormalizeDraftReturnsAllIdentifiableViolations(t *testing.T) {
	_, err := normalizeDraft(Draft{Workstreams: []Workstream{{
		Title: "通知深链", Objective: "兼容 /agent 路由",
		Deliverables: []Deliverable{{
			Result: "开放 UDP 123", State: "released", Environment: "test",
			Validation: "运行 systemd --user", NextAction: "检查 /agent", FactRefs: []string{"fact-999"},
		}},
	}}}, personalDaily, Period{Start: "2026-07-27", End: "2026-07-27"}, map[string]struct{}{
		"fact-001": {},
	})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error = %v, want ErrInvalid", err)
	}
	wantParts := []string{
		`title contains forbidden term "深链"`,
		"objective contains internal route",
		"result contains network port",
		"validation contains command-line flag",
		"released requires production environment",
		"unknown fact_ref fact-999",
		"fact_ref fact-001 is not accounted for",
	}
	for _, want := range wantParts {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestNormalizeDraftAllowsProductionWorkThatIsNotReleased(t *testing.T) {
	for _, state := range []string{"in_progress", "blocked", "completed"} {
		t.Run(state, func(t *testing.T) {
			payload, err := normalizeDraft(Draft{Workstreams: []Workstream{{
				Title: "生产问题处置", Objective: "恢复生产服务",
				Deliverables: []Deliverable{{
					Result: "完成问题定位", State: state, Environment: "production",
					Validation: "生产现象已复核", NextAction: "继续完成处置", FactRefs: []string{"fact-001"},
				}},
			}}}, personalDaily, Period{Start: "2026-07-27", End: "2026-07-27"}, map[string]struct{}{"fact-001": {}})
			if err != nil {
				t.Fatalf("production %s rejected: %v", state, err)
			}
			if payload.Workstreams[0].Deliverables[0].State != state {
				t.Fatalf("state changed: %#v", payload)
			}
		})
	}
}

func TestNormalizeDraftRequiresValidationNextActionAndExplicitNoWork(t *testing.T) {
	_, err := normalizeDraft(Draft{Workstreams: []Workstream{{
		Title: "日报优化", Objective: "提高内容质量",
		Deliverables: []Deliverable{{
			Result: "完成结构化摘要", State: "validated", Environment: "development", FactRefs: []string{"fact-001"},
		}},
	}}}, personalDaily, Period{Start: "2026-07-27", End: "2026-07-27"}, map[string]struct{}{"fact-001": {}})
	for _, want := range []string{"validation length", "next_action length", "validated requires test environment"} {
		if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), want) {
			t.Fatalf("error=%v, want %q", err, want)
		}
	}

	_, err = normalizeDraft(Draft{}, personalDaily, Period{Start: "2026-07-27", End: "2026-07-27"}, map[string]struct{}{})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "no_reportable_work must be true") {
		t.Fatalf("error=%v, want explicit no_reportable_work", err)
	}
}

func TestRecordInvalidAttemptUsesStageSpecificAtomicCounter(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{db: db}

	mock.ExpectQuery("INSERT INTO report_run_generation_attempts").
		WithArgs("00000000-0000-4000-8000-000000000001", "307", MaxBriefInvalidAttempts+1).
		WillReturnRows(sqlmock.NewRows([]string{"brief_invalid_attempts"}).AddRow(1))
	briefAttempts, err := service.recordInvalidAttempt(context.Background(), "307", "00000000-0000-4000-8000-000000000001", "brief")
	if err != nil || briefAttempts != 1 {
		t.Fatalf("brief attempts=%d err=%v", briefAttempts, err)
	}

	mock.ExpectQuery("INSERT INTO report_run_generation_attempts").
		WithArgs("00000000-0000-4000-8000-000000000001", "307", MaxResultInvalidAttempts+1).
		WillReturnRows(sqlmock.NewRows([]string{"result_invalid_attempts"}).AddRow(2))
	resultAttempts, err := service.recordInvalidAttempt(context.Background(), "307", "00000000-0000-4000-8000-000000000001", "result")
	if err != nil || resultAttempts != 2 {
		t.Fatalf("result attempts=%d err=%v", resultAttempts, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRejectInvalidBriefStopsAfterCorrectionBudget(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{db: db}
	mock.ExpectQuery("INSERT INTO report_run_generation_attempts").
		WithArgs("00000000-0000-4000-8000-000000000001", "307", MaxBriefInvalidAttempts+1).
		WillReturnRows(sqlmock.NewRows([]string{"brief_invalid_attempts"}).AddRow(3))

	_, err = service.rejectInvalidBrief(context.Background(), "307", "00000000-0000-4000-8000-000000000001", fmt.Errorf("%w: bad state", ErrInvalid))
	if !errors.Is(err, ErrBriefRetryExhausted) || !strings.Contains(err.Error(), "bad state") {
		t.Fatalf("error=%v, want retry exhausted with details", err)
	}
}

func TestRejectMalformedBriefUsesRunChecksAndCorrectionBudget(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{db: db}
	const (
		runID  = "00000000-0000-4000-8000-000000000001"
		userID = "307"
	)

	mock.ExpectQuery("SELECT business_type, status").
		WithArgs(runID, userID).
		WillReturnRows(sqlmock.NewRows([]string{
			"business_type", "status", "execution_stage", "model_id", "report_context_representation",
		}).AddRow("report_agent_run", "running", "agent_running", "deepseek-v4-flash", "work_evidence"))
	mock.ExpectQuery("INSERT INTO report_run_generation_attempts").
		WithArgs(runID, userID, MaxBriefInvalidAttempts+1).
		WillReturnRows(sqlmock.NewRows([]string{"brief_invalid_attempts"}).AddRow(3))

	_, err = service.RejectInvalid(context.Background(), userID, runID, "brief_json is malformed")
	if !errors.Is(err, ErrBriefRetryExhausted) || !strings.Contains(err.Error(), "brief_json is malformed") {
		t.Fatalf("error=%v, want retry exhausted with malformed JSON details", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
