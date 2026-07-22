package sessiondigestv2

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/aidashboard/api/internal/contentreader"
)

func TestCanonicalMessageFlowsThroughDigestPipeline(t *testing.T) {
	source := regressionSource(t, 10, "canonical.message", map[string]any{"payload": map[string]any{
		"role": "user", "phase": "unknown", "message": "这条消息是为了测试日报生成",
	}})
	projected, err := projectSafeEvent(source)
	if err != nil {
		t.Fatal(err)
	}
	extractor := NewExtractor()
	extractor.Consume(projected)
	digest, sourceCount, included, omitted, _, _ := extractor.Result()
	if sourceCount != 1 || included != 1 || omitted != 0 || len(digest.WorkUnits) != 1 ||
		digest.WorkUnits[0].Goal.Text != "这条消息是为了测试日报生成" {
		t.Fatalf("canonical pipeline result is incorrect: coverage=%d/%d/%d units=%+v",
			sourceCount, included, omitted, digest.WorkUnits)
	}
}

func TestCanonicalSupportDoesNotChangeCodexOrClaudeDigestBaselines(t *testing.T) {
	tests := []struct {
		name    string
		sources []contentreader.Event
		want    string
	}{
		{
			name: "codex",
			sources: []contentreader.Event{
				regressionSource(t, 10, "event_msg.user_message", map[string]any{"payload": map[string]any{"message": "修复上传竞态"}}),
				regressionSource(t, 100, "event_msg.task_complete", map[string]any{"payload": map[string]any{"last_agent_message": "上传竞态已经修复"}}),
			},
			want: "57083c424a9316b00b3a30ca27f5c8d2c12799301c11e7002fd27e093bded39e",
		},
		{
			name: "claude",
			sources: []contentreader.Event{
				regressionSource(t, 10, "user", map[string]any{"message": map[string]any{"content": []any{
					map[string]any{"type": "text", "text": "检查构建失败原因"},
				}}}),
				regressionSource(t, 100, "assistant", map[string]any{"message": map[string]any{"content": []any{
					map[string]any{"type": "text", "text": "构建失败来自缺失依赖"},
				}}}),
			},
			want: "3bc52fecc291e048f12af6ee51224824ae5701a92a63805348428987148abf72",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			extractor := NewExtractor()
			for _, source := range test.sources {
				projected, err := projectSafeEvent(source)
				if err != nil {
					t.Fatal(err)
				}
				extractor.Consume(projected)
			}
			_, _, _, _, _, encoded := extractor.Result()
			if got := HashBytes(encoded); got != test.want {
				t.Fatalf("digest baseline changed: got=%s want=%s\n%s", got, test.want, encoded)
			}
		})
	}
}

func regressionSource(t *testing.T, cursor int64, eventType string, value any) contentreader.Event {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return contentreader.Event{
		SourceStartCursor: cursor,
		SourceEndCursor:   cursor + int64(len(payload)),
		OccurredAt:        time.Date(2026, 7, 22, 1, 0, int(cursor%60), 0, time.UTC),
		EventType:         eventType,
		Payload:           payload,
		ContentSHA256:     HashBytes(payload),
	}
}
