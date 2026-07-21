package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestBeginSessionUploadReportsBusyWithoutInternalDetails(t *testing.T) {
	original := sessionUploadAcquire
	sessionUploadAcquire = func() (func(), error) {
		return nil, errSessionUploadBusy
	}
	t.Cleanup(func() { sessionUploadAcquire = original })

	var output bytes.Buffer
	release, code := beginSessionUpload(&output)
	if release != nil || code != 75 {
		t.Fatalf("release=%v code=%d, want nil and 75", release != nil, code)
	}
	if got := output.String(); got != "已有 Session 上传正在进行，请稍后再试\n" {
		t.Fatalf("output=%q", got)
	}
	for _, forbidden := range []string{"lock", "PID", "daemon", "auto-sync"} {
		if strings.Contains(output.String(), forbidden) {
			t.Fatalf("output exposed %q: %q", forbidden, output.String())
		}
	}
}

func TestBeginSessionUploadReturnsReleaseFunction(t *testing.T) {
	original := sessionUploadAcquire
	released := false
	sessionUploadAcquire = func() (func(), error) {
		return func() { released = true }, nil
	}
	t.Cleanup(func() { sessionUploadAcquire = original })

	release, code := beginSessionUpload(&bytes.Buffer{})
	if code != 0 || release == nil {
		t.Fatalf("release=%v code=%d", release != nil, code)
	}
	release()
	if !released {
		t.Fatal("release function was not called")
	}
}

func TestBeginSessionUploadHidesLockFailure(t *testing.T) {
	original := sessionUploadAcquire
	sessionUploadAcquire = func() (func(), error) {
		return nil, errors.New("open /home/user/.aida/upload.lock: permission denied")
	}
	t.Cleanup(func() { sessionUploadAcquire = original })

	var output bytes.Buffer
	_, code := beginSessionUpload(&output)
	if code != 1 || output.String() != "暂时无法开始 Session 上传，请稍后再试\n" {
		t.Fatalf("code=%d output=%q", code, output.String())
	}
}
