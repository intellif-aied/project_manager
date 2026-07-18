package contentreader

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestPostgresRepositoryResolvesTargetChunksAndExpectedEvents(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := &postgresRepository{db: database}
	fallback := time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC)

	mock.ExpectQuery("SELECT revision.generation_id").
		WithArgs("revision-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"generation_id", "content_parser_version", "status", "build_start_cursor",
			"content_indexed_cursor", "content_status", "raw_log_url", "started_at",
		}).AddRow("generation-1", "session-content-v2", "active", 0, 20, "available", "legacy.jsonl", fallback))
	target, err := repository.resolveTarget(context.Background(), "revision-1")
	if err != nil {
		t.Fatal(err)
	}
	if target.GenerationID != "generation-1" || target.ContentIndexedCursor != 20 || target.LegacyObjectKey != "legacy.jsonl" {
		t.Fatalf("target=%+v", target)
	}

	mock.ExpectQuery("SELECT id, start_cursor, end_cursor").
		WithArgs("generation-1", int64(0), int64(20)).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "start_cursor", "end_cursor", "content_sha256", "raw_object_key", "object_status", "event_start_at",
		}).AddRow("chunk-1", 0, 20, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "chunk.jsonl", "available", fallback))
	chunks, err := repository.listChunks(context.Background(), "generation-1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].ObjectKey != "chunk.jsonl" || !chunks[0].EventStartAt.Valid {
		t.Fatalf("chunks=%+v", chunks)
	}

	mock.ExpectQuery("SELECT source_start_cursor, source_end_cursor, content_sha256").
		WithArgs("revision-1", int64(0), int64(20)).
		WillReturnRows(sqlmock.NewRows([]string{"source_start_cursor", "source_end_cursor", "content_sha256"}).
			AddRow(0, 20, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	iterator, err := repository.expectedEvents(context.Background(), "revision-1", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !iterator.Next() {
		t.Fatalf("iterator err=%v", iterator.Err())
	}
	event := iterator.Event()
	if event.StartCursor != 0 || event.EndCursor != 20 {
		t.Fatalf("event=%+v", event)
	}
	if iterator.Next() || iterator.Err() != nil {
		t.Fatalf("unexpected extra row or error: %v", iterator.Err())
	}
	if err := iterator.Close(); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPostgresRepositoryClassifiesMissingRevision(t *testing.T) {
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	repository := &postgresRepository{db: database}
	mock.ExpectQuery("SELECT revision.generation_id").
		WithArgs("missing").
		WillReturnRows(sqlmock.NewRows([]string{
			"generation_id", "content_parser_version", "status", "build_start_cursor",
			"content_indexed_cursor", "content_status", "raw_log_url", "started_at",
		}))
	_, err = repository.resolveTarget(context.Background(), "missing")
	if !errors.Is(err, ErrRevisionUnavailable) {
		t.Fatalf("err=%v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
