package usage

import (
	"strings"
	"testing"
)

func TestParseCanonicalExactUsage(t *testing.T) {
	line := `{"schema":"aida.session.event.v1","event_id":"event-1","timestamp":"2026-07-21T01:02:03Z","type":"usage","payload":{"usage_fact_id":"workbuddy:req-42","owner_session_ref":"parent","identity_strategy":"native_request_id","occurred_at":"2026-07-21T01:02:03Z","model":"claude-sonnet","counter_mode":"delta","uncached_input_tokens":10,"cache_read_input_tokens":5,"cache_creation_5m_input_tokens":2,"cache_creation_1h_input_tokens":3,"output_tokens":4,"reasoning_output_tokens":1,"total_tokens":24,"quality":"exact"}}` + "\n"
	result, err := ParseCanonicalChunk("workbuddy", strings.NewReader(line), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.UnknownUsageCount != 0 {
		t.Fatalf("result=%+v", result)
	}
	record := result.Records[0]
	if record.Provider != "canonical" || record.ProviderFingerprint != "workbuddy:req-42" || record.OwnerSessionRef != "parent" || record.Quality != QualityExact {
		t.Fatalf("record=%+v", record)
	}
	normalized, err := Normalize(record)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.TotalTokens != 24 || normalized.CacheWrite5mTokens != 2 || normalized.CacheWrite1hTokens != 3 || normalized.IsEstimated {
		t.Fatalf("normalized=%+v", normalized)
	}
}

func TestParseCanonicalRejectsCumulativeAsExact(t *testing.T) {
	line := `{"schema":"aida.session.event.v1","event_id":"event-1","timestamp":"2026-07-21T01:02:03Z","type":"usage","payload":{"usage_fact_id":"x","owner_session_ref":"s","identity_strategy":"x","occurred_at":"2026-07-21T01:02:03Z","model":"m","counter_mode":"cumulative","total_tokens":0,"quality":"exact"}}` + "\n"
	result, err := ParseCanonicalChunk("workbuddy", strings.NewReader(line), 0, ParseState{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 0 || result.UnknownUsageCount != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestCanonicalUnavailableCapabilityBlocksAccounting(t *testing.T) {
	parsed := ParseResult{Records: []UsageRecord{{Quality: QualityExact}}}
	applyCanonicalCapabilityGate("unavailable", &parsed)
	if len(parsed.Records) != 1 || parsed.Records[0].Quality != QualityConflict || parsed.Records[0].QualityReason != "adapter release does not permit usage accounting" {
		t.Fatalf("parsed=%+v", parsed)
	}
}
