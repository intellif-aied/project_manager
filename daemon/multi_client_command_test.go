package main

import "testing"

func TestOpenClawBulkUploadIsAlwaysRejectedBeforeDiscovery(t *testing.T) {
	if code := cmdUploadClient([]string{"openclaw", "--all"}); code != 2 {
		t.Fatalf("exit code=%d, want 2", code)
	}
	if code := cmdUploadClient([]string{"openclaw", "-a"}); code != 2 {
		t.Fatalf("short flag exit code=%d, want 2", code)
	}
}
