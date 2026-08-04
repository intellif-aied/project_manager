//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

const autoSyncLaunchAgentLabel = "com.aida.auto-sync"

func autoSyncLaunchAgentPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", autoSyncLaunchAgentLabel+".plist")
}

func checkAutoSyncBackgroundSupport() error {
	if _, err := exec.LookPath("launchctl"); err != nil {
		return err
	}
	_, err := os.Executable()
	return err
}

func startAutoSyncBackground() error {
	if err := checkAutoSyncBackgroundSupport(); err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	path := autoSyncLaunchAgentPath()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	plist := fmt.Sprintf("<?xml version=\"1.0\" encoding=\"UTF-8\"?><!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\"><plist version=\"1.0\"><dict><key>Label</key><string>%s</string><key>ProgramArguments</key><array><string>%s</string><string>auto-sync</string><string>tick</string></array><key>StartInterval</key><integer>60</integer><key>RunAtLoad</key><true/></dict></plist>\n", autoSyncLaunchAgentLabel, xmlEscape(executable))
	if err := os.WriteFile(path, []byte(plist), 0644); err != nil {
		return err
	}
	uid := fmt.Sprint(os.Getuid())
	_ = exec.Command("launchctl", "bootout", "gui/"+uid, autoSyncLaunchAgentLabel).Run()
	return exec.Command("launchctl", "bootstrap", "gui/"+uid, path).Run()
}

func stopAutoSyncBackground() error {
	uid := fmt.Sprint(os.Getuid())
	err := exec.Command("launchctl", "bootout", "gui/"+uid, autoSyncLaunchAgentLabel).Run()
	if err != nil {
		return nil
	}
	return os.Remove(autoSyncLaunchAgentPath())
}
func restartAutoSyncBackground() error {
	_ = stopAutoSyncBackground()
	return startAutoSyncBackground()
}
func xmlEscape(value string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\"", "&quot;").Replace(value)
}
func acquireAutoSyncFileLock(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		file.Close()
		if err == unix.EWOULDBLOCK {
			return nil, errAutoSyncLockHeld
		}
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(func() { _ = unix.Flock(int(file.Fd()), unix.LOCK_UN); _ = file.Close() }) }, nil
}
