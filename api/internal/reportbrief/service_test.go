package reportbrief

import (
	"context"
	"encoding/json"
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

func TestNormalizeDraftAutomaticallyAccountsForUnselectedFacts(t *testing.T) {
	payload, err := normalizeDraft(Draft{
		Workstreams: []Workstream{{
			Subject: "AIDA 日报", Title: "日报生成质量优化",
			Deliverables: []Deliverable{{
				Result: "优化日报项目成果表达", FactRefs: []string{"fact-001"},
			}},
		}},
		ExcludedFacts: []ExcludedFact{{FactRef: "fact-003", Reason: "discussion"}},
	}, personalDaily, Period{Start: "2026-07-31", End: "2026-07-31"}, map[string]struct{}{
		"fact-001": {}, "fact-002": {}, "fact-003": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []ExcludedFact{
		{FactRef: "fact-002", Reason: "not_selected"},
		{FactRef: "fact-003", Reason: "discussion"},
	}
	if fmt.Sprint(payload.ExcludedFacts) != fmt.Sprint(want) {
		t.Fatalf("excluded facts = %#v, want %#v", payload.ExcludedFacts, want)
	}
}

func TestAcceptedBriefHidesAutomaticFactAccounting(t *testing.T) {
	accepted := (Stored{Payload: Payload{ExcludedFacts: []ExcludedFact{
		{FactRef: "fact-001", Reason: "not_selected"},
		{FactRef: "fact-002", Reason: "discussion"},
	}}}).Accepted()
	if len(accepted.ExcludedFacts) != 1 || accepted.ExcludedFacts[0].FactRef != "fact-002" {
		t.Fatalf("accepted Brief exposed automatic fact accounting: %#v", accepted.ExcludedFacts)
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

func TestNormalizeDraftRequiresExplicitExclusionsForNoReportableWork(t *testing.T) {
	_, err := normalizeDraft(
		Draft{NoReportableWork: true},
		personalDaily,
		Period{Start: "2026-07-27", End: "2026-07-27"},
		map[string]struct{}{"fact-001": {}},
	)
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "must be explicitly excluded when no_reportable_work is true") {
		t.Fatalf("error = %v, want explicit no-work exclusion", err)
	}
}

func TestNormalizeDraftAcceptsSecondaryActivityExclusion(t *testing.T) {
	payload, err := normalizeDraft(Draft{
		Workstreams: []Workstream{{
			Title: "核心协议设计", Objective: "完成核心交付方案",
			Deliverables: []Deliverable{{
				Result: "完成协议设计", State: "completed", Environment: "development",
				Validation: "设计内容已完成复核", NextAction: "进入实现验证", FactRefs: []string{"fact-001"},
			}},
		}},
		ExcludedFacts: []ExcludedFact{{FactRef: "fact-002", Reason: "secondary_activity"}},
	}, personalDaily, Period{Start: "2026-07-29", End: "2026-07-29"}, map[string]struct{}{
		"fact-001": {}, "fact-002": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := payload.ExcludedFacts[0].Reason; got != "secondary_activity" {
		t.Fatalf("exclusion reason = %q, want secondary_activity", got)
	}
}

func TestNormalizeDraftRejectsUnknownExclusionReason(t *testing.T) {
	_, err := normalizeDraft(Draft{
		NoReportableWork: true,
		ExcludedFacts:    []ExcludedFact{{FactRef: "fact-001", Reason: "not_important"}},
	}, personalDaily, Period{Start: "2026-07-29", End: "2026-07-29"}, map[string]struct{}{"fact-001": {}})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "reason is invalid") {
		t.Fatalf("error = %v, want invalid exclusion reason", err)
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

func TestNormalizeDraftMergesSharedSubjectAcrossDifferentHeadings(t *testing.T) {
	payload, err := normalizeDraft(Draft{Workstreams: []Workstream{
		{Subject: " StaffDeck ", Title: "StaffDeck 部署", Objective: "提供可用服务", Deliverables: []Deliverable{{
			Result: "完成服务部署", State: "completed", Environment: "development",
			Validation: "服务健康检查通过", NextAction: "持续观察", FactRefs: []string{"fact-001"},
		}}},
		{Subject: "staffdeck", Title: "StaffDeck walkthrough", Objective: "完成代码走读", Deliverables: []Deliverable{{
			Result: "完成代码走读文档", State: "validated", Environment: "test",
			Validation: "文档示例验证通过", NextAction: "无", FactRefs: []string{"fact-002"},
		}}},
	}}, personalDaily, Period{Start: "2026-07-28", End: "2026-07-28"}, map[string]struct{}{
		"fact-001": {}, "fact-002": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Workstreams) != 1 || len(payload.Workstreams[0].Deliverables) != 2 || payload.Workstreams[0].Subject != "StaffDeck" {
		t.Fatalf("workstreams = %#v, want one subject-merged workstream", payload.Workstreams)
	}
}

func TestNormalizeDraftKeepsDistinctBusinessSubjects(t *testing.T) {
	payload, err := normalizeDraft(Draft{Workstreams: []Workstream{
		{Subject: "baigong 任务分发协议", Title: "协议设计", Objective: "明确协议边界", Deliverables: []Deliverable{{
			Result: "完成协议设计", State: "completed", Environment: "development",
			Validation: "方案完成复核", NextAction: "进入实现", FactRefs: []string{"fact-001"},
		}}},
		{Subject: "baigong 交互原型", Title: "原型验证", Objective: "验证交互方案", Deliverables: []Deliverable{{
			Result: "完成原型验证", State: "validated", Environment: "test",
			Validation: "测试环境验证通过", NextAction: "收集反馈", FactRefs: []string{"fact-002"},
		}}},
	}}, personalDaily, Period{Start: "2026-07-30", End: "2026-07-30"}, map[string]struct{}{
		"fact-001": {}, "fact-002": {},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(payload.Workstreams) != 2 {
		t.Fatalf("workstreams = %#v, want distinct business subjects", payload.Workstreams)
	}
}

func TestNormalizeDraftRejectsPartiallyMissingSubjects(t *testing.T) {
	_, err := normalizeDraft(Draft{Workstreams: []Workstream{
		{Subject: "报告生成", Title: "生成优化", Objective: "提高质量", Deliverables: []Deliverable{{
			Result: "完成生成优化", State: "completed", Environment: "development",
			Validation: "测试通过", NextAction: "继续观察", FactRefs: []string{"fact-001"},
		}}},
		{Title: "页面优化", Objective: "改善体验", Deliverables: []Deliverable{{
			Result: "完成页面优化", State: "completed", Environment: "development",
			Validation: "测试通过", NextAction: "继续观察", FactRefs: []string{"fact-002"},
		}}},
	}}, personalDaily, Period{Start: "2026-07-30", End: "2026-07-30"}, map[string]struct{}{
		"fact-001": {}, "fact-002": {},
	})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "subject must be provided for every workstream") {
		t.Fatalf("error = %v, want partial subject rejection", err)
	}
}

func TestNormalizeDraftSubjectContractKeepsOnlyProjectOutcomes(t *testing.T) {
	draft := Draft{Workstreams: []Workstream{{
		Subject: "AIDA 日报", Title: "AI 日报生成质量优化", Objective: "旧字段不应进入新契约",
		Deliverables: []Deliverable{{
			Result: "优化日报工作主线关联，减少同一项目被拆分为多个事项",
			State:  "blocked", Environment: "production",
			Validation: "代码尚未合并", NextAction: "暂不建议上线",
			FactRefs: []string{"fact-001"},
		}},
	}}}
	payload, err := normalizeDraft(
		draft,
		personalDaily,
		Period{Start: "2026-07-30", End: "2026-07-30"},
		map[string]struct{}{"fact-001": {}},
	)
	if err != nil {
		t.Fatal(err)
	}
	workstream := payload.Workstreams[0]
	deliverable := workstream.Deliverables[0]
	if workstream.Objective != "" || deliverable.State != "" || deliverable.Environment != "" ||
		deliverable.Validation != "" || deliverable.NextAction != "" {
		t.Fatalf("project-outcome subject contract retained audit fields: %#v", workstream)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"objective", "state", "environment", "validation", "next_action", "暂不建议上线", "代码尚未合并"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("normalized project-outcome brief exposed %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(string(encoded), "减少同一项目被拆分为多个事项") {
		t.Fatalf("normalized project-outcome brief lost project outcome: %s", encoded)
	}
}

func TestNormalizeDraftSubjectContractDoesNotRejectWordingAsAQualityFailure(t *testing.T) {
	for _, subject := range []string{"AIDA 日报", "系统代码库", "deployment"} {
		t.Run(subject, func(t *testing.T) {
			payload, err := normalizeDraft(Draft{Workstreams: []Workstream{{
				Subject: subject,
				Title:   "日报工作主线优化",
				Deliverables: []Deliverable{{
					Result:   "完成日报工作主线关联优化",
					FactRefs: []string{"fact-001"},
				}},
			}}}, personalDaily, Period{Start: "2026-07-30", End: "2026-07-30"}, map[string]struct{}{"fact-001": {}})
			if err != nil || len(payload.Workstreams) != 1 {
				t.Fatalf("structurally valid Brief became a generation failure: payload=%#v err=%v", payload, err)
			}
		})
	}
}

func TestNormalizeDraftSubjectContractRejectsOperationalTraceAndAgentJudgement(t *testing.T) {
	for _, result := range []string{
		"工程上达到可合并标准，相关测试与检查全部通过",
		"代码尚未合并，暂不建议发布生产",
		"记录 Run ID 并完成 deployment validation",
		"评测认可该方案，聚合结论为 evidence_insufficient",
		"新增评测能力；不改动日报正文逻辑与默认模型",
		"完成工作单元关联能力，且不改动简报契约",
		"评测体系仅涉及评测能力，不改变日报内容与交互",
		"完成工作单元关联，原有简报契约保持不变",
	} {
		t.Run(result, func(t *testing.T) {
			_, err := normalizeDraft(Draft{Workstreams: []Workstream{{
				Subject: "AIDA 日报", Title: "日报生成质量优化",
				Deliverables: []Deliverable{{Result: result, FactRefs: []string{"fact-001"}}},
			}}}, personalDaily, Period{Start: "2026-07-31", End: "2026-07-31"}, map[string]struct{}{"fact-001": {}})
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "operational trace or Agent judgement") {
				t.Fatalf("result %q error=%v, want project-outcome rejection", result, err)
			}
		})
	}

	payload, err := normalizeDraft(Draft{Workstreams: []Workstream{{
		Subject: "AIDA 日报", Title: "日报生成质量优化",
		Deliverables: []Deliverable{{
			Result:   "整理生产环境手写日报数据集，不以单一样本作为验收标准",
			FactRefs: []string{"fact-001"},
		}},
	}}}, personalDaily, Period{Start: "2026-07-31", End: "2026-07-31"}, map[string]struct{}{"fact-001": {}})
	if err != nil || len(payload.Workstreams) != 1 {
		t.Fatalf("project outcome rejected: payload=%#v err=%v", payload, err)
	}
}

func TestNormalizeDraftSubjectContractAllowsTruthfulFactExclusion(t *testing.T) {
	for _, reason := range []string{"discussion", "duplicate", "trace", "low_reader_value"} {
		t.Run(reason, func(t *testing.T) {
			payload, err := normalizeDraft(Draft{
				Workstreams: []Workstream{{
					Subject: "AIDA 日报", Title: "日报生成优化",
					Deliverables: []Deliverable{{
						Result:   "优化日报项目成果表达",
						FactRefs: []string{"fact-001"},
					}},
				}},
				ExcludedFacts: []ExcludedFact{{FactRef: "fact-002", Reason: reason}},
			}, personalDaily, Period{Start: "2026-07-30", End: "2026-07-30"}, map[string]struct{}{
				"fact-001": {}, "fact-002": {},
			})
			if err != nil || len(payload.ExcludedFacts) != 1 {
				t.Fatalf("truthful exclusion %q rejected: payload=%#v err=%v", reason, payload, err)
			}
		})
	}
}

func TestNormalizeDraftRejectsCallerProvidedAutomaticExclusionReason(t *testing.T) {
	_, err := normalizeDraft(Draft{
		Workstreams: []Workstream{{
			Subject: "AIDA 日报", Title: "日报生成优化",
			Deliverables: []Deliverable{{
				Result:   "优化日报项目成果表达",
				FactRefs: []string{"fact-001"},
			}},
		}},
		ExcludedFacts: []ExcludedFact{{FactRef: "fact-002", Reason: automaticExclusionReason}},
	}, personalDaily, Period{Start: "2026-07-31", End: "2026-07-31"}, map[string]struct{}{
		"fact-001": {}, "fact-002": {},
	})
	if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "excluded fact reason is invalid") {
		t.Fatalf("caller-provided automatic exclusion reason accepted: %v", err)
	}
}

func TestValidateTextIssuesRejectsInternalDetails(t *testing.T) {
	for _, text := range []string{
		"后端返回 REPORT_SOURCE_UNAVAILABLE",
		"镜像标签 20260727-a66ffdb-report-actions",
		"开放 UDP 123 并运行 systemd --user 服务",
		"开放 3001 端口供局域网访问",
		"已部署至 16.36 提供局域网访问",
		"目标主机 16.74 已完成部署",
		"在 16.36 环境完成 StaffDeck 部署",
		"使用独立 Docker 网段 174.69.23.0/24",
		"模型密钥将在 2026-07-31 到期",
		"部署默认密码为 admin",
		"部署默认密码 admin123",
		"部署默认密码 `admin123`",
		"部署默认密码 **admin123**",
	} {
		if len(validateTextIssues("content", text, 0, MaxPayloadBytes)) == 0 {
			t.Fatalf("text %q should be rejected", text)
		}
	}
	if issues := validateTextIssues("content", "完成错误提示、通知跳转和操作布局优化，并在生产环境验证", 0, MaxPayloadBytes); len(issues) != 0 {
		t.Fatalf("reader-facing content rejected: %v", issues)
	}
	if issues := validateTextIssues("content", "建议登录后修改默认管理员密码", 0, MaxPayloadBytes); len(issues) != 0 {
		t.Fatalf("credential hardening advice rejected: %v", issues)
	}
	if issues := validateTextIssues("content", "完成 Plannotator 0.25.0 安装，并将版本升级至 1.2", 0, MaxPayloadBytes); len(issues) != 0 {
		t.Fatalf("software versions were mistaken for internal hosts: %v", issues)
	}
}

func TestValidateTextIssuesKeepsCredentialAndUnrelatedExpiryInSeparateClauses(t *testing.T) {
	text := "完成模型密钥轮换；测试证书将在 2026-08-01 到期"
	if issues := validateTextIssues("content", text, 0, MaxPayloadBytes); len(issues) != 0 {
		t.Fatalf("separate credential and expiry clauses were conflated: %v", issues)
	}
}

func TestValidateResultBriefIssuesKeepsNoWorkStateConsistent(t *testing.T) {
	noWork := Payload{NoReportableWork: true}
	if issues := validateResultBriefIssues(
		noWork,
		noReportableResultText,
		"## 工作概览\n\n本期无可核验的工作记录\n\n## 工作详情\n\n本期无可核验的工作记录",
	); len(issues) != 0 {
		t.Fatalf("valid no-work result rejected: %v", issues)
	}
	if issues := validateResultBriefIssues(noWork, "完成一项工作", "完成一项工作"); len(issues) != 2 {
		t.Fatalf("no-work brief accepted contradictory result: %v", issues)
	}

	withWork := Payload{Workstreams: []Workstream{{Title: "日报优化"}}}
	if issues := validateResultBriefIssues(withWork, "1. 完成日报优化", "### 日报优化\n\n完成实现"); len(issues) != 0 {
		t.Fatalf("normal report result rejected: %v", issues)
	}
	if issues := validateResultBriefIssues(withWork, noReportableResultText, noReportableResultText); len(issues) != 1 {
		t.Fatalf("non-empty brief accepted no-work claim: %v", issues)
	}

	twoWorkstreams := Payload{Workstreams: []Workstream{
		{Subject: "任务分发协议", Title: "协议"},
		{Subject: "任务协作原型", Title: "原型"},
	}}
	if issues := validateResultBriefIssues(
		twoWorkstreams,
		"1. 完成协议设计\n2. 完成原型验证",
		"### 协议\n\n完成设计\n\n### 原型\n\n完成验证",
	); len(issues) != 0 {
		t.Fatalf("aligned report result rejected: %v", issues)
	}
	outcomeOnlyBrief := Payload{Workstreams: []Workstream{{
		Subject: "AIDA 日报", Title: "日报生成质量优化",
		Deliverables: []Deliverable{{Result: "优化日报工作主线关联"}},
	}}}
	if issues := validateResultBriefIssues(
		outcomeOnlyBrief,
		"1. 优化日报工作主线关联",
		"### 日报生成质量优化\n\n优化日报工作主线关联，减少同一项目被拆分为多个事项。",
	); len(issues) != 0 {
		t.Fatalf("outcome-only result rejected: %v", issues)
	}
	if issues := validateResultBriefIssues(
		twoWorkstreams,
		"1. 完成协议设计",
		"### 协议\n\n完成设计\n\n### 原型\n\n完成验证\n\n### 额外主题\n\n不应拆分",
	); len(issues) != 2 {
		t.Fatalf("misaligned report result accepted: %v", issues)
	}
	longContent := "### 协议\n\n" + strings.Repeat("完成协议机制说明。", 90) + "\n\n### 原型\n\n" + strings.Repeat("完成原型机制说明。", 90)
	issues := validateResultBriefIssues(
		twoWorkstreams,
		"1. 完成协议设计\n2. 完成原型验证",
		longContent,
	)
	if len(issues) != 1 || !strings.Contains(issues[0], "content length must not exceed 1200 characters") {
		t.Fatalf("oversized subject-contract result accepted: %v", issues)
	}
	legacyBrief := Payload{Workstreams: []Workstream{{Title: "协议"}, {Title: "原型"}}}
	if issues := validateResultBriefIssues(
		legacyBrief,
		"1. 完成协议设计",
		"### 协议\n\n完成设计",
	); len(issues) != 0 {
		t.Fatalf("legacy subject-less brief must retain pre-contract validation: %v", issues)
	}
	if issues := validateResultBriefIssues(
		legacyBrief,
		"1. 完成协议设计",
		longContent,
	); len(issues) != 0 {
		t.Fatalf("legacy subject-less result must retain pre-contract length behavior: %v", issues)
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
		"result contains network port",
		"validation contains command-line flag",
		"released requires production environment",
		"unknown fact_ref fact-999",
	}
	for _, want := range wantParts {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestReaderFacingTextSafeAllowsRouteWithoutInternalLocation(t *testing.T) {
	if !ReaderFacingTextSafe("完成 /chat 页面验证") {
		t.Fatal("reader-facing page route must not trigger a hard quality failure")
	}
	for _, value := range []string{
		"访问 http://192.168.16.74/chat",
		"配置保存在 /home/aied/project/.env",
		"服务监听 UDP 3001 端口",
		"调用 /api/v1/internal/report",
		"检查 /admin/daily-report-value 页面",
		"调用 /api?scope=self",
		"查看 /admin#users",
		"检查 /metrics。",
		"检查 /api.",
		"查看 /admin,",
		"检查 /metrics;",
	} {
		if ReaderFacingTextSafe(value) {
			t.Fatalf("sensitive location remains reader-facing safe: %q", value)
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

func TestReaderFacingTechnologyNamesDoNotBlockBrief(t *testing.T) {
	issues := validateTextIssues(
		"workstreams[4].deliverables[0].validation",
		"TypeScript和ESLint通过，测试服验证",
		1,
		320,
	)
	if len(issues) != 0 {
		t.Fatalf("reader-facing technology names must be soft quality concerns, got %v", issues)
	}
}

func TestDegradedWriteReasonRequiresExhaustedQualityRetry(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := &Service{db: db}
	const runID = "00000000-0000-4000-8000-000000000001"

	mock.ExpectQuery("SELECT a.brief_invalid_attempts").
		WithArgs(runID, "198").
		WillReturnRows(sqlmock.NewRows([]string{"brief_invalid_attempts", "result_invalid_attempts"}).
			AddRow(MaxBriefInvalidAttempts+1, 0))
	reason, err := service.DegradedWriteReason(context.Background(), "198", runID)
	if err != nil || reason != "brief_retry_exhausted" {
		t.Fatalf("reason=%q err=%v", reason, err)
	}

	mock.ExpectQuery("SELECT a.brief_invalid_attempts").
		WithArgs(runID, "198").
		WillReturnRows(sqlmock.NewRows([]string{"brief_invalid_attempts", "result_invalid_attempts"}).
			AddRow(1, MaxResultInvalidAttempts+1))
	reason, err = service.DegradedWriteReason(context.Background(), "198", runID)
	if err != nil || reason != "result_retry_exhausted" {
		t.Fatalf("reason=%q err=%v", reason, err)
	}

	mock.ExpectQuery("SELECT a.brief_invalid_attempts").
		WithArgs(runID, "198").
		WillReturnRows(sqlmock.NewRows([]string{"brief_invalid_attempts", "result_invalid_attempts"}).
			AddRow(MaxBriefInvalidAttempts, MaxResultInvalidAttempts))
	if _, err := service.DegradedWriteReason(context.Background(), "198", runID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("non-exhausted run error=%v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestReaderFacingTextSafetyRetainsTechnologyAndRejectsLocations(t *testing.T) {
	if !ReaderFacingTextSafe("TypeScript和ESLint通过，测试服验证") {
		t.Fatal("public technology names must remain usable")
	}
	if ReaderFacingTextSafe("访问 http://192.168.14.182:9180 完成验证") {
		t.Fatal("internal locations must require the generic degraded fallback")
	}
}
