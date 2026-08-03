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
	"time"
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
	if err := checkSystemdUserManager(runAutoSyncSystemctl); err == nil {
		return nil
	}
	if _, err := os.UserHomeDir(); err != nil {
		return err
	}
	if _, err := os.Executable(); err != nil {
		return err
	}
	return os.MkdirAll(autoSyncDir(), 0700)
}

func startAutoSyncBackground() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return startAutoSyncBackgroundWithFallback(
		func() error { return checkSystemdUserManager(runAutoSyncSystemctl) },
		func() error { return ensureSystemdAutoSyncUnit(home, executable, runAutoSyncSystemctl) },
		func() error { return startDetachedAutoSync(executable) },
	)
}

func stopAutoSyncBackground() error {
	if err := checkSystemdUserManager(runAutoSyncSystemctl); err == nil {
		return runAutoSyncSystemctl("--user", "disable", "--now", "aida-auto-sync.service")
	}
	return stopDetachedAutoSync()
}

func restartAutoSyncBackground() error {
	if err := checkSystemdUserManager(runAutoSyncSystemctl); err == nil {
		return runAutoSyncSystemctl("--user", "restart", "aida-auto-sync.service")
	}
	if err := stopDetachedAutoSync(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return startDetachedAutoSync(executable)
}

type autoSyncSystemctlRunner func(args ...string) error

func startAutoSyncBackgroundWithFallback(checkSystemd, startSystemd, startDetached func() error) error {
	if err := checkSystemd(); err == nil {
		return startSystemd()
	}
	return startDetached()
}

func autoSyncDetachedPIDPath() string {
	return filepath.Join(autoSyncDir(), "daemon.pid")
}

func startDetachedAutoSync(executable string) error {
	if err := os.MkdirAll(autoSyncDir(), 0700); err != nil {
		return err
	}
	pidPath := autoSyncDetachedPIDPath()
	if pid, err := readAutoSyncPID(pidPath); err == nil && autoSyncProcessRunning(pid) {
		return nil
	}
	_ = os.Remove(pidPath)

	logFile, err := os.OpenFile(filepath.Join(autoSyncDir(), "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	command := exec.Command(executable, "auto-sync", "daemon-run")
	command.Stdin = nil
	command.Stdout = logFile
	command.Stderr = logFile
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := command.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("start detached AutoSync daemon: %w", err)
	}
	pid := command.Process.Pid
	if err := writeAutoSyncPID(pidPath, pid); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		logFile.Close()
		return err
	}
	if err := command.Process.Release(); err != nil {
		_ = syscall.Kill(pid, syscall.SIGTERM)
		_ = os.Remove(pidPath)
		logFile.Close()
		return err
	}
	return logFile.Close()
}

func stopDetachedAutoSync() error {
	pidPath := autoSyncDetachedPIDPath()
	pid, err := readAutoSyncPID(pidPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if autoSyncProcessRunning(pid) {
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
			return err
		}
		deadline := time.Now().Add(2 * time.Second)
		for autoSyncProcessRunning(pid) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if autoSyncProcessRunning(pid) {
			if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
				return err
			}
		}
	}
	return os.Remove(pidPath)
}

func readAutoSyncPID(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid AutoSync daemon pid")
	}
	return pid, nil
}

func writeAutoSyncPID(path string, pid int) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), ".daemon-pid-*.tmp")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := fmt.Fprintf(temporary, "%d\n", pid); err != nil {
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
	return os.Rename(temporaryPath, path)
}

func autoSyncProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	if err := syscall.Kill(pid, 0); err != nil {
		return err == syscall.EPERM
	}
	cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ReplaceAll(string(cmdline), "\x00", " "), "auto-sync daemon-run")
}

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
KillMode=control-group
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

func ensureSystemdAutoSyncUnit(home, executable string, run autoSyncSystemctlRunner) error {
	if err := run("--user", "is-active", "--quiet", "aida-auto-sync.service"); err == nil {
		return nil
	}
	return installSystemdAutoSyncUnit(home, executable, run)
}
