package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var autoSyncNow = time.Now

var autoSyncStartBackground = func() error {
	return fmt.Errorf("Linux 后台机制尚未确认")
}

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

func cmdAutoSyncCLI(args []string, input io.Reader, output io.Writer, interactive bool) int {
	if len(args) == 1 && args[0] == "enable" && !interactive {
		fmt.Fprintln(output, "开启自动 Session 同步需要交互式终端，请运行 aida auto-sync enable")
		return 2
	}
	return cmdAutoSync(args, input, output)
}

func cmdAutoSync(args []string, input io.Reader, output io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(output, "Usage: aida auto-sync <enable|status|set-time|disable>")
		return 2
	}

	switch args[0] {
	case "status":
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
	case "enable":
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
			choice, cancelled, chooseErr := autoSyncChoosePastTime(value, reader, output)
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
		cfg, err := loadAutoSyncConfig()
		if err != nil {
			fmt.Fprintf(output, "读取自动 Session 同步状态失败：%v\n", err)
			return 1
		}
		cfg.Enabled = false
		if err := saveAutoSyncConfig(cfg); err != nil {
			fmt.Fprintf(output, "关闭自动 Session 同步失败：%v\n", err)
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
		fmt.Fprintln(output, "请输入新的每日同步时间（HH:MM，按本机时间）：")
		value, err := bufio.NewReader(input).ReadString('\n')
		if err != nil && len(value) == 0 {
			fmt.Fprintln(output, "未修改每日同步时间")
			return 1
		}
		value = strings.TrimSpace(value)
		parsed, err := time.Parse("15:04", value)
		if err != nil || parsed.Format("15:04") != value {
			fmt.Fprintln(output, "时间格式无效，请使用 HH:MM，例如 18:00")
			return 1
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
