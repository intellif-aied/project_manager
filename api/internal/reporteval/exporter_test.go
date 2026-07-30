package reporteval

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExporterUsesFrozenInputAndServerArtifactsWithoutDatabase(t *testing.T) {
	dataset, err := LoadFrozenDataset(writeFrozenDatasetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "bundle")
	exporter := Exporter{OutputDir: output}
	if _, err := exporter.Initialize(dataset); err != nil {
		t.Fatal(err)
	}

	manifest := json.RawMessage(`{"schema_version":"report-generation-variant/v2","pipeline_profile":"digest_context_brief_final","stages":["digest","context","brief","final"]}`)
	var manifestValue any
	_ = json.Unmarshal(manifest, &manifestValue)
	manifestHash, _ := CanonicalSHA256(manifestValue)
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	finished := now.Add(time.Second)
	runID := "33333333-3333-4333-8333-333333333333"
	artifacts := RunArtifactEnvelope{
		SchemaVersion: RunArtifactsSchemaVersion, RunID: runID, Status: "succeeded",
		CreatedAt: now, StartedAt: &now, FinishedAt: &finished,
		SourceIdentitySHA256: dataset.Sources["case-001"].SourceIdentitySHA256,
		VariantManifest:      manifest, VariantSHA256: manifestHash,
		Digest: json.RawMessage(`{"work_units":[]}`), Context: json.RawMessage(`{"schema_version":"report-context/v1"}`),
		Brief: json.RawMessage(`{"schema_version":"report-brief/v1"}`), GeneratedDraft: "1. 完成协议设计\n",
	}
	receipt := RunReceipt{RunID: runID}
	variant := VariantSpec{VariantVersion: "candidate", AgentID: "default", ExpectedPipelineProfile: "digest_context_brief_final"}
	if err := exporter.ExportRun(dataset.Manifest.Cases[0], dataset.Sources["case-001"], variant, &receipt, artifacts); err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "succeeded" || receipt.ArtifactSHA256["variant-manifest.json"] == "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	for _, name := range []string{"variant-manifest.json", "digest.json", "context.json", "brief.json", "generated-draft.md", "run-metrics.json"} {
		if _, err := os.Stat(filepath.Join(output, "cases", "case-001", "runs", runID, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(output, dataset.Manifest.Cases[0].SourceEvidence.Path)); err != nil {
		t.Fatalf("frozen source was not copied: %v", err)
	}
}

func TestExporterRejectsRunFromDifferentSource(t *testing.T) {
	dataset, err := LoadFrozenDataset(writeFrozenDatasetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	exporter := Exporter{OutputDir: filepath.Join(t.TempDir(), "bundle")}
	if _, err := exporter.Initialize(dataset); err != nil {
		t.Fatal(err)
	}
	manifest := json.RawMessage(`{"pipeline_profile":"digest_context_brief_final"}`)
	var value any
	_ = json.Unmarshal(manifest, &value)
	hash, _ := CanonicalSHA256(value)
	now := time.Now().UTC()
	artifacts := RunArtifactEnvelope{
		SchemaVersion: RunArtifactsSchemaVersion, RunID: "33333333-3333-4333-8333-333333333333", Status: "failed", CreatedAt: now,
		SourceIdentitySHA256: strings.Repeat("d", 64), VariantManifest: manifest, VariantSHA256: hash,
	}
	receipt := RunReceipt{RunID: artifacts.RunID}
	err = exporter.ExportRun(dataset.Manifest.Cases[0], dataset.Sources["case-001"], validPlan().Variants[0], &receipt, artifacts)
	if err == nil || !strings.Contains(err.Error(), "does not match frozen") {
		t.Fatalf("expected source identity failure, got %v", err)
	}
}

func TestExporterRejectsActualModelDifferentFromPlan(t *testing.T) {
	dataset, err := LoadFrozenDataset(writeFrozenDatasetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	exporter := Exporter{OutputDir: filepath.Join(t.TempDir(), "bundle")}
	if _, err := exporter.Initialize(dataset); err != nil {
		t.Fatal(err)
	}
	manifest := json.RawMessage(`{"pipeline_profile":"digest_context_brief_final","model_id":"deepseek-v4-flash"}`)
	var value any
	_ = json.Unmarshal(manifest, &value)
	hash, _ := CanonicalSHA256(value)
	artifacts := RunArtifactEnvelope{
		SchemaVersion: RunArtifactsSchemaVersion, RunID: "33333333-3333-4333-8333-333333333333",
		Status: "failed", CreatedAt: time.Now().UTC(), SourceIdentitySHA256: dataset.Sources["case-001"].SourceIdentitySHA256,
		VariantManifest: manifest, VariantSHA256: hash,
	}
	receipt := RunReceipt{RunID: artifacts.RunID}
	variant := VariantSpec{VariantVersion: "candidate", AgentID: "default", ModelID: "MiniMax-M3"}
	err = exporter.ExportRun(dataset.Manifest.Cases[0], dataset.Sources["case-001"], variant, &receipt, artifacts)
	if err == nil || !strings.Contains(err.Error(), "does not match requested") {
		t.Fatalf("expected model identity failure, got %v", err)
	}
}

func TestExporterFinalizeRejectsPseudoVariantComparison(t *testing.T) {
	dataset, err := LoadFrozenDataset(writeFrozenDatasetFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	exporter := Exporter{OutputDir: filepath.Join(t.TempDir(), "bundle")}
	if _, err := exporter.Initialize(dataset); err != nil {
		t.Fatal(err)
	}
	sharedHash := strings.Repeat("a", 64)
	receipts := []RunReceipt{
		{RunID: "11111111-1111-4111-8111-111111111111", VariantVersion: "baseline", VariantSHA256: sharedHash},
		{RunID: "22222222-2222-4222-8222-222222222222", VariantVersion: "candidate", VariantSHA256: sharedHash},
	}
	if err := exporter.Finalize(dataset, validPlan(), receipts); err == nil || !strings.Contains(err.Error(), "share the same actual manifest") {
		t.Fatalf("expected pseudo comparison failure, got %v", err)
	}
}
