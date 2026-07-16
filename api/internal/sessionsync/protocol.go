package sessionsync

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

const PrefixCheckpointAlgorithm = "sha256-prefix-v1"

type ContentStatus string

const (
	ContentUploading      ContentStatus = "uploading"
	ContentUploadFailed   ContentStatus = "upload_failed"
	ContentAvailable      ContentStatus = "available"
	ContentClearing       ContentStatus = "clearing"
	ContentClearingFailed ContentStatus = "clearing_failed"
	ContentCleared        ContentStatus = "cleared"
	ContentDeleted        ContentStatus = "deleted"
)

type PrepareAction string

const (
	PrepareAppend          PrepareAction = "append"
	PrepareUnchanged       PrepareAction = "unchanged"
	PrepareRebuildRequired PrepareAction = "rebuild_required"
	PrepareContentCleared  PrepareAction = "content_cleared"
	PrepareRestore         PrepareAction = "restore"
	PrepareRejected        PrepareAction = "rejected"
)

const (
	ErrorCursorGap          = "CURSOR_GAP"
	ErrorCursorOverlap      = "CURSOR_OVERLAP"
	ErrorSourceDiverged     = "SOURCE_DIVERGED"
	ErrorChunkConflict      = "CHUNK_CONTENT_CONFLICT"
	ErrorContentCleared     = "CONTENT_CLEARED"
	ErrorContentNotWritable = "CONTENT_NOT_WRITABLE"
	ErrorInvalidChunk       = "INVALID_CHUNK"
	ErrorInvalidCheckpoint  = "INVALID_CHECKPOINT"
)

type GenerationCheckpoint struct {
	ID                   string
	Status               string
	ExpectedCursor       int64
	PrefixCheckpointHash string
	PrefixAlgorithm      string
}

type PrepareState struct {
	ContentStatus     ContentStatus
	ActiveGeneration  *GenerationCheckpoint
	RestoreGeneration *GenerationCheckpoint
}

type PrepareInput struct {
	LocalSize            int64
	PrefixCheckpointHash string
	PrefixAlgorithm      string
}

type PrepareDecision struct {
	Action     PrepareAction
	Generation *GenerationCheckpoint
	ErrorCode  string
	NextAction string
}

func DecidePrepare(state PrepareState, input PrepareInput) PrepareDecision {
	switch state.ContentStatus {
	case ContentUploading, ContentUploadFailed, ContentAvailable:
	case ContentClearing, ContentClearingFailed:
		return PrepareDecision{
			Action:     PrepareRejected,
			ErrorCode:  ErrorContentNotWritable,
			NextAction: "wait for content clearing to finish or resolve the failed clear operation",
		}
	case ContentDeleted:
		return PrepareDecision{
			Action:     PrepareRejected,
			ErrorCode:  ErrorContentNotWritable,
			NextAction: "the session was permanently deleted and cannot be uploaded",
		}
	case ContentCleared:
		if state.RestoreGeneration != nil {
			return PrepareDecision{Action: PrepareRestore, Generation: state.RestoreGeneration}
		}
		return PrepareDecision{
			Action:     PrepareContentCleared,
			ErrorCode:  ErrorContentCleared,
			NextAction: "confirm content restoration in Aida before uploading again",
		}
	default:
		return PrepareDecision{
			Action:     PrepareRejected,
			ErrorCode:  ErrorContentNotWritable,
			NextAction: "reload the session state before uploading",
		}
	}

	active := state.ActiveGeneration
	if active == nil {
		return PrepareDecision{Action: PrepareRebuildRequired}
	}
	if input.LocalSize < 0 || active.ExpectedCursor < 0 ||
		(active.ExpectedCursor > 0 && !validSHA256(active.PrefixCheckpointHash)) {
		return PrepareDecision{
			Action:     PrepareRejected,
			Generation: active,
			ErrorCode:  ErrorInvalidCheckpoint,
			NextAction: "rescan the local source and prepare again",
		}
	}
	if active.ExpectedCursor == 0 {
		if input.LocalSize == 0 {
			return PrepareDecision{Action: PrepareUnchanged, Generation: active}
		}
		return PrepareDecision{Action: PrepareAppend, Generation: active}
	}
	if input.LocalSize < active.ExpectedCursor {
		return PrepareDecision{
			Action:     PrepareRebuildRequired,
			Generation: active,
			ErrorCode:  ErrorSourceDiverged,
			NextAction: "create a staging generation and upload the source from cursor 0",
		}
	}
	if !validSHA256(input.PrefixCheckpointHash) {
		return PrepareDecision{
			Action:     PrepareRejected,
			Generation: active,
			ErrorCode:  ErrorInvalidCheckpoint,
			NextAction: "calculate the local prefix returned by the server and prepare again",
		}
	}
	if input.PrefixAlgorithm != active.PrefixAlgorithm || input.PrefixCheckpointHash != active.PrefixCheckpointHash {
		return PrepareDecision{
			Action:     PrepareRebuildRequired,
			Generation: active,
			ErrorCode:  ErrorSourceDiverged,
			NextAction: "create a staging generation and upload the source from cursor 0",
		}
	}
	if input.LocalSize == active.ExpectedCursor {
		return PrepareDecision{Action: PrepareUnchanged, Generation: active}
	}
	return PrepareDecision{Action: PrepareAppend, Generation: active}
}

type ChunkMetadata struct {
	StartCursor   int64      `json:"start_cursor"`
	EndCursor     int64      `json:"end_cursor"`
	StartLine     int64      `json:"start_line"`
	EndLine       int64      `json:"end_line"`
	ContentSHA256 string     `json:"content_sha256"`
	EventStartAt  *time.Time `json:"event_start_at,omitempty"`
	EventEndAt    *time.Time `json:"event_end_at,omitempty"`
}

type AcceptedChunk struct {
	StartCursor   int64
	EndCursor     int64
	ContentSHA256 string
}

func acceptedChunkIdentity(chunk ChunkMetadata) AcceptedChunk {
	return AcceptedChunk{
		StartCursor:   chunk.StartCursor,
		EndCursor:     chunk.EndCursor,
		ContentSHA256: chunk.ContentSHA256,
	}
}

type ChunkStatus string

const (
	ChunkAccepted  ChunkStatus = "accepted"
	ChunkDuplicate ChunkStatus = "duplicate"
	ChunkRejected  ChunkStatus = "rejected"
)

type ChunkDecision struct {
	Status         ChunkStatus
	AckedCursor    int64
	ExpectedCursor int64
	ErrorCode      string
	NextAction     string
}

func DecideChunk(expectedCursor int64, existing *AcceptedChunk, incoming ChunkMetadata) ChunkDecision {
	if expectedCursor < 0 || incoming.StartCursor < 0 || incoming.EndCursor <= incoming.StartCursor || !validSHA256(incoming.ContentSHA256) {
		return ChunkDecision{
			Status:         ChunkRejected,
			ExpectedCursor: expectedCursor,
			ErrorCode:      ErrorInvalidChunk,
			NextAction:     "rebuild the chunk from complete JSONL lines and retry",
		}
	}
	if existing != nil {
		if existing.StartCursor != incoming.StartCursor || existing.EndCursor != incoming.EndCursor {
			return ChunkDecision{
				Status:         ChunkRejected,
				ExpectedCursor: expectedCursor,
				ErrorCode:      ErrorInvalidChunk,
				NextAction:     "reload the accepted range from the server and retry",
			}
		}
		if existing.ContentSHA256 == incoming.ContentSHA256 {
			return ChunkDecision{
				Status:         ChunkDuplicate,
				AckedCursor:    incoming.EndCursor,
				ExpectedCursor: expectedCursor,
			}
		}
		return ChunkDecision{
			Status:         ChunkRejected,
			ExpectedCursor: expectedCursor,
			ErrorCode:      ErrorChunkConflict,
			NextAction:     "prepare the source again; do not overwrite the accepted range",
		}
	}
	if incoming.StartCursor < expectedCursor {
		return ChunkDecision{
			Status:         ChunkRejected,
			ExpectedCursor: expectedCursor,
			ErrorCode:      ErrorCursorOverlap,
			NextAction:     "resume from the expected cursor returned by the server",
		}
	}
	if incoming.StartCursor > expectedCursor {
		return ChunkDecision{
			Status:         ChunkRejected,
			ExpectedCursor: expectedCursor,
			ErrorCode:      ErrorCursorGap,
			NextAction:     "upload the missing range beginning at the expected cursor",
		}
	}
	return ChunkDecision{
		Status:         ChunkAccepted,
		AckedCursor:    incoming.EndCursor,
		ExpectedCursor: incoming.EndCursor,
	}
}

func ChunkObjectKey(generationID string, chunk ChunkMetadata) (string, error) {
	if generationID == "" || chunk.StartCursor < 0 || chunk.EndCursor <= chunk.StartCursor || !validSHA256(chunk.ContentSHA256) {
		return "", errors.New("invalid generation or chunk metadata")
	}
	return fmt.Sprintf(
		"session-chunks/%s/%020d-%020d-%s.jsonl",
		url.PathEscape(generationID),
		chunk.StartCursor,
		chunk.EndCursor,
		chunk.ContentSHA256,
	), nil
}

func HashPrefix(r io.Reader, size int64) (string, error) {
	if size < 0 {
		return "", errors.New("prefix size must not be negative")
	}
	h := sha256.New()
	if size == 0 {
		return hex.EncodeToString(h.Sum(nil)), nil
	}
	n, err := io.CopyN(h, r, size)
	if err != nil {
		if errors.Is(err, io.EOF) {
			return "", io.ErrUnexpectedEOF
		}
		return "", err
	}
	if n != size {
		return "", io.ErrUnexpectedEOF
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func HashBytes(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	if value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
