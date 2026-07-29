package reporteval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Exporter struct {
	DB        *sql.DB
	OutputDir string
}

type runArtifacts struct {
	Status                string
	FailureStage          string
	ErrorCode             string
	CreatedAt             time.Time
	StartedAt             sql.NullTime
	FinishedAt            sql.NullTime
	SourceIdentitySHA256  string
	VariantManifest       json.RawMessage
	VariantSHA256         string
	Context               json.RawMessage
	Brief                 json.RawMessage
	GeneratedDraft        string
	BriefInvalidAttempts  int
	ResultInvalidAttempts int
}

func (exporter Exporter) Initialize(dataset DatasetManifest) (string, error) {
	if exporter.DB == nil || strings.TrimSpace(exporter.OutputDir) == "" {
		return "", errors.New("database and output directory are required")
	}
	NormalizeDataset(&dataset)
	if err := dataset.Validate(); err != nil {
		return "", err
	}
	datasetHash, err := CanonicalSHA256(dataset)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(exporter.OutputDir, 0o750); err != nil {
		return "", err
	}
	if _, err := writeJSON(filepath.Join(exporter.OutputDir, "dataset-manifest.json"), dataset); err != nil {
		return "", err
	}
	for _, item := range dataset.Cases {
		caseDir := filepath.Join(exporter.OutputDir, "cases", item.CaseID)
		if err := os.MkdirAll(filepath.Join(caseDir, "runs"), 0o750); err != nil {
			return "", err
		}
		if _, err := writeJSON(filepath.Join(caseDir, "evidence-baseline.json"), item.EvidenceBaseline); err != nil {
			return "", err
		}
	}
	return datasetHash, nil
}

func (exporter Exporter) ExportRun(
	ctx context.Context,
	item EvaluationCase,
	variant VariantSpec,
	receipt *RunReceipt,
) error {
	artifacts, err := exporter.loadRunArtifacts(ctx, receipt.RunID)
	if err != nil {
		return err
	}
	receipt.Status = artifacts.Status
	receipt.FailureStage = artifacts.FailureStage
	receipt.ErrorCode = artifacts.ErrorCode
	receipt.VariantSHA256 = artifacts.VariantSHA256
	if len(artifacts.VariantManifest) == 0 {
		return errors.New("actual variant manifest is missing")
	}
	if variant.ExpectedVariantSHA256 != "" && variant.ExpectedVariantSHA256 != artifacts.VariantSHA256 {
		return fmt.Errorf("actual variant hash %s does not match expected %s", artifacts.VariantSHA256, variant.ExpectedVariantSHA256)
	}
	var manifest struct {
		PipelineProfile string `json:"pipeline_profile"`
	}
	if err := json.Unmarshal(artifacts.VariantManifest, &manifest); err != nil {
		return fmt.Errorf("decode variant manifest: %w", err)
	}
	if variant.ExpectedPipelineProfile != "" && variant.ExpectedPipelineProfile != manifest.PipelineProfile {
		return fmt.Errorf("actual pipeline profile %s does not match expected %s", manifest.PipelineProfile, variant.ExpectedPipelineProfile)
	}

	source, err := exporter.loadSourceEvidence(ctx, receipt.RunID, artifacts.SourceIdentitySHA256)
	if err != nil {
		return err
	}
	if expected := item.ExpectedSourceIdentitySHA256; expected != "" && expected != source.SourceIdentitySHA256 {
		return fmt.Errorf("actual source identity %s does not match case expectation %s", source.SourceIdentitySHA256, expected)
	}
	caseDir := filepath.Join(exporter.OutputDir, "cases", item.CaseID)
	sourcePath := filepath.Join(caseDir, "source-evidence.json")
	if existing, readErr := os.ReadFile(sourcePath); readErr == nil {
		var frozen SourceEvidence
		if err := json.Unmarshal(existing, &frozen); err != nil {
			return fmt.Errorf("decode frozen source evidence: %w", err)
		}
		frozen.SelectionID = ""
		comparableSource := source
		comparableSource.SelectionID = ""
		frozenHash, _ := CanonicalSHA256(frozen)
		actualHash, _ := CanonicalSHA256(comparableSource)
		if frozenHash != actualHash {
			return fmt.Errorf("source evidence changed within case %s", item.CaseID)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	} else if _, err := writeJSON(sourcePath, source); err != nil {
		return err
	}

	runDir := filepath.Join(caseDir, "runs", receipt.RunID)
	if err := os.MkdirAll(runDir, 0o750); err != nil {
		return err
	}
	hashes := map[string]string{}
	if hashes["variant-manifest.json"], err = writeJSON(filepath.Join(runDir, "variant-manifest.json"), artifacts.VariantManifest); err != nil {
		return err
	}
	if len(source.DigestPayload) > 0 && string(source.DigestPayload) != "null" {
		if hashes["digest.json"], err = writeJSON(filepath.Join(runDir, "digest.json"), source.DigestPayload); err != nil {
			return err
		}
	}
	if len(artifacts.Context) > 0 {
		if hashes["context.json"], err = writeJSON(filepath.Join(runDir, "context.json"), artifacts.Context); err != nil {
			return err
		}
	}
	if len(artifacts.Brief) > 0 {
		if hashes["brief.json"], err = writeJSON(filepath.Join(runDir, "brief.json"), artifacts.Brief); err != nil {
			return err
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

func (exporter Exporter) Finalize(dataset DatasetManifest, plan ExecutionPlan, datasetHash string, receipts []RunReceipt) error {
	manifest := BundleManifest{
		SchemaVersion: BundleSchemaVersion, DatasetVersion: dataset.DatasetVersion,
		DatasetSHA256: datasetHash, RubricVersion: dataset.RubricVersion,
		CreatedAt: time.Now().UTC(), Variants: plan.Variants, Runs: receipts,
	}
	_, err := writeJSON(filepath.Join(exporter.OutputDir, "manifest.json"), manifest)
	return err
}

func (exporter Exporter) loadRunArtifacts(ctx context.Context, runID string) (runArtifacts, error) {
	var result runArtifacts
	var failureStage, errorCode sql.NullString
	var contextPayload, briefPayload, variantPayload []byte
	err := exporter.DB.QueryRowContext(ctx, `
		SELECT ar.status, ar.failure_stage, ar.error_code, ar.created_at, ar.started_at, ar.finished_at,
			COALESCE(ar.source_identity_set_sha256, ''),
			COALESCE(variant.manifest_json, snapshot.variant_manifest_json, '{}'::jsonb),
			COALESCE(variant.manifest_sha256, snapshot.variant_sha256, ''),
			context.context_payload, brief.brief_payload,
			COALESCE(snapshot.generated_content, ''),
			COALESCE(attempts.brief_invalid_attempts, 0), COALESCE(attempts.result_invalid_attempts, 0)
		FROM ai_runs ar
		LEFT JOIN report_generation_snapshots snapshot ON snapshot.run_id = ar.id
		LEFT JOIN report_run_variant_manifests variant ON variant.run_id = ar.id
		LEFT JOIN report_run_contexts context ON context.run_id = ar.id
		LEFT JOIN report_run_briefs brief ON brief.run_id = ar.id
		LEFT JOIN report_run_generation_attempts attempts ON attempts.run_id = ar.id
		WHERE ar.id = $1 AND ar.business_type = 'report_agent_run'`, runID).Scan(
		&result.Status, &failureStage, &errorCode, &result.CreatedAt, &result.StartedAt, &result.FinishedAt,
		&result.SourceIdentitySHA256, &variantPayload, &result.VariantSHA256,
		&contextPayload, &briefPayload, &result.GeneratedDraft,
		&result.BriefInvalidAttempts, &result.ResultInvalidAttempts,
	)
	if err != nil {
		return runArtifacts{}, err
	}
	result.FailureStage = failureStage.String
	result.ErrorCode = errorCode.String
	if string(variantPayload) != "{}" {
		result.VariantManifest = append(json.RawMessage(nil), variantPayload...)
	}
	result.Context = append(json.RawMessage(nil), contextPayload...)
	result.Brief = append(json.RawMessage(nil), briefPayload...)
	return result, nil
}

func (exporter Exporter) loadSourceEvidence(ctx context.Context, runID, identityHash string) (SourceEvidence, error) {
	var source SourceEvidence
	var digestPayload []byte
	err := exporter.DB.QueryRowContext(ctx, `
		SELECT id::text, required_read_mode, COALESCE(digest_version_snapshot, ''),
			COALESCE(redaction_version_snapshot, ''), COALESCE(selection_digest_sha256, ''),
			selection_digest_payload
		FROM report_source_selections WHERE attached_run_id = $1`, runID).Scan(
		&source.SelectionID, &source.ReadMode, &source.DigestVersion, &source.RedactionVersion,
		&source.DigestSHA256, &digestPayload,
	)
	if err != nil {
		return SourceEvidence{}, err
	}
	source.SchemaVersion = SourceSchemaVersion
	source.SourceIdentitySHA256 = identityHash
	if len(digestPayload) > 0 {
		if !json.Valid(digestPayload) {
			return SourceEvidence{}, errors.New("selection digest payload is not valid JSON")
		}
		source.DigestPayload = append(json.RawMessage(nil), digestPayload...)
	}
	rows, err := exporter.DB.QueryContext(ctx, `
		SELECT session_id::text, session_ref_snapshot, agent_type,
			COALESCE(session_content_slice_id::text, ''), source_generation_id::text,
			content_projection_revision_id::text, content_epoch_snapshot, start_cursor, end_cursor,
			COALESCE(digest_sha256_snapshot, ''), COALESCE(digest_version_snapshot, '')
		FROM report_source_selection_items WHERE selection_id = $1
		ORDER BY session_id, start_cursor, end_cursor`, source.SelectionID)
	if err != nil {
		return SourceEvidence{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var item SourceEvidenceItem
		if err := rows.Scan(
			&item.SessionID, &item.SessionRef, &item.AgentType, &item.SessionContentSliceID,
			&item.SourceGenerationID, &item.ProjectionRevisionID, &item.ContentEpoch,
			&item.StartCursor, &item.EndCursor, &item.DigestSHA256, &item.DigestVersion,
		); err != nil {
			return SourceEvidence{}, err
		}
		source.Items = append(source.Items, item)
	}
	return source, rows.Err()
}

func buildMetrics(artifacts runArtifacts) RunMetrics {
	metrics := RunMetrics{
		SchemaVersion: MetricsSchemaVersion, Status: artifacts.Status,
		FailureStage: artifacts.FailureStage, ErrorCode: artifacts.ErrorCode,
		BriefInvalidAttempts:  artifacts.BriefInvalidAttempts,
		ResultInvalidAttempts: artifacts.ResultInvalidAttempts,
		InputTokens:           unavailable("report run token attribution is not available in V2 E-01"),
		OutputTokens:          unavailable("report run token attribution is not available in V2 E-01"),
		CostMicrousd:          unavailable("report run cost attribution is not available in V2 E-01"),
	}
	if artifacts.FinishedAt.Valid {
		start := artifacts.CreatedAt
		if artifacts.StartedAt.Valid {
			start = artifacts.StartedAt.Time
		}
		value := artifacts.FinishedAt.Time.Sub(start).Milliseconds()
		metrics.DurationMS = &value
	}
	return metrics
}

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
	sum := sha256Bytes(payload)
	return fmt.Sprintf("%x", sum)
}

func sha256Bytes(payload []byte) [32]byte {
	return sha256.Sum256(payload)
}
