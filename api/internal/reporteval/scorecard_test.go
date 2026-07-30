package reporteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeScorecardBundle(t *testing.T, baselineDraft, candidateDraft string) string {
	t.Helper()
	dataset, err := LoadFrozenDataset(writeFrozenDatasetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "bundle")
	exporter := Exporter{OutputDir: directory}
	if _, err := exporter.Initialize(dataset); err != nil {
		t.Fatal(err)
	}
	plan := validPlan()
	receipts := []RunReceipt{}
	for index, variant := range plan.Variants {
		manifest := json.RawMessage(`{"agent_id":"` + variant.AgentID + `","pipeline_profile":"test-final-only","stages":["final"]}`)
		var manifestValue any
		_ = json.Unmarshal(manifest, &manifestValue)
		manifestHash, _ := CanonicalSHA256(manifestValue)
		runID := []string{
			"11111111-1111-4111-8111-111111111111",
			"22222222-2222-4222-8222-222222222222",
		}[index]
		draft := []string{baselineDraft, candidateDraft}[index]
		now := time.Date(2026, 7, 30, 10, index, 0, 0, time.UTC)
		finished := now.Add(time.Second)
		artifacts := RunArtifactEnvelope{
			SchemaVersion: RunArtifactsSchemaVersion, RunID: runID, Status: "succeeded",
			CreatedAt: now, StartedAt: &now, FinishedAt: &finished,
			SourceIdentitySHA256: dataset.Sources["case-001"].SourceIdentitySHA256,
			VariantManifest:      manifest, VariantSHA256: manifestHash, GeneratedDraft: draft,
		}
		receipt := RunReceipt{RunID: runID, CaseID: "case-001", VariantVersion: variant.VariantVersion, Repetition: 1,
			Runtime: RuntimeAttestation{
				SchemaVersion: RuntimeAttestationVersion, Enabled: true, Environment: "test",
				BuildRevision: "revision-" + variant.VariantVersion, InstanceID: "instance-" + variant.VariantVersion,
			},
		}
		if err := exporter.ExportRun(dataset.Manifest.Cases[0], dataset.Sources["case-001"], variant, &receipt, artifacts); err != nil {
			t.Fatal(err)
		}
		receipts = append(receipts, receipt)
	}
	if err := exporter.Finalize(dataset, plan, receipts); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyBundle(directory)
	if err != nil || !verification.Valid {
		t.Fatalf("bundle verification = %#v err=%v", verification, err)
	}
	return directory
}

func scorecardReview(variant, grade, source string) CaseReview {
	hash := strings.Repeat("b", 64)
	review := CaseReview{
		CaseID: "case-001", Repetition: 1, VariantVersion: variant, Grade: grade,
		DirectlyUsable: grade != "unacceptable", Confidence: 1,
		RubricVersion: "daily-report-rubric/v1", InputSHA256: hash, OutputSHA256: hash,
	}
	if source == "ai" {
		review.ReviewerKind = "model"
		review.ReviewerModel = "reviewer"
		review.PromptSHA256 = hash
		review.SkillSHA256 = hash
	} else {
		review.ReviewerKind = "human"
		review.ReviewerID = "reviewer-001"
		review.HumanConfirmed = true
	}
	if grade != "pass" {
		review.Issues = []ReviewIssue{{
			ErrorType: "POOR_READABILITY", Severity: "minor", FirstBadStage: "final",
			FinalRefs: []string{"final:1"}, Explanation: "列表结构需要轻微整理",
		}}
	}
	return review
}

func writeReviewFile(t *testing.T, path string, reviews []CaseReview) {
	t.Helper()
	var payload []byte
	for _, review := range reviews {
		line, err := EncodeJSONLine(review)
		if err != nil {
			t.Fatal(err)
		}
		payload = append(payload, line...)
	}
	if err := os.WriteFile(path, payload, 0o640); err != nil {
		t.Fatal(err)
	}
}

func TestAggregateReviewsSupportsCleanPassImprovementAndReportsPattern(t *testing.T) {
	directory := writeScorecardBundle(t, "1. 完成协议设计", strings.Repeat("候选日报内容", 80))
	aiPath := filepath.Join(directory, "ai.jsonl")
	goldPath := filepath.Join(directory, "gold.jsonl")
	writeReviewFile(t, aiPath, []CaseReview{
		scorecardReview("baseline", "minor", "ai"), scorecardReview("candidate", "pass", "ai"),
	})
	writeReviewFile(t, goldPath, []CaseReview{
		scorecardReview("baseline", "minor", "gold"), scorecardReview("candidate", "pass", "gold"),
	})
	result, err := AggregateReviews(directory, aiPath, goldPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 0 || len(result.Comparisons) != 1 || result.Comparisons[0].Conclusion != "improvement_supported" || result.Comparisons[0].Wins != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Scorecards[1].Pattern.CharacterCount.GeneratedP50 <= result.Scorecards[1].Pattern.CharacterCount.BaselineP50 {
		t.Fatalf("pattern comparison was not recorded: %#v", result.Scorecards[1].Pattern)
	}
}

func TestAggregateReviewsRejectsFirstBadStageAbsentFromVariant(t *testing.T) {
	directory := writeScorecardBundle(t, "1. 基线", "1. 候选")
	aiPath := filepath.Join(directory, "ai.jsonl")
	bad := scorecardReview("baseline", "minor", "ai")
	bad.Issues[0].FirstBadStage = "brief"
	writeReviewFile(t, aiPath, []CaseReview{bad, scorecardReview("candidate", "pass", "ai")})
	if _, err := AggregateReviews(directory, aiPath, ""); err == nil || !strings.Contains(err.Error(), "absent first_bad_stage") {
		t.Fatalf("expected first bad stage failure, got %v", err)
	}
}

func TestAggregateReviewsRejectsUnknownEvidenceReference(t *testing.T) {
	directory := writeScorecardBundle(t, "1. 基线", "1. 候选")
	aiPath := filepath.Join(directory, "ai.jsonl")
	bad := scorecardReview("baseline", "minor", "ai")
	bad.Issues[0].EvidenceRefs = []string{"source-001/event-999999"}
	writeReviewFile(t, aiPath, []CaseReview{bad, scorecardReview("candidate", "pass", "ai")})
	if _, err := AggregateReviews(directory, aiPath, ""); err == nil || !strings.Contains(err.Error(), "references unknown evidence") {
		t.Fatalf("expected unknown evidence failure, got %v", err)
	}
}

func TestAggregateReviewsRejectsModelReviewUsedAsGold(t *testing.T) {
	directory := writeScorecardBundle(t, "1. 基线", "1. 候选")
	aiPath := filepath.Join(directory, "ai.jsonl")
	goldPath := filepath.Join(directory, "gold.jsonl")
	writeReviewFile(t, aiPath, []CaseReview{
		scorecardReview("baseline", "pass", "ai"), scorecardReview("candidate", "pass", "ai"),
	})
	writeReviewFile(t, goldPath, []CaseReview{scorecardReview("baseline", "pass", "ai")})
	if _, err := AggregateReviews(directory, aiPath, goldPath); err == nil || !strings.Contains(err.Error(), "invalid gold review") {
		t.Fatalf("expected nonhuman Gold failure, got %v", err)
	}
}

func TestReviewGradeMustMatchIssueSeverity(t *testing.T) {
	minor := scorecardReview("baseline", "minor", "ai")
	minor.Issues[0].Severity = "unacceptable"
	if validReview(minor, "ai") {
		t.Fatal("minor review must not contain an unacceptable issue")
	}

	unacceptable := scorecardReview("baseline", "unacceptable", "ai")
	if validReview(unacceptable, "ai") {
		t.Fatal("unacceptable review must contain an unacceptable issue")
	}
	unacceptable.Issues[0].Severity = "unacceptable"
	if !validReview(unacceptable, "ai") {
		t.Fatal("unacceptable review with an unacceptable issue should be valid")
	}
}

func TestPassToMinorDoesNotAloneBlockAnOtherwiseImprovedCandidate(t *testing.T) {
	baseline := VariantScorecard{DirectlyUsableRate: .8, CleanPassRate: .5}
	candidate := VariantScorecard{DirectlyUsableRate: .9, CleanPassRate: .6}
	comparison := VariantComparison{RegressedCases: []string{"case-001/1"}}
	if conclusion := concludeComparison(nil, baseline, candidate, comparison); conclusion != "improvement_supported" {
		t.Fatalf("pass to minor should be recorded but not veto improvement: %s", conclusion)
	}
	comparison.GoldUnacceptableRegressions = []string{"case-001/1"}
	if conclusion := concludeComparison(nil, baseline, candidate, comparison); conclusion != "improvement_not_supported" {
		t.Fatalf("Gold unacceptable regression must veto improvement: %s", conclusion)
	}
}
