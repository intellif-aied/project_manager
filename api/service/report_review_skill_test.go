package service

import (
	"strings"
	"testing"
)

func TestReportReviewSkillHasBoundedVerifierContract(t *testing.T) {
	markdown := ReportReviewSkillMarkdown()
	for _, required := range []string{
		"get_report_review_context exactly once", "project_candidates only as optional user-established project labels",
		"related Facts and cues match at least two Workstreams", "merge_workstream",
		"attach those Workstreams to canonical_name", "opaque business name",
		"aliases and workstream_cues explain the established scope",
		"identity_usage=parent_label_for_matching_cues",
		"proposed_targets are Resolver proposals", "service applies them deterministically",
		"one to three Workstreams", "two to six reader-facing lines", "35–90 Chinese characters",
		"Merge Workstreams that serve one daily objective", "metric-only", "machine names",
		"decision=accept", "decision=repair", "decision=conservative",
		"at most 8 patches", "write_report_review exactly once", "Do not retry",
	} {
		if !strings.Contains(markdown, required) {
			t.Fatalf("review Skill missing %q", required)
		}
	}
}
