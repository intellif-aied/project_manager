package reportbrief

import "testing"

func reviewCandidate() Stored {
	return Stored{
		ContextHash: "context-hash",
		Payload: Payload{
			SchemaVersion: SchemaVersion, ReportType: personalDaily,
			Period: Period{Start: "2026-08-06", End: "2026-08-06"},
			Workstreams: []Workstream{
				{
					Subject: "AIDA日报系统Project Memory与CLI修复", Title: "AIDA日报系统Project Memory与CLI修复",
					Deliverables: []Deliverable{
						{Result: "项目命名已经稳定且更加准确", FactRefs: []string{"fact-407"}},
						{Result: "已支持每日真实邮件投递", FactRefs: []string{"fact-433"}},
					},
				},
			},
		},
	}
}

func TestFinalizeReviewAppliesSupportedPatch(t *testing.T) {
	finalized, err := FinalizeReview(reviewCandidate(), ReviewDecision{
		Decision: ReviewDecisionRepair,
		Patches: []ReviewPatch{
			{Op: "replace_subject", Target: "w1", Value: "AIDA", SupportingFactRefs: []string{"fact-407"}},
			{Op: "replace_title", Target: "w1", Value: "推进 AIDA 日报项目关联与邮件能力", SupportingFactRefs: []string{"fact-407", "fact-433"}},
			{Op: "replace_result", Target: "w1.d1", Value: "完成测试服项目关联样本验证，项目命名效果仍待进一步验证", SupportingFactRefs: []string{"fact-407"}},
			{Op: "add_qualifier", Target: "w1.d2", Value: "完成日报邮件后端能力，功能默认关闭，待真实 SMTP 投递验证", SupportingFactRefs: []string{"fact-433"}},
		},
	}, map[string]struct{}{"fact-407": {}, "fact-433": {}})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Mode != ReviewModeRepaired || finalized.Stored.Payload.Workstreams[0].Subject != "AIDA" {
		t.Fatalf("finalized = %#v", finalized)
	}
	results := finalized.Stored.Payload.Workstreams[0].Deliverables
	if results[0].Result != "完成测试服项目关联样本验证，项目命名效果仍待进一步验证" ||
		results[1].Result != "完成日报邮件后端能力，功能默认关闭，待真实 SMTP 投递验证" {
		t.Fatalf("results = %#v", results)
	}
}

func TestFinalizeReviewInvalidPatchDropsKnownRiskBeforeCandidate(t *testing.T) {
	finalized, err := FinalizeReview(reviewCandidate(), ReviewDecision{
		Decision: ReviewDecisionRepair,
		Issues:   []ReviewIssue{{Code: "overclaim", Target: "w1.d1", FactRefs: []string{"fact-407"}}},
		Patches: []ReviewPatch{{
			Op: "replace_result", Target: "w1.d1", Value: "未经事实支持的新结论",
			SupportingFactRefs: []string{"fact-999"},
		}},
	}, map[string]struct{}{"fact-407": {}, "fact-433": {}})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Mode != ReviewModeConservative || len(finalized.Stored.Payload.Workstreams[0].Deliverables) != 1 {
		t.Fatalf("known risk was not removed: %#v", finalized)
	}
	if finalized.Stored.Payload.Workstreams[0].Deliverables[0].FactRefs[0] != "fact-433" {
		t.Fatalf("wrong deliverable survived: %#v", finalized.Stored.Payload.Workstreams[0].Deliverables)
	}
}

func TestFinalizeReviewCanAddOneFrozenOmission(t *testing.T) {
	finalized, err := FinalizeReview(reviewCandidate(), ReviewDecision{
		Decision: ReviewDecisionRepair,
		Patches: []ReviewPatch{{
			Op: "add_workstream", Subject: "AIDA CLI", Title: "修复 Windows 自动同步体验",
			Result: "修复 Windows 自动同步锁与闪窗问题", FactRefs: []string{"fact-351"},
		}},
	}, map[string]struct{}{"fact-351": {}})
	if err != nil {
		t.Fatal(err)
	}
	if len(finalized.Stored.Payload.Workstreams) != 2 || finalized.Stored.Payload.Workstreams[1].Subject != "AIDA CLI" {
		t.Fatalf("frozen omission was not added: %#v", finalized.Stored.Payload.Workstreams)
	}
}

func TestFinalizeReviewRejectsUnknownDecision(t *testing.T) {
	if _, err := FinalizeReview(reviewCandidate(), ReviewDecision{Decision: "retry"}, nil); err == nil {
		t.Fatal("expected unknown decision to fail")
	}
}

func TestFinalizeReviewOutOfRangePatchFallsBackWithoutPanic(t *testing.T) {
	candidate := reviewCandidate()
	finalized, err := FinalizeReview(candidate, ReviewDecision{
		Decision: ReviewDecisionRepair,
		Issues:   []ReviewIssue{{Code: "overclaim", Target: "w1.d1"}},
		Patches: []ReviewPatch{{
			Op: "replace_result", Target: "w5.d3", Value: "越界修改", SupportingFactRefs: []string{"fact-001"},
		}},
	}, map[string]struct{}{"fact-001": {}})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Mode != ReviewModeConservative || len(finalized.Stored.Payload.Workstreams) != 1 ||
		len(finalized.Stored.Payload.Workstreams[0].Deliverables) != 1 ||
		finalized.Stored.Payload.Workstreams[0].Deliverables[0].FactRefs[0] != "fact-433" {
		t.Fatalf("known risk was not conservatively removed: %#v", finalized)
	}
}

func TestFinalizeReviewAcceptCannotCarryKnownIssues(t *testing.T) {
	_, err := FinalizeReview(reviewCandidate(), ReviewDecision{
		Decision: ReviewDecisionAccept,
		Issues:   []ReviewIssue{{Code: "overclaim", Target: "w1.d1"}},
	}, map[string]struct{}{"fact-001": {}})
	if err == nil {
		t.Fatal("accept with known issues must be rejected")
	}
}

func TestFinalizeReviewWorkstreamRiskDropsWholeWorkstream(t *testing.T) {
	finalized, err := FinalizeReview(reviewCandidate(), ReviewDecision{
		Decision: ReviewDecisionConservative,
		Issues:   []ReviewIssue{{Code: "overclaim", Target: "w1"}},
	}, map[string]struct{}{"fact-001": {}})
	if err != nil {
		t.Fatal(err)
	}
	if !finalized.Stored.Payload.NoReportableWork || len(finalized.Stored.Payload.Workstreams) != 0 {
		t.Fatalf("workstream-level risk must not pass unchanged: %#v", finalized.Stored.Payload)
	}
}

func TestFinalizeReviewMergesFragmentedChildWorkstreamsIntoReviewedParent(t *testing.T) {
	candidate := reviewCandidate()
	candidate.Payload.Workstreams = append(candidate.Payload.Workstreams, Workstream{
		Subject: "CLI 工具", Title: "完善 CLI 工具",
		Deliverables: []Deliverable{{Result: "完善 CLI 同步流程", FactRefs: []string{"fact-433"}}},
	})
	finalized, err := FinalizeReview(candidate, ReviewDecision{
		Decision: ReviewDecisionRepair,
		Issues:   []ReviewIssue{{Code: "project_fragmentation", Target: "w1", FactRefs: []string{"fact-407", "fact-433"}}},
		Patches: []ReviewPatch{
			{Op: "replace_subject", Target: "w1", Value: "芯片验证平台", SupportingFactRefs: []string{"fact-407", "fact-433"}},
			{Op: "replace_title", Target: "w1", Value: "推进芯片验证平台功能建设", SupportingFactRefs: []string{"fact-407", "fact-433"}},
			{Op: "merge_workstream", Target: "w2", Destination: "w1"},
		},
	}, map[string]struct{}{"fact-407": {}, "fact-433": {}})
	if err != nil {
		t.Fatal(err)
	}
	if len(finalized.Stored.Payload.Workstreams) != 1 ||
		finalized.Stored.Payload.Workstreams[0].Subject != "芯片验证平台" ||
		len(finalized.Stored.Payload.Workstreams[0].Deliverables) != 3 {
		t.Fatalf("fragmented workstreams were not merged: %#v", finalized.Stored.Payload)
	}
}

func TestFinalizeReviewRejectsChainedWorkstreamMerge(t *testing.T) {
	candidate := reviewCandidate()
	candidate.Payload.Workstreams = append(candidate.Payload.Workstreams,
		Workstream{Subject: "模块二", Title: "模块二", Deliverables: []Deliverable{{Result: "结果二", FactRefs: []string{"fact-433"}}}},
		Workstream{Subject: "模块三", Title: "模块三", Deliverables: []Deliverable{{Result: "结果三", FactRefs: []string{"fact-434"}}}},
	)
	_, err := applyReviewPatches(candidate.Payload.Workstreams, []ReviewPatch{
		{Op: "merge_workstream", Target: "w2", Destination: "w1"},
		{Op: "merge_workstream", Target: "w3", Destination: "w2"},
	}, map[string]struct{}{"fact-407": {}, "fact-433": {}, "fact-434": {}})
	if err == nil {
		t.Fatal("chained merge must be rejected")
	}
}

func TestFinalizeReviewAcceptsBoundedReplacementAliases(t *testing.T) {
	candidate := reviewCandidate()
	finalized, err := FinalizeReview(candidate, ReviewDecision{
		Decision: ReviewDecisionRepair,
		Patches: []ReviewPatch{{
			Op: "replace_result", Target: "w1.d1", Result: "完成日报语义审核接入", FactRefs: []string{"fact-001"},
		}},
	}, map[string]struct{}{"fact-001": {}})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Mode != ReviewModeRepaired || finalized.Stored.Payload.Workstreams[0].Deliverables[0].Result != "完成日报语义审核接入" {
		t.Fatalf("replacement aliases were not applied: %#v", finalized)
	}
}
