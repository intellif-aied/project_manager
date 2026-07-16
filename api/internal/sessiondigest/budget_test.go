package sessiondigest

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestEnforceItemBudgetProducesValidBoundedUTF8JSON(t *testing.T) {
	digest := EmptyDigest()
	for index := 0; index < 80; index++ {
		digest.Goals = append(digest.Goals, strings.Repeat("需求中文", 80))
		digest.Outcomes = append(digest.Outcomes, strings.Repeat("完成中文", 100))
		digest.FilesChanged = append(digest.FilesChanged, strings.Repeat("目录/", 40)+"file.go")
		digest.Validations = append(digest.Validations, Validation{Name: "go test", Status: "passed", Summary: strings.Repeat("成功", 80)})
		digest.Blockers = append(digest.Blockers, strings.Repeat("阻塞", 80))
	}

	got, encoded, truncated := EnforceItemBudget(digest, 1024)
	if !truncated {
		t.Fatal("expected the oversized digest to be truncated")
	}
	if len(encoded) > 1024 {
		t.Fatalf("encoded digest exceeds budget: %d", len(encoded))
	}
	if !utf8.Valid(encoded) || json.Valid(encoded) == false {
		t.Fatalf("budget result is not valid UTF-8 JSON: %q", encoded)
	}
	if got.Goals == nil || got.Outcomes == nil || got.FilesChanged == nil || got.Validations == nil || got.Blockers == nil {
		t.Fatal("digest arrays must remain non-null")
	}
}

func TestCompactDigestPrefersRecentOutcomeAndFailedValidation(t *testing.T) {
	input := Digest{
		Goals:        []string{"first goal", "second goal"},
		Outcomes:     []string{"old outcome", "new outcome"},
		FilesChanged: []string{"a.go", "b.go", "c.go", "d.go"},
		Validations: []Validation{
			{Name: "go test", Status: "passed"},
			{Name: "pnpm build", Status: "failed"},
			{Name: "pnpm lint", Status: "passed"},
		},
		Blockers: []string{"old blocker", "new blocker"},
	}
	got := CompactDigest(input)
	if len(got.Outcomes) != 1 || got.Outcomes[0] != "new outcome" {
		t.Fatalf("unexpected outcomes: %#v", got.Outcomes)
	}
	if len(got.Validations) != 2 || got.Validations[0].Status != "failed" {
		t.Fatalf("failed validation should be retained first: %#v", got.Validations)
	}
}

func TestDetailedBudgetKeepsRecentOutcomesAndFailedValidations(t *testing.T) {
	digest := EmptyDigest()
	for index := 0; index < 25; index++ {
		digest.Outcomes = append(digest.Outcomes, "outcome-"+string(rune('a'+index)))
		status := "passed"
		if index == 24 {
			status = "failed"
		}
		digest.Validations = append(digest.Validations, Validation{Name: "validation-" + string(rune('a'+index)), Status: status})
	}
	got, _, truncated := EnforceItemBudget(digest, DefaultItemBytes)
	if !truncated || len(got.Outcomes) != 12 || got.Outcomes[len(got.Outcomes)-1] != "outcome-y" {
		t.Fatalf("recent outcomes were not retained: %#v", got.Outcomes)
	}
	if len(got.Validations) != 20 || got.Validations[0].Status != "failed" {
		t.Fatalf("failed validations must survive field-count compaction: %#v", got.Validations)
	}
}

func TestTruncateUTF8BytesRemainsValidAtTinyLimits(t *testing.T) {
	for limit := 1; limit <= 3; limit++ {
		got, truncated := truncateUTF8Bytes("中文", limit)
		if !truncated || !utf8.ValidString(got) || len(got) > limit {
			t.Fatalf("limit=%d got=%q valid=%v truncated=%v", limit, got, utf8.ValidString(got), truncated)
		}
	}
}
