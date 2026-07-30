package reporteval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"
)

type VerificationResult struct {
	SchemaVersion string                 `json:"schema_version"`
	Valid         bool                   `json:"valid"`
	DatasetSHA256 string                 `json:"dataset_sha256,omitempty"`
	CaseCount     int                    `json:"case_count"`
	RunCount      int                    `json:"run_count"`
	VariantRuns   map[string]int         `json:"variant_runs"`
	Errors        []string               `json:"errors"`
	Warnings      []string               `json:"warnings"`
	RunMetrics    []DeterministicMetrics `json:"run_metrics"`
}

func VerifyBundle(bundleDir string) (VerificationResult, error) {
	result := VerificationResult{
		SchemaVersion: "daily-report-evaluation-verification/v1",
		VariantRuns:   map[string]int{}, Errors: []string{}, Warnings: []string{},
	}
	var manifest BundleManifest
	if err := decodeJSONFile(filepath.Join(bundleDir, "manifest.json"), &manifest); err != nil {
		return result, err
	}
	if manifest.SchemaVersion != BundleSchemaVersion {
		result.Errors = append(result.Errors, "unsupported bundle schema_version")
	}
	if !isSHA256(manifest.PlanSHA256) || manifest.Repetitions < 1 || manifest.Repetitions > 5 || manifest.CreatedAt.IsZero() {
		result.Errors = append(result.Errors, "bundle plan_sha256, repetitions, or created_at is invalid")
	}
	var plan ExecutionPlan
	if err := decodeStrictJSONFile(filepath.Join(bundleDir, "execution-plan.json"), &plan); err != nil {
		result.Errors = append(result.Errors, "execution-plan.json is missing or invalid: "+err.Error())
	} else if err := plan.Validate(); err != nil {
		result.Errors = append(result.Errors, "execution plan is invalid: "+err.Error())
	} else {
		planHash, _ := CanonicalSHA256(plan)
		if planHash != manifest.PlanSHA256 || plan.Repetitions != manifest.Repetitions {
			result.Errors = append(result.Errors, "execution plan does not match bundle manifest")
		}
	}

	frozen, err := LoadFrozenDataset(filepath.Join(bundleDir, "dataset-manifest.json"))
	if err != nil {
		result.Errors = append(result.Errors, "frozen dataset is invalid: "+err.Error())
	} else {
		result.CaseCount = len(frozen.Manifest.Cases)
		result.DatasetSHA256 = frozen.DatasetSHA256
		if frozen.DatasetSHA256 != manifest.DatasetSHA256 {
			result.Errors = append(result.Errors, "dataset_sha256 does not match frozen dataset")
		}
		if frozen.Manifest.DatasetVersion != manifest.DatasetVersion || frozen.Manifest.RubricVersion != manifest.RubricVersion {
			result.Errors = append(result.Errors, "dataset or rubric version does not match bundle manifest")
		}
	}

	variants := map[string]VariantDescriptor{}
	for _, variant := range manifest.Variants {
		if !safeIdentifierPattern.MatchString(variant.VariantVersion) || variants[variant.VariantVersion].VariantVersion != "" || strings.TrimSpace(variant.AgentID) == "" {
			result.Errors = append(result.Errors, "variant descriptor is invalid or duplicated: "+variant.VariantVersion)
			continue
		}
		variants[variant.VariantVersion] = variant
	}
	if len(variants) < 2 || len(variants) > 3 {
		result.Errors = append(result.Errors, "bundle must contain 2 or 3 variants")
	}
	if len(plan.Variants) > 0 {
		if len(plan.Variants) != len(manifest.Variants) {
			result.Errors = append(result.Errors, "execution plan variants do not match bundle manifest")
		} else {
			for index, variant := range plan.Variants {
				expectedHash, _ := CanonicalSHA256(variant.Descriptor())
				actualHash, _ := CanonicalSHA256(manifest.Variants[index])
				if expectedHash != actualHash {
					result.Errors = append(result.Errors, "execution plan variant descriptor mismatch: "+variant.VariantVersion)
				}
			}
		}
	}
	cases := map[string]EvaluationCase{}
	for _, item := range frozen.Manifest.Cases {
		cases[item.CaseID] = item
		verifyCaseFiles(bundleDir, item, frozen.Sources[item.CaseID], &result)
	}

	coverage := map[string]int{}
	seenRuns := map[string]bool{}
	for _, run := range manifest.Runs {
		result.RunCount++
		result.VariantRuns[run.VariantVersion]++
		key := fmt.Sprintf("%s/%s/%d", run.CaseID, run.VariantVersion, run.Repetition)
		coverage[key]++
		if _, ok := cases[run.CaseID]; !ok {
			result.Errors = append(result.Errors, "run references unknown case: "+key)
		}
		variant, ok := variants[run.VariantVersion]
		if !ok {
			result.Errors = append(result.Errors, "run references unknown variant: "+key)
		}
		if run.Repetition < 1 || run.Repetition > manifest.Repetitions || coverage[key] != 1 {
			result.Errors = append(result.Errors, "run repetition is invalid or duplicated: "+key)
		}
		if _, err := uuid.Parse(run.RunID); err != nil || seenRuns[run.RunID] {
			result.Errors = append(result.Errors, "run_id is invalid or duplicated: "+run.RunID)
			continue
		}
		seenRuns[run.RunID] = true
		if err := run.Runtime.Validate(); err != nil {
			result.Errors = append(result.Errors, run.RunID+": runtime attestation is invalid")
		}
		if !terminalRunStatus(run.Status) || !isSHA256(run.VariantSHA256) {
			result.Errors = append(result.Errors, run.RunID+": status or variant_sha256 is invalid")
		}
		verifyRunFiles(bundleDir, run, variant, &result)
		runDir := filepath.Join(bundleDir, "cases", run.CaseID, "runs", run.RunID)
		result.RunMetrics = append(result.RunMetrics, collectDeterministicMetrics(runDir, run.RunID))
	}
	if err := validateVariantIdentityConsistency(manifest.Runs); err != nil {
		result.Errors = append(result.Errors, "variant identity is inconsistent: "+err.Error())
	}

	expectedRuns := len(cases) * len(variants) * manifest.Repetitions
	if len(manifest.Runs) != expectedRuns {
		result.Errors = append(result.Errors, fmt.Sprintf("bundle has %d runs; expected %d", len(manifest.Runs), expectedRuns))
	}
	for caseID := range cases {
		for variantVersion := range variants {
			for repetition := 1; repetition <= manifest.Repetitions; repetition++ {
				key := fmt.Sprintf("%s/%s/%d", caseID, variantVersion, repetition)
				if coverage[key] != 1 {
					result.Errors = append(result.Errors, "missing or duplicated run: "+key)
				}
			}
		}
	}
	sort.Strings(result.Errors)
	sort.Strings(result.Warnings)
	result.Valid = len(result.Errors) == 0
	return result, nil
}

func verifyCaseFiles(bundleDir string, item EvaluationCase, source SourceEvidence, result *VerificationResult) {
	if err := source.Validate(); err != nil {
		result.Errors = append(result.Errors, item.CaseID+": source evidence is invalid")
	}
	caseDir := filepath.Join(bundleDir, "cases", item.CaseID)
	var baseline EvidenceBaseline
	if err := decodeJSONFile(filepath.Join(caseDir, "evidence-baseline.json"), &baseline); err != nil {
		result.Errors = append(result.Errors, item.CaseID+": evidence-baseline.json is missing or invalid")
		return
	}
	actualHash, _ := CanonicalSHA256(baseline)
	expectedHash, _ := CanonicalSHA256(item.EvidenceBaseline)
	if actualHash != expectedHash {
		result.Errors = append(result.Errors, item.CaseID+": evidence baseline differs from dataset manifest")
	}
	if err := ValidateBaselineSourceRefs(baseline, source, item.ReportDate); err != nil {
		result.Errors = append(result.Errors, item.CaseID+": evidence baseline references invalid source evidence")
	}
}

var knownRunArtifacts = map[string]bool{
	"variant-manifest.json": true, "digest.json": true, "context.json": true,
	"brief.json": true, "generated-draft.md": true, "run-metrics.json": true,
}

func verifyRunFiles(bundleDir string, run RunReceipt, variant VariantDescriptor, result *VerificationResult) {
	runDir := filepath.Join(bundleDir, "cases", run.CaseID, "runs", run.RunID)
	if run.ArtifactSHA256["variant-manifest.json"] == "" || run.ArtifactSHA256["run-metrics.json"] == "" {
		result.Errors = append(result.Errors, run.RunID+": mandatory artifact hashes are missing")
	}
	for name, expected := range run.ArtifactSHA256 {
		if !knownRunArtifacts[name] || !isSHA256(expected) {
			result.Errors = append(result.Errors, run.RunID+": unknown artifact or invalid hash "+name)
			continue
		}
		payload, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			result.Errors = append(result.Errors, run.RunID+": missing artifact "+name)
		} else if CanonicalBytesSHA256(payload) != expected {
			result.Errors = append(result.Errors, run.RunID+": artifact hash mismatch for "+name)
		}
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		result.Errors = append(result.Errors, run.RunID+": run directory is missing")
		return
	}
	for _, entry := range entries {
		if entry.IsDir() || !knownRunArtifacts[entry.Name()] || run.ArtifactSHA256[entry.Name()] == "" {
			result.Errors = append(result.Errors, run.RunID+": untracked artifact "+entry.Name())
		}
	}

	var actualManifest map[string]any
	manifestPath := filepath.Join(runDir, "variant-manifest.json")
	if err := decodeJSONFile(manifestPath, &actualManifest); err != nil {
		result.Errors = append(result.Errors, run.RunID+": variant-manifest.json is missing or invalid")
		return
	}
	actualVariantSHA, _ := CanonicalSHA256(actualManifest)
	if actualVariantSHA != run.VariantSHA256 {
		result.Errors = append(result.Errors, run.RunID+": variant_sha256 does not match manifest")
	}
	if variant.ExpectedVariantSHA256 != "" && variant.ExpectedVariantSHA256 != actualVariantSHA {
		result.Errors = append(result.Errors, run.RunID+": variant does not match execution plan")
	}
	profile, _ := actualManifest["pipeline_profile"].(string)
	if variant.ExpectedPipelineProfile != "" && variant.ExpectedPipelineProfile != profile {
		result.Errors = append(result.Errors, run.RunID+": pipeline profile does not match execution plan")
	}
	modelID, _ := actualManifest["model_id"].(string)
	if variant.ModelID != "" && variant.ModelID != modelID {
		result.Errors = append(result.Errors, run.RunID+": model does not match execution plan")
	}
	if run.Status != "succeeded" {
		return
	}
	stages, ok := actualManifest["stages"].([]any)
	if !ok || len(stages) == 0 {
		result.Errors = append(result.Errors, run.RunID+": succeeded run variant has no stages")
		return
	}
	for _, stageValue := range stages {
		stage, _ := stageValue.(string)
		name := map[string]string{
			"digest": "digest.json", "context": "context.json",
			"brief": "brief.json", "final": "generated-draft.md",
		}[stage]
		if name == "" {
			result.Warnings = append(result.Warnings, run.RunID+": unknown stage "+stage)
		} else if run.ArtifactSHA256[name] == "" {
			result.Errors = append(result.Errors, run.RunID+": succeeded run is missing "+name)
		}
	}
}

func decodeJSONFile(path string, output any) error {
	payload, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(payload, output)
}
