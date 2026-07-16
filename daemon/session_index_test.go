package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSessionIndexInvalidatesOlderVersions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	indexPath := filepath.Join(home, ".aida", "session-index.json")
	if err := os.MkdirAll(filepath.Dir(indexPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(indexPath, []byte(`{"version":1,"entries":{"stale":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	index := loadSessionIndex()
	if index.Version != sessionIndexVersion || len(index.Entries) != 0 {
		t.Fatalf("index=%+v current_version=%d", index, sessionIndexVersion)
	}
}
