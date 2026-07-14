package sessionsync

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentParserGoldenFixtures(t *testing.T) {
	tests := []struct {
		name        string
		file        string
		wantEvents  int
		wantSummary string
	}{
		{name: "codex", file: "codex_session.jsonl", wantEvents: 6, wantSummary: "Implement contiguous Chunk upload."},
		{name: "claude", file: "claude_session.jsonl", wantEvents: 4, wantSummary: "Review the upload cursor contract."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "v2_sessions", tt.file))
			if err != nil {
				t.Fatal(err)
			}
			result, err := ParseContentChunk(bytes.NewReader(content), 100, nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Events) != tt.wantEvents || result.MalformedEventCount != 0 || result.EndCursor != 100+int64(len(content)) {
				t.Fatalf("events=%d malformed=%d end=%d", len(result.Events), result.MalformedEventCount, result.EndCursor)
			}
			found := false
			for _, event := range result.Events {
				if strings.Contains(event.Summary, tt.wantSummary) {
					found = true
				}
			}
			if !found {
				t.Fatalf("summary %q not found in %+v", tt.wantSummary, result.Events)
			}
		})
	}
}

func TestContentParserCountsMalformedJSONButPreservesCursor(t *testing.T) {
	content := []byte("not-json\n{\"type\":\"user\",\"timestamp\":\"2026-07-14T00:00:00Z\"}\n")
	result, err := ParseContentChunk(bytes.NewReader(content), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.MalformedEventCount != 1 || result.EndCursor != int64(len(content)) {
		t.Fatalf("result=%+v", result)
	}
}

func TestContentParserRejectsIncompleteOrOversizedLine(t *testing.T) {
	if _, err := ParseContentChunk(bytes.NewBufferString("{\"type\":\"user\"}"), 0, nil); !errors.Is(err, ErrIncompleteJSONLLine) {
		t.Fatalf("incomplete err=%v", err)
	}
	oversized := strings.Repeat("x", MaxContentProjectionLineBytes+1) + "\n"
	if _, err := ParseContentChunk(strings.NewReader(oversized), 0, nil); !errors.Is(err, ErrContentLineTooLarge) {
		t.Fatalf("oversized err=%v", err)
	}
}
