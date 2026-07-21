package usage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"

	"github.com/aidashboard/api/internal/biztime"
)

const (
	contributionInitial         = "initial"
	contributionAdvance         = "advance"
	contributionCheckpointDelta = "checkpoint_delta"
)

type contributionHashInput struct {
	RevisionID        string          `json:"revision_id"`
	LogicalEventID    string          `json:"logical_usage_event_id"`
	FromObservationID string          `json:"from_observation_id,omitempty"`
	ToObservationID   string          `json:"to_observation_id"`
	ChunkID           string          `json:"chunk_id"`
	Kind              string          `json:"kind"`
	Provider          string          `json:"provider"`
	Model             string          `json:"model,omitempty"`
	BillingVariant    string          `json:"billing_variant"`
	Normalized        NormalizedUsage `json:"normalized"`
}

func (p *Processor) insertContribution(
	ctx context.Context,
	tx *sql.Tx,
	revisionID, logicalID, fromObservationID, toObservationID string,
	chunk usageChunk,
	record UsageRecord,
	kind string,
) error {
	normalized, err := NormalizeWithOptions(record, NormalizerOptions{
		ClaudeCacheWriteVariant: p.claudeCacheWriteVariant,
	})
	if err != nil {
		return err
	}
	if normalized.TotalTokens == 0 {
		return nil
	}
	memberSessionID := chunk.SessionID
	if record.OwnerSessionID != "" {
		memberSessionID = record.OwnerSessionID
	}

	billingVariant := billingVariantForRecord(record, p.claudeCacheWriteVariant)
	quality := record.Quality
	if normalized.IsEstimated && quality == QualityExact {
		quality = QualityEstimated
	}
	assumptions, _ := json.Marshal(map[string]any{
		"quality_reason":       record.QualityReason,
		"request_input_tokens": record.Delta.RequestInputTokens,
	})
	hashPayload, _ := json.Marshal(contributionHashInput{
		RevisionID:        revisionID,
		LogicalEventID:    logicalID,
		FromObservationID: fromObservationID,
		ToObservationID:   toObservationID,
		ChunkID:           chunk.ID,
		Kind:              kind,
		Provider:          record.Provider,
		Model:             record.RawModel,
		BillingVariant:    billingVariant,
		Normalized:        normalized,
	})
	hash := sha256.Sum256(hashPayload)

	_, err = tx.ExecContext(ctx, `
		INSERT INTO session_usage_contributions (
			revision_id, generation_id, logical_usage_event_id,
			from_observation_id, to_observation_id, contribution_kind,
			member_session_id, user_id, chunk_id, activity_date, occurred_at,
			provider, raw_model, canonical_model, billing_variant,
			uncached_input_tokens, cache_read_tokens, cache_write_5m_tokens,
			cache_write_1h_tokens, output_tokens, total_tokens,
			normalization_strategy, quality_status, is_estimated,
			assumptions_json, contribution_hash
		) VALUES (
			$1, $2, $3, NULLIF($4, '')::uuid, $5, $6,
			$7, $8, $9, $10::date, $11,
			$12, NULLIF($13, ''), $13, $14,
			$15, $16, $17, $18, $19, $20,
			$21, $22, $23, $24::jsonb, $25
		)
		ON CONFLICT (
			revision_id, logical_usage_event_id, to_observation_id,
			canonical_model, billing_variant
		) DO NOTHING`,
		revisionID, chunk.GenerationID, logicalID, fromObservationID, toObservationID, kind,
		memberSessionID, chunk.UserID, chunk.ID, biztime.Date(record.OccurredAt), record.OccurredAt,
		record.Provider, record.RawModel, billingVariant,
		normalized.UncachedInputTokens, normalized.CacheReadTokens,
		normalized.CacheWrite5mTokens, normalized.CacheWrite1hTokens,
		normalized.OutputTokens, normalized.TotalTokens, normalized.Strategy,
		quality, normalized.IsEstimated, assumptions, hex.EncodeToString(hash[:]))
	return err
}

func billingVariantForRecord(record UsageRecord, claudeCacheWriteVariant string) string {
	if record.Provider == "claude_code" && record.Delta.CacheCreationTokens > 0 {
		return claudeCacheWriteVariant
	}
	if record.Provider == "codex" && record.Delta.RequestInputTokens > CodexLongContextInputThreshold {
		return "long_context"
	}
	return "unknown"
}
