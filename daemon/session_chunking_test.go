package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSES001GoldenSessionFixturesSplitIntoContiguousCompleteLines(t *testing.T) {
	fixtures := []struct {
		name   string
		size   int
		sha256 string
	}{
		{name: "claude_session.jsonl", size: 955, sha256: "25a5ed3acb5e48ce9455c77b9e739dcf2cea1e87f4035bf2fbbac070df76bc55"},
		{name: "codex_session.jsonl", size: 1021, sha256: "44fb6d7031c0563d21003551446eaeb862cbe460dd46fce2001bb56ec292ebc6"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			content, err := os.ReadFile(filepath.Join("..", "testdata", "v2_sessions", fixture.name))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(content)
			if len(content) != fixture.size || hex.EncodeToString(sum[:]) != fixture.sha256 {
				t.Fatalf("golden fixture changed: size=%d sha256=%s", len(content), hex.EncodeToString(sum[:]))
			}
			chunks, pendingTail, err := splitSessionJSONL(bytes.NewReader(content), 0, 1, sessionChunkLimits{
				MaxEvents:     2,
				MaxChunkBytes: 4096,
				MaxLineBytes:  4096,
			})
			if err != nil {
				t.Fatal(err)
			}
			if pendingTail {
				t.Fatal("golden fixture unexpectedly has an incomplete tail")
			}
			var rebuilt []byte
			var cursor int64
			for _, chunk := range chunks {
				if chunk.StartCursor != cursor {
					t.Fatalf("chunk starts at %d, want %d", chunk.StartCursor, cursor)
				}
				if chunk.EndCursor <= chunk.StartCursor || chunk.StartLine > chunk.EndLine {
					t.Fatalf("invalid chunk metadata: %+v", chunk)
				}
				rebuilt = append(rebuilt, chunk.Content...)
				cursor = chunk.EndCursor
			}
			if !bytes.Equal(rebuilt, content) {
				t.Fatalf("rebuilt fixture differs: got=%d want=%d", len(rebuilt), len(content))
			}
		})
	}
}

func TestGoldenClaudeFixtureParserBaseline(t *testing.T) {
	path := filepath.Join("..", "testdata", "v2_sessions", "claude_session.jsonl")
	session := parseJSONL(path)
	if session == nil {
		t.Fatal("parseJSONL returned nil")
	}
	if session.SessionRef != "fixture-claude-001" || session.Summary != "Review the upload cursor contract." {
		t.Fatalf("unexpected identity/summary: ref=%q summary=%q", session.SessionRef, session.Summary)
	}
	if session.SummaryStatus != "ok" || session.SummarySource != "user.message" {
		t.Fatalf("unexpected Claude summary diagnostics: status=%q source=%q", session.SummaryStatus, session.SummarySource)
	}
	if session.TotalTok != 240 || session.InputTok != 150 || session.OutputTok != 32 || session.CacheCreateTok != 8 || session.CacheReadTok != 50 {
		t.Fatalf("unexpected Claude usage baseline: %+v", session)
	}
	if len(session.ActivitySlices) != 2 || session.ActivitySlices[0].ActivityDate != "2026-07-10" || session.ActivitySlices[1].ActivityDate != "2026-07-11" {
		t.Fatalf("unexpected Claude activity slices: %+v", session.ActivitySlices)
	}
}

func TestGoldenCodexFixtureParserBaseline(t *testing.T) {
	path := filepath.Join("..", "testdata", "v2_sessions", "codex_session.jsonl")
	session := parseCodexJSONL(path)
	if session == nil {
		t.Fatal("parseCodexJSONL returned nil")
	}
	if session.SessionRef != "fixture-codex-001" || session.Summary != "Implement contiguous Chunk upload." {
		t.Fatalf("unexpected identity/summary: ref=%q summary=%q", session.SessionRef, session.Summary)
	}
	if session.SummaryStatus != "ok" || session.SummarySource != "event_msg.user_message" {
		t.Fatalf("unexpected Codex summary diagnostics: status=%q source=%q", session.SummaryStatus, session.SummarySource)
	}
	if session.TotalTok != 220 || session.InputTok != 180 || session.OutputTok != 40 || session.CacheReadTok != 50 {
		t.Fatalf("unexpected Codex usage baseline: %+v", session)
	}
	if len(session.ActivitySlices) != 2 || session.ActivitySlices[0].TotalTokens != 120 || session.ActivitySlices[1].TotalTokens != 100 {
		t.Fatalf("unexpected Codex activity slices: %+v", session.ActivitySlices)
	}
}

func TestSES002IncompleteTailDoesNotAdvanceCursorAndUploadsOnceAfterCompletion(t *testing.T) {
	full := []byte("{\"event\":1}\n{\"event\":2}\n")
	partial := full[:len(full)-2]
	limits := sessionChunkLimits{MaxEvents: 10, MaxChunkBytes: 1024, MaxLineBytes: 1024}

	first, pendingTail, err := splitSessionJSONL(bytes.NewReader(partial), 0, 1, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !pendingTail || len(first) != 1 {
		t.Fatalf("first scan chunks=%d pendingTail=%t", len(first), pendingTail)
	}
	expectedCursor := first[0].EndCursor
	if expectedCursor != int64(len("{\"event\":1}\n")) {
		t.Fatalf("expected cursor=%d", expectedCursor)
	}

	second, pendingTail, err := splitSessionJSONL(bytes.NewReader(full[expectedCursor:]), expectedCursor, 2, limits)
	if err != nil {
		t.Fatal(err)
	}
	if pendingTail || len(second) != 1 {
		t.Fatalf("second scan chunks=%d pendingTail=%t", len(second), pendingTail)
	}
	if string(second[0].Content) != "{\"event\":2}\n" || second[0].StartCursor != expectedCursor {
		t.Fatalf("unexpected completed tail chunk: %+v", second[0])
	}
}

func TestSES026IdenticalContentAtDifferentRangesProducesTwoChunks(t *testing.T) {
	line := []byte("{\"same\":true}\n")
	content := append(append([]byte(nil), line...), line...)
	chunks, pendingTail, err := splitSessionJSONL(bytes.NewReader(content), 0, 1, sessionChunkLimits{
		MaxEvents:     1,
		MaxChunkBytes: 1024,
		MaxLineBytes:  1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pendingTail || len(chunks) != 2 {
		t.Fatalf("chunks=%d pendingTail=%t", len(chunks), pendingTail)
	}
	if chunks[0].ContentSHA256 != chunks[1].ContentSHA256 || chunks[0].StartCursor == chunks[1].StartCursor {
		t.Fatalf("same bytes at different ranges were not preserved: %+v", chunks)
	}
}

func TestSES020OversizedJSONLLineIsRejected(t *testing.T) {
	_, _, err := splitSessionJSONL(bytes.NewReader([]byte("{\"message\":\"too long\"}\n")), 0, 1, sessionChunkLimits{
		MaxEvents:     10,
		MaxChunkBytes: 1024,
		MaxLineBytes:  8,
	})
	if !errors.Is(err, errSessionLineTooLarge) {
		t.Fatalf("error=%v, want %v", err, errSessionLineTooLarge)
	}
}

func TestJSONLLineAboveFourMiBFitsUnifiedEightMiBChunk(t *testing.T) {
	line := append([]byte(`{"message":"`), bytes.Repeat([]byte("x"), (4<<20)+128)...)
	line = append(line, []byte(`"}`+"\n")...)
	chunks, pending, err := splitSessionJSONL(bytes.NewReader(line), 0, 1, sessionChunkLimits{
		MaxEvents: defaultSyncChunkEvents, MaxChunkBytes: defaultSyncChunkBytes, MaxLineBytes: defaultSyncMaxLineBytes,
	})
	if err != nil || pending || len(chunks) != 1 || len(chunks[0].Content) != len(line) {
		t.Fatalf("chunks=%d pending=%t err=%v", len(chunks), pending, err)
	}
}

func TestStreamingChunkerStopsImmediatelyWhenEmitterFails(t *testing.T) {
	emitErr := errors.New("stop after first ACK failure")
	emitted := 0
	_, err := streamSessionJSONLChunks(
		bytes.NewReader([]byte("{\"event\":1}\n{\"event\":2}\n{\"event\":3}\n")),
		0,
		1,
		sessionChunkLimits{MaxEvents: 1, MaxChunkBytes: 1024, MaxLineBytes: 1024},
		func(localSessionChunk) error {
			emitted++
			return emitErr
		},
	)
	if !errors.Is(err, emitErr) || emitted != 1 {
		t.Fatalf("err=%v emitted=%d", err, emitted)
	}
}
