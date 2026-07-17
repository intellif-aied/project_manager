package sessiondigestv2

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestDailySummariesPreserveLaterPeriodBeforeRevisionCompaction(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	digest := EmptyDigest()
	for index := 0; index < 80; index++ {
		day := 15
		if index >= 40 {
			day = 16
		}
		end := time.Date(2026, 7, day, 9, index%60, 0, 0, location)
		unit := WorkUnit{
			WorkUnitRef:     "wu-" + strconvItoa(index),
			Sequence:        index + 1,
			ActivityStartAt: end.Add(-time.Minute).UTC().Format(time.RFC3339),
			ActivityEndAt:   end.UTC().Format(time.RFC3339),
			PeriodRelation:  "unknown",
			Goal: Goal{
				Text:   strings.Repeat("实现报告期结果保留", 30),
				Source: "user_message",
			},
			Category:      "implementation",
			Status:        "completed",
			EvidenceGrade: "B",
			ResultStatements: []ResultStatement{{
				Text:         "完成第 " + strconvItoa(index) + " 项结果",
				Source:       "agent_claim_with_evidence",
				EvidenceRefs: []string{"ev-file-" + strconvItoa(index)},
			}},
			AgentClaims: []AgentClaim{},
			Evidence:    []Evidence{},
			Changes: []Change{{
				Path: "file-" + strconvItoa(index) + ".go",
			}},
			Validations: []Validation{},
			Unresolved:  []Unresolved{},
		}
		digest.WorkUnits = append(digest.WorkUnits, unit)
	}
	digest.DailySummaries = BuildDailySummaries(
		digest.WorkUnits, location, 0,
	)
	digest.Coverage = Coverage{
		SourceWorkUnitCount:   len(digest.WorkUnits),
		DetailedWorkUnitCount: len(digest.WorkUnits),
		Representation:        "result_focused",
	}
	recalculateSummary(&digest)

	revision, encoded, truncated := EnforceItemBudget(digest, 20<<10)
	if !truncated || !json.Valid(encoded) {
		t.Fatalf("revision compaction failed: truncated=%v bytes=%d", truncated, len(encoded))
	}
	if len(revision.DailySummaries) != 2 {
		t.Fatalf("daily summaries were lost: %#v", revision.DailySummaries)
	}
	for _, day := range revision.DailySummaries {
		if !day.OutcomeCoverage.Complete || day.HighlightsTruncated ||
			day.OutcomeCoverage.SourceCount != 40 || len(day.Highlights) != 40 {
			t.Fatalf("daily result coverage was reduced: %#v", day.OutcomeCoverage)
		}
	}

	period := time.Date(2026, 7, 16, 0, 0, 0, 0, location)
	selected, selectedJSON, _ := PrepareForPeriod(
		revision, period, period, location, DefaultPeriodItemBytes,
	)
	if !json.Valid(selectedJSON) {
		t.Fatalf("period payload is invalid: %d", len(selectedJSON))
	}
	if selected.ReportPeriodSummary == nil ||
		len(selected.ReportPeriodSummary.Days) != 1 ||
		selected.ReportPeriodSummary.Days[0].Date != "2026-07-16" {
		t.Fatalf("wrong period summary: %#v", selected.ReportPeriodSummary)
	}
	highlights := selected.ReportPeriodSummary.Days[0].Highlights
	if len(highlights) != 40 || highlights[0].Sequence != 41 ||
		!selected.ReportPeriodSummary.Days[0].OutcomeCoverage.Complete {
		t.Fatalf("report-period results were not preserved: %#v", highlights)
	}
	for _, day := range selected.ReportPeriodSummary.Days {
		if day.Date == "2026-07-15" {
			t.Fatalf("out-of-period day leaked into report summary: %#v", day)
		}
	}
}

func TestResultFocusedClaimDropsNextQuestionTail(t *testing.T) {
	got := resultFocusedClaim(
		"已完成展示名缓存方案并更新 ADR。 **问题 46：是否继续增加控制能力？**",
	)
	if got != "已完成展示名缓存方案并更新 ADR。" {
		t.Fatalf("unexpected focused claim: %q", got)
	}
	if got := resultFocusedClaim("**Q37：是否默认选择平台能力？**"); got != "" {
		t.Fatalf("question-only claim must be removed: %q", got)
	}
}

func TestReportFacingClaimRemovesEngineeringProcess(t *testing.T) {
	unit := WorkUnit{
		Category: "deployment",
		Goal:     Goal{Text: "发布 Aida CLI"},
	}
	got := reportFacingClaim(unit, `相关 CLI 代码已单独提交：
`+"```text\n9fc940c feat: release aida cli 0.1.11\n```"+`
发布说明已写入 /home/intellif/dev/project_manager/doc/release.md。
已验证：
- go test -count=1 ./...
- go vet ./...
- 全部 SHA256 校验
- 版本为 0.1.11
- 客户端自动升级已生效。`)
	if got != "Aida CLI 0.1.11 已完成发布，客户端自动升级已生效。" {
		t.Fatalf("unexpected report-facing release: %q", got)
	}
	for _, forbidden := range []string{
		"go test", "go vet", "SHA256", "release.md", "9fc940c",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("report-facing claim leaked %q: %q", forbidden, got)
		}
	}
}

func TestReportFacingClaimKeepsCapabilitiesWithoutInternalFields(t *testing.T) {
	unit := WorkUnit{
		Category: "implementation",
		Goal:     Goal{Text: "恢复 Session 列表目录列"},
	}
	got := reportFacingClaim(unit, `已恢复目录列，显示规则：
- 优先显示 Session 的 Cwd 工作目录。
- 没有 Cwd 时显示 ProjectDir。
- 长路径中间省略，保留首尾便于区分。
时间确实是最后活动时间：
- 优先取 EndedAt；
- 没有则取 StartedAt。
测试包已升至 0.1.7。`)
	for _, expected := range []string{
		"已恢复目录列", "长路径中间省略", "最后活动时间",
	} {
		if !strings.Contains(got, expected) {
			t.Fatalf("report-facing claim missing %q: %q", expected, got)
		}
	}
	for _, forbidden := range []string{
		"Cwd", "ProjectDir", "EndedAt", "StartedAt", "测试包", "0.1.7",
	} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("report-facing claim leaked %q: %q", forbidden, got)
		}
	}
}

func TestReportFacingClaimDropsDocumentInventory(t *testing.T) {
	unit := WorkUnit{
		Category: "document",
		Goal:     Goal{Text: "整理方案文档"},
	}
	got := reportFacingClaim(unit, `已统一整理到：
[README.md](/home/intellif/dev/project_manager/doc/v2/README.md)
- 产品需求.md
- 开发方案.md
- 测试与验收方案.md`)
	if got != "" {
		t.Fatalf("document inventory must not become a daily outcome: %q", got)
	}
}

func TestReportFacingClaimDropsDocumentCountInventory(t *testing.T) {
	unit := WorkUnit{
		Category: "implementation",
		Goal:     Goal{Text: "开始吧。"},
	}
	got := reportFacingClaim(unit,
		"方案文档已完成并同步到：Session Digest 第二阶段结果质量优化，"+
			"本次共新增 10 份文档、约 2,411 行。")
	if got != "" {
		t.Fatalf("document counts must not become a daily outcome: %q", got)
	}
}

func TestReportFacingClaimDropsInternalArchitectureInventory(t *testing.T) {
	unit := WorkUnit{
		Category: "implementation",
		Goal:     Goal{Text: "完成 Aida Report 发布"},
	}
	got := reportFacingClaim(unit,
		"Digest v2 的 Work Unit、结果证据链和完成状态模型；"+
			"Digest、MCP、Report Skill、数据库兼容及开发方案；"+
			"全量 Session manifest、全量结构回放、分层 A/B、人工 Gold 和 holdout。")
	if got != "" {
		t.Fatalf("internal architecture inventory must not become an outcome: %q", got)
	}
}

func TestReportFacingClaimSummarizesFullFrontendRelease(t *testing.T) {
	unit := WorkUnit{
		Category: "implementation",
		Goal:     Goal{Text: "确保前端完整发布"},
	}
	got := reportFacingClaim(unit,
		"确认，这次发布的是完整最新前端代码，不是只复制修复文件。")
	if got != "最新前端代码已完整发布。" {
		t.Fatalf("unexpected frontend release projection: %q", got)
	}
}

func TestReportFacingClaimDropsReleaseOrchestrationProcess(t *testing.T) {
	unit := WorkUnit{
		Category: "deployment",
		Goal:     Goal{Text: "尽快上线"},
	}
	got := reportFacingClaim(unit,
		"以当前最新完整代码发布，不再筛 commit；API、Web、CLI、Skill、"+
			"Digest、时区、上传及 fork 修复统一发布。")
	if got != "" {
		t.Fatalf("release orchestration must not become an outcome: %q", got)
	}
}

func TestReportFacingClaimDropsRolloutChecklist(t *testing.T) {
	unit := WorkUnit{
		Category: "deployment",
		Goal:     Goal{Text: "尽快上线"},
	}
	got := reportFacingClaim(unit,
		"明确禁止 canary、shadow 和账号灰度；完整上线顺序、配置、"+
			"冒烟及回退步骤；排除 doc/v1/db-backups/。")
	if got != "" {
		t.Fatalf("rollout checklist must not become an outcome: %q", got)
	}
}

func TestReportFacingClaimDropsIncompleteDeploymentTail(t *testing.T) {
	unit := WorkUnit{
		Category: "deployment",
		Status:   "partial",
		Goal:     Goal{Text: "尽快上线"},
	}
	got := reportFacingClaim(unit, "当前唯一待处理项：把尚未上线的 026。")
	if got != "" {
		t.Fatalf("incomplete deployment tail must not become an outcome: %q", got)
	}
}

func TestReportFacingClaimSummarizesClipboardCompatibilityDefect(t *testing.T) {
	unit := WorkUnit{
		Category: "implementation",
		Status:   "partial",
		Goal:     Goal{Text: "修复复制按钮"},
	}
	got := reportFacingClaim(unit,
		"这是兼容分支实现缺陷，不是单纯的浏览器权限问题；HTTP/IP 环境没有 "+
			"Clipboard API，只能使用兼容复制；原实现未检查 execCommand(\"copy\") "+
			"结果，失败时仍提示“已复制”。")
	want := "HTTP/IP 环境下复制按钮仍可能误报成功，需补充兼容复制处理。"
	if got != want {
		t.Fatalf("unexpected clipboard defect projection: got=%q want=%q", got, want)
	}
}

func TestReportFacingGoalUsesOutcomeForVagueContinuation(t *testing.T) {
	unit := WorkUnit{
		Category: "deployment",
		Goal:     Goal{Text: "开始吧。"},
	}
	results := []ResultStatement{{Text: "Aida Report 1.0.10 已完成发布。"}}
	got := reportFacingGoal(unit, results)
	if got != "Aida Report 1.0.10 已完成发布" {
		t.Fatalf("vague goal was not replaced by the outcome: %q", got)
	}
}

func TestReportFacingClaimSplitsInlineBullets(t *testing.T) {
	unit := WorkUnit{
		Category: "implementation",
		Goal:     Goal{Text: "改进 Session 选择交互"},
	}
	got := reportFacingClaim(unit,
		"已完成： - 仅替换真实 TTY 下的 aida upload。"+
			" - 支持搜索、多选、筛选结果全选、取消。"+
			" - go test ./... 通过。")
	if !strings.Contains(got, "支持搜索、多选、筛选结果全选、取消") {
		t.Fatalf("material inline bullet missing: %q", got)
	}
	for _, forbidden := range []string{"TTY", "aida upload", "go test"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("inline process detail leaked %q: %q", forbidden, got)
		}
	}
}

func TestReportFacingClaimDoesNotTurnRuntimeVersionIntoRelease(t *testing.T) {
	unit := WorkUnit{
		Category: "implementation",
		Goal:     Goal{Text: "真实运行六类默认 Agent"},
	}
	got := reportFacingClaim(unit,
		"生产六类默认 Agent 已真实执行，运行时使用 10086/aida-report@1.0.3。")
	if strings.Contains(got, "已完成发布") {
		t.Fatalf("runtime dependency was misreported as release: %q", got)
	}
	if got != "生产六类默认 Agent 已真实执行。" {
		t.Fatalf("unexpected runtime-version projection: %q", got)
	}
}

func TestReportFacingClaimDoesNotRenameUnrelatedCLI(t *testing.T) {
	unit := WorkUnit{
		Category: "deployment",
		Goal:     Goal{Text: "发布内部运维 CLI"},
	}
	got := reportFacingClaim(unit, "内部运维 CLI 2.3.4 已完成发布。")
	if strings.Contains(got, "Aida CLI") {
		t.Fatalf("unrelated CLI was renamed as Aida CLI: %q", got)
	}
	if got != "内部运维 CLI 2.3.4 已完成发布。" {
		t.Fatalf("unexpected unrelated CLI projection: %q", got)
	}
}

func TestResolveUnitMarksOngoingImplementationPartial(t *testing.T) {
	unit := WorkUnit{
		Category:      "implementation",
		Status:        "pending",
		EvidenceGrade: "D",
		AgentClaims: []AgentClaim{{
			Text: "前端实现已写入，但 Go 容器仍在下载依赖，尚未进入测试执行。",
		}},
		Changes:     []Change{{Path: "frontend/app.tsx", Operation: "update"}},
		Validations: []Validation{},
		Unresolved:  []Unresolved{},
	}
	resolveUnit(&unit)
	if unit.Status != "partial" {
		t.Fatalf("ongoing implementation status=%q want=partial", unit.Status)
	}
}

func TestDeliveredOutcomeOutranksProcessOnlyInvestigation(t *testing.T) {
	delivered := WorkUnit{
		Category:      "verification",
		Status:        "completed",
		EvidenceGrade: "A",
		Goal:          Goal{Text: "走完整真实流程"},
		ResultStatements: []ResultStatement{{
			Text:   "已完成 Session Digest 开发和部署。",
			Source: "agent_claim_with_evidence",
		}},
	}
	investigation := WorkUnit{
		Category:      "investigation",
		Status:        "completed",
		EvidenceGrade: "A",
		Goal:          Goal{Text: "调研可借鉴方案"},
		ResultStatements: []ResultStatement{{
			Text:   "确认该方案可以借鉴。",
			Source: "agent_claim_with_evidence",
		}},
	}
	if dailyWorkUnitPriority(delivered) <= dailyWorkUnitPriority(investigation) {
		t.Fatalf(
			"delivered outcome priority=%d investigation=%d",
			dailyWorkUnitPriority(delivered),
			dailyWorkUnitPriority(investigation),
		)
	}
}

func TestReportFacingWorkUnitDropsProcessOnlyVerification(t *testing.T) {
	processOnly := WorkUnit{
		Category: "verification",
		Goal:     Goal{Text: "完成全流程测试"},
		ResultStatements: []ResultStatement{{
			Text: "已完成个人、小组、部门日报和周报的全流程测试。",
		}},
	}
	if hasReportFacingWorkUnit(processOnly) {
		t.Fatalf("process-only verification became a report outcome: %#v", processOnly)
	}

	delivered := WorkUnit{
		Category: "verification",
		Goal:     Goal{Text: "走完整真实流程"},
		ResultStatements: []ResultStatement{{
			Text: "已完成 Session Digest 开发和测试环境部署。",
		}},
	}
	if !hasReportFacingWorkUnit(delivered) {
		t.Fatalf("delivered result was hidden by verification category: %#v", delivered)
	}
}

func TestReportFacingClaimRemovesBuildProofTail(t *testing.T) {
	unit := WorkUnit{
		Category: "deployment",
		Goal:     Goal{Text: "发布最新前端"},
	}
	got := reportFacingClaim(unit,
		"本次发布使用完整最新前端代码，构建基线已包含远端最新提交。")
	if got != "最新前端代码已完整发布。" {
		t.Fatalf("unexpected build-proof projection: %q", got)
	}
}

func TestReportFacingClaimRemovesStandaloneBuildProofSegment(t *testing.T) {
	unit := WorkUnit{
		Category: "deployment",
		Goal:     Goal{Text: "发布最新前端"},
	}
	got := reportFacingClaim(unit,
		"本次发布使用完整最新前端代码；构建基线已包含远端最新提交。")
	if got != "最新前端代码已完整发布。" {
		t.Fatalf("unexpected standalone build-proof projection: %q", got)
	}
}

func TestDailySummaryHidesEngineeringEvidenceFromReportView(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	unit := WorkUnit{
		WorkUnitRef:     "wu-1",
		Sequence:        1,
		ActivityStartAt: "2026-07-16T01:00:00Z",
		ActivityEndAt:   "2026-07-16T02:00:00Z",
		Goal:            Goal{Text: "完成日报质量优化"},
		Category:        "implementation",
		Status:          "completed",
		EvidenceGrade:   "A",
		ResultStatements: []ResultStatement{{
			Text: "日报质量优化已完成", Source: "agent_claim_with_evidence",
		}},
		Changes:     []Change{{Path: "api/internal/example.go"}},
		Validations: []Validation{{Name: "go test", Attempts: 12, LastStatus: "passed"}},
	}
	digest := EmptyDigest()
	digest.WorkUnits = []WorkUnit{unit}
	digest.DailySummaries = BuildDailySummaries(digest.WorkUnits, location, 6)
	recalculateSummary(&digest)
	period := time.Date(2026, 7, 16, 0, 0, 0, 0, location)
	_, encoded, _ := PrepareForPeriod(digest, period, period, location, DefaultPeriodItemBytes)
	text := string(encoded)
	for _, forbidden := range []string{
		"change_count", "changed_files", "validation_count", "validations",
		`"work_unit_count":`, `"status_counts":`, "go test", "12",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("report view leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "日报质量优化已完成") {
		t.Fatalf("material outcome missing from report view: %s", text)
	}
}

func TestDailySummaryPreservesEveryResultAndDoesNotRewriteArtifactMentions(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	const count = 12
	units := make([]WorkUnit, 0, count)
	for index := 0; index < count; index++ {
		text := "成果 " + strconvItoa(index) + " 已完成"
		if index == 7 {
			text = "已完成 Session Digest v2.1 的方案、开发与测试环境部署，并同步发布 aida-report@1.0.10。"
		}
		units = append(units, WorkUnit{
			WorkUnitRef:     "wu-low-loss-" + strconvItoa(index),
			Sequence:        index + 1,
			ActivityStartAt: "2026-07-16T01:00:00Z",
			ActivityEndAt:   "2026-07-16T02:00:00Z",
			Goal:            Goal{Text: "完成工作 " + strconvItoa(index)},
			Category:        "implementation",
			Status:          "completed",
			EvidenceGrade:   "A",
			ResultStatements: []ResultStatement{{
				Text: text, Source: "agent_claim_with_evidence",
			}},
		})
	}

	days := BuildDailySummaries(units, location, 5)
	if len(days) != 1 || len(days[0].Highlights) != count {
		t.Fatalf("result-bearing Work Units were capped: %#v", days)
	}
	coverage := days[0].OutcomeCoverage
	if !coverage.Complete || coverage.SourceCount != count ||
		coverage.RepresentedCount != count || days[0].HighlightsTruncated {
		t.Fatalf("incomplete outcome coverage: %#v", coverage)
	}
	got := days[0].Highlights[7].ResultStatements[0].Text
	if !strings.Contains(got, "Session Digest v2.1") ||
		!strings.Contains(got, "方案、开发与测试环境部署") ||
		!strings.Contains(got, "aida-report@1.0.10") {
		t.Fatalf("server rewrote a material lifecycle outcome: %q", got)
	}
}

func TestDailySummaryKeepsMeaningfulPendingUserGoal(t *testing.T) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	units := []WorkUnit{{
		WorkUnitRef: "wu-pending", Sequence: 1,
		ActivityStartAt: "2026-07-16T01:00:00Z",
		ActivityEndAt:   "2026-07-16T01:01:00Z",
		Goal:            Goal{Text: "继续定位尚未解决的上传故障", Source: "user_message"},
		Category:        "investigation", Status: "pending", EvidenceGrade: "D",
	}}
	days := BuildDailySummaries(units, location, 0)
	if len(days) != 1 || len(days[0].Highlights) != 1 ||
		days[0].Highlights[0].Goal != units[0].Goal.Text {
		t.Fatalf("pending user work was omitted: %+v", days)
	}
}

func TestDailyCandidatesCollapseOlderSkillVersionAndSameDigestTopic(t *testing.T) {
	units := []WorkUnit{
		{
			Sequence: 1,
			Goal:     Goal{Text: "发布 aida-report@1.0.6 并启用 Session Digest"},
			Category: "deployment",
			ResultStatements: []ResultStatement{{
				Text: "已发布 aida-report@1.0.6", Source: "agent_claim_with_evidence",
			}},
		},
		{
			Sequence: 2,
			Goal:     Goal{Text: "完成 Session Digest v2.2 和 aida-report@1.0.11 发布"},
			Category: "deployment",
			ResultStatements: []ResultStatement{{
				Text:   "已发布 aida-report@1.0.11，Session Digest v2.2 已验证",
				Source: "agent_claim_with_evidence",
			}},
		},
	}
	got := consolidateDailyCandidates(units)
	if len(got) != 1 || got[0].Sequence != 2 {
		t.Fatalf("older final state was not superseded: %#v", got)
	}
}

func TestSkillVersionDoesNotHideDifferentPrimaryOutcome(t *testing.T) {
	units := []WorkUnit{
		{
			Sequence: 1,
			Goal:     Goal{Text: "修复日报上海时区契约"},
			Category: "implementation",
			ResultStatements: []ResultStatement{{
				Text:   "时区问题已修复，并使用 aida-report@1.0.7 和 Session Digest 验证",
				Source: "agent_claim_with_evidence",
			}},
		},
		{
			Sequence: 2,
			Goal:     Goal{Text: "发布 aida-report@1.0.11"},
			Category: "deployment",
			ResultStatements: []ResultStatement{{
				Text: "aida-report@1.0.11 已发布", Source: "agent_claim_with_evidence",
			}},
		},
	}
	got := consolidateDailyCandidates(units)
	if len(got) != 2 {
		t.Fatalf("different primary outcome was hidden by asset version: %#v", got)
	}
}

func TestMergeReportPeriodSummariesKeepsLatestCrossSliceFinalState(t *testing.T) {
	older := &ReportPeriodSummary{
		StartDate: "2026-07-16",
		EndDate:   "2026-07-16",
		Days: []DailySummary{{
			Date: "2026-07-16",
			Highlights: []DailyHighlight{
				{
					WorkUnitRef:   "old-digest",
					Sequence:      64,
					ActivityEndAt: "2026-07-16T12:54:06Z",
					Category:      "implementation",
					Status:        "completed",
					EvidenceGrade: "A",
					Goal:          "开始吧",
					ResultStatements: []ResultStatement{{
						Text:   "已完成 Session Digest v2.1 开发和测试环境部署。",
						Source: "agent_claim_with_evidence",
					}},
				},
				{
					WorkUnitRef:   "old-rtk",
					Sequence:      61,
					ActivityEndAt: "2026-07-16T11:15:44Z",
					Category:      "investigation",
					Status:        "completed",
					EvidenceGrade: "A",
					Goal:          "调研 RTK 清洗 Session 的方式",
					ResultStatements: []ResultStatement{{
						Text:   "RTK 可以借鉴。",
						Source: "agent_claim_with_evidence",
					}},
				},
			},
		}},
	}
	newer := &ReportPeriodSummary{
		StartDate: "2026-07-16",
		EndDate:   "2026-07-16",
		Days: []DailySummary{{
			Date: "2026-07-16",
			Highlights: []DailyHighlight{
				{
					WorkUnitRef:   "new-digest",
					Sequence:      4,
					ActivityEndAt: "2026-07-16T14:43:09Z",
					Category:      "implementation",
					Status:        "completed",
					EvidenceGrade: "A",
					Goal:          "走完日报质量优化和真实流程测试",
					ResultStatements: []ResultStatement{{
						Text:   "Session Digest v2.4 已完成结果质量优化。",
						Source: "agent_claim_with_evidence",
					}},
				},
				{
					WorkUnitRef:   "new-rtk",
					Sequence:      3,
					ActivityEndAt: "2026-07-16T13:50:00Z",
					Category:      "investigation",
					Status:        "completed",
					EvidenceGrade: "B",
					Goal:          "完成 RTK Session 清洗借鉴与稳定性影响评审",
					ResultStatements: []ResultStatement{{
						Text:   "RTK 借鉴范围已收敛。",
						Source: "agent_claim_with_evidence",
					}},
				},
			},
		}},
	}

	got := MergeReportPeriodSummaries(
		[]*ReportPeriodSummary{older, newer},
		"2026-07-16",
		"2026-07-16",
		6,
	)
	if got == nil || len(got.Days) != 1 {
		t.Fatalf("unexpected merged period: %#v", got)
	}
	refs := map[string]bool{}
	for _, highlight := range got.Days[0].Highlights {
		refs[highlight.WorkUnitRef] = true
	}
	if !refs["old-digest"] || !refs["old-rtk"] ||
		!refs["new-digest"] || !refs["new-rtk"] ||
		!got.Days[0].OutcomeCoverage.Complete {
		t.Fatalf("cross-slice outcomes were not preserved: %#v", got.Days[0].Highlights)
	}
}

func TestMergeReportPeriodSummarySourcesPreservesSessionCoverage(t *testing.T) {
	makeSummary := func(prefix string, count int, grade string) *ReportPeriodSummary {
		highlights := make([]DailyHighlight, 0, count)
		for index := range count {
			highlights = append(highlights, DailyHighlight{
				WorkUnitRef:   prefix + "-" + string(rune('a'+index)),
				Sequence:      count - index,
				ActivityEndAt: "2026-07-16T15:00:00Z",
				Category:      "implementation",
				Status:        "completed",
				EvidenceGrade: grade,
				Goal:          prefix + " feature" + string(rune('a'+index)),
				ResultStatements: []ResultStatement{{
					Text:   prefix + " 已完成独立功能 " + string(rune('a'+index)),
					Source: "agent_claim_with_evidence",
				}},
			})
		}
		return &ReportPeriodSummary{
			StartDate: "2026-07-16",
			EndDate:   "2026-07-16",
			Days: []DailySummary{{
				Date:       "2026-07-16",
				Highlights: highlights,
			}},
		}
	}

	got := MergeReportPeriodSummarySources(
		[]ReportPeriodSummarySource{
			{SourceRef: "session-a", Summary: makeSummary("alpha", 6, "A")},
			{SourceRef: "session-b", Summary: makeSummary("bravo", 1, "B")},
			{SourceRef: "session-c", Summary: makeSummary("charlie", 1, "B")},
		},
		"2026-07-16",
		"2026-07-16",
		6,
	)
	if got == nil || len(got.Days) != 1 || len(got.Days[0].Highlights) != 8 ||
		!got.Days[0].OutcomeCoverage.Complete {
		t.Fatalf("unexpected merged period: %#v", got)
	}
	refs := map[string]bool{}
	for _, highlight := range got.Days[0].Highlights {
		refs[highlight.WorkUnitRef] = true
	}
	if !refs["bravo-a"] || !refs["charlie-a"] {
		t.Fatalf("selected sessions lost all representation: %#v", got.Days[0].Highlights)
	}
}

func TestConsolidateDailyCandidatesKeepsLatestAidaCLIState(t *testing.T) {
	units := []WorkUnit{
		{
			Sequence: 1,
			Goal:     Goal{Text: "支持 Aida CLI 自动升级"},
			Category: "deployment",
			ResultStatements: []ResultStatement{{
				Text:   "Aida CLI 0.1.6 已支持自动升级，无需再次运行 install.sh。",
				Source: "agent_claim_with_evidence",
			}},
		},
		{
			Sequence: 2,
			Goal:     Goal{Text: "发布 Aida CLI 最新版本"},
			Category: "deployment",
			ResultStatements: []ResultStatement{{
				Text:   "Aida CLI 0.1.11 已完成发布。",
				Source: "agent_claim_with_evidence",
			}},
		},
	}
	got := consolidateDailyCandidates(units)
	if len(got) != 1 || got[0].Sequence != 2 ||
		len(got[0].ResultStatements) != 2 {
		t.Fatalf("latest release must absorb earlier capability: %#v", got)
	}
	highlight := makeDailyHighlight(got[0], false)
	if len(highlight.ResultStatements) != 2 ||
		highlight.ResultStatements[0].Text != "Aida CLI 0.1.11 已完成发布。" ||
		!strings.Contains(highlight.ResultStatements[1].Text, "自动升级") {
		t.Fatalf("version history was not preserved for Agent synthesis: %#v", highlight)
	}
}

func TestConsolidateDailyCandidatesMergesSubAgentListRefinements(t *testing.T) {
	units := []WorkUnit{
		{
			Sequence: 1,
			Goal:     Goal{Text: "优化 Session 列表"},
			Category: "implementation",
			ResultStatements: []ResultStatement{{
				Text:   "按 ParentSessionRef 将真实子 Agent Session 归并到根 Session。",
				Source: "agent_claim_with_evidence",
			}},
		},
		{
			Sequence: 2,
			Goal:     Goal{Text: "子 Agent 显示名称不清晰"},
			Category: "implementation",
			ResultStatements: []ResultStatement{{
				Text:   "子 Agent 数量统一显示为 sub-agent。",
				Source: "agent_claim_with_evidence",
			}},
		},
	}
	got := consolidateDailyCandidates(units)
	if len(got) != 1 || len(got[0].ResultStatements) != 2 {
		t.Fatalf("sub-agent refinements were not merged: %#v", got)
	}
}
