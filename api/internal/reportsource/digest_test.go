package reportsource

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/sessiondigest"
	"github.com/aidashboard/api/internal/sessiondigestv2"
)

func TestReportSourceConfigReadModeRouting(t *testing.T) {
	full, err := DefaultConfig().Normalized()
	if err != nil || full.RequiredReadMode("42") != ReadModeFull {
		t.Fatalf("default config must remain full: config=%#v err=%v", full, err)
	}
	shadow := DefaultConfig()
	shadow.SessionReadMode = ReadModeShadow
	shadow, err = shadow.Normalized()
	if err != nil || shadow.RequiredReadMode("42") != ReadModeFull {
		t.Fatalf("shadow runs must remain full: mode=%q err=%v", shadow.RequiredReadMode("42"), err)
	}
	digest := DefaultConfig()
	digest.SessionReadMode = ReadModeDigestV1
	digest.DigestRolloutPct = 0
	digest.DigestCanaryUserIDs = []string{"42", "42"}
	digest, err = digest.Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if digest.RequiredReadMode("42") != ReadModeDigestV1 || digest.RequiredReadMode("43") != ReadModeFull {
		t.Fatalf("unexpected canary routing: 42=%q 43=%q", digest.RequiredReadMode("42"), digest.RequiredReadMode("43"))
	}
	digest.DigestRolloutPct = 100
	if digest.RequiredReadMode("43") != ReadModeDigestV1 {
		t.Fatal("100 percent rollout must route every user to digest")
	}
	v2 := DefaultConfig()
	v2.SessionReadMode = ReadModeDigestV2
	v2.DigestVersion = sessiondigestv2.Version
	v2.DigestRolloutPct = 0
	v2, err = v2.Normalized()
	if err != nil || v2.RequiredReadMode("43") != ReadModeDigestV2 {
		t.Fatalf("digest v2 must be an explicit all-user mode: mode=%q err=%v",
			v2.RequiredReadMode("43"), err)
	}
}

func TestProductConfigUsesCurrentDigestV2ForAllUsers(t *testing.T) {
	config, err := ProductConfig().Normalized()
	if err != nil {
		t.Fatal(err)
	}
	if config.SessionReadMode != ReadModeDigestV2 ||
		config.DigestVersion != sessiondigestv2.Version ||
		config.RedactionVersion != sessiondigestv2.RedactionVersion ||
		config.DigestTargetBytes != 0 ||
		config.DigestHardLimit != 0 ||
		config.RequiredReadMode("42") != ReadModeDigestV2 ||
		len(config.DigestCanaryUserIDs) != 0 {
		t.Fatalf("unexpected product report-source policy: %#v", config)
	}
}

func TestLargeContextWarningIsAdvisoryAndUsesExactBoundary(t *testing.T) {
	atLimit := Selection{ContextBytes: LargeContextWarningBytes}
	applySelectionContextWarning(&atLimit)
	if atLimit.WarningRequired || atLimit.WarningCode != "" {
		t.Fatalf("exactly 1 MiB must not warn: %+v", atLimit)
	}

	overLimit := Selection{ContextBytes: LargeContextWarningBytes + 1}
	applySelectionContextWarning(&overLimit)
	if !overLimit.WarningRequired || overLimit.WarningCode != LargeContextWarningCode {
		t.Fatalf("context over 1 MiB must warn without rejecting: %+v", overLimit)
	}
}

func TestReportSourceConfigRejectsUnsafeValues(t *testing.T) {
	tests := []Config{
		{SessionReadMode: "automatic", DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048},
		{SessionReadMode: ReadModeFull, DigestVersion: "future", RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048},
		{SessionReadMode: ReadModeDigestV2, DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048},
		{SessionReadMode: ReadModeDigestV1, DigestVersion: sessiondigestv2.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048},
		{SessionReadMode: ReadModeFull, DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 4096, DigestHardLimit: 2048},
		{SessionReadMode: ReadModeFull, DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: productDigestEnvelopeBytes + 1},
		{SessionReadMode: ReadModeDigestV1, DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048, DigestRolloutPct: 101},
		{SessionReadMode: ReadModeDigestV1, DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048, DigestCanaryUserIDs: []string{"not-a-number"}},
		{SessionReadMode: ReadModeDigestV1, DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048, DigestCanaryUserIDs: []string{"-42"}},
	}
	for index, input := range tests {
		if _, err := input.Normalized(); err == nil {
			t.Fatalf("case %d: expected invalid config", index)
		}
	}
}

func TestAssembleDigestV2PayloadKeepsCompleteResultShape(t *testing.T) {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	digest := sessiondigestv2.EmptyDigest()
	for index := 0; index < 8; index++ {
		digest.WorkUnits = append(digest.WorkUnits, sessiondigestv2.WorkUnit{
			WorkUnitRef:     "wu-" + string(rune('a'+index)),
			Sequence:        index + 1,
			ActivityStartAt: now.Add(time.Duration(index) * time.Minute).Format(time.RFC3339),
			ActivityEndAt:   now.Add(time.Duration(index+1) * time.Minute).Format(time.RFC3339),
			PeriodRelation:  "overlap",
			Goal: sessiondigestv2.Goal{
				Text: strings.Repeat("实现结果优先的摘要", 20), Source: "user_message",
			},
			Category:      "implementation",
			Status:        "completed",
			EvidenceGrade: "A",
			ResultStatements: []sessiondigestv2.ResultStatement{{
				Text: strings.Repeat("完成稳定实现并通过验证", 20),
			}},
			Evidence:    []sessiondigestv2.Evidence{},
			Changes:     []sessiondigestv2.Change{},
			Validations: []sessiondigestv2.Validation{},
			Unresolved:  []sessiondigestv2.Unresolved{},
		})
	}
	digest.Coverage.SourceWorkUnitCount = len(digest.WorkUnits)
	digest.Coverage.DetailedWorkUnitCount = len(digest.WorkUnits)
	digest.Coverage.Representation = "result_focused"
	page := digestV2ContentPage{
		SourceMode:       "explicit",
		ContentMode:      ReadModeDigestV2,
		Timezone:         biztime.Zone,
		DigestVersion:    sessiondigestv2.Version,
		RedactionVersion: sessiondigestv2.RedactionVersion,
		ContentSnapshot:  biztime.FormatRFC3339(now),
		Completeness:     "complete",
		ReturnedCount:    1,
		ReportPeriod: &sessiondigestv2.ReportPeriodSummary{
			StartDate: "2026-07-16",
			EndDate:   "2026-07-16",
			Days: []sessiondigestv2.DailySummary{{
				Date: "2026-07-16",
				Highlights: []sessiondigestv2.DailyHighlight{{
					WorkUnitRef:   "wu-current",
					ActivityEndAt: now.Format(time.RFC3339),
					Category:      "implementation",
					Status:        "completed",
					Goal:          "完成日报结果质量优化",
					ResultStatements: []sessiondigestv2.ResultStatement{{
						Text: "已形成选择级最终态摘要",
					}},
				}},
			}},
		},
		Items: []DigestV2ContentItem{{
			SourceItemRef: "item-a",
			SessionRef:    "session-a",
			AgentType:     "codex",
			ActivityStart: biztime.FormatRFC3339(now),
			ActivityEnd:   biztime.FormatRFC3339(now.Add(time.Hour)),
			StartDate:     biztime.Date(now),
			EndDate:       biztime.Date(now.Add(time.Hour)),
			DigestSHA256:  strings.Repeat("a", 64),
			Digest:        digest,
			Coverage: DigestItemCoverage{
				SourceEventCount: 100, IncludedEventCount: 20, OmittedEventCount: 80,
				Representation: "result_focused",
			},
		}},
		Coverage: DigestCoverage{
			Complete: true, SourceItemCount: 1, RepresentedItemCount: 1,
			SourceEventCount: 100, IncludedEventCount: 20, OmittedEventCount: 80,
		},
	}
	payload, err := assembleDigestV2Payload(page)
	if err != nil {
		t.Fatal(err)
	}
	var decoded digestV2ContentPage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ContentMode != ReadModeDigestV2 ||
		decoded.ReportPeriod == nil ||
		len(decoded.ReportPeriod.Days) != 1 ||
		decoded.Items[0].Digest.SchemaVersion != sessiondigestv2.Version ||
		len(decoded.Items[0].Digest.WorkUnits) != len(digest.WorkUnits) ||
		decoded.Size.ActualBytes != len(payload) ||
		decoded.Size.WarningThresholdBytes != digestPayloadWarningBytes {
		t.Fatalf("invalid v2 payload: %+v bytes=%d", decoded, len(payload))
	}
}

func TestAssembleDigestPayloadStabilizesActualBytes(t *testing.T) {
	page := digestFixturePage(2, "完成摘要")
	payload, compaction, err := assembleDigestPayload(page, 64<<10, 128<<10)
	if err != nil {
		t.Fatal(err)
	}
	if compaction != "detailed" || !json.Valid(payload) {
		t.Fatalf("unexpected payload: compaction=%q valid=%v", compaction, json.Valid(payload))
	}
	var decoded digestContentPage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Budget.ActualBytes != len(payload) || !decoded.Coverage.Complete || decoded.HasMore {
		t.Fatalf("invalid frozen budget/coverage: %#v bytes=%d", decoded, len(payload))
	}
	if decoded.Timezone != biztime.Zone || decoded.ContentSnapshot != "2026-07-16T09:02:03+08:00" {
		t.Fatalf("digest payload does not expose Shanghai time: %#v", decoded)
	}
	if decoded.Items[0].ActivityStart != "2026-07-16T09:02:03+08:00" ||
		decoded.Items[0].StartDate != "2026-07-16" || decoded.Items[0].EndDate != "2026-07-16" {
		t.Fatalf("digest item does not expose local timestamps and dates: %#v", decoded.Items[0])
	}
}

func TestAssembleDigestPayloadCompactsEveryItemAndEnforcesHardLimit(t *testing.T) {
	page := digestFixturePage(4, strings.Repeat("很长的完成结果", 80))
	payload, compaction, err := assembleDigestPayload(page, 1024, 128<<10)
	if err != nil {
		t.Fatal(err)
	}
	if compaction != "compact" {
		t.Fatalf("expected compact payload, got %q (%d bytes)", compaction, len(payload))
	}
	var decoded digestContentPage
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	for index, item := range decoded.Items {
		if item.Coverage.Representation != "compact" || !item.Coverage.Truncated {
			t.Fatalf("item %d was not fairly compacted: %#v", index, item.Coverage)
		}
	}
	if _, _, err := assembleDigestPayload(page, 1024, 1100); !errors.Is(err, ErrDigestLimitExceeded) {
		t.Fatalf("expected hard-limit failure, got %v", err)
	}
}

func digestFixturePage(count int, outcome string) digestContentPage {
	now := time.Date(2026, 7, 16, 1, 2, 3, 0, time.UTC)
	page := digestContentPage{
		SourceMode: "default", ContentMode: ReadModeDigestV1,
		Timezone:      biztime.Zone,
		DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion,
		ContentSnapshot: biztime.FormatRFC3339(now), Completeness: "complete", HasMore: false,
		Items: []DigestContentItem{},
	}
	for index := 0; index < count; index++ {
		digest := sessiondigest.EmptyDigest()
		digest.Goals = append(digest.Goals, "实现服务端 Digest")
		digest.Outcomes = append(digest.Outcomes, outcome)
		page.Items = append(page.Items, DigestContentItem{
			SourceItemRef: "item-" + string(rune('a'+index)), SessionRef: "session-ref",
			AgentType: "codex", ActivityStart: biztime.FormatRFC3339(now), ActivityEnd: biztime.FormatRFC3339(now.Add(time.Hour)),
			StartDate: biztime.Date(now), EndDate: biztime.Date(now.Add(time.Hour)),
			DigestSHA256: strings.Repeat("a", 64), Digest: digest,
			Coverage: DigestItemCoverage{
				SourceEventCount: 10, IncludedEventCount: 2, OmittedEventCount: 8,
				Representation: "detailed",
			},
		})
	}
	page.ReturnedCount = len(page.Items)
	page.Coverage = DigestCoverage{
		Complete: true, SourceItemCount: count, RepresentedItemCount: count,
		SourceEventCount: int64(count * 10), IncludedEventCount: int64(count * 2), OmittedEventCount: int64(count * 8),
	}
	return page
}
