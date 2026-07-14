package sessionsync

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
)

func TestPostgresChunkRepositoryACKCASIntegration(t *testing.T) {
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := projectdb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}

	userID := int64(990002)
	_, _ = database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'v2-repository-test')`, userID); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
		_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
	}()

	var sessionID, sourceID, generationID string
	if err := database.QueryRow(`
		INSERT INTO sessions (session_ref, user_id, agent_type, started_at)
		VALUES ('repository-session', $1, 'codex', now()) RETURNING id`, userID).Scan(&sessionID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_sources (session_id, source_role, source_key)
		VALUES ($1, 'main', 'codex:repository-session:main') RETURNING id`, sessionID).Scan(&sourceID); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`
		INSERT INTO session_source_generations (source_id, status)
		VALUES ($1, 'active') RETURNING id`, sourceID).Scan(&generationID); err != nil {
		t.Fatal(err)
	}

	repository, err := NewPostgresChunkRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeVerifiedStore{}
	acceptor, err := NewChunkAcceptor(repository, store)
	if err != nil {
		t.Fatal(err)
	}

	firstContent := []byte("{\"event\":1}\n")
	firstRequest := integrationAcceptRequest(userID, generationID, 0, 1, firstContent)
	first, err := acceptor.Accept(context.Background(), firstRequest)
	if err != nil || first.Status != ChunkAccepted {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	retryRequest := integrationAcceptRequest(userID, generationID, 0, 1, firstContent)
	retry, err := acceptor.Accept(context.Background(), retryRequest)
	if err != nil || retry.Status != ChunkDuplicate {
		t.Fatalf("retry=%+v err=%v", retry, err)
	}

	secondContent := []byte("{\"event\":2}\n")
	start := first.ExpectedCursor
	results := make(chan ChunkDecision, 2)
	errorsCh := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request := integrationAcceptRequest(userID, generationID, start, 2, secondContent)
			decision, acceptErr := acceptor.Accept(context.Background(), request)
			results <- decision
			errorsCh <- acceptErr
		}()
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for acceptErr := range errorsCh {
		if acceptErr != nil {
			t.Fatal(acceptErr)
		}
	}
	accepted := 0
	duplicate := 0
	for result := range results {
		switch result.Status {
		case ChunkAccepted:
			accepted++
		case ChunkDuplicate:
			duplicate++
		}
	}
	if accepted != 1 || duplicate != 1 {
		t.Fatalf("accepted=%d duplicate=%d", accepted, duplicate)
	}

	var expectedCursor int64
	var chunkCount, jobCount int
	var prefixHash string
	if err := database.QueryRow(`
		SELECT expected_cursor, prefix_checkpoint_hash
		FROM session_source_generations WHERE id = $1`, generationID).Scan(&expectedCursor, &prefixHash); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM session_upload_chunks WHERE generation_id = $1`, generationID).Scan(&chunkCount); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM session_processing_jobs WHERE generation_id = $1`, generationID).Scan(&jobCount); err != nil {
		t.Fatal(err)
	}
	wantHash := HashBytes(append(append([]byte(nil), firstContent...), secondContent...))
	if expectedCursor != int64(len(firstContent)+len(secondContent)) || prefixHash != wantHash || chunkCount != 2 || jobCount != 4 {
		t.Fatalf("cursor=%d hash=%s chunks=%d jobs=%d", expectedCursor, prefixHash, chunkCount, jobCount)
	}
}

func integrationAcceptRequest(userID int64, generationID string, startCursor, line int64, content []byte) AcceptChunkRequest {
	now := time.Date(2026, 7, 14, 10, int(line), 0, 0, time.UTC)
	return AcceptChunkRequest{
		UserID:       sqlInt64String(userID),
		GenerationID: generationID,
		Chunk: ChunkMetadata{
			StartCursor:   startCursor,
			EndCursor:     startCursor + int64(len(content)),
			StartLine:     line,
			EndLine:       line,
			ContentSHA256: HashBytes(content),
			EventStartAt:  &now,
			EventEndAt:    &now,
		},
		ContentSize: int64(len(content)),
		Content:     bytesReader(content),
	}
}

func sqlInt64String(value int64) string {
	return fmt.Sprintf("%d", value)
}

func bytesReader(content []byte) *bytes.Reader {
	return bytes.NewReader(content)
}
