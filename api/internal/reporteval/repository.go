package reporteval

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type RunArtifactRepository struct {
	database *sql.DB
}

func NewRunArtifactRepository(database *sql.DB) (*RunArtifactRepository, error) {
	if database == nil {
		return nil, errors.New("database is required")
	}
	return &RunArtifactRepository{database: database}, nil
}

func (repository *RunArtifactRepository) Load(ctx context.Context, userID, runID string) (RunArtifactEnvelope, error) {
	if repository == nil || repository.database == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(runID) == "" {
		return RunArtifactEnvelope{}, errors.New("repository, user_id, and run_id are required")
	}
	var result RunArtifactEnvelope
	var failureStage, errorCode sql.NullString
	var startedAt, finishedAt sql.NullTime
	var variantPayload, digestPayload, contextPayload, briefPayload []byte
	err := repository.database.QueryRowContext(ctx, `
		SELECT ar.status, ar.failure_stage, ar.error_code, ar.created_at, ar.started_at, ar.finished_at,
			COALESCE(ar.source_identity_set_sha256, ''),
			COALESCE(variant.manifest_json, snapshot.variant_manifest_json, '{}'::jsonb),
			COALESCE(variant.manifest_sha256, snapshot.variant_sha256, ''),
			selection.selection_digest_payload, context.context_payload, brief.brief_payload,
			COALESCE(snapshot.generated_content, ''),
			COALESCE(attempts.brief_invalid_attempts, 0), COALESCE(attempts.result_invalid_attempts, 0)
		FROM ai_runs ar
		LEFT JOIN report_generation_snapshots snapshot ON snapshot.run_id = ar.id
		LEFT JOIN report_run_variant_manifests variant ON variant.run_id = ar.id
		LEFT JOIN report_source_selections selection ON selection.attached_run_id = ar.id
		LEFT JOIN report_run_contexts context ON context.run_id = ar.id
		LEFT JOIN report_run_briefs brief ON brief.run_id = ar.id
		LEFT JOIN report_run_generation_attempts attempts ON attempts.run_id = ar.id
		WHERE ar.id = $1 AND ar.user_id = $2 AND ar.business_type = 'report_agent_run'`, runID, userID).Scan(
		&result.Status, &failureStage, &errorCode, &result.CreatedAt, &startedAt, &finishedAt,
		&result.SourceIdentitySHA256, &variantPayload, &result.VariantSHA256,
		&digestPayload, &contextPayload, &briefPayload, &result.GeneratedDraft,
		&result.BriefInvalidAttempts, &result.ResultInvalidAttempts,
	)
	if err != nil {
		return RunArtifactEnvelope{}, err
	}
	result.SchemaVersion = RunArtifactsSchemaVersion
	result.RunID = runID
	result.FailureStage = failureStage.String
	result.ErrorCode = errorCode.String
	result.StartedAt = nullableTime(startedAt)
	result.FinishedAt = nullableTime(finishedAt)
	for name, input := range map[string][]byte{
		"variant_manifest": variantPayload, "digest": digestPayload, "context": contextPayload, "brief": briefPayload,
	} {
		if len(input) > 0 && !json.Valid(input) {
			return RunArtifactEnvelope{}, fmt.Errorf("stored %s is invalid JSON", name)
		}
	}
	result.VariantManifest = append(json.RawMessage(nil), variantPayload...)
	result.Digest = append(json.RawMessage(nil), digestPayload...)
	result.Context = append(json.RawMessage(nil), contextPayload...)
	result.Brief = append(json.RawMessage(nil), briefPayload...)
	return result, nil
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}
