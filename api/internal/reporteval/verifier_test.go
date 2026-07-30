package reporteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyBundleRejectsMissingVariantCoverage(t *testing.T) {
	dataset, err := LoadFrozenDataset(writeFrozenDatasetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(t.TempDir(), "bundle")
	exporter := Exporter{OutputDir: directory}
	if _, err := exporter.Initialize(dataset); err != nil {
		t.Fatal(err)
	}
	if err := exporter.Finalize(dataset, validPlan(), nil); err != nil {
		t.Fatal(err)
	}
	result, err := VerifyBundle(directory)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || !containsString(result.Errors, "bundle has 0 runs; expected 2") {
		t.Fatalf("verification = %#v", result)
	}
}

func TestVerifyRunFilesRejectsActualModelDifferentFromPlan(t *testing.T) {
	directory := t.TempDir()
	runID := "33333333-3333-4333-8333-333333333333"
	runDirectory := filepath.Join(directory, "cases", "case-001", "runs", runID)
	if err := os.MkdirAll(runDirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	manifest := json.RawMessage(`{"pipeline_profile":"digest_context_brief_final","model_id":"deepseek-v4-flash"}`)
	var value any
	_ = json.Unmarshal(manifest, &value)
	variantHash, _ := CanonicalSHA256(value)
	manifestHash, err := writeJSON(filepath.Join(runDirectory, "variant-manifest.json"), manifest)
	if err != nil {
		t.Fatal(err)
	}
	metricsHash, err := writeJSON(filepath.Join(runDirectory, "run-metrics.json"), map[string]any{"status": "failed"})
	if err != nil {
		t.Fatal(err)
	}
	run := RunReceipt{
		CaseID: "case-001", RunID: runID, Status: "failed", VariantSHA256: variantHash,
		ArtifactSHA256: map[string]string{
			"variant-manifest.json": manifestHash,
			"run-metrics.json":      metricsHash,
		},
	}
	result := VerificationResult{Errors: []string{}, Warnings: []string{}}
	verifyRunFiles(directory, run, VariantDescriptor{ModelID: "MiniMax-M3"}, &result)
	if !containsString(result.Errors, "model does not match execution plan") {
		t.Fatalf("expected model identity failure, got %#v", result.Errors)
	}
}

func TestVerifyBundleRejectsVariantIdentityDrift(t *testing.T) {
	directory := writeScorecardBundle(t, "1. 基线", "1. 候选")
	var manifest BundleManifest
	manifestPath := filepath.Join(directory, "manifest.json")
	if err := decodeJSONFile(manifestPath, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Runs[1].VariantSHA256 = manifest.Runs[0].VariantSHA256
	if _, err := writeJSON(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	result, err := VerifyBundle(directory)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || !containsString(result.Errors, "share the same actual manifest") {
		t.Fatalf("expected variant identity failure, got %#v", result.Errors)
	}
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if strings.Contains(value, expected) {
			return true
		}
	}
	return false
}
