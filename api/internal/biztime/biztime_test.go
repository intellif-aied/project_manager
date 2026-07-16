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

func TestFormatRFC3339UsesShanghaiOffsetWithoutChangingInstant(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{input: "2026-07-15T01:00:00Z", want: "2026-07-15T09:00:00+08:00"},
		{input: "2026-07-15T16:00:00Z", want: "2026-07-16T00:00:00+08:00"},
	}
	for _, test := range tests {
		input, err := time.Parse(time.RFC3339, test.input)
		if err != nil {
			t.Fatal(err)
		}
		got := FormatRFC3339(input)
		if got != test.want {
			t.Fatalf("FormatRFC3339(%s) = %s, want %s", test.input, got, test.want)
		}
		converted, err := time.Parse(time.RFC3339, got)
		if err != nil {
			t.Fatal(err)
		}
		if !converted.Equal(input) {
			t.Fatalf("conversion changed instant: input=%s output=%s", input, converted)
		}
	}
}

func TestDateBoundsUsesShanghaiNaturalDays(t *testing.T) {
	start, end, err := DateBounds("2026-07-16", "2026-07-16")
	if err != nil {
		t.Fatal(err)
	}
	if got := start.Format(time.RFC3339); got != "2026-07-15T16:00:00Z" {
		t.Fatalf("start = %s", got)
	}
	if got := end.Format(time.RFC3339); got != "2026-07-16T16:00:00Z" {
		t.Fatalf("end = %s", got)
	}
	if _, _, err := DateBounds("2026-07-17", "2026-07-16"); err == nil {
		t.Fatal("expected reversed range to fail")
	}
}
