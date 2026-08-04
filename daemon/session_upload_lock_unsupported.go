//go:build !linux && !darwin && !windows

package main

func acquireSessionUploadLock() (func(), error) {
	return func() {}, nil
}
