//go:build linux

package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionUploadLockExcludesConcurrentUploadAndRecoversAfterRelease(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	releaseFirst, err := acquireSessionUploadLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireSessionUploadLock(); !errors.Is(err, errSessionUploadBusy) {
		t.Fatalf("second acquire error=%v, want busy", err)
	}
	releaseFirst()

	releaseAgain, err := acquireSessionUploadLock()
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	releaseAgain()
}

func TestUploadAllDoesNotSendRequestsWhenAnotherUploadHoldsLock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := saveConfig(&Config{APIURL: "http://127.0.0.1:1", Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(home, ".claude", "projects", "lock-test")
	if err := os.MkdirAll(sessionDir, 0700); err != nil {
		t.Fatal(err)
	}
	session := `{"type":"user","sessionId":"lock-test","timestamp":"2026-07-21T08:00:00Z","message":{"content":[{"type":"text","text":"test upload lock"}]}}` + "\n"
	if err := os.WriteFile(filepath.Join(sessionDir, "lock-test.jsonl"), []byte(session), 0600); err != nil {
		t.Fatal(err)
	}

	release, err := acquireSessionUploadLock()
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	originalStdout := os.Stdout
	os.Stdout = writer
	code := cmdUpload([]string{"--all"})
	_ = writer.Close()
	os.Stdout = originalStdout
	output, readErr := io.ReadAll(reader)
	_ = reader.Close()
	if readErr != nil {
		t.Fatal(readErr)
	}

	if code != 75 {
		t.Fatalf("exit code=%d output=%q", code, string(output))
	}
	if !strings.Contains(string(output), "已有 Session 上传正在进行，请稍后再试") {
		t.Fatalf("output=%q", string(output))
	}
}
