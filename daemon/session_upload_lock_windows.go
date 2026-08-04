//go:build windows

package main

func acquireSessionUploadLock() (func(), error) {
	return acquireAutoSyncFileLock(sessionUploadLockPath())
}
