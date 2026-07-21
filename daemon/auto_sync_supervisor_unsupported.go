//go:build !linux

package main

import "fmt"

func acquireAutoSyncFileLock(string) (func(), error) {
	return nil, fmt.Errorf("%w: file locking is supported only on Linux", errAutoSyncSystemdUnavailable)
}

func checkAutoSyncBackgroundSupport() error {
	return errAutoSyncSystemdUnavailable
}

func startAutoSyncBackground() error {
	return errAutoSyncSystemdUnavailable
}

func stopAutoSyncBackground() error {
	return nil
}

func restartAutoSyncBackground() error {
	return errAutoSyncSystemdUnavailable
}
