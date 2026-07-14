package sessionsync

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestPrepareDecisionsForSES005SES006SES007SES013(t *testing.T) {
	checkpoint := &GenerationCheckpoint{
		ID:                   "generation-1",
		Status:               "active",
		ExpectedCursor:       128,
		PrefixCheckpointHash: HashBytes([]byte("accepted-prefix")),
		PrefixAlgorithm:      PrefixCheckpointAlgorithm,
	}

	tests := []struct {
		name  string
		state PrepareState
		input PrepareInput
		want  PrepareAction
		code  string
	}{
		{
			name:  "append",
			state: PrepareState{ContentStatus: ContentAvailable, ActiveGeneration: checkpoint},
			input: PrepareInput{LocalSize: 256, PrefixCheckpointHash: checkpoint.PrefixCheckpointHash, PrefixAlgorithm: PrefixCheckpointAlgorithm},
			want:  PrepareAppend,
		},
		{
			name:  "unchanged",
			state: PrepareState{ContentStatus: ContentAvailable, ActiveGeneration: checkpoint},
			input: PrepareInput{LocalSize: 128, PrefixCheckpointHash: checkpoint.PrefixCheckpointHash, PrefixAlgorithm: PrefixCheckpointAlgorithm},
			want:  PrepareUnchanged,
		},
		{
			name:  "truncated source rebuilds",
			state: PrepareState{ContentStatus: ContentAvailable, ActiveGeneration: checkpoint},
			input: PrepareInput{LocalSize: 64, PrefixCheckpointHash: checkpoint.PrefixCheckpointHash, PrefixAlgorithm: PrefixCheckpointAlgorithm},
			want:  PrepareRebuildRequired,
			code:  ErrorSourceDiverged,
		},
		{
			name:  "prefix mismatch rebuilds",
			state: PrepareState{ContentStatus: ContentAvailable, ActiveGeneration: checkpoint},
			input: PrepareInput{LocalSize: 256, PrefixCheckpointHash: HashBytes([]byte("different-prefix")), PrefixAlgorithm: PrefixCheckpointAlgorithm},
			want:  PrepareRebuildRequired,
			code:  ErrorSourceDiverged,
		},
		{
			name:  "cleared content requires confirmation",
			state: PrepareState{ContentStatus: ContentCleared},
			input: PrepareInput{},
			want:  PrepareContentCleared,
			code:  ErrorContentCleared,
		},
		{
			name: "confirmed restore uses staging generation",
			state: PrepareState{
				ContentStatus:     ContentCleared,
				RestoreGeneration: &GenerationCheckpoint{ID: "restore-1", Status: "staging"},
			},
			input: PrepareInput{},
			want:  PrepareRestore,
		},
		{
			name:  "clearing rejects writes",
			state: PrepareState{ContentStatus: ContentClearing},
			input: PrepareInput{},
			want:  PrepareRejected,
			code:  ErrorContentNotWritable,
		},
		{
			name:  "unknown content state rejects writes",
			state: PrepareState{ContentStatus: "future-state", ActiveGeneration: checkpoint},
			input: PrepareInput{LocalSize: 256, PrefixCheckpointHash: checkpoint.PrefixCheckpointHash, PrefixAlgorithm: PrefixCheckpointAlgorithm},
			want:  PrepareRejected,
			code:  ErrorContentNotWritable,
		},
		{
			name:  "malformed checkpoint is rejected",
			state: PrepareState{ContentStatus: ContentAvailable, ActiveGeneration: checkpoint},
			input: PrepareInput{LocalSize: 256, PrefixCheckpointHash: "not-a-sha256", PrefixAlgorithm: PrefixCheckpointAlgorithm},
			want:  PrepareRejected,
			code:  ErrorInvalidCheckpoint,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecidePrepare(tt.state, tt.input)
			if got.Action != tt.want || got.ErrorCode != tt.code {
				t.Fatalf("DecidePrepare() = %+v, want action=%s code=%s", got, tt.want, tt.code)
			}
		})
	}
}

func TestChunkHashMustUseCanonicalLowercaseSHA256(t *testing.T) {
	hash := HashBytes([]byte("content"))
	if !validSHA256(hash) {
		t.Fatal("canonical hash was rejected")
	}
	if validSHA256(strings.ToUpper(hash)) {
		t.Fatal("uppercase hash would pass Go validation but fail the database constraint")
	}
}

func TestPrepareWithoutLocalStateReturnsServerCheckpoint(t *testing.T) {
	checkpoint := &GenerationCheckpoint{
		ID: "generation-1", Status: "active", ExpectedCursor: 128,
		PrefixCheckpointHash: HashBytes([]byte("accepted-prefix")), PrefixAlgorithm: PrefixCheckpointAlgorithm,
	}
	decision := DecidePrepare(PrepareState{
		ContentStatus: ContentAvailable, ActiveGeneration: checkpoint,
	}, PrepareInput{LocalSize: 256, PrefixAlgorithm: PrefixCheckpointAlgorithm})
	if decision.Action != PrepareRejected || decision.ErrorCode != ErrorInvalidCheckpoint || decision.Generation != checkpoint {
		t.Fatalf("decision=%+v", decision)
	}
	truncated := DecidePrepare(PrepareState{
		ContentStatus: ContentAvailable, ActiveGeneration: checkpoint,
	}, PrepareInput{LocalSize: 64, PrefixAlgorithm: PrefixCheckpointAlgorithm})
	if truncated.Action != PrepareRebuildRequired || truncated.ErrorCode != ErrorSourceDiverged {
		t.Fatalf("truncated=%+v", truncated)
	}
}

func TestChunkRulesForCORE001SES003SES004(t *testing.T) {
	hashA := HashBytes([]byte("chunk-a"))
	hashB := HashBytes([]byte("chunk-b"))

	tests := []struct {
		name     string
		expected int64
		existing *AcceptedChunk
		incoming ChunkMetadata
		status   ChunkStatus
		code     string
		next     int64
	}{
		{
			name:     "accept contiguous range",
			expected: 100,
			incoming: ChunkMetadata{StartCursor: 100, EndCursor: 200, ContentSHA256: hashA},
			status:   ChunkAccepted,
			next:     200,
		},
		{
			name:     "same range and hash is duplicate",
			expected: 200,
			existing: &AcceptedChunk{StartCursor: 100, EndCursor: 200, ContentSHA256: hashA},
			incoming: ChunkMetadata{StartCursor: 100, EndCursor: 200, ContentSHA256: hashA},
			status:   ChunkDuplicate,
			next:     200,
		},
		{
			name:     "same range and different hash conflicts",
			expected: 200,
			existing: &AcceptedChunk{StartCursor: 100, EndCursor: 200, ContentSHA256: hashA},
			incoming: ChunkMetadata{StartCursor: 100, EndCursor: 200, ContentSHA256: hashB},
			status:   ChunkRejected,
			code:     ErrorChunkConflict,
			next:     200,
		},
		{
			name:     "gap",
			expected: 100,
			incoming: ChunkMetadata{StartCursor: 120, EndCursor: 200, ContentSHA256: hashA},
			status:   ChunkRejected,
			code:     ErrorCursorGap,
			next:     100,
		},
		{
			name:     "overlap",
			expected: 200,
			incoming: ChunkMetadata{StartCursor: 150, EndCursor: 250, ContentSHA256: hashA},
			status:   ChunkRejected,
			code:     ErrorCursorOverlap,
			next:     200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecideChunk(tt.expected, tt.existing, tt.incoming)
			if got.Status != tt.status || got.ErrorCode != tt.code || got.ExpectedCursor != tt.next {
				t.Fatalf("DecideChunk() = %+v, want status=%s code=%s next=%d", got, tt.status, tt.code, tt.next)
			}
		})
	}
}

func TestSES026DifferentRangesMayUseSameContentHash(t *testing.T) {
	hash := HashBytes([]byte("same raw bytes\n"))
	first := DecideChunk(0, nil, ChunkMetadata{StartCursor: 0, EndCursor: 15, ContentSHA256: hash})
	second := DecideChunk(first.ExpectedCursor, nil, ChunkMetadata{StartCursor: 15, EndCursor: 30, ContentSHA256: hash})
	if first.Status != ChunkAccepted || second.Status != ChunkAccepted || second.ExpectedCursor != 30 {
		t.Fatalf("same hash at different ranges must be accepted: first=%+v second=%+v", first, second)
	}
}

func TestCORE027ConcurrentCursorCASAcceptsOneWriter(t *testing.T) {
	type state struct {
		sync.Mutex
		expected int64
		accepted map[string]AcceptedChunk
	}
	s := &state{accepted: map[string]AcceptedChunk{}}
	hashes := []string{HashBytes([]byte("writer-a")), HashBytes([]byte("writer-b"))}
	results := make(chan ChunkDecision, len(hashes))
	var wg sync.WaitGroup

	for _, hash := range hashes {
		wg.Add(1)
		go func(hash string) {
			defer wg.Done()
			s.Lock()
			defer s.Unlock()
			incoming := ChunkMetadata{StartCursor: 0, EndCursor: 64, ContentSHA256: hash}
			decision := DecideChunk(s.expected, nil, incoming)
			if decision.Status == ChunkAccepted {
				s.expected = decision.ExpectedCursor
				s.accepted[hash] = acceptedChunkIdentity(incoming)
			}
			results <- decision
		}(hash)
	}
	wg.Wait()
	close(results)

	accepted := 0
	rejected := 0
	for result := range results {
		switch result.Status {
		case ChunkAccepted:
			accepted++
		case ChunkRejected:
			rejected++
		}
	}
	if accepted != 1 || rejected != 1 || s.expected != 64 || len(s.accepted) != 1 {
		t.Fatalf("accepted=%d rejected=%d expected=%d chunks=%d", accepted, rejected, s.expected, len(s.accepted))
	}
}

func TestCORE005HashPrefixRequiresExactAcceptedPrefix(t *testing.T) {
	content := []byte("first line\nsecond line\n")
	want := HashBytes(content[:11])
	got, err := HashPrefix(bytes.NewReader(content), 11)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("HashPrefix() = %s, want %s", got, want)
	}
	if _, err := HashPrefix(bytes.NewReader(content[:5]), 11); err != io.ErrUnexpectedEOF {
		t.Fatalf("short prefix error = %v, want %v", err, io.ErrUnexpectedEOF)
	}
}

func TestCORE028ChunkObjectKeyIsDeterministic(t *testing.T) {
	chunk := ChunkMetadata{StartCursor: 12, EndCursor: 34, ContentSHA256: HashBytes([]byte("content"))}
	first, err := ChunkObjectKey("generation/1", chunk)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ChunkObjectKey("generation/1", chunk)
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first == "" {
		t.Fatalf("object key is not deterministic: %q %q", first, second)
	}
}
