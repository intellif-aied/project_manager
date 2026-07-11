package handler

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	reportUUIDPattern             = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	reportRawIDFieldPattern       = regexp.MustCompile(`(?i)\b(?:user_id|team_id|department_id|leader_id|director_user_id|report_id|session_id|run_id|uuid)\b`)
	reportIDLabelPattern          = regexp.MustCompile(`(?i)(?:用户|成员|团队|小组|部门|负责人|总监|报告|会话|运行)\s*(?:id|编号)`)
	reportISOWeekdayPattern       = regexp.MustCompile(`(20[0-9]{2})-([0-9]{1,2})-([0-9]{1,2})\s*(?:[（(]\s*)?(?:周|星期)([一二三四五六日天])\s*[）)]?`)
	reportChineseWeekdayPattern   = regexp.MustCompile(`(?:(20[0-9]{2})\s*年\s*)?([0-9]{1,2})\s*月\s*([0-9]{1,2})\s*日\s*(?:[（(]\s*)?(?:周|星期)([一二三四五六日天])\s*[）)]?`)
	reportNoPersonalDailyPattern  = regexp.MustCompile(`(?:无(?:任何)?个人日报(?:记录)?|个人日报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
	reportNoPersonalWeeklyPattern = regexp.MustCompile(`(?:无(?:任何)?个人周报(?:记录)?|个人周报\s*[:：]?\s*(?:0\s*份|零份|无人提交|均未提交))`)
)

func reportContentValidationIssues(content, date, weekStart, weekEnd string) []string {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	issues := []string{}
	seen := map[string]struct{}{}
	add := func(issue string) {
		if issue == "" {
			return
		}
		if _, ok := seen[issue]; ok {
			return
		}
		seen[issue] = struct{}{}
		issues = append(issues, issue)
	}

	plain := strings.NewReplacer("**", "", "__", "", "`", "").Replace(content)
	if reportRawIDFieldPattern.MatchString(plain) || reportIDLabelPattern.MatchString(plain) {
		add("报告正文包含内部 ID 字段或 ID/编号标签，请改用展示名称")
	}
	if reportUUIDPattern.MatchString(content) {
		add("报告正文包含 UUID，请删除内部标识")
	}

	periodStart, periodEnd := reportValidationPeriodBounds(date, weekStart, weekEnd)
	for _, match := range reportISOWeekdayPattern.FindAllStringSubmatch(content, -1) {
		year, _ := strconv.Atoi(match[1])
		month, _ := strconv.Atoi(match[2])
		day, _ := strconv.Atoi(match[3])
		if value, ok := validCalendarDate(year, month, day); ok {
			addWeekdayValidationIssue(add, value, match[4])
		}
	}
	for _, match := range reportChineseWeekdayPattern.FindAllStringSubmatch(content, -1) {
		month, _ := strconv.Atoi(match[2])
		day, _ := strconv.Atoi(match[3])
		var value time.Time
		var ok bool
		if match[1] != "" {
			year, _ := strconv.Atoi(match[1])
			value, ok = validCalendarDate(year, month, day)
		} else {
			value, ok = resolvePartialReportDate(month, day, periodStart, periodEnd)
		}
		if ok {
			addWeekdayValidationIssue(add, value, match[4])
		}
	}
	return issues
}

func reportValidationPeriodBounds(date, weekStart, weekEnd string) (time.Time, time.Time) {
	if date != "" {
		value, _ := time.Parse("2006-01-02", date)
		return value, value
	}
	start, _ := time.Parse("2006-01-02", weekStart)
	end, _ := time.Parse("2006-01-02", weekEnd)
	return start, end
}

func validCalendarDate(year, month, day int) (time.Time, bool) {
	if year <= 0 || month <= 0 || day <= 0 {
		return time.Time{}, false
	}
	value := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	if value.Year() != year || int(value.Month()) != month || value.Day() != day {
		return time.Time{}, false
	}
	return value, true
}

func resolvePartialReportDate(month, day int, periodStart, periodEnd time.Time) (time.Time, bool) {
	years := []int{periodStart.Year()}
	if periodEnd.Year() != 0 && periodEnd.Year() != periodStart.Year() {
		years = append(years, periodEnd.Year())
	}
	var first time.Time
	for _, year := range years {
		value, ok := validCalendarDate(year, month, day)
		if !ok {
			continue
		}
		if first.IsZero() {
			first = value
		}
		if !periodStart.IsZero() && !periodEnd.IsZero() && !value.Before(periodStart) && !value.After(periodEnd) {
			return value, true
		}
	}
	return first, !first.IsZero()
}

func addWeekdayValidationIssue(add func(string), value time.Time, shown string) {
	want := reportWeekdayLabel(value)
	shown = "周" + strings.ReplaceAll(shown, "天", "日")
	if shown == want {
		return
	}
	add(fmt.Sprintf("日期 %s 的星期应为%s，正文写成了%s；请修正或删除星期", value.Format("2006-01-02"), want, shown))
}

func reportSourceConsistencyIssues(ctx context.Context, db *sql.DB, reportType, date, weekStart, weekEnd string, target reportTarget, content string) ([]string, error) {
	var table, periodColumn, periodValue, scopeClause, scopeID, sourceLabel string
	switch reportType {
	case reportTypeTeamDaily:
		if !reportNoPersonalDailyPattern.MatchString(content) {
			return nil, nil
		}
		table, periodColumn, periodValue = "daily_reports", "report_date", date
		scopeClause, scopeID, sourceLabel = "u.team_id::text = $2", target.TeamID, "个人日报"
	case reportTypeDepartmentDaily:
		if !reportNoPersonalDailyPattern.MatchString(content) {
			return nil, nil
		}
		table, periodColumn, periodValue = "daily_reports", "report_date", date
		scopeClause, scopeID, sourceLabel = "t.director_user_id::text = $2", target.DepartmentID, "个人日报"
	case reportTypeTeamWeekly:
		if !reportNoPersonalWeeklyPattern.MatchString(content) {
			return nil, nil
		}
		table, periodColumn, periodValue = "personal_weekly_reports", "week_start", weekStart
		scopeClause, scopeID, sourceLabel = "u.team_id::text = $2", target.TeamID, "个人周报"
	case reportTypeDepartmentWeekly:
		if !reportNoPersonalWeeklyPattern.MatchString(content) {
			return nil, nil
		}
		table, periodColumn, periodValue = "personal_weekly_reports", "week_start", weekStart
		scopeClause, scopeID, sourceLabel = "t.director_user_id::text = $2", target.DepartmentID, "个人周报"
	default:
		return nil, nil
	}
	if scopeID == "" || periodValue == "" {
		return nil, nil
	}

	query := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %s r
		JOIN users u ON u.id = r.user_id
		LEFT JOIN teams t ON t.id = u.team_id
		WHERE r.%s = $1
		  AND %s
		  AND r.status IS NOT NULL
		  AND NULLIF(TRIM(COALESCE(r.content, '')), '') IS NOT NULL`, table, periodColumn, scopeClause)
	var count int
	if err := db.QueryRowContext(ctx, query, periodValue, scopeID).Scan(&count); err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	period := periodValue
	if weekEnd != "" {
		period += " 至 " + weekEnd
	}
	return []string{fmt.Sprintf("数据源在 %s 实际包含 %d 份%s，正文却声称没有或为 0 份；请按下级报告正文修正覆盖情况", period, count, sourceLabel)}, nil
}
