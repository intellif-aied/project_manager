//go:build linux

package main

import (
	"errors"
	"os"
	"path/filepath"
)

func acquireSessionUploadLock() (func(), error) {
	path := sessionUploadLockPath()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	release, err := acquireAutoSyncFileLock(path)
	if errors.Is(err, errAutoSyncLockHeld) {
		return nil, errSessionUploadBusy
	}
	return release, err
}
