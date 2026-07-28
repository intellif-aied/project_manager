package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

var autoSyncNow = time.Now
var autoSyncPlatform = runtime.GOOS

const (
	autoSyncConfigSchemaVersion = 3
	autoSyncTimezone            = "Asia/Shanghai"
)

var autoSyncLocation = loadAutoSyncLocation()

func loadAutoSyncLocation() *time.Location {
	location, err := time.LoadLocation(autoSyncTimezone)
	if err == nil {
		return location
	}
	return time.FixedZone(autoSyncTimezone, 8*60*60)
}

func autoSyncBusinessTime(value time.Time) time.Time {
	return value.In(autoSyncLocation)
}

var errAutoSyncSystemdUnavailable = errors.New("systemd user manager unavailable")
var errAutoSyncLockHeld = errors.New("AutoSync lock already held")

var autoSyncCheckBackgroundSupport = checkAutoSyncBackgroundSupport
var autoSyncStartBackground = startAutoSyncBackground
var autoSyncStopBackground = stopAutoSyncBackground
var autoSyncRestartBackground = restartAutoSyncBackground
var autoSyncRunDaemon = runAutoSyncDaemon
var autoSyncExecute = executeAutoSync
var autoSyncEnsure = ensureAutoSyncBackground

type autoSyncConfig struct {
	SchemaVersion       int    `json:"schema_version"`
	Enabled             bool   `json:"enabled"`
	DailyTime           string `json:"daily_time,omitempty"`
	Timezone            string `json:"timezone,omitempty"`
	ScheduleEffectiveAt string `json:"schedule_effective_at,omitempty"`
	Mode                string `json:"mode,omitempty"`
}

type autoSyncSchedule struct {
	SchemaVersion   int    `json:"schema_version"`
	LastSuccessDate string `json:"last_success_date,omitempty"`
	LastAttemptAt   string `json:"last_attempt_at,omitempty"`
}

func autoSyncDue(now time.Time, cfg autoSyncConfig, schedule autoSyncSchedule) bool {
	if !cfg.Enabled || cfg.DailyTime == "" {
		return false
	}
	parsed, err := time.Parse("15:04", cfg.DailyTime)
	if err != nil {
		return false
	}
	businessNow := autoSyncBusinessTime(now)
	target := time.Date(businessNow.Year(), businessNow.Month(), businessNow.Day(), parsed.Hour(), parsed.Minute(), 0, 0, autoSyncLocation)
	if businessNow.Before(target) {
		return false
	}
	if cfg.ScheduleEffectiveAt != "" {
		effectiveAt, err := time.Parse(time.RFC3339, cfg.ScheduleEffectiveAt)
		if err != nil || now.Before(effectiveAt) {
			return false
		}
	}
	if schedule.LastSuccessDate == businessNow.Format("2006-01-02") {
		return false
	}
	if schedule.LastAttemptAt != "" {
		lastAttempt, err := time.Parse(time.RFC3339, schedule.LastAttemptAt)
		if err != nil || now.Sub(lastAttempt) < time.Minute {
			return false
		}
	}
	return true
}

func autoSyncDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aida", "auto-sync")
}

func autoSyncConfigPath() string {
	return filepath.Join(autoSyncDir(), "config.json")
}

func autoSyncSchedulePath() string {
	return filepath.Join(autoSyncDir(), "schedule.json")
}

func acquireAutoSyncNamedLock(name string) (func(), error) {
	if err := os.MkdirAll(autoSyncDir(), 0700); err != nil {
		return nil, err
	}
	return acquireAutoSyncFileLock(filepath.Join(autoSyncDir(), name))
}

func loadAutoSyncConfig() (autoSyncConfig, error) {
	data, err := os.ReadFile(autoSyncConfigPath())
	if os.IsNotExist(err) {
		return autoSyncConfig{SchemaVersion: autoSyncConfigSchemaVersion, Timezone: autoSyncTimezone, Mode: uploadModePersonal}, nil
	}
	if err != nil {
		return autoSyncConfig{}, err
	}

	var cfg autoSyncConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return autoSyncConfig{}, err
	}
	needsTimezoneMigration := cfg.SchemaVersion < 2 || cfg.Timezone == ""
	if cfg.SchemaVersion < autoSyncConfigSchemaVersion || cfg.Timezone == "" || (cfg.Mode != uploadModePersonal && cfg.Mode != uploadModeTeam) {
		cfg.SchemaVersion = autoSyncConfigSchemaVersion
		cfg.Timezone = autoSyncTimezone
		cfg.Mode = uploadModePersonal
		if cfg.Enabled && cfg.DailyTime != "" && (needsTimezoneMigration || cfg.ScheduleEffectiveAt == "") {
			if parsed, parseErr := time.Parse("15:04", cfg.DailyTime); parseErr == nil {
				now := autoSyncBusinessTime(autoSyncNow())
				target := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, autoSyncLocation)
				if !now.Before(target) {
					target = now
				}
				cfg.ScheduleEffectiveAt = target.Format(time.RFC3339)
			}
		}
		if err := saveAutoSyncConfig(cfg); err != nil {
			return autoSyncConfig{}, err
		}
	}
	return cfg, nil
}

func autoSyncConfigNeedsMigration() bool {
	data, err := os.ReadFile(autoSyncConfigPath())
	if err != nil {
		return false
	}
	var stored struct {
		SchemaVersion int    `json:"schema_version"`
		Timezone      string `json:"timezone"`
		Mode          string `json:"mode"`
	}
	if json.Unmarshal(data, &stored) != nil {
		return false
	}
	return stored.SchemaVersion < autoSyncConfigSchemaVersion || stored.Timezone == "" || (stored.Mode != uploadModePersonal && stored.Mode != uploadModeTeam)
}

func saveAutoSyncConfig(cfg autoSyncConfig) error {
	cfg.SchemaVersion = autoSyncConfigSchemaVersion
	cfg.Timezone = autoSyncTimezone
	if cfg.Mode != uploadModeTeam {
		cfg.Mode = uploadModePersonal
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := autoSyncDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, autoSyncConfigPath())
}

func loadAutoSyncSchedule() (autoSyncSchedule, error) {
	data, err := os.ReadFile(autoSyncSchedulePath())
	if os.IsNotExist(err) {
		return autoSyncSchedule{SchemaVersion: 1}, nil
	}
	if err != nil {
		return autoSyncSchedule{}, err
	}
	var schedule autoSyncSchedule
	if err := json.Unmarshal(data, &schedule); err != nil {
		return autoSyncSchedule{}, err
	}
	return schedule, nil
}

func saveAutoSyncSchedule(schedule autoSyncSchedule) error {
	if schedule.SchemaVersion == 0 {
		schedule.SchemaVersion = 1
	}
	data, err := json.MarshalIndent(schedule, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := autoSyncDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".schedule-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, autoSyncSchedulePath())
}

func runAutoSyncOnce(now time.Time, execute func() int) (bool, error) {
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		return false, err
	}
	schedule, err := loadAutoSyncSchedule()
	if err != nil {
		return false, err
	}
	if !autoSyncDue(now, cfg, schedule) {
		return false, nil
	}
	schedule.LastAttemptAt = now.UTC().Format(time.RFC3339)
	if err := saveAutoSyncSchedule(schedule); err != nil {
		return false, err
	}
	if code := execute(); code != 0 {
		return true, fmt.Errorf("automatic Session upload exited with code %d", code)
	}
	schedule.LastSuccessDate = autoSyncBusinessTime(now).Format("2006-01-02")
	if err := saveAutoSyncSchedule(schedule); err != nil {
		return true, err
	}
	return true, nil
}

func runAutoSyncExecuteProcess() int {
	executable, err := os.Executable()
	if err != nil {
		return 1
	}
	logFile, err := os.OpenFile(filepath.Join(autoSyncDir(), "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 1
	}
	defer logFile.Close()
	command := exec.Command(executable, "auto-sync", "execute")
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	if err := command.Run(); err != nil {
		return 1
	}
	return 0
}

func runAutoSyncDaemon() int {
	if err := os.MkdirAll(autoSyncDir(), 0700); err != nil {
		return 1
	}
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		return 1
	}
	if !cfg.Enabled {
		return 0
	}
	release, err := acquireAutoSyncNamedLock("daemon.lock")
	if errors.Is(err, errAutoSyncLockHeld) {
		return 0
	}
	if err != nil {
		return 1
	}
	defer release()
	run := func() {
		if _, err := runAutoSyncOnce(autoSyncNow(), runAutoSyncExecuteProcess); err != nil {
			if logFile, openErr := os.OpenFile(filepath.Join(autoSyncDir(), "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); openErr == nil {
				fmt.Fprintf(logFile, "%s AutoSync: %v\n", autoSyncNow().Format(time.RFC3339), err)
				logFile.Close()
			}
		}
	}
	run()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		cfg, err := loadAutoSyncConfig()
		if err != nil || !cfg.Enabled {
			return 0
		}
		run()
	}
	return 0
}

func runAutoSyncExecute(update func() error, upload func() int, output io.Writer) int {
	if err := update(); err != nil {
		fmt.Fprintf(output, "版本检查或更新失败：%v；继续自动上传全部 Session\n", err)
	}
	return upload()
}

func runAutoSyncUploadAllProcess() int {
	executable, err := os.Executable()
	if err != nil {
		return 1
	}
	command := exec.Command(executable, "auto-sync", "upload-all")
	command.Stdin = nil
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode()
		}
		return 1
	}
	return 0
}

func executeAutoSync() int {
	release, err := acquireAutoSyncNamedLock("run.lock")
	if errors.Is(err, errAutoSyncLockHeld) {
		fmt.Fprintln(os.Stderr, "已有自动 Session 同步正在执行，本次跳过")
		return 75
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "获取自动 Session 同步执行锁失败：%v\n", err)
		return 1
	}
	defer release()
	update := func() error {
		_, updateErr := performSelfUpdate(loadConfig())
		return updateErr
	}
	return runAutoSyncExecute(update, runAutoSyncUploadAllProcess, os.Stderr)
}

func cmdAutoSyncCLI(args []string, input io.Reader, output io.Writer, interactive bool) int {
	if len(args) >= 1 && args[0] == "enable" && !interactive {
		fmt.Fprintln(output, "开启自动 Session 同步需要交互式终端，请运行 aida auto-sync enable")
		return 2
	}
	if len(args) >= 1 && (args[0] == "enable" || args[0] == "status" || args[0] == "set-time") {
		_ = ensureAutoSyncBackground()
	}
	return cmdAutoSync(args, input, output)
}

func finishLoginAutoSync(input io.Reader, output io.Writer, interactive bool) {
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		fmt.Fprintln(output, "无法读取自动同步设置，请运行 aida status 检查")
		return
	}
	if cfg.Enabled {
		return
	}
	if !interactive {
		fmt.Fprintln(output, "自动 Session 同步未开启，可在交互式终端运行 aida auto-sync enable")
		return
	}
	_ = cmdAutoSync([]string{"enable"}, input, output)
}

func ensureAutoSyncBackground() error {
	release, err := acquireAutoSyncNamedLock("start.lock")
	if err != nil {
		return err
	}
	defer release()
	needsRestart := autoSyncConfigNeedsMigration()
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	if err := autoSyncCheckBackgroundSupport(); err != nil {
		return err
	}
	if err := autoSyncStartBackground(); err != nil {
		return err
	}
	if needsRestart {
		return autoSyncRestartBackground()
	}
	return nil
}

func restartAutoSyncAfterUpdate() error {
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		return err
	}
	if !cfg.Enabled {
		return nil
	}
	return autoSyncRestartBackground()
}

func writeAutoSyncStatus(output io.Writer) int {
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		fmt.Fprintln(output, "无法读取自动同步设置，请运行 aida status 检查")
		return 1
	}
	if cfg.Enabled {
		fmt.Fprintln(output, "自动 Session 同步：已开启")
		if cfg.Mode == uploadModeTeam {
			fmt.Fprintln(output, "同步模式：团队")
		}
		fmt.Fprintf(output, "每日同步时间：%s（北京时间）\n", cfg.DailyTime)
	} else {
		fmt.Fprintln(output, "自动 Session 同步：未开启")
		fmt.Fprintln(output, "开启方式：aida auto-sync enable")
	}
	return 0
}

func cmdAutoSync(args []string, input io.Reader, output io.Writer) int {
	if len(args) < 1 || len(args) > 2 || (len(args) == 2 && (args[0] != "enable" || args[1] != "--team")) {
		writeAutoSyncHelp(output)
		return 2
	}
	requestedMode := uploadModePersonal
	if len(args) == 2 && args[1] == "--team" {
		requestedMode = uploadModeTeam
	}

	switch args[0] {
	case "help", "--help", "-h":
		writeAutoSyncHelp(output)
		return 0
	case "daemon-run":
		return autoSyncRunDaemon()
	case "execute":
		return autoSyncExecute()
	case "upload-all":
		cfg, err := loadAutoSyncConfig()
		if err != nil {
			return 1
		}
		if cfg.Mode == uploadModeTeam {
			return cmdUpload([]string{"--team"})
		}
		return cmdUpload([]string{"--all"})
	case "ensure":
		if err := ensureAutoSyncBackground(); err != nil {
			fmt.Fprintln(output, "自动同步暂时不可用，请运行 aida status 检查")
			return 1
		}
		return 0
	case "status":
		return writeAutoSyncStatus(output)
	case "enable":
		cfg, err := loadAutoSyncConfig()
		if err != nil {
			fmt.Fprintln(output, "无法读取自动同步设置，请运行 aida status 检查")
			return 1
		}
		if cfg.Enabled {
			if cfg.Mode != requestedMode {
				cfg.Mode = requestedMode
				if err := saveAutoSyncConfig(cfg); err != nil {
					fmt.Fprintln(output, "自动同步模式修改失败，请运行 aida status 检查")
					return 1
				}
				if err := autoSyncRestartBackground(); err != nil {
					fmt.Fprintln(output, "自动同步模式已保存，但后台重启失败，请运行 aida status 检查")
					return 1
				}
			}
			if cfg.Mode == uploadModeTeam {
				fmt.Fprintf(output, "自动同步已开启，每天 %s（北京时间），模式：团队\n", cfg.DailyTime)
			} else {
				fmt.Fprintf(output, "自动同步已开启，每天 %s（北京时间）\n", cfg.DailyTime)
			}
			return 0
		}
		if err := autoSyncCheckBackgroundSupport(); err != nil {
			if errors.Is(err, errAutoSyncSystemdUnavailable) {
				fmt.Fprintln(output, "当前环境暂不支持自动同步")
				return 2
			}
			fmt.Fprintln(output, "无法检查自动同步运行环境，请运行 aida status 检查")
			return 1
		}

		enabled, cancelled, chooseErr := autoSyncChooseEnable(input, output)
		if chooseErr != nil {
			fmt.Fprintln(output, "无法完成自动同步设置，请重试")
			return 1
		}
		if cancelled || !enabled {
			fmt.Fprintln(output, "自动 Session 同步保持未开启")
			return 0
		}

		value, cancelled, chooseErr := autoSyncChooseTime("18:00", input, output)
		if chooseErr != nil {
			fmt.Fprintln(output, "无法完成时间设置，请重试")
			return 1
		}
		if cancelled {
			fmt.Fprintln(output, "自动 Session 同步保持未开启")
			return 0
		}
		parsed, err := time.Parse("15:04", value)
		if err != nil {
			fmt.Fprintln(output, "无法完成时间设置，请重试")
			return 1
		}

		now := autoSyncBusinessTime(autoSyncNow())
		effectiveAt := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, autoSyncLocation)
		var pastChoice *autoSyncStartChoice
		if effectiveAt.Before(now) {
			choice, cancelled, chooseErr := autoSyncChoosePastTime(value, input, output)
			if chooseErr != nil {
				fmt.Fprintln(output, "无法完成首次同步设置，请重试")
				return 1
			}
			if cancelled {
				fmt.Fprintln(output, "自动 Session 同步保持未开启")
				return 0
			}
			pastChoice = &choice
			if choice == autoSyncStartImmediate {
				effectiveAt = now
			} else {
				effectiveAt = effectiveAt.AddDate(0, 0, 1)
			}
		}

		cfg.Enabled = true
		cfg.Mode = requestedMode
		cfg.DailyTime = value
		cfg.ScheduleEffectiveAt = effectiveAt.Format(time.RFC3339)
		releaseStart, err := acquireAutoSyncNamedLock("start.lock")
		if err != nil {
			fmt.Fprintln(output, "另一项自动 Session 同步管理操作正在执行，请稍后重试")
			return 1
		}
		defer releaseStart()
		latestConfig, err := loadAutoSyncConfig()
		if err != nil {
			fmt.Fprintln(output, "无法读取自动同步设置，请运行 aida status 检查")
			return 1
		}
		if latestConfig.Enabled {
			if latestConfig.Mode == uploadModeTeam {
				fmt.Fprintf(output, "自动同步已开启，每天 %s（北京时间），模式：团队\n", latestConfig.DailyTime)
			} else {
				fmt.Fprintf(output, "自动同步已开启，每天 %s（北京时间）\n", latestConfig.DailyTime)
			}
			return 0
		}
		if err := saveAutoSyncConfig(cfg); err != nil {
			fmt.Fprintln(output, "自动同步开启失败，请运行 aida status 检查")
			return 1
		}
		if err := autoSyncStartBackground(); err != nil {
			cfg.Enabled = false
			_ = saveAutoSyncConfig(cfg)
			fmt.Fprintln(output, "自动同步开启失败，请运行 aida status 检查")
			return 1
		}

		fmt.Fprintln(output, "自动同步已开启")
		if cfg.Mode == uploadModeTeam {
			fmt.Fprintf(output, "每天 %s（北京时间）同步 Session，模式：团队\n", value)
		} else {
			fmt.Fprintf(output, "每天 %s（北京时间）同步 Session\n", value)
		}
		if pastChoice != nil {
			if *pastChoice == autoSyncStartImmediate {
				fmt.Fprintln(output, "今天将立即同步一次")
			} else {
				fmt.Fprintf(output, "首次自动同步：明天 %s（北京时间）\n", value)
			}
		}
		return 0
	case "disable":
		releaseStart, err := acquireAutoSyncNamedLock("start.lock")
		if err != nil {
			fmt.Fprintln(output, "另一项自动 Session 同步管理操作正在执行，请稍后重试")
			return 1
		}
		defer releaseStart()
		cfg, err := loadAutoSyncConfig()
		if err != nil {
			fmt.Fprintln(output, "无法读取自动同步设置，请运行 aida status 检查")
			return 1
		}
		if !cfg.Enabled {
			fmt.Fprintln(output, "自动 Session 同步当前已经处于未开启状态")
			return 0
		}
		cfg.Enabled = false
		if err := saveAutoSyncConfig(cfg); err != nil {
			fmt.Fprintln(output, "自动同步关闭失败，请运行 aida status 检查")
			return 1
		}
		if err := autoSyncStopBackground(); err != nil {
			fmt.Fprintln(output, "自动同步关闭未完全生效，请运行 aida status 检查")
			return 1
		}
		fmt.Fprintln(output, "自动同步已关闭")
		return 0
	case "set-time":
		cfg, err := loadAutoSyncConfig()
		if err != nil {
			fmt.Fprintln(output, "无法读取自动同步设置，请运行 aida status 检查")
			return 1
		}
		if !cfg.Enabled {
			fmt.Fprintln(output, "自动 Session 同步尚未开启，请先运行 aida auto-sync enable")
			return 1
		}

		fmt.Fprintf(output, "当前每日同步时间：%s（北京时间）\n", cfg.DailyTime)
		value, cancelled, chooseErr := autoSyncChooseTime(cfg.DailyTime, input, output)
		if chooseErr != nil {
			fmt.Fprintln(output, "无法完成时间设置，请重试")
			return 1
		}
		if cancelled {
			fmt.Fprintln(output, "未修改每日同步时间")
			return 0
		}
		parsed, err := time.Parse("15:04", value)
		if err != nil {
			fmt.Fprintln(output, "无法完成时间设置，请重试")
			return 1
		}

		now := autoSyncBusinessTime(autoSyncNow())
		effectiveAt := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, autoSyncLocation)
		var pastChoice *autoSyncStartChoice
		if effectiveAt.Before(now) {
			choice, cancelled, err := autoSyncChoosePastTime(value, input, output)
			if err != nil {
				fmt.Fprintln(output, "无法完成首次同步设置，请重试")
				return 1
			}
			if cancelled {
				fmt.Fprintln(output, "未修改每日同步时间")
				return 0
			}
			pastChoice = &choice
			if choice == autoSyncStartImmediate {
				effectiveAt = now
			} else {
				effectiveAt = effectiveAt.AddDate(0, 0, 1)
			}
		}
		cfg.DailyTime = value
		cfg.ScheduleEffectiveAt = effectiveAt.Format(time.RFC3339)
		releaseStart, err := acquireAutoSyncNamedLock("start.lock")
		if err != nil {
			fmt.Fprintln(output, "另一项自动 Session 同步管理操作正在执行，请稍后重试")
			return 1
		}
		defer releaseStart()
		latestConfig, err := loadAutoSyncConfig()
		if err != nil {
			fmt.Fprintln(output, "无法读取自动同步设置，请运行 aida status 检查")
			return 1
		}
		if !latestConfig.Enabled {
			fmt.Fprintln(output, "自动 Session 同步已关闭，本次时间修改未保存")
			return 1
		}
		cfg = latestConfig
		cfg.DailyTime = value
		cfg.ScheduleEffectiveAt = effectiveAt.Format(time.RFC3339)
		if err := saveAutoSyncConfig(cfg); err != nil {
			fmt.Fprintln(output, "同步时间修改失败，请运行 aida status 检查")
			return 1
		}
		fmt.Fprintf(output, "同步时间已改为每天 %s（北京时间）\n", value)
		if pastChoice != nil {
			if *pastChoice == autoSyncStartImmediate {
				fmt.Fprintln(output, "今天将立即同步一次")
			} else {
				fmt.Fprintf(output, "首次自动同步：明天 %s（北京时间）\n", value)
			}
		}
		return 0
	}

	writeAutoSyncHelp(output)
	return 2
}

func writeAutoSyncHelp(output io.Writer) {
	fmt.Fprint(output, `自动同步 Session

用法：
  aida auto-sync <命令>

命令：
  enable          开启个人模式自动同步
  enable --team   开启团队模式自动同步
  status      查看自动同步状态
  set-time    修改每天的同步时间
  disable     关闭自动同步
`)
}
