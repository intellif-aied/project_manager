package config

import "testing"

func TestSessionWorkersDefaultOffAndCacheVariantUnspecified(t *testing.T) {
	t.Setenv("AIDA_SESSION_SYNC_CONTENT_WORKER_ENABLED", "")
	t.Setenv("AIDA_SESSION_SYNC_USAGE_WORKER_ENABLED", "")
	t.Setenv("AIDA_CLAUDE_CACHE_WRITE_VARIANT", "")
	loaded := Load()
	if loaded.SessionSyncContentWorkerEnabled {
		t.Fatal("content worker must default off")
	}
	if loaded.SessionSyncUsageWorkerEnabled {
		t.Fatal("usage worker must default off")
	}
	if loaded.ClaudeCacheWriteVariant != "" {
		t.Fatalf("cache variant=%q", loaded.ClaudeCacheWriteVariant)
	}
}
