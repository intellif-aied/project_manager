package biztime

import (
	"fmt"
	"sync"
	"time"
)

const Zone = "Asia/Shanghai"

var (
	locationOnce sync.Once
	location     *time.Location
)

func Location() *time.Location {
	locationOnce.Do(func() {
		loc, err := time.LoadLocation(Zone)
		if err != nil {
			loc = time.FixedZone(Zone, 8*3600)
		}
		location = loc
	})
	return location
}

func Now() time.Time {
	return InLocation(time.Now())
}

// InLocation converts an absolute timestamp to Aida's business timezone
// without changing the represented instant.
func InLocation(value time.Time) time.Time {
	return value.In(Location())
}

// FormatRFC3339 returns an explicit Asia/Shanghai RFC3339 timestamp for model
// and UI boundaries. RFC3339Nano preserves any source precision.
func FormatRFC3339(value time.Time) string {
	return InLocation(value).Format(time.RFC3339Nano)
}

func Date(value time.Time) string {
	return value.In(Location()).Format("2006-01-02")
}

func Today() string {
	return Date(time.Now())
}

func StartOfDay(value time.Time) time.Time {
	local := value.In(Location())
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, Location())
}

func WeekStart(value time.Time) time.Time {
	start := StartOfDay(value)
	weekday := int(start.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return start.AddDate(0, 0, 1-weekday)
}

func MonthStart(value time.Time) time.Time {
	local := value.In(Location())
	return time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, Location())
}

func ParseDate(value string) (time.Time, error) {
	return time.ParseInLocation("2006-01-02", value, Location())
}

// DateBounds converts an inclusive business-date range to UTC timestamp
// bounds suitable for TIMESTAMPTZ queries: [start, endExclusive).
func DateBounds(dateStart, dateEnd string) (startUTC, endExclusiveUTC time.Time, err error) {
	start, err := ParseDate(dateStart)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start date: %w", err)
	}
	end, err := ParseDate(dateEnd)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end date: %w", err)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, fmt.Errorf("end date must be on or after start date")
	}
	return start.UTC(), end.AddDate(0, 0, 1).UTC(), nil
}
