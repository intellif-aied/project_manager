package canonicalsync

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	projectdb "github.com/aidashboard/api/db"
	"github.com/aidashboard/api/internal/sessionsync"
)

func TestPrepareFamilyRegistersParentAndCanonicalSourceAtomically(t *testing.T) {
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
	userID := int64(990026)
	_, _ = db.Exec(`DELETE FROM sessions WHERE user_id=$1`, userID)
	_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	defer func() {
		_, _ = db.Exec(`DELETE FROM sessions WHERE user_id=$1`, userID)
		_, _ = db.Exec(`DELETE FROM users WHERE id=$1`, userID)
	}()
	if _, err = db.Exec(`INSERT INTO users(id,username) VALUES($1,'canonical-family-test')`, userID); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db, ReleasePolicy{"opencode": {
		ClientVersion: "test", AdapterVersion: "opencode-v1", MaximumUsageCapability: "unavailable",
	}})
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 7, 21, 1, 2, 3, 0, time.UTC)
	body := []byte("{\"schema\":\"aida.session.event.v1\"}\n")
	result, err := service.PrepareFamily(context.Background(), fmt.Sprint(userID), PrepareRequest{ClientVersion: "test", Sessions: []PrepareSession{
		{SessionRef: "child", AgentType: "opencode", ParentSessionRef: "parent", StartedAt: &at, Sources: []PrepareSource{{SourceRole: "main", SourceKey: "opencode:child:main", LocalSize: int64(len(body)), PrefixCheckpointHash: sessionsync.HashBytes(nil), PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm, SourceFormat: "aida_event_v1", IngestionMetadata: IngestionMetadata{AdapterVersion: "opencode-v1", NativeClientVersion: "1.0", UsageCapability: "unavailable"}}}},
		{SessionRef: "parent", AgentType: "opencode", StartedAt: &at},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || result[0].Action != "rebuild_required" || result[0].GenerationStatus != "staging" {
		t.Fatalf("result=%+v", result)
	}
	var count int
	if err = db.QueryRow(`SELECT count(*) FROM sessions WHERE user_id=$1 AND agent_type='opencode'`, userID).Scan(&count); err != nil || count != 2 {
		t.Fatalf("sessions=%d err=%v", count, err)
	}
	var format, capability string
	if err = db.QueryRow(`SELECT source_format,ingestion_metadata->>'usage_capability' FROM session_sources src JOIN sessions s ON s.id=src.session_id WHERE s.user_id=$1`, userID).Scan(&format, &capability); err != nil || format != "aida_event_v1" || capability != "unavailable" {
		t.Fatalf("format=%s capability=%s err=%v", format, capability, err)
	}
	if _, err = service.PrepareFamily(context.Background(), fmt.Sprint(userID), PrepareRequest{ClientVersion: "test", Sessions: []PrepareSession{{SessionRef: "child", AgentType: "opencode", StartedAt: &at}}}); err == nil {
		t.Fatal("expected an existing session family reparent attempt to fail")
	}
	var storedParent string
	if err = db.QueryRow(`SELECT parent_session_ref FROM sessions WHERE user_id=$1 AND agent_type='opencode' AND session_ref='child'`, userID).Scan(&storedParent); err != nil || storedParent != "parent" {
		t.Fatalf("storedParent=%s err=%v", storedParent, err)
	}
	repository, err := sessionsync.NewPostgresChunkRepository(db)
	if err != nil {
		t.Fatal(err)
	}
	acceptor, err := sessionsync.NewChunkAcceptor(repository, canonicalVerifiedStore{})
	if err != nil {
		t.Fatal(err)
	}
	decision, err := acceptor.Accept(context.Background(), sessionsync.AcceptChunkRequest{UserID: fmt.Sprint(userID), GenerationID: result[0].GenerationID, Chunk: sessionsync.ChunkMetadata{StartCursor: 0, EndCursor: int64(len(body)), StartLine: 1, EndLine: 1, ContentSHA256: sessionsync.HashBytes(body), EventStartAt: &at, EventEndAt: &at}, ContentSize: int64(len(body)), Content: bytes.NewReader(body)})
	if err != nil || decision.Status != sessionsync.ChunkAccepted {
		t.Fatalf("decision=%+v err=%v", decision, err)
	}
	legacySync, err := sessionsync.NewSyncService(db)
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := legacySync.Finalize(context.Background(), fmt.Sprint(userID), result[0].GenerationID, sessionsync.FinalizeRequest{DeclaredEndCursor: int64(len(body)), PrefixCheckpointHash: sessionsync.HashBytes(body), PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm})
	if err != nil || finalized.Status != "active" {
		t.Fatalf("finalized=%+v err=%v", finalized, err)
	}
}

type canonicalVerifiedStore struct{}

func (canonicalVerifiedStore) PutVerified(_ context.Context, _ string, reader io.Reader, size int64, _ string) error {
	written, err := io.Copy(io.Discard, reader)
	if err != nil {
		return err
	}
	if written != size {
		return fmt.Errorf("stored=%d want=%d", written, size)
	}
	return nil
}
