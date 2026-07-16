package sessionmigration

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

const (
	maxChunkEvents = 500
	maxChunkBytes  = 500 << 20
	maxLineBytes   = 500 << 20
)

type ObjectStore interface {
	sessionsync.VerifiedChunkStore
	Download(context.Context, string) (io.ReadCloser, error)
}

type Options struct {
	Apply      bool
	Limit      int
	UserID     int64
	SessionRef string
}

type Report struct {
	Candidates            int      `json:"candidates"`
	MissingRawSessions    int64    `json:"missing_raw_sessions"`
	MissingRawTotalTokens int64    `json:"missing_raw_total_tokens"`
	Migrated              int      `json:"migrated"`
	Skipped               int      `json:"skipped"`
	Failed                int      `json:"failed"`
	Errors                []string `json:"errors,omitempty"`
}

type Backfiller struct {
	db       *sql.DB
	store    ObjectStore
	service  *sessionsync.SyncService
	acceptor *sessionsync.ChunkAcceptor
}

func New(database *sql.DB, store ObjectStore) (*Backfiller, error) {
	if database == nil || store == nil {
		return nil, errors.New("database and object store are required")
	}
	service, err := sessionsync.NewSyncService(database)
	if err != nil {
		return nil, err
	}
	repository, err := sessionsync.NewPostgresChunkRepository(database)
	if err != nil {
		return nil, err
	}
	acceptor, err := sessionsync.NewChunkAcceptor(repository, store)
	if err != nil {
		return nil, err
	}
	return &Backfiller{db: database, store: store, service: service, acceptor: acceptor}, nil
}

type candidate struct {
	SessionRef     string
	UserID         int64
	AgentType      string
	Summary        string
	StartedAt      time.Time
	LastActivityAt time.Time
	RawObjectKey   string
}

func (b *Backfiller) Run(ctx context.Context, options Options) (Report, error) {
	if options.Limit < 0 || options.UserID < 0 {
		return Report{}, errors.New("limit and user ID must not be negative")
	}
	if options.Apply && options.Limit == 0 {
		return Report{}, errors.New("apply requires a positive limit for bounded batches")
	}
	rows, err := b.db.QueryContext(ctx, `
		SELECT s.session_ref, s.user_id, s.agent_type, COALESCE(s.summary, ''),
			s.started_at, COALESCE(s.last_activity_at, s.ended_at, s.started_at), s.raw_log_url
		FROM sessions s
		LEFT JOIN session_sources src ON src.session_id = s.id AND src.source_role = 'main'
		WHERE COALESCE(length(s.raw_log_url), 0) > 0
			AND EXISTS (SELECT 1 FROM token_usage tu WHERE tu.session_id = s.id)
			AND ($1::bigint = 0 OR s.user_id = $1)
			AND ($3::text = '' OR s.session_ref = $3)
			AND (
				src.id IS NULL OR (
					src.active_generation_id IS NULL
					AND src.source_key = s.agent_type || ':' || s.session_ref || ':main'
				)
			)
		ORDER BY s.user_id, s.started_at, s.id
		LIMIT NULLIF($2, 0)`, options.UserID, options.Limit, strings.TrimSpace(options.SessionRef))
	if err != nil {
		return Report{}, err
	}
	defer rows.Close()

	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.SessionRef, &item.UserID, &item.AgentType, &item.Summary,
			&item.StartedAt, &item.LastActivityAt, &item.RawObjectKey); err != nil {
			return Report{}, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return Report{}, err
	}

	report := Report{Candidates: len(candidates)}
	if err := b.db.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT s.id), COALESCE(SUM(tu.total_tokens), 0)
		FROM sessions s
		JOIN token_usage tu ON tu.session_id = s.id
		WHERE COALESCE(length(s.raw_log_url), 0) = 0
			AND ($1::bigint = 0 OR s.user_id = $1)
			AND ($2::text = '' OR s.session_ref = $2)`,
		options.UserID, strings.TrimSpace(options.SessionRef),
	).Scan(&report.MissingRawSessions, &report.MissingRawTotalTokens); err != nil {
		return Report{}, err
	}
	if !options.Apply {
		return report, nil
	}
	for _, item := range candidates {
		status, migrateErr := b.migrateOne(ctx, item)
		if migrateErr != nil {
			report.Failed++
			report.Errors = append(report.Errors, fmt.Sprintf("%d/%s: %v", item.UserID, item.SessionRef, migrateErr))
			continue
		}
		if status == "skipped" {
			report.Skipped++
		} else {
			report.Migrated++
		}
	}
	return report, nil
}

func (b *Backfiller) migrateOne(ctx context.Context, item candidate) (string, error) {
	temp, err := os.CreateTemp("", "aida-session-backfill-*.jsonl")
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	object, err := b.store.Download(ctx, item.RawObjectKey)
	if err != nil {
		temp.Close()
		return "", err
	}
	size, copyErr := io.Copy(temp, object)
	closeObjectErr := object.Close()
	closeTempErr := temp.Close()
	if copyErr != nil {
		return "", copyErr
	}
	if closeObjectErr != nil {
		return "", closeObjectErr
	}
	if closeTempErr != nil {
		return "", closeTempErr
	}
	if size == 0 {
		return "", errors.New("raw log is empty")
	}

	userID := strconv.FormatInt(item.UserID, 10)
	sourceKey := normalizedAgentType(item.AgentType) + ":" + item.SessionRef + ":main"
	resumePrefix, err := b.resumePrefix(ctx, item, sourceKey, size, tempPath)
	if err != nil {
		return "", err
	}
	prepared, err := b.prepare(ctx, userID, item, sourceKey, size, resumePrefix)
	if err != nil {
		return "", err
	}
	if prepared.ExpectedCursor > size {
		return "", errors.New("server cursor exceeds legacy raw log size")
	}
	if prepared.Action == sessionsync.PrepareRejected &&
		prepared.ErrorCode == sessionsync.ErrorInvalidCheckpoint && prepared.ExpectedCursor > 0 {
		prefix, prefixErr := hashPrefixFile(tempPath, prepared.ExpectedCursor)
		if prefixErr != nil {
			return "", prefixErr
		}
		prepared, err = b.prepare(ctx, userID, item, sourceKey, size, prefix)
		if err != nil {
			return "", err
		}
	}
	if prepared.Action == sessionsync.PrepareContentCleared || prepared.Action == sessionsync.PrepareRejected {
		return "", fmt.Errorf("prepare %s: %s %s", prepared.Action, prepared.ErrorCode, prepared.NextAction)
	}
	if prepared.GenerationID == "" {
		return "", errors.New("prepare returned no generation")
	}
	if prepared.Action == sessionsync.PrepareUnchanged && prepared.GenerationStatus == "active" {
		return "skipped", nil
	}

	file, err := os.Open(tempPath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	prefixHasher := sha256.New()
	startLine, err := initializePrefix(file, prefixHasher, prepared.ExpectedCursor)
	if err != nil {
		return "", err
	}
	if _, err := file.Seek(prepared.ExpectedCursor, io.SeekStart); err != nil {
		return "", err
	}
	endCursor, _, err := streamChunks(file, prepared.ExpectedCursor, startLine+1, func(chunk chunk) error {
		decision, acceptErr := b.acceptor.Accept(ctx, sessionsync.AcceptChunkRequest{
			UserID: userID, GenerationID: prepared.GenerationID,
			Chunk: sessionsync.ChunkMetadata{
				StartCursor: chunk.StartCursor, EndCursor: chunk.EndCursor,
				StartLine: chunk.StartLine, EndLine: chunk.EndLine,
				ContentSHA256: chunk.Hash,
			},
			ContentSize: int64(len(chunk.Content)), Content: bytes.NewReader(chunk.Content),
		})
		if acceptErr != nil {
			return acceptErr
		}
		if decision.Status != sessionsync.ChunkAccepted && decision.Status != sessionsync.ChunkDuplicate {
			return fmt.Errorf("chunk rejected: %s %s", decision.ErrorCode, decision.NextAction)
		}
		_, writeErr := prefixHasher.Write(chunk.Content)
		return writeErr
	})
	if err != nil {
		return "", err
	}
	if endCursor != size {
		return "", fmt.Errorf("chunked cursor %d does not match raw log size %d", endCursor, size)
	}
	_, err = b.service.Finalize(ctx, userID, prepared.GenerationID, sessionsync.FinalizeRequest{
		DeclaredEndCursor: endCursor, PrefixCheckpointHash: hex.EncodeToString(prefixHasher.Sum(nil)),
		PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm,
	})
	if err != nil {
		return "", err
	}
	return "migrated", nil
}

func (b *Backfiller) resumePrefix(ctx context.Context, item candidate, sourceKey string, size int64, path string) (string, error) {
	var expectedCursor int64
	err := b.db.QueryRowContext(ctx, `
		SELECT g.expected_cursor
		FROM sessions s
		JOIN session_sources src ON src.session_id = s.id AND src.source_role = 'main'
		JOIN session_source_generations g ON g.id = src.staging_generation_id
		WHERE s.user_id = $1 AND s.agent_type = $2 AND s.session_ref = $3
			AND src.source_key = $4 AND g.status = 'staging'`,
		item.UserID, normalizedAgentType(item.AgentType), item.SessionRef, sourceKey,
	).Scan(&expectedCursor)
	if errors.Is(err, sql.ErrNoRows) || expectedCursor == 0 {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if expectedCursor > size {
		return "", errors.New("server staging cursor exceeds legacy raw log size")
	}
	return hashPrefixFile(path, expectedCursor)
}

func (b *Backfiller) prepare(ctx context.Context, userID string, item candidate, sourceKey string, size int64, prefix string) (sessionsync.PrepareSourceResponse, error) {
	responses, err := b.service.Prepare(ctx, userID, sessionsync.PrepareSessionRequest{
		SessionRef: item.SessionRef, AgentType: normalizedAgentType(item.AgentType), Summary: item.Summary,
		StartedAt: &item.StartedAt, LastActivityAt: &item.LastActivityAt,
		Sources: []sessionsync.PrepareSourceRequest{{
			SourceRole: "main", SourceKey: sourceKey, LocalSize: size,
			PrefixCheckpointHash: prefix, PrefixCheckpointAlgorithmVersion: sessionsync.PrefixCheckpointAlgorithm,
		}},
	})
	if err != nil {
		return sessionsync.PrepareSourceResponse{}, err
	}
	if len(responses) != 1 {
		return sessionsync.PrepareSourceResponse{}, errors.New("prepare returned an unexpected source count")
	}
	return responses[0], nil
}

type chunk struct {
	StartCursor int64
	EndCursor   int64
	StartLine   int64
	EndLine     int64
	Hash        string
	Content     []byte
}

func streamChunks(reader io.Reader, startCursor, startLine int64, emit func(chunk) error) (int64, int64, error) {
	buffered := bufio.NewReaderSize(reader, 64<<10)
	cursor, line := startCursor, startLine
	chunkStart, chunkLine := cursor, line
	var content bytes.Buffer
	events := 0
	flush := func() error {
		if content.Len() == 0 {
			return nil
		}
		payload := append([]byte(nil), content.Bytes()...)
		hashValue := sha256.Sum256(payload)
		if err := emit(chunk{
			StartCursor: chunkStart, EndCursor: cursor, StartLine: chunkLine, EndLine: line - 1,
			Hash: hex.EncodeToString(hashValue[:]), Content: payload,
		}); err != nil {
			return err
		}
		content.Reset()
		events = 0
		chunkStart, chunkLine = cursor, line
		return nil
	}
	for {
		lineBytes, err := buffered.ReadBytes('\n')
		if len(lineBytes) > maxLineBytes {
			return cursor, line - 1, fmt.Errorf("raw log contains a line larger than %d MiB", maxLineBytes>>20)
		}
		if len(lineBytes) > 0 {
			if err == io.EOF && !json.Valid(bytes.TrimSpace(lineBytes)) {
				return cursor, line - 1, errors.New("raw log ends with an incomplete JSONL record")
			}
			if err != nil && !errors.Is(err, io.EOF) {
				return cursor, line - 1, err
			}
			if content.Len() > 0 && (events >= maxChunkEvents || content.Len()+len(lineBytes) > maxChunkBytes) {
				if flushErr := flush(); flushErr != nil {
					return cursor, line - 1, flushErr
				}
			}
			if content.Len() == 0 && len(lineBytes) > maxChunkBytes/2 {
				startCursor, startLine := cursor, line
				cursor += int64(len(lineBytes))
				line++
				hashValue := sha256.Sum256(lineBytes)
				if emitErr := emit(chunk{
					StartCursor: startCursor, EndCursor: cursor, StartLine: startLine, EndLine: startLine,
					Hash: hex.EncodeToString(hashValue[:]), Content: lineBytes,
				}); emitErr != nil {
					return cursor, line - 1, emitErr
				}
				chunkStart, chunkLine = cursor, line
				if err == io.EOF {
					break
				}
				continue
			}
			content.Write(lineBytes)
			cursor += int64(len(lineBytes))
			line++
			events++
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return cursor, line - 1, err
		}
	}
	if err := flush(); err != nil {
		return cursor, line - 1, err
	}
	return cursor, line - 1, nil
}

func initializePrefix(file *os.File, hasher hash.Hash, size int64) (int64, error) {
	if size == 0 {
		return 0, nil
	}
	prefix := io.LimitReader(file, size)
	buffer := make([]byte, 64*1024)
	var lines int64
	for {
		n, err := prefix.Read(buffer)
		if n > 0 {
			if _, writeErr := hasher.Write(buffer[:n]); writeErr != nil {
				return 0, writeErr
			}
			lines += int64(bytes.Count(buffer[:n], []byte{'\n'}))
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
	}
	return lines, nil
}

func hashPrefixFile(path string, size int64) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := initializePrefix(file, hasher, size); err != nil {
		return "", err
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func normalizedAgentType(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "claude_code"
	}
	return value
}
