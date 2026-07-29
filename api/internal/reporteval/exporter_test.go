package reporteval

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestExporterKeepsFailedRunVariantAndOmitsAbsentStages(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	output := filepath.Join(t.TempDir(), "bundle")
	exporter := Exporter{DB: database, OutputDir: output}
	dataset := validDataset()
	if _, err := exporter.Initialize(dataset); err != nil {
		t.Fatal(err)
	}

	manifest := json.RawMessage(`{"schema_version":"report-generation-variant/v2","pipeline_profile":"digest_context_brief_final"}`)
	var manifestValue any
	_ = json.Unmarshal(manifest, &manifestValue)
	manifestHash, _ := CanonicalSHA256(manifestValue)
	now := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	mock.ExpectQuery("(?s)SELECT ar.status.*FROM ai_runs").WithArgs("33333333-3333-4333-8333-333333333333").
		WillReturnRows(sqlmock.NewRows([]string{
			"status", "failure_stage", "error_code", "created_at", "started_at", "finished_at",
			"source_identity_set_sha256", "manifest_json", "manifest_sha256", "context_payload",
			"brief_payload", "generated_content", "brief_invalid_attempts", "result_invalid_attempts",
		}).AddRow("failed", "agent_running", "MODEL_ERROR", now, now, now.Add(time.Second),
			strings.Repeat("a", 64), manifest, manifestHash, nil, nil, "", 0, 0))
	mock.ExpectQuery("SELECT id::text, required_read_mode").WithArgs("33333333-3333-4333-8333-333333333333").
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "required_read_mode", "digest_version_snapshot", "redaction_version_snapshot", "selection_digest_sha256", "selection_digest_payload",
		}).AddRow("44444444-4444-4444-8444-444444444444", "digest_v2", "digest/v2", "redaction/v1", strings.Repeat("b", 64), []byte(`{"work_units":[]}`)))
	mock.ExpectQuery("SELECT session_id::text").WithArgs("44444444-4444-4444-8444-444444444444").
		WillReturnRows(sqlmock.NewRows([]string{
			"session_id", "session_ref_snapshot", "agent_type", "session_content_slice_id", "source_generation_id",
			"content_projection_revision_id", "content_epoch_snapshot", "start_cursor", "end_cursor", "digest_sha256_snapshot", "digest_version_snapshot",
		}).AddRow(
			"55555555-5555-4555-8555-555555555555", "session-ref", "codex",
			"11111111-1111-4111-8111-111111111111", "66666666-6666-4666-8666-666666666666",
			"77777777-7777-4777-8777-777777777777", 1, 0, 10, strings.Repeat("c", 64), "digest/v2",
		))

	receipt := RunReceipt{RunID: "33333333-3333-4333-8333-333333333333"}
	if err := exporter.ExportRun(context.Background(), dataset.Cases[0], VariantSpec{
		VariantVersion: "candidate", AgentID: "default", ExpectedPipelineProfile: "digest_context_brief_final",
	}, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Status != "failed" || receipt.ArtifactSHA256["variant-manifest.json"] == "" {
		t.Fatalf("receipt = %#v", receipt)
	}
	runDir := filepath.Join(output, "cases", "case-001", "runs", receipt.RunID)
	for _, name := range []string{"variant-manifest.json", "digest.json", "run-metrics.json"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	for _, name := range []string{"brief.json", "generated-draft.md"} {
		if _, err := os.Stat(filepath.Join(runDir, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected %s", name)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
