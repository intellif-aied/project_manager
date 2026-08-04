package main

import "testing"

func TestAutoSyncLockIdentityIsStableAndDistinct(t *testing.T) {
	t.Parallel()

	tick := autoSyncLockIdentity("tick.lock")
	if tick != autoSyncLockIdentity("tick.lock") {
		t.Fatal("same lock path must produce the same identity")
	}
	if tick == autoSyncLockIdentity("run.lock") {
		t.Fatal("different lock paths must produce different identities")
	}
}
