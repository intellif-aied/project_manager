package sessionsync

import (
	"bytes"
	"testing"
)

func TestCORE005ServerPrefixStateResumesWithoutTrustingClientHash(t *testing.T) {
	first := []byte("{\"event\":1}\n")
	second := []byte("{\"event\":2}\n")
	hasher, err := resumePrefixHasher(StoredPrefixCheckpoint{})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = hasher.Write(first)
	checkpoint, err := snapshotPrefixHasher(hasher)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint.ExpectedCursor = int64(len(first))

	resumed, err := resumePrefixHasher(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = resumed.Write(second)
	got, err := snapshotPrefixHasher(resumed)
	if err != nil {
		t.Fatal(err)
	}
	want := HashBytes(bytes.Join([][]byte{first, second}, nil))
	if got.Hash != want {
		t.Fatalf("resumed prefix hash=%s, want=%s", got.Hash, want)
	}
}

func TestCORE005CorruptServerPrefixStateIsRejected(t *testing.T) {
	checkpoint := StoredPrefixCheckpoint{
		ExpectedCursor: 10,
		Hash:           HashBytes([]byte("accepted")),
		Algorithm:      PrefixCheckpointAlgorithm,
		State:          []byte("corrupt"),
		StateFormat:    PrefixCheckpointStateFormat,
	}
	if _, err := resumePrefixHasher(checkpoint); err == nil {
		t.Fatal("corrupt prefix state was accepted")
	}
}
