package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSessionIgnoreConfigRoundTripUsesPrivateFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	config := sessionIgnoreConfig{Sessions: []ignoredSession{{AgentType: "codex", SessionRef: "session-1"}}, Directories: []string{"/work/private"}}
	if err := saveSessionIgnoreConfig(config); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(sessionIgnorePath())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("permissions=%o", info.Mode().Perm())
	}
	loaded, err := loadSessionIgnoreConfig()
	if err != nil || len(loaded.Sessions) != 1 || len(loaded.Directories) != 1 {
		t.Fatalf("config=%+v err=%v", loaded, err)
	}
}

func TestFilterIgnoredSessionGroupsSkipsWholeCodexGroup(t *testing.T) {
	root := &SessionInfo{SessionRef: "root", AgentType: "codex", Cwd: "/work/public"}
	child := &SessionInfo{SessionRef: "private-child", AgentType: "codex", Cwd: "/work/private"}
	root.SelectionChildren = []*SessionInfo{child}
	other := &SessionInfo{SessionRef: "other", AgentType: "codex", Cwd: "/work/other"}
	result := filterIgnoredSessionGroups([]*SessionInfo{root, other}, sessionIgnoreConfig{Sessions: []ignoredSession{{AgentType: "codex", SessionRef: "private-child"}}})
	if len(result) != 1 || result[0] != other {
		t.Fatalf("result=%+v", result)
	}
}

func TestDirectoryContainsSessionUsesPathBoundary(t *testing.T) {
	base := t.TempDir()
	private := filepath.Join(base, "private")
	if !directoryContainsSession(private, filepath.Join(private, "child")) {
		t.Fatal("expected child directory to match")
	}
	if directoryContainsSession(private, filepath.Join(base, "private-copy")) {
		t.Fatal("prefix sibling must not match")
	}
}
