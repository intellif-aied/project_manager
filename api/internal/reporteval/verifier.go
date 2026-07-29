package reporteval

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
	var dataset DatasetManifest
	if err := decodeJSONFile(filepath.Join(bundleDir, "dataset-manifest.json"), &dataset); err != nil {
		result.Errors = append(result.Errors, "dataset-manifest.json is missing or invalid: "+err.Error())
	} else {
		NormalizeDataset(&dataset)
		if err := dataset.Validate(); err != nil {
			result.Errors = append(result.Errors, "dataset is invalid: "+err.Error())
		} else {
			result.CaseCount = len(dataset.Cases)
			hash, _ := CanonicalSHA256(dataset)
			result.DatasetSHA256 = hash
			if hash != manifest.DatasetSHA256 {
				result.Errors = append(result.Errors, "dataset_sha256 does not match dataset-manifest.json")
			}
		}
	}
	variants := map[string]VariantSpec{}
	for _, variant := range manifest.Variants {
		if !safeIdentifierPattern.MatchString(variant.VariantVersion) || variants[variant.VariantVersion].VariantVersion != "" {
			result.Errors = append(result.Errors, "variant_version is invalid or duplicated: "+variant.VariantVersion)
			continue
		}
		variants[variant.VariantVersion] = variant
	}
	if len(variants) < 2 || len(variants) > 3 {
		result.Errors = append(result.Errors, "bundle must contain 2 or 3 variants")
	}
	cases := map[string]EvaluationCase{}
	for _, item := range dataset.Cases {
		cases[item.CaseID] = item
		verifyCaseFiles(bundleDir, item, &result)
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
		if _, err := uuid.Parse(run.RunID); err != nil || seenRuns[run.RunID] {
			result.Errors = append(result.Errors, "run_id is invalid or duplicated: "+run.RunID)
			continue
		}
		seenRuns[run.RunID] = true
		verifyRunFiles(bundleDir, run, variant, &result)
		runDir := filepath.Join(bundleDir, "cases", run.CaseID, "runs", run.RunID)
		result.RunMetrics = append(result.RunMetrics, collectDeterministicMetrics(runDir, run.RunID))
	}
	for _, item := range dataset.Cases {
		for _, variant := range manifest.Variants {
			found := false
			for key, count := range coverage {
				prefix := item.CaseID + "/" + variant.VariantVersion + "/"
				if len(key) >= len(prefix) && key[:len(prefix)] == prefix && count == 1 {
					found = true
				}
			}
			if !found {
				result.Errors = append(result.Errors, fmt.Sprintf("missing run for %s/%s", item.CaseID, variant.VariantVersion))
			}
		}
	}
	sort.Strings(result.Errors)
	sort.Strings(result.Warnings)
	result.Valid = len(result.Errors) == 0
	return result, nil
}

func verifyCaseFiles(bundleDir string, item EvaluationCase, result *VerificationResult) {
	caseDir := filepath.Join(bundleDir, "cases", item.CaseID)
	var source SourceEvidence
	if err := decodeJSONFile(filepath.Join(caseDir, "source-evidence.json"), &source); err != nil {
		result.Errors = append(result.Errors, item.CaseID+": source-evidence.json is missing or invalid")
	} else {
		if source.SchemaVersion != SourceSchemaVersion || source.SourceIdentitySHA256 == "" {
			result.Errors = append(result.Errors, item.CaseID+": source evidence identity is invalid")
		}
		if expected := item.ExpectedSourceIdentitySHA256; expected != "" && expected != source.SourceIdentitySHA256 {
			result.Errors = append(result.Errors, item.CaseID+": source identity differs from dataset")
		}
	}
	var baseline map[string]any
	if err := decodeJSONFile(filepath.Join(caseDir, "evidence-baseline.json"), &baseline); err != nil {
		result.Errors = append(result.Errors, item.CaseID+": evidence-baseline.json is missing or invalid")
	}
}

func verifyRunFiles(bundleDir string, run RunReceipt, variant VariantSpec, result *VerificationResult) {
	runDir := filepath.Join(bundleDir, "cases", run.CaseID, "runs", run.RunID)
	for name, expected := range run.ArtifactSHA256 {
		payload, err := os.ReadFile(filepath.Join(runDir, name))
		if err != nil {
			result.Errors = append(result.Errors, run.RunID+": missing artifact "+name)
		} else if CanonicalBytesSHA256(payload) != expected {
			result.Errors = append(result.Errors, run.RunID+": artifact hash mismatch for "+name)
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
	if run.Status != "succeeded" {
		return
	}
	stages, _ := actualManifest["stages"].([]any)
	for _, stageValue := range stages {
		stage, _ := stageValue.(string)
		name := map[string]string{
			"digest": "digest.json", "context": "context.json",
			"brief": "brief.json", "final": "generated-draft.md",
		}[stage]
		if name == "" {
			result.Warnings = append(result.Warnings, run.RunID+": unknown stage "+stage)
		} else if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
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
