package contentreader

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/aidashboard/api/internal/sessionsync"
)

var (
	ErrInvalidRequest           = errors.New("invalid content reader request")
	ErrRevisionUnavailable      = errors.New("content revision is unavailable")
	ErrContentUnavailable       = errors.New("session content is unavailable")
	ErrObjectUnavailable        = errors.New("session content object is unavailable")
	ErrCursorIntegrity          = errors.New("session content cursor integrity error")
	ErrContentIntegrity         = errors.New("session content hash or event integrity error")
	ErrUnsupportedParserVersion = errors.New("unsupported content parser version")
)

// Event is the canonical event projection produced by the shared Session content parser.
type Event = sessionsync.ProjectedContentEvent

type ValidationMode string

const (
	// ValidationFull verifies complete overlapping objects and is the default for background consumers.
	ValidationFull ValidationMode = ""
	// ValidationIndexedRange reads only the requested byte range and verifies every event against the index.
	ValidationIndexedRange ValidationMode = "indexed_range"
)

// Request identifies an indexed revision range. Cursor bounds use [start, end) semantics.
type Request struct {
	RevisionID     string
	StartCursor    int64
	EndCursor      int64
	ValidationMode ValidationMode
}

// Result describes the successfully validated range. It is valid only when Stream returns nil.
type Result struct {
	StartCursor         int64
	EndCursor           int64
	EventCount          int64
	MalformedEventCount int64
	ObjectCount         int
}

// ObjectStore downloads immutable Session source objects.
type ObjectStore interface {
	Download(context.Context, string) (io.ReadCloser, error)
}

// Reader reconstructs indexed Session events from their immutable MinIO source objects.
type Reader struct {
	repository metadataRepository
	store      ObjectStore
}

// New creates a Reader backed by PostgreSQL metadata and an object store.
func New(database *sql.DB, store ObjectStore) (*Reader, error) {
	if database == nil || store == nil {
		return nil, errors.New("database and object store are required")
	}
	return newReader(&postgresRepository{db: database}, store)
}

func newReader(repository metadataRepository, store ObjectStore) (*Reader, error) {
	if repository == nil || store == nil {
		return nil, errors.New("metadata repository and object store are required")
	}
	return &Reader{repository: repository, store: store}, nil
}

// Stream validates and synchronously emits a frozen revision range.
//
// The callback provides natural backpressure. Callers must not commit callback side effects until
// Stream returns nil because an object-level checksum is finalized after its events are parsed.
func (r *Reader) Stream(
	ctx context.Context,
	request Request,
	consume func(Event) error,
) (result Result, returnedErr error) {
	request.RevisionID = strings.TrimSpace(request.RevisionID)
	if request.RevisionID == "" || request.StartCursor < 0 || request.EndCursor <= request.StartCursor || consume == nil ||
		(request.ValidationMode != ValidationFull && request.ValidationMode != ValidationIndexedRange) {
		return Result{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	target, err := r.repository.resolveTarget(ctx, request.RevisionID)
	if err != nil {
		return Result{}, err
	}
	if err := validateTarget(target, request); err != nil {
		return Result{}, err
	}
	chunks, err := r.repository.listChunks(ctx, target.GenerationID, request.StartCursor, request.EndCursor)
	if err != nil {
		return Result{}, err
	}
	if len(chunks) > 0 {
		if err := validateChunkCoverage(chunks, request.StartCursor, request.EndCursor); err != nil {
			return Result{}, err
		}
	} else if strings.TrimSpace(target.LegacyObjectKey) == "" {
		return Result{}, fmt.Errorf("%w: revision %s has no chunk ledger or legacy object", ErrObjectUnavailable, request.RevisionID)
	}

	expected, err := r.repository.expectedEvents(ctx, request.RevisionID, request.StartCursor, request.EndCursor)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		if closeErr := expected.Close(); returnedErr == nil && closeErr != nil {
			returnedErr = closeErr
		}
	}()
	matcher := expectedMatcher{iterator: expected}
	result = Result{StartCursor: request.StartCursor, EndCursor: request.EndCursor}

	visit := func(event Event) error {
		if event.SourceEndCursor <= request.StartCursor || event.SourceStartCursor >= request.EndCursor {
			return nil
		}
		if event.SourceStartCursor < request.StartCursor || event.SourceEndCursor > request.EndCursor {
			return fmt.Errorf(
				"%w: event [%d,%d) crosses requested range [%d,%d)",
				ErrCursorIntegrity,
				event.SourceStartCursor,
				event.SourceEndCursor,
				request.StartCursor,
				request.EndCursor,
			)
		}
		if err := matcher.match(event); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := consume(event); err != nil {
			return err
		}
		result.EventCount++
		return nil
	}

	if len(chunks) == 0 {
		fallback := nullableTime(target.LegacyFallbackTime)
		scanResult, err := r.scanObject(
			ctx,
			target.LegacyObjectKey,
			0,
			-1,
			"",
			request.StartCursor,
			request.EndCursor,
			request.ValidationMode == ValidationIndexedRange,
			fallback,
			visit,
			nil,
		)
		if err != nil {
			return Result{}, err
		}
		if scanResult.EndCursor < request.EndCursor {
			return Result{}, fmt.Errorf(
				"%w: legacy object ends at %d before requested cursor %d",
				ErrCursorIntegrity,
				scanResult.EndCursor,
				request.EndCursor,
			)
		}
		result.MalformedEventCount += scanResult.MalformedEventCount
		result.ObjectCount = 1
	} else {
		for _, chunk := range chunks {
			fallback := nullableTime(chunk.EventStartAt)
			rangeStart := max(request.StartCursor, chunk.StartCursor)
			rangeEnd := min(request.EndCursor, chunk.EndCursor)
			scanResult, err := r.scanObject(
				ctx,
				chunk.ObjectKey,
				chunk.StartCursor,
				chunk.EndCursor,
				chunk.ContentSHA256,
				rangeStart,
				rangeEnd,
				request.ValidationMode == ValidationIndexedRange,
				fallback,
				visit,
				nil,
			)
			if err != nil {
				return Result{}, err
			}
			result.MalformedEventCount += scanResult.MalformedEventCount
			result.ObjectCount++
		}
	}
	if err := matcher.finish(); err != nil {
		return Result{}, err
	}
	return result, nil
}

// WriteRaw validates and writes every V2 chunk in a complete projection revision.
// It preserves the original JSONL bytes and never reads event payloads from PostgreSQL.
func (r *Reader) WriteRaw(
	ctx context.Context,
	revisionID string,
	output io.Writer,
) (result Result, returnedErr error) {
	revisionID = strings.TrimSpace(revisionID)
	if revisionID == "" || output == nil {
		return Result{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	target, err := r.repository.resolveTarget(ctx, revisionID)
	if err != nil {
		return Result{}, err
	}
	endCursor := target.ContentIndexedCursor
	request := Request{RevisionID: revisionID, StartCursor: 0, EndCursor: endCursor}
	if endCursor <= 0 || target.BuildStartCursor != 0 {
		return Result{}, fmt.Errorf("%w: revision %s is not a complete V2 source", ErrRevisionUnavailable, revisionID)
	}
	if err := validateTarget(target, request); err != nil {
		return Result{}, err
	}
	chunks, err := r.repository.listChunks(ctx, target.GenerationID, 0, endCursor)
	if err != nil {
		return Result{}, err
	}
	if len(chunks) == 0 {
		return Result{}, fmt.Errorf("%w: revision %s has no V2 chunks", ErrObjectUnavailable, revisionID)
	}
	if err := validateChunkCoverage(chunks, 0, endCursor); err != nil {
		return Result{}, err
	}
	expected, err := r.repository.expectedEvents(ctx, revisionID, 0, endCursor)
	if err != nil {
		return Result{}, err
	}
	defer func() {
		if closeErr := expected.Close(); returnedErr == nil && closeErr != nil {
			returnedErr = closeErr
		}
	}()
	matcher := expectedMatcher{iterator: expected}
	result = Result{StartCursor: 0, EndCursor: endCursor}
	visit := func(event Event) error {
		if err := matcher.match(event); err != nil {
			return err
		}
		result.EventCount++
		return nil
	}
	for _, chunk := range chunks {
		fallback := nullableTime(chunk.EventStartAt)
		scanResult, err := r.scanObject(
			ctx,
			chunk.ObjectKey,
			chunk.StartCursor,
			chunk.EndCursor,
			chunk.ContentSHA256,
			chunk.StartCursor,
			chunk.EndCursor,
			false,
			fallback,
			visit,
			output,
		)
		if err != nil {
			return Result{}, err
		}
		result.MalformedEventCount += scanResult.MalformedEventCount
		result.ObjectCount++
	}
	if err := matcher.finish(); err != nil {
		return Result{}, err
	}
	return result, nil
}

func validateTarget(target readTarget, request Request) error {
	if target.ContentStatus != "available" {
		return fmt.Errorf("%w: session content status is %s", ErrContentUnavailable, target.ContentStatus)
	}
	if target.ParserVersion != sessionsync.ContentParserVersion {
		return fmt.Errorf(
			"%w: revision uses %s, reader supports %s",
			ErrUnsupportedParserVersion,
			target.ParserVersion,
			sessionsync.ContentParserVersion,
		)
	}
	switch target.RevisionStatus {
	case "building", "validated", "active", "superseded":
	default:
		return fmt.Errorf("%w: revision status is %s", ErrRevisionUnavailable, target.RevisionStatus)
	}
	if request.StartCursor < target.BuildStartCursor || request.EndCursor > target.ContentIndexedCursor {
		return fmt.Errorf(
			"%w: requested [%d,%d) outside indexed range [%d,%d)",
			ErrRevisionUnavailable,
			request.StartCursor,
			request.EndCursor,
			target.BuildStartCursor,
			target.ContentIndexedCursor,
		)
	}
	return nil
}

func validateChunkCoverage(chunks []contentChunk, startCursor, endCursor int64) error {
	covered := startCursor
	previousEnd := int64(-1)
	for index, chunk := range chunks {
		if chunk.ObjectStatus != "available" {
			return fmt.Errorf("%w: chunk %s object status is %s", ErrObjectUnavailable, chunk.ID, chunk.ObjectStatus)
		}
		if chunk.StartCursor < 0 || chunk.EndCursor <= chunk.StartCursor || strings.TrimSpace(chunk.ObjectKey) == "" {
			return fmt.Errorf("%w: chunk %s has invalid metadata", ErrCursorIntegrity, chunk.ID)
		}
		if index == 0 {
			if chunk.StartCursor > startCursor || chunk.EndCursor <= startCursor {
				return fmt.Errorf("%w: first chunk does not cover cursor %d", ErrCursorIntegrity, startCursor)
			}
		} else if chunk.StartCursor != previousEnd {
			return fmt.Errorf(
				"%w: chunk boundary %d follows %d",
				ErrCursorIntegrity,
				chunk.StartCursor,
				previousEnd,
			)
		}
		if chunk.StartCursor > covered {
			return fmt.Errorf("%w: gap before cursor %d", ErrCursorIntegrity, chunk.StartCursor)
		}
		if chunk.EndCursor > covered {
			covered = chunk.EndCursor
		}
		previousEnd = chunk.EndCursor
	}
	if covered < endCursor {
		return fmt.Errorf("%w: chunk coverage ends at %d before %d", ErrCursorIntegrity, covered, endCursor)
	}
	return nil
}

func (r *Reader) scanObject(
	ctx context.Context,
	objectKey string,
	startCursor int64,
	expectedEndCursor int64,
	expectedSHA256 string,
	rangeStartCursor int64,
	rangeEndCursor int64,
	useIndexedRange bool,
	fallbackTime *time.Time,
	visit func(Event) error,
	copyTo io.Writer,
) (sessionsync.ContentScanResult, error) {
	object, err := r.store.Download(ctx, objectKey)
	if err != nil {
		return sessionsync.ContentScanResult{}, fmt.Errorf("%w: %s: %v", ErrObjectUnavailable, objectKey, err)
	}
	parseStartCursor := startCursor
	parseEndCursor := expectedEndCursor
	verifySHA256 := expectedSHA256
	input := io.Reader(&contextReader{ctx: ctx, reader: object})
	if useIndexedRange && (rangeStartCursor > startCursor || expectedEndCursor < 0 || rangeEndCursor < expectedEndCursor) {
		if seeker, ok := object.(io.Seeker); ok {
			offset := rangeStartCursor - startCursor
			if offset < 0 || rangeEndCursor <= rangeStartCursor {
				_ = object.Close()
				return sessionsync.ContentScanResult{}, ErrInvalidRequest
			}
			if _, err := seeker.Seek(offset, io.SeekStart); err != nil {
				_ = object.Close()
				return sessionsync.ContentScanResult{}, fmt.Errorf("%w: seek %s: %v", ErrObjectUnavailable, objectKey, err)
			}
			input = io.LimitReader(
				&contextReader{ctx: ctx, reader: object},
				rangeEndCursor-rangeStartCursor,
			)
			parseStartCursor = rangeStartCursor
			parseEndCursor = rangeEndCursor
			verifySHA256 = ""
		}
	}
	hasher := sha256.New()
	if copyTo != nil {
		input = io.TeeReader(input, copyTo)
	}
	if verifySHA256 != "" {
		input = io.TeeReader(input, hasher)
	}
	var visitErr error
	scanResult, scanErr := sessionsync.ScanContentChunk(input, parseStartCursor, fallbackTime, func(event Event) error {
		if err := visit(event); err != nil {
			visitErr = err
			return err
		}
		return nil
	})
	closeErr := object.Close()
	if visitErr != nil {
		return sessionsync.ContentScanResult{}, visitErr
	}
	if scanErr != nil {
		if errors.Is(scanErr, context.Canceled) || errors.Is(scanErr, context.DeadlineExceeded) {
			return sessionsync.ContentScanResult{}, scanErr
		}
		if errors.Is(scanErr, sessionsync.ErrIncompleteJSONLLine) || errors.Is(scanErr, sessionsync.ErrContentLineTooLarge) {
			return sessionsync.ContentScanResult{}, fmt.Errorf("%w: %s: %v", ErrContentIntegrity, objectKey, scanErr)
		}
		return sessionsync.ContentScanResult{}, fmt.Errorf("%w: %s: %v", ErrObjectUnavailable, objectKey, scanErr)
	}
	if closeErr != nil {
		return sessionsync.ContentScanResult{}, fmt.Errorf("%w: close %s: %v", ErrObjectUnavailable, objectKey, closeErr)
	}
	if parseEndCursor >= 0 && scanResult.EndCursor != parseEndCursor {
		return sessionsync.ContentScanResult{}, fmt.Errorf(
			"%w: object %s ends at %d, chunk ends at %d",
			ErrCursorIntegrity,
			objectKey,
			scanResult.EndCursor,
			parseEndCursor,
		)
	}
	if verifySHA256 != "" {
		actual := hex.EncodeToString(hasher.Sum(nil))
		if actual != verifySHA256 {
			return sessionsync.ContentScanResult{}, fmt.Errorf(
				"%w: object %s sha256 %s, expected %s",
				ErrContentIntegrity,
				objectKey,
				actual,
				verifySHA256,
			)
		}
	}
	return scanResult, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type expectedMatcher struct {
	iterator expectedEventIterator
}

func (m *expectedMatcher) match(event Event) error {
	if !m.iterator.Next() {
		if err := m.iterator.Err(); err != nil {
			return err
		}
		return fmt.Errorf(
			"%w: raw event [%d,%d) is absent from the lightweight index",
			ErrContentIntegrity,
			event.SourceStartCursor,
			event.SourceEndCursor,
		)
	}
	expected := m.iterator.Event()
	if expected.StartCursor != event.SourceStartCursor ||
		expected.EndCursor != event.SourceEndCursor ||
		expected.ContentSHA256 != event.ContentSHA256 {
		return fmt.Errorf(
			"%w: raw event [%d,%d) sha256 %s, index event [%d,%d) sha256 %s",
			ErrContentIntegrity,
			event.SourceStartCursor,
			event.SourceEndCursor,
			event.ContentSHA256,
			expected.StartCursor,
			expected.EndCursor,
			expected.ContentSHA256,
		)
	}
	return nil
}

func (m *expectedMatcher) finish() error {
	if m.iterator.Next() {
		expected := m.iterator.Event()
		return fmt.Errorf(
			"%w: indexed event [%d,%d) is absent from the raw content",
			ErrContentIntegrity,
			expected.StartCursor,
			expected.EndCursor,
		)
	}
	return m.iterator.Err()
}

func nullableTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time
	return &result
}
