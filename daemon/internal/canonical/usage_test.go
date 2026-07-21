package canonical_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/aidashboard/daemon/internal/canonical"
)

func TestUsageJSONUsesCanonicalFieldNames(t *testing.T) {
	content, err := json.Marshal(canonical.Usage{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(content, &fields); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"usage_fact_id", "owner_session_ref", "identity_strategy", "occurred_at", "model", "counter_mode",
		"uncached_input_tokens", "cache_read_input_tokens", "cache_creation_5m_input_tokens",
		"cache_creation_1h_input_tokens", "output_tokens", "reasoning_output_tokens", "total_tokens", "quality",
	} {
		if _, ok := fields[name]; !ok {
			t.Fatalf("canonical field %q missing from %s", name, content)
		}
	}
	if _, ok := fields["FactID"]; ok {
		t.Fatalf("Go field name leaked into canonical JSON: %s", content)
	}
}

func TestValidateExactUsageRequiresStableBalancedFact(t *testing.T) {
	valid := canonical.Usage{
		FactID: "request:req-1", OwnerSessionRef: "session-parent", IdentityStrategy: "provider_request_id",
		OccurredAt: time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC), Model: "model-v1", CounterMode: canonical.CounterDelta,
		UncachedInputTokens: 100, CacheReadInputTokens: 20, CacheCreation5mInputTokens: 10,
		CacheCreation1hInputTokens: 5, OutputTokens: 30, ReasoningOutputTokens: 4, TotalTokens: 165,
		Quality: canonical.QualityExact,
	}
	if err := canonical.ValidateUsage(valid); err != nil {
		t.Fatalf("valid exact usage: %v", err)
	}

	for name, mutate := range map[string]func(*canonical.Usage){
		"missing fact identity": func(usage *canonical.Usage) { usage.FactID = "" },
		"missing owner":         func(usage *canonical.Usage) { usage.OwnerSessionRef = "" },
		"unbalanced total":      func(usage *canonical.Usage) { usage.TotalTokens++ },
		"reasoning above output": func(usage *canonical.Usage) {
			usage.ReasoningOutputTokens = usage.OutputTokens + 1
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := valid
			mutate(&candidate)
			if err := canonical.ValidateUsage(candidate); err == nil {
				t.Fatalf("invalid exact usage accepted: %+v", candidate)
			}
		})
	}
}
