package usage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/canonicalsync"
	"github.com/aidashboard/api/internal/sessionsync"
)

type canonicalObjectStore struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func (s *canonicalObjectStore) PutVerified(_ context.Context, key string, reader io.Reader, size int64, _ string) error {
	content, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if int64(len(content)) != size {
		return fmt.Errorf("size=%d want=%d", len(content), size)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.objects[key] = content
	return nil
}
func (s *canonicalObjectStore) Download(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	content, ok := s.objects[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

func TestCanonicalExactUsageIsAttributedToDeclaredForkOwnerIntegration(t *testing.T) {
	url := os.Getenv("AIDA_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AIDA_TEST_DATABASE_URL is not set")
	}
	db, err := projectdb.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = projectdb.RunMigrations(db); err != nil {
		t.Fatal(err)
	}
	userID := int64(990027)
	_, _ = db.Exec(`DELETE FROM sessions WHERE user_id=$1`, userID)
	_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	defer func() {
		_, _ = db.Exec(`DELETE FROM sessions WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	}()
	if _, err = db.Exec(`INSERT INTO users(id,username)VALUES($1,'canonical-owner-test')`, userID); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 21, 3, 4, 5, 0, time.UTC)
	body := []byte(`{"schema":"aida.session.event.v1","event_id":"usage-1","timestamp":"2026-07-21T03:04:05Z","type":"usage","payload":{"usage_fact_id":"opencode:req-1","owner_session_ref":"parent","identity_strategy":"native_request_id","occurred_at":"2026-07-21T03:04:05Z","model":"model-x","counter_mode":"delta","uncached_input_tokens":10,"cache_read_input_tokens":2,"cache_creation_5m_input_tokens":1,"cache_creation_1h_input_tokens":0,"output_tokens":4,"reasoning_output_tokens":1,"total_tokens":17,"quality":"exact"}}` + "\n")
	canonicalService, err := canonicalsync.NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := canonicalService.PrepareFamily(context.Background(), fmt.Sprint(userID), canonicalsync.PrepareRequest{
		ClientVersion: "test",
		Sessions: []canonicalsync.PrepareSession{
			{SessionRef: "child", AgentType: "opencode", ParentSessionRef: "parent", ForkedAt: &at, ForkSource: "native", StartedAt: &at, Sources: []canonicalsync.PrepareSource{{SourceRole: "main", SourceKey: "opencode:child:main", LocalSize: int64(len(body)), PrefixCheckpointHash: sessionsync.HashBytes(nil), PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm, SourceFormat: "aida_event_v1", IngestionMetadata: canonicalsync.IngestionMetadata{AdapterVersion: "opencode-v1", UsageCapability: "unavailable"}}}},
			{SessionRef: "parent", AgentType: "opencode", StartedAt: &at},
		},
	})
	if err != nil || len(prepared) != 1 {
		t.Fatalf("prepared=%+v err=%v", prepared, err)
	}
	if _, err = db.Exec(`UPDATE session_sources SET ingestion_metadata=jsonb_set(ingestion_metadata,'{usage_capability}',to_jsonb('exact'::text)) WHERE id=(SELECT source_id FROM session_source_generations WHERE id=$1)`, prepared[0].GenerationID); err != nil {
		t.Fatal(err)
	}
	store := &canonicalObjectStore{objects: map[string][]byte{}}
	repository, err := sessionsync.NewPostgresChunkRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	acceptor, err := sessionsync.NewChunkAcceptor(repository, store)
	if err != nil {
		t.Fatal(err)
	}
	decision, err := acceptor.Accept(context.Background(), sessionsync.AcceptChunkRequest{UserID: fmt.Sprint(userID), GenerationID: prepared[0].GenerationID, Chunk: sessionsync.ChunkMetadata{StartCursor: 0, EndCursor: int64(len(body)), StartLine: 1, EndLine: 1, ContentSHA256: sessionsync.HashBytes(body), EventStartAt: &at, EventEndAt: &at}, ContentSize: int64(len(body)), Content: bytes.NewReader(body)})
	if err != nil || decision.Status != sessionsync.ChunkAccepted {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	legacySync, err := sessionsync.NewSyncService(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = legacySync.Finalize(context.Background(), fmt.Sprint(userID), prepared[0].GenerationID, sessionsync.FinalizeRequest{DeclaredEndCursor: int64(len(body)), PrefixCheckpointHash: sessionsync.HashBytes(body), PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm}); err != nil {
		t.Fatal(err)
	}
	var job sessionsync.ProcessingJob
	err = db.QueryRow(`SELECT id,job_type,session_id,generation_id,chunk_id,target_revision_id,content_epoch,payload,attempts,max_attempts FROM session_processing_jobs WHERE generation_id=$1 AND job_type='parse_usage_chunk'`, prepared[0].GenerationID).Scan(&job.ID, &job.Type, &job.SessionID, &job.GenerationID, &job.ChunkID, &job.TargetRevisionID, &job.ContentEpoch, &job.Payload, &job.Attempts, &job.MaxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	processor, err := NewProcessor(db, store, "")
	if err != nil {
		t.Fatal(err)
	}
	if err = processor.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	var ownerRef, provider, fingerprint, quality string
	var total int64
	err = db.QueryRow(`SELECT owner.session_ref,contribution.provider,claim.provider_event_fingerprint,contribution.quality_status,contribution.total_tokens FROM session_usage_contributions contribution JOIN sessions owner ON owner.id=contribution.member_session_id JOIN session_logical_usage_events event ON event.id=contribution.logical_usage_event_id JOIN session_usage_event_claims claim ON claim.active_logical_usage_event_id=event.id WHERE contribution.user_id=$1`, userID).Scan(&ownerRef, &provider, &fingerprint, &quality, &total)
	if err != nil {
		t.Fatal(err)
	}
	if ownerRef != "parent" || provider != "canonical" || fingerprint != "opencode:req-1" || quality != "exact" || total != 17 {
		t.Fatalf("owner=%s provider=%s fingerprint=%s quality=%s total=%d", ownerRef, provider, fingerprint, quality, total)
	}

	preparedParent, err := canonicalService.PrepareFamily(context.Background(), fmt.Sprint(userID), canonicalsync.PrepareRequest{ClientVersion: "test", Sessions: []canonicalsync.PrepareSession{{SessionRef: "parent", AgentType: "opencode", StartedAt: &at, Sources: []canonicalsync.PrepareSource{{SourceRole: "main", SourceKey: "opencode:parent:main", LocalSize: int64(len(body)), PrefixCheckpointHash: sessionsync.HashBytes(nil), PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm, SourceFormat: "aida_event_v1", IngestionMetadata: canonicalsync.IngestionMetadata{AdapterVersion: "opencode-v1", UsageCapability: "unavailable"}}}}}})
	if err != nil || len(preparedParent) != 1 {
		t.Fatalf("preparedParent=%+v err=%v", preparedParent, err)
	}
	if _, err = db.Exec(`UPDATE session_sources SET ingestion_metadata=jsonb_set(ingestion_metadata,'{usage_capability}',to_jsonb('exact'::text)) WHERE id=(SELECT source_id FROM session_source_generations WHERE id=$1)`, preparedParent[0].GenerationID); err != nil {
		t.Fatal(err)
	}
	decision, err = acceptor.Accept(context.Background(), sessionsync.AcceptChunkRequest{UserID: fmt.Sprint(userID), GenerationID: preparedParent[0].GenerationID, Chunk: sessionsync.ChunkMetadata{StartCursor: 0, EndCursor: int64(len(body)), StartLine: 1, EndLine: 1, ContentSHA256: sessionsync.HashBytes(body), EventStartAt: &at, EventEndAt: &at}, ContentSize: int64(len(body)), Content: bytes.NewReader(body)})
	if err != nil || decision.Status != sessionsync.ChunkAccepted {
		t.Fatalf("parent decision=%+v err=%v", decision, err)
	}
	if _, err = legacySync.Finalize(context.Background(), fmt.Sprint(userID), preparedParent[0].GenerationID, sessionsync.FinalizeRequest{DeclaredEndCursor: int64(len(body)), PrefixCheckpointHash: sessionsync.HashBytes(body), PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm}); err != nil {
		t.Fatal(err)
	}
	job = sessionsync.ProcessingJob{}
	err = db.QueryRow(`SELECT id,job_type,session_id,generation_id,chunk_id,target_revision_id,content_epoch,payload,attempts,max_attempts FROM session_processing_jobs WHERE generation_id=$1 AND job_type='parse_usage_chunk'`, preparedParent[0].GenerationID).Scan(&job.ID, &job.Type, &job.SessionID, &job.GenerationID, &job.ChunkID, &job.TargetRevisionID, &job.ContentEpoch, &job.Payload, &job.Attempts, &job.MaxAttempts)
	if err != nil {
		t.Fatal(err)
	}
	if err = processor.Process(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	var claimCount int
	var activeTotal int64
	err = db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(contribution.total_tokens),0) FROM session_usage_event_claims claim JOIN session_usage_contributions contribution ON contribution.logical_usage_event_id=claim.active_logical_usage_event_id WHERE claim.user_id=$1 AND claim.provider='canonical' AND claim.provider_event_fingerprint='opencode:req-1'`, userID).Scan(&claimCount, &activeTotal)
	if err != nil {
		t.Fatal(err)
	}
	if claimCount != 1 || activeTotal != 17 {
		t.Fatalf("claimCount=%d activeTotal=%d", claimCount, activeTotal)
	}
}
