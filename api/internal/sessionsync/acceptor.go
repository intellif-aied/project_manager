package sessionsync

import (
	"context"
	"errors"
	"io"
)

const (
	JobIndexContentChunk         = "index_content_chunk"
	JobParseUsageChunk           = "parse_usage_chunk"
	JobRebuildContentRevision    = "rebuild_content_revision"
	JobRebuildMetricsRevision    = "rebuild_metrics_revision"
	JobBuildMeteringEnvelope     = "build_metering_envelope"
	JobBuildContentSliceDigest   = "build_content_slice_digest"
	JobBuildContentSliceDigestV2 = "build_content_slice_digest_v2"
	JobDeleteObject              = "delete_object"
)

type ChunkSnapshot struct {
	ExpectedCursor        int64
	Existing              *AcceptedChunk
	ContentStatus         ContentStatus
	ContentEpoch          int64
	RestoreWritable       bool
	PrefixCheckpointHash  string
	PrefixAlgorithm       string
	PrefixCheckpointState []byte
	PrefixStateFormat     string
}

type ProcessingJobSpec struct {
	Type         string
	ContentEpoch *int64
}

type CommitChunkRequest struct {
	UserID                   string
	GenerationID             string
	Chunk                    ChunkMetadata
	ObjectKey                string
	ObservedEpoch            int64
	NextPrefixCheckpointHash string
	NextPrefixAlgorithm      string
	NextPrefixState          []byte
	NextPrefixStateFormat    string
	Jobs                     []ProcessingJobSpec
}

type ChunkRepository interface {
	InspectChunk(context.Context, string, string, ChunkMetadata) (ChunkSnapshot, error)
	CommitChunk(context.Context, CommitChunkRequest) (ChunkDecision, error)
}

type VerifiedChunkStore interface {
	PutVerified(context.Context, string, io.Reader, int64, string) error
}

type AcceptChunkRequest struct {
	UserID       string
	GenerationID string
	Chunk        ChunkMetadata
	ContentSize  int64
	Content      io.Reader
}

type ChunkAcceptor struct {
	repository ChunkRepository
	store      VerifiedChunkStore
}

func NewChunkAcceptor(repository ChunkRepository, store VerifiedChunkStore) (*ChunkAcceptor, error) {
	if repository == nil || store == nil {
		return nil, errors.New("chunk repository and verified store are required")
	}
	return &ChunkAcceptor{repository: repository, store: store}, nil
}

func (a *ChunkAcceptor) Accept(ctx context.Context, request AcceptChunkRequest) (ChunkDecision, error) {
	if request.UserID == "" || request.GenerationID == "" || request.Content == nil ||
		request.ContentSize <= 0 || request.Chunk.EndCursor-request.Chunk.StartCursor != request.ContentSize ||
		request.Chunk.StartLine < 1 || request.Chunk.EndLine < request.Chunk.StartLine ||
		(request.Chunk.EventStartAt != nil && request.Chunk.EventEndAt != nil && request.Chunk.EventEndAt.Before(*request.Chunk.EventStartAt)) {
		return invalidChunkDecision(0), nil
	}

	snapshot, err := a.repository.InspectChunk(ctx, request.UserID, request.GenerationID, request.Chunk)
	if err != nil {
		return ChunkDecision{}, err
	}
	if decision := writableContentDecision(snapshot); decision != nil {
		return *decision, nil
	}
	decision := DecideChunk(snapshot.ExpectedCursor, snapshot.Existing, request.Chunk)
	if decision.Status != ChunkAccepted {
		return decision, nil
	}

	objectKey, err := ChunkObjectKey(request.GenerationID, request.Chunk)
	if err != nil {
		return invalidChunkDecision(snapshot.ExpectedCursor), nil
	}
	prefixHasher, err := resumePrefixHasher(StoredPrefixCheckpoint{
		ExpectedCursor: snapshot.ExpectedCursor,
		Hash:           snapshot.PrefixCheckpointHash,
		Algorithm:      snapshot.PrefixAlgorithm,
		State:          snapshot.PrefixCheckpointState,
		StateFormat:    snapshot.PrefixStateFormat,
	})
	if err != nil {
		return invalidCheckpointDecision(snapshot.ExpectedCursor), nil
	}
	if err := a.store.PutVerified(ctx, objectKey, io.TeeReader(request.Content, prefixHasher), request.ContentSize, request.Chunk.ContentSHA256); err != nil {
		return ChunkDecision{}, err
	}
	nextCheckpoint, err := snapshotPrefixHasher(prefixHasher)
	if err != nil {
		return ChunkDecision{}, err
	}

	epoch := snapshot.ContentEpoch
	return a.repository.CommitChunk(ctx, CommitChunkRequest{
		UserID:                   request.UserID,
		GenerationID:             request.GenerationID,
		Chunk:                    request.Chunk,
		ObjectKey:                objectKey,
		ObservedEpoch:            snapshot.ContentEpoch,
		NextPrefixCheckpointHash: nextCheckpoint.Hash,
		NextPrefixAlgorithm:      nextCheckpoint.Algorithm,
		NextPrefixState:          nextCheckpoint.State,
		NextPrefixStateFormat:    nextCheckpoint.StateFormat,
		Jobs: []ProcessingJobSpec{
			{Type: JobIndexContentChunk, ContentEpoch: &epoch},
			{Type: JobParseUsageChunk},
		},
	})
}

func writableContentDecision(snapshot ChunkSnapshot) *ChunkDecision {
	switch snapshot.ContentStatus {
	case ContentUploading, ContentUploadFailed, ContentAvailable:
		return nil
	case ContentCleared:
		if snapshot.RestoreWritable {
			return nil
		}
		decision := ChunkDecision{
			Status:         ChunkRejected,
			ExpectedCursor: snapshot.ExpectedCursor,
			ErrorCode:      ErrorContentCleared,
			NextAction:     "confirm content restoration in Aida before uploading again",
		}
		return &decision
	default:
		decision := ChunkDecision{
			Status:         ChunkRejected,
			ExpectedCursor: snapshot.ExpectedCursor,
			ErrorCode:      ErrorContentNotWritable,
			NextAction:     "reload the session state before uploading",
		}
		return &decision
	}
}

func invalidChunkDecision(expectedCursor int64) ChunkDecision {
	return ChunkDecision{
		Status:         ChunkRejected,
		ExpectedCursor: expectedCursor,
		ErrorCode:      ErrorInvalidChunk,
		NextAction:     "rebuild the chunk from complete JSONL lines and retry",
	}
}

func invalidCheckpointDecision(expectedCursor int64) ChunkDecision {
	return ChunkDecision{
		Status:         ChunkRejected,
		ExpectedCursor: expectedCursor,
		ErrorCode:      ErrorInvalidCheckpoint,
		NextAction:     "rebuild the server checkpoint from accepted objects before accepting more chunks",
	}
}
