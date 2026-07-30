package reporteval

import (
	"strings"
	"testing"
)

func validDataset() DatasetManifest {
	return DatasetManifest{
		SchemaVersion: DatasetSchemaVersion, DatasetVersion: "daily-report-eval/v2",
		RubricVersion: "daily-report-rubric/v1", ReportType: "personal_daily",
		PatternBaseline: PatternBaselineReference{
			DatasetVersion: "production-manual-daily/v1",
			Statistics:     FileReference{Path: "pattern/statistics.json", SHA256: strings.Repeat("a", 64)},
		},
		Cases: []EvaluationCase{{
			CaseID: "case-001", ReportDate: "2026-06-11",
			SelectedSessionSliceKeys: []string{"11111111-1111-4111-8111-111111111111"},
			SourceEvidence:           FileReference{Path: "sources/case-001.json", SHA256: strings.Repeat("b", 64)},
			EvidenceBaseline: EvidenceBaseline{
				SchemaVersion: EvidenceBaselineSchemaVersion,
				Items: []EvidenceItem{{
					EvidenceID: "work-001", Disposition: "required", Statement: "完成协议设计",
					SourceRefs: []string{"source-001/event-000001"}, State: "completed", Environment: "development",
				}},
				TopicRelations: []TopicRelation{}, ForbiddenAdditions: []string{"不得声称已经发布生产"},
			},
			Tags: []string{"short"}, Source: "local_session", UsageAuthorized: true,
		}},
	}
}

func validPlan() ExecutionPlan {
	return ExecutionPlan{
		SchemaVersion: PlanSchemaVersion, DatasetFile: "dataset.json",
		Repetitions: 1, TimeoutSeconds: 300,
		Variants: []VariantSpec{
			{VariantVersion: "baseline", AgentID: "default", Runtime: RuntimeSpec{BaseURL: "http://127.0.0.1:18090", TokenEnv: "AIDA_EVAL_BASELINE_TOKEN"}},
			{VariantVersion: "candidate", AgentID: "candidate-agent", Runtime: RuntimeSpec{BaseURL: "http://127.0.0.1:28090", TokenEnv: "AIDA_EVAL_CANDIDATE_TOKEN"}},
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

func TestDatasetRequiresFrozenSourceReference(t *testing.T) {
	dataset := validDataset()
	dataset.Cases[0].SourceEvidence = FileReference{}
	if err := dataset.Validate(); err == nil {
		t.Fatal("expected missing frozen source reference to be rejected")
	}
}

func TestDatasetRejectsReservedOrDuplicateReferencedPaths(t *testing.T) {
	dataset := validDataset()
	dataset.PatternBaseline.Statistics.Path = "manifest.json"
	if err := dataset.Validate(); err == nil || !strings.Contains(err.Error(), "reserved bundle path") {
		t.Fatalf("expected reserved path failure, got %v", err)
	}

	dataset = validDataset()
	dataset.Cases[0].SourceEvidence.Path = dataset.PatternBaseline.Statistics.Path
	if err := dataset.Validate(); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("expected duplicate path failure, got %v", err)
	}

	dataset = validDataset()
	dataset.Cases[0].SourceEvidence.Path = "cases/case-001/source.json"
	if err := dataset.Validate(); err == nil || !strings.Contains(err.Error(), "reserved bundle path") {
		t.Fatalf("expected cases path failure, got %v", err)
	}
}

func TestDatasetRejectsDuplicateSliceKeysAndTags(t *testing.T) {
	dataset := validDataset()
	dataset.Cases[0].SelectedSessionSliceKeys = append(dataset.Cases[0].SelectedSessionSliceKeys, dataset.Cases[0].SelectedSessionSliceKeys[0])
	if err := dataset.Validate(); err == nil {
		t.Fatal("expected duplicate slice key to be rejected")
	}
	dataset = validDataset()
	dataset.Cases[0].Tags = []string{"short", "short"}
	if err := dataset.Validate(); err == nil {
		t.Fatal("expected duplicate tag to be rejected")
	}
}

func TestEvidenceBaselineRejectsEmptyAndUnknownRelations(t *testing.T) {
	dataset := validDataset()
	dataset.Cases[0].EvidenceBaseline = EvidenceBaseline{SchemaVersion: EvidenceBaselineSchemaVersion}
	if err := dataset.Validate(); err == nil {
		t.Fatal("expected empty baseline to be rejected")
	}

	dataset = validDataset()
	dataset.Cases[0].EvidenceBaseline.TopicRelations = []TopicRelation{{
		Relation: "same_workstream", EvidenceRefs: []string{"work-001", "work-999"},
	}}
	if err := dataset.Validate(); err == nil {
		t.Fatal("expected unknown evidence relation to be rejected")
	}
}

func TestPlanRequiresTwoOrThreeAttestedRuntimeCandidates(t *testing.T) {
	plan := validPlan()
	if err := plan.Validate(); err != nil {
		t.Fatalf("valid plan: %v", err)
	}
	plan.Variants = plan.Variants[:1]
	if err := plan.Validate(); err == nil {
		t.Fatal("expected one variant to be rejected")
	}

	plan = validPlan()
	plan.Variants[0].Runtime.BaseURL = "file:///tmp/not-a-runtime"
	if err := plan.Validate(); err == nil {
		t.Fatal("expected non-HTTP runtime to be rejected")
	}
}

func TestPlanSupportsExactPerCaseTokenEnvironments(t *testing.T) {
	dataset := validDataset()
	plan := validPlan()
	for index := range plan.Variants {
		plan.Variants[index].Runtime.TokenEnv = ""
		plan.Variants[index].Runtime.CaseTokenEnvs = map[string]string{
			"case-001": "AIDA_EVAL_CASE_001_TOKEN",
		}
	}
	if err := plan.ValidateForDataset(dataset); err != nil {
		t.Fatalf("valid per-case credentials: %v", err)
	}

	plan.Variants[0].Runtime.CaseTokenEnvs["case-unknown"] = "AIDA_EVAL_UNKNOWN_TOKEN"
	if err := plan.ValidateForDataset(dataset); err == nil || !strings.Contains(err.Error(), "cover every dataset case") {
		t.Fatalf("expected exact coverage failure, got %v", err)
	}

	plan = validPlan()
	plan.Variants[0].Runtime.CaseTokenEnvs = map[string]string{"case-001": "AIDA_EVAL_CASE_001_TOKEN"}
	if err := plan.Validate(); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected ambiguous credential strategy failure, got %v", err)
	}
}

func TestRuntimeAttestationRejectsDisabledOrProduction(t *testing.T) {
	valid := RuntimeAttestation{
		SchemaVersion: RuntimeAttestationVersion, Enabled: true, Environment: "test",
		BuildRevision: "revision-a", InstanceID: "test-instance-a",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid attestation: %v", err)
	}
	valid.Environment = "production"
	if err := valid.Validate(); err == nil {
		t.Fatal("expected production runtime to be rejected")
	}
}

func TestVariantIdentityRequiresStableUniqueActualManifest(t *testing.T) {
	hashA := strings.Repeat("a", 64)
	hashB := strings.Repeat("b", 64)
	valid := []RunReceipt{
		{RunID: "run-a-1", VariantVersion: "baseline", VariantSHA256: hashA},
		{RunID: "run-a-2", VariantVersion: "baseline", VariantSHA256: hashA},
		{RunID: "run-b-1", VariantVersion: "candidate", VariantSHA256: hashB},
	}
	if err := validateVariantIdentityConsistency(valid); err != nil {
		t.Fatalf("valid variant identities: %v", err)
	}

	drift := append([]RunReceipt(nil), valid...)
	drift[1].VariantSHA256 = strings.Repeat("c", 64)
	if err := validateVariantIdentityConsistency(drift); err == nil || !strings.Contains(err.Error(), "multiple actual manifests") {
		t.Fatalf("expected version drift failure, got %v", err)
	}

	pseudoComparison := append([]RunReceipt(nil), valid...)
	pseudoComparison[2].VariantSHA256 = hashA
	if err := validateVariantIdentityConsistency(pseudoComparison); err == nil || !strings.Contains(err.Error(), "share the same actual manifest") {
		t.Fatalf("expected duplicate actual manifest failure, got %v", err)
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
