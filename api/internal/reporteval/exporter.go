package reporteval

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const RunArtifactsSchemaVersion = "daily-report-evaluation-run-artifacts/v1"

// RunArtifactEnvelope is the test server's narrow, user-authorized export
// contract. The CLI never receives database credentials.
type RunArtifactEnvelope struct {
	SchemaVersion         string          `json:"schema_version"`
	RunID                 string          `json:"run_id"`
	Status                string          `json:"status"`
	FailureStage          string          `json:"failure_stage,omitempty"`
	ErrorCode             string          `json:"error_code,omitempty"`
	CreatedAt             time.Time       `json:"created_at"`
	StartedAt             *time.Time      `json:"started_at,omitempty"`
	FinishedAt            *time.Time      `json:"finished_at,omitempty"`
	SourceIdentitySHA256  string          `json:"source_identity_set_sha256"`
	VariantManifest       json.RawMessage `json:"variant_manifest"`
	VariantSHA256         string          `json:"variant_sha256"`
	Digest                json.RawMessage `json:"digest,omitempty"`
	Context               json.RawMessage `json:"context,omitempty"`
	Brief                 json.RawMessage `json:"brief,omitempty"`
	GeneratedDraft        string          `json:"generated_draft,omitempty"`
	BriefInvalidAttempts  int             `json:"brief_invalid_attempts"`
	ResultInvalidAttempts int             `json:"result_invalid_attempts"`
}

func (artifacts RunArtifactEnvelope) Validate() error {
	if artifacts.SchemaVersion != RunArtifactsSchemaVersion || strings.TrimSpace(artifacts.RunID) == "" {
		return errors.New("run artifacts schema or run_id is invalid")
	}
	if !terminalRunStatus(artifacts.Status) || artifacts.CreatedAt.IsZero() || !isSHA256(artifacts.SourceIdentitySHA256) {
		return errors.New("run artifacts status, timestamps, or source identity is invalid")
	}
	if len(artifacts.VariantManifest) == 0 || !json.Valid(artifacts.VariantManifest) || !isSHA256(artifacts.VariantSHA256) {
		return errors.New("run artifacts variant manifest is invalid")
	}
	for name, payload := range map[string]json.RawMessage{"digest": artifacts.Digest, "context": artifacts.Context, "brief": artifacts.Brief} {
		if len(payload) > 0 && !json.Valid(payload) {
			return fmt.Errorf("run artifacts %s is invalid JSON", name)
		}
	}
	if artifacts.BriefInvalidAttempts < 0 || artifacts.ResultInvalidAttempts < 0 {
		return errors.New("run artifact attempt counts cannot be negative")
	}
	return nil
}

type Exporter struct {
	OutputDir string
}

func (exporter Exporter) Initialize(dataset FrozenDataset) (string, error) {
	if strings.TrimSpace(exporter.OutputDir) == "" {
		return "", errors.New("output directory is required")
	}
	if err := dataset.Manifest.Validate(); err != nil {
		return "", err
	}
	if len(dataset.Sources) != len(dataset.Manifest.Cases) || len(dataset.PatternPayload) == 0 || !isSHA256(dataset.DatasetSHA256) {
		return "", errors.New("frozen dataset is incomplete")
	}
	if err := os.Mkdir(exporter.OutputDir, 0o750); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("output directory already exists: %s", exporter.OutputDir)
		}
		return "", err
	}
	if _, err := writeJSON(filepath.Join(exporter.OutputDir, "dataset-manifest.json"), dataset.Manifest); err != nil {
		return "", err
	}
	if err := exporter.writeReferencedFile(dataset.Manifest.PatternBaseline.Statistics, dataset.PatternPayload); err != nil {
		return "", err
	}
	for _, item := range dataset.Manifest.Cases {
		source, ok := dataset.Sources[item.CaseID]
		payload := dataset.SourcePayloads[item.CaseID]
		if !ok || len(payload) == 0 || source.SourceIdentitySHA256 == "" {
			return "", fmt.Errorf("case %s frozen source is missing", item.CaseID)
		}
		if err := exporter.writeReferencedFile(item.SourceEvidence, payload); err != nil {
			return "", err
		}
		caseDir := filepath.Join(exporter.OutputDir, "cases", item.CaseID)
		if err := os.MkdirAll(filepath.Join(caseDir, "runs"), 0o750); err != nil {
			return "", err
		}
		if _, err := writeJSON(filepath.Join(caseDir, "evidence-baseline.json"), item.EvidenceBaseline); err != nil {
			return "", err
		}
	}
	return dataset.DatasetSHA256, nil
}

func (exporter Exporter) writeReferencedFile(reference FileReference, payload []byte) error {
	if err := reference.Validate("file_reference"); err != nil {
		return err
	}
	if actual := CanonicalBytesSHA256(payload); actual != reference.SHA256 {
		return fmt.Errorf("referenced file %s hash mismatch", reference.Path)
	}
	destination := filepath.Join(exporter.OutputDir, filepath.FromSlash(reference.Path))
	if err := os.MkdirAll(filepath.Dir(destination), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(destination, payload, 0o640); err != nil {
		return err
	}
	return nil
}

func (exporter Exporter) ExportRun(
	item EvaluationCase,
	frozenSource SourceEvidence,
	variant VariantSpec,
	receipt *RunReceipt,
	artifacts RunArtifactEnvelope,
) error {
	if receipt == nil || receipt.RunID == "" || receipt.RunID != artifacts.RunID {
		return errors.New("run receipt does not match artifacts")
	}
	if err := artifacts.Validate(); err != nil {
		return err
	}
	if err := frozenSource.Validate(); err != nil {
		return err
	}
	if artifacts.SourceIdentitySHA256 != frozenSource.SourceIdentitySHA256 {
		return fmt.Errorf("run source identity %s does not match frozen case source %s", artifacts.SourceIdentitySHA256, frozenSource.SourceIdentitySHA256)
	}
	var manifestValue any
	if err := json.Unmarshal(artifacts.VariantManifest, &manifestValue); err != nil {
		return fmt.Errorf("decode variant manifest: %w", err)
	}
	actualVariantHash, err := CanonicalSHA256(manifestValue)
	if err != nil {
		return err
	}
	if actualVariantHash != artifacts.VariantSHA256 {
		return fmt.Errorf("stored variant hash %s does not match manifest %s", artifacts.VariantSHA256, actualVariantHash)
	}
	if variant.ExpectedVariantSHA256 != "" && variant.ExpectedVariantSHA256 != actualVariantHash {
		return fmt.Errorf("actual variant hash %s does not match expected %s", actualVariantHash, variant.ExpectedVariantSHA256)
	}
	var manifest struct {
		PipelineProfile string `json:"pipeline_profile"`
		ModelID         string `json:"model_id"`
	}
	if err := json.Unmarshal(artifacts.VariantManifest, &manifest); err != nil {
		return fmt.Errorf("decode variant manifest: %w", err)
	}
	if variant.ExpectedPipelineProfile != "" && variant.ExpectedPipelineProfile != manifest.PipelineProfile {
		return fmt.Errorf("actual pipeline profile %s does not match expected %s", manifest.PipelineProfile, variant.ExpectedPipelineProfile)
	}
	if variant.ModelID != "" && variant.ModelID != manifest.ModelID {
		return fmt.Errorf("actual model %s does not match requested %s", manifest.ModelID, variant.ModelID)
	}

	receipt.Status = artifacts.Status
	receipt.FailureStage = artifacts.FailureStage
	receipt.ErrorCode = artifacts.ErrorCode
	receipt.VariantSHA256 = actualVariantHash
	runDir := filepath.Join(exporter.OutputDir, "cases", item.CaseID, "runs", receipt.RunID)
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		return err
	}
	hashes := map[string]string{}
	if hashes["variant-manifest.json"], err = writeJSON(filepath.Join(runDir, "variant-manifest.json"), artifacts.VariantManifest); err != nil {
		return err
	}
	for name, payload := range map[string]json.RawMessage{
		"digest.json": artifacts.Digest, "context.json": artifacts.Context, "brief.json": artifacts.Brief,
	} {
		if len(payload) > 0 && string(payload) != "null" {
			if hashes[name], err = writeJSON(filepath.Join(runDir, name), payload); err != nil {
				return err
			}
		}
	}
	if artifacts.GeneratedDraft != "" {
		if hashes["generated-draft.md"], err = writeFile(filepath.Join(runDir, "generated-draft.md"), []byte(artifacts.GeneratedDraft)); err != nil {
			return err
		}
	}
	metrics := buildMetrics(artifacts)
	if hashes["run-metrics.json"], err = writeJSON(filepath.Join(runDir, "run-metrics.json"), metrics); err != nil {
		return err
	}
	receipt.ArtifactSHA256 = hashes
	return nil
}

func (exporter Exporter) Finalize(dataset FrozenDataset, plan ExecutionPlan, receipts []RunReceipt) error {
	if err := validateVariantIdentityConsistency(receipts); err != nil {
		return fmt.Errorf("variant identity is inconsistent: %w", err)
	}
	variants := make([]VariantDescriptor, 0, len(plan.Variants))
	for _, variant := range plan.Variants {
		variants = append(variants, variant.Descriptor())
	}
	planHash, err := CanonicalSHA256(plan)
	if err != nil {
		return err
	}
	if _, err := writeJSON(filepath.Join(exporter.OutputDir, "execution-plan.json"), plan); err != nil {
		return err
	}
	manifest := BundleManifest{
		SchemaVersion: BundleSchemaVersion, DatasetVersion: dataset.Manifest.DatasetVersion,
		DatasetSHA256: dataset.DatasetSHA256, PlanSHA256: planHash, RubricVersion: dataset.Manifest.RubricVersion,
		Repetitions: plan.Repetitions, CreatedAt: time.Now().UTC(), Variants: variants, Runs: receipts,
	}
	_, err = writeJSON(filepath.Join(exporter.OutputDir, "manifest.json"), manifest)
	return err
}

func buildMetrics(artifacts RunArtifactEnvelope) RunMetrics {
	metrics := RunMetrics{
		SchemaVersion: MetricsSchemaVersion, Status: artifacts.Status,
		FailureStage: artifacts.FailureStage, ErrorCode: artifacts.ErrorCode,
		BriefInvalidAttempts:  artifacts.BriefInvalidAttempts,
		ResultInvalidAttempts: artifacts.ResultInvalidAttempts,
		InputTokens:           unavailable("report run token attribution is not available in V2"),
		OutputTokens:          unavailable("report run token attribution is not available in V2"),
		CostMicrousd:          unavailable("report run cost attribution is not available in V2"),
	}
	if artifacts.FinishedAt != nil {
		start := artifacts.CreatedAt
		if artifacts.StartedAt != nil {
			start = *artifacts.StartedAt
		}
		value := artifacts.FinishedAt.Sub(start).Milliseconds()
		metrics.DurationMS = &value
	}
	return metrics
}

func terminalRunStatus(status string) bool {
	return status == "succeeded" || status == "failed" || status == "timeout"
}

func IsTerminalRunStatus(status string) bool { return terminalRunStatus(status) }

func unavailable(reason string) AvailabilityValue {
	return AvailabilityValue{Status: "not_available", Reason: reason}
}

func writeJSON(path string, value any) (string, error) {
	var payload []byte
	var err error
	if raw, ok := value.(json.RawMessage); ok {
		if !json.Valid(raw) {
			return "", errors.New("invalid JSON artifact")
		}
		var formatted bytes.Buffer
		if err := json.Indent(&formatted, raw, "", "  "); err != nil {
			return "", err
		}
		payload = formatted.Bytes()
	} else {
		payload, err = json.MarshalIndent(value, "", "  ")
	}
	if err != nil {
		return "", err
	}
	payload = append(payload, '\n')
	return writeFile(path, payload)
}

func writeFile(path string, payload []byte) (string, error) {
	if err := os.WriteFile(path, payload, 0o640); err != nil {
		return "", err
	}
	return CanonicalBytesSHA256(payload), nil
}

func CanonicalBytesSHA256(payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%x", sum)
}
