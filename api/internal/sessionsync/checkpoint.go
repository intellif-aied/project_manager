package sessionsync

import (
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"errors"
	"hash"
)

const PrefixCheckpointStateFormat = "go-sha256-v1"

type StoredPrefixCheckpoint struct {
	ExpectedCursor int64
	Hash           string
	Algorithm      string
	State          []byte
	StateFormat    string
}

func resumePrefixHasher(checkpoint StoredPrefixCheckpoint) (hash.Hash, error) {
	hasher := sha256.New()
	if checkpoint.ExpectedCursor == 0 {
		return hasher, nil
	}
	if checkpoint.ExpectedCursor < 0 || checkpoint.Algorithm != PrefixCheckpointAlgorithm ||
		checkpoint.StateFormat != PrefixCheckpointStateFormat || !validSHA256(checkpoint.Hash) || len(checkpoint.State) == 0 {
		return nil, errors.New("invalid stored prefix checkpoint")
	}
	unmarshaler, ok := hasher.(encoding.BinaryUnmarshaler)
	if !ok {
		return nil, errors.New("sha256 implementation cannot restore checkpoint state")
	}
	if err := unmarshaler.UnmarshalBinary(checkpoint.State); err != nil {
		return nil, err
	}
	if hex.EncodeToString(hasher.Sum(nil)) != checkpoint.Hash {
		return nil, errors.New("stored prefix state does not match checkpoint hash")
	}
	return hasher, nil
}

func snapshotPrefixHasher(hasher hash.Hash) (StoredPrefixCheckpoint, error) {
	marshaler, ok := hasher.(encoding.BinaryMarshaler)
	if !ok {
		return StoredPrefixCheckpoint{}, errors.New("sha256 implementation cannot persist checkpoint state")
	}
	state, err := marshaler.MarshalBinary()
	if err != nil {
		return StoredPrefixCheckpoint{}, err
	}
	return StoredPrefixCheckpoint{
		Hash:        hex.EncodeToString(hasher.Sum(nil)),
		Algorithm:   PrefixCheckpointAlgorithm,
		State:       state,
		StateFormat: PrefixCheckpointStateFormat,
	}, nil
}
