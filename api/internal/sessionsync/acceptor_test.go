package sessionsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
)

type fakeChunkRepository struct {
	mu                sync.Mutex
	expectedCursor    int64
	contentStatus     ContentStatus
	contentEpoch      int64
	restoreWritable   bool
	chunks            map[string]AcceptedChunk
	jobs              []ProcessingJobSpec
	events            []string
	prefixHash        string
	prefixAlgorithm   string
	prefixState       []byte
	prefixStateFormat string
	commitErr         error
	inspectBarrier    chan struct{}
	inspectArrivals   chan struct{}
}

func newFakeChunkRepository() *fakeChunkRepository {
	return &fakeChunkRepository{
		contentStatus: ContentAvailable,
		contentEpoch:  1,
		chunks:        map[string]AcceptedChunk{},
	}
}

func (r *fakeChunkRepository) InspectChunk(_ context.Context, _, _ string, chunk ChunkMetadata) (ChunkSnapshot, error) {
	r.mu.Lock()
	r.events = append(r.events, "inspect")
	existing, ok := r.chunks[chunkRangeKey(chunk.StartCursor, chunk.EndCursor)]
	snapshot := ChunkSnapshot{
		ExpectedCursor:        r.expectedCursor,
		ContentStatus:         r.contentStatus,
		ContentEpoch:          r.contentEpoch,
		RestoreWritable:       r.restoreWritable,
		PrefixCheckpointHash:  r.prefixHash,
		PrefixAlgorithm:       r.prefixAlgorithm,
		PrefixCheckpointState: append([]byte(nil), r.prefixState...),
		PrefixStateFormat:     r.prefixStateFormat,
	}
	if ok {
		existingCopy := existing
		snapshot.Existing = &existingCopy
	}
	r.mu.Unlock()

	if r.inspectArrivals != nil {
		r.inspectArrivals <- struct{}{}
	}
	if r.inspectBarrier != nil {
		<-r.inspectBarrier
	}
	return snapshot, nil
}

func (r *fakeChunkRepository) CommitChunk(_ context.Context, request CommitChunkRequest) (ChunkDecision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "commit")
	if r.commitErr != nil {
		return ChunkDecision{}, r.commitErr
	}
	if request.ObservedEpoch != r.contentEpoch || (r.contentStatus != ContentAvailable && !r.restoreWritable) {
		decision := writableContentDecision(ChunkSnapshot{
			ExpectedCursor:  r.expectedCursor,
			ContentStatus:   r.contentStatus,
			ContentEpoch:    r.contentEpoch,
			RestoreWritable: r.restoreWritable,
		})
		if decision != nil {
			return *decision, nil
		}
		return ChunkDecision{
			Status:         ChunkRejected,
			ExpectedCursor: r.expectedCursor,
			ErrorCode:      ErrorContentNotWritable,
			NextAction:     "reload the session state before uploading",
		}, nil
	}
	key := chunkRangeKey(request.Chunk.StartCursor, request.Chunk.EndCursor)
	existing, ok := r.chunks[key]
	var existingPtr *AcceptedChunk
	if ok {
		existingCopy := existing
		existingPtr = &existingCopy
	}
	decision := DecideChunk(r.expectedCursor, existingPtr, request.Chunk)
	if decision.Status != ChunkAccepted {
		return decision, nil
	}
	r.expectedCursor = decision.ExpectedCursor
	r.chunks[key] = acceptedChunkIdentity(request.Chunk)
	r.prefixHash = request.NextPrefixCheckpointHash
	r.prefixAlgorithm = request.NextPrefixAlgorithm
	r.prefixState = append([]byte(nil), request.NextPrefixState...)
	r.prefixStateFormat = request.NextPrefixStateFormat
	r.jobs = append(r.jobs, request.Jobs...)
	return decision, nil
}

func TestRestoreGenerationMayUploadWhileOrdinaryClearedGenerationCannot(t *testing.T) {
	content := []byte("{\"event\":1}\n")
	for _, test := range []struct {
		name            string
		restoreWritable bool
		want            ChunkStatus
	}{
		{name: "ordinary cleared generation", want: ChunkRejected},
		{name: "confirmed restore generation", restoreWritable: true, want: ChunkAccepted},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := newFakeChunkRepository()
			repository.contentStatus = ContentCleared
			repository.restoreWritable = test.restoreWritable
			acceptor, err := NewChunkAcceptor(repository, &fakeVerifiedStore{})
			if err != nil {
				t.Fatal(err)
			}
			decision, err := acceptor.Accept(context.Background(), newAcceptRequest(content))
			if err != nil {
				t.Fatal(err)
			}
			if decision.Status != test.want {
				t.Fatalf("decision=%+v", decision)
			}
		})
	}
}

func chunkRangeKey(start, end int64) string {
	return fmt.Sprintf("%d:%d", start, end)
}

type fakeVerifiedStore struct {
	mu       sync.Mutex
	events   *[]string
	putCount int
	err      error
	afterPut func()
}

func (s *fakeVerifiedStore) PutVerified(_ context.Context, _ string, content io.Reader, size int64, expectedHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putCount++
	if s.events != nil {
		*s.events = append(*s.events, "store")
	}
	if s.err != nil {
		return s.err
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	if int64(len(data)) != size || HashBytes(data) != expectedHash {
		return errors.New("stored content verification failed")
	}
	if s.afterPut != nil {
		s.afterPut()
	}
	return nil
}

func newAcceptRequest(content []byte) AcceptChunkRequest {
	return newAcceptRequestAt(0, content)
}

func newAcceptRequestAt(start int64, content []byte) AcceptChunkRequest {
	return AcceptChunkRequest{
		UserID:       "user-1",
		GenerationID: "generation-1",
		Chunk: ChunkMetadata{
			StartCursor:   start,
			EndCursor:     start + int64(len(content)),
			StartLine:     1,
			EndLine:       1,
			ContentSHA256: HashBytes(content),
		},
		ContentSize: int64(len(content)),
		Content:     bytes.NewReader(content),
	}
}

func TestCORE028ChunkAcceptorAcknowledgesOnlyAfterStoreAndCommit(t *testing.T) {
	repository := newFakeChunkRepository()
	store := &fakeVerifiedStore{events: &repository.events}
	acceptor, err := NewChunkAcceptor(repository, store)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := acceptor.Accept(context.Background(), newAcceptRequest([]byte("{\"event\":1}\n")))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != ChunkAccepted || decision.AckedCursor == 0 {
		t.Fatalf("decision=%+v", decision)
	}
	if store.putCount != 1 || repository.expectedCursor != decision.AckedCursor || len(repository.jobs) != 2 {
		t.Fatalf("put=%d expected=%d jobs=%d", store.putCount, repository.expectedCursor, len(repository.jobs))
	}
	if got := fmt.Sprint(repository.events); got != "[inspect store commit]" {
		t.Fatalf("accept sequence=%s, want [inspect store commit]", got)
	}
	if repository.jobs[0].Type != JobIndexContentChunk || repository.jobs[0].ContentEpoch == nil ||
		repository.jobs[1].Type != JobParseUsageChunk || repository.jobs[1].ContentEpoch != nil {
		t.Fatalf("jobs do not preserve independent content/usage lifecycle: %+v", repository.jobs)
	}
	if repository.prefixHash != HashBytes([]byte("{\"event\":1}\n")) || repository.prefixAlgorithm != PrefixCheckpointAlgorithm || len(repository.prefixState) == 0 {
		t.Fatalf("prefix checkpoint was not advanced: hash=%s algorithm=%s state=%d", repository.prefixHash, repository.prefixAlgorithm, len(repository.prefixState))
	}
}

func TestCORE005ChunkAcceptorAdvancesFullPrefixCheckpoint(t *testing.T) {
	firstContent := []byte("{\"event\":1}\n")
	secondContent := []byte("{\"event\":2}\n")
	repository := newFakeChunkRepository()
	store := &fakeVerifiedStore{}
	acceptor, err := NewChunkAcceptor(repository, store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := acceptor.Accept(context.Background(), newAcceptRequestAt(0, firstContent))
	if err != nil || first.Status != ChunkAccepted {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := acceptor.Accept(context.Background(), newAcceptRequestAt(first.ExpectedCursor, secondContent))
	if err != nil || second.Status != ChunkAccepted {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	want := HashBytes(bytes.Join([][]byte{firstContent, secondContent}, nil))
	if repository.prefixHash != want || repository.expectedCursor != int64(len(firstContent)+len(secondContent)) {
		t.Fatalf("hash=%s want=%s cursor=%d", repository.prefixHash, want, repository.expectedCursor)
	}
}

func TestCORE028StoreOrCommitFailureNeverAcknowledges(t *testing.T) {
	content := []byte("{\"event\":1}\n")
	tests := []struct {
		name      string
		storeErr  error
		commitErr error
		wantPuts  int
	}{
		{name: "store failure", storeErr: errors.New("object store unavailable"), wantPuts: 1},
		{name: "database failure after store", commitErr: errors.New("transaction rolled back"), wantPuts: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := newFakeChunkRepository()
			repository.commitErr = tt.commitErr
			store := &fakeVerifiedStore{err: tt.storeErr}
			acceptor, err := NewChunkAcceptor(repository, store)
			if err != nil {
				t.Fatal(err)
			}
			decision, err := acceptor.Accept(context.Background(), newAcceptRequest(content))
			if err == nil || decision.Status != "" || decision.AckedCursor != 0 {
				t.Fatalf("decision=%+v err=%v", decision, err)
			}
			if store.putCount != tt.wantPuts || repository.expectedCursor != 0 || len(repository.jobs) != 0 {
				t.Fatalf("put=%d expected=%d jobs=%d", store.putCount, repository.expectedCursor, len(repository.jobs))
			}
		})
	}
}

func TestCORE001ResponseLossRetryReturnsDuplicateWithoutDuplicateJobs(t *testing.T) {
	content := []byte("{\"event\":1}\n")
	repository := newFakeChunkRepository()
	store := &fakeVerifiedStore{}
	acceptor, err := NewChunkAcceptor(repository, store)
	if err != nil {
		t.Fatal(err)
	}
	first, err := acceptor.Accept(context.Background(), newAcceptRequest(content))
	if err != nil || first.Status != ChunkAccepted {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := acceptor.Accept(context.Background(), newAcceptRequest(content))
	if err != nil || second.Status != ChunkDuplicate {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if store.putCount != 1 || len(repository.chunks) != 1 || len(repository.jobs) != 2 {
		t.Fatalf("put=%d chunks=%d jobs=%d", store.putCount, len(repository.chunks), len(repository.jobs))
	}
}

func TestCORE027ConcurrentSameCursorCommitsOnce(t *testing.T) {
	repository := newFakeChunkRepository()
	repository.inspectBarrier = make(chan struct{})
	repository.inspectArrivals = make(chan struct{}, 2)
	store := &fakeVerifiedStore{}
	acceptor, err := NewChunkAcceptor(repository, store)
	if err != nil {
		t.Fatal(err)
	}

	contents := [][]byte{[]byte("{\"writer\":\"a\"}\n"), []byte("{\"writer\":\"b\"}\n")}
	results := make(chan ChunkDecision, 2)
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for _, content := range contents {
		wg.Add(1)
		go func(content []byte) {
			defer wg.Done()
			decision, err := acceptor.Accept(context.Background(), newAcceptRequest(content))
			results <- decision
			errorsCh <- err
		}(content)
	}
	<-repository.inspectArrivals
	<-repository.inspectArrivals
	close(repository.inspectBarrier)
	wg.Wait()
	close(results)
	close(errorsCh)

	for err := range errorsCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	accepted := 0
	rejected := 0
	for result := range results {
		if result.Status == ChunkAccepted {
			accepted++
		} else if result.Status == ChunkRejected {
			rejected++
		}
	}
	if accepted != 1 || rejected != 1 || len(repository.chunks) != 1 || len(repository.jobs) != 2 {
		t.Fatalf("accepted=%d rejected=%d chunks=%d jobs=%d", accepted, rejected, len(repository.chunks), len(repository.jobs))
	}
}

func TestCORE029EpochChangeAfterObjectWriteRejectsCommit(t *testing.T) {
	content := []byte("{\"event\":1}\n")
	repository := newFakeChunkRepository()
	store := &fakeVerifiedStore{afterPut: func() {
		repository.mu.Lock()
		defer repository.mu.Unlock()
		repository.contentEpoch++
		repository.contentStatus = ContentClearing
	}}
	acceptor, err := NewChunkAcceptor(repository, store)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := acceptor.Accept(context.Background(), newAcceptRequest(content))
	if err != nil {
		t.Fatal(err)
	}
	if decision.Status != ChunkRejected || decision.ErrorCode != ErrorContentNotWritable || decision.AckedCursor != 0 {
		t.Fatalf("decision=%+v", decision)
	}
	if repository.expectedCursor != 0 || len(repository.chunks) != 0 || len(repository.jobs) != 0 {
		t.Fatalf("expected=%d chunks=%d jobs=%d", repository.expectedCursor, len(repository.chunks), len(repository.jobs))
	}
}
