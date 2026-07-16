package reportsource

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactReportUsageMetricsPreservesContent(t *testing.T) {
	payload := json.RawMessage(`{
		"type":"response_item",
		"message":{"content":[{"type":"text","text":"完成完整切片回归"}],"usage":{"input_tokens":120,"output_tokens":30}},
		"total_token_usage":{"total_tokens":150},
		"metadata":{"model":"gpt-test"}
	}`)

	got := string(redactReportUsageMetrics(payload))
	for _, forbidden := range []string{"usage", "input_tokens", "output_tokens", "total_tokens"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("redacted payload still contains %q: %s", forbidden, got)
		}
	}
	for _, required := range []string{"完成完整切片回归", "gpt-test"} {
		if !strings.Contains(got, required) {
			t.Fatalf("redacted payload lost %q: %s", required, got)
		}
	}
}

func TestRedactReportUsageMetricsLeavesOrdinaryPayloadUnchanged(t *testing.T) {
	payload := json.RawMessage(`{"type":"event_msg","payload":{"type":"user_message","message":"修复报告来源"}}`)
	if got := string(redactReportUsageMetrics(payload)); got != string(payload) {
		t.Fatalf("ordinary payload changed: %s", got)
	}
}
