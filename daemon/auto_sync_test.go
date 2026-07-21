package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAutoSyncStatusReportsDisabledWithoutConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var output bytes.Buffer
	code := cmdAutoSync([]string{"status"}, strings.NewReader(""), &output)

	if code != 0 {
		t.Fatalf("status exit code = %d, want 0", code)
	}
	want := "自动 Session 同步：未开启\n开启方式：aida auto-sync enable\n"
	if output.String() != want {
		t.Fatalf("status output = %q, want %q", output.String(), want)
	}
}

func TestAutoSyncSetTimeUpdatesFutureSchedule(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveAutoSyncConfig(autoSyncConfig{
		SchemaVersion: 1,
		Enabled:       true,
		DailyTime:     "18:00",
	}); err != nil {
		t.Fatal(err)
	}
	oldNow := autoSyncNow
	autoSyncNow = func() time.Time {
		return time.Date(2026, 7, 21, 18, 30, 0, 0, time.UTC)
	}
	t.Cleanup(func() { autoSyncNow = oldNow })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"set-time"}, strings.NewReader("19:00\n"), &output); code != 0 {
		t.Fatalf("set-time exit code = %d, want 0; output=%q", code, output.String())
	}
	want := "当前每日同步时间：18:00（本机时间）\n" +
		"请输入新的每日同步时间（HH:MM，按本机时间）：\n" +
		"每日同步时间已更新为 19:00（本机时间）\n"
	if output.String() != want {
		t.Fatalf("set-time output = %q, want %q", output.String(), want)
	}

	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DailyTime != "19:00" || cfg.ScheduleEffectiveAt != "2026-07-21T19:00:00Z" {
		t.Fatalf("saved config = %+v", cfg)
	}
}

func TestAutoSyncSetTimeRepromptsAfterInvalidValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveAutoSyncConfig(autoSyncConfig{SchemaVersion: 1, Enabled: true, DailyTime: "18:00"}); err != nil {
		t.Fatal(err)
	}
	oldNow := autoSyncNow
	autoSyncNow = func() time.Time { return time.Date(2026, 7, 21, 18, 30, 0, 0, time.UTC) }
	t.Cleanup(func() { autoSyncNow = oldNow })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"set-time"}, strings.NewReader("bad\n19:00\n"), &output); code != 0 {
		t.Fatalf("set-time exit code = %d; output=%q", code, output.String())
	}
	if strings.Count(output.String(), "请输入新的每日同步时间") != 2 {
		t.Fatalf("set-time did not reprompt: %q", output.String())
	}
	if !strings.Contains(output.String(), "时间格式无效") {
		t.Fatalf("set-time output = %q", output.String())
	}
}

func TestAutoSyncSetPastTimeCanStartTomorrow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveAutoSyncConfig(autoSyncConfig{
		SchemaVersion: 1,
		Enabled:       true,
		DailyTime:     "17:00",
	}); err != nil {
		t.Fatal(err)
	}
	oldNow := autoSyncNow
	autoSyncNow = func() time.Time {
		return time.Date(2026, 7, 21, 18, 30, 0, 0, time.UTC)
	}
	t.Cleanup(func() { autoSyncNow = oldNow })
	oldChooser := autoSyncChoosePastTime
	autoSyncChoosePastTime = func(string, io.Reader, io.Writer) (autoSyncStartChoice, bool, error) {
		return autoSyncStartTomorrow, false, nil
	}
	t.Cleanup(func() { autoSyncChoosePastTime = oldChooser })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"set-time"}, strings.NewReader("18:00\n"), &output); code != 0 {
		t.Fatalf("set-time exit code = %d, want 0; output=%q", code, output.String())
	}
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ScheduleEffectiveAt != "2026-07-22T18:00:00Z" {
		t.Fatalf("schedule effective at = %q, want next day", cfg.ScheduleEffectiveAt)
	}
	if !strings.Contains(output.String(), "首次自动同步：明天 18:00（本机时间）") {
		t.Fatalf("set-time output = %q", output.String())
	}
}

func TestAutoSyncEnablePersistsFutureScheduleAfterConfirmation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldChecker := autoSyncCheckBackgroundSupport
	autoSyncCheckBackgroundSupport = func() error { return nil }
	t.Cleanup(func() { autoSyncCheckBackgroundSupport = oldChecker })
	oldNow := autoSyncNow
	autoSyncNow = func() time.Time {
		return time.Date(2026, 7, 21, 18, 30, 0, 0, time.UTC)
	}
	t.Cleanup(func() { autoSyncNow = oldNow })
	oldStarter := autoSyncStartBackground
	autoSyncStartBackground = func() error { return nil }
	t.Cleanup(func() { autoSyncStartBackground = oldStarter })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"enable"}, strings.NewReader("y\n19:00\n"), &output); code != 0 {
		t.Fatalf("enable exit code = %d, want 0; output=%q", code, output.String())
	}
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.DailyTime != "19:00" || cfg.ScheduleEffectiveAt != "2026-07-21T19:00:00Z" {
		t.Fatalf("saved config = %+v", cfg)
	}
	if !strings.Contains(output.String(), "自动 Session 同步已开启") ||
		!strings.Contains(output.String(), "每天 19:00（本机时间）自动上传全部 Session") {
		t.Fatalf("enable output = %q", output.String())
	}
}

func TestAutoSyncEnableRejectsLinuxWithoutSystemdUserManager(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldChecker := autoSyncCheckBackgroundSupport
	autoSyncCheckBackgroundSupport = func() error { return errAutoSyncSystemdUnavailable }
	t.Cleanup(func() { autoSyncCheckBackgroundSupport = oldChecker })

	var output bytes.Buffer
	code := cmdAutoSync([]string{"enable"}, strings.NewReader("y\n19:00\n"), &output)

	if code != 2 {
		t.Fatalf("unsupported Linux enable exit code = %d, want 2", code)
	}
	want := "当前 Linux 环境缺少可用的 systemd user manager，暂不支持自动 Session 同步\n"
	if output.String() != want {
		t.Fatalf("unsupported Linux output = %q, want %q", output.String(), want)
	}
	if _, err := os.Stat(autoSyncConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("unsupported Linux enable wrote config: %v", err)
	}
}

func TestAutoSyncEnableRejectsNonInteractiveInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var output bytes.Buffer
	code := cmdAutoSyncCLI([]string{"enable"}, strings.NewReader("y\n19:00\n"), &output, false)

	if code != 2 {
		t.Fatalf("non-interactive enable exit code = %d, want 2", code)
	}
	if output.String() != "开启自动 Session 同步需要交互式终端，请运行 aida auto-sync enable\n" {
		t.Fatalf("non-interactive enable output = %q", output.String())
	}
	if _, err := os.Stat(autoSyncConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("non-interactive enable wrote config: %v", err)
	}
}

func TestLoginNonInteractiveSkipsAutoSyncPrompt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	var output bytes.Buffer
	finishLoginAutoSync(strings.NewReader(""), &output, false)

	want := "自动 Session 同步未开启，可在交互式终端运行 aida auto-sync enable\n"
	if output.String() != want {
		t.Fatalf("post-login output = %q, want %q", output.String(), want)
	}
	if _, err := os.Stat(autoSyncConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("non-interactive login wrote AutoSync config: %v", err)
	}
}

func TestRunRoutesAutoSyncStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if code := run([]string{"auto-sync", "status"}); code != 0 {
		t.Fatalf("aida auto-sync status exit code = %d, want 0", code)
	}
}

func TestRunStatusPrintsAutoSyncBeforeRequiredUpdateFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveAutoSyncConfig(autoSyncConfig{SchemaVersion: 1, Enabled: true, DailyTime: "18:00"}); err != nil {
		t.Fatal(err)
	}
	if err := saveConfig(&Config{
		ReleaseURL:       "http://127.0.0.1:1",
		LastKnownVersion: "0.1.15",
	}); err != nil {
		t.Fatal(err)
	}
	oldVersion := Version
	Version = "0.1.14"
	t.Cleanup(func() { Version = oldVersion })

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	code := run([]string{"status"})
	writer.Close()
	os.Stdout = oldStdout
	output, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatal(err)
	}

	if code != 3 {
		t.Fatalf("status exit code = %d, want update failure 3", code)
	}
	if !strings.Contains(string(output), "自动 Session 同步：已开启\n每日同步时间：18:00（本机时间）") {
		t.Fatalf("status output = %q", string(output))
	}
}

func TestAutoSyncStatusReportsEnabledSchedule(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveAutoSyncConfig(autoSyncConfig{
		SchemaVersion: 1,
		Enabled:       true,
		DailyTime:     "18:00",
	}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	code := cmdAutoSync([]string{"status"}, strings.NewReader(""), &output)

	if code != 0 {
		t.Fatalf("status exit code = %d, want 0", code)
	}
	want := "自动 Session 同步：已开启\n每日同步时间：18:00（本机时间）\n"
	if output.String() != want {
		t.Fatalf("status output = %q, want %q", output.String(), want)
	}
}

func TestAutoSyncDisablePersistsUserChoice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldStopper := autoSyncStopBackground
	autoSyncStopBackground = func() error { return nil }
	t.Cleanup(func() { autoSyncStopBackground = oldStopper })
	if err := saveAutoSyncConfig(autoSyncConfig{
		SchemaVersion: 1,
		Enabled:       true,
		DailyTime:     "18:00",
	}); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"disable"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("disable exit code = %d, want 0", code)
	}
	want := "自动 Session 同步已关闭\n你仍可使用 aida upload 或 aida upload --all 手动上传 Session\n"
	if output.String() != want {
		t.Fatalf("disable output = %q, want %q", output.String(), want)
	}

	output.Reset()
	if code := cmdAutoSync([]string{"status"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("status exit code = %d, want 0", code)
	}
	if !strings.Contains(output.String(), "自动 Session 同步：未开启") {
		t.Fatalf("status output after disable = %q", output.String())
	}
}

func TestAutoSyncDisableIsIdempotentWhenAlreadyDisabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldStopper := autoSyncStopBackground
	stops := 0
	autoSyncStopBackground = func() error {
		stops++
		return nil
	}
	t.Cleanup(func() { autoSyncStopBackground = oldStopper })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"disable"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("disable exit code = %d, want 0", code)
	}
	if output.String() != "自动 Session 同步当前已经处于未开启状态\n" {
		t.Fatalf("disable output = %q", output.String())
	}
	if stops != 0 {
		t.Fatalf("background stops = %d, want 0", stops)
	}
}

func TestAutoSyncDueFollowsDailySuccessAndRetryRules(t *testing.T) {
	now := time.Date(2026, 7, 21, 18, 30, 0, 0, time.UTC)
	baseConfig := autoSyncConfig{
		SchemaVersion:       1,
		Enabled:             true,
		DailyTime:           "18:00",
		ScheduleEffectiveAt: "2026-07-20T18:00:00Z",
	}

	tests := []struct {
		name     string
		now      time.Time
		cfg      autoSyncConfig
		schedule autoSyncSchedule
		want     bool
	}{
		{name: "due after daily time", now: now, cfg: baseConfig, want: true},
		{name: "not due before daily time", now: time.Date(2026, 7, 21, 17, 59, 0, 0, time.UTC), cfg: baseConfig, want: false},
		{name: "not due after success today", now: now, cfg: baseConfig, schedule: autoSyncSchedule{LastSuccessDate: "2026-07-21"}, want: false},
		{name: "not due within retry interval", now: now, cfg: baseConfig, schedule: autoSyncSchedule{LastAttemptAt: "2026-07-21T18:29:30Z"}, want: false},
		{name: "not due before schedule becomes effective", now: now, cfg: autoSyncConfig{SchemaVersion: 1, Enabled: true, DailyTime: "18:00", ScheduleEffectiveAt: "2026-07-22T18:00:00Z"}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := autoSyncDue(test.now, test.cfg, test.schedule); got != test.want {
				t.Fatalf("autoSyncDue() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestAutoSyncRunOnceRecordsDailySuccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 7, 21, 18, 30, 0, 0, time.UTC)
	if err := saveAutoSyncConfig(autoSyncConfig{
		SchemaVersion:       1,
		Enabled:             true,
		DailyTime:           "18:00",
		ScheduleEffectiveAt: "2026-07-20T18:00:00Z",
	}); err != nil {
		t.Fatal(err)
	}

	executions := 0
	execute := func() int {
		executions++
		return 0
	}
	if ran, err := runAutoSyncOnce(now, execute); err != nil || !ran {
		t.Fatalf("first run: ran=%t err=%v", ran, err)
	}
	if ran, err := runAutoSyncOnce(now.Add(2*time.Minute), execute); err != nil || ran {
		t.Fatalf("second run: ran=%t err=%v", ran, err)
	}
	if executions != 1 {
		t.Fatalf("executions = %d, want 1", executions)
	}
	schedule, err := loadAutoSyncSchedule()
	if err != nil {
		t.Fatal(err)
	}
	if schedule.LastSuccessDate != "2026-07-21" {
		t.Fatalf("last success date = %q", schedule.LastSuccessDate)
	}
}

func TestAutoSyncDaemonRunUsesHiddenSchedulerEntry(t *testing.T) {
	oldRunner := autoSyncRunDaemon
	called := false
	autoSyncRunDaemon = func() int {
		called = true
		return 0
	}
	t.Cleanup(func() { autoSyncRunDaemon = oldRunner })

	if code := cmdAutoSync([]string{"daemon-run"}, strings.NewReader(""), io.Discard); code != 0 {
		t.Fatalf("daemon-run exit code = %d, want 0", code)
	}
	if !called {
		t.Fatal("daemon-run did not call scheduler")
	}
}

func TestAutoSyncExecuteContinuesUploadWhenUpdateFails(t *testing.T) {
	uploaded := false
	var output bytes.Buffer
	code := runAutoSyncExecute(
		func() error { return errors.New("release unavailable") },
		func() int {
			uploaded = true
			return 0
		},
		&output,
	)

	if code != 0 {
		t.Fatalf("execute exit code = %d, want 0", code)
	}
	if !uploaded {
		t.Fatal("upload was skipped after update failure")
	}
	if !strings.Contains(output.String(), "版本检查或更新失败") {
		t.Fatalf("execute output = %q", output.String())
	}
}
