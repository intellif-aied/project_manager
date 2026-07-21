package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var (
	errSessionUploadBusy = errors.New("session upload is already running")
	sessionUploadAcquire = acquireSessionUploadLock
)

func sessionUploadLockPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".aida", "upload.lock")
}

func beginSessionUpload(output io.Writer) (func(), int) {
	release, err := sessionUploadAcquire()
	if errors.Is(err, errSessionUploadBusy) {
		fmt.Fprintln(output, "已有 Session 上传正在进行，请稍后再试")
		return nil, 75
	}
	if err != nil {
		fmt.Fprintln(output, "暂时无法开始 Session 上传，请稍后再试")
		return nil, 1
	}
	return release, 0
}
