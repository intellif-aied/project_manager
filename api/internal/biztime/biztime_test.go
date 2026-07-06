package biztime

import (
	"testing"
	"time"
)

func TestBusinessDateUsesShanghaiBoundary(t *testing.T) {
	utc := time.Date(2026, 7, 5, 16, 30, 0, 0, time.UTC)
	if got := Date(utc); got != "2026-07-06" {
		t.Fatalf("Date() = %s, want 2026-07-06", got)
	}
	if got := WeekStart(utc).Format("2006-01-02"); got != "2026-07-06" {
		t.Fatalf("WeekStart() = %s, want 2026-07-06", got)
	}
	if got := MonthStart(utc).Format("2006-01-02"); got != "2026-07-01" {
		t.Fatalf("MonthStart() = %s, want 2026-07-01", got)
	}
}
