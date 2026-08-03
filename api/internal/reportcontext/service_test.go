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
	"github.com/aidashboard/api/internal/reportmemory"
	"github.com/aidashboard/api/internal/reportsource"
	"github.com/aidashboard/api/internal/sessiondigestv2"
)

type sourceStub struct {
	page  reportsource.ContentPage
	err   error
	calls int
}

func TestProjectMemoryContextKeepsMatchedFactsAsOptionalBackground(t *testing.T) {
	context := projectMemoryContextFromHints([]reportmemory.HistoricalProjectHint{{
		ProjectRef: "project-1", CanonicalName: "Symphony",
		Aliases: []string{"Symphony 任务编排器"}, MatchedFactRef: []string{"fact-016"}, Confidence: 0.9,
	}})
	if context == nil || !strings.Contains(context.GroupingRule, "不是归属要求") ||
		!strings.Contains(context.GroupingRule, "两个相似项目不得因历史背景被合并") {
		t.Fatalf("grouping rule = %#v", context)
	}
	if len(context.Hints) != 1 || !strings.Contains(context.Hints[0].Instruction, "不是项目归属结论") ||
		!strings.Contains(context.Hints[0].Instruction, "自行判断是否采用") ||
		strings.Join(context.Hints[0].RelatedFactRefs, ",") != "fact-016" {
		t.Fatalf("hint instruction = %#v", context.Hints)
	}
	payload, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(payload), "recent_context") || strings.Contains(string(payload), "continuity_context") {
		t.Fatalf("compact Project Memory Context leaked historical content: %s", payload)
	}
	for _, forbidden := range []string{"matched_fact_refs", "anchor_fact_refs", "workstream_subject", "max_workstreams"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("Project Memory Context retained coercive field %q: %s", forbidden, payload)
		}
	}
	if !strings.Contains(string(payload), "related_fact_refs") {
		t.Fatalf("Project Memory Context did not expose optional Fact relations: %s", payload)
	}
}

func TestProjectMemoryContextKeepsUnanchoredCandidateOptional(t *testing.T) {
	context := projectMemoryContextFromHints([]reportmemory.HistoricalProjectHint{{
		ProjectRef: "project-1", CanonicalName: "芯片验证平台",
		Aliases: []string{"CATP"}, MatchedFactRef: []string{}, CandidateOnly: true,
	}})
	if context == nil || len(context.Hints) != 1 {
		t.Fatalf("context = %#v", context)
	}
	hint := context.Hints[0]
	if !hint.CandidateOnly || len(hint.RelatedFactRefs) != 0 {
		t.Fatalf("candidate hint = %#v", hint)
	}
	if !strings.Contains(hint.Instruction, "通常应忽略") || !strings.Contains(hint.Instruction, "不得为了使用候选而合并工作") {
		t.Fatalf("candidate contract = %#v", context)
	}
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
			SummaryFocus:    "个人当日推进的主要项目与工作成果；Git、测试、构建、合并和部署只用于关联工作主题，不作为日报结论。",
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
	legacy, err := projectPayloadForRepresentation(payload, "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(legacy.Sessions) != 1 || len(legacy.Sources.SessionDigest) == 0 || legacy.WorkEvidence != nil || legacy.PresentationProfile != nil {
		t.Fatalf("historical run shape changed: %+v", legacy)
	}
	current, err := projectPayloadForRepresentation(payload, RepresentationWorkEvidence, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Sessions) != 0 || len(current.Sources.SessionDigest) != 0 || current.WorkEvidence == nil || current.PresentationProfile == nil {
		t.Fatalf("new run projection was not applied: %+v", current)
	}
}

func TestProjectPayloadForRepresentationRejectsIncompleteProjection(t *testing.T) {
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

	if _, err := projectPayloadForRepresentation(payload, RepresentationWorkEvidence, false); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("incomplete Projection must fail instead of exposing the frozen Digest, got %v", err)
	}
}

func TestProjectPayloadRemovesOnlyProvablyDuplicateLegacyDigest(t *testing.T) {
	digest := validFrozenDigestV2()
	payload := Payload{
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
		Sources:  Sources{SessionDigest: digest},
		Run:      Run{ReportType: ReportTypePersonalDaily},
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

func TestProjectPayloadDoesNotAddFactRefsToOtherReportTypes(t *testing.T) {
	digest := validFrozenDigestV2()
	payload := Payload{
		Run:      Run{ReportType: ReportTypePersonalWeekly},
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
		Sources:  Sources{SessionDigest: digest},
	}

	projected, err := projectPayloadWithThreads(payload, true)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"fact_ref"`) || strings.Contains(string(encoded), `"thread_ref"`) || strings.Contains(string(encoded), `"threads"`) {
		t.Fatalf("non-personal-daily projection changed: %s", encoded)
	}
}

func TestProjectPayloadUsesReadableFactsAndOmitsRawGoals(t *testing.T) {
	digest := validFrozenDigestV2()
	payload := Payload{
		Run:      Run{ReportType: ReportTypePersonalDaily},
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: digest}},
		Sources:  Sources{SessionDigest: digest},
	}

	withoutThreads, err := projectPayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(withoutThreads.WorkEvidence.Threads) != 0 {
		t.Fatalf("personal report Agent received system work threads: %+v", withoutThreads.WorkEvidence.Threads)
	}
	for _, fact := range withoutThreads.WorkEvidence.Facts {
		if len(fact.ThreadRefs) != 0 {
			t.Fatalf("personal report Agent fact received system thread refs: %+v", fact)
		}
	}

	projected, err := projectPayloadWithThreads(payload, true)
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
	if len(evidence.Threads) != 3 ||
		evidence.Threads[0].ThreadRef != "thread-001" || evidence.Threads[0].Goal != "same goal" ||
		evidence.Threads[1].ThreadRef != "thread-002" || evidence.Threads[1].Goal != "same goal" ||
		evidence.Threads[2].ThreadRef != "thread-003" || evidence.Threads[2].Goal != "other goal" {
		t.Fatalf("work thread dictionary was not preserved: %+v", evidence.Threads)
	}
	if strings.Join(evidence.Facts[0].ThreadRefs, ",") != "thread-001,thread-002" ||
		strings.Join(evidence.Facts[1].ThreadRefs, ",") != "thread-003" ||
		strings.Join(evidence.Facts[2].ThreadRefs, ",") != "thread-003" {
		t.Fatalf("facts were not linked to their work threads: %+v", evidence.Facts)
	}
	if evidence.Facts[0].Kind != "result" ||
		evidence.Facts[0].FactRef != "fact-001" ||
		evidence.Facts[0].Text != "same result" ||
		evidence.Facts[0].Source != "tool_result" ||
		len(evidence.Facts[0].Observations) != 2 ||
		evidence.Facts[0].Observations[0].Date != "2026-07-23" ||
		evidence.Facts[0].Observations[0].ObservedAt != "2026-07-23T09:00:00+08:00" ||
		evidence.Facts[0].Observations[0].Category != "implementation" ||
		evidence.Facts[0].Observations[0].Status != "completed" ||
		evidence.Facts[0].Observations[0].OccurrenceCount != 1 ||
		evidence.Facts[0].Observations[1].ObservedAt != "2026-07-23T10:00:00+08:00" ||
		evidence.Facts[0].Observations[1].Category != "validation" ||
		evidence.Facts[0].Observations[1].Status != "completed" ||
		evidence.Facts[0].Observations[1].OccurrenceCount != 1 {
		t.Fatalf("plain fact lost report semantics: %+v", evidence.Facts[0])
	}
	if evidence.Facts[2].FactRef != "fact-003" || evidence.Facts[2].Kind != "unresolved" || evidence.Facts[2].Text != "follow up" || evidence.Facts[2].Source != "" {
		t.Fatalf("unresolved fact was lost: %+v", evidence.Facts[2])
	}
	encoded, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	visible := string(encoded)
	for _, forbidden := range []string{
		`"fact_columns"`, `"lookup_columns"`, `"row_reference_base"`,
		`"evidence_by_exact_goal"`, `"work_unit_ref"`, `"source_ref"`,
		`"evidence_refs"`, `"selection_id"`, `"digest_sha256"`, `"session-1"`,
	} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("work evidence leaked transport or raw goal field %s: %s", forbidden, visible)
		}
	}
	if !strings.Contains(visible, `"facts":[{"fact_ref":"fact-001","kind":"result","text":"same result","source":"tool_result","thread_refs":["thread-001","thread-002"],"observations":[{"date":"2026-07-23","observed_at":"2026-07-23T09:00:00+08:00","category":"implementation","status":"completed","occurrence_count":1`) ||
		!strings.Contains(visible, `"fact_ref":"fact-003","kind":"unresolved","text":"follow up"`) {
		t.Fatalf("work evidence is not readable object JSON: %s", visible)
	}
}

func TestProjectPayloadWithoutThreadsKeepsSourceScopedWorkUnitIdentity(t *testing.T) {
	var digest map[string]any
	if err := json.Unmarshal(validFrozenDigestV2(), &digest); err != nil {
		t.Fatal(err)
	}
	digest["returned_item_count"] = float64(2)
	coverage := digest["coverage"].(map[string]any)
	coverage["source_item_count"] = float64(2)
	coverage["represented_item_count"] = float64(2)
	items := digest["items"].([]any)
	secondItemJSON, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var secondItem map[string]any
	if err := json.Unmarshal(secondItemJSON, &secondItem); err != nil {
		t.Fatal(err)
	}
	secondItem["source_item_ref"] = "item-2"
	secondItem["session_ref"] = "session-2"
	digest["items"] = append(items, secondItem)

	period := digest["report_period_summary"].(map[string]any)
	days := period["days"].([]any)
	highlights := days[0].(map[string]any)["highlights"].([]any)
	highlights[0].(map[string]any)["source_ref"] = "session-1"
	highlights[1].(map[string]any)["source_ref"] = "session-2"
	highlights[1].(map[string]any)["work_unit_ref"] = "wu-1"
	highlights[2].(map[string]any)["source_ref"] = "session-1"

	encoded, err := json.Marshal(digest)
	if err != nil {
		t.Fatal(err)
	}
	payload := Payload{
		Run:      Run{ReportType: ReportTypePersonalDaily},
		Sessions: []SessionSource{{SelectionID: "selection-1", Mode: "digest_v2", Digest: encoded}},
		Sources:  Sources{SessionDigest: encoded},
	}
	projected, err := projectPayload(payload)
	if err != nil {
		t.Fatalf("source-scoped work units without exposed threads were rejected: %v", err)
	}
	if len(projected.WorkEvidence.Threads) != 0 {
		t.Fatalf("personal Agent received system work threads: %+v", projected.WorkEvidence.Threads)
	}
}

func TestAppendWorkEvidenceFactAggregatesOnlyEquivalentObservations(t *testing.T) {
	projection := WorkEvidence{}
	indexes := make(map[workEvidenceFactIdentity]int)
	observationIndexes := make([]map[workEvidenceObservationIdentity]int, 0)
	identity := workEvidenceFactIdentity{Kind: "result", Text: "same result", Source: "tool_result"}
	appendWorkEvidenceFact(&projection, indexes, &observationIndexes, identity, "thread-001", WorkEvidenceObservation{
		Date: "2026-07-23", ObservedAt: "2026-07-23T09:00:00+08:00",
		Category: "validation", Status: "completed",
	})
	appendWorkEvidenceFact(&projection, indexes, &observationIndexes, identity, "thread-002", WorkEvidenceObservation{
		Date: "2026-07-23", ObservedAt: "2026-07-23T10:00:00+08:00",
		Category: "validation", Status: "completed",
	})
	appendWorkEvidenceFact(&projection, indexes, &observationIndexes, identity, "thread-002", WorkEvidenceObservation{
		Date: "2026-07-23", ObservedAt: "2026-07-23T11:00:00+08:00",
		Category: "validation", Status: "failed",
	})

	observations := projection.Facts[0].Observations
	if len(observations) != 2 || observations[0].OccurrenceCount != 2 ||
		observations[0].FirstObservedAt != "2026-07-23T09:00:00+08:00" ||
		observations[0].ObservedAt != "2026-07-23T10:00:00+08:00" ||
		observations[1].Status != "failed" || observations[1].OccurrenceCount != 1 {
		t.Fatalf("observation status or occurrence evidence was lost: %+v", observations)
	}
	if strings.Join(projection.Facts[0].ThreadRefs, ",") != "thread-001,thread-002" {
		t.Fatalf("fact thread links were not deduplicated: %+v", projection.Facts[0].ThreadRefs)
	}
}

func TestProjectPayloadKeepsChronologicalCorrectionChain(t *testing.T) {
	digest := string(validFrozenDigestV2())
	for _, workUnitRef := range []string{"wu-1", "wu-2", "wu-3"} {
		digest = strings.Replace(
			digest,
			`"work_unit_ref":"`+workUnitRef+`"`,
			`"source_ref":"session-1","work_unit_ref":"`+workUnitRef+`"`,
			1,
		)
	}
	digest = strings.Replace(digest, `"text":"same result"`, `"text":"Digest 使用 LLM"`, 1)
	second := strings.Index(digest, `"source_ref":"session-1","work_unit_ref":"wu-2"`)
	if second < 0 {
		t.Fatal("second Work Unit marker missing")
	}
	digest = digest[:second] + strings.Replace(
		digest[second:], `"text":"same result"`, `"text":"Digest 没有 LLM"`, 1,
	)
	digest = strings.Replace(digest, `"text":"other result"`, `"text":"最终完成 Projection"`, 1)
	digest = strings.ReplaceAll(digest, `"category":"validation"`, `"category":"implementation"`)
	digest = strings.ReplaceAll(digest, `"category":"investigation"`, `"category":"implementation"`)

	projected, err := projectPayload(Payload{
		Sessions: []SessionSource{{
			SelectionID: "selection-1", Mode: "digest_v2", Digest: json.RawMessage(digest),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if projected.WorkEvidence == nil {
		t.Fatal("work evidence missing")
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	wrong := strings.Index(text, "Digest 使用 LLM")
	correction := strings.Index(text, "Digest 没有 LLM")
	completed := strings.Index(text, "最终完成 Projection")
	if wrong < 0 || correction <= wrong || completed <= correction || strings.Contains(text, "session-1") {
		t.Fatalf("chronological correction chain was lost or source identity leaked: %s", text)
	}
	for _, observedAt := range []string{
		"2026-07-23T09:00:00+08:00",
		"2026-07-23T10:00:00+08:00",
		"2026-07-23T11:00:00+08:00",
	} {
		if !strings.Contains(text, observedAt) {
			t.Fatalf("observation time %q was lost: %s", observedAt, text)
		}
	}
}

func TestProjectPayloadPreservesFactsAcrossWorkUnitStatesAndRemovesStructuralNoise(t *testing.T) {
	var digest frozenDigestV2
	if err := json.Unmarshal(validFrozenDigestV2(), &digest); err != nil {
		t.Fatal(err)
	}
	digest.ReportPeriod.Days[0].Highlights = []sessiondigestv2.DailyHighlight{
		{
			SourceRef: "session-1", WorkUnitRef: "wu-completed", Sequence: 1,
			ActivityEndAt: "2026-07-23T09:00:00+08:00", Category: "implementation", Status: "completed", EvidenceGrade: "A",
			ResultStatements: []sessiondigestv2.ResultStatement{{
				Text: "我会先检查现状。\n\n已完成上传并验证结果。", Source: "agent_claim",
			}},
		},
		{
			SourceRef: "session-1", WorkUnitRef: "wu-partial", Sequence: 2,
			ActivityEndAt: "2026-07-23T10:00:00+08:00", Category: "discussion", Status: "partial", EvidenceGrade: "B",
			ResultStatements: []sessiondigestv2.ResultStatement{{
				Text: "## 结果\n修复缓存失效问题。\n\n## 实现细节\n```go\nfunc internalOnly() {}\n```\n$ go test ./...\n\n## 验证\n12 项测试全部通过。", Source: "tool_result",
			}},
		},
		{
			SourceRef: "session-1", WorkUnitRef: "wu-unknown", Sequence: 3,
			ActivityEndAt: "2026-07-23T11:00:00+08:00", Category: "administrative", Status: "unknown", EvidenceGrade: "B",
			ResultStatements: []sessiondigestv2.ResultStatement{{
				Text: "发现发布配置缺少必要变量，当前部署存在失败风险。", Source: "agent_claim",
			}},
			Unresolved: []sessiondigestv2.Unresolved{{Text: "需要补齐测试服变量后重新验证。"}},
		},
		{
			SourceRef: "session-1", WorkUnitRef: "wu-git-only", Sequence: 4,
			ActivityEndAt: "2026-07-23T12:00:00+08:00", Category: "administrative", Status: "completed", EvidenceGrade: "C",
			ResultStatements: []sessiondigestv2.ResultStatement{{
				Text: "已执行 git add、git commit 和 push，提交号为 abc123。", Source: "agent_claim",
			}},
		},
	}
	digest.ReportPeriod.Days[0].OutcomeCoverage = sessiondigestv2.OutcomeCoverage{
		SourceCount: 4, RepresentedCount: 4, Complete: true,
	}
	digest.ReportPeriod.ResultWorkUnitCount = 4
	digest.ReportPeriod.Days[0].ResultWorkUnitCount = 4
	raw, err := json.Marshal(digest)
	if err != nil {
		t.Fatal(err)
	}

	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2, Digest: raw,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	for _, required := range []string{
		"已完成上传并验证结果。",
		"修复缓存失效问题。",
		"12 项测试全部通过。",
		"发现发布配置缺少必要变量，当前部署存在失败风险。",
		"需要补齐测试服变量后重新验证。",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("report fact %q was lost: %s", required, text)
		}
	}
	for _, noise := range []string{
		"我会先检查现状", "func internalOnly", "$ go test", "git add", "abc123",
	} {
		if strings.Contains(text, noise) {
			t.Fatalf("structural noise %q leaked into Projection: %s", noise, text)
		}
	}
}

func TestProjectPayloadKeepsBusinessValidationFromMixedGitStatement(t *testing.T) {
	digest := strings.Replace(
		string(validFrozenDigestV2()),
		`"text":"same result"`,
		`"text":"已完成分支 rebase 和 merge，前端测试及生产构建均通过。"`,
		1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	if !strings.Contains(text, "前端测试及生产构建均通过。") {
		t.Fatalf("business validation was removed with Git trace: %s", text)
	}
	if strings.Contains(text, "rebase") || strings.Contains(text, "merge") {
		t.Fatalf("Git trace was not removed from mixed statement: %s", text)
	}
}

func TestProjectPayloadKeepsBusinessFactsFromFlattenedStructures(t *testing.T) {
	for name, input := range map[string]string{
		"flattened headings":       "## 完成 ### 服务交付 - 完成 API 改造。 - 12/12 测试通过。",
		"delegated conclusion":     "审计结论已发给主代理：当前实现未并行执行，需要调整调度。",
		"markdown table":           "| 变更 | 状态 | 验证 | |---|---|---| | 缓存修复 | 完成 | 12/12 通过 |",
		"flattened Git validation": "Rebase completed successfully. - Branch: feat/cache - New HEAD: abc123 - All 548 frontend tests passed - Full Go test suite passed",
		"command prefix prose":     "`docker compose --env-file .env` 只负责变量替换，不会自动把变量注入容器；当前服务必须显式配置必要变量。",
	} {
		t.Run(name, func(t *testing.T) {
			digest := strings.Replace(
				string(validFrozenDigestV2()), `"text":"same result"`,
				`"text":`+string(mustJSON(t, input)), 1,
			)
			projected, err := projectPayload(Payload{Sessions: []SessionSource{{
				SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
				Digest: json.RawMessage(digest),
			}}})
			if err != nil {
				t.Fatal(err)
			}
			visible, err := json.Marshal(projected.WorkEvidence)
			if err != nil {
				t.Fatal(err)
			}
			text := string(visible)
			for _, required := range map[string][]string{
				"flattened headings":       {"完成 API 改造。", "12/12 测试通过。"},
				"delegated conclusion":     {"当前实现未并行执行", "需要调整调度"},
				"markdown table":           {"缓存修复", "完成", "12/12 通过"},
				"flattened Git validation": {"548 frontend tests passed", "Full Go test suite passed"},
				"command prefix prose":     {"只负责变量替换", "必须显式配置必要变量"},
			}[name] {
				if !strings.Contains(text, required) {
					t.Fatalf("business fact %q was lost: %s", required, text)
				}
			}
			for _, noise := range []string{"发给主代理", "|---|", "New HEAD", "abc123"} {
				if strings.Contains(text, noise) {
					t.Fatalf("structural noise %q leaked into Projection: %s", noise, text)
				}
			}
		})
	}
}

func TestProjectPayloadKeepsBusinessFactsAfterFlattenedCodeFences(t *testing.T) {
	input := "确认有问题，而且是两个问题叠加。这个请求参数是： ```text period_start=2026-07-16 ``` " +
		"日期筛选实际没有生效。后端只读取： ```text activity_from activity_to ``` " +
		"冷缓存耗时约 18 秒，并发时出现 30 秒失败；根因是日期条件未提前限制事件范围。"
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	for _, required := range []string{
		"确认有问题，而且是两个问题叠加", "日期筛选实际没有生效", "冷缓存耗时约 18 秒",
		"根因是日期条件未提前限制事件范围", "period_start=2026-07-16",
		"activity_from activity_to",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("business fact after flattened code fence %q was lost: %s", required, text)
		}
	}
	for _, noise := range []string{"```"} {
		if strings.Contains(text, noise) {
			t.Fatalf("flattened code noise %q leaked into Projection: %s", noise, text)
		}
	}
}

func TestProjectPayloadPreservesBusinessFactsAcrossMultilineFences(t *testing.T) {
	for name, testCase := range map[string]struct {
		input     string
		required  []string
		forbidden []string
	}{
		"text fence": {
			input:    "确认日期筛选异常。\n```text\nperiod_start=2026-07-16\n```\n修复后验证通过。",
			required: []string{"确认日期筛选异常", "period_start=2026-07-16", "修复后验证通过"},
		},
		"unknown fence": {
			input:    "形成业务规则。\n```decision\n审批失败时保留原状态。\n```\n专项测试通过。",
			required: []string{"形成业务规则", "审批失败时保留原状态", "专项测试通过"},
		},
		"unclosed fence": {
			input:    "已完成缓存修复。\n```go\nfunc cacheFix() {}\n未闭合围栏后的验证仍然通过。",
			required: []string{"已完成缓存修复", "未闭合围栏后的验证仍然通过"},
		},
		"known code fence": {
			input:     "已完成缓存修复。\n```go\nfunc cacheFix() {}\n```\n12 项测试通过。",
			required:  []string{"已完成缓存修复", "12 项测试通过"},
			forbidden: []string{"func cacheFix"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			text := projectSingleResultText(t, testCase.input)
			for _, required := range testCase.required {
				if !strings.Contains(text, required) {
					t.Fatalf("business fact %q was lost: %s", required, text)
				}
			}
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(text, forbidden) {
					t.Fatalf("known code %q leaked into Projection: %s", forbidden, text)
				}
			}
		})
	}
}

func TestProjectPayloadDoesNotTreatBusinessLanguageAsGitTrace(t *testing.T) {
	for name, testCase := range map[string]struct {
		input     string
		required  []string
		forbidden []string
	}{
		"data merge": {
			input: "完成两个数据源合并。", required: []string{"完成两个数据源合并"},
		},
		"message delivery": {
			input: "完成消息推送。", required: []string{"完成消息推送"},
		},
		"form submission failure": {
			input: "表单 提交：失败，需要修复。", required: []string{"表单", "失败", "需要修复"},
		},
		"mixed file metadata and validation": {
			input: "修改文件：config.go；验证失败，需要回滚。", required: []string{"验证失败", "需要回滚"},
		},
		"git followed by validation": {
			input: "已执行 git commit 和 git push，随后 12 项验证全部通过。", required: []string{"12 项验证全部通过"},
		},
		"deployment with commit metadata": {
			input:     "已完成提交、测试服部署和验收。\n功能提交：`94a058f`\n发布记录提交：`65054c0`\n测试服 API 健康检查通过\n回退镜像已保留。",
			required:  []string{"已完成", "测试服部署和验收", "测试服 API 健康检查通过", "回退镜像已保留"},
			forbidden: []string{"94a058f", "65054c0"},
		},
		"branch metadata inside result": {
			input:     "代码改造与回归测试完成，完整测试通过。当前改动位于 `fix/report-context-readable-facts`，尚未部署测试服。",
			required:  []string{"代码改造与回归测试完成", "完整测试通过", "尚未部署测试服"},
			forbidden: []string{"fix/report-context-readable-facts"},
		},
		"pure git": {
			input: "已执行 git commit 和 git push，提交号为 abc123。", forbidden: []string{"git commit", "abc123"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			text := projectSingleResultText(t, testCase.input)
			for _, required := range testCase.required {
				if !strings.Contains(text, required) {
					t.Fatalf("business fact %q was lost: %s", required, text)
				}
			}
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(text, forbidden) {
					t.Fatalf("Git trace %q was retained: %s", forbidden, text)
				}
			}
		})
	}
}

func TestProjectPayloadKeepsBusinessExplanationInsideTechnicalSection(t *testing.T) {
	input := "已完成并发隔离修复。\n\n## 技术细节\n\n" +
		"interactive Worker 只领取交互任务，后台积压不会占用预留容量。\n\n" +
		"```go\nfunc internalOnly() {}\n```\n\n" +
		"/api/internal/reportcontext/projection.go\n\n## 验证\n\n" +
		"5000 个后台任务排队时，交互任务仍被独立 Worker 领取。"
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	for _, required := range []string{
		"已完成并发隔离修复", "interactive Worker 只领取交互任务",
		"后台积压不会占用预留容量", "5000 个后台任务排队时",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("business explanation in technical section %q was lost: %s", required, text)
		}
	}
	for _, noise := range []string{"func internalOnly", "/api/internal/reportcontext/projection.go", "```"} {
		if strings.Contains(text, noise) {
			t.Fatalf("technical noise %q leaked into Projection: %s", noise, text)
		}
	}
}

func TestProjectPayloadDropsPromptCopiedToAnotherSession(t *testing.T) {
	input := "复制下面这段给另一个会话： ```text 继续执行生产发布。先检查服务状态，再构建镜像并发布。 ```"
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	for _, promptText := range []string{"复制下面这段", "另一个会话", "继续执行生产发布", "构建镜像并发布"} {
		if strings.Contains(text, promptText) {
			t.Fatalf("handoff prompt %q became report evidence: %s", promptText, text)
		}
	}
}

func TestProjectPayloadKeepsBusinessMappingThatContainsBranchMetadata(t *testing.T) {
	input := "当前 QEMU pipeline 使用非 TST 入口。具体是： " +
		"```text Jenkins job: ngu800p 分支: master PIPELINE_ID: qemu_tst_pipeline ``` " +
		"所以虽然名称包含 tst，但实际是非 TST 框架复用 TST 测试工具。"
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	for _, required := range []string{
		"当前 QEMU pipeline 使用非 TST 入口", "Jenkins job: ngu800p",
		"PIPELINE_ID: qemu_tst_pipeline", "实际是非 TST 框架复用 TST 测试工具",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("business mapping containing branch metadata %q was lost: %s", required, text)
		}
	}
}

func TestProjectPayloadKeepsGitRelatedFailureAndSecurityBlocker(t *testing.T) {
	input := "Push did not complete. The environment blocked git push because it would export internal platform details. " +
		"SSH also failed with connection reset, so the release remains blocked."
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	for _, required := range []string{
		"did not complete", "blocked git push", "export internal platform details",
		"connection reset", "release remains blocked",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Git-related failure or security blocker %q was lost: %s", required, text)
		}
	}
}

func TestProjectPayloadKeepsDeliveryAfterGitTraceInSameStatement(t *testing.T) {
	input := "已完成并部署。本地实现已提交并推送到 origin：主要内容包括硬件资源登记、lease 排队释放、" +
		"异常工单入口、利用率统计、后端 API 和前端硬件管理页面。服务健康检查通过。"
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	for _, required := range []string{
		"已完成并部署", "硬件资源登记", "lease 排队释放", "异常工单入口",
		"利用率统计", "后端 API", "前端硬件管理页面", "服务健康检查通过",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("delivery after Git trace %q was lost: %s", required, text)
		}
	}
}

func TestProjectPayloadKeepsGitAnalysisStatistics(t *testing.T) {
	input := "已完成 Context 膨胀分析：372 条回复中，121 条包含代码块，118 条包含 Git/Commit/Merge 信息，" +
		"196 条包含命令或文件路径；这些分类有重叠。"
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	for _, required := range []string{
		"372 条回复", "121 条包含代码块", "118 条包含 Git/Commit/Merge 信息",
		"196 条包含命令或文件路径", "这些分类有重叠",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Git analysis fact %q was lost: %s", required, text)
		}
	}
}

func TestProjectPayloadKeepsValidationAfterMergeClause(t *testing.T) {
	input := "Merge commit 已完成，工作区干净；API/daemon 全量测试已通过。"
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	visible, err := json.Marshal(projected.WorkEvidence)
	if err != nil {
		t.Fatal(err)
	}
	text := string(visible)
	if !strings.Contains(text, "API/daemon 全量测试已通过") {
		t.Fatalf("validation after merge clause was lost: %s", text)
	}
}

func mustJSON(t *testing.T, value string) []byte {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}

func TestProjectPayloadKeepsAllLegacyResultsWithoutSourceRefs(t *testing.T) {
	digest := strings.Replace(
		string(validFrozenDigestV2()),
		`{"text":"same result","source":"tool_result","evidence_refs":["ev-1"]}`,
		`{"text":"legacy first","source":"tool_result","evidence_refs":["ev-1"]},{"text":"legacy middle","source":"tool_result","evidence_refs":["ev-1"]},{"text":"legacy latest","source":"tool_result","evidence_refs":["ev-1"]}`,
		1,
	)
	projected, err := projectPayload(Payload{
		Sessions: []SessionSource{{
			SelectionID: "selection-1", Mode: "digest_v2", Digest: json.RawMessage(digest),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	visible, _ := json.Marshal(projected.WorkEvidence)
	for _, expected := range []string{"legacy first", "legacy middle", "legacy latest"} {
		if !strings.Contains(string(visible), expected) {
			t.Fatalf("legacy result %q was lost: %s", expected, visible)
		}
	}
}

func TestProjectPayloadKeepsDifferentWorkCategoriesInOneSession(t *testing.T) {
	digest := string(validFrozenDigestV2())
	for _, workUnitRef := range []string{"wu-1", "wu-2", "wu-3"} {
		digest = strings.Replace(
			digest,
			`"work_unit_ref":"`+workUnitRef+`"`,
			`"source_ref":"session-1","work_unit_ref":"`+workUnitRef+`"`,
			1,
		)
	}
	second := strings.Index(digest, `"source_ref":"session-1","work_unit_ref":"wu-2"`)
	digest = digest[:second] + strings.Replace(
		digest[second:], `"text":"same result"`, `"text":"validation result"`, 1,
	)

	projected, err := projectPayload(Payload{
		Sessions: []SessionSource{{
			SelectionID: "selection-1", Mode: "digest_v2", Digest: json.RawMessage(digest),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	visible, _ := json.Marshal(projected.WorkEvidence)
	for _, expected := range []string{"same result", "validation result", "other result"} {
		if !strings.Contains(string(visible), expected) {
			t.Fatalf("independent category result %q was lost: %s", expected, visible)
		}
	}
}

func TestProjectPayloadRejectsUnknownHighlightSource(t *testing.T) {
	digest := strings.Replace(
		string(validFrozenDigestV2()),
		`"work_unit_ref":"wu-1"`,
		`"source_ref":"unknown-session","work_unit_ref":"wu-1"`,
		1,
	)
	_, err := projectPayload(Payload{
		Sessions: []SessionSource{{
			SelectionID: "selection-1", Mode: "digest_v2", Digest: json.RawMessage(digest),
		}},
	})
	if !errors.Is(err, ErrIncomplete) {
		t.Fatalf("unknown source_ref must fail projection, got %v", err)
	}
}

func TestProjectReportFactTextKeepsOutcomeLeadAndDropsProcessDetail(t *testing.T) {
	input := "已完成方案收口。测试已全部通过。\n\n## 实现细节\n\n" +
		"```go\nfunc internalOnly() {}\n```\n\n/api/internal/reportcontext/projection.go"
	if got, want := projectReportFactText(input), "已完成方案收口。测试已全部通过。"; got != want {
		t.Fatalf("projected fact=%q want=%q", got, want)
	}
}

func TestProjectReportFactTextKeepsOutcomeAndValidationListItems(t *testing.T) {
	input := "## 修改结果\n\n- 已修复报告超时并完成真实链路验证。\n- `go test ./...` 已通过。\n- 修改文件：`api/internal/reportcontext/projection.go`。"
	if got, want := projectReportFactText(input), "已修复报告超时并完成真实链路验证。\n`go test ./...` 已通过。"; got != want {
		t.Fatalf("projected fact=%q want=%q", got, want)
	}
}

func TestProjectReportFactTextKeepsMultipleOutcomeBlocksUpToLimit(t *testing.T) {
	input := "## 结果\n\n- 完成报告 Context 收敛。\n- 真实样本验证通过。\n- 仍需人工核对最终日报质量。\n\n## 实现细节\n\n`git status --short`\n\n/api/internal/reportcontext/projection.go"
	want := "完成报告 Context 收敛。\n真实样本验证通过。\n仍需人工核对最终日报质量。"
	if got := projectReportFactText(input); got != want {
		t.Fatalf("multiple report outcomes were not retained: got=%q want=%q", got, want)
	}
}

func TestProjectReportFactTextDoesNotTruncateLongUsefulProse(t *testing.T) {
	input := strings.Repeat("已完成可验证成果，", 150)
	got := projectReportFactText(input)
	if got != input {
		t.Fatalf("useful report prose must not be truncated: got runes=%d want=%d", len([]rune(got)), len([]rune(input)))
	}
}

func TestProjectReportFactTextKeepsFlattenedOutcomeList(t *testing.T) {
	input := "已完成 8 项问题修复，并同步到前后端规范与唯一评审稿。 主要改进： - 增加查询预算。 - 补齐状态机。 - 修复缓存规则。"
	want := "已完成 8 项问题修复，并同步到前后端规范与唯一评审稿。 主要改进：\n增加查询预算。\n补齐状态机。\n修复缓存规则。"
	if got := projectReportFactText(input); got != want {
		t.Fatalf("projected fact=%q want=%q", got, want)
	}
}

func TestProjectReportFactTextDropsGitItemFromFlattenedOutcomeList(t *testing.T) {
	input := "完成报告事实收敛。 - 修复纠正链丢失问题。 - 当前分支：fix/report-context。 - 运行代码：main@abc123。 - 未推送远端。 - 真实样本回放通过。"
	want := "完成报告事实收敛。\n修复纠正链丢失问题。\n真实样本回放通过。"
	if got := projectReportFactText(input); got != want {
		t.Fatalf("flattened Git item was not isolated: got=%q want=%q", got, want)
	}
}

func TestProjectReportFactTextDropsFlattenedCodeFence(t *testing.T) {
	input := "脚本语法检查通过，但缺少运行依赖，无法确认端到端训练成功；直接运行会报： ```text ModuleNotFoundError ``` 后续命令省略。"
	if got, want := projectReportFactText(input), "脚本语法检查通过，但缺少运行依赖，无法确认端到端训练成功；直接运行会报： ModuleNotFoundError 后续命令省略。"; got != want {
		t.Fatalf("projected fact=%q want=%q", got, want)
	}
}

func TestProjectReportFactTextDoesNotKeepGenericShortSentenceAlone(t *testing.T) {
	input := "已完成。日报投影现在只保留最终结果并排除过程命令和文件路径"
	if got, want := projectReportFactText(input), input; got != want {
		t.Fatalf("projected fact=%q want=%q", got, want)
	}
}

func TestProjectReportFactTextDropsPureGitOperation(t *testing.T) {
	input := "已执行 git add、git commit 和 merge，提交号为 abc123。"
	if got := projectReportFactText(input); got != "" {
		t.Fatalf("pure Git operation became report fact: %q", got)
	}
}

func TestProjectReportFactTextDropsPureWorktreeMerge(t *testing.T) {
	if got := projectReportFactText("已合入当前 worktree。"); got != "" {
		t.Fatalf("pure worktree merge became report fact: %q", got)
	}
}

func TestProjectReportFactTextKeepsValidationAndDropsGitClauses(t *testing.T) {
	input := "已完成分支 rebase 和 merge，前端测试及生产构建均通过。"
	if got, want := projectReportFactText(input), "前端测试及生产构建均通过。"; got != want {
		t.Fatalf("mixed Git validation projected as %q, want %q", got, want)
	}
}

func TestProjectReportFactTextKeepsOutcomeAndStripsGitTrace(t *testing.T) {
	for input, expected := range map[string]string{
		"已完成预审问题修复，并推送前后端功能分支。":                     "已完成预审问题修复。",
		"已完成 commit 和测试服部署。":                        "已完成测试服部署。",
		"已合入当前 worktree，当前 HEAD：`9ba749b`。":         "",
		"已记录到：`doc/v2/bug清单/严重问题.md` 提交：`d0f6193`。": "已记录到：`doc/v2/bug清单/严重问题.md`。",
	} {
		if got := projectReportFactText(input); got != expected {
			t.Fatalf("Git trace sanitization mismatch: input=%q got=%q want=%q", input, got, expected)
		}
	}
}

func TestProjectReportFactTextKeepsNonGitSubmissionBlocker(t *testing.T) {
	input := "当前提交不能进入人工测试，静态 Review 发现 2 个 P0 竞态和 1 个现有流程回归。"
	if got := projectReportFactText(input); got != input {
		t.Fatalf("non-Git blocker was lost: %q", got)
	}
}

func TestProjectReportFactTextStripsInternalDelegationAndKeepsConclusion(t *testing.T) {
	for input, expected := range map[string]string{
		"审查结论已发给主代理：前端旧技术文档可以删除。":  "前端旧技术文档可以删除。",
		"只读分析已完成，并已向主代理提交完整流水线结论。": "",
	} {
		if got := projectReportFactText(input); got != expected {
			t.Fatalf("internal delegation projected as %q, want %q", got, expected)
		}
	}
}

func TestProjectReportFactTextDropsInstructionAcknowledgement(t *testing.T) {
	input := "已完整阅读 AGENTS.md，后续任务我会严格遵守远程开发约定。"
	if got := projectReportFactText(input); got != "" {
		t.Fatalf("instruction acknowledgement became report fact: %q", got)
	}
}

func TestProjectReportFactTextDropsProcessNarration(t *testing.T) {
	for _, input := range []string{
		"我会先按领域建模方法只读拆解现有手册。",
		"我先只做文档职责和引用核对，不会直接删除或改名。",
		"你的担忧完全正确，而且这不是零散后端问题。",
		"只读分析结论如下，可直接用于后续文档。",
	} {
		if got := projectReportFactText(input); got != "" {
			t.Fatalf("process narration became report fact: input=%q got=%q", input, got)
		}
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

func projectSingleResultText(t *testing.T, input string) string {
	t.Helper()
	digest := strings.Replace(
		string(validFrozenDigestV2()), `"text":"same result"`,
		`"text":`+string(mustJSON(t, input)), 1,
	)
	projected, err := projectPayload(Payload{Sessions: []SessionSource{{
		SelectionID: "selection-1", Mode: reportsource.ReadModeDigestV2,
		Digest: json.RawMessage(digest),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if projected.WorkEvidence == nil {
		t.Fatal("work evidence missing")
	}
	texts := make([]string, 0, len(projected.WorkEvidence.Facts))
	for _, fact := range projected.WorkEvidence.Facts {
		texts = append(texts, fact.Text)
	}
	return strings.Join(texts, "\n")
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
