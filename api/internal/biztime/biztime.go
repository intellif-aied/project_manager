package biztime

import (
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
	return time.Now().In(Location())
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
