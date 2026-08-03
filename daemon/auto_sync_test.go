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
		return time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	}
	t.Cleanup(func() { autoSyncNow = oldNow })
	oldTimeChooser := autoSyncChooseTime
	autoSyncChooseTime = func(string, io.Reader, io.Writer) (string, bool, error) {
		return "19:00", false, nil
	}
	t.Cleanup(func() { autoSyncChooseTime = oldTimeChooser })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"set-time"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("set-time exit code = %d, want 0; output=%q", code, output.String())
	}
	want := "当前每日同步时间：18:00（北京时间）\n" +
		"同步时间已改为每天 19:00（北京时间）\n"
	if output.String() != want {
		t.Fatalf("set-time output = %q, want %q", output.String(), want)
	}

	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DailyTime != "19:00" || cfg.ScheduleEffectiveAt != "2026-07-21T19:00:00+08:00" || cfg.Timezone != autoSyncTimezone {
		t.Fatalf("saved config = %+v", cfg)
	}
}

func TestAutoSyncSetTimeCancellationKeepsCurrentValue(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveAutoSyncConfig(autoSyncConfig{SchemaVersion: 1, Enabled: true, DailyTime: "18:00"}); err != nil {
		t.Fatal(err)
	}
	oldTimeChooser := autoSyncChooseTime
	autoSyncChooseTime = func(string, io.Reader, io.Writer) (string, bool, error) {
		return "", true, nil
	}
	t.Cleanup(func() { autoSyncChooseTime = oldTimeChooser })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"set-time"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("set-time exit code = %d; output=%q", code, output.String())
	}
	if !strings.Contains(output.String(), "未修改每日同步时间") {
		t.Fatalf("set-time output = %q", output.String())
	}
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DailyTime != "18:00" {
		t.Fatalf("daily time = %q, want unchanged", cfg.DailyTime)
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
		return time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	}
	t.Cleanup(func() { autoSyncNow = oldNow })
	oldChooser := autoSyncChoosePastTime
	autoSyncChoosePastTime = func(string, io.Reader, io.Writer) (autoSyncStartChoice, bool, error) {
		return autoSyncStartTomorrow, false, nil
	}
	t.Cleanup(func() { autoSyncChoosePastTime = oldChooser })
	oldTimeChooser := autoSyncChooseTime
	autoSyncChooseTime = func(string, io.Reader, io.Writer) (string, bool, error) {
		return "18:00", false, nil
	}
	t.Cleanup(func() { autoSyncChooseTime = oldTimeChooser })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"set-time"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("set-time exit code = %d, want 0; output=%q", code, output.String())
	}
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ScheduleEffectiveAt != "2026-07-22T18:00:00+08:00" {
		t.Fatalf("schedule effective at = %q, want next day", cfg.ScheduleEffectiveAt)
	}
	if !strings.Contains(output.String(), "首次自动同步：明天 18:00（北京时间）") {
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
		return time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	}
	t.Cleanup(func() { autoSyncNow = oldNow })
	oldStarter := autoSyncStartBackground
	autoSyncStartBackground = func() error { return nil }
	t.Cleanup(func() { autoSyncStartBackground = oldStarter })
	oldEnableChooser := autoSyncChooseEnable
	autoSyncChooseEnable = func(io.Reader, io.Writer) (bool, bool, error) {
		return true, false, nil
	}
	t.Cleanup(func() { autoSyncChooseEnable = oldEnableChooser })
	oldTimeChooser := autoSyncChooseTime
	autoSyncChooseTime = func(string, io.Reader, io.Writer) (string, bool, error) {
		return "19:00", false, nil
	}
	t.Cleanup(func() { autoSyncChooseTime = oldTimeChooser })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"enable"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("enable exit code = %d, want 0; output=%q", code, output.String())
	}
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.DailyTime != "19:00" || cfg.ScheduleEffectiveAt != "2026-07-21T19:00:00+08:00" {
		t.Fatalf("saved config = %+v", cfg)
	}
	if output.String() != "自动同步已开启\n每天 19:00（北京时间）同步 Session\n" {
		t.Fatalf("enable output = %q", output.String())
	}
}

func TestAutoSyncEnableRejectsWhenNoBackgroundSupervisorIsAvailable(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldChecker := autoSyncCheckBackgroundSupport
	autoSyncCheckBackgroundSupport = func() error { return errAutoSyncSystemdUnavailable }
	t.Cleanup(func() { autoSyncCheckBackgroundSupport = oldChecker })

	var output bytes.Buffer
	code := cmdAutoSync([]string{"enable"}, strings.NewReader("y\n19:00\n"), &output)

	if code != 2 {
		t.Fatalf("unsupported Linux enable exit code = %d, want 2", code)
	}
	want := "当前环境暂不支持自动同步\n"
	if output.String() != want {
		t.Fatalf("unsupported Linux output = %q, want %q", output.String(), want)
	}
	if _, err := os.Stat(autoSyncConfigPath()); !os.IsNotExist(err) {
		t.Fatalf("unsupported Linux enable wrote config: %v", err)
	}
}

func TestAutoSyncEnableReportsUnsupportedNonLinuxPlatform(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldPlatform := autoSyncPlatform
	autoSyncPlatform = "darwin"
	t.Cleanup(func() { autoSyncPlatform = oldPlatform })
	oldChecker := autoSyncCheckBackgroundSupport
	autoSyncCheckBackgroundSupport = func() error { return errAutoSyncSystemdUnavailable }
	t.Cleanup(func() { autoSyncCheckBackgroundSupport = oldChecker })

	var output bytes.Buffer
	code := cmdAutoSync([]string{"enable"}, strings.NewReader(""), &output)
	if code != 2 {
		t.Fatalf("unsupported platform exit code = %d, want 2", code)
	}
	want := "当前环境暂不支持自动同步\n"
	if output.String() != want {
		t.Fatalf("unsupported platform output = %q, want %q", output.String(), want)
	}
}

func TestAutoSyncEnableShowsExistingStateBeforeSupportCheck(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := saveAutoSyncConfig(autoSyncConfig{SchemaVersion: 1, Enabled: true, DailyTime: "18:00"}); err != nil {
		t.Fatal(err)
	}
	oldChecker := autoSyncCheckBackgroundSupport
	autoSyncCheckBackgroundSupport = func() error { return errAutoSyncSystemdUnavailable }
	t.Cleanup(func() { autoSyncCheckBackgroundSupport = oldChecker })

	var output bytes.Buffer
	if code := cmdAutoSync([]string{"enable"}, strings.NewReader(""), &output); code != 0 {
		t.Fatalf("enable exit code = %d, want 0", code)
	}
	if output.String() != "自动同步已开启，每天 18:00（北京时间）\n" {
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
	oldEnsure := autoSyncEnsure
	autoSyncEnsure = func() error { return nil }
	t.Cleanup(func() { autoSyncEnsure = oldEnsure })
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
	if !strings.Contains(string(output), "自动 Session 同步：已开启\n每日同步时间：18:00（北京时间）") {
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
	want := "自动 Session 同步：已开启\n每日同步时间：18:00（北京时间）\n"
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
	want := "自动同步已关闭\n"
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

func TestAutoSyncMigratesLegacyLocalTimeToBeijingTime(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(autoSyncDir(), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"schema_version":1,"enabled":true,"daily_time":"15:35","schedule_effective_at":"2026-07-21T15:35:00Z"}`)
	if err := os.WriteFile(autoSyncConfigPath(), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	oldNow := autoSyncNow
	autoSyncNow = func() time.Time {
		return time.Date(2026, 7, 21, 7, 36, 0, 0, time.UTC)
	}
	t.Cleanup(func() { autoSyncNow = oldNow })

	cfg, err := loadAutoSyncConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SchemaVersion != autoSyncConfigSchemaVersion || cfg.Timezone != autoSyncTimezone {
		t.Fatalf("migrated config = %+v", cfg)
	}
	if cfg.DailyTime != "15:35" || cfg.ScheduleEffectiveAt != "2026-07-21T15:36:00+08:00" {
		t.Fatalf("migrated schedule = %+v", cfg)
	}
	if !autoSyncDue(autoSyncNow(), cfg, autoSyncSchedule{}) {
		t.Fatal("migrated overdue schedule should run immediately")
	}
}

func TestAutoSyncDueFollowsDailySuccessAndRetryRules(t *testing.T) {
	now := time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	baseConfig := autoSyncConfig{
		SchemaVersion:       1,
		Enabled:             true,
		DailyTime:           "18:00",
		ScheduleEffectiveAt: "2026-07-20T18:00:00+08:00",
	}

	tests := []struct {
		name     string
		now      time.Time
		cfg      autoSyncConfig
		schedule autoSyncSchedule
		want     bool
	}{
		{name: "due after daily time", now: now, cfg: baseConfig, want: true},
		{name: "not due before daily time", now: time.Date(2026, 7, 21, 9, 59, 0, 0, time.UTC), cfg: baseConfig, want: false},
		{name: "not due after success today", now: now, cfg: baseConfig, schedule: autoSyncSchedule{LastSuccessDate: "2026-07-21"}, want: false},
		{name: "not due within retry interval", now: now, cfg: baseConfig, schedule: autoSyncSchedule{LastAttemptAt: "2026-07-21T10:29:30Z"}, want: false},
		{name: "not due before schedule becomes effective", now: now, cfg: autoSyncConfig{SchemaVersion: 2, Enabled: true, DailyTime: "18:00", ScheduleEffectiveAt: "2026-07-22T18:00:00+08:00"}, want: false},
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
	now := time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	if err := saveAutoSyncConfig(autoSyncConfig{
		SchemaVersion:       1,
		Enabled:             true,
		DailyTime:           "18:00",
		ScheduleEffectiveAt: "2026-07-20T18:00:00+08:00",
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

func TestAutoSyncFailureBacksOffInsteadOfRetryingEveryMinute(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 7, 21, 10, 30, 0, 0, time.UTC)
	if err := saveAutoSyncConfig(autoSyncConfig{
		SchemaVersion:       1,
		Enabled:             true,
		DailyTime:           "18:00",
		ScheduleEffectiveAt: "2026-07-20T18:00:00+08:00",
	}); err != nil {
		t.Fatal(err)
	}

	executions := 0
	execute := func() int {
		executions++
		return 1
	}
	if ran, err := runAutoSyncOnce(now, execute); err == nil || !ran {
		t.Fatalf("first failure: ran=%t err=%v", ran, err)
	}
	if ran, err := runAutoSyncOnce(now.Add(2*time.Minute), execute); err != nil || ran {
		t.Fatalf("retry inside backoff: ran=%t err=%v", ran, err)
	}
	if executions != 1 {
		t.Fatalf("executions inside backoff=%d want=1", executions)
	}
	if ran, err := runAutoSyncOnce(now.Add(5*time.Minute), execute); err == nil || !ran {
		t.Fatalf("retry after backoff: ran=%t err=%v", ran, err)
	}
	if executions != 2 {
		t.Fatalf("executions after backoff=%d want=2", executions)
	}
}

func TestAutoSyncStopsRetryingAfterDailyFailureLimit(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	now := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
	if err := saveAutoSyncConfig(autoSyncConfig{
		Enabled:             true,
		DailyTime:           "08:00",
		ScheduleEffectiveAt: "2026-07-20T08:00:00+08:00",
	}); err != nil {
		t.Fatal(err)
	}

	executions := 0
	execute := func() int {
		executions++
		return 1
	}
	for _, offset := range []time.Duration{0, 5 * time.Minute, 20 * time.Minute, 80 * time.Minute, 320 * time.Minute} {
		if ran, err := runAutoSyncOnce(now.Add(offset), execute); err == nil || !ran {
			t.Fatalf("failure at %s: ran=%t err=%v", offset, ran, err)
		}
	}
	if ran, err := runAutoSyncOnce(now.Add(321*time.Minute), execute); err != nil || ran {
		t.Fatalf("retry after daily limit: ran=%t err=%v", ran, err)
	}
	if executions != autoSyncMaxFailuresPerDay {
		t.Fatalf("executions=%d want=%d", executions, autoSyncMaxFailuresPerDay)
	}
	if ran, err := runAutoSyncOnce(now.Add(24*time.Hour), execute); err == nil || !ran {
		t.Fatalf("next-day retry: ran=%t err=%v", ran, err)
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

func TestAutoSyncEnsureRespectsDisabledChoice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldChecker := autoSyncCheckBackgroundSupport
	autoSyncCheckBackgroundSupport = func() error { return nil }
	t.Cleanup(func() { autoSyncCheckBackgroundSupport = oldChecker })
	oldStarter := autoSyncStartBackground
	starts := 0
	autoSyncStartBackground = func() error {
		starts++
		return nil
	}
	t.Cleanup(func() { autoSyncStartBackground = oldStarter })

	if err := ensureAutoSyncBackground(); err != nil {
		t.Fatal(err)
	}
	if starts != 0 {
		t.Fatalf("background starts = %d, want 0", starts)
	}

	if err := saveAutoSyncConfig(autoSyncConfig{SchemaVersion: 1, Enabled: true, DailyTime: "18:00"}); err != nil {
		t.Fatal(err)
	}
	if err := ensureAutoSyncBackground(); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("background starts = %d, want 1", starts)
	}
}

func TestAutoSyncEnsureRestartsBackgroundAfterTimezoneMigration(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(autoSyncDir(), 0700); err != nil {
		t.Fatal(err)
	}
	legacy := []byte(`{"schema_version":1,"enabled":true,"daily_time":"15:35","schedule_effective_at":"2026-07-21T15:35:00Z"}`)
	if err := os.WriteFile(autoSyncConfigPath(), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	oldChecker := autoSyncCheckBackgroundSupport
	autoSyncCheckBackgroundSupport = func() error { return nil }
	t.Cleanup(func() { autoSyncCheckBackgroundSupport = oldChecker })
	oldStarter := autoSyncStartBackground
	starts := 0
	autoSyncStartBackground = func() error {
		starts++
		return nil
	}
	t.Cleanup(func() { autoSyncStartBackground = oldStarter })
	oldRestarter := autoSyncRestartBackground
	restarts := 0
	autoSyncRestartBackground = func() error {
		restarts++
		return nil
	}
	t.Cleanup(func() { autoSyncRestartBackground = oldRestarter })

	if err := ensureAutoSyncBackground(); err != nil {
		t.Fatal(err)
	}
	if starts != 1 || restarts != 1 {
		t.Fatalf("starts=%d restarts=%d, want one start and one migration restart", starts, restarts)
	}
	if autoSyncConfigNeedsMigration() {
		t.Fatal("config still requires migration")
	}
}

func TestAutoSyncRestartAfterUpdateRespectsEnabledChoice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	oldRestarter := autoSyncRestartBackground
	restarts := 0
	autoSyncRestartBackground = func() error {
		restarts++
		return nil
	}
	t.Cleanup(func() { autoSyncRestartBackground = oldRestarter })

	if err := restartAutoSyncAfterUpdate(); err != nil {
		t.Fatal(err)
	}
	if restarts != 0 {
		t.Fatalf("disabled restarts = %d, want 0", restarts)
	}
	if err := saveAutoSyncConfig(autoSyncConfig{SchemaVersion: 1, Enabled: true, DailyTime: "18:00"}); err != nil {
		t.Fatal(err)
	}
	if err := restartAutoSyncAfterUpdate(); err != nil {
		t.Fatal(err)
	}
	if restarts != 1 {
		t.Fatalf("enabled restarts = %d, want 1", restarts)
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
