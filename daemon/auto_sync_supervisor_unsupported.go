//go:build !linux

package main

func checkAutoSyncBackgroundSupport() error {
	return errAutoSyncSystemdUnavailable
}

func startAutoSyncBackground() error {
	return errAutoSyncSystemdUnavailable
}

func stopAutoSyncBackground() error {
	return nil
}
