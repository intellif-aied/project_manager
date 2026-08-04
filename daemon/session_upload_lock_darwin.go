//go:build darwin

package main

func acquireSessionUploadLock() (func(), error) {
	return acquireAutoSyncFileLock(sessionUploadLockPath())
}
