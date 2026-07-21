package main

import (
	"bytes"
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

func TestRunRoutesAutoSyncStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if code := run([]string{"auto-sync", "status"}); code != 0 {
		t.Fatalf("aida auto-sync status exit code = %d, want 0", code)
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
