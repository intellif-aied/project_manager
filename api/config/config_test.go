package config

import "testing"

func TestCacheVariantDefaultsToUnspecified(t *testing.T) {
	t.Setenv("AIDA_CLAUDE_CACHE_WRITE_VARIANT", "")
	loaded := Load()
	if loaded.ClaudeCacheWriteVariant != "" {
		t.Fatalf("cache variant=%q", loaded.ClaudeCacheWriteVariant)
	}
}
