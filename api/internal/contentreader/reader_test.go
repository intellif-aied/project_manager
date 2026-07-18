package contentreader

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"testing"

	"github.com/aidashboard/api/internal/sessionsync"
)

func TestReaderStreamsV2ChunksAndVerifiesIndex(t *testing.T) {
	first := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\",\"payload\":{\"message\":\"first\"}}\n")
	second := []byte("{\"type\":\"assistant\",\"timestamp\":\"2026-07-18T01:00:01Z\",\"payload\":{\"message\":\"second\"}}\n")
	firstEvents := parseEvents(t, first, 0)
	secondEvents := parseEvents(t, second, int64(len(first)))
	end := int64(len(first) + len(second))
	repository := &fakeRepository{
		target: availableTarget(end),
		chunks: []contentChunk{
			{
				ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(first)),
				ContentSHA256: sessionsync.HashBytes(first), ObjectKey: "first.jsonl", ObjectStatus: "available",
			},
			{
				ID: "chunk-2", StartCursor: int64(len(first)), EndCursor: end,
				ContentSHA256: sessionsync.HashBytes(second), ObjectKey: "second.jsonl", ObjectStatus: "available",
			},
		},
		expected: append(expectedFrom(firstEvents), expectedFrom(secondEvents)...),
	}
	reader := mustReader(t, repository, memoryObjectStore{
		"first.jsonl":  first,
		"second.jsonl": second,
	})
	var events []Event
	result, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: end,
	}, func(event Event) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || result.EventCount != 2 || result.ObjectCount != 2 || result.EndCursor != end {
		t.Fatalf("events=%d result=%+v", len(events), result)
	}
	if events[0].Summary != "first" || events[1].Summary != "second" {
		t.Fatalf("events=%+v", events)
	}
}

func TestReaderFallsBackToLegacyObject(t *testing.T) {
	content := []byte(
		"{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\"}\n" +
			"{\"type\":\"assistant\",\"timestamp\":\"2026-07-18T01:00:01Z\"}\n",
	)
	events := parseEvents(t, content, 0)
	repository := &fakeRepository{
		target: func() readTarget {
			target := availableTarget(int64(len(content)))
			target.LegacyObjectKey = "legacy.jsonl"
			return target
		}(),
		expected: expectedFrom(events),
	}
	reader := mustReader(t, repository, memoryObjectStore{"legacy.jsonl": content})
	result, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: int64(len(content)),
	}, func(Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if result.EventCount != 2 || result.ObjectCount != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestReaderRejectsChunkCoverageGapOrOverlap(t *testing.T) {
	tests := []struct {
		name        string
		secondStart int64
	}{
		{name: "gap", secondStart: 6},
		{name: "overlap", secondStart: 4},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &fakeRepository{
				target: availableTarget(20),
				chunks: []contentChunk{
					{ID: "chunk-1", StartCursor: 0, EndCursor: 5, ObjectKey: "one", ObjectStatus: "available"},
					{ID: "chunk-2", StartCursor: test.secondStart, EndCursor: 20, ObjectKey: "two", ObjectStatus: "available"},
				},
			}
			reader := mustReader(t, repository, memoryObjectStore{})
			_, err := reader.Stream(context.Background(), Request{
				RevisionID: "revision-1", StartCursor: 0, EndCursor: 20,
			}, func(Event) error { return nil })
			if !errors.Is(err, ErrCursorIntegrity) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestReaderRejectsChunkHashMismatch(t *testing.T) {
	content := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\"}\n")
	events := parseEvents(t, content, 0)
	repository := &fakeRepository{
		target: availableTarget(int64(len(content))),
		chunks: []contentChunk{{
			ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(content)),
			ContentSHA256: sessionsync.HashBytes([]byte("different")), ObjectKey: "chunk.jsonl", ObjectStatus: "available",
		}},
		expected: expectedFrom(events),
	}
	reader := mustReader(t, repository, memoryObjectStore{"chunk.jsonl": content})
	_, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: int64(len(content)),
	}, func(Event) error { return nil })
	if !errors.Is(err, ErrContentIntegrity) {
		t.Fatalf("err=%v", err)
	}
}

func TestReaderClassifiesMissingObject(t *testing.T) {
	content := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\"}\n")
	events := parseEvents(t, content, 0)
	repository := &fakeRepository{
		target: availableTarget(int64(len(content))),
		chunks: []contentChunk{{
			ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(content)),
			ContentSHA256: sessionsync.HashBytes(content), ObjectKey: "missing.jsonl", ObjectStatus: "available",
		}},
		expected: expectedFrom(events),
	}
	reader := mustReader(t, repository, memoryObjectStore{})
	_, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: int64(len(content)),
	}, func(Event) error { return nil })
	if !errors.Is(err, ErrObjectUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestReaderClassifiesIncompleteJSONLAsContentIntegrity(t *testing.T) {
	content := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\"}")
	repository := &fakeRepository{
		target: availableTarget(int64(len(content))),
		chunks: []contentChunk{{
			ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(content)),
			ContentSHA256: sessionsync.HashBytes(content), ObjectKey: "chunk.jsonl", ObjectStatus: "available",
		}},
	}
	reader := mustReader(t, repository, memoryObjectStore{"chunk.jsonl": content})
	_, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: int64(len(content)),
	}, func(Event) error { return nil })
	if !errors.Is(err, ErrContentIntegrity) {
		t.Fatalf("err=%v", err)
	}
}

func TestReaderRejectsEventIndexMismatch(t *testing.T) {
	content := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\"}\n")
	events := parseEvents(t, content, 0)
	expected := expectedFrom(events)
	expected[0].ContentSHA256 = sessionsync.HashBytes([]byte("different"))
	repository := &fakeRepository{
		target: availableTarget(int64(len(content))),
		chunks: []contentChunk{{
			ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(content)),
			ContentSHA256: sessionsync.HashBytes(content), ObjectKey: "chunk.jsonl", ObjectStatus: "available",
		}},
		expected: expected,
	}
	reader := mustReader(t, repository, memoryObjectStore{"chunk.jsonl": content})
	called := false
	_, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: int64(len(content)),
	}, func(Event) error {
		called = true
		return nil
	})
	if !errors.Is(err, ErrContentIntegrity) || called {
		t.Fatalf("err=%v called=%t", err, called)
	}
}

func TestReaderRejectsEventBoundaryCut(t *testing.T) {
	content := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\"}\n")
	events := parseEvents(t, content, 0)
	repository := &fakeRepository{
		target: availableTarget(int64(len(content))),
		chunks: []contentChunk{{
			ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(content)),
			ContentSHA256: sessionsync.HashBytes(content), ObjectKey: "chunk.jsonl", ObjectStatus: "available",
		}},
		expected: expectedFrom(events),
	}
	reader := mustReader(t, repository, memoryObjectStore{"chunk.jsonl": content})
	_, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 1, EndCursor: int64(len(content)),
	}, func(Event) error { return nil })
	if !errors.Is(err, ErrCursorIntegrity) {
		t.Fatalf("err=%v", err)
	}
}

func TestReaderHonorsCancellationAndConsumerBackpressure(t *testing.T) {
	first := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\"}\n")
	second := []byte("{\"type\":\"assistant\",\"timestamp\":\"2026-07-18T01:00:01Z\"}\n")
	content := append(append([]byte{}, first...), second...)
	events := parseEvents(t, content, 0)
	repository := &fakeRepository{
		target: availableTarget(int64(len(content))),
		chunks: []contentChunk{{
			ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(content)),
			ContentSHA256: sessionsync.HashBytes(content), ObjectKey: "chunk.jsonl", ObjectStatus: "available",
		}},
		expected: expectedFrom(events),
	}
	reader := mustReader(t, repository, memoryObjectStore{"chunk.jsonl": content})
	ctx, cancel := context.WithCancel(context.Background())
	visits := 0
	_, err := reader.Stream(ctx, Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: int64(len(content)),
	}, func(Event) error {
		visits++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || visits != 1 {
		t.Fatalf("err=%v visits=%d", err, visits)
	}
}

func TestReaderIndexedRangeSeeksAndReadsOnlyRequestedBytes(t *testing.T) {
	first := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\",\"payload\":{\"message\":\"first\"}}\n")
	second := []byte("{\"type\":\"assistant\",\"timestamp\":\"2026-07-18T01:00:01Z\",\"payload\":{\"message\":\"second\"}}\n")
	third := []byte("{\"type\":\"assistant\",\"timestamp\":\"2026-07-18T01:00:02Z\",\"payload\":{\"message\":\"third\"}}\n")
	content := append(append(append([]byte{}, first...), second...), third...)
	start := int64(len(first))
	end := start + int64(len(second))
	events := parseEvents(t, second, start)
	repository := &fakeRepository{
		target: func() readTarget {
			target := availableTarget(int64(len(content)))
			target.LegacyObjectKey = "legacy.jsonl"
			return target
		}(),
		expected: expectedFrom(events),
	}
	store := &trackingSeekStore{content: content}
	reader := mustReader(t, repository, store)
	var received []Event
	result, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: start, EndCursor: end,
		ValidationMode: ValidationIndexedRange,
	}, func(event Event) error {
		received = append(received, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(received) != 1 || received[0].Summary != "second" || result.EventCount != 1 {
		t.Fatalf("received=%+v result=%+v", received, result)
	}
	if store.last == nil || store.last.seekOffset != start || store.last.bytesRead != int64(len(second)) {
		t.Fatalf("range read=%+v want offset=%d bytes=%d", store.last, start, len(second))
	}
}

func TestReaderWritesRawV2ChunksWithNullPayloadIndex(t *testing.T) {
	first := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\",\"payload\":{\"message\":\"first\"}}\n")
	second := []byte("{\"type\":\"assistant\",\"timestamp\":\"2026-07-18T01:00:01Z\",\"payload\":{\"message\":\"second\"}}\n")
	firstEvents := parseEvents(t, first, 0)
	secondEvents := parseEvents(t, second, int64(len(first)))
	end := int64(len(first) + len(second))
	repository := &fakeRepository{
		target: availableTarget(end),
		chunks: []contentChunk{
			{
				ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(first)),
				ContentSHA256: sessionsync.HashBytes(first), ObjectKey: "first.jsonl", ObjectStatus: "available",
			},
			{
				ID: "chunk-2", StartCursor: int64(len(first)), EndCursor: end,
				ContentSHA256: sessionsync.HashBytes(second), ObjectKey: "second.jsonl", ObjectStatus: "available",
			},
		},
		expected: append(expectedFrom(firstEvents), expectedFrom(secondEvents)...),
	}
	reader := mustReader(t, repository, memoryObjectStore{
		"first.jsonl": first, "second.jsonl": second,
	})
	var output bytes.Buffer
	result, err := reader.WriteRaw(context.Background(), "revision-1", &output)
	if err != nil {
		t.Fatal(err)
	}
	want := append(append([]byte{}, first...), second...)
	if !bytes.Equal(output.Bytes(), want) || result.EventCount != 2 || result.ObjectCount != 2 ||
		result.StartCursor != 0 || result.EndCursor != end {
		t.Fatalf("output=%q result=%+v", output.Bytes(), result)
	}
}

func TestReaderRejectsUnknownValidationMode(t *testing.T) {
	reader := mustReader(t, &fakeRepository{}, memoryObjectStore{})
	_, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: 1,
		ValidationMode: ValidationMode("unknown"),
	}, func(Event) error { return nil })
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("err=%v", err)
	}
}

func TestReaderValidatesRevisionAndContentState(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*readTarget)
		want   error
	}{
		{
			name:   "cleared content",
			modify: func(target *readTarget) { target.ContentStatus = "cleared" },
			want:   ErrContentUnavailable,
		},
		{
			name:   "unsupported parser",
			modify: func(target *readTarget) { target.ParserVersion = "session-content-v1" },
			want:   ErrUnsupportedParserVersion,
		},
		{
			name:   "range not indexed",
			modify: func(target *readTarget) { target.ContentIndexedCursor = 5 },
			want:   ErrRevisionUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target := availableTarget(10)
			test.modify(&target)
			reader := mustReader(t, &fakeRepository{target: target}, memoryObjectStore{})
			_, err := reader.Stream(context.Background(), Request{
				RevisionID: "revision-1", StartCursor: 0, EndCursor: 10,
			}, func(Event) error { return nil })
			if !errors.Is(err, test.want) {
				t.Fatalf("err=%v want=%v", err, test.want)
			}
		})
	}
}

func TestReaderPropagatesConsumerError(t *testing.T) {
	content := []byte("{\"type\":\"user\",\"timestamp\":\"2026-07-18T01:00:00Z\"}\n")
	events := parseEvents(t, content, 0)
	repository := &fakeRepository{
		target: availableTarget(int64(len(content))),
		chunks: []contentChunk{{
			ID: "chunk-1", StartCursor: 0, EndCursor: int64(len(content)),
			ContentSHA256: sessionsync.HashBytes(content), ObjectKey: "chunk.jsonl", ObjectStatus: "available",
		}},
		expected: expectedFrom(events),
	}
	reader := mustReader(t, repository, memoryObjectStore{"chunk.jsonl": content})
	want := errors.New("consumer failed")
	_, err := reader.Stream(context.Background(), Request{
		RevisionID: "revision-1", StartCursor: 0, EndCursor: int64(len(content)),
	}, func(Event) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err=%v", err)
	}
}

type fakeRepository struct {
	target      readTarget
	targetErr   error
	chunks      []contentChunk
	chunksErr   error
	expected    []expectedEvent
	expectedErr error
}

func (r *fakeRepository) resolveTarget(context.Context, string) (readTarget, error) {
	return r.target, r.targetErr
}

func (r *fakeRepository) listChunks(context.Context, string, int64, int64) ([]contentChunk, error) {
	return r.chunks, r.chunksErr
}

func (r *fakeRepository) expectedEvents(context.Context, string, int64, int64) (expectedEventIterator, error) {
	if r.expectedErr != nil {
		return nil, r.expectedErr
	}
	return &sliceExpectedIterator{events: r.expected}, nil
}

type sliceExpectedIterator struct {
	events  []expectedEvent
	index   int
	current expectedEvent
	closed  bool
}

func (i *sliceExpectedIterator) Next() bool {
	if i.index >= len(i.events) {
		return false
	}
	i.current = i.events[i.index]
	i.index++
	return true
}

func (i *sliceExpectedIterator) Event() expectedEvent { return i.current }
func (i *sliceExpectedIterator) Err() error           { return nil }
func (i *sliceExpectedIterator) Close() error {
	i.closed = true
	return nil
}

type memoryObjectStore map[string][]byte

func (s memoryObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	content, ok := s[key]
	if !ok {
		return nil, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

type trackingSeekStore struct {
	content []byte
	last    *trackingSeekReadCloser
}

func (s *trackingSeekStore) Download(_ context.Context, _ string) (io.ReadCloser, error) {
	s.last = &trackingSeekReadCloser{reader: bytes.NewReader(s.content), seekOffset: -1}
	return s.last, nil
}

type trackingSeekReadCloser struct {
	reader     *bytes.Reader
	seekOffset int64
	bytesRead  int64
}

func (r *trackingSeekReadCloser) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.bytesRead += int64(count)
	return count, err
}

func (r *trackingSeekReadCloser) Seek(offset int64, whence int) (int64, error) {
	position, err := r.reader.Seek(offset, whence)
	if err == nil && whence == io.SeekStart {
		r.seekOffset = position
	}
	return position, err
}

func (r *trackingSeekReadCloser) Close() error { return nil }

func availableTarget(indexedCursor int64) readTarget {
	return readTarget{
		GenerationID:         "generation-1",
		ParserVersion:        sessionsync.ContentParserVersion,
		RevisionStatus:       "active",
		BuildStartCursor:     0,
		ContentIndexedCursor: indexedCursor,
		ContentStatus:        "available",
		LegacyFallbackTime:   sql.NullTime{},
	}
}

func parseEvents(t *testing.T, content []byte, startCursor int64) []Event {
	t.Helper()
	result, err := sessionsync.ParseContentChunk(bytes.NewReader(content), startCursor, nil)
	if err != nil {
		t.Fatal(err)
	}
	return result.Events
}

func expectedFrom(events []Event) []expectedEvent {
	result := make([]expectedEvent, 0, len(events))
	for _, event := range events {
		result = append(result, expectedEvent{
			StartCursor: event.SourceStartCursor, EndCursor: event.SourceEndCursor, ContentSHA256: event.ContentSHA256,
		})
	}
	return result
}

func mustReader(t *testing.T, repository metadataRepository, store ObjectStore) *Reader {
	t.Helper()
	reader, err := newReader(repository, store)
	if err != nil {
		t.Fatal(err)
	}
	return reader
}
