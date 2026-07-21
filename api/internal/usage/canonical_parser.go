package usage

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type canonicalEnvelope struct {
	Schema    string          `json:"schema"`
	EventID   string          `json:"event_id"`
	Timestamp time.Time       `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type canonicalUsage struct {
	FactID              string        `json:"usage_fact_id"`
	OwnerSessionRef     string        `json:"owner_session_ref"`
	IdentityStrategy    string        `json:"identity_strategy"`
	OccurredAt          time.Time     `json:"occurred_at"`
	Model               string        `json:"model"`
	CounterMode         string        `json:"counter_mode"`
	UncachedInputTokens int64         `json:"uncached_input_tokens"`
	CacheReadTokens     int64         `json:"cache_read_input_tokens"`
	CacheWrite5mTokens  int64         `json:"cache_creation_5m_input_tokens"`
	CacheWrite1hTokens  int64         `json:"cache_creation_1h_input_tokens"`
	OutputTokens        int64         `json:"output_tokens"`
	ReasoningTokens     int64         `json:"reasoning_output_tokens"`
	TotalTokens         int64         `json:"total_tokens"`
	Quality             QualityStatus `json:"quality"`
}

// ParseCanonicalChunk is deliberately separate from ParseProviderChunk: legacy
// Claude/Codex bytes can never enter this parser without source_format=aida_event_v1.
func ParseCanonicalChunk(clientType string, reader io.Reader, startCursor int64, state ParseState) (ParseResult, error) {
	if strings.TrimSpace(clientType) == "" || reader == nil || startCursor < 0 {
		return ParseResult{}, errors.New("canonical client, reader, and non-negative cursor are required")
	}
	buffered := bufio.NewReaderSize(reader, 64<<10)
	result := ParseResult{EndCursor: startCursor, State: state}
	for {
		line, err := readUsageLine(buffered)
		if errors.Is(err, io.EOF) {
			return result, nil
		}
		if err != nil {
			return ParseResult{}, err
		}
		start := result.EndCursor
		result.EndCursor += int64(len(line))
		result.ScannedLineCount++
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var envelope canonicalEnvelope
		if json.Unmarshal(trimmed, &envelope) != nil || envelope.Schema != "aida.session.event.v1" || strings.TrimSpace(envelope.EventID) == "" || envelope.Timestamp.IsZero() {
			result.MalformedCount++
			continue
		}
		if envelope.Type != "usage" {
			continue
		}
		record, ok := parseCanonicalUsage(clientType, envelope, line, start, result.EndCursor)
		if !ok {
			result.UnknownUsageCount++
			continue
		}
		result.Records = append(result.Records, record)
	}
}

func parseCanonicalUsage(clientType string, envelope canonicalEnvelope, rawLine []byte, start, end int64) (UsageRecord, bool) {
	var usage canonicalUsage
	if json.Unmarshal(envelope.Payload, &usage) != nil {
		return UsageRecord{}, false
	}
	if usage.FactID == "" || usage.OwnerSessionRef == "" || usage.IdentityStrategy == "" || usage.OccurredAt.IsZero() || usage.Model == "" || usage.CounterMode != "delta" {
		return UsageRecord{}, false
	}
	if usage.Quality == QualityExact && !strings.HasPrefix(usage.FactID, strings.TrimSpace(clientType)+":") {
		return UsageRecord{}, false
	}
	if usage.Quality != QualityExact && usage.Quality != QualityEstimated && usage.Quality != QualityIncomplete && usage.Quality != QualityConflict {
		return UsageRecord{}, false
	}
	c := TokenCounters{InputTokens: usage.UncachedInputTokens, CachedInputTokens: usage.CacheReadTokens, CacheCreationTokens: usage.CacheWrite5mTokens, RequestInputTokens: usage.CacheWrite1hTokens, OutputTokens: usage.OutputTokens, ReasoningTokens: usage.ReasoningTokens, TotalTokens: usage.TotalTokens}
	want := c.InputTokens + c.CachedInputTokens + c.CacheCreationTokens + c.RequestInputTokens + c.OutputTokens
	if countersHaveNegative(c) || c.ReasoningTokens > c.OutputTokens || c.TotalTokens != want {
		return UsageRecord{}, false
	}
	if !envelope.Timestamp.Equal(usage.OccurredAt) {
		return UsageRecord{}, false
	}
	raw := compactJSON(envelope.Payload)
	eventKey := "canonical:fact:" + usage.FactID
	return UsageRecord{Provider: "canonical", EventKey: eventKey, IdentityStrategy: usage.IdentityStrategy, ProviderFingerprint: usage.FactID, SourceStartCursor: start, SourceEndCursor: end, OccurredAt: usage.OccurredAt.UTC(), RawModel: usage.Model, RawUsage: raw, RawUsageHash: hashBytes(raw), Counters: c, Delta: c, Quality: usage.Quality, OwnerSessionRef: usage.OwnerSessionRef}, true
}
