//go:build darwin

package main

import "errors"

func acquireSessionUploadLock() (func(), error) {
	release, err := acquireAutoSyncFileLock(sessionUploadLockPath())
	if errors.Is(err, errAutoSyncLockHeld) {
		return nil, errSessionUploadBusy
	}
	return release, err
}
