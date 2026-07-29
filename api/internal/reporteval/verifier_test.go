package reporteval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestVerifyBundleRejectsMissingVariantCoverage(t *testing.T) {
	directory := t.TempDir()
	dataset := validDataset()
	NormalizeDataset(&dataset)
	datasetHash, _ := CanonicalSHA256(dataset)
	if _, err := writeJSON(filepath.Join(directory, "dataset-manifest.json"), dataset); err != nil {
		t.Fatal(err)
	}
	manifest := BundleManifest{
		SchemaVersion: BundleSchemaVersion, DatasetVersion: dataset.DatasetVersion,
		DatasetSHA256: datasetHash, RubricVersion: dataset.RubricVersion, CreatedAt: time.Now().UTC(),
		Variants: validPlan().Variants, Runs: []RunReceipt{},
	}
	if _, err := writeJSON(filepath.Join(directory, "manifest.json"), manifest); err != nil {
		t.Fatal(err)
	}
	caseDir := filepath.Join(directory, "cases", dataset.Cases[0].CaseID)
	if err := os.MkdirAll(caseDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := writeJSON(filepath.Join(caseDir, "source-evidence.json"), SourceEvidence{
		SchemaVersion: SourceSchemaVersion, SourceIdentitySHA256: strings.Repeat("a", 64),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := writeJSON(filepath.Join(caseDir, "evidence-baseline.json"), dataset.Cases[0].EvidenceBaseline); err != nil {
		t.Fatal(err)
	}
	result, err := VerifyBundle(directory)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid || len(result.Errors) != 2 {
		t.Fatalf("verification = %#v", result)
	}
}
