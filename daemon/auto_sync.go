package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var autoSyncNow = time.Now

var errAutoSyncSystemdUnavailable = errors.New("systemd user manager unavailable")
var errAutoSyncLockHeld = errors.New("AutoSync lock already held")

var autoSyncCheckBackgroundSupport = checkAutoSyncBackgroundSupport
var autoSyncStartBackground = startAutoSyncBackground
var autoSyncStopBackground = stopAutoSyncBackground
var autoSyncRestartBackground = restartAutoSyncBackground
var autoSyncRunDaemon = runAutoSyncDaemon
var autoSyncExecute = executeAutoSync

type autoSyncConfig struct {
	SchemaVersion       int    `json:"schema_version"`
	Enabled             bool   `json:"enabled"`
	DailyTime           string `json:"daily_time,omitempty"`
	ScheduleEffectiveAt string `json:"schedule_effective_at,omitempty"`
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
	target := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	if now.Before(target) {
		return false
	}
	if cfg.ScheduleEffectiveAt != "" {
		effectiveAt, err := time.Parse(time.RFC3339, cfg.ScheduleEffectiveAt)
		if err != nil || now.Before(effectiveAt) {
			return false
		}
	}
	if schedule.LastSuccessDate == now.Format("2006-01-02") {
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
		return autoSyncConfig{SchemaVersion: 1}, nil
	}
	if err != nil {
		return autoSyncConfig{}, err
	}

	var cfg autoSyncConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return autoSyncConfig{}, err
	}
	return cfg, nil
}

func saveAutoSyncConfig(cfg autoSyncConfig) error {
	if cfg.SchemaVersion == 0 {
		cfg.SchemaVersion = 1
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
	schedule.LastAttemptAt = now.Format(time.RFC3339)
	if err := saveAutoSyncSchedule(schedule); err != nil {
		return false, err
	}
	if code := execute(); code != 0 {
		return true, fmt.Errorf("automatic Session upload exited with code %d", code)
	}
	schedule.LastSuccessDate = now.Format("2006-01-02")
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
	if len(args) == 1 && args[0] == "enable" && !interactive {
		fmt.Fprintln(output, "开启自动 Session 同步需要交互式终端，请运行 aida auto-sync enable")
		return 2
	}
	return cmdAutoSync(args, input, output)
}

func finishLoginAutoSync(input io.Reader, output io.Writer, interactive bool) {
	cfg, err := loadAutoSyncConfig()
	if err != nil {
		fmt.Fprintf(output, "读取自动 Session 同步状态失败：%v\n", err)
		return
	}
	if cfg.Enabled {
		fmt.Fprintln(output, "自动 Session 同步：已开启")
		fmt.Fprintf(output, "每日同步时间：%s（本机时间）\n", cfg.DailyTime)
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
	return autoSyncStartBackground()
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
		fmt.Fprintf(output, "读取自动 Session 同步状态失败：%v\n", err)
		return 1
	}
	if cfg.Enabled {
		fmt.Fprintln(output, "自动 Session 同步：已开启")
		fmt.Fprintf(output, "每日同步时间：%s（本机时间）\n", cfg.DailyTime)
	} else {
		fmt.Fprintln(output, "自动 Session 同步：未开启")
		fmt.Fprintln(output, "开启方式：aida auto-sync enable")
	}
	return 0
}

func cmdAutoSync(args []string, input io.Reader, output io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(output, "Usage: aida auto-sync <enable|status|set-time|disable>")
		return 2
	}

	switch args[0] {
	case "daemon-run":
		return autoSyncRunDaemon()
	case "execute":
		return autoSyncExecute()
	case "upload-all":
		return cmdUpload([]string{"--all"})
	case "ensure":
		if err := ensureAutoSyncBackground(); err != nil {
			fmt.Fprintf(output, "自动 Session 同步后台自检失败：%v\n", err)
			return 1
		}
		return 0
	case "status":
		return writeAutoSyncStatus(output)
	case "enable":
		if err := autoSyncCheckBackgroundSupport(); err != nil {
			if errors.Is(err, errAutoSyncSystemdUnavailable) {
				fmt.Fprintln(output, "当前 Linux 环境缺少可用的 systemd user manager，暂不支持自动 Session 同步")
				return 2
			}
			fmt.Fprintf(output, "检测自动 Session 同步运行环境失败：%v\n", err)
			return 1
		}
		cfg, err := loadAutoSyncConfig()
		if err != nil {
			fmt.Fprintf(output, "读取自动 Session 同步状态失败：%v\n", err)
			return 1
		}
		if cfg.Enabled {
			fmt.Fprintln(output, "自动 Session 同步已经开启")
			fmt.Fprintf(output, "每日同步时间：%s（本机时间）\n", cfg.DailyTime)
			fmt.Fprintln(output, "修改时间：aida auto-sync set-time")
			fmt.Fprintln(output, "关闭方式：aida auto-sync disable")
			return 0
		}

		reader := bufio.NewReader(input)
		fmt.Fprintln(output, "开启后，Aida 将在你选择的每日时间自动上传全部 Session。")
		fmt.Fprintln(output, "如果错过该时间，将在 Aida 恢复运行后第一时间补传。")
		fmt.Fprintln(output, "是否继续？[y/N]")
		confirmation, _ := reader.ReadString('\n')
		if !strings.EqualFold(strings.TrimSpace(confirmation), "y") {
			fmt.Fprintln(output, "自动 Session 同步保持未开启")
			return 0
		}

		var value string
		var parsed time.Time
		for {
			fmt.Fprintln(output, "请选择每天同步时间（HH:MM，按本机时间）：")
			line, readErr := reader.ReadString('\n')
			if readErr != nil && len(line) == 0 {
				fmt.Fprintln(output, "自动 Session 同步保持未开启")
				return 0
			}
			value = strings.TrimSpace(line)
			parsed, err = time.Parse("15:04", value)
			if err == nil && parsed.Format("15:04") == value {
				break
			}
			fmt.Fprintln(output, "时间格式无效，请使用 HH:MM，例如 18:00")
		}

		now := autoSyncNow()
		effectiveAt := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
		var pastChoice *autoSyncStartChoice
		if effectiveAt.Before(now) {
			choice, cancelled, chooseErr := autoSyncChoosePastTime(value, input, output)
			if chooseErr != nil {
				fmt.Fprintf(output, "选择首次同步时间失败：%v\n", chooseErr)
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
			fmt.Fprintf(output, "读取自动 Session 同步状态失败：%v\n", err)
			return 1
		}
		if latestConfig.Enabled {
			fmt.Fprintln(output, "自动 Session 同步已经开启")
			fmt.Fprintf(output, "每日同步时间：%s（本机时间）\n", latestConfig.DailyTime)
			return 0
		}
		if err := saveAutoSyncConfig(cfg); err != nil {
			fmt.Fprintf(output, "开启自动 Session 同步失败：%v\n", err)
			return 1
		}
		if err := autoSyncStartBackground(); err != nil {
			cfg.Enabled = false
			_ = saveAutoSyncConfig(cfg)
			fmt.Fprintf(output, "开启自动 Session 同步失败：%v\n", err)
			return 1
		}

		fmt.Fprintln(output, "自动 Session 同步已开启")
		fmt.Fprintf(output, "每天 %s（本机时间）自动上传全部 Session\n", value)
		fmt.Fprintln(output, "如果错过该时间，将在 Aida 恢复运行后第一时间补传")
		if pastChoice != nil {
			if *pastChoice == autoSyncStartImmediate {
				fmt.Fprintln(output, "今天将立即同步一次")
			} else {
				fmt.Fprintf(output, "首次自动同步：明天 %s（本机时间）\n", value)
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
			fmt.Fprintf(output, "读取自动 Session 同步状态失败：%v\n", err)
			return 1
		}
		if !cfg.Enabled {
			fmt.Fprintln(output, "自动 Session 同步当前已经处于未开启状态")
			return 0
		}
		cfg.Enabled = false
		if err := saveAutoSyncConfig(cfg); err != nil {
			fmt.Fprintf(output, "关闭自动 Session 同步失败：%v\n", err)
			return 1
		}
		if err := autoSyncStopBackground(); err != nil {
			fmt.Fprintf(output, "自动 Session 同步已关闭，但停止后台服务失败：%v\n", err)
			return 1
		}
		fmt.Fprintln(output, "自动 Session 同步已关闭")
		fmt.Fprintln(output, "你仍可使用 aida upload 或 aida upload --all 手动上传 Session")
		return 0
	case "set-time":
		cfg, err := loadAutoSyncConfig()
		if err != nil {
			fmt.Fprintf(output, "读取自动 Session 同步状态失败：%v\n", err)
			return 1
		}
		if !cfg.Enabled {
			fmt.Fprintln(output, "自动 Session 同步尚未开启，请先运行 aida auto-sync enable")
			return 1
		}

		fmt.Fprintf(output, "当前每日同步时间：%s（本机时间）\n", cfg.DailyTime)
		reader := bufio.NewReader(input)
		var value string
		var parsed time.Time
		for {
			fmt.Fprintln(output, "请输入新的每日同步时间（HH:MM，按本机时间）：")
			line, readErr := reader.ReadString('\n')
			if readErr != nil && len(line) == 0 {
				fmt.Fprintln(output, "未修改每日同步时间")
				return 0
			}
			value = strings.TrimSpace(line)
			parsed, err = time.Parse("15:04", value)
			if err == nil && parsed.Format("15:04") == value {
				break
			}
			fmt.Fprintln(output, "时间格式无效，请使用 HH:MM，例如 18:00")
		}

		now := autoSyncNow()
		effectiveAt := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
		var pastChoice *autoSyncStartChoice
		if effectiveAt.Before(now) {
			choice, cancelled, err := autoSyncChoosePastTime(value, input, output)
			if err != nil {
				fmt.Fprintf(output, "选择首次同步时间失败：%v\n", err)
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
			fmt.Fprintf(output, "读取自动 Session 同步状态失败：%v\n", err)
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
			fmt.Fprintf(output, "修改每日同步时间失败：%v\n", err)
			return 1
		}
		fmt.Fprintf(output, "每日同步时间已更新为 %s（本机时间）\n", value)
		if pastChoice != nil {
			if *pastChoice == autoSyncStartImmediate {
				fmt.Fprintln(output, "今天将立即同步一次")
			} else {
				fmt.Fprintf(output, "首次自动同步：明天 %s（本机时间）\n", value)
			}
		}
		return 0
	}

	fmt.Fprintln(output, "Usage: aida auto-sync <enable|status|set-time|disable>")
	return 2
}
