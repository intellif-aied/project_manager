package sessiondigestv2

import "time"

func AnnotatePeriodRelations(digest *Digest, periodStart, periodEnd time.Time, location *time.Location) {
	if digest == nil {
		return
	}
	if location == nil {
		location = time.UTC
	}
	startDate := dateKey(periodStart, location)
	endDate := dateKey(periodEnd, location)
	for index := range digest.WorkUnits {
		unit := &digest.WorkUnits[index]
		unitStart, startOK := parseTimestamp(unit.ActivityStartAt)
		unitEnd, endOK := parseTimestamp(unit.ActivityEndAt)
		switch {
		case !startOK && !endOK:
			unit.PeriodRelation = "unknown"
		case endOK && dateKey(unitEnd, location) < startDate:
			unit.PeriodRelation = "before"
		case startOK && dateKey(unitStart, location) > endDate:
			unit.PeriodRelation = "after"
		default:
			unit.PeriodRelation = "overlap"
		}
	}
}

func parseTimestamp(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func dateKey(value time.Time, location *time.Location) string {
	return value.In(location).Format("2006-01-02")
}
