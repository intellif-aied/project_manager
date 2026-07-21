package canonical

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type CounterMode string

const (
	CounterDelta CounterMode = "delta"
)

type Quality string

const (
	QualityExact      Quality = "exact"
	QualityEstimated  Quality = "estimated"
	QualityIncomplete Quality = "incomplete"
	QualityConflict   Quality = "conflict"
)

type Usage struct {
	FactID                     string      `json:"usage_fact_id"`
	OwnerSessionRef            string      `json:"owner_session_ref"`
	IdentityStrategy           string      `json:"identity_strategy"`
	OccurredAt                 time.Time   `json:"occurred_at"`
	Model                      string      `json:"model"`
	CounterMode                CounterMode `json:"counter_mode"`
	UncachedInputTokens        int64       `json:"uncached_input_tokens"`
	CacheReadInputTokens       int64       `json:"cache_read_input_tokens"`
	CacheCreation5mInputTokens int64       `json:"cache_creation_5m_input_tokens"`
	CacheCreation1hInputTokens int64       `json:"cache_creation_1h_input_tokens"`
	OutputTokens               int64       `json:"output_tokens"`
	ReasoningOutputTokens      int64       `json:"reasoning_output_tokens"`
	TotalTokens                int64       `json:"total_tokens"`
	Quality                    Quality     `json:"quality"`
}

func ValidateUsage(usage Usage) error {
	if usage.CounterMode != CounterDelta {
		return errors.New("canonical usage must contain per-fact delta counters")
	}
	if usage.OccurredAt.IsZero() || strings.TrimSpace(usage.Model) == "" {
		return errors.New("occurred_at and model are required")
	}
	if usage.Quality != QualityExact && usage.Quality != QualityEstimated &&
		usage.Quality != QualityIncomplete && usage.Quality != QualityConflict {
		return errors.New("unsupported usage quality")
	}
	if usage.Quality == QualityExact &&
		(strings.TrimSpace(usage.FactID) == "" || strings.TrimSpace(usage.OwnerSessionRef) == "" || strings.TrimSpace(usage.IdentityStrategy) == "") {
		return errors.New("exact usage requires fact identity, owner session, and identity strategy")
	}
	values := []int64{
		usage.UncachedInputTokens,
		usage.CacheReadInputTokens,
		usage.CacheCreation5mInputTokens,
		usage.CacheCreation1hInputTokens,
		usage.OutputTokens,
		usage.ReasoningOutputTokens,
		usage.TotalTokens,
	}
	for _, value := range values {
		if value < 0 {
			return errors.New("usage token values cannot be negative")
		}
	}
	if usage.ReasoningOutputTokens > usage.OutputTokens {
		return errors.New("reasoning output tokens cannot exceed output tokens")
	}
	wantTotal := usage.UncachedInputTokens + usage.CacheReadInputTokens +
		usage.CacheCreation5mInputTokens + usage.CacheCreation1hInputTokens + usage.OutputTokens
	if usage.TotalTokens != wantTotal {
		return fmt.Errorf("total tokens %d do not match components %d", usage.TotalTokens, wantTotal)
	}
	return nil
}
