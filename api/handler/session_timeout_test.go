package handler

import (
	"testing"
	"time"
)

func TestSessionRawLogUploadTimeoutCoversLargeUploads(t *testing.T) {
	if sessionRawLogUploadTimeout != 5*time.Minute {
		t.Fatalf("session raw log upload timeout = %s, want 5m", sessionRawLogUploadTimeout)
	}
}
