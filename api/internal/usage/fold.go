package usage

type FoldAction string

const (
	FoldKeep      FoldAction = "keep"
	FoldDuplicate FoldAction = "duplicate"
	FoldAdvance   FoldAction = "advance"
	FoldConflict  FoldAction = "conflict"
)

type FoldResult struct {
	Action FoldAction
	Reason string
}

func FoldClaudeObservation(current, next UsageRecord) FoldResult {
	if current.EventKey == "" {
		return FoldResult{Action: FoldAdvance, Reason: "first observation"}
	}
	if current.EventKey != next.EventKey || current.Provider != "claude_code" || next.Provider != "claude_code" {
		return FoldResult{Action: FoldConflict, Reason: "logical event identity changed"}
	}
	if current.RawUsageHash == next.RawUsageHash {
		return FoldResult{Action: FoldDuplicate, Reason: "same raw usage hash"}
	}
	if current.RawModel != next.RawModel {
		return FoldResult{Action: FoldConflict, Reason: "logical event model diverged"}
	}
	comparison := compareCounters(current.Counters, next.Counters)
	switch comparison {
	case 0:
		return FoldResult{Action: FoldDuplicate, Reason: "same usage vector"}
	case 1:
		return FoldResult{Action: FoldAdvance, Reason: "component-wise monotonic usage snapshot"}
	default:
		return FoldResult{Action: FoldConflict, Reason: "usage snapshot is not component-wise monotonic"}
	}
}

// compareCounters returns 0 for equal, 1 when next is a monotonic advance, and -1 otherwise.
func compareCounters(current, next TokenCounters) int {
	before := []int64{current.InputTokens, current.CachedInputTokens, current.CacheCreationTokens, current.OutputTokens}
	after := []int64{next.InputTokens, next.CachedInputTokens, next.CacheCreationTokens, next.OutputTokens}
	advanced := false
	for index := range before {
		if after[index] < before[index] {
			return -1
		}
		if after[index] > before[index] {
			advanced = true
		}
	}
	if advanced {
		return 1
	}
	return 0
}
