package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUploadStateV1MigratesToPersonalNamespace(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	path := uploadStatePath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	legacyKey := "codex\nsession-1\ncodex:session-1:main"
	legacy := uploadStateFile{
		Version: 1,
		Sources: map[string]localUploadState{
			legacyKey: {SessionRef: "session-1", AgentType: "codex", SourceKey: "codex:session-1:main"},
		},
	}
	content, _ := json.Marshal(legacy)
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	state, err := loadUploadStates()
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != uploadStateVersion {
		t.Fatalf("version=%d", state.Version)
	}
	personalKey := uploadStateKey("codex", "session-1", "codex:session-1:main")
	if _, ok := state.Sources[personalKey]; !ok {
		t.Fatalf("personal key missing: %q", personalKey)
	}
	if _, ok := state.Sources[uploadStateKeyForMode(uploadModeTeam, "codex", "session-1", "codex:session-1:main")]; ok {
		t.Fatal("legacy state leaked into team namespace")
	}
}

func TestTeamPrepareSendsModeAndReturnsUnmappedWithoutChunk(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/session-syncs/prepare" {
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
		var request prepareBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.UploadMode != uploadModeTeam {
			t.Fatalf("upload mode=%q", request.UploadMode)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"session_ref":"team-unmapped","source_key":"codex:team-unmapped:main","action":"rejected","error_code":"TEAM_DIRECTORY_UNMAPPED"}]}`))
	}))
	defer server.Close()
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, "team-unmapped.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	session := &SessionInfo{SessionRef: "team-unmapped", AgentType: "codex", FilePath: path, Cwd: "/workspace/unmapped"}
	results, err := uploadSessionGroupIncrementalWithMode(
		&Config{APIURL: server.URL, Token: "token"},
		[]sessionWithFile{{info: session, filePath: path}}, session.SessionRef, uploadModeTeam,
	)
	if err != nil || len(results) != 1 || results[0].ErrorCode != "TEAM_DIRECTORY_UNMAPPED" {
		t.Fatalf("results=%+v err=%v", results, err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d want=1 prepare only", requests)
	}
}

func TestTeamSyncUnresolvedSnapshotReplacesAfterCompleteScan(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := updateTeamSyncUnresolved(map[string]int{"/workspace/a": 2, "/workspace/b": 1}, true); err != nil {
		t.Fatal(err)
	}
	if err := updateTeamSyncUnresolved(map[string]int{"/workspace/b": 3}, true); err != nil {
		t.Fatal(err)
	}
	state, err := loadTeamSyncUnresolved()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Directories) != 1 || state.Directories[0].Path != "/workspace/b" || state.Directories[0].SessionCount != 3 {
		t.Fatalf("state=%+v", state)
	}
	var output bytes.Buffer
	if code := cmdTeamSyncLog(&output); code != 0 {
		t.Fatalf("code=%d output=%q", code, output.String())
	}
	if !strings.Contains(output.String(), "/workspace/b（3 个 Session") || !strings.Contains(output.String(), "我的 Token") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestCompleteTeamSyncScanClearsStaleDirectoriesDespiteOtherFailures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := updateTeamSyncUnresolved(map[string]int{"/workspace/stale": 4}, true); err != nil {
		t.Fatal(err)
	}
	if err := completeTeamSyncScan(map[string]int{}); err != nil {
		t.Fatal(err)
	}
	state, err := loadTeamSyncUnresolved()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Directories) != 0 {
		t.Fatalf("stale directories remained: %+v", state.Directories)
	}
}

func TestWriteSessionUploadErrorsIncludesConcreteReason(t *testing.T) {
	var output bytes.Buffer
	writeSessionUploadErrors(&output, []string{"prepare rejected: TEAM_CONTEXT_CHANGED"})
	if !strings.Contains(output.String(), "原因：prepare rejected: TEAM_CONTEXT_CHANGED") {
		t.Fatalf("output=%q", output.String())
	}
}

func TestFormatTeamUploadSummaryShowsGroupsAndSessions(t *testing.T) {
	got := formatTeamUploadSummary(13, 31, 143, 485, 0, 0, false)
	want := "上传完成：成功 13 组（31 个 Session），待配置 143 组（485 个 Session）；运行 aida log 查看目录。"
	if got != want {
		t.Fatalf("summary=%q want=%q", got, want)
	}

	got = formatTeamUploadSummary(2, 4, 1, 3, 1, 2, true)
	want = "上传完成：成功 2 组（4 个 Session），待配置 1 组（3 个 Session），失败 1 组（2 个 Session）"
	if got != want {
		t.Fatalf("failure summary=%q want=%q", got, want)
	}
}

func TestAutoSyncConfigV2MigratesToPersonalMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(autoSyncDir(), 0700); err != nil {
		t.Fatal(err)
	}
	content := []byte(`{"schema_version":2,"enabled":true,"daily_time":"18:00","timezone":"Asia/Shanghai","schedule_effective_at":"2026-07-28T18:00:00+08:00"}`)
	if err := os.WriteFile(autoSyncConfigPath(), content, 0600); err != nil {
		t.Fatal(err)
	}
	oldNow := autoSyncNow
	autoSyncNow = func() time.Time { return time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { autoSyncNow = oldNow })
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != autoSyncConfigSchemaVersion || cfg.Mode != uploadModePersonal {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestAutoSyncEnableTeamPersistsTeamMode(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldChecker := autoSyncCheckBackgroundSupport
	autoSyncCheckBackgroundSupport = func() error { return nil }
	t.Cleanup(func() { autoSyncCheckBackgroundSupport = oldChecker })
	oldStarter := autoSyncStartBackground
	autoSyncStartBackground = func() error { return nil }
	t.Cleanup(func() { autoSyncStartBackground = oldStarter })
	oldEnableChooser := autoSyncChooseEnable
	autoSyncChooseEnable = func(io.Reader, io.Writer) (bool, bool, error) { return true, false, nil }
	t.Cleanup(func() { autoSyncChooseEnable = oldEnableChooser })
	oldTimeChooser := autoSyncChooseTime
	autoSyncChooseTime = func(string, io.Reader, io.Writer) (string, bool, error) { return "19:00", false, nil }
	t.Cleanup(func() { autoSyncChooseTime = oldTimeChooser })
	oldNow := autoSyncNow
	autoSyncNow = func() time.Time { return time.Date(2026, 7, 28, 1, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { autoSyncNow = oldNow })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"enable", "--team"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("code=%d output=%q", code, output.String())
	}
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.Mode != uploadModeTeam {
		t.Fatalf("cfg=%+v", cfg)
	}
	if !strings.Contains(output.String(), "模式：团队") {
		t.Fatalf("output=%q", output.String())
	}
}
