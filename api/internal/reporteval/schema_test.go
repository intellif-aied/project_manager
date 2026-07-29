package reporteval

import (
	"encoding/json"
	"testing"
)

func validDataset() DatasetManifest {
	return DatasetManifest{
		SchemaVersion: DatasetSchemaVersion, DatasetVersion: "daily-report-eval/v1",
		RubricVersion: "daily-report-rubric/v1", ReportType: "personal_daily",
		Cases: []EvaluationCase{{
			CaseID: "case-001", ReportDate: "2026-06-11",
			SelectedSessionSliceKeys: []string{"11111111-1111-4111-8111-111111111111"},
			EvidenceBaseline:         json.RawMessage(`{"facts":[]}`), Tags: []string{"short"},
			Source: "local_session", UsageAuthorized: true,
		}},
	}
}

func validPlan() ExecutionPlan {
	return ExecutionPlan{
		SchemaVersion: PlanSchemaVersion, DatasetFile: "dataset.json",
		Repetitions: 1, TimeoutSeconds: 300,
		Variants: []VariantSpec{
			{VariantVersion: "baseline", AgentID: "default"},
			{VariantVersion: "candidate", AgentID: "candidate-agent"},
		},
	}
}

func TestDatasetValidate(t *testing.T) {
	dataset := validDataset()
	if err := dataset.Validate(); err != nil {
		t.Fatalf("valid dataset: %v", err)
	}
	dataset.Cases[0].UsageAuthorized = false
	if err := dataset.Validate(); err == nil {
		t.Fatal("expected unauthorized source to be rejected")
	}
}

func TestPlanRequiresTwoOrThreeVariants(t *testing.T) {
	plan := validPlan()
	if err := plan.Validate(); err != nil {
		t.Fatalf("valid plan: %v", err)
	}
	plan.Variants = plan.Variants[:1]
	if err := plan.Validate(); err == nil {
		t.Fatal("expected one variant to be rejected")
	}
}

func TestCanonicalSHA256StableForDatasetNormalization(t *testing.T) {
	left := validDataset()
	left.Cases[0].Tags = []string{"long", "short"}
	left.Cases[0].SelectedSessionSliceKeys = []string{"22222222-2222-4222-8222-222222222222", "11111111-1111-4111-8111-111111111111"}
	right := validDataset()
	right.Cases[0].Tags = []string{"short", "long"}
	right.Cases[0].SelectedSessionSliceKeys = []string{"11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222"}
	NormalizeDataset(&left)
	NormalizeDataset(&right)
	leftHash, _ := CanonicalSHA256(left)
	rightHash, _ := CanonicalSHA256(right)
	if leftHash != rightHash {
		t.Fatalf("normalized hash mismatch: %s != %s", leftHash, rightHash)
	}
}
