package usage

import "errors"

var ErrClaudeCacheWriteVariantRequired = errors.New("Claude cache creation tokens require a reviewed 5m or 1h variant")

type NormalizerOptions struct {
	ClaudeCacheWriteVariant string
}

type NormalizedUsage struct {
	UncachedInputTokens int64
	CacheReadTokens     int64
	CacheWrite5mTokens  int64
	CacheWrite1hTokens  int64
	OutputTokens        int64
	TotalTokens         int64
	Strategy            string
	IsEstimated         bool
}

func Normalize(record UsageRecord) (NormalizedUsage, error) {
	return NormalizeWithOptions(record, NormalizerOptions{})
}

func NormalizeWithOptions(record UsageRecord, options NormalizerOptions) (NormalizedUsage, error) {
	var result NormalizedUsage
	switch record.Provider {
	case "claude_code":
		result = NormalizedUsage{
			UncachedInputTokens: record.Delta.InputTokens,
			CacheReadTokens:     record.Delta.CachedInputTokens,
			OutputTokens:        record.Delta.OutputTokens,
			Strategy:            "claude_usage_components_v1",
			IsEstimated:         record.Quality != QualityExact,
		}
		if record.Delta.CacheCreationTokens > 0 {
			switch options.ClaudeCacheWriteVariant {
			case "5m":
				result.CacheWrite5mTokens = record.Delta.CacheCreationTokens
			case "1h":
				result.CacheWrite1hTokens = record.Delta.CacheCreationTokens
			default:
				return NormalizedUsage{}, ErrClaudeCacheWriteVariantRequired
			}
			result.IsEstimated = true
		}
	case "codex":
		result = NormalizedUsage{
			UncachedInputTokens: record.Delta.InputTokens - record.Delta.CachedInputTokens,
			CacheReadTokens:     record.Delta.CachedInputTokens,
			OutputTokens:        record.Delta.OutputTokens,
			Strategy:            "codex_cumulative_delta_v1",
			IsEstimated:         record.Quality != QualityExact,
		}
	default:
		return NormalizedUsage{}, errors.New("unsupported provider")
	}
	result.TotalTokens = result.UncachedInputTokens + result.CacheReadTokens + result.CacheWrite5mTokens + result.CacheWrite1hTokens + result.OutputTokens
	if result.UncachedInputTokens < 0 || result.CacheReadTokens < 0 || result.CacheWrite5mTokens < 0 || result.CacheWrite1hTokens < 0 || result.OutputTokens < 0 {
		return NormalizedUsage{}, errors.New("normalized token component is negative")
	}
	if record.Provider == "codex" && result.TotalTokens != record.Delta.TotalTokens {
		return NormalizedUsage{}, errors.New("normalized Codex total does not match provider delta")
	}
	return result, nil
}
