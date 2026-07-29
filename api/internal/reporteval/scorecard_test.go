package reporteval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAggregateReviewsSupportsCleanPassImprovement(t *testing.T) {
	directory := t.TempDir()
	caseID := "case-001"
	runs := []RunReceipt{
		{CaseID: caseID, VariantVersion: "baseline", Repetition: 1, RunID: "11111111-1111-4111-8111-111111111111", Status: "succeeded"},
		{CaseID: caseID, VariantVersion: "candidate", Repetition: 1, RunID: "22222222-2222-4222-8222-222222222222", Status: "succeeded"},
	}
	manifest := BundleManifest{
		SchemaVersion: BundleSchemaVersion, DatasetVersion: "dataset/v1", DatasetSHA256: strings.Repeat("a", 64),
		RubricVersion: "rubric/v1", CreatedAt: time.Now(), Variants: validPlan().Variants, Runs: runs,
	}
	if _, err := writeJSON(filepath.Join(directory, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	for _, run := range runs {
		runDir := filepath.Join(directory, "cases", caseID, "runs", run.RunID)
		if err := os.MkdirAll(runDir, 0o750); err != nil {
			t.Fatal(err)
		}
		duration := int64(100)
		if _, err := writeJSON(filepath.Join(runDir, "run-metrics.json"), RunMetrics{
			SchemaVersion: MetricsSchemaVersion, Status: "succeeded", DurationMS: &duration,
		}); err != nil {
			t.Fatal(err)
		}
	}
	hash := strings.Repeat("b", 64)
	aiPath := filepath.Join(directory, "ai.jsonl")
	goldPath := filepath.Join(directory, "gold.jsonl")
	ai := []CaseReview{
		{CaseID: caseID, Repetition: 1, VariantVersion: "baseline", Grade: "minor", DirectlyUsable: true, Confidence: .9, ReviewerModel: "gpt-5.6-sol", InputSHA256: hash, OutputSHA256: hash},
		{CaseID: caseID, Repetition: 1, VariantVersion: "candidate", Grade: "pass", DirectlyUsable: true, Confidence: .9, ReviewerModel: "gpt-5.6-sol", InputSHA256: hash, OutputSHA256: hash},
	}
	gold := []CaseReview{
		{CaseID: caseID, Repetition: 1, VariantVersion: "baseline", Grade: "minor", DirectlyUsable: true, Confidence: 1, ReviewerModel: "human", InputSHA256: hash, OutputSHA256: hash},
		{CaseID: caseID, Repetition: 1, VariantVersion: "candidate", Grade: "pass", DirectlyUsable: true, Confidence: 1, ReviewerModel: "human", InputSHA256: hash, OutputSHA256: hash},
	}
	writeReviews := func(path string, reviews []CaseReview) {
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
	writeReviews(aiPath, ai)
	writeReviews(goldPath, gold)
	result, err := AggregateReviews(directory, aiPath, goldPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Missing) != 0 || len(result.Comparisons) != 1 || result.Comparisons[0].Conclusion != "improvement_supported" || result.Comparisons[0].Wins != 1 {
		t.Fatalf("result = %#v", result)
	}
}
