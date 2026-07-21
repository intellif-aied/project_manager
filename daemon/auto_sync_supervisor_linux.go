package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

func runAutoSyncSystemctl(args ...string) error {
	command := exec.Command("systemctl", args...)
	if output, err := command.CombinedOutput(); err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, message)
	}
	return nil
}

func checkAutoSyncBackgroundSupport() error {
	return checkSystemdUserManager(runAutoSyncSystemctl)
}

func startAutoSyncBackground() error {
	if err := checkAutoSyncBackgroundSupport(); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return installSystemdAutoSyncUnit(home, executable, runAutoSyncSystemctl)
}

func stopAutoSyncBackground() error {
	return runAutoSyncSystemctl("--user", "disable", "--now", "aida-auto-sync.service")
}

func restartAutoSyncBackground() error {
	return runAutoSyncSystemctl("--user", "restart", "aida-auto-sync.service")
}

type autoSyncSystemctlRunner func(args ...string) error

func acquireAutoSyncFileLock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
			return nil, errAutoSyncLockHeld
		}
		return nil, err
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
			_ = file.Close()
		})
	}, nil
}

func checkSystemdUserManager(run autoSyncSystemctlRunner) error {
	if err := run("--user", "show-environment"); err != nil {
		return fmt.Errorf("%w: %v", errAutoSyncSystemdUnavailable, err)
	}
	return nil
}

func installSystemdAutoSyncUnit(home, executable string, run autoSyncSystemctlRunner) error {
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0755); err != nil {
		return fmt.Errorf("create systemd user directory: %w", err)
	}
	executableArg := strings.ReplaceAll(strconv.Quote(executable), "%", "%%")
	unit := `[Unit]
Description=Aida automatic Session sync
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=` + executableArg + ` auto-sync daemon-run
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=default.target
`
	temporary, err := os.CreateTemp(unitDir, ".aida-auto-sync-*.tmp")
	if err != nil {
		return fmt.Errorf("create systemd unit: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0644); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.WriteString(unit); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	unitPath := filepath.Join(unitDir, "aida-auto-sync.service")
	if err := os.Rename(temporaryPath, unitPath); err != nil {
		return fmt.Errorf("install systemd unit: %w", err)
	}
	if err := run("--user", "daemon-reload"); err != nil {
		return fmt.Errorf("reload systemd user units: %w", err)
	}
	if err := run("--user", "enable", "--now", "aida-auto-sync.service"); err != nil {
		_ = run("--user", "disable", "--now", "aida-auto-sync.service")
		return fmt.Errorf("start systemd user unit: %w", err)
	}
	return nil
}
