//go:build !linux

package main

func acquireSessionUploadLock() (func(), error) {
	return func() {}, nil
}
