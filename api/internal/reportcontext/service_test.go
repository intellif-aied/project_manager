package reportcontext

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/reportsource"
)

type sourceStub struct {
	page  reportsource.ContentPage
	err   error
	calls int
}

func (s *sourceStub) ReadAttachedSelection(context.Context, string, string, string, string, reportsource.Period, string) (reportsource.ContentPage, error) {
	s.calls++
	return s.page, s.err
}

func TestBuildPersonalDailyStoresCompleteFrozenContext(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectQuery("FROM report_run_contexts c").WithArgs("run-1", "7").WillReturnRows(
		sqlmock.NewRows([]string{"context_payload", "context_hash", "context_bytes"}),
	)
	mock.ExpectBegin()
	mock.ExpectQuery("FROM ai_runs").WithArgs("run-1", "7").WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectQuery("FROM users u LEFT JOIN teams").WithArgs("7").WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "team_id", "team_name"}).AddRow("7", "测试用户", "team-1", "研发一组"),
	)
	mock.ExpectQuery("FROM requirements r").WillReturnRows(emptyRequirementRows())
	mock.ExpectQuery("FROM tasks t").WillReturnRows(emptyTaskRows())
	mock.ExpectExec("INSERT INTO report_run_contexts").
		WithArgs("run-1", SchemaVersion, "selection-1", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	source := &sourceStub{page: reportsource.ContentPage{FrozenPayload: validFrozenDigestV2()}}
	svc := &Service{db: db, source: source}
	stored, err := svc.Build(context.Background(), BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalDaily,
		Period:   reportsource.Period{Start: "2026-07-16", End: "2026-07-16"},
		Timezone: biztime.Zone, TriggerSource: "manual", ModelID: "model-1",
		Target: Target{Type: "self", UserID: "7"}, SourceSelectionID: "selection-1",
		Representation: RepresentationWorkEvidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	if source.calls != 1 || stored.Bytes == 0 || len(stored.Hash) != 64 {
		t.Fatalf("unexpected stored context: calls=%d context=%+v", source.calls, stored)
	}
	var payload struct {
		SchemaVersion       string               `json:"schema_version"`
		SourceState         SourceState          `json:"source_state"`
		Requirements        []Requirement        `json:"requirements"`
		Tasks               []Task               `json:"tasks"`
		Sessions            []SessionSource      `json:"sessions"`
		Sources             Sources              `json:"sources"`
		WorkEvidence        json.RawMessage      `json:"work_evidence"`
		PresentationProfile *PresentationProfile `json:"presentation_profile"`
	}
	if err := json.Unmarshal(stored.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.SchemaVersion != SchemaVersion || !payload.SourceState.CoverageComplete || payload.SourceState.SourceMode != "sessions_only" || payload.SourceState.Mode != "digest_v2" {
		t.Fatalf("unexpected payload state: %+v", payload.SourceState)
	}
	if len(payload.Sessions) != 0 || len(payload.WorkEvidence) == 0 || len(payload.Requirements) != 0 || len(payload.Tasks) != 0 {
		t.Fatalf("unexpected payload facts: %+v", payload)
	}
	if len(payload.Sources.SessionDigest) != 0 {
		t.Fatal("new context must not duplicate the frozen digest in sources.session_digest")
	}
	if payload.PresentationProfile == nil || payload.PresentationProfile.SummaryFocus == "" || payload.PresentationProfile.ContentGrouping == "" {
		t.Fatalf("presentation profile was not frozen: %+v", payload.PresentationProfile)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBuildRequestFreezesProjectionCompatibility(t *testing.T) {
	request := BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalDaily,
		Period:   reportsource.Period{Start: "2026-07-23", End: "2026-07-23"},
		Timezone: biztime.Zone, Target: Target{Type: "self", UserID: "7"},
		SourceSelectionID: "selection-1",
	}
	if err := request.validate(); err != nil {
		t.Fatalf("legacy run without representation must remain readable: %v", err)
	}
	request.Representation = RepresentationWorkEvidence
	if err := request.validate(); err != nil {
		t.Fatalf("work evidence representation must be valid: %v", err)
	}
	request.Representation = "unknown"
	if !errors.Is(request.validate(), ErrInvalidRequest) {
		t.Fatal("unknown frozen representation must be rejected")
	}
}

func TestPresentationProfileForEveryManagedReportType(t *testing.T) {
	tests := map[string]PresentationProfile{
		ReportTypePersonalDaily: {
			SummaryFocus:    "个人当日推进的主要目标、关键成果、验证和整体状态；只有存在明确证据时才提及风险或阻塞。",
			ContentGrouping: "按个人工作目标归并；同一目标下的开发、文档、部署、验证和修复合并表达。",
		},
		ReportTypePersonalWeekly: {
			SummaryFocus:    "个人本周核心进展、里程碑、最新状态和明确风险。",
			ContentGrouping: "跨日期归并持续目标，不按星期或日报逐条复述。",
		},
		ReportTypeTeamDaily: {
			SummaryFocus:    "小组当日共同目标、团队交付、整体状态和共享阻塞。",
			ContentGrouping: "按小组共同目标归并，不默认逐人罗列；只有解释职责或阻塞归属时提及成员。",
		},
		ReportTypeTeamWeekly: {
			SummaryFocus:    "小组本周交付、关键里程碑、协作状态和明确风险。",
			ContentGrouping: "按小组业务目标与里程碑归并，不按成员或日期罗列。",
		},
		ReportTypeDepartmentDaily: {
			SummaryFocus:    "部门当日重要进展、整体状态和需要管理关注的跨团队问题。",
			ContentGrouping: "按部门级目标归并，不机械罗列小组；只有解释责任或依赖时提及小组。",
		},
		ReportTypeDepartmentWeekly: {
			SummaryFocus:    "部门本周整体成果、关键进度、跨团队依赖和管理关注项。",
			ContentGrouping: "按部门级目标和关键里程碑归并，不按小组逐份复述。",
		},
	}
	for reportType, want := range tests {
		t.Run(reportType, func(t *testing.T) {
			got, err := presentationProfileFor(reportType)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatalf("profile=%+v, want %+v", got, want)
			}
		})
	}
	if _, err := presentationProfileFor("unknown"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unknown report type error=%v", err)
	}
}

func TestProjectPayloadForRepresentationKeepsHistoricalRunShape(t *testing.T) {
	digest := validFrozenDigestV2()
	payload := Payload{
		Run:      Run{ReportType: ReportTypePersonalDaily},
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
		Sources:  Sources{SessionDigest: digest},
	}
	legacy, err := projectPayloadForRepresentation(payload, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Sessions) != 1 || len(legacy.Sources.SessionDigest) == 0 || legacy.WorkEvidence != nil || legacy.PresentationProfile != nil {
		t.Fatalf("historical run shape changed: %+v", legacy)
	}
	current, err := projectPayloadForRepresentation(payload, RepresentationWorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Sessions) != 0 || len(current.Sources.SessionDigest) != 0 || current.WorkEvidence == nil || current.PresentationProfile == nil {
		t.Fatalf("new run projection was not applied: %+v", current)
	}
}

func TestProjectPayloadForRepresentationFallsBackToFrozenDigest(t *testing.T) {
	digest := json.RawMessage(strings.Replace(
		string(validFrozenDigestV2()),
		`"work_unit_ref":"wu-3"`,
		`"work_unit_ref":"wu-2"`,
		1,
	))
	payload := Payload{
		Run:      Run{ReportType: ReportTypePersonalDaily},
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
		Sources:  Sources{SessionDigest: digest},
	}

	projected, err := projectPayloadForRepresentation(payload, RepresentationWorkEvidence)
	if err != nil {
		t.Fatalf("projection mismatch must fall back to the frozen digest: %v", err)
	}
	if len(projected.Sessions) != 1 || projected.WorkEvidence != nil {
		t.Fatalf("frozen digest fallback was not preserved: %+v", projected)
	}
	if len(projected.Sources.SessionDigest) != 0 {
		t.Fatal("fallback must still remove a byte-identical duplicate digest")
	}
	if projected.PresentationProfile == nil {
		t.Fatal("fallback must retain the report presentation profile")
	}
}

func TestProjectPayloadRemovesOnlyProvablyDuplicateLegacyDigest(t *testing.T) {
	digest := validFrozenDigestV2()
	payload := Payload{
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
		Sources:  Sources{SessionDigest: digest},
	}

	if err := removeDuplicateLegacyDigest(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sessions) != 1 || string(payload.Sessions[0].Digest) != string(digest) {
		t.Fatalf("canonical session digest was not preserved: %+v", payload.Sessions)
	}
	if len(payload.Sources.SessionDigest) != 0 {
		t.Fatal("provably duplicate legacy digest was retained")
	}
	payload.Sources.SessionDigest = json.RawMessage("\n" + string(digest))
	if err := removeDuplicateLegacyDigest(&payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Sources.SessionDigest) == 0 {
		t.Fatal("byte-distinct legacy digest must be retained even when its JSON value is equal")
	}

	distinct := json.RawMessage(`{"content_mode":"digest_v2","different":true}`)
	payload.Sources.SessionDigest = distinct
	if err := removeDuplicateLegacyDigest(&payload); err != nil {
		t.Fatal(err)
	}
	if string(payload.Sources.SessionDigest) != string(distinct) {
		t.Fatal("non-identical legacy digest must not be removed")
	}
}

func TestProjectPayloadUsesReadableFactsAndOmitsRawGoals(t *testing.T) {
	digest := validFrozenDigestV2()
	payload := Payload{
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
		Sources:  Sources{SessionDigest: digest},
	}

	projected, err := projectPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if projected.WorkEvidence == nil || len(projected.Sessions) != 0 || len(projected.Sources.SessionDigest) != 0 {
		t.Fatalf("digest was not replaced by one work evidence projection: %+v", projected)
	}
	evidence := projected.WorkEvidence
	if len(evidence.Facts) != 3 {
		t.Fatalf("report-bearing facts were lost: %+v", evidence.Facts)
	}
	if evidence.Facts[0].Kind != "result" ||
		evidence.Facts[0].Text != "same result" ||
		evidence.Facts[0].Source != "tool_result" ||
		len(evidence.Facts[0].Observations) != 1 ||
		evidence.Facts[0].Observations[0].Date != "2026-07-23" ||
		evidence.Facts[0].Observations[0].Status != "completed" {
		t.Fatalf("plain fact lost report semantics: %+v", evidence.Facts[0])
	}
	if evidence.Facts[2].Kind != "unresolved" || evidence.Facts[2].Text != "follow up" || evidence.Facts[2].Source != "" {
		t.Fatalf("unresolved fact was lost: %+v", evidence.Facts[2])
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	visible := string(encoded)
	for _, forbidden := range []string{
		`"fact_columns"`, `"lookup_columns"`, `"row_reference_base"`,
		`"evidence_by_exact_goal"`, `"work_unit_ref"`, `"goal"`,
		`"evidence_refs"`, `"selection_id"`, `"digest_sha256"`,
	} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("work evidence leaked transport or raw goal field %s: %s", forbidden, visible)
		}
	}
	if !strings.Contains(visible, `"facts":[{"kind":"result","text":"same result","source":"tool_result","observations":[{"date":"2026-07-23"`) ||
		!strings.Contains(visible, `"kind":"unresolved","text":"follow up"`) {
		t.Fatalf("work evidence is not readable object JSON: %s", visible)
	}
}

func TestProjectReportFactTextKeepsCompleteSupportedReply(t *testing.T) {
	input := "已完成方案收口。测试已全部通过。 ## 实现细节\n" + strings.Repeat("内部实现和路径 ", 1000)
	if got := projectReportFactText(input); got != strings.TrimSpace(input) {
		t.Fatalf("complete supported reply changed: chars=%d", len(got))
	}
}

func TestProjectReportFactTextRejectsHandoffPromptArtifact(t *testing.T) {
	input := `[{"type":"text","text":"下面这段可直接交给下一个能访问工作区的大模型：\n\n` +
		"```text\\n请接手并继续执行全部任务\\n```" + `"}]`
	if got := projectReportFactText(input); got != "" {
		t.Fatalf("handoff prompt became report fact: %q", got)
	}
}

func TestProjectReportFactTextKeepsSingleSupportedStatement(t *testing.T) {
	input := "定位到报告超时来自工具结果过大，并完成回归验证"
	if got := projectReportFactText(input); got != input {
		t.Fatalf("single supported statement changed: %q", got)
	}
}

func TestProjectPayloadRejectsDuplicateWorkUnitReference(t *testing.T) {
	digest := json.RawMessage(strings.Replace(string(validFrozenDigestV2()), `"work_unit_ref":"wu-3"`, `"work_unit_ref":"wu-2"`, 1))
	_, err := projectPayload(Payload{
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
	})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("duplicate work unit reference must fail projection, got %v", err)
	}
}

func TestProjectPayloadRejectsContradictoryTruncatedCoverage(t *testing.T) {
	digest := json.RawMessage(strings.Replace(
		string(validFrozenDigestV2()),
		`"outcome_coverage":{"source_count":3`,
		`"highlights_truncated":true,"outcome_coverage":{"source_count":3`,
		1,
	))
	_, err := projectPayload(Payload{
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
	})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("truncated day must fail projection even when coverage claims complete, got %v", err)
	}
}

func TestProjectPayloadDoesNotExposeRawPendingGoalWithoutOutcome(t *testing.T) {
	longPrompt := strings.Repeat("raw-user-prompt-", 1000)
	digest := strings.Replace(
		string(validFrozenDigestV2()),
		`"status":"partial","evidence_grade":"B","goal":"other goal","result_statements":[{"text":"other result","source":"agent_claim","evidence_refs":null}],"unresolved":[{"text":"follow up","evidence_ref":"ev-3"}]`,
		`"status":"pending","evidence_grade":"B","goal":"`+longPrompt+`","result_statements":[],"unresolved":[]`,
		1,
	)
	projected, err := projectPayload(Payload{
		Sessions: []SessionSource{{
			SelectionID: "selection-1",
			Mode:        "digest_v2",
			Digest:      json.RawMessage(digest),
		}},
	})
	if err != nil {
		t.Fatalf("pending fact without outcome must remain valid: %v", err)
	}
	if projected.WorkEvidence == nil {
		t.Fatal("work evidence is missing")
	}
	if len(projected.WorkEvidence.Facts) != 1 {
		t.Fatalf("goal-only pending input became a report fact: %+v", projected.WorkEvidence.Facts)
	}
	encoded, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), longPrompt) || strings.Contains(string(encoded), `"goal"`) {
		t.Fatal("raw user prompt leaked into Agent-facing Projection")
	}
}

func TestProjectPayloadRejectsIncompleteFactAndSourceIdentity(t *testing.T) {
	for name, digest := range map[string]json.RawMessage{
		"empty result text": json.RawMessage(strings.Replace(string(validFrozenDigestV2()), `"text":"same result"`, `"text":""`, 1)),
		"empty source item": json.RawMessage(strings.Replace(string(validFrozenDigestV2()), `"source_item_ref":"item-1"`, `"source_item_ref":""`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := projectPayload(Payload{
				Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
			})
			if !errors.Is(err, ErrIncomplete) {
				t.Fatalf("incomplete frozen fact must fail projection, got %v", err)
			}
		})
	}
}

func validFrozenDigestV2() json.RawMessage {
	return json.RawMessage(`{
		"content_mode":"digest_v2",
		"timezone":"Asia/Shanghai",
		"digest_version":"session-digest/v2.10.0",
		"redaction_version":"report-redaction/v1",
		"content_snapshot_at":"2026-07-23T12:00:00+08:00",
		"completeness":"complete",
		"returned_item_count":1,
		"has_more":false,
		"coverage":{"complete":true,"source_item_count":1,"represented_item_count":1,"source_event_count":9,"included_event_count":3,"omitted_event_count":6,"truncated_item_count":1},
		"report_period_summary":{"start_date":"2026-07-23","end_date":"2026-07-23","result_work_unit_count":3,"days":[{"date":"2026-07-23","result_work_unit_count":3,"highlights":[
			{"work_unit_ref":"wu-1","sequence":1,"activity_end_at":"2026-07-23T09:00:00+08:00","category":"implementation","status":"completed","evidence_grade":"A","goal":"same goal","result_statements":[{"text":"same result","source":"tool_result","evidence_refs":["ev-1"]}],"unresolved":[]},
			{"work_unit_ref":"wu-2","sequence":2,"activity_end_at":"2026-07-23T10:00:00+08:00","category":"validation","status":"completed","evidence_grade":"A","goal":"same goal","result_statements":[{"text":"same result","source":"tool_result","evidence_refs":["ev-2"]}],"unresolved":[]},
			{"work_unit_ref":"wu-3","sequence":3,"activity_end_at":"2026-07-23T11:00:00+08:00","category":"investigation","status":"partial","evidence_grade":"B","goal":"other goal","result_statements":[{"text":"other result","source":"agent_claim","evidence_refs":null}],"unresolved":[{"text":"follow up","evidence_ref":"ev-3"}]}
		],"outcome_coverage":{"source_count":3,"represented_count":3,"complete":true,"text_compacted":false}}]},
		"items":[{"source_item_ref":"item-1","session_ref":"session-1","agent_type":"codex","activity_start_at":"2026-07-23T08:00:00+08:00","activity_end_at":"2026-07-23T11:00:00+08:00","digest_sha256":"abc","coverage":{"source_event_count":9,"included_event_count":3,"omitted_event_count":6,"truncated":true,"representation":"period_result_focused"},"digest":{"coverage":{"source_work_unit_count":3,"detailed_work_unit_count":0,"aggregated_work_unit_count":3}}}]
	}`)
}

func TestBuildReturnsExistingContextWithoutReadingSources(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	payload := []byte(`{"schema_version":"report-context/v1"}`)
	mock.ExpectQuery("FROM report_run_contexts c").WithArgs("run-1", "7").WillReturnRows(
		sqlmock.NewRows([]string{"context_payload", "context_hash", "context_bytes"}).AddRow(payload, "abc", len(payload)),
	)
	source := &sourceStub{}
	svc := &Service{db: db, source: source}
	stored, err := svc.Build(context.Background(), BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalDaily,
		Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Timezone: biztime.Zone,
		Target: Target{Type: "self", UserID: "7"}, SourceSelectionID: "selection-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Hash != "abc" || source.calls != 0 {
		t.Fatalf("existing context was not reused: %+v calls=%d", stored, source.calls)
	}
}

func TestPersonalWeeklyAllowsNoSessionSelection(t *testing.T) {
	request := BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalWeekly,
		Period: reportsource.Period{Start: "2026-07-13", End: "2026-07-19"}, Timezone: biztime.Zone,
		Target: Target{Type: "self", UserID: "7"},
	}
	if err := request.validate(); err != nil {
		t.Fatalf("personal weekly without selection must be valid: %v", err)
	}
	request.ReportType = ReportTypePersonalDaily
	if err := request.validate(); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("personal daily without selection must fail, got %v", err)
	}
}

func TestLoadTeamScopeIncludesCurrentLeaderIdentity(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	mock.ExpectBegin()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery("SELECT t.name").
		WithArgs("team-1").
		WillReturnRows(sqlmock.NewRows([]string{"name", "leader_id", "leader_name"}).AddRow("研发一组", "7", "负责人甲"))
	mock.ExpectQuery("FROM users u LEFT JOIN teams").
		WithArgs("team-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "team_id", "team_name"}).
			AddRow("7", "负责人甲", "team-1", "研发一组").
			AddRow("8", "成员乙", "team-1", "研发一组"))

	scope, err := loadScope(context.Background(), tx, BuildRequest{
		UserID: "7", ReportType: ReportTypeTeamDaily,
		Target: Target{Type: "team", TeamID: "team-1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(scope)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"leader_id":"7"`) || !strings.Contains(string(encoded), `"leader_name":"负责人甲"`) {
		t.Fatalf("team leader identity missing from scope: %s", encoded)
	}
	mock.ExpectRollback()
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCollectMemberCoverageKeepsMissingAndInvalidMembers(t *testing.T) {
	members := []Actor{{ID: "1", Name: "甲"}, {ID: "2", Name: "乙"}, {ID: "3", Name: "丙"}}
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	rows := []reportRow{
		{ID: "r1", Owner: members[0], Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Content: "完成 A", UpdatedAt: now},
		{ID: "r3", Owner: members[2], Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Content: "  ", UpdatedAt: now},
	}
	reports, coverage, issues, err := collectMemberCoverage(members, rows, ReportTypePersonalDaily, "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || len(coverage) != 3 || len(issues) != 2 {
		t.Fatalf("unexpected coverage result: reports=%d coverage=%+v issues=%+v", len(reports), coverage, issues)
	}
	if coverage[1].SourceStatus != "missing" || coverage[2].SourceStatus != "invalid" || coverage[2].ReportID != "r3" {
		t.Fatalf("missing/invalid semantics lost: %+v", coverage)
	}
}

func TestCollectMemberCoverageRejectsDuplicateReports(t *testing.T) {
	member := Actor{ID: "1", Name: "甲"}
	rows := []reportRow{{ID: "r1", Owner: member, Content: "a"}, {ID: "r2", Owner: member, Content: "b"}}
	_, _, _, err := collectMemberCoverage([]Actor{member}, rows, ReportTypePersonalDaily, "")
	if !errors.Is(err, ErrDuplicate) {
		t.Fatalf("expected duplicate error, got %v", err)
	}
}

func TestWeeklyReportWithMismatchedEndIsInvalid(t *testing.T) {
	member := Actor{ID: "1", Name: "甲"}
	rows := []reportRow{{
		ID: "r1", Owner: member,
		Period:  reportsource.Period{Start: "2026-07-13", End: "2026-07-18"},
		Content: "周报正文",
	}}
	reports, coverage, issues, err := collectMemberCoverage([]Actor{member}, rows, ReportTypePersonalWeekly, "2026-07-19")
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 0 || len(issues) != 1 || coverage[0].SourceStatus != "invalid" || coverage[0].InvalidReason != "period_mismatch" {
		t.Fatalf("mismatched weekly period was not rejected: reports=%+v coverage=%+v issues=%+v", reports, coverage, issues)
	}
}

func TestBuildRejectsMissingFrozenPayload(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("FROM report_run_contexts c").WithArgs("run-1", "7").WillReturnRows(
		sqlmock.NewRows([]string{"context_payload", "context_hash", "context_bytes"}),
	)
	svc := &Service{db: db, source: &sourceStub{page: reportsource.ContentPage{}}}
	_, err = svc.Build(context.Background(), BuildRequest{
		UserID: "7", RunID: "run-1", ReportType: ReportTypePersonalDaily,
		Period: reportsource.Period{Start: "2026-07-16", End: "2026-07-16"}, Timezone: biztime.Zone,
		Target: Target{Type: "self", UserID: "7"}, SourceSelectionID: "selection-1",
	})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("expected ErrIncomplete, got %v", err)
	}
}

func TestGetScopesContextToRunOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectQuery("FROM report_run_contexts c").
		WithArgs("run-1", "7").
		WillReturnRows(sqlmock.NewRows([]string{"context_payload", "context_hash", "context_bytes"}).AddRow([]byte(`{"schema_version":"report-context/v1"}`), "abc", 38))
	svc := &Service{db: db}
	stored, err := svc.Get(context.Background(), "7", "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Hash != "abc" {
		t.Fatalf("unexpected hash %q", stored.Hash)
	}
}

func emptyRequirementRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "title", "description", "status", "priority", "progress", "deadline", "creator_id", "creator_name", "creator_team_id", "creator_team_name", "responsibles", "team_ids", "updated_at"})
}

func emptyTaskRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "requirement_id", "requirement_title", "title", "status", "priority", "progress", "due_date", "creator_id", "creator_name", "creator_team_id", "creator_team_name", "responsibles", "updated_at"})
}
