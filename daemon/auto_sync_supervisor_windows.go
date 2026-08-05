//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"golang.org/x/sys/windows"
)

const autoSyncWindowsTaskName = "AidaAutoSync"

func checkAutoSyncBackgroundSupport() error {
	if _, err := exec.LookPath("schtasks.exe"); err != nil {
		return err
	}
	if _, err := exec.LookPath("wscript.exe"); err != nil {
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
	if err := os.MkdirAll(autoSyncDir(), 0700); err != nil {
		return err
	}
	launcherPath := filepath.Join(autoSyncDir(), autoSyncWindowsLauncherName)
	if err := os.WriteFile(launcherPath, []byte(windowsAutoSyncLauncherScript(executable)), 0600); err != nil {
		return err
	}
	task := windowsAutoSyncTaskAction(launcherPath)
	return exec.Command("schtasks.exe", "/Create", "/TN", autoSyncWindowsTaskName, "/TR", task, "/SC", "MINUTE", "/MO", "1", "/F").Run()
}
func stopAutoSyncBackground() error {
	_ = exec.Command("schtasks.exe", "/Delete", "/TN", autoSyncWindowsTaskName, "/F").Run()
	_ = os.Remove(filepath.Join(autoSyncDir(), autoSyncWindowsLauncherName))
	return nil
}
func restartAutoSyncBackground() error {
	_ = stopAutoSyncBackground()
	return startAutoSyncBackground()
}
func acquireAutoSyncFileLock(path string) (func(), error) {
	name, err := windows.UTF16PtrFromString("Global\\AidaAutoSync-" + autoSyncLockIdentity(path))
	if err != nil {
		return nil, err
	}
	handle, err := windows.CreateMutex(nil, false, name)
	if err != nil {
		return nil, err
	}
	if windows.GetLastError() == windows.ERROR_ALREADY_EXISTS {
		windows.CloseHandle(handle)
		return nil, errAutoSyncLockHeld
	}
	var once sync.Once
	return func() { once.Do(func() { _ = windows.CloseHandle(handle) }) }, nil
}
