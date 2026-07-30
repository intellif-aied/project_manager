package reportsource

import "testing"

func TestCanonicalSourceIdentityIsStableAndUsesImmutableRange(t *testing.T) {
	first := SelectionItem{
		SliceID: "11111111-1111-4111-8111-111111111111", SessionID: "22222222-2222-4222-8222-222222222222",
		SourceID: "33333333-3333-4333-8333-333333333333", SessionRef: "session-a", AgentType: "codex",
		GenerationID: "44444444-4444-4444-8444-444444444444", ProjectionRevision: "55555555-5555-4555-8555-555555555555",
		ContentEpoch: 1, StartCursor: 10, EndCursor: 20,
	}
	second := SelectionItem{
		SliceID: "66666666-6666-4666-8666-666666666666", SessionID: "77777777-7777-4777-8777-777777777777",
		SourceID: "88888888-8888-4888-8888-888888888888", SessionRef: "session-b", AgentType: "claude",
		GenerationID: "99999999-9999-4999-8999-999999999999", ProjectionRevision: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa",
		ContentEpoch: 2, StartCursor: 30, EndCursor: 40,
	}

	canonical, hash, err := CanonicalSourceIdentity([]SelectionItem{first, second, first})
	if err != nil {
		t.Fatal(err)
	}
	reordered, reorderedHash, err := CanonicalSourceIdentity([]SelectionItem{second, first})
	if err != nil {
		t.Fatal(err)
	}
	if len(canonical) != 2 || len(reordered) != 2 || len(hash) != 64 || hash != reorderedHash {
		t.Fatalf("canonical=%#v reordered=%#v hash=%s reordered_hash=%s", canonical, reordered, hash, reorderedHash)
	}

	second.EndCursor++
	_, changedHash, err := CanonicalSourceIdentity([]SelectionItem{first, second})
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == hash {
		t.Fatal("changing the immutable event range must change the source identity")
	}
}

func TestCanonicalSourceIdentitySupportsSourceFreeRuns(t *testing.T) {
	items, hash, err := CanonicalSourceIdentity(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || len(hash) != 64 {
		t.Fatalf("items=%#v hash=%s", items, hash)
	}
}
