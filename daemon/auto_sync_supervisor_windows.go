//go:build windows

package main

import (
	"os"
	"os/exec"
	"sync"

	"golang.org/x/sys/windows"
)

const autoSyncWindowsTaskName = "AidaAutoSync"

func checkAutoSyncBackgroundSupport() error {
	if _, err := exec.LookPath("schtasks.exe"); err != nil {
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
	task := "\"" + executable + "\" auto-sync tick"
	return exec.Command("schtasks.exe", "/Create", "/TN", autoSyncWindowsTaskName, "/TR", task, "/SC", "MINUTE", "/MO", "1", "/F").Run()
}
func stopAutoSyncBackground() error {
	_ = exec.Command("schtasks.exe", "/Delete", "/TN", autoSyncWindowsTaskName, "/F").Run()
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
