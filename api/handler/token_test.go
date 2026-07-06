package handler

import (
	"testing"
	"time"
)

func TestResolvePeriodAtUsesBusinessTimezone(t *testing.T) {
	now := time.Date(2026, 7, 5, 16, 30, 0, 0, time.UTC) // 2026-07-06 00:30 Asia/Shanghai.

	start, end, err := resolvePeriodAt("today", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-06" || end != "2026-07-06" {
		t.Fatalf("today = %s..%s, want 2026-07-06..2026-07-06", start, end)
	}

	start, end, err = resolvePeriodAt("week", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-06" || end != "2026-07-06" {
		t.Fatalf("week = %s..%s, want 2026-07-06..2026-07-06", start, end)
	}

	start, end, err = resolvePeriodAt("month", "", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if start != "2026-07-01" || end != "2026-07-06" {
		t.Fatalf("month = %s..%s, want 2026-07-01..2026-07-06", start, end)
	}
}
