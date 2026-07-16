package reportsource

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/biztime"
	"github.com/aidashboard/api/internal/sessiondigest"
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
}

func TestReportSourceConfigRejectsUnsafeValues(t *testing.T) {
	tests := []Config{
		{SessionReadMode: "automatic", DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048},
		{SessionReadMode: ReadModeFull, DigestVersion: "future", RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 1024, DigestHardLimit: 2048},
		{SessionReadMode: ReadModeFull, DigestVersion: sessiondigest.Version, RedactionVersion: sessiondigest.RedactionVersion, DigestTargetBytes: 4096, DigestHardLimit: 2048},
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
