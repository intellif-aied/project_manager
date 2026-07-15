package sessionsync

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
)

func TestSyncServicePrepareAcceptFinalizeIntegration(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	userID := int64(990010)
	cleanupSyncIntegrationUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'v2-sync-service-test')`, userID); err != nil {
		t.Fatal(err)
	}
	defer cleanupSyncIntegrationUser(t, database, userID)

	service, err := NewSyncService(database)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("{\"type\":\"message\",\"value\":1}\n")
	request := integrationPrepareRequest("sync-e2e", content)
	prepared, err := service.Prepare(context.Background(), fmt.Sprint(userID), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared) != 1 || prepared[0].Action != PrepareRebuildRequired || prepared[0].GenerationStatus != "staging" {
		t.Fatalf("prepared=%+v", prepared)
	}

	repository, err := NewPostgresChunkRepository(database)
	if err != nil {
		t.Fatal(err)
	}
	acceptor, err := NewChunkAcceptor(repository, &fakeVerifiedStore{})
	if err != nil {
		t.Fatal(err)
	}
	eventAt := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	decision, err := acceptor.Accept(context.Background(), AcceptChunkRequest{
		UserID: fmt.Sprint(userID), GenerationID: prepared[0].GenerationID,
		Chunk: ChunkMetadata{
			StartCursor: 0, EndCursor: int64(len(content)), StartLine: 1, EndLine: 1,
			ContentSHA256: HashBytes(content), EventStartAt: &eventAt, EventEndAt: &eventAt,
		},
		ContentSize: int64(len(content)), Content: bytes.NewReader(content),
	})
	if err != nil || decision.Status != ChunkAccepted {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}

	finalized, err := service.Finalize(context.Background(), fmt.Sprint(userID), prepared[0].GenerationID, FinalizeRequest{
		DeclaredEndCursor: int64(len(content)), PrefixCheckpointHash: HashBytes(content),
		PrefixCheckpointAlgorithmVersion: PrefixCheckpointAlgorithm,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finalized.Status != "active" || finalized.ExpectedCursor != int64(len(content)) {
		t.Fatalf("finalized=%+v", finalized)
	}

	request.Sources[0].PrefixCheckpointHash = HashBytes(content)
	request.Summary = "updated session summary"
	preparedAgain, err := service.Prepare(context.Background(), fmt.Sprint(userID), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(preparedAgain) != 1 || preparedAgain[0].Action != PrepareUnchanged || preparedAgain[0].GenerationID != prepared[0].GenerationID {
		t.Fatalf("preparedAgain=%+v", preparedAgain)
	}

	var sourceCount, activeCount, stagingCount, chunkCount, jobCount, revisionCount int
	err = database.QueryRow(`
		SELECT
			(SELECT COUNT(*) FROM session_sources src JOIN sessions s ON s.id = src.session_id WHERE s.user_id = $1),
			(SELECT COUNT(*) FROM session_source_generations g JOIN session_sources src ON src.id = g.source_id JOIN sessions s ON s.id = src.session_id WHERE s.user_id = $1 AND g.status = 'active'),
			(SELECT COUNT(*) FROM session_source_generations g JOIN session_sources src ON src.id = g.source_id JOIN sessions s ON s.id = src.session_id WHERE s.user_id = $1 AND g.status = 'staging'),
			(SELECT COUNT(*) FROM session_upload_chunks c JOIN session_source_generations g ON g.id = c.generation_id JOIN session_sources src ON src.id = g.source_id JOIN sessions s ON s.id = src.session_id WHERE s.user_id = $1),
			(SELECT COUNT(*) FROM session_processing_jobs j JOIN sessions s ON s.id = j.session_id WHERE s.user_id = $1),
			(SELECT COUNT(*) FROM session_content_projection_revisions p JOIN session_source_generations g ON g.id = p.generation_id JOIN session_sources src ON src.id = g.source_id JOIN sessions s ON s.id = src.session_id WHERE s.user_id = $1)`, userID,
	).Scan(&sourceCount, &activeCount, &stagingCount, &chunkCount, &jobCount, &revisionCount)
	if err != nil {
		t.Fatal(err)
	}
	if sourceCount != 1 || activeCount != 1 || stagingCount != 0 || chunkCount != 1 || jobCount != 4 || revisionCount != 1 {
		t.Fatalf("sources=%d active=%d staging=%d chunks=%d jobs=%d revisions=%d", sourceCount, activeCount, stagingCount, chunkCount, jobCount, revisionCount)
	}
	var summary string
	if err := database.QueryRow(`SELECT summary FROM sessions WHERE user_id = $1 AND session_ref = $2`, userID, request.SessionRef).Scan(&summary); err != nil {
		t.Fatal(err)
	}
	if summary != request.Summary {
		t.Fatalf("summary=%q want=%q", summary, request.Summary)
	}

	secondContent := []byte("{\"type\":\"message\",\"value\":2}\n")
	request.Sources[0].LocalSize += int64(len(secondContent))
	appending, err := service.Prepare(context.Background(), fmt.Sprint(userID), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(appending) != 1 || appending[0].Action != PrepareAppend || appending[0].GenerationID != prepared[0].GenerationID {
		t.Fatalf("appending=%+v", appending)
	}
	secondDecision, err := acceptor.Accept(context.Background(), AcceptChunkRequest{
		UserID: fmt.Sprint(userID), GenerationID: prepared[0].GenerationID,
		Chunk: ChunkMetadata{
			StartCursor: int64(len(content)), EndCursor: int64(len(content) + len(secondContent)), StartLine: 2, EndLine: 2,
			ContentSHA256: HashBytes(secondContent), EventStartAt: &eventAt, EventEndAt: &eventAt,
		},
		ContentSize: int64(len(secondContent)), Content: bytes.NewReader(secondContent),
	})
	if err != nil || secondDecision.Status != ChunkAccepted {
		t.Fatalf("secondDecision=%+v err=%v", secondDecision, err)
	}
	var finalCursor int64
	var finalHash string
	if err := database.QueryRow(`
		SELECT expected_cursor, prefix_checkpoint_hash
		FROM session_source_generations WHERE id = $1`, prepared[0].GenerationID).Scan(&finalCursor, &finalHash); err != nil {
		t.Fatal(err)
	}
	combined := append(append([]byte(nil), content...), secondContent...)
	if finalCursor != int64(len(combined)) || finalHash != HashBytes(combined) {
		t.Fatalf("finalCursor=%d finalHash=%s", finalCursor, finalHash)
	}

	request.Sources[0].SourceKey = "different-source-key"
	if _, err := service.Prepare(context.Background(), fmt.Sprint(userID), request); !errors.Is(err, ErrSourceKeyConflict) {
		t.Fatalf("source key conflict err=%v", err)
	}
}

func TestSyncServiceConcurrentPrepareCreatesOneStagingGeneration(t *testing.T) {
	database := openSyncIntegrationDatabase(t)
	userID := int64(990011)
	cleanupSyncIntegrationUser(t, database, userID)
	if _, err := database.Exec(`INSERT INTO users (id, username) VALUES ($1, 'v2-concurrent-prepare-test')`, userID); err != nil {
		t.Fatal(err)
	}
	defer cleanupSyncIntegrationUser(t, database, userID)
	service, _ := NewSyncService(database)
	request := integrationPrepareRequest("prepare-race", []byte("{\"event\":1}\n"))

	const workers = 8
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	ids := make(chan string, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			prepared, err := service.Prepare(context.Background(), fmt.Sprint(userID), request)
			if err == nil {
				ids <- prepared[0].GenerationID
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	close(ids)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("generation ids=%v", unique)
	}
	var count int
	if err := database.QueryRow(`
		SELECT COUNT(*) FROM session_source_generations g
		JOIN session_sources src ON src.id = g.source_id
		JOIN sessions s ON s.id = src.session_id
		WHERE s.user_id = $1 AND g.status = 'staging'`, userID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("staging generation count=%d", count)
	}
}

func integrationPrepareRequest(sessionRef string, content []byte) PrepareSessionRequest {
	startedAt := time.Date(2026, 7, 14, 9, 0, 0, 0, time.UTC)
	return PrepareSessionRequest{
		SessionRef: sessionRef, AgentType: "codex", StartedAt: &startedAt, LastActivityAt: &startedAt,
		CWD: "/tmp/project", ProjectName: "project", Summary: "initial session summary",
		Sources: []PrepareSourceRequest{{
			SourceRole: "main", SourceKey: "codex:" + sessionRef + ":main", LocalSize: int64(len(content)),
			PrefixCheckpointHash: HashBytes(nil), PrefixCheckpointAlgorithmVersion: PrefixCheckpointAlgorithm,
		}},
	}
}

func openSyncIntegrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	databaseURL := os.Getenv("AIDA_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	database, err := projectdb.Connect(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := projectdb.RunMigrations(database); err != nil {
		t.Fatal(err)
	}
	return database
}

func cleanupSyncIntegrationUser(t *testing.T, database *sql.DB, userID int64) {
	t.Helper()
	_, _ = database.Exec(`DELETE FROM sessions WHERE user_id = $1`, userID)
	_, _ = database.Exec(`DELETE FROM users WHERE id = $1`, userID)
}
