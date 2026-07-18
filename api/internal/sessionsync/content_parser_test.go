package sessionsync

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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

func TestScanContentChunkMatchesParseContentChunk(t *testing.T) {
	content := []byte(
		"not-json\n" +
			"{\"type\":\"user\",\"timestamp\":\"2026-07-14T00:00:00Z\"}\n" +
			"{\"type\":\"assistant\",\"timestamp\":\"2026-07-14T00:00:01Z\"}\n",
	)
	parsed, err := ParseContentChunk(bytes.NewReader(content), 37, nil)
	if err != nil {
		t.Fatal(err)
	}
	var streamed []ProjectedContentEvent
	scanned, err := ScanContentChunk(bytes.NewReader(content), 37, nil, func(event ProjectedContentEvent) error {
		streamed = append(streamed, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned.EndCursor != parsed.EndCursor || scanned.MalformedEventCount != parsed.MalformedEventCount {
		t.Fatalf("scan=%+v parse=%+v", scanned, parsed)
	}
	if len(streamed) != len(parsed.Events) {
		t.Fatalf("streamed=%d parsed=%d", len(streamed), len(parsed.Events))
	}
	for index := range streamed {
		if streamed[index].SourceStartCursor != parsed.Events[index].SourceStartCursor ||
			streamed[index].SourceEndCursor != parsed.Events[index].SourceEndCursor ||
			streamed[index].ContentSHA256 != parsed.Events[index].ContentSHA256 {
			t.Fatalf("event %d streamed=%+v parsed=%+v", index, streamed[index], parsed.Events[index])
		}
	}
}

func TestScanContentChunkStopsOnVisitorError(t *testing.T) {
	content := []byte(
		"{\"type\":\"user\",\"timestamp\":\"2026-07-14T00:00:00Z\"}\n" +
			"{\"type\":\"assistant\",\"timestamp\":\"2026-07-14T00:00:01Z\"}\n",
	)
	want := errors.New("stop")
	visits := 0
	_, err := ScanContentChunk(bytes.NewReader(content), 0, nil, func(ProjectedContentEvent) error {
		visits++
		return want
	})
	if !errors.Is(err, want) || visits != 1 {
		t.Fatalf("err=%v visits=%d", err, visits)
	}
}

func TestScanContentChunkEmitsBeforeReadingEntireObject(t *testing.T) {
	first := "{\"type\":\"user\",\"timestamp\":\"2026-07-14T00:00:00Z\"}\n"
	second := "{\"type\":\"assistant\",\"timestamp\":\"2026-07-14T00:00:01Z\",\"payload\":{\"message\":\"" +
		strings.Repeat("x", 256<<10) + "\"}}\n"
	reader := &limitedTrackingReader{content: []byte(first + second), maxRead: 256}
	firstVisitBytes := 0
	result, err := ScanContentChunk(reader, 0, nil, func(ProjectedContentEvent) error {
		if firstVisitBytes == 0 {
			firstVisitBytes = reader.bytesRead
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstVisitBytes == 0 || firstVisitBytes >= len(reader.content) {
		t.Fatalf("first visit after %d of %d bytes", firstVisitBytes, len(reader.content))
	}
	if result.EndCursor != int64(len(reader.content)) {
		t.Fatalf("result=%+v", result)
	}
}

type limitedTrackingReader struct {
	content   []byte
	offset    int
	maxRead   int
	bytesRead int
}

func (r *limitedTrackingReader) Read(buffer []byte) (int, error) {
	if r.offset >= len(r.content) {
		return 0, io.EOF
	}
	limit := len(buffer)
	if limit > r.maxRead {
		limit = r.maxRead
	}
	remaining := len(r.content) - r.offset
	if limit > remaining {
		limit = remaining
	}
	copy(buffer[:limit], r.content[r.offset:r.offset+limit])
	r.offset += limit
	r.bytesRead += limit
	return limit, nil
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

func TestContentParserSanitizesPostgresUnsupportedNullCharacters(t *testing.T) {
	content := []byte(`{"type":"user","timestamp":"2026-07-14T00:00:00Z","payload":{"type":"message","message":"before\u0000after","literal":"\\u0000"}}` + "\n")
	result, err := ParseContentChunk(bytes.NewReader(content), 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Events) != 1 || result.MalformedEventCount != 0 {
		t.Fatalf("result=%+v", result)
	}
	event := result.Events[0]
	if event.Summary != "before\uFFFDafter" || strings.ContainsRune(event.Summary, '\x00') {
		t.Fatalf("summary=%q", event.Summary)
	}
	var payload struct {
		Payload struct {
			Message string `json:"message"`
			Literal string `json:"literal"`
		} `json:"payload"`
	}
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Payload.Message != "before\uFFFDafter" || payload.Payload.Literal != `\u0000` {
		t.Fatalf("payload=%+v raw=%s", payload.Payload, event.Payload)
	}
}
